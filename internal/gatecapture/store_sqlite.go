package gatecapture

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	//nolint:revive // sqlite3 driver registration side effect
	_ "modernc.org/sqlite"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/migrate"
)

// llmScoreResult aliases the leaf-01 score result type for the Recorder seam:
// the alias keeps internal/gatecapture's seam identical in shape to
// llm.CommandScoreResult without duplicating the struct (the jevgate.Recorder
// interface references the llm type directly; the alias satisfies it).
type llmScoreResult = llm.CommandScoreResult

// gateDB is the sqlite half of the capture store (mirror of the shadow
// store_sqlite.go conventions: modernc driver, WAL, central migrate registry,
// MkdirAll on the parent).
type gateDB struct {
	db *sql.DB
}

// errInvalidChokepoint / errInvalidDecision reject rows outside the frozen
// C4 vocabularies - a bad row is a caller bug, not silent garbage in the
// training corpus.
func errInvalidChokepoint(v string) error {
	return fmt.Errorf("gatecapture: invalid chokepoint %q (want %q or %q)",
		v, ChokepointDecisionGateDeny, ChokepointPermissionTableAsk)
}

func errInvalidDecision(v string) error {
	return fmt.Errorf("gatecapture: invalid human decision %q (want %q or %q)",
		v, DecisionAllowed, DecisionDenied)
}

// captureSchemaV1 is the v1 baseline DDL: one table + two indexes.
const captureSchemaV1SQL = `
	CREATE TABLE IF NOT EXISTS command_gate_records (
		id TEXT PRIMARY KEY,
		ts TEXT NOT NULL,
		chokepoint TEXT NOT NULL,
		command TEXT NOT NULL,
		context_blob TEXT NOT NULL,
		prior_score REAL NOT NULL DEFAULT 0.0,
		prior_confidence REAL NOT NULL DEFAULT 0.0,
		prior_degraded INTEGER NOT NULL DEFAULT 0,
		human_decision TEXT NOT NULL,
		approver TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_command_gate_records_ts ON command_gate_records(ts);
	CREATE INDEX IF NOT EXISTS idx_command_gate_records_chokepoint ON command_gate_records(chokepoint);
`

// CaptureStoreSchemaVersion mirrors the shadow store's downgrade-detection
// diagnostic constant (the registry ladder's target).
const CaptureStoreSchemaVersion = 1

// captureMigrations is the declared schema history for the capture database
// (central internal/migrate registry, same contract as the shadow store).
var captureMigrations = func() *migrate.Registry {
	r, err := migrate.NewRegistry("gatecapture/store", []migrate.Migration{
		{
			Version: 1,
			Name:    "baseline command_gate_records",
			SQL:     captureSchemaV1SQL,
		},
	})
	if err != nil {
		panic(fmt.Sprintf("gatecapture: migration registry: %v", err))
	}
	return r
}()

// openGateDB opens and migrates the sqlite capture DB.
func openGateDB(dbPath string) (*gateDB, error) {
	// Command-gate capture rows can carry secret-bearing argv: the DB
	// lives under a user-only directory with owner-only file modes
	// (bughunt 2026-11-06 security F-1; sqlite's default create mode is
	// 0644 = world-readable).
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("gatecapture: failed to create directory: %w", err)
	}
	if f, err := os.OpenFile(dbPath, os.O_CREATE|os.O_RDWR, 0o600); err == nil {
		_ = f.Close() //nolint:errcheck // existence+mode pin; sql.Open re-opens
	} else if !os.IsExist(err) {
		return nil, fmt.Errorf("gatecapture: failed to create database file: %w", err)
	}
	// An existing file (created by an older 0644 build) is tightened here;
	// WAL side files are best-effort.
	_ = os.Chmod(dbPath, 0o600)        //nolint:errcheck // best-effort tighten
	_ = os.Chmod(dbPath+"-wal", 0o600) //nolint:errcheck // best-effort tighten

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("gatecapture: failed to open database: %w", err)
	}

	g := &gateDB{db: db}
	if err := captureMigrations.Apply(db, nil); err != nil {
		db.Close()
		return nil, fmt.Errorf("gatecapture: failed to apply schema migrations: %w", err)
	}
	return g, nil
}

// insert writes one row. The caller has already validated the vocabularies
// and stamped ID/Ts (Store.Record).
func (g *gateDB) insert(_ context.Context, rec *CommandGateRecord) error {
	degraded := 0
	if rec.PriorDegraded {
		degraded = 1
	}
	_, err := g.db.Exec(`
		INSERT INTO command_gate_records (
			id, ts, chokepoint, command, context_blob,
			prior_score, prior_confidence, prior_degraded,
			human_decision, approver
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		rec.ID,
		rec.Ts.UTC().Format(time.RFC3339Nano),
		rec.Chokepoint,
		rec.Command,
		rec.ContextBlob,
		rec.PriorScore,
		rec.PriorConfidence,
		degraded,
		rec.HumanDecision,
		rec.Approver,
	)
	if err != nil {
		return fmt.Errorf("gatecapture: insert record: %w", err)
	}
	return nil
}

// list returns records newest-first under the filters.
func (g *gateDB) list(_ context.Context, opts ListOptions) ([]CommandGateRecord, error) {
	q := `SELECT id, ts, chokepoint, command, context_blob,
	     prior_score, prior_confidence, prior_degraded,
	     human_decision, approver
      FROM command_gate_records`
	// Filter fragments are compile-time constants joined with WHERE/AND;
	// all user-influenced values bind through args (gosec G202).
	var args []any
	var conds []string
	if opts.Chokepoint != "" {
		conds = append(conds, "chokepoint = ?")
		args = append(args, opts.Chokepoint)
	}
	if opts.Since != nil {
		conds = append(conds, "ts >= ?")
		args = append(args, opts.Since.UTC().Format(time.RFC3339Nano))
	}
	if len(conds) > 0 {
		// #nosec G202 -- conditions use parameterized ? placeholders; dynamic WHERE is safe
		q += " WHERE " + strings.Join(conds, " AND ") // #nosec G202 -- conditions use parameterized ? placeholders; dynamic WHERE is safe
	}
	q += " ORDER BY ts DESC, id DESC"
	if opts.Limit > 0 {
		q += " LIMIT ? OFFSET ?"
		args = append(args, opts.Limit, opts.Offset)
	} else if opts.Offset > 0 {
		q += " OFFSET ?"
		args = append(args, opts.Offset)
	}

	rows, err := g.db.Query(q, args...)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("gatecapture: list records: %w", err)
	}
	defer rows.Close()

	out := make([]CommandGateRecord, 0)
	for rows.Next() {
		var rec CommandGateRecord
		var tsStr string
		var degraded int
		if err := rows.Scan(
			&rec.ID, &tsStr, &rec.Chokepoint, &rec.Command, &rec.ContextBlob,
			&rec.PriorScore, &rec.PriorConfidence, &degraded,
			&rec.HumanDecision, &rec.Approver,
		); err != nil {
			return nil, fmt.Errorf("gatecapture: scan record: %w", err)
		}
		ts, err := time.Parse(time.RFC3339Nano, tsStr)
		if err != nil {
			return nil, fmt.Errorf("gatecapture: parse ts %q: %w", tsStr, err)
		}
		rec.Ts = ts
		rec.PriorDegraded = degraded != 0
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("gatecapture: list rows: %w", err)
	}
	return out, nil
}

// close closes the handle.
func (g *gateDB) close() error {
	if g == nil || g.db == nil {
		return nil
	}
	return g.db.Close()
}
