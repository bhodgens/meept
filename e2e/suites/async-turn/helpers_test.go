//go:build e2e

// Shared HTTP submit helpers for the async-turn suite: raw submit variants
// for rejection-path tests.
package asyncturn

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// postChatSubmit posts an arbitrary submit payload to the HTTP endpoint.
func postChatSubmit(t *testing.T, s *harness.Stack, payload map[string]any) map[string]any {
	t.Helper()
	status, body := postRawChatSubmit(t, s, mustJSON(t, payload))
	if status != http.StatusOK {
		t.Fatalf("chat submit status %d: %s", status, body)
	}
	var ack map[string]any
	if err := json.Unmarshal(body, &ack); err != nil {
		t.Fatalf("chat submit ack decode: %v", err)
	}
	return ack
}

// postRawChatSubmit posts raw bytes and returns status + body.
func postRawChatSubmit(t *testing.T, s *harness.Stack, raw string) (int, []byte) {
	t.Helper()
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // scratch cert
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.HTTPBaseURL()+"/api/v1/chat/submit", bytes.NewReader([]byte(raw)))
	if err != nil {
		t.Fatalf("build submit request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("chat submit: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// waitTerminalViaPoll subscribes, polls turn.terminal for the turn's final
// non-parked event, and unsubscribes (self-contained single-use variant).
func waitTerminalViaPoll(t *testing.T, s *harness.Stack, turnID string, timeout time.Duration) terminalEvent {
	t.Helper()
	sess := openRPCSession(t, s)
	defer sess.Close()
	subRaw, err := sess.call("bus.subscribe", map[string]any{
		"topics": []string{"turn.terminal"},
	})
	if err != nil {
		t.Fatalf("bus.subscribe: %v", err)
	}
	var sub struct {
		SubscriptionID string `json:"subscription_id"`
	}
	if err := json.Unmarshal(subRaw, &sub); err != nil || sub.SubscriptionID == "" {
		t.Fatalf("bus.subscribe ack unparseable: %s", subRaw)
	}
	defer func() {
		_, _ = sess.call("bus.unsubscribe",
			map[string]string{"subscription_id": sub.SubscriptionID})
	}()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		raw, err := sess.call("bus.poll", map[string]string{
			"subscription_id": sub.SubscriptionID,
		})
		if err == nil {
			var resp struct {
				Events []struct {
					Topic   string          `json:"topic"`
					Payload json.RawMessage `json:"payload"`
				} `json:"events"`
			}
			if json.Unmarshal(raw, &resp) == nil {
				for _, ev := range resp.Events {
					if ev.Topic != "turn.terminal" {
						continue
					}
					var payload terminalEvent
					if json.Unmarshal(ev.Payload, &payload) != nil || payload.TurnID != turnID {
						continue
					}
					if payload.Status != "parked" {
						return payload
					}
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("async-turn: no non-parked turn.terminal for %s within %s", turnID, timeout)
	return terminalEvent{}
}

// readDaemonLog returns the FULL daemon log (LogTail is bounded to 4KB).
func readDaemonLog(t *testing.T, s *harness.Stack) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.Work, "daemon.log"))
	if err != nil {
		t.Fatalf("read daemon log: %v", err)
	}
	return string(data)
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}
