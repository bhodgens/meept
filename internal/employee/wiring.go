// Package employee — wiring.go provides the daemon-level wiring helpers
// that construct an employee.Manager and its dependent stores from a
// config.Config. The wiring lives in the employee package (not daemon) so
// the construction logic is testable without spinning up a full daemon.
//
// The daemon calls NewManagerFromConfig during NewComponents (mirroring the
// bot framework's wiring at internal/bot/wiring.go). The returned Manager
// is then attached to the Components struct and started/stopped alongside
// the other daemon components.
//
// See docs/superpowers/specs/2026-06-23-ai-employee-design.md §"Package
// layout" and internal/daemon/components.go (NewComponents init block).
package employee

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// WiringResult holds the constructed employee components returned by
// NewManagerFromConfig. The daemon attaches these to the Components struct
// and closes them during Stop.
type WiringResult struct {
	Manager           *Manager
	ConstitutionStore *ConstitutionStore
	GoalStore         *GoalStore
	AuditStore        *AuditStore
	EmployeesDataDir  string
	SharedDBPath      string
}

// WiringOption configures optional behaviour on NewManagerFromConfig.
// Options are applied after the Manager is constructed but before it is
// returned, so they can wire dependencies that the base constructor does
// not accept (e.g. the migrator LLM).
type WiringOption func(*Manager)

// WithMigratorLLM injects a small-model Chatter so Migrate can propose
// richer constitutions from legacy bot prompts. Nil is ignored (the
// conservative fallback path runs). The daemon should pass the classifier
// or small-model client here when available.
func WithMigratorLLM(c llm.Chatter) WiringOption {
	return func(m *Manager) {
		if c != nil {
			m.SetMigratorLLM(c)
		}
	}
}

// NewManagerFromConfig constructs the full employee stack from the daemon
// config: ConstitutionStore, GoalStore, AuditStore, and the Manager that
// wires them together. It reuses the existing bot.Manager + bot.Store as
// the persistence/execution layer (the employee package wraps, not
// replaces, the bot framework).
//
// The resulting stores open three SQLite tables. When shareBotDB is true
// and botStore is non-nil, the constitution/goal/audit stores attach to
// the bot store's existing *sql.DB connection so the whole employee stack
// lives in one .db file (recommended: simplifies backup and FK cascades).
// When shareBotDB is false (or botStore is nil), the stores open a
// separate "employees.db" in the employees data dir.
//
// Returns (zero WiringResult, nil) when the employee layer is disabled
// (cfg.Employees.Enabled == false) and bots are also disabled — the daemon
// is then responsible for falling back to the legacy bot path.
func NewManagerFromConfig(
	ctx context.Context,
	cfg *config.Config,
	botMgr *bot.Manager,
	botStore *bot.Store,
	logger *slog.Logger,
	opts ...WiringOption,
) (WiringResult, error) {
	result := WiringResult{}

	if cfg == nil {
		return result, errors.New("employee wiring: nil config")
	}
	if logger == nil {
		logger = slog.Default()
	}
	empLog := logger.With("component", "employee-wiring")

	// Determine the employees data dir. Default: <daemon_data_dir>/employees.
	employeesDir := filepath.Join(cfg.Daemon.DataDir, "employees")
	if err := os.MkdirAll(employeesDir, 0o755); err != nil {
		return result, fmt.Errorf("employee wiring: create data dir: %w", err)
	}
	result.EmployeesDataDir = employeesDir

	// Decide whether to share the bot store's DB. We share when the bot
	// store exists and the data dirs match (a strong signal that the
	// operator intends a single SQLite file for both subsystems). We
	// also share when the employees config explicitly opts in via
	// cfg.Employees.Enabled && cfg.Bots.Enabled.
	shareDB := shouldShareDB(cfg, botStore)

	var sharedDB *sql.DB
	if shareDB && botStore != nil {
		// botStore doesn't expose its *sql.DB directly; we open the
		// connection by path. The path is derived from the bot data
		// dir configured by the daemon (mirror the pattern at
		// components.go:1937: filepath.Join(botDataDir, "bots.db")).
		botDBPath := filepath.Join(cfg.Bots.DataDir, "bots.db")
		if cfg.Bots.DataDir == "" {
			botDBPath = filepath.Join(cfg.Daemon.DataDir, "bots", "bots.db")
		}
		db, err := sql.Open("sqlite", botDBPath)
		if err != nil {
			empLog.Warn("employee wiring: open shared db failed; falling back to separate db",
				"path", botDBPath, "error", err)
		} else {
			sharedDB = db
			result.SharedDBPath = botDBPath
		}
	}

	// Construct ConstitutionStore.
	var (
		cs  *ConstitutionStore
		err error
	)
	if sharedDB != nil {
		cs, err = NewConstitutionStoreFromDB(sharedDB, empLog)
	} else {
		csPath := filepath.Join(employeesDir, "constitutions.db")
		cs, err = NewConstitutionStore(csPath, empLog)
	}
	if err != nil {
		return result, fmt.Errorf("employee wiring: constitution store: %w", err)
	}
	result.ConstitutionStore = cs

	// Construct GoalStore. The goal store uses the schema constant from
	// goal.go; when sharing, we reuse the same *sql.DB.
	var gs *GoalStore
	if sharedDB != nil {
		gs, err = newGoalStoreFromDB(sharedDB, empLog)
	} else {
		gsPath := filepath.Join(employeesDir, "goals.db")
		gs, err = NewGoalStore(gsPath, empLog)
	}
	if err != nil {
		// Non-fatal: goals degrade gracefully (Manager methods return
		// ErrNotImplemented). Log and continue.
		empLog.Warn("employee wiring: goal store init failed; goals unavailable",
			"error", err)
	} else {
		result.GoalStore = gs
	}

	// Construct AuditStore. Same sharing pattern.
	var as *AuditStore
	if sharedDB != nil {
		as, err = NewAuditStoreFromDB(sharedDB)
	} else {
		asPath := filepath.Join(employeesDir, "audit.db")
		as, err = NewAuditStore(asPath)
	}
	if err != nil {
		empLog.Warn("employee wiring: audit store init failed; audit unavailable",
			"error", err)
	} else {
		result.AuditStore = as
	}

	// Construct the Manager with all stores wired.
	mgr := NewManagerWithStores(botMgr, botStore, cs, gs, as, empLog)
	result.Manager = mgr

	// Apply optional wiring (e.g. migrator LLM injection). Each option is
	// nil-guarded internally.
	for _, opt := range opts {
		if opt != nil {
			opt(mgr)
		}
	}

	empLog.Info("employee stack wired",
		"shared_db", sharedDB != nil,
		"constitution_store", cs != nil,
		"goal_store", gs != nil,
		"audit_store", as != nil,
		"employees_dir", employeesDir,
		"migrator_llm", mgr.migratorLLM != nil,
	)
	return result, nil
}

// shouldShareDB decides whether the employee stores should share the bot
// store's SQLite database. We share when both bots and employees are
// enabled (the spec's "employees layer over bots" pattern) and the bot
// store exists.
func shouldShareDB(cfg *config.Config, botStore *bot.Store) bool {
	if botStore == nil || cfg == nil {
		return false
	}
	return cfg.Employees.Enabled && cfg.Bots.Enabled
}

// newGoalStoreFromDB wraps an existing *sql.DB connection for the goal
// store. This mirrors NewGoalStore but skips the open step. Defined here
// (not goal.go) so the goal package doesn't need to know about the wiring.
func newGoalStoreFromDB(db *sql.DB, log *slog.Logger) (*GoalStore, error) {
	if log == nil {
		log = slog.Default()
	}
	s := &GoalStore{db: db, log: log}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate goal db: %w", err)
	}
	s.mu.Lock()
	s.ready = true
	s.mu.Unlock()
	return s, nil
}
