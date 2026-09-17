package agent

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// (The injectable clock — fakeClock / newFakeClock — is shared with
// turn_registry_test.go, leaf 01; newFakeClock(start) seeds it.)

// watchdogTestClock is the canonical seed for watchdog tests.
func watchdogTestClock() *fakeClock {
	return newFakeClock(time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC))
}

// captureEmitter collects TurnTerminalEvents concurrently.
type captureEmitter struct {
	mu     sync.Mutex
	events []TurnTerminalEvent
}

func (e *captureEmitter) emit(ev TurnTerminalEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, ev)
}

func (e *captureEmitter) snapshot() []TurnTerminalEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]TurnTerminalEvent, len(e.events))
	copy(out, e.events)
	return out
}

// watchdogTestRegistry builds a TurnRegistry with the fake clock wired in.
func watchdogTestRegistry(clock *fakeClock) *TurnRegistry {
	reg := NewTurnRegistry()
	reg.now = clock.Now
	return reg
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)) }

// ---------------------------------------------------------------------------
// Task 1: reapOnce semantics
// ---------------------------------------------------------------------------

// TestTurnWatchdog_ReapsStaleKeepsFresh verifies one pass: two stale + one
// fresh turn → exactly two emits with the frozen reaped payload, both stale
// turns Completed, the fresh turn untouched in the registry.
func TestTurnWatchdog_ReapsStaleKeepsFresh(t *testing.T) {
	clock := watchdogTestClock()
	reg := watchdogTestRegistry(clock)

	reg.Register("turn-stale-1", "conv-1")
	reg.Register("turn-stale-2", "conv-2")
	reg.Register("turn-fresh", "conv-3")
	reg.AttachTask("turn-stale-1", "task-1")
	reg.AttachTask("turn-stale-2", "task-2")

	// Advance past the stale threshold: all three registered turns now
	// have LastProgressAt == start; a Touch resets only the fresh one.
	clock.Advance(2 * time.Minute)
	reg.Touch("turn-fresh")

	em := &captureEmitter{}
	w := NewTurnWatchdog(reg, em.emit, discardLogger())

	got := w.RunOnce(90 * time.Second)
	if got != 2 {
		t.Fatalf("reaped %d turns, want 2", got)
	}

	events := em.snapshot()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	byTurn := map[string]TurnTerminalEvent{}
	for _, ev := range events {
		byTurn[ev.TurnID] = ev
	}
	for _, id := range []string{"turn-stale-1", "turn-stale-2"} {
		ev, ok := byTurn[id]
		if !ok {
			t.Fatalf("no reaped event for %s", id)
		}
		if ev.Status != "failed" {
			t.Errorf("%s status = %q, want failed", id, ev.Status)
		}
		if ev.HandlerCase != "turn_reaped" {
			t.Errorf("%s handler_case = %q, want turn_reaped", id, ev.HandlerCase)
		}
		wantErr := "turn reaped: no progress for 1m30s"
		if ev.Error != wantErr {
			t.Errorf("%s error = %q, want %q", id, ev.Error, wantErr)
		}
		if ev.Reply != "this turn stopped responding and was marked failed" {
			t.Errorf("%s reply = %q", id, ev.Reply)
		}
		if ev.ConversationID == "" {
			t.Errorf("%s event missing conversation_id", id)
		}
		if ev.TaskID == "" {
			t.Errorf("%s event missing task_id (AttachTask wiring)", id)
		}
	}

	// Stale turns are gone from the registry; the fresh one remains
	// (and a second pass is a no-op — no double-reap).
	if got := w.RunOnce(90 * time.Second); got != 0 {
		t.Errorf("second pass reaped %d turns, want 0 (Complete idempotency)", got)
	}
	if len(em.snapshot()) != 2 {
		t.Errorf("second pass emitted again; want no events")
	}
	if _, ok := registryGet(t, reg, "turn-fresh"); !ok {
		t.Error("fresh turn dropped from registry after pass; want it kept")
	}
	if _, ok := registryGet(t, reg, "turn-stale-1"); ok {
		t.Error("reaped turn still registered; want it Completed")
	}
}

// TestTurnWatchdog_NoProgressErrorTextsUsesInjectedStaleAfter verifies the
// error string reflects the staleAfter actually in force.
func TestTurnWatchdog_NoProgressErrorTextsUsesInjectedStaleAfter(t *testing.T) {
	clock := watchdogTestClock()
	reg := watchdogTestRegistry(clock)
	reg.Register("turn-x", "conv-x")
	clock.Advance(1 * time.Hour)

	em := &captureEmitter{}
	w := NewTurnWatchdog(reg, em.emit, discardLogger())
	w.RunOnce(5 * time.Minute)

	events := em.snapshot()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	want := "turn reaped: no progress for 5m0s"
	if events[0].Error != want {
		t.Errorf("error = %q, want %q", events[0].Error, want)
	}
}

// TestTurnWatchdog_ReapedEventWireContract marshals a reaped event and
// pins it to the frozen turn.terminal contract: every key it carries is a
// contract key (no strays), and the reaped-critical fields are present
// with the exact values. The reaped event legitimately leaves the
// omitempty optional fields (session_id, intent_type, agent_id,
// classified_by, model) unset — unlike the all-fields test in
// handler_terminal_event_test.go, which pins the full 13-key shape.
func TestTurnWatchdog_ReapedEventWireContract(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC))
	reg := watchdogTestRegistry(clock)
	reg.Register("turn-wire", "conv-wire")
	reg.AttachTask("turn-wire", "task-wire")
	clock.Advance(10 * time.Minute)

	em := &captureEmitter{}
	w := NewTurnWatchdog(reg, em.emit, discardLogger())
	w.RunOnce(time.Minute)

	b, err := json.Marshal(em.snapshot()[0])
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	contractKeys := map[string]bool{
		"conversation_id": true, "session_id": true, "turn_id": true,
		"task_id": true, "intent_type": true, "agent_id": true,
		"handler_case": true, "status": true, "reply": true,
		"duration_ms": true, "classified_by": true, "model": true,
		"error": true,
	}
	for key := range got {
		if !contractKeys[key] {
			t.Errorf("reaped payload carries non-contract key %q", key)
		}
	}
	for _, key := range []string{"conversation_id", "turn_id", "task_id", "handler_case", "status", "reply", "error"} {
		if _, ok := got[key]; !ok {
			t.Errorf("reaped payload missing required key %q", key)
		}
	}
	if got["status"] != "failed" || got["handler_case"] != "turn_reaped" {
		t.Errorf("status/handler_case = %v/%v, want failed/turn_reaped", got["status"], got["handler_case"])
	}
}

// ---------------------------------------------------------------------------
// Task 1: lifecycle (Start/Stop)
// ---------------------------------------------------------------------------

// TestTurnWatchdog_StartStop_NoLeak starts the loop with a 5ms interval and
// asserts Stop terminates within a bounded wait — the loop's exit closes a
// channel the test can observe (no goroutine leak).
func TestTurnWatchdog_StartStop_NoLeak(t *testing.T) {
	clock := watchdogTestClock()
	reg := watchdogTestRegistry(clock)
	em := &captureEmitter{}
	w := NewTurnWatchdog(reg, em.emit, discardLogger())

	w.Start(5*time.Millisecond, 50*time.Millisecond)

	// Give the loop a few ticks, then stop.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(em.snapshot()) == 0 {
		time.Sleep(2 * time.Millisecond)
	}

	stopped := make(chan struct{})
	go func() {
		w.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		// clean exit
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return within 2s; watchdog goroutine leaked")
	}

	// Idempotent Stop.
	w.Stop()
}

// TestTurnWatchdog_StartIsIdempotent guards the single-goroutine contract:
// two Starts must not stack loops (a second Start would double-reap races).
func TestTurnWatchdog_StartIsIdempotent(t *testing.T) {
	clock := watchdogTestClock()
	reg := watchdogTestRegistry(clock)
	em := &captureEmitter{}
	w := NewTurnWatchdog(reg, em.emit, discardLogger())

	w.Start(5*time.Millisecond, time.Millisecond)
	w.Start(5*time.Millisecond, time.Millisecond) // no-op
	w.Stop()
}

// TestTurnWatchdog_DisabledNilParts verifies the inert construction paths:
// nil registry or nil emit means Start spawns nothing and RunOnce is a
// no-op (composition-site guard for the disabled-config case).
func TestTurnWatchdog_DisabledNilParts(t *testing.T) {
	clock := watchdogTestClock()
	em := &captureEmitter{}

	nilReg := NewTurnWatchdog(nil, em.emit, discardLogger())
	nilReg.Start(time.Millisecond, time.Millisecond)
	if got := nilReg.RunOnce(time.Millisecond); got != 0 {
		t.Errorf("nil-registry watchdog reaped %d, want 0", got)
	}
	nilReg.Stop()

	nilEmit := NewTurnWatchdog(watchdogTestRegistry(clock), nil, discardLogger())
	nilEmit.Start(time.Millisecond, time.Millisecond)
	if got := nilEmit.RunOnce(time.Millisecond); got != 0 {
		t.Errorf("nil-emit watchdog reaped %d, want 0", got)
	}
	nilEmit.Stop()

	if len(em.snapshot()) != 0 {
		t.Error("disabled watchdogs emitted events")
	}
}

// TestTurnWatchdog_LoopReapsOverTime drives Start with a tiny interval and
// injected clock and proves the LOOP (not just RunOnce) reaps: a turn goes
// stale while the loop is running and is reaped without a test-side
// RunOnce call.
func TestTurnWatchdog_LoopReapsOverTime(t *testing.T) {
	clock := watchdogTestClock()
	reg := watchdogTestRegistry(clock)
	reg.Register("turn-loop", "conv-loop")

	em := &captureEmitter{}
	w := NewTurnWatchdog(reg, em.emit, discardLogger())
	w.Start(5*time.Millisecond, 30*time.Millisecond)
	defer w.Stop()

	// Walk the clock past the threshold; the loop's ticker keeps firing
	// in real time, so a pass within the bounded wait must see the turn
	// as stale.
	clock.Advance(31 * time.Millisecond)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(em.snapshot()) > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	events := em.snapshot()
	if len(events) != 1 {
		t.Fatalf("loop emitted %d events, want 1", len(events))
	}
	if events[0].TurnID != "turn-loop" || events[0].HandlerCase != "turn_reaped" {
		t.Errorf("unexpected event: %+v", events[0])
	}
}

// ---------------------------------------------------------------------------
// Emit panic isolation
// ---------------------------------------------------------------------------

// TestTurnWatchdog_EmitPanicDoesNotKillLoop proves a panicking emit is
// recovered per record and the loop keeps passing (the second stale turn
// still gets Completed, and a later pass still runs).
func TestTurnWatchdog_EmitPanicDoesNotKillLoop(t *testing.T) {
	clock := watchdogTestClock()
	reg := watchdogTestRegistry(clock)
	reg.Register("turn-p1", "conv-p1")
	reg.Register("turn-p2", "conv-p2")
	clock.Advance(time.Hour)

	var calls int
	var mu sync.Mutex
	w := NewTurnWatchdog(reg, func(TurnTerminalEvent) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			panic("emit exploded")
		}
	}, discardLogger())

	got := w.RunOnce(time.Minute)
	if got != 2 {
		t.Fatalf("reaped %d, want 2 (panic must not abort the pass)", got)
	}
	if reg.Stale(time.Minute) != nil && len(reg.Stale(time.Minute)) != 0 {
		t.Error("registry not emptied after pass")
	}

	// The loop itself survives: Start + later stale turn still reaps.
	reg.Register("turn-after", "conv-after")
	clock.Advance(time.Hour)
	w.Start(time.Millisecond, time.Minute)
	defer w.Stop()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(reg.Stale(time.Minute)) == 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if left := reg.Stale(time.Minute); len(left) != 0 {
		t.Errorf("post-panic pass did not reap: %+v", left)
	}
}

// ---------------------------------------------------------------------------
// Task 4: observability log line
// ---------------------------------------------------------------------------

// TestTurnWatchdog_LogsWarnLine captures slog output and asserts the Warn
// "turn reaped" line carries turn_id, conversation_id, and the stale
// duration (leaf-06 task 4).
func TestTurnWatchdog_LogsWarnLine(t *testing.T) {
	clock := watchdogTestClock()
	reg := watchdogTestRegistry(clock)
	reg.Register("turn-log", "conv-log")
	clock.Advance(10 * time.Minute)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	em := &captureEmitter{}
	w := NewTurnWatchdog(reg, em.emit, logger)
	w.RunOnce(time.Minute)

	out := buf.String()
	if !strings.Contains(out, `level=WARN`) || !strings.Contains(out, `msg="turn reaped"`) {
		t.Fatalf("expected WARN 'turn reaped' log line, got: %s", out)
	}
	for _, want := range []string{"turn_id=turn-log", "conversation_id=conv-log", "stale_for=1m0s"} {
		if !strings.Contains(out, want) {
			t.Errorf("log line missing %q: %s", want, out)
		}
	}
}
