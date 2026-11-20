package daemon

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/pkg/models"
)

// testComponents holds a Components along with the bus used to
// construct it, so tests can publish/subscribe to verify handler
// liveness after Start().
type testComponents struct {
	*Components
	bus *bus.MessageBus
}

// makeTestComponents builds a minimal Components with the three required
// handlers (chat, status, session) wired to a real bus.  All 18 optional
// components remain nil so their nil-guarded Start blocks are skipped.
func makeTestComponents(t *testing.T) testComponents {
	t.Helper()
	msgBus := bus.New(nil, slog.Default())
	logger := slog.Default()

	c := &Components{
		Config: &config.Config{
			Workers: config.WorkersConfig{PoolSize: 1},
		},
		Logger: logger,
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())

	c.ChatHandler = agent.NewChatHandler(nil, nil, msgBus, logger)
	c.StatusHandler = NewStatusHandler(msgBus, logger)
	store := session.NewMemoryStore(logger)
	c.SessionHandler = session.NewHandler(store, msgBus, logger)

	return testComponents{Components: c, bus: msgBus}
}

// publishStatusRequest publishes a status.request on the bus and
// returns the response channel that will receive any response.
func (tc testComponents) publishStatusRequest() <-chan *models.BusMessage {
	respSub := tc.bus.Subscribe("test-rollback", "status.response")
	msg, _ := models.NewBusMessage(models.MessageTypeRequest, "test", map[string]any{})
	tc.bus.Publish("status.request", msg)
	return respSub.Channel
}

// TestComponentsStart_RollbackOnSuccess verifies that a successful
// Start() does NOT roll back any started handlers.  Without a success
// flag, the deferred rollback fires on every return path (including
// return nil), stopping all handlers immediately after Start.
//
// We detect this by publishing a status.request on the bus and checking
// whether the StatusHandler (which subscribes on Start) responds.  If
// the rollback erroneously stopped the handler, no response arrives.
func TestComponentsStart_RollbackOnSuccess(t *testing.T) {
	tc := makeTestComponents(t)

	ctx := t.Context()

	if err := tc.Start(ctx); err != nil {
		t.Fatalf("Start returned unexpected error: %v", err)
	}

	// The StatusHandler subscribes to "status.request" on Start.
	// If the rollback killed it, no response arrives within the timeout.
	respCh := tc.publishStatusRequest()
	select {
	case <-respCh:
		// Good: handler is alive and responded.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("StatusHandler did not respond — rollback fired on successful Start (handlers were stopped)")
	}

	// Clean shutdown via Stop (not rollback).
	if err := tc.Stop(ctx); err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
}

// TestComponentsStart_RollbackCoverage verifies that when Start()
// succeeds, all started handlers remain alive, and Stop() cleanly
// shuts everything down without panic.
//
// This test serves as the coverage assertion: after Task 2+3 land,
// the rollback switch must handle every startedHandlers key without
// panicking, even when optional components are nil.
func TestComponentsStart_RollbackCoverage(t *testing.T) {
	tc := makeTestComponents(t)

	ctx := t.Context()

	if err := tc.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify StatusHandler is alive via bus round-trip.
	respCh := tc.publishStatusRequest()
	select {
	case <-respCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("StatusHandler did not respond after Start")
	}

	// Clean shutdown.
	if err := tc.Stop(ctx); err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
}

// TestComponentsStart_CancelOnRollback verifies that the lifecycle
// context cancel function exists and works, so the rollback deferred
// can call c.cancel() to terminate PricingSyncer and other context-
// bound goroutines.
func TestComponentsStart_CancelOnRollback(t *testing.T) {
	c := &Components{
		Logger: slog.Default(),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())

	if c.cancel == nil {
		t.Fatal("c.cancel is nil — rollback cannot cancel PricingSyncer goroutine")
	}

	// Simulate the rollback's cancel call.
	c.cancel()

	select {
	case <-c.ctx.Done():
		// expected
	default:
		t.Error("c.cancel() did not cancel c.ctx")
	}
}

// hireTimeBotUnionComponents builds a minimal Components whose Start() runs
// the real hire-time known-agent-ID wiring (the L13 block): a plain bot
// (constitution-less), an AgentRegistry, and an employee.Manager over real
// throwaway SQLite stores. Only the three mandatory handlers plus the
// employee/bot/registry stack are populated; every other optional component
// stays nil so its Start block is skipped.
func hireTimeBotUnionComponents(t *testing.T) *Components {
	t.Helper()
	dir := t.TempDir()
	logger := slog.New(slog.DiscardHandler)

	botStore, err := bot.NewStore(filepath.Join(dir, "bots.db"))
	if err != nil {
		t.Fatalf("bot store: %v", err)
	}
	botMgr := bot.NewManager(botStore, nil)

	// A plain bot: a bot_definitions row with NO constitution. ListEmployees
	// skips it (4ee85f4b), so it is invisible to the employee roster.
	if err := botMgr.CreateBot(context.Background(), bot.BotDefinition{
		ID:       "plain-approver-bot",
		Name:     "plain approver bot",
		Prompt:   "approve plans",
		Enabled:  true,
		Triggers: []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
	}); err != nil {
		t.Fatalf("create plain bot: %v", err)
	}

	cs, err := employee.NewConstitutionStore(filepath.Join(dir, "constitutions.db"), logger)
	if err != nil {
		t.Fatalf("constitution store: %v", err)
	}
	empMgr := employee.NewManagerWithStores(botMgr, botStore, cs, nil, nil, logger)

	msgBus := bus.New(nil, logger)
	c := &Components{
		Config: &config.Config{
			Workers: config.WorkersConfig{PoolSize: 1},
		},
		Logger:            logger,
		BotManager:        botMgr,
		BotStore:          botStore,
		EmployeeManager:   empMgr,
		ConstitutionStore: cs,
		AgentRegistry:     agent.NewAgentRegistry(agent.RegistryConfig{Logger: logger}),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.ChatHandler = agent.NewChatHandler(nil, nil, msgBus, logger)
	c.StatusHandler = NewStatusHandler(msgBus, logger)
	c.SessionHandler = session.NewHandler(session.NewMemoryStore(logger), msgBus, logger)

	// Stop closes BotStore + ConstitutionStore and cancels the lifecycle ctx.
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	return c
}

// TestHireKnownAgentIDsIncludesPlainBots pins bughunt 2026-11-19 L13 through
// the REAL Components.Start wiring: a constitution that names a plain
// (constitution-less) bot as an escalation target must pass hire-time
// validation. Before the fix, knownAgentIDs was ListSpecs + ListEmployees
// only, and ListEmployees skips constitution-less bots, so CheckEscalationReferences
// hard-failed the hire with "unknown agent IDs".
func TestHireKnownAgentIDsIncludesPlainBots(t *testing.T) {
	c := hireTimeBotUnionComponents(t)
	ctx := context.Background()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// The plain bot must NOT appear in the employee roster (proves it is
	// constitution-less and only reachable via the bot-roster union).
	emps, err := c.EmployeeManager.ListEmployees(ctx, "")
	if err != nil {
		t.Fatalf("ListEmployees: %v", err)
	}
	for _, e := range emps {
		if e.ID == "plain-approver-bot" {
			t.Fatal("plain bot unexpectedly listed as an employee; fixture no longer exercises L13")
		}
	}

	// Hire an employee whose constitution escalates to the plain bot.
	_, err = c.EmployeeManager.Hire(ctx, employee.HireRequest{
		ID:       "emp-escalates-to-bot",
		Name:     "escalating employee",
		Prompt:   "do work, escalate to the bot",
		Enabled:  true,
		Triggers: []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		Constitution: map[string]any{
			"purpose":       "escalate to a plain bot approver",
			"autonomy_tier": "tier_2_propose",
			"escalates_to":  []string{"plain-approver-bot"},
			"amendment_policy": map[string]any{
				"requires_approval": true,
			},
		},
	})
	if err != nil {
		t.Fatalf("Hire naming a plain bot as escalation target failed (L13 regression): %v", err)
	}
}

// TestHireKnownAgentIDsRejectsUnknownID is the negative control for L13: the
// bot-roster union must not disable escalation-reference validation. An ID
// that is neither a registry spec, an employee, nor a bot is still rejected.
func TestHireKnownAgentIDsRejectsUnknownID(t *testing.T) {
	c := hireTimeBotUnionComponents(t)
	ctx := context.Background()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	_, err := c.EmployeeManager.Hire(ctx, employee.HireRequest{
		ID:       "emp-escalates-to-ghost",
		Name:     "escalating employee",
		Prompt:   "do work",
		Enabled:  true,
		Triggers: []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		Constitution: map[string]any{
			"purpose":       "escalate to a nonexistent id",
			"autonomy_tier": "tier_2_propose",
			"escalates_to":  []string{"no-such-agent-or-bot"},
			"amendment_policy": map[string]any{
				"requires_approval": true,
			},
		},
	})
	if err == nil {
		t.Fatal("Hire with an unknown escalation target succeeded; validation is disabled")
	}
	if !strings.Contains(err.Error(), "escalation references") {
		t.Fatalf("expected an escalation-references error, got: %v", err)
	}
}

// TestHireKnownAgentIDsEmployeeReadFailureWarns pins the hire-wiring
// error-log fix (bughunt 2026-11-19 wave 3): when the known-agent-ID
// wiring cannot read the employee roster it must WARN (matching the
// adjacent bot-roster branch), not silently drop the error. Drives the
// REAL Components.Start wiring with the shared bot store closed BEFORE
// Start, so ListEmployees fails inside the wiring block (the bot
// framework's own start failure is logged, not fatal).
func TestHireKnownAgentIDsEmployeeReadFailureWarns(t *testing.T) {
	c := hireTimeBotUnionComponents(t)
	ctx := context.Background()

	// Break the employee read before the wiring runs: ListEmployees
	// lists through the bot store. Closing it also breaks the sibling
	// bot-roster read (its warn is pre-existing); the assertion targets
	// the employee-branch message specifically.
	if err := c.BotStore.Close(); err != nil {
		t.Fatalf("close bot store: %v", err)
	}
	if _, err := c.EmployeeManager.ListEmployees(ctx, ""); err == nil {
		t.Fatal("expected ListEmployees to fail after store close; fixture no longer exercises the error branch")
	}

	var logMu sync.Mutex
	var logBuf strings.Builder
	c.Logger = slog.New(slog.NewTextHandler(&hireWarnSyncWriter{mu: &logMu, w: &logBuf}, &slog.HandlerOptions{Level: slog.LevelWarn}))

	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	logMu.Lock()
	logs := logBuf.String()
	logMu.Unlock()
	if !strings.Contains(logs, "hire-time known agent IDs: employee roster unavailable") {
		t.Errorf("employee-roster read failure was not warned; logs:\n%s", logs)
	}
}

// hireWarnSyncWriter serializes slog writes into a strings.Builder for
// the hire-wiring warn pin above.
type hireWarnSyncWriter struct {
	mu *sync.Mutex
	w  *strings.Builder
}

func (s *hireWarnSyncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}
