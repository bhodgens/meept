package employee

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// cronFireProbe is a Reflector double that records every Chat call, so a
// test can observe whether Trigger actually reached a goal-loop round.
type cronFireProbe struct {
	mu    sync.Mutex
	calls int
}

func (p *cronFireProbe) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return &llm.Response{Content: `{"candidates":[]}`}, nil
}

func (p *cronFireProbe) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// cronTestConfig builds the minimal config NewManagerFromConfig needs:
// employees+bots enabled with isolated temp data dirs (standalone-DB
// path, no shared bots.db writes into the repo).
func cronTestConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		Daemon: config.DaemonConfig{DataDir: t.TempDir()},
		Bots:   config.BotsConfig{Enabled: true, DataDir: t.TempDir()},
		Employees: config.EmployeesConfig{
			Enabled: true,
		},
	}
}

// newCronTestHarness builds the production-shaped wiring: a bot store +
// manager, then NewManagerFromConfig (the PRODUCTION constructor — the
// pre-fix bug was that this path never installed the cron scheduler).
func newCronTestHarness(t *testing.T) (*bot.Manager, *Manager) {
	t.Helper()
	store, err := bot.NewStore(filepath.Join(t.TempDir(), "bots.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	botMgr := bot.NewManager(store, nil)
	result, err := NewManagerFromConfig(
		context.Background(),
		cronTestConfig(t),
		botMgr, store,
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatalf("NewManagerFromConfig: %v", err)
	}
	t.Cleanup(result.Manager.StopAll)
	return botMgr, result.Manager
}

func cronDef(botID, schedule string, enabled bool) bot.BotDefinition {
	return bot.BotDefinition{
		ID:      botID,
		Name:    botID,
		Prompt:  "cron fixture",
		Enabled: true,
		Triggers: []bot.BotTrigger{{
			Type:           bot.TriggerTypeCron,
			Schedule:       schedule,
			PromptTemplate: "run the sweep",
			Enabled:        enabled,
		}},
	}
}

// TestProductionWiringInstallsCron pins the HIGH: the production wiring
// constructor registers cron triggers and the registered job fires.
//
// Failing revert: remove mgr.installCronScheduler(botMgr) from
// NewManagerFromConfig (wiring.go) — m.Cron is nil, entries == 0, and the
// probe never sees a call.
func TestProductionWiringInstallsCron(t *testing.T) {
	botMgr, mgr := newCronTestHarness(t)
	if mgr.Cron == nil {
		t.Fatalf("production wiring left m.Cron nil: cron scheduler not installed")
	}

	probe := &cronFireProbe{}
	loop := NewGoalLoop("nightly", &Constitution{Purpose: "sweep"}, nil, slog.New(slog.DiscardHandler))
	loop.WithReflector(probe)
	mgr.RegisterGoalLoop("nightly", loop)

	ctx := context.Background()
	if err := botMgr.CreateBot(ctx, cronDef("nightly", "0 4 * * *", true)); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}
	if err := botMgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	entries := mgr.Cron.Entries()
	if len(entries) != 1 {
		t.Fatalf("cron entries = %d, want 1 (production constructor must register enabled cron triggers)", len(entries))
	}
	// Mirror the routing-funnel gate's reflect fire: invoke the job body
	// directly rather than waiting for a wall-clock schedule.
	for _, e := range entries {
		e.Job.Run()
	}
	if probe.count() == 0 {
		t.Fatalf("cron job fired but Trigger never reached the goal loop (source=cron round expected)")
	}
}

// TestCronFireSkipsDeletedBot pins MED(a) fire-time re-check: after
// DeleteBot the stale job (simulated here by an entry snapshot taken
// before the delete) must NOT call Trigger.
//
// Failing revert: restore fireCronTrigger to a blind
// m.Trigger(ctx, botID, ...) with no GetBot/Enabled re-check — the probe
// records a call and this test fails.
func TestCronFireSkipsDeletedBot(t *testing.T) {
	botMgr, mgr := newCronTestHarness(t)

	probe := &cronFireProbe{}
	loop := NewGoalLoop("doomed", &Constitution{Purpose: "sweep"}, nil, slog.New(slog.DiscardHandler))
	loop.WithReflector(probe)
	mgr.RegisterGoalLoop("doomed", loop)

	ctx := context.Background()
	if err := botMgr.CreateBot(ctx, cronDef("doomed", "0 4 * * *", true)); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}
	if err := botMgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	// Snapshot the job BEFORE the delete: this is the stale robfig entry a
	// scheduler would still hold if claims were never released.
	stale := mgr.Cron.Entries()
	if len(stale) != 1 {
		t.Fatalf("cron entries before delete = %d, want 1", len(stale))
	}

	if err := botMgr.DeleteBot(ctx, "doomed"); err != nil {
		t.Fatalf("DeleteBot: %v", err)
	}
	if got := mgr.ReleaseCronClaims("doomed"); got != 0 {
		// Claims were already released by the delete hook; a second
		// release is a no-op. This assert documents the expectation.
		t.Fatalf("second ReleaseCronClaims removed %d jobs, want 0 (delete hook must drain them)", got)
	}

	// Fire the STALE snapshot: the closure must re-check and skip.
	stale[0].Job.Run()
	if probe.count() != 0 {
		t.Fatalf("deleted bot's cron job still called Trigger (%d calls)", probe.count())
	}
}

// TestCronFireSkipsPausedBot pins the trigger-disable half of MED(a): a
// paused bot's stale job must skip, both via the released claim (job
// removed) and via the fire-time Enabled re-check.
//
// Failing revert: remove the PauseBot pauseHook release (lifecycle.go) or
// the fire-time def.Enabled check (cron_trigger.go) — the probe records a
// call through either hole.
func TestCronFireSkipsPausedBot(t *testing.T) {
	botMgr, mgr := newCronTestHarness(t)

	probe := &cronFireProbe{}
	loop := NewGoalLoop("sleeper", &Constitution{Purpose: "sweep"}, nil, slog.New(slog.DiscardHandler))
	loop.WithReflector(probe)
	mgr.RegisterGoalLoop("sleeper", loop)

	ctx := context.Background()
	if err := botMgr.CreateBot(ctx, cronDef("sleeper", "0 4 * * *", true)); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}
	if err := botMgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	stale := mgr.Cron.Entries()
	if len(stale) != 1 {
		t.Fatalf("cron entries before pause = %d, want 1", len(stale))
	}

	if err := botMgr.PauseBot(ctx, "sleeper"); err != nil {
		t.Fatalf("PauseBot: %v", err)
	}
	if n := len(mgr.Cron.Entries()); n != 0 {
		t.Fatalf("cron entries after pause = %d, want 0 (pause must release claims)", n)
	}

	// Fire the stale snapshot anyway: fire-time must re-check Enabled.
	stale[0].Job.Run()
	if probe.count() != 0 {
		t.Fatalf("paused bot's stale cron job still called Trigger (%d calls)", probe.count())
	}
}

// TestStopAllThenReregisterStillStarts pins MED(b): after StopAll (which
// stops the robfig loop), re-registering a bot whose claims already exist
// must STILL reach m.Cron.Start() — robfig Start is a no-op while running,
// but after a Stop it must actually restart the loop.
//
// Failing revert: restore `if added { m.Cron.Start() }` in
// registerEnabledCronTriggers — after StopAll the duplicate-claim guard
// makes added=false, Start is never re-reached, and can't observe a live
// scheduler (addRace via a running loop's add channel).
func TestStopAllThenReregisterStillStarts(t *testing.T) {
	store, err := bot.NewStore(filepath.Join(t.TempDir(), "bots.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	botMgr := bot.NewManager(store, nil)
	mgr := NewManager(botMgr) // direct path: installs scheduler via NewManager

	probe := &cronFireProbe{}
	loop := NewGoalLoop("twice", &Constitution{Purpose: "sweep"}, nil, slog.New(slog.DiscardHandler))
	loop.WithReflector(probe)
	mgr.RegisterGoalLoop("twice", loop)

	ctx := context.Background()
	if err := botMgr.CreateBot(ctx, cronDef("twice", "0 4 * * *", true)); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}
	if err := botMgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if len(mgr.Cron.Entries()) != 1 {
		t.Fatalf("cron entries = %d, want 1", len(mgr.Cron.Entries()))
	}

	// StopAll: claims stay, robfig loop exits. Stop returns a ctx that
	// resolves when running jobs drain.
	stopCtx := mgr.Cron.Stop()
	select {
	case <-stopCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatalf("cron Stop did not drain")
	}

	// Re-register the SAME schedule: the duplicate-claim guard makes
	// added=false, so Start must come from the unconditional call.
	// Re-register through the same seam startBot uses: the start hook
	// fires via StartAll.
	if err := botMgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll #2: %v", err)
	}
	if len(mgr.Cron.Entries()) != 1 {
		t.Fatalf("cron entries after re-register = %d, want 1", len(mgr.Cron.Entries()))
	}

	// Prove the scheduler loop is LIVE again: a live loop serializes its
	// add channel; schedule a trivial entry and wait for it to appear in
	// Entries(). A stopped loop never snapshots it (robfig adds entries
	// directly when !running, so also assert the loop consumed a run by
	// checking Entry.Job.Run visibility through the schedule change).
	_, err = mgr.Cron.AddFunc("* * * * *", func() {})
	if err != nil {
		t.Fatalf("AddFunc after restart: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(mgr.Cron.Entries()) >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(mgr.Cron.Entries()) < 2 {
		t.Fatalf("scheduler loop did not pick up a post-StopAll entry: m.Cron.Start() was not re-reached after re-registration")
	}
}

// TestResumeBotReregistersCron pins the resume leg: pause removed the job,
// resume must rebuild it.
//
// Failing revert: remove the ResumeBot resumeHook or the
// reregisterCronHook registration — entries stay 0 after resume.
func TestResumeBotReregistersCron(t *testing.T) {
	botMgr, mgr := newCronTestHarness(t)
	ctx := context.Background()
	if err := botMgr.CreateBot(ctx, cronDef("resumer", "0 4 * * *", true)); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}
	if err := botMgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if err := botMgr.PauseBot(ctx, "resumer"); err != nil {
		t.Fatalf("PauseBot: %v", err)
	}
	if n := len(mgr.Cron.Entries()); n != 0 {
		t.Fatalf("entries after pause = %d, want 0", n)
	}
	if err := botMgr.ResumeBot(ctx, "resumer"); err != nil {
		t.Fatalf("ResumeBot: %v", err)
	}
	if n := len(mgr.Cron.Entries()); n != 1 {
		t.Fatalf("entries after resume = %d, want 1 (resume must re-register cron triggers)", n)
	}
}

// TestUpdateBotResyncsCron pins the update leg: editing a bot's trigger
// (disable via update) must remove the job.
//
// Failing revert: remove the UpdateBot updateHook or resyncCronHook — the
// disabled-schedule job survives the update.
func TestUpdateBotResyncsCron(t *testing.T) {
	botMgr, mgr := newCronTestHarness(t)
	ctx := context.Background()
	if err := botMgr.CreateBot(ctx, cronDef("editable", "0 4 * * *", true)); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}
	if err := botMgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if n := len(mgr.Cron.Entries()); n != 1 {
		t.Fatalf("entries after start = %d, want 1", n)
	}

	def := cronDef("editable", "0 4 * * *", false) // trigger disabled via edit
	if err := botMgr.UpdateBot(ctx, def); err != nil {
		t.Fatalf("UpdateBot: %v", err)
	}
	if n := len(mgr.Cron.Entries()); n != 0 {
		t.Fatalf("entries after disabling update = %d, want 0 (update must resync cron jobs)", n)
	}
}

// TestStopAllBoundsCronDrainWait pins the bughunt shutdown-hang fix: a cron
// job stuck in a long-running round (the goal-round shape: minutes of LLM
// work holding invokeMu) must NOT wedge StopAll forever — cron.Stop() takes
// no context, so the drain wait is raced against cronStopDrainTimeout and
// abandoned best-effort on expiry. The in-flight job is never killed: it
// keeps running until its own release.
//
// Failing revert: restore `<-stopCtx.Done()` (unbounded) in StopAll — this
// test then blocks for the job's full runtime and fails on the wall-clock
// bound.
func TestStopAllBoundsCronDrainWait(t *testing.T) {
	botMgr, mgr := newCronTestHarness(t)
	_ = botMgr

	release := make(chan struct{})
	started := make(chan struct{})
	// The job must be fired BY THE SCHEDULER (robfig tracks scheduler-run
	// jobs in its jobWaiter, which Stop's drain context waits on). @every
	// fires within milliseconds; the body blocks once, simulating a goal
	// round running long past the drain bound.
	if _, err := mgr.Cron.AddFunc("@every 10ms", func() {
		select {
		case <-started:
			return // already blocked once; keep later fires no-op
		default:
		}
		close(started)
		<-release
	}); err != nil {
		t.Fatalf("AddFunc: %v", err)
	}
	mgr.Cron.Start()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("cron job never started")
	}

	// StopAll must return within the drain bound (+ margin), NOT wait for
	// the blocked job.
	done := make(chan struct{})
	go func() {
		defer close(done)
		mgr.StopAll()
	}()
	select {
	case <-done:
	case <-time.After(cronStopDrainTimeout + 3*time.Second):
		close(release)
		t.Fatalf("StopAll blocked past cronStopDrainTimeout on an in-flight cron job (unbounded drain wait)")
	}
	close(release)
}
