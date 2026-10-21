package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// TestReflectionCollector_ConcurrentTurnsCarryOwnScope pins the shared
// collector scope fix: ONE ReflectionCollector is shared by every session
// clone (daemon singleton + ConfigSnapshot propagation). The former
// SetScopeProvider seam re-pointed the shared collector at whichever loop
// finished a turn last — an unsynchronized write read back by the
// background reflection goroutine, so session A's reflection LLM call
// could carry session B's WithTaskScope (plus a plain -race data race).
//
// The fix captures the scope at turn end (loop.scopeOptions(), same
// snapshot block as convSnapshot/convID) and passes it PER CALL via
// ReflectTurnWithScope. This test runs two loops through concurrent
// scoped reflections against a real llm.Client whose ExtraHeaders expose
// the WithTaskScope session id on the wire (the ${session_id} sentinel —
// same observation channel as loop_session_scope_test.go), and asserts
// each reflection request carries the scope of the trajectory it is
// reflecting, never the other session's. Run with -race.
func TestReflectionCollector_ConcurrentTurnsCarryOwnScope(t *testing.T) {
	const headerName = "X-Meept-Reflect-Session"

	type observed struct {
		header       string
		bodySessionA bool
		bodySessionB bool
	}
	var mu sync.Mutex
	var seen []observed

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get(headerName)
		buf := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			_, _ = r.Body.Read(buf)
		}
		body := string(buf)
		mu.Lock()
		seen = append(seen, observed{
			header:       header,
			bodySessionA: strings.Contains(body, "sess-scope-a"),
			bodySessionB: strings.Contains(body, "sess-scope-b"),
		})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		resp, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": `{"proposal": null}`}},
			},
		})
		_, _ = w.Write(resp)
	}))
	t.Cleanup(srv.Close)

	client := llm.NewClient(&llm.ModelConfig{
		BaseURL:      srv.URL,
		ModelID:      "reflect-model",
		APIKey:       "dummy-" + "value",
		ProviderID:   llm.ProviderIDOpenAI,
		ExtraHeaders: map[string]string{headerName: "${session_id}"},
	})

	// ONE shared collector — the production shape.
	loader := newPlannerTemplateLoader()
	loader.fallbacks["reflection/turn.md"] = reflectionTurnFallbackBody
	rc := NewReflectionCollector(
		config.ReflectionCollectorConfig{Enabled: true, AutoQueue: true, TurnConfidenceMin: 0.6},
		client, "", loader, filepath.Join(t.TempDir(), "improvements.md"), nil,
	)

	// Two loops, each with its own accounting session (the scope source).
	loopA := NewAgentLoop("loop-a", t.TempDir())
	loopB := NewAgentLoop("loop-b", t.TempDir())
	for l, sess := range map[*AgentLoop]string{loopA: "sess-scope-a", loopB: "sess-scope-b"} {
		l.mu.Lock()
		l.turnAccountingSessionID = sess
		l.mu.Unlock()
	}

	// Capture the scope per loop at "turn end" (the loop.go snapshot
	// block), then reflect concurrently — the goroutine race window the
	// fix removes.
	var wg sync.WaitGroup
	for _, pair := range []struct {
		l    *AgentLoop
		sess string
	}{{loopA, "sess-scope-a"}, {loopB, "sess-scope-b"}} {
		scope := pair.l.scopeOptions()
		sess := pair.sess
		wg.Add(1)
		go func() {
			defer wg.Done()
			traj := ReflectionTrajectory{
				SessionID: sess,
				AgentID:   "test",
				UserInput: "do the thing for " + sess,
				Outcome:   "success",
			}
			for i := 0; i < 5; i++ {
				if err := rc.ReflectTurnWithScope(context.Background(), traj, scope); err != nil {
					t.Errorf("ReflectTurnWithScope(%s): %v", sess, err)
					return
				}
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 10 {
		t.Fatalf("classifier calls observed = %d, want 10", len(seen))
	}
	for i, o := range seen {
		switch {
		case o.bodySessionA && o.bodySessionB:
			t.Fatalf("call %d: prompt carried both sessions — fixture broken", i)
		case o.bodySessionA && o.header != "sess-scope-a":
			t.Fatalf("call %d: session A reflection carried scope header %q, want sess-scope-a (cross-session misattribution)", i, o.header)
		case o.bodySessionB && o.header != "sess-scope-b":
			t.Fatalf("call %d: session B reflection carried scope header %q, want sess-scope-b (cross-session misattribution)", i, o.header)
		}
	}
}
