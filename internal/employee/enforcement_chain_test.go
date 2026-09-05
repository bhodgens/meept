package employee

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/auditlog"
)

// recordingEmitter captures Emit calls without needing a real chain DB.
type recordingEmitter struct {
	mu     sync.Mutex
	called int
	last   auditlog.Record
	err    error
}

func (r *recordingEmitter) Emit(rec auditlog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.called++
	r.last = rec
	return r.err
}

func TestAuditStore_CreateChainsFinding(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	store, err := NewAuditStore(dbPath)
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	defer store.Close()

	chainDB, err := sql.Open("sqlite",
		filepath.Join(t.TempDir(), "chain.db")+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open chain db: %v", err)
	}
	defer chainDB.Close()
	if err := auditlog.Migrate(context.Background(), chainDB); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	em := &recordingEmitter{}
	store.SetChainEmitter(em)
	store.SetChainEmitter(nil) // nil guard: must not clear em

	f := AuditFinding{
		EmployeeID: "e1",
		Severity:   SeverityCritical,
		Checkpoint: CheckpointPostTurn,
		Evidence:   "violation",
		DetectedAt: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
	}
	if err := store.Create(context.Background(), f); err != nil {
		t.Fatalf("Create: %v", err)
	}
	em.mu.Lock()
	defer em.mu.Unlock()
	if em.called != 1 {
		t.Fatalf("Emit called %d times, want 1", em.called)
	}
	if em.last.Type != "audit_finding" || em.last.EmployeeID != "e1" {
		t.Fatalf("record fields: %+v", em.last)
	}
	fid, ok := em.last.Payload["finding_id"].(string)
	if !ok || fid == "" {
		t.Fatalf("finding_id missing: %+v", em.last.Payload)
	}
	if em.last.At.IsZero() {
		t.Fatal("At must be stamped")
	}
}

func TestAuditStore_EmitFailureDoesNotFailCreate(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	store, err := NewAuditStore(dbPath)
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	defer store.Close()

	store.SetChainEmitter(&recordingEmitter{err: errBoom})
	f := AuditFinding{EmployeeID: "e1", Severity: SeverityInfo, Checkpoint: CheckpointPostTurn}
	if err := store.Create(context.Background(), f); err != nil {
		t.Fatalf("Create must succeed despite Emit failure: %v", err)
	}
}

func TestAuditStore_ChainDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	store, err := NewAuditStore(dbPath)
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	defer store.Close()
	if store.ChainDB() == nil {
		t.Fatal("ChainDB must return the underlying handle")
	}
}

var errBoom = errorString("boom")

type errorString string

func (e errorString) Error() string { return string(e) }
