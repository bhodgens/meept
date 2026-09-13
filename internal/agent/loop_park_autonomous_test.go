package agent

// Park/resume autonomy pin (bughunt 2026-09-12 wave, F14 follow-up).
//
// F14 moved the AUTONOMOUS marker from the LOOP (a one-way latch) onto the
// TURN's context (daemon stepJobTurnContext). That fixed the leak into later
// interactive turns, but the marker did not survive PARK/RESUME: the park
// record carried no autonomy and every resume path re-entered the loop with
// the parker's own fresh context. A step job that parked on a provider
// throttle therefore resumed INTERACTIVE — file_write/file_edit staged a
// pending change nobody could accept, the step reported success, and the
// artifact never existed. That is e2e run 8, re-created by the fix for the
// leak.
//
// The invariant pinned here: a parked-then-resumed STEP turn still executes
// autonomously, and the flag rides the record's payload (the only channel the
// SQLite park store persists) as well as the record itself.

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// autonomyRecordingChatter serves errs in order, then succeeds with reply,
// recording per call whether the context it was handed carried the
// AUTONOMOUS marker. The LLM call is where the loop forwards the turn's
// context (loop.go chatWithFailoverRaw), so this observes exactly what the
// turn's tools would observe.
type autonomyRecordingChatter struct {
	errs  []error
	reply string
	calls atomic.Int32

	mu         sync.Mutex
	autonomous []bool
}

func (m *autonomyRecordingChatter) record(ctx context.Context) {
	m.mu.Lock()
	m.autonomous = append(m.autonomous, tools.AutonomousFromContext(ctx))
	m.mu.Unlock()
}

func (m *autonomyRecordingChatter) autonomySeen() []bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]bool(nil), m.autonomous...)
}

func (m *autonomyRecordingChatter) callCount() int { return int(m.calls.Load()) }

func (m *autonomyRecordingChatter) Chat(ctx context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	m.record(ctx)
	i := int(m.calls.Add(1)) - 1
	if i < len(m.errs) && m.errs[i] != nil {
		return nil, m.errs[i]
	}
	return &llm.Response{Content: m.reply, FinishReason: "stop"}, nil
}

func (m *autonomyRecordingChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *autonomyRecordingChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "autonomy-recording"}
}

// TestParkedStepTurnResumesAutonomous is the park/resume half of the F14 pin:
// a step job's turn (AUTONOMOUS marker on the turn context) that parks on a
// throttle and resumes later must STILL be autonomous when it re-enters the
// loop. Both carriers are asserted — ParkedTurnRecord.Autonomous (in-process
// resume) and the "autonomous" key inside TurnPayload (the value the SQLite
// park store round-trips, so a resume after a daemon restart keeps it).
func TestParkedStepTurnResumesAutonomous(t *testing.T) {
	throttleErr := &llm.ThrottleBackoffError{ProviderID: "p1", ModelID: "m1"}
	chatter := &autonomyRecordingChatter{errs: []error{throttleErr}, reply: "done"}
	loop, _, parker, clock := newThrottleResumeLoop(t, chatter)

	// The daemon runs step jobs through RunOnce under tools.ContextWithAutonomous
	// (internal/daemon stepJobTurnContext).
	ctx := tools.ContextWithAutonomous(context.Background())
	if _, err := loop.RunOnce(ctx, "write the file", "conv-step-park"); err != nil {
		t.Fatalf("run step turn: %v", err)
	}
	if got := chatter.autonomySeen(); len(got) != 1 || !got[0] {
		t.Fatalf("parked attempt ran with autonomous=%v, want [true] (test harness premise)", got)
	}
	if parker.Pending() != 1 {
		t.Fatalf("pending = %d, want 1 after the throttle park", parker.Pending())
	}

	rec := parkedThrottleRecord(parker)
	if !rec.Autonomous {
		t.Error("parked record did not carry the autonomous flag: the resumed turn will stage changes nothing accepts (e2e run 8)")
	}
	if !strings.Contains(string(rec.TurnPayload), `"autonomous":true`) {
		t.Errorf("park payload = %s, want an \"autonomous\":true key — the park store persists ONLY turn_payload, so a resume after a restart would lose the marker", rec.TurnPayload)
	}

	firstResume, ok := parker.Next(llm.FailureThrottle)
	if !ok {
		t.Fatal("no throttle resume scheduled")
	}
	// The resume callback runs with the PARKER's context (parker.Start's), i.e.
	// a fresh context with no turn marker.
	clock.advance(time.Until(firstResume) + time.Second)
	clock.proceed()
	waitUntil(t, 2*time.Second, func() bool {
		return parker.Pending() == 0 && chatter.callCount() >= 2
	})

	seen := chatter.autonomySeen()
	if len(seen) < 2 {
		t.Fatalf("chatter calls = %d, want the parked + resumed turns", len(seen))
	}
	if !seen[1] {
		t.Fatalf("resumed step turn ran with autonomous=%v, want [true, true]: the resume path dropped the marker and file_write/file_edit would stage a change nothing accepts", seen)
	}
}

// TestResumedStepTurnAutonomousFromPayloadOnly pins the RESTART re-arm shape:
// when the SQLite park store reloads a record, turn_payload is the only
// carrier that survives (the autonomous flag is not a column), so the resume
// must derive autonomy from the payload copy alone. The record comes from the
// REAL store's Load, not a hand-built struct, so this proves the restart path
// rather than assuming it.
func TestResumedStepTurnAutonomousFromPayloadOnly(t *testing.T) {
	chatter := &autonomyRecordingChatter{reply: "done"}
	_, _, parker, clock := newThrottleResumeLoop(t, chatter)

	payload, err := throttleTurnToRecord(throttleParkedTurn{
		Message:        "write the file",
		ConversationID: "conv-rearm",
		ProviderID:     "p1",
		Autonomous:     true,
	})
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	payload.ResumeAt = clock.now.Add(50 * time.Millisecond)

	// Real store round-trip: Save then Load is exactly what persistence +
	// reArm do across a daemon restart.
	ctx := context.Background()
	store := newTestParkStore(t)
	if err := store.Save(ctx, ParkKindChat, "throttle|conv-rearm|0", payload); err != nil {
		t.Fatalf("store save: %v", err)
	}
	recs, keys, err := store.Load(ctx, ParkKindChat, clock.now)
	if err != nil {
		t.Fatalf("store load: %v", err)
	}
	if len(recs) != 1 || len(keys) != 1 {
		t.Fatalf("store load returned %d records / %d keys, want 1/1 (row pruned or lost)", len(recs), len(keys))
	}
	rec := recs[0]
	if rec.Autonomous {
		t.Fatal("test premise broken: the re-armed record must NOT carry the in-process flag")
	}
	if !strings.Contains(string(rec.TurnPayload), `"autonomous":true`) {
		t.Fatalf("store round-trip lost the marker: payload = %s", rec.TurnPayload)
	}
	if !parker.Park(rec) {
		t.Fatal("Park refused the re-armed record")
	}

	clock.advance(time.Second)
	clock.proceed()
	waitUntil(t, 2*time.Second, func() bool {
		return parker.Pending() == 0 && chatter.callCount() >= 1
	})

	seen := chatter.autonomySeen()
	if len(seen) == 0 {
		t.Fatal("resume never reached the loop")
	}
	if !seen[0] {
		t.Fatalf("re-armed step turn resumed with autonomous=%v, want [true]: the payload carrier was ignored, so a park that survives a daemon restart stages again", seen)
	}
}

// TestParkedInteractiveTurnResumesInteractive is the other direction of the
// same contract: an interactive turn that parks must NOT come back
// autonomous — a resume that set the marker would silently bypass the
// pending-change preview/accept workflow for a user who is still there.
func TestParkedInteractiveTurnResumesInteractive(t *testing.T) {
	throttleErr := &llm.ThrottleBackoffError{ProviderID: "p1", ModelID: "m1"}
	chatter := &autonomyRecordingChatter{errs: []error{throttleErr}, reply: "done"}
	loop, _, parker, clock := newThrottleResumeLoop(t, chatter)

	if _, err := loop.RunOnce(context.Background(), "hello", "conv-chat-park"); err != nil {
		t.Fatalf("run interactive turn: %v", err)
	}
	rec := parkedThrottleRecord(parker)
	if rec.Autonomous {
		t.Error("interactive park marked autonomous: the resumed turn would skip the pending-change preview")
	}
	if strings.Contains(string(rec.TurnPayload), `"autonomous"`) {
		t.Errorf("interactive park payload = %s, want no autonomous key", rec.TurnPayload)
	}

	firstResume, ok := parker.Next(llm.FailureThrottle)
	if !ok {
		t.Fatal("no throttle resume scheduled")
	}
	clock.advance(time.Until(firstResume) + time.Second)
	clock.proceed()
	waitUntil(t, 2*time.Second, func() bool {
		return parker.Pending() == 0 && chatter.callCount() >= 2
	})

	seen := chatter.autonomySeen()
	if len(seen) < 2 {
		t.Fatalf("chatter calls = %d, want the parked + resumed turns", len(seen))
	}
	if seen[1] {
		t.Fatalf("interactive resume came back autonomous: %v", seen)
	}
}
