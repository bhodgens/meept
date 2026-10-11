package employee

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/runtime"
)

// ---------------------------------------------------------------------------
// Log capture (finding #2: gate outcomes observable at INFO)
// ---------------------------------------------------------------------------

// recordingHandler captures every record at INFO or above, in order, so
// tests can assert on emitted log lines (there is no shared log-capture
// helper in this package).
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo
}

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *recordingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *recordingHandler) find(msg string) *slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.records {
		if h.records[i].Message == msg {
			return &h.records[i]
		}
	}
	return nil
}

func (h *recordingHandler) messages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.records))
	for i, r := range h.records {
		out[i] = r.Message
	}
	return out
}

func (h *recordingHandler) attr(r *slog.Record, key string) (slog.Value, bool) {
	var found slog.Value
	var ok bool
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			found, ok = a.Value, true
			return false
		}
		return true
	})
	return found, ok
}

// goalLookupFunc adapts a func to the GoalLookup interface (test-local).
type goalLookupFunc func(ctx context.Context, employeeID string) (*Goal, error)

func (f goalLookupFunc) ActiveGoal(ctx context.Context, employeeID string) (*Goal, error) {
	return f(ctx, employeeID)
}

// gateLogHarness mirrors gateLoopHarness (gate_loop_test.go) but injects
// the recording logger and a stub goal lookup so the INFO line can carry
// a goal_id.
func gateLogHarness(t *testing.T, rec *recordingHandler, gateOutput string, gateExit int, skip bool) (*GoalLoop, *stubReflector) {
	t.Helper()
	reflector := newStubReflector()
	goal := &Goal{ID: "goal_gate_log", EmployeeID: "emp-test", State: GoalActive}
	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, slog.New(rec)).
		WithReflector(reflector).
		WithGoalLookup(goalLookupFunc(func(_ context.Context, _ string) (*Goal, error) {
			return goal, nil
		})).
		WithGateBackend(&stubBackend{results: []*runtime.CommandResult{
			{Output: "", ExitCode: 0},
			{Output: "head\n", ExitCode: 0},
			{Output: gateOutput, ExitCode: gateExit},
			{Output: "", ExitCode: 0}, // repeat hash cmds for later rounds
			{Output: "head\n", ExitCode: 0},
			{Output: gateOutput, ExitCode: gateExit},
		}}).
		WithGateWorkdir("/ws")
	cfg := &GateConfig{
		Command:           "make check",
		TimeoutSeconds:    30,
		SkipWhenUnchanged: skip,
	}
	loop.SetGateConfig(cfg)
	return loop, reflector
}

func gateLogReflect(t *testing.T, loop *GoalLoop, reflector *stubReflector) {
	t.Helper()
	reflector.queueResponse(`{"health":"healthy","reasoning":"done"}`)
	result := &bot.BotExecutionResult{Success: true, Output: "done"}
	if _, err := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result); err != nil {
		t.Fatalf("Reflect error: %v", err)
	}
}

// TestReflect_GatePass_LogsInfoWithDetails pins finding #2: a PASSING gate
// must leave an INFO-level trace with the goal id, gate command, and
// duration — the audit record only stores a hash, so this log line is the
// operator's window into gate outcomes.
func TestReflect_GatePass_LogsInfoWithDetails(t *testing.T) {
	rec := &recordingHandler{}
	loop, reflector := gateLogHarness(t, rec, "all green\n", 0, false)

	gateLogReflect(t, loop, reflector)

	r := rec.find("quality gate passed")
	if r == nil {
		t.Fatalf("no INFO log %q after passing gate; records = %v", "quality gate passed", rec.messages())
	}
	if r.Level != slog.LevelInfo {
		t.Errorf("level = %v, want INFO", r.Level)
	}
	if v, ok := rec.attr(r, "goal_id"); !ok || v.String() != "goal_gate_log" {
		t.Errorf("goal_id attr = %v (ok=%v), want goal_gate_log", v, ok)
	}
	if v, ok := rec.attr(r, "gate_command"); !ok || v.String() != "make check" {
		t.Errorf("gate_command attr = %v (ok=%v), want make check", v, ok)
	}
	if v, ok := rec.attr(r, "duration_ms"); !ok || v.Kind() != slog.KindInt64 || v.Int64() < 0 {
		t.Errorf("duration_ms attr = %v (ok=%v), want non-negative int", v, ok)
	}
}

// TestReflect_GateFail_LogsInfoWithoutRawOutput pins the failure side of
// finding #2: a failing gate logs at INFO (outcome + duration) but the
// raw output stays out of the INFO record (it may contain code; it lives
// in the feedback block and the audit hash).
func TestReflect_GateFail_LogsInfoWithoutRawOutput(t *testing.T) {
	rec := &recordingHandler{}
	loop, reflector := gateLogHarness(t, rec, "FAIL tests broke\n", 1, false)

	gateLogReflect(t, loop, reflector)

	r := rec.find("quality gate failed")
	if r == nil {
		t.Fatalf("no INFO log %q after failing gate; records = %v", "quality gate failed", rec.messages())
	}
	if r.Level != slog.LevelInfo {
		t.Errorf("level = %v, want INFO", r.Level)
	}
	if v, ok := rec.attr(r, "goal_id"); !ok || v.String() != "goal_gate_log" {
		t.Errorf("goal_id attr = %v (ok=%v), want goal_gate_log", v, ok)
	}
	if v, ok := rec.attr(r, "gate_command"); !ok || v.String() != "make check" {
		t.Errorf("gate_command attr = %v (ok=%v), want make check", v, ok)
	}
	// Raw output must never ride along on the INFO record.
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "output" || strings.Contains(a.Value.String(), "FAIL tests broke") {
			t.Errorf("INFO record leaks gate output via attr %q = %q", a.Key, a.Value.String())
		}
		return true
	})
}

// TestReflect_GateSkip_LogsInfo pins the skip side: a skipped re-run
// (SkipWhenUnchanged) logs at INFO with the skip reason so the operator
// can tell a skip from a silent round.
func TestReflect_GateSkip_LogsInfo(t *testing.T) {
	rec := &recordingHandler{}
	loop, reflector := gateLogHarness(t, rec, "FAIL broke\n", 1, true)

	// Round 1: the gate runs and fails (seeds the skip memory).
	gateLogReflect(t, loop, reflector)

	// Round 2: unchanged workspace → gate is skipped.
	gateLogReflect(t, loop, reflector)

	r := rec.find("quality gate skipped")
	if r == nil {
		t.Fatalf("no INFO log %q after skipped gate; records = %v", "quality gate skipped", rec.messages())
	}
	if r.Level != slog.LevelInfo {
		t.Errorf("level = %v, want INFO", r.Level)
	}
	if v, ok := rec.attr(r, "goal_id"); !ok || v.String() != "goal_gate_log" {
		t.Errorf("goal_id attr = %v (ok=%v), want goal_gate_log", v, ok)
	}
	if v, ok := rec.attr(r, "reason"); !ok || v.String() == "" {
		t.Errorf("reason attr missing/empty on skip record")
	}
}
