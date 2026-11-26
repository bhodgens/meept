package agent

// Bughunt 2026-11-25 A5-3 pin: RunAgentWithModelRef arms the one-shot
// override on the SHARED per-agent loop. Two concurrent delegate children of
// the SAME agent used to race: child B's arm overwrote child A's before A's
// reasoning cycle consumed it (one child silently ran the OTHER's route —
// the C10 allowlist-gated route enforcement dropped), and either child's
// deferred clear wiped the other's unconsumed arm. The fix serializes the
// arm→run→clear window per agent (routeLocks); this pin proves each
// concurrent caller's OWN ref reaches the wire.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newSlowModelCaptureServer mirrors newModelCaptureServer with a per-request
// delay so concurrent routed turns overlap their arm windows.
func newSlowModelCaptureServer(cap *modelCapture, delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		cap.record(body.Model)
		time.Sleep(delay)
		if body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"role":"assistant","content":"ok"},"finish_reason":null}]}`)
			fmt.Fprintln(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`)
			fmt.Fprintln(w, "data: [DONE]")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`))
	}))
}

func TestRunAgentWithModelRef_ConcurrentChildrenEachSeeOwnRef(t *testing.T) {
	cap := &modelCapture{}
	// Slow responses (~250ms/turn) so the two concurrent calls genuinely
	// overlap their arm→run→clear windows in the UNFIXED shape.
	server := newSlowModelCaptureServer(cap, 250*time.Millisecond)
	defer server.Close()

	reg, _ := newR3RouteRegistry(t, server.URL)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = reg.RunAgentWithModelRef(context.Background(), "probe", "child A", "conv-r3-conc-a", "local/user-b")
	}()
	go func() {
		defer wg.Done()
		_, _ = reg.RunAgentWithModelRef(context.Background(), "probe", "child B", "conv-r3-conc-b", "local/a")
	}()
	wg.Wait()

	// Pre-fix corruption: B's ApplyRequestModel replaces A's before A
	// consumes it, so BOTH turns serve alias-model-a and user-override-b
	// never reaches the wire (A silently lost its gated route). Serialized,
	// every armed ref serves its own child's turn.
	cap.mu.Lock()
	models := append([]string(nil), cap.models...)
	cap.mu.Unlock()
	require.GreaterOrEqual(t, len(models), 2, "both children must have reached the LLM")
	sawB := false
	sawA := false
	for _, m := range models {
		switch m {
		case "user-override-b":
			sawB = true
		case "alias-model-a":
			sawA = true
		}
	}
	if !sawB {
		t.Fatalf("child A's armed route (user-override-b) never served — the shared-override race lost it (A5-3): captured %v", models)
	}
	if !sawA {
		t.Fatalf("child B's armed route (alias-model-a) never served — captured %v", models)
	}
}

func TestRunAgentWithModelRef_RouteGateIsPerAgent(t *testing.T) {
	// Non-vacuity for the gate's scope: the route lock is PER AGENT — one
	// agent's in-flight routed turn must never block another agent's.
	// (Timing-based parallel-run assertions are flaky under -race; the
	// gate itself is the contract, tested white-box.)
	reg := NewAgentRegistry(RegistryConfig{Logger: silentLogger()})
	reg.mu.Lock()
	reg.specs["probe"] = &AgentSpec{ID: "probe", Enabled: true}
	reg.specs["probe2"] = &AgentSpec{ID: "probe2", Enabled: true}
	reg.mu.Unlock()

	releaseA := reg.lockAgentRoute("probe")

	// Same agent: blocked until A releases.
	blocked := make(chan struct{})
	go func() {
		unlock := reg.lockAgentRoute("probe")
		close(blocked)
		unlock()
	}()
	select {
	case <-blocked:
		t.Fatal("same-agent route gate did not serialize (A5-3 gate ineffective)")
	case <-time.After(120 * time.Millisecond):
	}

	// Different agent: acquires immediately while probe is held.
	acquired := make(chan struct{})
	go func() {
		unlock := reg.lockAgentRoute("probe2")
		close(acquired)
		unlock()
	}()
	select {
	case <-acquired:
	case <-time.After(120 * time.Millisecond):
		t.Fatal("cross-agent runs serialized — the route gate must be per-agent")
	}

	// After release, the same-agent waiter proceeds.
	releaseA()
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("same-agent waiter never acquired after release")
	}
}
