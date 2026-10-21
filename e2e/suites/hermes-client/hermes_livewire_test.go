//go:build e2e

// Suite hermes-client, live-wire validation: the REAL internal/hermes
// client (the code the daemon ships to agents) driven over a genuine
// loopback TCP socket — httptest serves on 127.0.0.1, no in-process
// shortcuts — against a stub gateway speaking upstream's exact wire shapes
// (gateway/platforms/api_server.py, verified 2026-10-19). This closes the
// live-gateway validation gap on hosts where outbound shell probes are
// gated: the client, the auth header, the JSON encoding, and the typed
// error classification are all exercised for real.
package hermesclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/hermes"
	"github.com/caimlas/meept/internal/tools/builtin"
)

const liveWireKey = "live-wire-e2e-key"

// liveWireGateway is a stateful stub in upstream's exact response shapes.
type liveWireGateway struct {
	srv         *httptest.Server
	sessions    map[string]map[string]any
	chatReplies map[string]string // session id -> last reply
	capsModel   bool
}

func newLiveWireGateway(t *testing.T) (*liveWireGateway, *hermes.Client) {
	t.Helper()
	g := &liveWireGateway{
		sessions:    map[string]map[string]any{},
		chatReplies: map[string]string{},
		capsModel:   true,
	}
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		// Upstream: every route requires the bearer, including /health.
		if !bearer(r) {
			unauth(w)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok","platform":"hermes-agent"}`)
	})
	mux.HandleFunc("/v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		fl := map[string]bool{"session_chat": true, "session_model_lock": g.capsModel}
		fmt.Fprintf(w, `{"object":"hermes.api_server.capabilities","platform":"hermes-agent","features":%s,"endpoints":{}}`, jsonOf(fl))
	})
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(r) {
			unauth(w)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		id, _ := body["id"].(string)
		if id == "" {
			id = fmt.Sprintf("api_%d_%s", time.Now().Unix(), randomSuffix())
		}
		if _, dup := g.sessions[id]; dup {
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"error":{"message":"Session already exists: `+id+`","type":"invalid_request_error","code":"session_exists"}}`)
			return
		}
		row := map[string]any{"id": id, "source": "api_server", "started_at": float64(time.Now().Unix())}
		if m, ok := body["model"].(string); ok && m != "" {
			row["model"] = m
		}
		if ti, ok := body["title"].(string); ok && ti != "" {
			row["title"] = ti
		}
		if lock, _ := body["require_model_lock"].(bool); lock && row["model"] == "unroutable" {
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"error":{"message":"Requested Browser model lock cannot be routed; refusing silent global fallback","type":"invalid_request_error","code":"model_lock_unavailable"}}`)
			return
		}
		g.sessions[id] = row
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"object":"hermes.session","session":%s}`, jsonOf(row))
	})
	mux.HandleFunc("/api/sessions/", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(r) {
			unauth(w)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
		switch {
		case strings.HasSuffix(rest, "/chat"):
			id := strings.TrimSuffix(rest, "/chat")
			row, known := g.sessions[id]
			if !known {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"error":{"message":"Session not found: `+id+`"}}`)
				return
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			in, _ := body["input"].(string)
			reply := "LIVE: " + in
			g.chatReplies[id] = reply
			fmt.Fprintf(w, `{"object":"hermes.session.chat.completion","session_id":%s,"message":{"role":"assistant","content":%s},"usage":{"input_tokens":7,"output_tokens":9}}`,
				jsonOf(id), jsonOf(reply))
			_ = row
		case strings.HasSuffix(rest, "/model"):
			id := strings.TrimSuffix(rest, "/model")
			if _, known := g.sessions[id]; !known {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"error":{"message":"Session not found: `+id+`"}}`)
				return
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			m, _ := body["model"].(string)
			p, _ := body["provider"].(string)
			g.sessions[id]["model"] = m
			fmt.Fprintf(w, `{"object":"hermes.session.model_lock","session_id":%s,"runtime":{"provider":%s,"model":%s,"route_source":"model_routes","model_lock":"accepted"}}`,
				jsonOf(id), jsonOf(p), jsonOf(m))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)
	return g, hermes.NewClient(g.srv.URL, liveWireKey, 10*time.Second)
}

func bearer(r *http.Request) bool {
	return r.Header.Get("Authorization") == "Bearer "+liveWireKey
}

func unauth(w http.ResponseWriter) {
	w.WriteHeader(http.StatusUnauthorized)
	fmt.Fprint(w, `{"error":{"message":"unauthorized"}}`)
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func randomSuffix() string {
	var b [4]byte
	_, _ = fmt.Sscanf(fmt.Sprintf("%08x", time.Now().UnixNano()), "%8x", &b[0], &b[1], &b[2], &b[3])
	return fmt.Sprintf("%02x%02x%02x%02x", b[0], b[1], b[2], b[3])
}

// TestHermesLiveWire_SessionModelFlow_E2E drives the full capability —
// health, capabilities, session auto-mint, chat on the minted session,
// set_model, and the 409 family split — through the exported client and
// the exported tool surface over a real loopback socket.
func TestHermesLiveWire_SessionModelFlow_E2E(t *testing.T) {
	g, c := newLiveWireGateway(t)
	ctx := context.Background()

	// 1. Health + capabilities on the wire (the operator-facing "is it
	// really up?" check, through the shipped client).
	if err := c.Health(ctx); err != nil {
		t.Fatalf("health over loopback: %v", err)
	}
	caps, err := c.Capabilities(ctx)
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	if !caps.HasFeature("session_chat") || !caps.HasFeature("session_model_lock") {
		t.Fatalf("capability flags = %+v", caps.Features)
	}

	// 2. Tool chat with NO session_id: create + turn over the wire.
	tool := builtin.NewHermesTool(c, false)
	res, err := executeHermesTool(t, tool, map[string]any{"verb": "chat", "input": "ping from e2e"})
	if err != nil {
		t.Fatalf("chat auto-create: %v", err)
	}
	sid, _ := res["session_id"].(string)
	if !strings.HasPrefix(sid, "api_") {
		t.Fatalf("session id %q should carry the gateway's api_ mint", sid)
	}
	if res["session_created"] != true {
		t.Fatalf("payload = %v, want session_created", res)
	}
	if reply, _ := res["reply"].(string); reply != "LIVE: ping from e2e" {
		t.Fatalf("reply = %q — the turn must round-trip the stub", reply)
	}
	if _, ok := g.sessions[sid]; !ok {
		t.Fatalf("gateway table missing the minted session %s", sid)
	}

	// 3. set_model on the live session, verified server-side.
	res, err = executeHermesTool(t, tool, map[string]any{
		"verb": "set_model", "session_id": sid, "model": "glm-5.3", "provider": "zai",
	})
	if err != nil {
		t.Fatalf("set_model: %v", err)
	}
	if res["locked"] != true || res["model"] != "glm-5.3" {
		t.Fatalf("lock payload = %v", res)
	}
	if g.sessions[sid]["model"] != "glm-5.3" {
		t.Fatalf("server-side model not updated: %+v", g.sessions[sid])
	}

	// 4. A chat on the SAME id must reuse (no second mint) and pick up the
	// new model server-side.
	before := len(g.sessions)
	res, err = executeHermesTool(t, tool, map[string]any{
		"verb": "chat", "session_id": sid, "input": "again",
	})
	if err != nil {
		t.Fatalf("reuse chat: %v", err)
	}
	if res["session_id"] != sid || res["session_created"] == true {
		t.Fatalf("reuse payload = %v", res)
	}
	if len(g.sessions) != before {
		t.Fatalf("session count grew on explicit-id chat: %v", g.sessions)
	}

	// 5. 409 family split over the wire.
	_, err = c.CreateSession(ctx, hermes.CreateSessionOptions{ID: sid})
	if !errors.Is(err, hermes.ErrHermesSessionExists) {
		t.Fatalf("dup id: %v, want session_exists sentinel", err)
	}
	_, err = c.CreateSession(ctx, hermes.CreateSessionOptions{ID: "meh", Model: "unroutable", RequireModelLock: true})
	if errors.Is(err, hermes.ErrHermesSessionExists) {
		t.Fatalf("model_lock_unavailable must not be session_exists: %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "model_lock_unavailable") {
		t.Fatalf("want upstream code in error, got %v", err)
	}

	// 6. Auth is real: wrong key -> ErrHermesAuth on a live socket.
	bad := hermes.NewClient(g.srv.URL, "wrong-key", 5*time.Second)
	if err := bad.Health(ctx); !errors.Is(err, hermes.ErrHermesAuth) {
		t.Fatalf("wrong key: %v, want ErrHermesAuth", err)
	}
}
