//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/bus"
)

func TestBotLifecycle_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Set up store
	dir := t.TempDir()
	store, err := bot.NewStore(filepath.Join(dir, "bots.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	// Set up event router with nil handler (we don't need actual execution)
	msgBus := bus.New(nil, nil)
	router := bot.NewEventActionRouter(msgBus, nil)

	// Create manager
	mgr := bot.NewManager(store, router)

	// Create a bot
	def := bot.BotDefinition{
		ID:          "test-ci-monitor",
		Name:        "CI Monitor",
		Description: "Monitors CI pipeline status",
		Prompt:      "Check the CI status for the main project. Report any failures.",
		Triggers: []bot.BotTrigger{
			{Type: bot.TriggerTypeCron, Schedule: "*/15 * * * *", Enabled: true},
			{Type: bot.TriggerTypeBusEvent, Topic: "calendar.reminder", Enabled: true},
		},
		MemoryScope: bot.MemoryScopePrivate,
		Tools:       []string{"web_fetch", "memory_store", "memory_search"},
		Constraints: bot.BotConstraints{
			MaxIterations:        5,
			Timeout:              2 * time.Minute,
			DailyBudgetCents:     100,
			MaxInvocationsPerDay: 50,
		},
		Enabled: true,
	}

	if err := mgr.CreateBot(ctx, def); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}

	// List bots
	bots, err := mgr.ListBots(ctx)
	if err != nil {
		t.Fatalf("ListBots: %v", err)
	}
	if len(bots) != 1 {
		t.Fatalf("expected 1 bot, got %d", len(bots))
	}

	// Get bot
	got, err := mgr.GetBot(ctx, "test-ci-monitor")
	if err != nil {
		t.Fatalf("GetBot: %v", err)
	}
	if got.ID != "test-ci-monitor" {
		t.Errorf("ID = %q, want %q", got.ID, "test-ci-monitor")
	}
	if got.Name != "CI Monitor" {
		t.Errorf("Name = %q, want %q", got.Name, "CI Monitor")
	}
	if got.MemoryScope != bot.MemoryScopePrivate {
		t.Errorf("MemoryScope = %q, want %q", got.MemoryScope, bot.MemoryScopePrivate)
	}

	// Pause and resume
	if err := mgr.PauseBot(ctx, "test-ci-monitor"); err != nil {
		t.Fatalf("PauseBot: %v", err)
	}
	paused, _ := mgr.GetBot(ctx, "test-ci-monitor")
	if paused.Enabled {
		t.Error("bot should be paused")
	}
	if err := mgr.ResumeBot(ctx, "test-ci-monitor"); err != nil {
		t.Fatalf("ResumeBot: %v", err)
	}
	resumed, _ := mgr.GetBot(ctx, "test-ci-monitor")
	if !resumed.Enabled {
		t.Error("bot should be resumed")
	}

	// Test runner budget check
	runner := bot.NewBotRunner(def)
	state := &bot.BotState{
		TodayCostCents: 50,
		TodayDate:      time.Now().Format("2006-01-02"),
	}
	if !runner.ShouldRun(state) {
		t.Error("should allow run under budget")
	}
	state.TodayCostCents = 100
	if runner.ShouldRun(state) {
		t.Error("should deny run at budget cap")
	}

	// Test invocation cap
	state.TodayRuns = 50
	state.TodayCostCents = 0
	if runner.ShouldRun(state) {
		t.Error("should deny run at invocation cap")
	}

	// Test memory namespace
	ns := bot.NewMemoryNamespace("test-ci-monitor")
	if ns.Prefix() != "bot:test-ci-monitor" {
		t.Errorf("Prefix = %q, want %q", ns.Prefix(), "bot:test-ci-monitor")
	}
	query := ns.ScopeQuery(bot.MemoryScopePrivate, "last CI results")
	if query != "bot:test-ci-monitor last CI results" {
		t.Errorf("unexpected scoped query: %q", query)
	}
	sharedQuery := ns.ScopeQuery(bot.MemoryScopeShared, "last CI results")
	if sharedQuery != "last CI results" {
		t.Errorf("shared query should pass through: %q", sharedQuery)
	}

	// Test runner prompt construction
	prompt := runner.BuildSystemPrompt("CI pipeline check at midnight")
	if prompt == "" {
		t.Fatal("expected non-empty system prompt")
	}

	// Test status
	status, err := mgr.GetBotStatus(ctx, "test-ci-monitor")
	if err != nil {
		t.Fatalf("GetBotStatus: %v", err)
	}
	if status.Status != bot.BotStatusStopped {
		t.Errorf("Status = %q, want %q", status.Status, bot.BotStatusStopped)
	}

	// Delete bot
	if err := mgr.DeleteBot(ctx, "test-ci-monitor"); err != nil {
		t.Fatalf("DeleteBot: %v", err)
	}
	bots, _ = mgr.ListBots(ctx)
	if len(bots) != 0 {
		t.Errorf("expected 0 bots after delete, got %d", len(bots))
	}
}

func TestBotWebhook_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()

	dir := t.TempDir()
	store, _ := bot.NewStore(filepath.Join(dir, "bots.db"))
	defer store.Close()

	mgr := bot.NewManager(store, nil)
	handler := bot.NewWebhookHandler(mgr)

	def := bot.BotDefinition{
		ID:     "webhook-int-test",
		Name:   "Webhook Integration Test",
		Prompt: "Handle webhook events",
		Triggers: []bot.BotTrigger{
			{Type: bot.TriggerTypeWebhook, Enabled: true, PromptTemplate: "Event: {{.action}} on {{.repo}}"},
		},
		MemoryScope: bot.MemoryScopePrivate,
		Constraints: bot.BotConstraints{
			MaxIterations: 3,
			Timeout:       time.Minute,
		},
		Enabled: true,
	}
	mgr.CreateBot(ctx, def)

	// POST to trigger
	body := `{"action":"push","repo":"meept"}`
	req := httptest.NewRequest("POST", "/api/v1/bot/webhook-int-test/trigger", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "triggered" {
		t.Errorf("status = %v, want %q", resp["status"], "triggered")
	}
}
