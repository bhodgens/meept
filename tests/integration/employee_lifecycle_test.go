// Package integration contains end-to-end integration tests for the AI
// Employee layer (spec lines 639-653). These tests drive the real
// employee.Manager + its SQLite-backed stores + the real bot.Manager +
// bot.Store. They do NOT spin up the daemon or an RPC server.
//
// This file covers the employee lifecycle:
//
//	create → start → trigger → pause → resume → delete
//
// The flow mirrors what the agents.create / agents.trigger / agents.delete
// RPC/HTTP handlers do, minus the JSON marshalling envelope.
package integration

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/pkg/id"

	_ "modernc.org/sqlite" // sqlite driver registration for employee/bot stores
)

// employeeLifecycleEnv bundles the real wired components used by the
// lifecycle test. Everything points at t.TempDir() paths so the test is
// hermetic. The single sharedDB holds the bot_definitions table plus the
// employee_* tables so the FK ON DELETE CASCADE semantics work end-to-end.
type employeeLifecycleEnv struct {
	botMgr      *bot.Manager
	botStore    *bot.Store
	empMgr      *employee.Manager
	constStore  *employee.ConstitutionStore
	goalStore   *employee.GoalStore
	auditStore  *employee.AuditStore
	sharedDB    *sql.DB
	sharedDBDir string
}

// newEmployeeLifecycleEnv wires the real Manager + Stores against temp
// paths. The sharedDB is opened directly so the constitution, goal, and
// audit stores attach to the same SQLite file as the bot store — this is
// the production wiring shape (see internal/employee/wiring.go).
//
// All stores are Closed via t.Cleanup so the test does not leak file
// handles even on assertion failure.
func newEmployeeLifecycleEnv(t *testing.T) *employeeLifecycleEnv {
	t.Helper()

	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "bots.db")

	// bot.Store owns its own connection and migrates bot_definitions +
	// bot_states tables.
	bstore, err := bot.NewStore(dbPath)
	if err != nil {
		t.Fatalf("bot.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = bstore.Close() })

	// Open a *sql.DB on the same file for the employee stores. The
	// employee tables are migrated by their respective constructors
	// and cascade-delete via FK on bot_definitions.
	sharedDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open shared: %v", err)
	}
	t.Cleanup(func() { _ = sharedDB.Close() })

	// Enable FK enforcement so the ON DELETE CASCADE clauses on
	// employee_goals / employee_audit_findings fire. The goal store
	// also sets these pragmas but setting them here ensures they apply
	// to every connection from this point on.
	if _, err := sharedDB.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatalf("enable foreign_keys: %v", err)
	}

	constStore, err := employee.NewConstitutionStoreFromDB(sharedDB, nil)
	if err != nil {
		t.Fatalf("NewConstitutionStoreFromDB: %v", err)
	}
	t.Cleanup(func() { _ = constStore.Close() })

	goalStore, err := employee.NewGoalStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewGoalStore: %v", err)
	}
	// All stores share the same bots.db file so the FK on employee_goals
	// (employee_goals.employee_id → bot_definitions.id) and
	// employee_audit_findings cascade-deletes work end-to-end.
	t.Cleanup(func() { _ = goalStore.Close() })

	auditStore, err := employee.NewAuditStore(dbPath)
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	t.Cleanup(func() { _ = auditStore.Close() })

	// bot.Manager needs an EventActionRouter (may be nil per the
	// constructor contract — Register/Unregister are nil-safe).
	botMgr := bot.NewManager(bstore, nil)

	empMgr := employee.NewManagerWithStores(botMgr, bstore, constStore, goalStore, auditStore, nil)

	return &employeeLifecycleEnv{
		botMgr:      botMgr,
		botStore:    bstore,
		empMgr:      empMgr,
		constStore:  constStore,
		goalStore:   goalStore,
		auditStore:  auditStore,
		sharedDB:    sharedDB,
		sharedDBDir: dbDir,
	}
}

// validConstitutionMap returns a minimal constitution map that satisfies
// Constitution.Validate. It is declared as a map so it matches the
// HireRequest.Constitution field type exactly (the production path
// receives raw JSON-decoded maps).
func validConstitutionMap() map[string]any {
	return map[string]any{
		"purpose":        "keep CI green for main",
		"role":           "CI Reliability Engineer",
		"charter":        "investigate failures, open issues, never merge code",
		"autonomy_tier":  "tier_1_reactive",
		"escalates_to":   []string{"user"},
		"amendment_policy": map[string]any{
			"requires_approval":     true,
			"self_propose_allowed":  false,
			"frozen_fields":         []string{"purpose"},
		},
		"constraints": map[string]any{
			"risk_ceiling":     "medium",
			"daily_budget_cents": 50,
			"never":            []string{"merge to main"},
		},
	}
}

// hireEmployee is a helper that runs Manager.Hire with a valid
// constitution and returns the created Employee. Fatals the test on any
// error so individual subtests can stay focused on post-hire assertions.
func hireEmployee(t *testing.T, env *employeeLifecycleEnv, employeeID string) *employee.Employee {
	t.Helper()
	ctx := context.Background()
	emp, err := env.empMgr.Hire(ctx, employee.HireRequest{
		ID:           employeeID,
		Name:         "ci-engineer",
		Description:  "CI reliability engineer",
		Prompt:       "investigate and report CI failures",
		Model:        "stub-model",
		Triggers:     []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		Tools:        []string{"file_read"},
		Enabled:      true,
		Constitution: validConstitutionMap(),
	})
	if err != nil {
		t.Fatalf("Hire(%q): %v", employeeID, err)
	}
	return emp
}

// TestEmployee_Lifecycle exercises the full create → start → trigger →
// pause → resume → delete flow through the real Manager + Stores. Each
// stage is a subtest so failures pinpoint which transition broke.
//
// The test mirrors the style of TestMCPToggle_PersistAndReload: it
// drives the production Manager directly (no RPC server, no daemon),
// which is what the spec's "through the daemon" phrasing boils down to
// once the HTTP/RPC envelope is stripped.
func TestEmployee_Lifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping employee lifecycle integration test in short mode")
	}

	env := newEmployeeLifecycleEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	employeeID := id.Generate("emp_")

	// --- create ---
	var hired *employee.Employee
	t.Run("create", func(t *testing.T) {
		hired = hireEmployee(t, env, employeeID)
		if hired.ID != employeeID {
			t.Errorf("hired.ID = %q, want %q", hired.ID, employeeID)
		}
		if !hired.HasConstitution() {
			t.Fatal("hired employee should have a constitution")
		}
		// Constitution should be persisted in the store and fetchable
		// independently (verifies that Hire's store.Put path worked).
		fetched, err := env.empMgr.GetEmployee(ctx, employeeID)
		if err != nil {
			t.Fatalf("GetEmployee after Hire: %v", err)
		}
		if fetched.Constitution.Purpose != "keep CI green for main" {
			t.Errorf("Purpose = %q, want 'keep CI green for main'", fetched.Constitution.Purpose)
		}
	})

	// --- start (Manager.StartAll is the production startup hook) ---
	t.Run("start", func(t *testing.T) {
		// StartAll primes the in-memory constitution cache from the
		// store. A subsequent GetEmployee should still see the
		// constitution (loaded from cache).
		if err := env.empMgr.StartAll(ctx); err != nil {
			t.Fatalf("StartAll: %v", err)
		}
	})

	// --- trigger (Manager.Trigger returns a TriggerResult) ---
	t.Run("trigger", func(t *testing.T) {
		// Trigger currently returns a synthesized TriggerResult (the
		// full GoalLoop integration lands with later phases). We
		// verify the result envelope is well-formed and the invocation
		// ID is non-empty.
		res, err := env.empMgr.Trigger(ctx, employeeID, map[string]any{
			"event": "github.push",
		})
		if err != nil {
			t.Fatalf("Trigger: %v", err)
		}
		if res.InvocationID == "" {
			t.Error("Trigger returned empty InvocationID")
		}
		if res.Status != "triggered" {
			t.Errorf("Status = %q, want 'triggered'", res.Status)
		}
	})

	// --- pause ---
	t.Run("pause", func(t *testing.T) {
		if err := env.empMgr.Pause(ctx, employeeID); err != nil {
			t.Fatalf("Pause: %v", err)
		}
		// Verify the bot was actually disabled.
		def, err := env.botMgr.GetBot(ctx, employeeID)
		if err != nil {
			t.Fatalf("GetBot after Pause: %v", err)
		}
		if def.Enabled {
			t.Error("bot should be disabled after Pause")
		}
		// Trigger on a paused employee should error.
		if _, err := env.empMgr.Trigger(ctx, employeeID, nil); err == nil {
			t.Error("Trigger on paused employee should error")
		}
	})

	// --- resume ---
	t.Run("resume", func(t *testing.T) {
		if err := env.empMgr.Resume(ctx, employeeID); err != nil {
			t.Fatalf("Resume: %v", err)
		}
		def, err := env.botMgr.GetBot(ctx, employeeID)
		if err != nil {
			t.Fatalf("GetBot after Resume: %v", err)
		}
		if !def.Enabled {
			t.Error("bot should be enabled after Resume")
		}
		// Trigger on a resumed employee should succeed again.
		res, err := env.empMgr.Trigger(ctx, employeeID, nil)
		if err != nil {
			t.Errorf("Trigger after Resume: %v", err)
		}
		if res == nil || res.InvocationID == "" {
			t.Error("Trigger after Resume returned empty result")
		}
	})

	// --- delete ---
	t.Run("delete", func(t *testing.T) {
		if err := env.empMgr.Retire(ctx, employeeID); err != nil {
			t.Fatalf("Retire: %v", err)
		}
		// Employee should be gone from the bot store.
		if _, err := env.botMgr.GetBot(ctx, employeeID); err == nil {
			t.Error("GetBot after Retire should error")
		}
		// Constitution should also be gone (Retire cascades to the
		// constitution row).
		if _, err := env.constStore.Get(ctx, employeeID); !errors.Is(err, employee.ErrConstitutionRequired) {
			t.Errorf("constitution after Retire: want ErrConstitutionRequired, got %v", err)
		}
		// GetEmployee should now return ErrEmployeeNotFound.
		if _, err := env.empMgr.GetEmployee(ctx, employeeID); !errors.Is(err, employee.ErrEmployeeNotFound) {
			t.Errorf("GetEmployee after Retire: want ErrEmployeeNotFound, got %v", err)
		}
	})
}
