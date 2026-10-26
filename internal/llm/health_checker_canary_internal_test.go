// Gate tests for the llama-hang-watchdog tree (docs/plans/llama-hang-watchdog),
// leaf 00T — written BEFORE the implementation exists. This file must fail to
// compile naming ONLY the new contract symbols (RuntimeConfig.Canary* fields,
// VerdictNow/VerdictHung). Leaf 01 (canary probe) makes the request-shape /
// arming / ledger rows pass; the two failure-counting rows assert through the
// Contract-2 verdict surface ("assert ... the verdict after threshold ticks" —
// 00T brief Task 2), so they go GREEN with leaf 02. Referencing the
// unexported canaryFails counter instead was rejected: it is a leaf-01
// implementation detail outside the contracted symbol set the vet gate allows.
package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// canaryFixture is a fake llama-server: /health answers with a programmable
// status, /v1/chat/completions runs a programmable handler and records every
// request (count + last method/path/body). Mirrors the httptest+mux style of
// health_checker_test.go.
type canaryFixture struct {
	srv *httptest.Server

	mu            sync.Mutex
	healthStatus  int
	completionsFn func(w http.ResponseWriter)
	hits          int
	lastMethod    string
	lastPath      string
	lastBody      []byte
}

func newCanaryFixture(t *testing.T) *canaryFixture {
	t.Helper()
	fx := &canaryFixture{healthStatus: http.StatusOK, completionsFn: canaryRespondOK}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		fx.mu.Lock()
		status := fx.healthStatus
		fx.mu.Unlock()
		w.WriteHeader(status)
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fx.mu.Lock()
		fx.hits++
		fx.lastMethod = r.Method
		fx.lastPath = r.URL.Path
		fx.lastBody = body
		fn := fx.completionsFn
		fx.mu.Unlock()
		// Response handler runs OUTSIDE the fixture lock so a hanging
		// completions handler does not block request recording.
		fn(w)
	})
	fx.srv = httptest.NewServer(mux)
	t.Cleanup(fx.srv.Close)
	return fx
}

// canaryRespondOK answers the Contract-5 success shape: 200 + non-empty choices.
func canaryRespondOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}]}`))
}

func (fx *canaryFixture) setHealth(status int) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	fx.healthStatus = status
}

func (fx *canaryFixture) setCompletions(fn func(w http.ResponseWriter)) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	fx.completionsFn = fn
}

func (fx *canaryFixture) completionHits() int {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return fx.hits
}

func (fx *canaryFixture) lastRequest() (method, path string, body []byte) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return fx.lastMethod, fx.lastPath, append([]byte(nil), fx.lastBody...)
}

// gateWaitFor polls cond every 2ms until it returns true or the deadline
// expires. Returns whether cond became true (caller fails the test with its
// own message). Bounds are generous; correct implementations resolve in
// tens of milliseconds, so per-test wall-clock stays well under 200ms.
func gateWaitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return cond()
}

// newCanaryChecker builds a HealthChecker against the fixture with tiny
// deterministic canary cadence (leaf 01 runs canaries from the same run loop
// via a time accumulator, so HealthInterval==CanaryInterval fires a canary
// every tick). prompt/threshold are per-test.
func newCanaryChecker(t *testing.T, fx *canaryFixture, prompt string, threshold int) *HealthChecker {
	t.Helper()
	modelPath := filepath.Join(t.TempDir(), "canary-model.gguf")
	if err := os.WriteFile(modelPath, []byte("fake model"), 0o644); err != nil {
		t.Fatalf("write temp model file: %v", err)
	}
	cfg := &RuntimeConfig{
		Type:            RuntimeLlamaCpp,
		ModelPath:       modelPath,
		ModelPaths:      map[string]string{"test-model": modelPath},
		HealthEndpoint:  "/health",
		HealthInterval:  5 * time.Millisecond,
		HealthTimeout:   100 * time.Millisecond,
		HealthThreshold: 1,
		CanaryEnabled:   true,
		CanaryInterval:  5 * time.Millisecond,
		CanaryTimeout:   100 * time.Millisecond,
		CanaryThreshold: threshold,
		CanaryPrompt:    prompt,
	}
	return NewHealthChecker(cfg, fx.srv.URL)
}

// TestCanaryRequestShape — gate matrix F2: the canary probe issues
// POST {server-root}/v1/chat/completions with the Contract-5 body: JSON keys
// model/messages/max_tokens/stream present, max_tokens==1, stream==false, and
// messages[0] a user message whose content is the configured canary prompt.
func TestCanaryRequestShape(t *testing.T) {
	fx := newCanaryFixture(t)
	const prompt = "canary-shape-probe"
	hc := newCanaryChecker(t, fx, prompt, 3)

	hc.Start(context.Background())
	defer hc.Stop()

	if !gateWaitFor(2*time.Second, func() bool { return fx.completionHits() >= 1 }) {
		t.Fatalf("no canary request reached /v1/chat/completions (hits=%d)", fx.completionHits())
	}

	method, path, body := fx.lastRequest()
	if method != http.MethodPost {
		t.Errorf("canary method = %q, want POST", method)
	}
	if path != "/v1/chat/completions" {
		t.Errorf("canary path = %q, want /v1/chat/completions", path)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("canary body is not valid JSON (%v): %s", err, body)
	}
	for _, key := range []string{"model", "messages", "max_tokens", "stream"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("canary body missing key %q: %s", key, body)
		}
	}
	if got, ok := payload["max_tokens"].(float64); !ok || got != 1 {
		t.Errorf("max_tokens = %v, want 1", payload["max_tokens"])
	}
	if got, ok := payload["stream"].(bool); !ok || got {
		t.Errorf("stream = %v, want false", payload["stream"])
	}
	msgs, ok := payload["messages"].([]any)
	if !ok || len(msgs) == 0 {
		t.Fatalf("messages = %v, want a non-empty array", payload["messages"])
	}
	first, ok := msgs[0].(map[string]any)
	if !ok {
		t.Fatalf("messages[0] = %v, want an object", msgs[0])
	}
	if got, _ := first["role"].(string); got != "user" {
		t.Errorf("messages[0].role = %v, want %q", first["role"], "user")
	}
	if got, _ := first["content"].(string); got != prompt {
		t.Errorf("messages[0].content = %v, want the configured prompt %q", first["content"], prompt)
	}
}

// TestCanaryEmptyChoicesIsFailure — gate matrix F3: a 200 response with an
// EMPTY choices array is a canary failure (Contract 5), not a success. After
// CanaryThreshold such ticks with /health still answering 200, the checker
// reports VerdictHung.
func TestCanaryEmptyChoicesIsFailure(t *testing.T) {
	fx := newCanaryFixture(t)
	fx.setCompletions(func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[]}`))
	})
	hc := newCanaryChecker(t, fx, "ping", 2)

	hc.Start(context.Background())
	defer hc.Stop()

	if !gateWaitFor(2*time.Second, func() bool { return hc.VerdictNow() == VerdictHung }) {
		t.Fatalf("verdict = %v after %d empty-choices canary ticks, want VerdictHung",
			hc.VerdictNow(), fx.completionHits())
	}
	if fx.completionHits() < 2 {
		t.Errorf("expected at least CanaryThreshold=2 canary requests, got %d", fx.completionHits())
	}
}

// TestCanaryNon200IsFailure — gate matrix F3: any non-200 status is a canary
// failure, and so is a 200 whose body is unparseable (Contract 5). Each case
// drives VerdictHung after threshold consecutive failures.
func TestCanaryNon200IsFailure(t *testing.T) {
	cases := []struct {
		name string
		fn   func(w http.ResponseWriter)
	}{
		{"500 response", func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusInternalServerError)
		}},
		{"200 with unparseable body", func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`this is not json {{{`))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newCanaryFixture(t)
			fx.setCompletions(tc.fn)
			hc := newCanaryChecker(t, fx, "ping", 2)

			hc.Start(context.Background())
			defer hc.Stop()

			if !gateWaitFor(2*time.Second, func() bool { return hc.VerdictNow() == VerdictHung }) {
				t.Fatalf("verdict = %v after %d failing canary ticks, want VerdictHung",
					hc.VerdictNow(), fx.completionHits())
			}
		})
	}
}

// TestCanaryArmsOnlyAfterFirstHealthOK — gate matrix F4: cold-load guard. A
// fresh checker whose /health has never answered 200 issues NO canary probes,
// no matter how many run-loop ticks pass. Once /health flips to 200, canaries
// begin (a completion request arrives).
func TestCanaryArmsOnlyAfterFirstHealthOK(t *testing.T) {
	fx := newCanaryFixture(t)
	fx.setHealth(http.StatusServiceUnavailable) // 503: cold load, not healthy yet
	hc := newCanaryChecker(t, fx, "ping", 3)

	hc.Start(context.Background())
	defer hc.Stop()

	// ~10 health ticks at 5ms interval: the canary must stay disarmed.
	time.Sleep(50 * time.Millisecond)
	if hits := fx.completionHits(); hits != 0 {
		t.Fatalf("canary fired %d requests before the first /health 200 (cold-load guard)", hits)
	}

	fx.setHealth(http.StatusOK)
	if !gateWaitFor(2*time.Second, func() bool { return fx.completionHits() >= 1 }) {
		t.Fatal("no canary request arrived after /health flipped to 200")
	}
}

// TestCanaryBypassesLedger — gate matrix F9: canary probes are infrastructure,
// not turns — they must never write an llm_calls row in metrics.db (Contract
// 5: no Recorder, no Resolver, no slot gate). Behavior-driven pin: run real
// canary ticks against a live metrics store and assert zero rows. The
// structural half of the contract (the canary client is a plain http.Client
// with no Recorder field) is pinned by leaf 01's implementation comment; no
// recorder seam exists on HealthChecker itself, so the row-count assertion is
// the observable gate (per 00T brief Task 2 fallback guidance).
func TestCanaryBypassesLedger(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "metrics.db")
	_, _ = newUsageTestStoreAtPath(t, dbPath) // schema-creating store fixture (client_usage_store_test.go)
	db := openLLMCallsSidecar(t, dbPath)

	fx := newCanaryFixture(t)
	hc := newCanaryChecker(t, fx, "ping", 3)

	hc.Start(context.Background())
	if !gateWaitFor(2*time.Second, func() bool { return fx.completionHits() >= 3 }) {
		hc.Stop()
		t.Fatalf("expected at least 3 canary ticks, got %d", fx.completionHits())
	}
	hc.Stop()

	var count int
	if err := db.Get(&count, `SELECT COUNT(*) FROM llm_calls`); err != nil {
		t.Fatalf("count llm_calls: %v", err)
	}
	if count != 0 {
		t.Errorf("canary ticks wrote %d llm_calls rows, want 0 (canary is infrastructure, not a turn)", count)
	}
}
