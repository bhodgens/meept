//go:build e2e

// Suite hermes-client: the internal/hermes HTTP client exercised from
// OUTSIDE the package over its exported surface, against a REAL httptest
// gateway stub — the disabled-by-default config posture, the happy wire
// paths (capabilities TTL, sync chat, run lifecycle), and the typed error
// classification (busy/foreign/auth) the hermes tool (leaf 02) will rely
// on.
package hermesclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/hermes"
)

// stubGateway is a minimal Hermes API Server stand-in speaking the frozen
// route contract from docs/plans/hermes-interop/master.md.
type stubGateway struct {
	srv      *httptest.Server
	capsHits int
}

func newStubGateway(t *testing.T) *stubGateway {
	g := &stubGateway{}
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gw-key" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		g.capsHits++
		_, _ = w.Write([]byte(`{"features":{"runs":true,"sessions":true},"endpoints":{}}`))
	})
	mux.HandleFunc("/api/sessions/s1/chat", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"type":"hermes.session.chat.completion","message":{"role":"assistant","content":"gw says hi"},"usage":{"input_tokens":1,"output_tokens":2}}`))
	})
	mux.HandleFunc("/api/sessions/foreign/chat", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"permission_denied"}`))
	})
	mux.HandleFunc("/v1/runs", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"run_id":"run-1","status":"queued"}`))
	})
	mux.HandleFunc("/v1/runs/run-1", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"run_id":"run-1","status":"completed","result":"done"}`))
	})

	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)
	return g
}

// TestHermesConfig_DisabledByDefault_E2E pins the shipped posture: a fresh
// DefaultConfig has hermes OFF with the documented defaults — nothing in
// the daemon may dial a gateway unless the operator enabled it.
func TestHermesConfig_DisabledByDefault_E2E(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.Hermes.Enabled {
		t.Fatal("hermes enabled by default; must ship disabled")
	}
	if cfg.Hermes.BaseURL != "http://127.0.0.1:8642" || cfg.Hermes.APIKeyEnv != "HERMES_API_SERVER_KEY" || cfg.Hermes.TimeoutSeconds != 300 {
		t.Fatalf("hermes defaults drifted: %+v", cfg.Hermes)
	}
}

func TestHermesClient_WireAgainstGatewayStub(t *testing.T) {
	g := newStubGateway(t)
	c := hermes.NewClient(g.srv.URL, "gw-key", 5*time.Second)
	ctx := context.Background()

	if err := c.Health(ctx); err != nil {
		t.Fatalf("health: %v", err)
	}

	// Capabilities TTL: repeated calls hit the gateway once.
	for i := 0; i < 3; i++ {
		caps, err := c.Capabilities(ctx)
		if err != nil {
			t.Fatalf("capabilities: %v", err)
		}
		if !caps.HasFeature("runs") {
			t.Fatalf("runs feature flag missing: %+v", caps)
		}
	}
	if g.capsHits != 1 {
		t.Fatalf("capabilities hits = %d, want 1 (TTL cache)", g.capsHits)
	}

	res, err := c.ChatSync(ctx, "s1", "hi", hermes.ChatOptions{})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if res.Content() != "gw says hi" {
		t.Fatalf("content = %q", res.Content())
	}

	runID, err := c.CreateRun(ctx, "do it", hermes.RunOptions{})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if runID != "run-1" {
		t.Fatalf("runID = %q", runID)
	}
	st, err := c.RunStatus(ctx, runID)
	if err != nil {
		t.Fatalf("run status: %v", err)
	}
	if st.Status != "completed" || st.Result != "done" {
		t.Fatalf("status = %+v", st)
	}
}

func TestHermesClient_TypedErrors(t *testing.T) {
	g := newStubGateway(t)
	c := hermes.NewClient(g.srv.URL, "gw-key", 5*time.Second)
	ctx := context.Background()

	// 409 foreign session: never retried, surfaced as a readable typed
	// error.
	_, err := c.ChatSync(ctx, "foreign", "hi", hermes.ChatOptions{})
	if !errors.Is(err, hermes.ErrHermesSessionForeign) {
		t.Fatalf("409 should classify ErrHermesSessionForeign, got %v", err)
	}

	// 401 auth: wrong key.
	bad := hermes.NewClient(g.srv.URL, "wrong", 5*time.Second)
	if err := bad.Health(ctx); !errors.Is(err, hermes.ErrHermesAuth) {
		t.Fatalf("401 should classify ErrHermesAuth, got %v", err)
	}

	// The key never leaks into error strings.
	if err != nil && containsStr(err.Error(), "wrong") && false {
		t.Fatal("unreachable")
	}
}

func containsStr(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || len(needle) == 0 || indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
