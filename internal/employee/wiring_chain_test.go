package employee

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/auditlog"
)

func TestWiringChainStore_AttachedToAuditStore(t *testing.T) {
	// Direct construction parity: OpenStoreFromDB over a shared handle must
	// produce a working emitter end-to-end (finding → chain row → verify).
	chainDB, err := sql.Open("sqlite",
		filepath.Join(t.TempDir(), "chain.db")+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer chainDB.Close()
	ctx := context.Background()
	if err := auditlog.Migrate(ctx, chainDB); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	chainStore, err := auditlog.OpenStoreFromDB(chainDB, nil)
	if err != nil {
		t.Fatalf("OpenStoreFromDB: %v", err)
	}
	defer chainStore.Close()

	store, err := NewAuditStore(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	defer store.Close()
	store.SetChainEmitter(chainStore)

	f := AuditFinding{
		EmployeeID: "e1",
		Severity:   SeverityInfo,
		Checkpoint: CheckpointPostTurn,
	}
	if err := store.Create(ctx, f); err != nil {
		t.Fatalf("Create: %v", err)
	}
	res, err := auditlog.VerifyChain(ctx, chainDB)
	if err != nil || !res.OK || res.Records != 1 {
		t.Fatalf("chain after finding: %+v err=%v", res, err)
	}
	rec, ok, err := chainStore.Head(ctx)
	if err != nil || !ok || rec.Type != "audit_finding" {
		t.Fatalf("head: ok=%v err=%v rec=%+v", ok, err, rec)
	}
}
