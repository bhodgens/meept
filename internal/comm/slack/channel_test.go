package slack

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/services"
)

// newTestAPIServer spins an httptest server standing in for the Slack Web
// API. It records the last request body and the last response ts.
type apiCapture struct {
	srv    *httptest.Server
	method string
	body   map[string]any
	respTS string
}

func newTestAPIServer(t *testing.T, status int, envelope map[string]any) *apiCapture {
	t.Helper()
	cap := &apiCapture{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &cap.body)
		cap.method = strings.TrimPrefix(r.URL.Path, "/")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(envelope)
		if ts, ok := envelope["ts"].(string); ok {
			cap.respTS = ts
		}
	})
	cap.srv = httptest.NewServer(mux)
	t.Cleanup(cap.srv.Close)
	return cap
}

func testSlackCfg(base string) SlackConfig {
	return SlackConfig{
		Enabled:        true,
		BotToken:       "xoxb-test-token",
		SigningSecret:  "test-secret",
		ChannelID:      "CCHAN",
		AllowedUserIDs: []string{"U123ABC"},
		APIBaseURL:     base,
	}
}

func TestPushPostsChatPostMessageWithButtons(t *testing.T) {
	// T2: Push posts chat.postMessage JSON to the (test) API server.
	cap := newTestAPIServer(t, http.StatusOK, map[string]any{"ok": true, "ts": "1700000000.000100"})
	ch := NewSlackChannel(testSlackCfg(cap.srv.URL), slog.Default())

	msg := &services.PushMessage{
		SessionID: "s1",
		Type:      services.PushTypeAlert,
		Content:   "build finished",
	}
	if err := ch.Push(context.Background(), msg); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if cap.method != "chat.postMessage" {
		t.Fatalf("api method = %q, want chat.postMessage", cap.method)
	}
	if cap.body["channel"] != "CCHAN" {
		t.Errorf("channel = %v, want CCHAN", cap.body["channel"])
	}

	// The Notifier path (interactive card) must carry the pinned action
	// grammar perm:<requestID>:<verb> for all three verbs.
	ch2 := NewSlackChannel(testSlackCfg(cap.srv.URL), slog.Default())
	req := acp.PendingPermission{ID: "perm-r1", AgentID: "agd", Summary: "run rm -rf"}
	if !ch2.Notifier().NotifyPermissionRequested(req) {
		t.Fatal("NotifyPermissionRequested = false, want true")
	}
	ch2.Wait() // test seam: wait for the async post
	if cap.method != "chat.postMessage" {
		t.Fatalf("api method = %q, want chat.postMessage", cap.method)
	}
	raw, _ := json.Marshal(cap.body["blocks"])
	blocksJSON := string(raw)
	for _, verb := range []string{"allow", "deny", "always"} {
		want := ActionValue("perm-r1", verb)
		if !strings.Contains(blocksJSON, want) {
			t.Errorf("blocks missing action value %q", want)
		}
	}
}

func TestNotifierIsNonBlocking(t *testing.T) {
	// The PermissionNotifier contract (relay.go:85-98, mutexio): calls
	// return within milliseconds even though the HTTP post takes longer.
	// The test API server blocks until released.
	release := make(chan struct{})
	var releaseOnce sync.Once
	doRelease := func() { releaseOnce.Do(func() { close(release) }) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "ts": "1.1"})
	}))

	ch := NewSlackChannel(testSlackCfg(srv.URL), slog.Default())
	t.Cleanup(func() {
		doRelease()
		ch.Wait()
		srv.Close()
	})

	start := time.Now()
	if !ch.Notifier().NotifyPermissionRequested(acp.PendingPermission{ID: "perm-nb"}) {
		t.Fatal("NotifyPermissionRequested = false, want true")
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("NotifyPermissionRequested blocked %v (mutexio violation)", elapsed)
	}
	doRelease()
	ch.Wait()
}

func TestNotifyPermissionResolvedUpdatesCard(t *testing.T) {
	cap := newTestAPIServer(t, http.StatusOK, map[string]any{"ok": true, "ts": "1700000000.000500"})
	ch := NewSlackChannel(testSlackCfg(cap.srv.URL), slog.Default())

	if !ch.Notifier().NotifyPermissionRequested(acp.PendingPermission{ID: "perm-up"}) {
		t.Fatal("NotifyPermissionRequested = false")
	}
	ch.Wait()
	if cap.method != "chat.postMessage" {
		t.Fatalf("expected postMessage, got %q", cap.method)
	}
	postedTS := cap.respTS
	if postedTS == "" {
		t.Fatal("test API server returned no ts")
	}

	ch.Notifier().NotifyPermissionResolved(
		acp.PendingPermission{ID: "perm-up"}, "allow", "answered",
		acp.Approver{Kind: acp.ApproverKindSlack, Detail: "U123ABC"})
	ch.Wait()

	if cap.method != "chat.update" {
		t.Fatalf("api method = %q, want chat.update (card flip)", cap.method)
	}
	if cap.body["ts"] != postedTS {
		t.Errorf("update ts = %v, want the posted ts %q", cap.body["ts"], postedTS)
	}
}

func TestNotifyPermissionRequestedDisabledIsFalse(t *testing.T) {
	ch := NewSlackChannel(SlackConfig{}, slog.Default())
	if ch.Notifier().NotifyPermissionRequested(acp.PendingPermission{ID: "x"}) {
		t.Fatal("disabled channel reported a live listener")
	}
}
