package employee

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/config"
)

// standaloneWiringConfig builds a config with employees enabled and bots
// disabled — the wiring combination that routes the goal/audit stores onto
// standalone SQLite files under <data_dir>/employees/.
func standaloneWiringConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		Daemon: config.DaemonConfig{DataDir: t.TempDir()},
		Employees: config.EmployeesConfig{
			Enabled: true,
		},
		Bots: config.BotsConfig{
			Enabled: false,
		},
	}
}

// TestWiringStandaloneGoalStoreFKResolves reproduces the standalone-DB FK
// breakage: with bots.enabled=false the goal store gets its own goals.db,
// but employee_goals declares FOREIGN KEY (employee_id) REFERENCES
// bot_definitions(id), a table that only existed in the shared bots.db.
// Before the fix, EVERY insert failed with
// "no such table: main.bot_definitions" (the referenced table was absent
// from the standalone file entirely). After the fix, migrate() creates a
// schema-only bot_definitions stub in the standalone DB so the FK resolves.
// This test mirrors the daemon's wiring order: the bot store exists on its
// own bots.db (components.go constructs it whenever bots OR employees are
// enabled), the employee stores get standalone files, and goals are
// inserted through Manager.CreateGoal.
func TestWiringStandaloneGoalStoreFKResolves(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	botDataDir := filepath.Join(dir, "bots")
	if err := os.MkdirAll(botDataDir, 0o755); err != nil {
		t.Fatalf("mkdir bots dir: %v", err)
	}
	cfg := &config.Config{
		Daemon:    config.DaemonConfig{DataDir: dir},
		Employees: config.EmployeesConfig{Enabled: true},
		Bots:      config.BotsConfig{Enabled: false},
	}

	// The daemon's bot framework still exists ("for_employees") with its
	// own bots.db — standalone employee stores must not depend on sharing
	// that file.
	botStore, err := bot.NewStore(filepath.Join(botDataDir, "bots.db"))
	if err != nil {
		t.Fatalf("bot.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = botStore.Close() })
	botMgr := bot.NewManager(botStore, nil)
	const empID = "bot-standalone-1"
	if err := botMgr.CreateBot(ctx, bot.BotDefinition{
		ID:        empID,
		Name:      "standalone",
		Prompt:    "p",
		Triggers:  []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}

	result, err := NewManagerFromConfig(ctx, cfg, botMgr, botStore, nil)
	if err != nil {
		t.Fatalf("NewManagerFromConfig: %v", err)
	}
	t.Cleanup(func() {
		_ = result.GoalStore.Close()
		_ = result.AuditStore.Close()
	})
	if result.GoalStore == nil {
		t.Fatal("goal store not constructed in standalone mode")
	}
	if result.SharedDBPath != "" {
		t.Fatalf("bots disabled must NOT share the bots db, got shared %q", result.SharedDBPath)
	}

	// Standalone wiring mirrors each existing employee into the goal DB's
	// bot_definitions stub (the FK must keep resolving across the roster).
	g := &Goal{
		EmployeeID: empID,
		Title:      "standalone goal",
		Mandate:    "must insert without no such table: main.bot_definitions",
		State:      GoalActive,
		Source:     SourceUser,
		CreatedAt:  time.Now().UTC(),
	}
	if err := result.GoalStore.Create(ctx, g); err != nil {
		t.Fatalf("goal insert in standalone mode failed: %v", err)
	}
	// The inserted goal must reference the stub row wiring created:
	var n int
	if err := result.GoalStore.db.QueryRow(
		`SELECT COUNT(*) FROM bot_definitions WHERE id = ?`, empID).Scan(&n); err != nil {
		t.Fatalf("count stub row: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected wiring to mirror employee %q into the standalone stub, got %d rows", empID, n)
	}
}

// TestWiringStandaloneAuditStoreFKResolves is the sibling check: the audit
// findings table carries the same bot_definitions FK and fails the same
// way on a standalone audit.db.
func TestWiringStandaloneAuditStoreFKResolves(t *testing.T) {
	ctx := context.Background()
	cfg := standaloneWiringConfig(t)

	result, err := NewManagerFromConfig(ctx, cfg, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManagerFromConfig: %v", err)
	}
	t.Cleanup(func() {
		_ = result.GoalStore.Close()
		_ = result.AuditStore.Close()
	})
	if result.AuditStore == nil {
		t.Fatal("audit store not constructed in standalone mode")
	}

	f := AuditFinding{
		EmployeeID: "bot-standalone-1",
		Severity:   SeverityInfo,
		Checkpoint: CheckpointPeriodic,
	}
	if err := result.AuditStore.Create(ctx, f); err != nil {
		t.Fatalf("audit finding insert in standalone mode failed: %v", err)
	}
}

// TestWiringStandaloneFKStillEnforced pins the semantics of option (a):
// the bot_definitions stub keeps the FK real, so a goal for an
// employee_id with no bot_definitions row must still be rejected.
func TestWiringStandaloneFKStillEnforced(t *testing.T) {
	ctx := context.Background()
	cfg := standaloneWiringConfig(t)

	result, err := NewManagerFromConfig(ctx, cfg, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManagerFromConfig: %v", err)
	}
	t.Cleanup(func() {
		_ = result.GoalStore.Close()
		_ = result.AuditStore.Close()
	})

	g := &Goal{
		EmployeeID: "bot-does-not-exist",
		Title:      "orphan",
		Mandate:    "FK must reject",
		State:      GoalActive,
		Source:     SourceUser,
		CreatedAt:  time.Now().UTC(),
	}
	err = result.GoalStore.Create(ctx, g)
	if err == nil {
		t.Fatal("goal insert for missing bot_definitions row must violate FK")
	}
	if !strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
		t.Fatalf("expected FK violation, got: %v", err)
	}
}

// TestWiringSharedDBStillSeedsEmployees pins the shared path: when bots are
// enabled the employee stores keep attaching to the bots database and
// seeding a bot definition there makes goal inserts work exactly as
// before the fix.
func TestWiringSharedDBStillSeedsEmployees(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	botDataDir := filepath.Join(dir, "bots")
	if err := os.MkdirAll(botDataDir, 0o755); err != nil {
		t.Fatalf("mkdir bots dir: %v", err)
	}
	cfg := &config.Config{
		Daemon:    config.DaemonConfig{DataDir: dir},
		Employees: config.EmployeesConfig{Enabled: true},
		Bots:      config.BotsConfig{Enabled: true, DataDir: botDataDir},
	}

	botStore, err := bot.NewStore(filepath.Join(botDataDir, "bots.db"))
	if err != nil {
		t.Fatalf("bot.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = botStore.Close() })
	if err := botStore.Create(ctx, bot.BotDefinition{
		ID:        "bot-shared-1",
		Name:      "shared",
		Prompt:    "p",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("botStore.Create: %v", err)
	}

	result, err := NewManagerFromConfig(ctx, cfg, nil, botStore, nil)
	if err != nil {
		t.Fatalf("NewManagerFromConfig: %v", err)
	}
	t.Cleanup(func() {
		_ = result.GoalStore.Close()
		_ = result.AuditStore.Close()
	})
	if result.SharedDBPath == "" {
		t.Fatal("expected shared bots.db wiring when bots enabled")
	}

	g := &Goal{
		EmployeeID: "bot-shared-1",
		Title:      "shared goal",
		Mandate:    "inserts into the shared bots.db",
		State:      GoalActive,
		Source:     SourceUser,
		CreatedAt:  time.Now().UTC(),
	}
	if err := result.GoalStore.Create(ctx, g); err != nil {
		t.Fatalf("goal insert in shared mode failed: %v", err)
	}
}

// TestWiringStandaloneFKDrivesFromStubs guards against a stub vs real
// schema mismatch: the standalone stub must carry the same columns as
// bot.Store's bot_definitions, so a later bot.Store migration on the same
// file stays compatible.
func TestWiringStandaloneStubMatchesBotSchema(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "bots.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(botDefinitionsStubSQL); err != nil {
		t.Fatalf("apply stub: %v", err)
	}
	_ = db.Close()

	// The real bot store must migrate cleanly on top of the stub.
	s, err := bot.NewStore(path)
	if err != nil {
		t.Fatalf("bot.NewStore over stub: %v", err)
	}
	defer s.Close()
	if err := s.Create(ctx, bot.BotDefinition{
		ID:        "bot-x",
		Name:      "x",
		Prompt:    "p",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("bot.Create over stub: %v", err)
	}
}

// TestWiringStandaloneHireMirrorsStub pins the runtime half of the fix:
// employees hired AFTER wiring (the agents.create RPC path) must be
// mirrored into the standalone stubs, or their goals/finding fail FK.
func TestWiringStandaloneHireMirrorsStub(t *testing.T) {
	ctx := context.Background()
	cfg := standaloneWiringConfig(t)

	result, err := NewManagerFromConfig(ctx, cfg, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManagerFromConfig: %v", err)
	}
	t.Cleanup(func() {
		_ = result.GoalStore.Close()
		_ = result.AuditStore.Close()
	})
	if result.Manager == nil || result.GoalStore == nil {
		t.Fatal("wiring incomplete")
	}

	// Hire needs a bot backend; construct one over the daemon-conventional
	// bots.db path and attach it post-construction (the Manager degrades
	// gracefully when wiring ran with nil stores).
	botDir := filepath.Join(cfg.Daemon.DataDir, "bots")
	if err := os.MkdirAll(botDir, 0o755); err != nil {
		t.Fatal(err)
	}
	botStore, err := bot.NewStore(filepath.Join(botDir, "bots.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = botStore.Close() })
	result.Manager.SetBotStore(botStore)
	// Attach a live bot backend so Hire works (wiring ran with nil stores).
	result.Manager = NewManagerWithStores(
		bot.NewManager(botStore, nil), botStore,
		result.ConstitutionStore, result.GoalStore, result.AuditStore, nil)
	result.Manager.SetStandaloneEmployeeDBs([]*sql.DB{result.GoalStore.db, result.AuditStore.db})

	const empID = "bot-hired-later"
	c := validConstitution()
	if _, err := result.Manager.Hire(ctx, HireRequest{
		ID:           empID,
		Name:         "late hire",
		Prompt:       "p",
		Enabled:      true,
		Triggers:     []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		Constitution: constitutionToMap(t, c),
	}); err != nil {
		t.Fatalf("Hire: %v", err)
	}

	g := &Goal{
		EmployeeID: empID,
		Title:      "post-hire goal",
		Mandate:    "FK must resolve via hire-time mirror",
		State:      GoalActive,
		Source:     SourceUser,
		CreatedAt:  time.Now().UTC(),
	}
	if err := result.GoalStore.Create(ctx, g); err != nil {
		t.Fatalf("goal insert for post-wiring hire failed: %v", err)
	}
}
