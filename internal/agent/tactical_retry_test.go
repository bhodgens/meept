package agent

// 02-routing TEST-GATE T6 (W12; R5/A2-A5; F-ORCH-28; master matrix F6).
//
// Three resilience pins:
//
//  1. TestBlockedStepRetry - ScheduleReadySteps skips semaphore-blocked
//     steps at Debug and NOTHING retries them: reschedule callers are
//     all event-driven, so a wave that blocks with no in-flight steps
//     to complete hangs forever. Contract (02-routing/04 T2): a bounded
//     retry ticker (orchestrator.step_retry_interval, default 15s, 0
//     disables) sweeps ready-but-unscheduled steps. RED today: after
//     the slots free, the blocked step stays ready indefinitely.
//
//  2. TestNoSingletonDirMutation - resolveAgent's fallback MUTATES the
//     shared registry singleton's workingDir when session-loop creation
//     fails (dispatcher.go "race risk" warn). Under concurrent sessions
//     one session's dir applies to another's tools. Contract: a
//     GetOrCreateWired failure never mutates the shared loop; the turn
//     binds its own dir or refuses actionably. RED today: the singleton
//     comes back carrying the session's project path.
//
// The daemon-side dir-binding pin (TestStepJobDirBinding) lives in
// internal/daemon/stepjob_dir_test.go.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
)

// retryGateScheduler builds a tactical scheduler with a tiny global
// semaphore over a real task/step store and a recording queue.
func retryGateScheduler(t *testing.T, jobs, perAgent int) (*TacticalScheduler, *recordingQueue) {
	t.Helper()
	store, err := task.NewStore(filepath.Join(t.TempDir(), "tasks.db"), testLogger())
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("close task store: %v", closeErr)
		}
	})
	q := &recordingQueue{}
	ts := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore:             store.StepStore(),
		TaskStore:             store,
		Queue:                 q,
		Logger:                testLogger(),
		MaxConcurrentJobs:     jobs,
		MaxConcurrentPerAgent: perAgent,
	})
	return ts, q
}

// seedReadyWave creates an executing task with n ready coder steps.
func seedReadyWave(t *testing.T, ts *TacticalScheduler, name string, n int) []*task.TaskStep {
	t.Helper()
	parent := task.NewTask(name, "retry gate wave")
	parent.TotalJobs = n
	parent.SetState(task.StateExecuting)
	if err := ts.taskStore.Create(parent); err != nil {
		t.Fatalf("create task: %v", err)
	}
	steps := make([]*task.TaskStep, 0, n)
	for i := 0; i < n; i++ {
		step := task.NewTaskStep(parent.ID, "wave step", i)
		step.ToolHint = "code"
		step.AgentID = config.AgentIDCoder
		step.State = task.StepReady
		if err := ts.stepStore.Create(step); err != nil {
			t.Fatalf("create step %d: %v", i, err)
		}
		steps = append(steps, step)
	}
	return steps
}

// stepState reloads a step's persisted state.
func stepState(t *testing.T, ts *TacticalScheduler, stepID string) task.StepState {
	t.Helper()
	got, err := ts.stepStore.GetByID(stepID)
	if err != nil || got == nil {
		t.Fatalf("reload step %s: %v", stepID, err)
	}
	return got.State
}

// TestBlockedStepRetry_TickerSchedulesBlockedSteps pins the retry
// contract: a semaphore-full wave leaves steps ready; once the slots
// free (leases released, NO completion event fires), the bounded retry
// ticker must schedule them without any event-driven nudge.
func TestBlockedStepRetry_TickerSchedulesBlockedSteps(t *testing.T) {
	ts, q := retryGateScheduler(t, 1, 1)

	// Fast ticker for the gate: the config knob lands with 02-routing/04
	// (orchestrator.step_retry_interval). Wire it through the optional
	// setter seam; a missing seam is part of the RED state.
	type retryIntervalWired interface {
		SetStepRetryInterval(d time.Duration)
	}
	if w, ok := any(ts).(retryIntervalWired); ok {
		w.SetStepRetryInterval(100 * time.Millisecond)
	}

	steps := seedReadyWave(t, ts, "blocked-wave", 2)

	// First sweep: one step schedules (takes the only slot), one blocks.
	if err := ts.ScheduleReadySteps(context.Background(), steps[0].TaskID); err != nil {
		t.Fatalf("ScheduleReadySteps: %v", err)
	}
	scheduled, blocked := 0, 0
	var blockedID string
	for _, s := range steps {
		switch stepState(t, ts, s.ID) {
		case task.StepScheduled:
			scheduled++
		case task.StepReady:
			blocked++
			blockedID = s.ID
		}
	}
	if scheduled != 1 || blocked != 1 {
		t.Fatalf("wave split = %d scheduled / %d blocked, want 1/1 (semaphore precondition)", scheduled, blocked)
	}

	// Free the slot WITHOUT any completion event: release the held
	// lease directly (the lease-leak / no-event shape F-ORCH-28
	// observed). No OnJobCompleted, no orchestrator.schedule - only
	// the retry ticker can rescue the blocked step now.
	ts.releaseStepSlot(steps[0].ID)
	if got := len(ts.globalSemaphore); got != 0 {
		t.Fatalf("global slots held after release = %d, want 0", got)
	}

	// Wait for the bounded retry sweep. Default interval is 15s; the
	// gate waits past it so the pin holds even without the fast seam.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if stepState(t, ts, blockedID) == task.StepScheduled {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if got := stepState(t, ts, blockedID); got != task.StepScheduled {
		t.Fatalf("blocked step %s state = %q after slots freed + 20s: semaphore-blocked steps are skipped at Debug and NEVER retried (no ticker) - the wave hangs with no in-flight steps to fire a completion event (W12)",
			blockedID, got)
	}
	q.mu.Lock()
	enqueued := len(q.enqList)
	q.mu.Unlock()
	if enqueued < 2 {
		t.Errorf("queue holds %d jobs, want >= 2 (the retried step must actually enqueue)", enqueued)
	}
}

// TestBlockedStepRetry_ProgressSurfacesBlockedState pins the visibility
// half: a sweep that blocks steps must surface the blocked state in
// task.progress (chat-visible), not a silent Debug line.
func TestBlockedStepRetry_ProgressSurfacesBlockedState(t *testing.T) {
	msgBus := bus.New(nil, testLogger())
	t.Cleanup(msgBus.Close)
	progressSub := msgBus.Subscribe("gate-blocked-progress", "task.progress")
	t.Cleanup(func() { msgBus.Unsubscribe(progressSub) })

	ts, _ := retryGateScheduler(t, 1, 1)
	ts.bus = msgBus

	steps := seedReadyWave(t, ts, "blocked-progress", 2)
	if err := ts.ScheduleReadySteps(context.Background(), steps[0].TaskID); err != nil {
		t.Fatalf("ScheduleReadySteps: %v", err)
	}

	var sawBlocked bool
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !sawBlocked {
		select {
		case msg := <-progressSub.Channel:
			if msg == nil {
				continue
			}
			var payload map[string]any
			if json.Unmarshal(msg.Payload, &payload) != nil {
				continue
			}
			if blocked, ok := payload["blocked_by_semaphore"].(float64); ok && blocked > 0 {
				sawBlocked = true
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !sawBlocked {
		t.Error("task.progress never surfaced blocked_by_semaphore > 0: a blocked wave is invisible to the user (W12 requires the blocked state in task.progress)")
	}
}

// TestNoSingletonDirMutation_FailedSessionLoopNeverMutatesShared pins
// the dir-binding contract on the dispatcher fallback: when the
// session-scoped loop cannot be created, resolveAgent must NOT retarget
// the shared registry singleton's workingDir. RED today: the fallback
// calls agent.SetWorkingDir(projectPath) on the singleton ("race risk"
// warn) - session B's tools then run in session A's project.
//
// The GetOrCreateWired failure is forced deterministically: a manager
// pre-seeded with a BROKEN cached loop for the session key whose
// eviction path errors is not constructible from outside, so the gate
// drives the failure through the manager's nil-template contract
// first (precondition), then asserts the dispatcher-level invariant
// that holds on EVERY resolveAgent outcome: the shared singleton's
// workingDir is never retargeted by a session dispatch.
func TestNoSingletonDirMutation_FailedSessionLoopNeverMutatesShared(t *testing.T) {
	logger := testLogger()
	registry := NewAgentRegistry(RegistryConfig{Logger: logger})
	if err := registry.RegisterSpec(&AgentSpec{
		ID: config.AgentIDCoder, Name: "coder", Role: RoleExecutor, Enabled: true,
	}); err != nil {
		t.Fatalf("register coder: %v", err)
	}

	singleton, err := registry.Get(config.AgentIDCoder)
	if err != nil {
		t.Fatalf("registry.Get(coder): %v", err)
	}
	singleton.SetWorkingDir("/daemon/original")

	ss := session.NewMemoryStore(logger)
	t.Cleanup(func() {
		if closeErr := ss.Close(); closeErr != nil {
			t.Errorf("close session store: %v", closeErr)
		}
	})
	sess, err := ss.Create("dir-mutation-gate")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess.ProjectPath = "/session-a/project"

	d := NewDispatcher(DispatcherConfig{Registry: registry, Logger: logger})
	d.SetSessionStore(ss)
	// The failure shape: session store wired but NO loop manager, so the
	// session-scoped creation is unavailable and resolveAgent falls into
	// the "mutating shared singleton workingDir (race risk)" branch.
	// (A wired manager that cannot create the loop lands in the same
	// branch; manager-absent is the deterministic construction.)

	// Drive the REAL resolver for the session. Whether the session loop
	// creates or falls back, the SHARED singleton must keep its own dir.
	resolved := d.resolveAgent(config.AgentIDCoder, sess.ConversationID)
	if resolved == nil {
		t.Fatal("resolveAgent returned nil")
	}

	if got := singleton.GetWorkingDir(); got != "/daemon/original" {
		t.Errorf("shared singleton workingDir mutated to %q by a session dispatch: GetOrCreateWired failure (or any session resolution) must never retarget the shared loop - concurrent sessions cross directories (R5/A2)",
			got)
	}
	if resolved == singleton && resolved.GetWorkingDir() == sess.ProjectPath {
		t.Error("resolveAgent returned the shared singleton carrying the session's project path: the turn must bind a session-scoped loop or refuse actionably, never mutate shared state")
	}
}

// TestNoSingletonDirMutation_ConcurrentSessionsIsolated drives the race
// shape directly: two sessions with different project paths resolve
// concurrently; neither resolution may leave the OTHER session's dir
// on the shared singleton, and each resolved loop must carry its OWN
// session's dir (or refuse).
func TestNoSingletonDirMutation_ConcurrentSessionsIsolated(t *testing.T) {
	logger := testLogger()
	registry := NewAgentRegistry(RegistryConfig{Logger: logger})
	if err := registry.RegisterSpec(&AgentSpec{
		ID: config.AgentIDCoder, Name: "coder", Role: RoleExecutor, Enabled: true,
	}); err != nil {
		t.Fatalf("register coder: %v", err)
	}
	singleton, err := registry.Get(config.AgentIDCoder)
	if err != nil {
		t.Fatalf("registry.Get(coder): %v", err)
	}
	singleton.SetWorkingDir("/daemon/original")

	ss := session.NewMemoryStore(logger)
	t.Cleanup(func() {
		if closeErr := ss.Close(); closeErr != nil {
			t.Errorf("close session store: %v", closeErr)
		}
	})
	sessA, err := ss.Create("dir-race-a")
	if err != nil {
		t.Fatalf("create session A: %v", err)
	}
	sessA.ProjectPath = "/project/a"
	sessB, err := ss.Create("dir-race-b")
	if err != nil {
		t.Fatalf("create session B: %v", err)
	}
	sessB.ProjectPath = "/project/b"

	d := NewDispatcher(DispatcherConfig{Registry: registry, Logger: logger})
	d.SetSessionStore(ss)
	d.SetAgentLoopManager(NewManager(ManagerConfig{SessionStore: ss}))

	type outcome struct {
		convID string
		dir    string
	}
	results := make(chan outcome, 2)
	for _, sess := range []*session.Session{sessA, sessB} {
		go func(s *session.Session) {
			loop := d.resolveAgent(config.AgentIDCoder, s.ConversationID)
			dir := ""
			if loop != nil {
				dir = loop.GetWorkingDir()
			}
			results <- outcome{convID: s.ConversationID, dir: dir}
		}(sess)
	}

	want := map[string]string{
		sessA.ConversationID: sessA.ProjectPath,
		sessB.ConversationID: sessB.ProjectPath,
	}
	for i := 0; i < 2; i++ {
		got := <-results
		if got.dir != want[got.convID] {
			t.Errorf("session %s resolved loop dir = %q, want its OWN project %q (cross-session dir race)",
				got.convID, got.dir, want[got.convID])
		}
	}
	if got := singleton.GetWorkingDir(); got != "/daemon/original" {
		t.Errorf("shared singleton dir = %q after concurrent session dispatches, want /daemon/original untouched", got)
	}
}
