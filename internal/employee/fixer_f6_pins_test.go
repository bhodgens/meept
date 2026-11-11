package employee

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/llm"
)

// ---------------------------------------------------------------------------
// C34: canonical tool-name compare at enforcement.
// ---------------------------------------------------------------------------

// TestPreExecCheck_MisCasedForbiddenDenies pins the C34 fix: a mis-cased
// "Shell" in tools_forbidden must DENY the "shell" tool (it silently failed
// open before the canonicalToolName compare).
func TestPreExecCheck_MisCasedForbiddenDenies(t *testing.T) {
	c := &Constitution{}
	c.Constraints.ToolsForbidden = []string{"Shell"} // mis-cased on purpose
	p := NewPreExecChecker("emp_c34f", c)
	dec := p.Check("test", "shell", map[string]string{})
	if dec.Allowed {
		t.Fatal("mis-cased forbidden entry must deny; got allow (fail-open regression)")
	}
	if !strings.Contains(dec.Reason, "forbidden") {
		t.Fatalf("deny reason should name tools_forbidden, got %q", dec.Reason)
	}
}

// TestPreExecCheck_MisCasedAllowedAllows pins the allow-side of C34: a
// mis-cased "FILE_READ" in tools_allowed must still ADMIT file_read.
func TestPreExecCheck_MisCasedAllowedAllows(t *testing.T) {
	c := &Constitution{}
	c.Constraints.ToolsAllowed = []string{"FILE_READ"} // mis-cased on purpose
	p := NewPreExecChecker("emp_c34a", c)
	dec := p.Check("test", "file_read", map[string]string{})
	if !dec.Allowed {
		t.Fatalf("mis-cased allowed entry must admit the tool; got deny: %s", dec.Reason)
	}
	// And a tool not in the list stays denied (fail-closed preserved).
	dec = p.Check("test", "shell", map[string]string{})
	if dec.Allowed {
		t.Fatal("tool outside tools_allowed must stay denied")
	}
}

// ---------------------------------------------------------------------------
// C25: gate-parked quiescence — the GateParker predicate the daemon wiring
// consults.
// ---------------------------------------------------------------------------

// gateParkPersistenceStub is a minimal ParkPersistence so the gate test
// parker indexes persistence keys (the daemon wiring always sets a store;
// a memory-only parker leaves parkKeys empty and drop-by-key is a no-op).
type gateParkPersistenceStub struct {
	mu   sync.Mutex
	rows map[string]agent.ParkedTurnRecord
}

func (f *gateParkPersistenceStub) Save(_ context.Context, kind agent.ParkKind, key string, rec agent.ParkedTurnRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rows == nil {
		f.rows = make(map[string]agent.ParkedTurnRecord)
	}
	f.rows[string(kind)+"|"+key] = rec
	return nil
}

func (f *gateParkPersistenceStub) Delete(_ context.Context, kind agent.ParkKind, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, string(kind)+"|"+key)
	return nil
}

func (f *gateParkPersistenceStub) Load(_ context.Context, _ agent.ParkKind, _ time.Time) ([]agent.ParkedTurnRecord, []string, error) {
	return nil, nil, nil
}

// TestGateParker_HasParkedGateForID pins the C25 predicate primitive: a
// parked goal gate is visible by id across both namespace kinds, and the
// resume/removal clears it.
func TestGateParker_HasParkedGateForID(t *testing.T) {
	turns := agent.NewGateTurnParker(slog.New(slog.DiscardHandler), func(context.Context, agent.ParkedTurnRecord) {})
	turns.SetParkPersistence(&gateParkPersistenceStub{}, agent.ParkKindGate)
	parker := agent.NewGateParker(slog.New(slog.DiscardHandler), turns)
	if parker.HasParkedGateForID("goal_c25") {
		t.Fatal("nothing parked yet; probe must report false")
	}
	if !parker.ParkGate(agent.GateParkData{
		GateKind:    "goal",
		GateID:      "goal_c25",
		TurnReentry: map[string]any{"employee_id": "emp_c25"},
	}) {
		t.Fatal("ParkGate refused a valid payload")
	}
	if !parker.HasParkedGateForID("goal_c25") {
		t.Fatal("parked goal gate must be visible to HasParkedGateForID (C25 regression: driver would re-run rounds during a pending approval)")
	}
	// Nil/empty args fail open.
	if parker.HasParkedGateForID("") {
		t.Fatal("empty id must report false")
	}
	var nilParker *agent.GateParker
	if nilParker.HasParkedGateForID("goal_c25") {
		t.Fatal("nil parker must report false (fail-open)")
	}
	// Release clears the probe.
	if _, err := parker.ReleaseByKey("goal_c25", "rejected"); err != nil {
		t.Fatalf("release: %v", err)
	}
	if parker.HasParkedGateForID("goal_c25") {
		t.Fatal("released gate must no longer be visible")
	}
}

// ---------------------------------------------------------------------------
// C27: errored-round disarm keys the GOAL id.
// ---------------------------------------------------------------------------

// TestGoalRoundDriver_ErroredRoundDisarmsGoal pins C27: after a deferred
// round re-enters Trigger and fails, the driver's per-goal override must be
// disarmed (isArmed false) — keyed by goal ID, not employee ID.
func TestGoalRoundDriver_ErroredRoundDisarmsGoal(t *testing.T) {
	ctx := context.Background()
	mgr := newDriverAutoTestManager(t, "emp_c27")
	created, err := mgr.CreateGoal(ctx, GoalSpec{
		EmployeeID: "emp_c27", Title: "c27", Mandate: "m", Source: "user", MaxRounds: 5,
	})
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}
	harness := newGoalRoundDriverAutoTestHarness()
	d := NewGoalRoundDriver(mgr, slog.New(slog.DiscardHandler)).
		WithDeferredScheduler(harness)

	if outcome, err := d.OnRoundSettled(ctx, "emp_c27"); err != nil || outcome != GoalRoundAdvanced {
		t.Fatalf("settle = %v, %v; want advanced", outcome, err)
	}

	// Make the deferred round fail: pause the employee so Manager.Trigger
	// returns an error path without a live bot backend need.
	if err := mgr.PauseWithReason(ctx, "emp_c27", "forced failure", "test"); err != nil {
		t.Logf("pause not available (%v); Trigger will fail on bot lookup instead", err)
	}

	harness.fireAll()

	goal, err := mgr.GetGoal(ctx, created.ID)
	if err != nil {
		t.Fatalf("get goal: %v", err)
	}
	if d.isArmed(goal) {
		t.Fatal("errored round must disarm the goal (isArmed=true after failure — C27 regression)")
	}
	// The disarm must live under the GOAL id key.
	d.mu.Lock()
	override, ok := d.armed[created.ID]
	d.mu.Unlock()
	if !ok || override == nil || *override {
		t.Fatalf("d.armed[%s] must exist and be false (C27 mis-key regression)", created.ID)
	}
}

// TestGoalRoundDriver_AutonomousFlagDuringDeferredTurn pins C31: the
// goal-tools autonomous flag must be raised inside the DEFERRED round's
// execution window (when the harness fires the job), not during
// OnRoundSettled — which runs before the round executes.
func TestGoalRoundDriver_AutonomousFlagDuringDeferredTurn(t *testing.T) {
	ctx := context.Background()
	mgr := newDriverAutoTestManager(t, "emp_c31")
	if _, err := mgr.CreateGoal(ctx, GoalSpec{
		EmployeeID: "emp_c31", Title: "c31", Mandate: "m", Source: "user", MaxRounds: 3,
	}); err != nil {
		t.Fatalf("create goal: %v", err)
	}

	var flagRaisedInJob atomic.Bool
	harness := newGoalRoundDriverAutoTestHarness()
	d := NewGoalRoundDriver(mgr, slog.New(slog.DiscardHandler)).
		WithDeferredScheduler(harness).
		WithAutonomousRoundSetter(func(v bool) {
			if v {
				flagRaisedInJob.Store(true)
			}
		})

	if outcome, err := d.OnRoundSettled(ctx, "emp_c31"); err != nil || outcome != GoalRoundAdvanced {
		t.Fatalf("settle = %v, %v; want advanced", outcome, err)
	}

	// The reservation phase must NOT have raised the flag (C31: the old
	// set/defer-clear pair ran here, before the round executed).
	if flagRaisedInJob.Load() {
		t.Fatal("autonomous flag raised during OnRoundSettled (C31 regression: flag belongs to the deferred execution window)")
	}

	// Fire the deferred job: the setter must observe the raise inside
	// the execution window.
	harness.fireAll()
	if !flagRaisedInJob.Load() {
		t.Fatal("autonomous flag never raised during the deferred round's execution window (C31 regression)")
	}
}

// ---------------------------------------------------------------------------
// C28/C29: revision CAS on UpdateWithTransition.
// ---------------------------------------------------------------------------

func newCASGoalStore(t *testing.T) *GoalStore {
	t.Helper()
	// newDriverTestManager seeds the employee FK anchor and returns a
	// Manager carrying the goal store it opened.
	mgr := newDriverTestManager(t, "emp_c29")
	if mgr.goalStore == nil {
		t.Fatal("test manager carries no goal store")
	}
	return mgr.goalStore
}

// TestUpdateWithTransition_RevisionConflictRejectsStaleWrite pins C29: a
// write from a copy loaded before another writer's commit must fail with
// ErrGoalRevisionConflict, and the interleaved human edit must survive.
func TestUpdateWithTransition_RevisionConflictRejectsStaleWrite(t *testing.T) {
	ctx := context.Background()
	store := newCASGoalStore(t)

	if err := store.Create(ctx, &Goal{
		ID: "goal_c29", EmployeeID: "emp_c29", Title: "t", Mandate: "original",
		State: GoalActive, Source: SourceUser,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Stale driver copy: a fresh store load, mutated in memory but not
	// persisted yet (a struct copy would clone the Goal's mutex — vet).
	stale, err := store.Get(ctx, "goal_c29")
	if err != nil {
		t.Fatalf("stale load: %v", err)
	}
	stale.Mandate = "driver overwrote this"

	// Concurrent human edit lands first (another fresh load, mutated,
	// persisted).
	human, err := store.Get(ctx, "goal_c29")
	if err != nil {
		t.Fatalf("human load: %v", err)
	}
	human.Mandate = "human mandate edit"
	if err := store.Update(ctx, human); err != nil {
		t.Fatalf("human update: %v", err)
	}

	// The stale write must now fail with a conflict, not a lost update.
	err = store.Update(ctx, stale)
	if err == nil {
		t.Fatal("stale write must fail (lost-update regression)")
	}
	if !errors.Is(err, ErrGoalRevisionConflict) {
		t.Fatalf("stale write error = %v; want ErrGoalRevisionConflict", err)
	}

	// The human edit survives.
	cur, err := store.Get(ctx, "goal_c29")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if cur.Mandate != "human mandate edit" {
		t.Fatalf("human edit destroyed by stale write: mandate=%q", cur.Mandate)
	}

	// A fresh copy CAN still write (no deadlock on the CAS).
	fresh, err := store.Get(ctx, "goal_c29")
	if err != nil {
		t.Fatalf("fresh load: %v", err)
	}
	fresh.Mandate = "fresh driver write"
	if err := store.Update(ctx, fresh); err != nil {
		t.Fatalf("fresh write after conflict: %v", err)
	}
}

// TestUpdateWithTransition_BlockedGoalSurvivesStaleDriverWrite pins C28: a
// human block lands between the driver's load and its write -> the goal
// stays blocked and the stale driver write (which would legalize
// blocked->active via allowHumanBypass) fails with the CAS conflict.
func TestUpdateWithTransition_BlockedGoalSurvivesStaleDriverWrite(t *testing.T) {
	ctx := context.Background()
	store := newCASGoalStore(t)

	if err := store.Create(ctx, &Goal{
		ID: "goal_c28", EmployeeID: "emp_c29", Title: "t", Mandate: "m",
		State: GoalActive, Source: SourceUser,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Stale driver copy still believes the goal is active (fresh load,
	// never persisted — no struct copy: Goal carries a mutex).
	stale, err := store.Get(ctx, "goal_c28")
	if err != nil {
		t.Fatalf("stale load: %v", err)
	}

	// Human blocks the goal.
	blocked, err := store.Get(ctx, "goal_c28")
	if err != nil {
		t.Fatalf("block load: %v", err)
	}
	blocked.State = GoalBlocked
	blocked.BlockedReason = &BlockedReason{Code: "human", Message: "operator stop"}
	if err := store.Update(ctx, blocked); err != nil {
		t.Fatalf("block: %v", err)
	}

	// Stale driver write rides the bypass path (the ONLY bypass caller
	// shape, Manager.UpdateGoal) — the CAS must refuse it.
	err = store.UpdateWithTransition(ctx, stale, true)
	if !errors.Is(err, ErrGoalRevisionConflict) {
		t.Fatalf("stale bypass write error = %v; want ErrGoalRevisionConflict", err)
	}

	cur, err := store.Get(ctx, "goal_c28")
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if cur.State != GoalBlocked {
		t.Fatalf("human-blocked goal resurrected to %v (C28 regression)", cur.State)
	}
}

// ---------------------------------------------------------------------------
// C30: resume serialization under the invoke mutex.
// ---------------------------------------------------------------------------

// TestResumeGoalGateParked_SerializesWithInvokeMutex pins C30 at the manager
// seam: while a deferred round holds the invoke mutex (simulated by taking
// it first), the resume drain must BLOCK until the round settles — the two
// critical sections serialize, so the verdict can only apply once.
func TestResumeGoalGateParked_SerializesWithInvokeMutex(t *testing.T) {
	mgr := newDriverAutoTestManager(t, "emp_c30")

	entered := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	drainCalls := 0
	mgr.SetGoalGateResumeFn(func(ctx context.Context, goalID string) bool {
		mu.Lock()
		drainCalls++
		mu.Unlock()
		close(entered)
		<-release
		return true
	})

	// Hold the invoke mutex the way a deferred round does (triggerViaGoalLoop
	// holds it across Decide).
	invokeMu := mgr.acquireInvokeMutex("emp_c30")
	invokeMu.Lock()
	done := make(chan bool, 1)
	go func() {
		resumed, err := mgr.ResumeGoalGateParked(context.Background(), "emp_c30", "goal_x")
		if err != nil {
			t.Errorf("resume: %v", err)
		}
		done <- resumed
	}()

	// The resume must NOT run its drain while the round holds the mutex.
	select {
	case <-entered:
		release <- struct{}{}
		t.Fatal("resume drained while an invoke-mutex round was in flight (C30 regression: no serialization)")
	case <-time.After(200 * time.Millisecond):
		// Still blocked: correct serialization.
	}

	invokeMu.Unlock()
	<-entered
	release <- struct{}{}
	if resumed := <-done; !resumed {
		t.Fatal("resume should report resumed=true after the round settles")
	}
	mu.Lock()
	defer mu.Unlock()
	if drainCalls != 1 {
		t.Fatalf("drain ran %d times; want exactly 1 (single verdict)", drainCalls)
	}
}

// ---------------------------------------------------------------------------
// C26: store-backed budget checker.
// ---------------------------------------------------------------------------

// TestBudgetChecker_LimitsExcessInvocationsRefused pins the enforcement
// half: with MaxInvocationsPerDay set, recorded rounds accumulate and the
// pre-exec checker refuses the excess invocation.
func TestBudgetChecker_LimitsExcessInvocationsRefused(t *testing.T) {
	botStore := newDriverAutoTestManager(t, "emp_budget").botStore
	if botStore == nil {
		t.Skip("manager helper exposes no bot store on this build")
	}
	checker := NewBotStoreBudgetChecker(botStore, slog.New(slog.DiscardHandler))
	if checker == nil {
		t.Fatal("checker must construct over a live store")
	}

	// Seed a state row (RecordGoalRound upserts the existing row only).
	ctx := context.Background()
	if err := botStore.UpdateState(ctx, bot.BotState{
		DefinitionID: "emp_budget", TodayDate: time.Now().Format("2006-01-02"),
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	// Record 3 goal rounds against the persisted counters.
	for i := 0; i < 3; i++ {
		checker.RecordGoalRound("emp_budget")
	}
	_, _, invocations := checker.SpentToday("emp_budget")
	if invocations != 3 {
		t.Fatalf("SpentToday invocations = %d; want 3 (recorded rounds must count)", invocations)
	}

	// Enforcement: constitution cap 3 -> the 4th invocation is refused.
	c := &Constitution{}
	c.Constraints.MaxInvocationsPerDay = 3
	p := NewPreExecChecker("emp_budget", c, WithBudgetChecker(checker))
	dec := p.Check("test", "shell", map[string]string{})
	if dec.Allowed {
		t.Fatal("excess invocation must be refused when MaxInvocationsPerDay reached (C26 regression)")
	}
	if !strings.Contains(dec.Reason, "max invocations") {
		t.Fatalf("refusal reason should name the invocation cap, got %q", dec.Reason)
	}
}

// TestBudgetChecker_RestartCounterPersists pins the restart half: a NEW
// checker instance (fresh process semantics, same store) still reports the
// recorded rounds — the counter lives in the store, not process memory.
func TestBudgetChecker_RestartCounterPersists(t *testing.T) {
	botStore := newDriverAutoTestManager(t, "emp_restart2").botStore
	if botStore == nil {
		t.Skip("manager helper exposes no bot store on this build")
	}
	ctx := context.Background()
	if err := botStore.UpdateState(ctx, bot.BotState{
		DefinitionID: "emp_restart2", TodayDate: time.Now().Format("2006-01-02"),
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	first := NewBotStoreBudgetChecker(botStore, slog.New(slog.DiscardHandler))
	first.RecordGoalRound("emp_restart2")
	first.RecordGoalRound("emp_restart2")

	// "Restart": brand-new checker over the same store.
	restarted := NewBotStoreBudgetChecker(botStore, slog.New(slog.DiscardHandler))
	_, _, invocations := restarted.SpentToday("emp_restart2")
	if invocations < 2 {
		t.Fatalf("restarted checker sees %d invocations; want >= 2 (persisted counter lost — C26 regression)", invocations)
	}
}

// ---------------------------------------------------------------------------
// C20: reflector-bound prompt masking.
// ---------------------------------------------------------------------------

// capturingReflector records the messages each Chat call receives.
type capturingReflector struct {
	mu   sync.Mutex
	seen [][]llm.ChatMessage
}

func (r *capturingReflector) Chat(_ context.Context, messages []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	r.mu.Lock()
	r.seen = append(r.seen, append([]llm.ChatMessage(nil), messages...))
	r.mu.Unlock()
	return &llm.Response{Content: `{"health":"healthy"}`}, nil
}

func (r *capturingReflector) captured(t *testing.T) string {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	var all strings.Builder
	for _, msgs := range r.seen {
		for _, m := range msgs {
			all.WriteString(m.Content)
		}
	}
	return all.String()
}

// newMaskingTestLoop builds a loop wired with the capturing reflector and
// (optionally) the mask hook, over the shared test-manager helper.
func newMaskingTestLoop(t *testing.T, id string, hook *stubMaskHook) (*GoalLoop, *capturingReflector) {
	t.Helper()
	_ = newDriverAutoTestManager(t, id) // FK anchor + shared DB hygiene
	loop := NewGoalLoop(id, testTier1Constitution(), nil, slog.New(slog.DiscardHandler))
	refl := &capturingReflector{}
	loop = loop.WithReflector(refl)
	if hook != nil {
		loop.SetPiiPromptMaskHook(hook)
		loop.SetSpeakContext("sess_"+id, "conv_"+id) // mask scope
	}
	return loop, refl
}

// stubMaskHook is a PromptMaskHook test double replacing a marker with the
// masked form.
type stubMaskHook struct{ hits int }

func (h *stubMaskHook) MaskOutboundScoped(messages []llm.ChatMessage, sessionID string) []llm.ChatMessage {
	h.hits++
	for i := range messages {
		messages[i].Content = strings.ReplaceAll(messages[i].Content, "SECRETMARKER", "[PII:masked]")
	}
	return messages
}

// TestGoalLoop_AssessPromptMasked pins C20 on the Assess send: with a mask
// hook wired, the trigger-derived marker reaches the reflector masked.
func TestGoalLoop_AssessPromptMasked(t *testing.T) {
	hook := &stubMaskHook{}
	loop, refl := newMaskingTestLoop(t, "emp_c20a", hook)

	trigger := TriggerEvent{Source: "user", Payload: []byte(`{"message":"call SECRETMARKER now"}`)}
	if _, err := loop.Assess(context.Background(), trigger); err != nil {
		t.Fatalf("assess: %v", err)
	}
	got := refl.captured(t)
	if strings.Contains(got, "SECRETMARKER") {
		t.Fatal("reflector-bound Assess prompt carries the raw marker (C20 regression)")
	}
	if !strings.Contains(got, "[PII:masked]") {
		t.Fatal("masked form missing from Assess prompt")
	}
}

// TestGoalLoop_AssessPromptUnmaskedWhenNilHook pins the disabled path: no
// hook -> byte-identical passthrough.
func TestGoalLoop_AssessPromptUnmaskedWhenNilHook(t *testing.T) {
	loop, refl := newMaskingTestLoop(t, "emp_c20b", nil)

	trigger := TriggerEvent{Source: "user", Payload: []byte(`{"message":"plain text"}`)}
	if _, err := loop.Assess(context.Background(), trigger); err != nil {
		t.Fatalf("assess: %v", err)
	}
	if !strings.Contains(refl.captured(t), "plain text") {
		t.Fatal("nil hook must leave the prompt byte-identical")
	}
}

// TestGoalLoop_ReflectPromptMasked pins C20 on the Reflect send.
func TestGoalLoop_ReflectPromptMasked(t *testing.T) {
	hook := &stubMaskHook{}
	loop, refl := newMaskingTestLoop(t, "emp_c20c", hook)

	plan := PlanRef{ID: "plan_x", Prompt: "do it"}
	result := &bot.BotExecutionResult{BotID: "b", Output: "output mentions SECRETMARKER", Success: true}
	if _, err := loop.reflectViaLLM(context.Background(), refl, testTier1Constitution(), plan, result); err != nil {
		t.Fatalf("reflectViaLLM: %v", err)
	}
	got := refl.captured(t)
	if strings.Contains(got, "SECRETMARKER") {
		t.Fatal("reflector-bound Reflect prompt carries the raw marker (C20 regression)")
	}
	if !strings.Contains(got, "[PII:masked]") {
		t.Fatal("masked form missing from Reflect prompt")
	}
}

// capturingChatter satisfies llm.Chatter (Chat + ChatWithProgress).
type capturingChatter struct {
	capturingReflector
}

func (c *capturingChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, messages, opts...)
}

// Config implements the llm.Chatter config seam (nil config is fine — the
// auditors never read it).
func (c *capturingChatter) Config() *llm.ModelConfig { return nil }

// TestPostTurnAuditor_PromptMasked pins C20 on the enforcement audit send.
func TestPostTurnAuditor_PromptMasked(t *testing.T) {
	chatter := &capturingChatter{}
	auditor := NewPostTurnAuditor(chatter, nil, "")
	hook := &stubMaskHook{}
	auditor.SetPromptMaskHook(hook)

	turn := TurnRecord{
		EmployeeID:   "emp_c20d",
		TurnID:       "turn_c20d",
		FinalOutput:  "ran SECRETMARKER",
		Constitution: &Constitution{},
	}
	if _, err := auditor.Audit(context.Background(), turn); err != nil {
		t.Fatalf("audit: %v", err)
	}
	got := chatter.captured(t)
	if strings.Contains(got, "SECRETMARKER") {
		t.Fatal("audit prompt carries the raw marker (C20 regression)")
	}
	if !strings.Contains(got, "[PII:masked]") {
		t.Fatal("masked form missing from audit prompt")
	}
}
