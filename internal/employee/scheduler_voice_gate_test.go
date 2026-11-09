package employee

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/llm"
)

// fakeVoiceGateReflector is a Reflector stub whose Chat records each call,
// so the tests can assert whether the GoalLoop Assess phase ran at all.
type fakeVoiceGateReflector struct {
	chats int
}

func (f *fakeVoiceGateReflector) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	f.chats++
	return &llm.Response{Content: `{"candidates": []}`}, nil
}

// newVoiceGateTestManager builds a Manager over a real bot + constitution
// store with one enabled, constitution-holding employee, and registers a
// GoalLoop whose Assess is observable through the fake reflector. It
// mirrors the newGoalTestManager scaffolding (shared SQLite file, mirroring
// the daemon's shared-DB wiring mode).
func newVoiceGateTestManager(t *testing.T) (*Manager, string, *fakeVoiceGateReflector) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "shared.db")

	botStore, err := bot.NewStore(dbPath)
	if err != nil {
		t.Fatalf("bot store: %v", err)
	}
	t.Cleanup(func() { botStore.Close() })

	constStore, err := NewConstitutionStore(dbPath, nil)
	if err != nil {
		t.Fatalf("constitution store: %v", err)
	}
	t.Cleanup(func() { constStore.Close() })

	m := NewManagerWithStores(bot.NewManager(botStore, nil), botStore, constStore, nil, nil, nil)

	ctx := context.Background()
	c := validConstitution()
	c.EscalatesTo = []string{"user"}
	if _, err := m.Hire(ctx, HireRequest{
		ID:           "emp-voice-gate",
		Name:         "voice gate test",
		Prompt:       "test prompt",
		Enabled:      true,
		Triggers:     []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		Constitution: constitutionToMap(t, c),
	}); err != nil {
		t.Fatalf("hire: %v", err)
	}

	reflector := &fakeVoiceGateReflector{}
	loop := NewGoalLoop("emp-voice-gate", &c, nil, nil).WithReflector(reflector)
	m.RegisterGoalLoop("emp-voice-gate", loop)
	return m, "emp-voice-gate", reflector
}

// TestAssessSkipped_WhileVoiceCallLive pins the live-voice-call gate: with
// the predicate reporting a live call for the employee, the scheduled tick
// returns before Trigger (no GoalLoop Assess → zero reflector chats).
func TestAssessSkipped_WhileVoiceCallLive(t *testing.T) {
	m, empID, reflector := newVoiceGateTestManager(t)

	m.SetVoiceCallLiveFunc(func(id string) bool { return id == empID })

	m.runAssessForEmployee(context.Background(), empID)
	if reflector.chats != 0 {
		t.Errorf("assessment ran despite live voice call: %d reflector chats, want 0", reflector.chats)
	}
}

// TestAssessRuns_WhenPredicateNilOrFalse pins the legacy behavior: nil
// predicate (no gate wired) and a false predicate both let the assessment
// run through Trigger into the GoalLoop's Assess phase.
func TestAssessRuns_WhenPredicateNilOrFalse(t *testing.T) {
	t.Run("nil predicate runs as before", func(t *testing.T) {
		m, empID, reflector := newVoiceGateTestManager(t)
		// No SetVoiceCallLiveFunc call at all — the nil-guard path.
		m.runAssessForEmployee(context.Background(), empID)
		if reflector.chats == 0 {
			t.Error("assessment did not run with nil predicate (expected Trigger → Assess → reflector chat)")
		}
	})

	t.Run("predicate false runs as before", func(t *testing.T) {
		m, empID, reflector := newVoiceGateTestManager(t)
		m.SetVoiceCallLiveFunc(func(string) bool { return false })
		m.runAssessForEmployee(context.Background(), empID)
		if reflector.chats == 0 {
			t.Error("assessment did not run with false predicate (expected Trigger → Assess → reflector chat)")
		}
	})
}

// TestSetVoiceCallLiveFunc_NilGuard pins the mandated Set* nil-guard
// pattern (internal/tools/builtin/setters_test.go style): a nil receiver
// or a nil predicate never panics, and setting nil after a real predicate
// restores the no-gate (always-run) behavior.
func TestSetVoiceCallLiveFunc_NilGuard(t *testing.T) {
	t.Run("nil predicate on zero-value manager does not panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SetVoiceCallLiveFunc panicked on nil predicate: %v", r)
			}
		}()
		m := &Manager{}
		m.SetVoiceCallLiveFunc(nil)
	})

	t.Run("nil predicate on real manager does not panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SetVoiceCallLiveFunc panicked on nil predicate: %v", r)
			}
		}()
		m, _, _ := newVoiceGateTestManager(t)
		m.SetVoiceCallLiveFunc(nil)
	})

	t.Run("reset to nil restores always-run", func(t *testing.T) {
		m, empID, reflector := newVoiceGateTestManager(t)

		m.SetVoiceCallLiveFunc(func(string) bool { return true })
		m.runAssessForEmployee(context.Background(), empID)
		if reflector.chats != 0 {
			t.Fatalf("gate: %d reflector chats, want 0", reflector.chats)
		}

		m.SetVoiceCallLiveFunc(nil)
		m.runAssessForEmployee(context.Background(), empID)
		if reflector.chats == 0 {
			t.Error("after nil reset the assessment still skipped (gate not cleared)")
		}
	})
}

// TestVoiceCallLiveFunc_ConcurrentSetRead pins the lock discipline: the
// predicate is swapped concurrently while ticks check it — no data race
// (run under -race) and no panic.
func TestVoiceCallLiveFunc_ConcurrentSetRead(t *testing.T) {
	m, empID, _ := newVoiceGateTestManager(t)

	stop := make(chan struct{})
	go func() {
		defer close(stop)
		for i := 0; i < 200; i++ {
			on := i%2 == 0
			m.SetVoiceCallLiveFunc(func(string) bool { return on })
		}
	}()

	for {
		select {
		case <-stop:
			m.runAssessForEmployee(context.Background(), empID)
			return
		default:
		}
		m.runAssessForEmployee(context.Background(), empID)
		if reflector := m.voiceCallLiveSnapshot(); reflector == nil {
			// fine — either state is legal; keep looping
			_ = reflector
		}
		time.Sleep(time.Millisecond)
	}
}
