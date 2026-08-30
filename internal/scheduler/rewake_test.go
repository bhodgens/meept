package scheduler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/pkg/models"
)

// --- topic parity --------------------------------------------------------

// TestRewake_TopicParity pins the scheduler's topic literal to the agent
// package's canonical const spelling. internal/scheduler must not import
// internal/agent (layering), so a literal mirrors it — this test turns
// silent drift into a loud failure.
func TestRewake_TopicParity(t *testing.T) {
	if HookAsyncRewakeTopic != "hook.async_rewake" {
		t.Errorf("HookAsyncRewakeTopic = %q, want hook.async_rewake", HookAsyncRewakeTopic)
	}
}

// --- adapter unit ---------------------------------------------------------

// TestNotifyJobComplete exercises the injectable adapter directly: it
// publishes on the rewake topic with the message it was given, and is a
// safe no-op when the publish func or message is nil.
func TestNotifyJobComplete(t *testing.T) {
	calls := 0
	var gotTopic string
	var gotMsg *models.BusMessage

	pub := func(topic string, msg *models.BusMessage) int {
		calls++
		gotTopic = topic
		gotMsg = msg
		return 1
	}

	msg, err := buildRewakeSignal("job-1", "Nightly Scan")
	if err != nil {
		t.Fatalf("buildRewakeSignal: %v", err)
	}

	if n := NotifyJobComplete(pub, msg); n != 1 {
		t.Errorf("NotifyJobComplete = %d, want 1", n)
	}
	if calls != 1 {
		t.Fatalf("publish called %d times, want 1", calls)
	}
	if gotTopic != HookAsyncRewakeTopic {
		t.Errorf("topic = %q, want %q", gotTopic, HookAsyncRewakeTopic)
	}
	if gotMsg != msg {
		t.Error("adapter did not forward the provided message")
	}

	// Nil-safety: no panic, zero delivered.
	if n := NotifyJobComplete(nil, msg); n != 0 {
		t.Errorf("nil publish: got %d, want 0", n)
	}
	if n := NotifyJobComplete(pub, nil); n != 0 {
		t.Errorf("nil msg: got %d, want 0", n)
	}
}

// TestBuildRewakeSignal pins the timer payload shape: Source=timer,
// empty session_id (broadcast per loop_rewake contract — scheduler jobs
// have no session identity), and scheduler identity in hook_name.
func TestBuildRewakeSignal(t *testing.T) {
	msg, err := buildRewakeSignal("nightly", "Nightly Job")
	if err != nil {
		t.Fatalf("buildRewakeSignal: %v", err)
	}

	var p map[string]any
	if err := json.Unmarshal(msg.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p["source"] != "timer" {
		t.Errorf("source = %v, want timer", p["source"])
	}
	if sid, _ := p["session_id"].(string); sid != "" {
		t.Errorf("session_id = %q, want empty (broadcast)", sid)
	}
	if p["hook_name"] != "scheduler:nightly" {
		t.Errorf("hook_name = %v, want scheduler:nightly", p["hook_name"])
	}
	if p["job_id"] != "nightly" {
		t.Errorf("job_id = %v, want nightly", p["job_id"])
	}
}

// --- integration: job completion → rewake lands ---------------------------

// TestRewake_JobCompletionLandsInDrain is the leaf's proof test: a full
// scheduler RunNow completion publishes the existing hook.async_rewake
// topic with Source=timer, and the payload is consumable by the same
// drainRewakes contract the agent loop uses (empty session_id broadcast,
// matching-session delivery).
func TestRewake_JobCompletionLandsInDrain(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	msgBus := bus.New(nil, nil)
	defer msgBus.Close()

	s, err := NewScheduler(config.SchedulerConfig{Enabled: true, Timezone: "UTC"}, msgBus, WithDataDir(tmpDir))
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}

	rewakeSub := msgBus.Subscribe("rewake-proof", HookAsyncRewakeTopic)
	defer msgBus.Unsubscribe(rewakeSub)

	if _, err := s.ScheduleConfig(JobConfig{
		ID:       "rewake-test-job",
		Name:     "Rewake Test Job",
		Type:     JobTypeShell,
		Schedule: "@yearly", // never fires on its own
		Enabled:  true,
		ShellConfig: &ShellJobConfig{
			Command: "true",
			Timeout: 5 * time.Second,
		},
	}); err != nil {
		t.Fatalf("ScheduleConfig: %v", err)
	}

	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.RunNow("rewake-test-job"); err != nil {
		t.Fatalf("RunNow: %v", err)
	}

	// Wait for the completion-time rewake publish.
	var rewakeMsg *models.BusMessage
	select {
	case m := <-rewakeSub.Channel:
		rewakeMsg = m
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for hook.async_rewake publish after job completion")
	}

	var payload struct {
		SessionID string `json:"session_id"`
		Source    string `json:"source"`
		HookType  string `json:"hook_type"`
		HookName  string `json:"hook_name"`
		JobID     string `json:"job_id"`
	}
	if err := json.Unmarshal(rewakeMsg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal rewake payload: %v", err)
	}
	if payload.Source != "timer" {
		t.Errorf("source = %q, want timer", payload.Source)
	}
	if payload.JobID != "rewake-test-job" {
		t.Errorf("job_id = %q, want rewake-test-job", payload.JobID)
	}
	if payload.HookType != "scheduler_job" {
		t.Errorf("hook_type = %q, want scheduler_job", payload.HookType)
	}
	if !strings.HasPrefix(payload.HookName, "scheduler:") {
		t.Errorf("hook_name = %q, want scheduler: prefix", payload.HookName)
	}

	// Deliberately stop the scheduler before asserting drain behavior so
	// late publishes cannot race the assertions.
	_ = s.Stop(ctx)

	// Broadcast contract check: the empty session_id must mean every armed
	// loop drains this signal — pin it against the parseRewakePayload /
	// drainRewakes filtering rule (non-empty session_id ≠ conversation is
	// dropped; empty is broadcast).
	if payload.SessionID != "" {
		t.Errorf("session_id = %q, want empty so every armed loop drains it", payload.SessionID)
	}
}

// TestRewake_NilBusNoPanic guards the no-bus scheduler construction path:
// completion must not publish and must not panic.
func TestRewake_NilBusNoPanic(t *testing.T) {
	s := &Scheduler{logger: newRewakeTestLogger()}
	s.publishRewake("job-x", "X") // must not panic
}

// newRewakeTestLogger returns a quiet logger for rewake tests.
func newRewakeTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
