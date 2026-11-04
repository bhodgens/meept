package daemon

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/memory"
)

// Leaf 04 T3/T4: the turn-terminal capture writes recall_outcomes.jsonl rows
// (C5 shape, cited per the overlap rule), non-completed turns write none,
// and the shared (ConversationID, Reply) LRU dedupes double terminals into
// ONE row set.
func TestMemoryCaptureRecallOutcomes(t *testing.T) {
	items := []memory.RecallOutcomeRecord{{
		MemoryID: "mem-out-1",
		Query:    "what is the convoy passphrase",
		Tokens:   memory.TokenEstimate("the convoy passphrase is amber Otter 4431"),
		Salient:  memory.SalientTokens("the convoy passphrase is amber Otter 4431"),
	}}

	t.Run("completed citing turn writes cited=true row", func(t *testing.T) {
		b, store, _, _, _, _, sessionID := newCaptureHarness(t)
		outcomes := memory.NewRecallOutcomeLogger(t.TempDir(), nil)
		outcomes.SetLoopSource(func(convID string) []memory.RecallOutcomeRecord {
			if convID == sessionID {
				return items
			}
			return nil
		})
		sub := WireMemoryCapture(b, store, nil, nil, nil, captureLogger(), outcomes)
		defer b.Unsubscribe(sub)

		publishTurnTerminal(t, b, agent.TurnTerminalEvent{
			ConversationID: sessionID,
			SessionID:      sessionID,
			TurnID:         "turn-ro-1",
			Status:         "completed",
			// Restates the injected item verbatim + extra words: >= 8
			// shared occurrences -> cited (the F5 fixture's shape).
			Reply: "the convoy passphrase is amber Otter 4431 — noted for the operation.",
		})

		path := outcomes.Path()
		waitFor(t, 2*time.Second, func() bool {
			rows, err := memory.OutcomeRows(path)
			return err == nil && len(rows) == 1
		}, "1 recall outcome row")
		rows, err := memory.OutcomeRows(path)
		if err != nil {
			t.Fatalf("OutcomeRows: %v", err)
		}
		row := rows[0]
		for _, key := range []struct {
			name string
			ok   bool
		}{{"ts", row.TS != ""}, {"session_id", row.SessionID == sessionID},
			{"memory_id", row.MemoryID == "mem-out-1"}, {"query", row.Query != ""},
			{"injected", row.Injected}, {"cited", row.Cited}, {"tokens", row.Tokens > 0}} {
			if !key.ok {
				t.Errorf("C5 field %s not satisfied: %+v", key.name, row)
			}
		}
		if !row.Cited {
			t.Errorf("citing reply produced cited=false: %+v", row)
		}
	})

	t.Run("unrelated reply writes cited=false", func(t *testing.T) {
		b, store, _, _, _, _, sessionID := newCaptureHarness(t)
		outcomes := memory.NewRecallOutcomeLogger(t.TempDir(), nil)
		outcomes.SetLoopSource(func(string) []memory.RecallOutcomeRecord { return items })
		sub := WireMemoryCapture(b, store, nil, nil, nil, captureLogger(), outcomes)
		defer b.Unsubscribe(sub)

		publishTurnTerminal(t, b, agent.TurnTerminalEvent{
			ConversationID: sessionID,
			SessionID:      sessionID,
			Status:         "completed",
			Reply:          "nothing to do with any stored fact whatsoever.",
		})
		path := outcomes.Path()
		waitFor(t, 2*time.Second, func() bool {
			rows, err := memory.OutcomeRows(path)
			return err == nil && len(rows) == 1
		}, "1 recall outcome row")
		rows, _ := memory.OutcomeRows(path)
		if rows[0].Cited {
			t.Errorf("unrelated reply produced cited=true: %+v", rows[0])
		}
	})

	t.Run("non-completed turns write none", func(t *testing.T) {
		b, store, _, _, _, _, sessionID := newCaptureHarness(t)
		outcomes := memory.NewRecallOutcomeLogger(t.TempDir(), nil)
		outcomes.SetLoopSource(func(string) []memory.RecallOutcomeRecord { return items })
		sub := WireMemoryCapture(b, store, nil, nil, nil, captureLogger(), outcomes)
		defer b.Unsubscribe(sub)

		for _, status := range []string{"failed", "timeout", "parked"} {
			publishTurnTerminal(t, b, agent.TurnTerminalEvent{
				ConversationID: sessionID,
				SessionID:      sessionID,
				Status:         status,
				Reply:          "some reply",
			})
		}
		time.Sleep(200 * time.Millisecond)
		if rows, err := memory.OutcomeRows(outcomes.Path()); err == nil && len(rows) != 0 {
			t.Errorf("non-completed turns wrote %d rows, want 0", len(rows))
		}
	})

	t.Run("double terminal produces ONE row set (shared LRU)", func(t *testing.T) {
		b, store, _, _, _, _, sessionID := newCaptureHarness(t)
		outcomes := memory.NewRecallOutcomeLogger(t.TempDir(), nil)
		// The drain is consuming: a second read returns nothing — the LRU
		// must drop the duplicate event BEFORE the drain runs again.
		drainOnce := true
		outcomes.SetLoopSource(func(string) []memory.RecallOutcomeRecord {
			if drainOnce {
				drainOnce = false
				return items
			}
			return nil
		})
		sub := WireMemoryCapture(b, store, nil, nil, nil, captureLogger(), outcomes)
		defer b.Unsubscribe(sub)

		ev := agent.TurnTerminalEvent{
			ConversationID: sessionID,
			SessionID:      sessionID,
			Status:         "completed",
			Reply:          "the convoy passphrase is amber Otter 4431, confirmed.",
		}
		publishTurnTerminal(t, b, ev)
		publishTurnTerminal(t, b, ev) // relay+direct double emission
		path := outcomes.Path()
		waitFor(t, 2*time.Second, func() bool {
			rows, err := memory.OutcomeRows(path)
			return err == nil && len(rows) >= 1
		}, "recall outcome rows")
		time.Sleep(150 * time.Millisecond)
		rows, err := memory.OutcomeRows(path)
		if err != nil {
			t.Fatalf("OutcomeRows: %v", err)
		}
		if len(rows) != len(items) {
			t.Errorf("double terminal wrote %d rows, want %d (single set)", len(rows), len(items))
		}
	})

	t.Run("nil outcomes logger is fully inert", func(t *testing.T) {
		b, store, recorder, enq, _, _, sessionID := newCaptureHarness(t)
		sub := WireMemoryCapture(b, store, recorder, enq, nil, captureLogger(), nil)
		defer b.Unsubscribe(sub)
		seedUserMessage(t, store, sessionID, "hello")
		publishTurnTerminal(t, b, agent.TurnTerminalEvent{
			ConversationID: sessionID,
			SessionID:      sessionID,
			Status:         "completed",
			Reply:          "reply",
		})
		waitFor(t, 2*time.Second, func() bool { return enq.count() == 1 }, "existing capture still works")
	})
}

// The drain closure shape the daemon wiring uses (agent loop sources) is
// exercised indirectly above via SetLoopSource; pin the memory-side drain
// plumbing too.
func TestRecallOutcomeDrainSeam(t *testing.T) {
	outcomes := memory.NewRecallOutcomeLogger(t.TempDir(), nil)
	if got := outcomes.Drain("conv-x"); got != nil {
		t.Errorf("drain without a loop source returned %v, want nil", got)
	}
	var mu sync.Mutex
	calls := 0
	outcomes.SetLoopSource(func(convID string) []memory.RecallOutcomeRecord {
		mu.Lock()
		calls++
		mu.Unlock()
		if convID == "conv-1" {
			return []memory.RecallOutcomeRecord{{MemoryID: "m"}}
		}
		return nil
	})
	if got := outcomes.Drain("conv-2"); got != nil {
		t.Errorf("drain for unknown conv returned %v, want nil", got)
	}
	got := outcomes.Drain("conv-1")
	if len(got) != 1 || got[0].MemoryID != "m" {
		t.Errorf("drain = %v, want the staged item", got)
	}
	// A nil logger is nil-safe.
	var disabled *memory.RecallOutcomeLogger
	disabled.SetLoopSource(nil)
	_ = disabled.Drain("conv-1")
	_ = filepath.Join // keep filepath import used if helpers change
}
