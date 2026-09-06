package employee

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/auditlog"
)

func TestHandleAuditVerify_OKAndBroken(t *testing.T) {
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
	for i := 0; i < 3; i++ {
		if _, err := chainStore.Append(ctx, auditlog.Record{
			Type: "audit_finding", EmployeeID: "e",
			Payload: map[string]any{"i": i},
		}); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	// The production path threads the chain DB through the Manager (the
	// daemon constructs NewRPCHandler with only the Manager, so a direct
	// RPCHandler field would require touching internal/daemon — out of
	// scope per the leaf's implementer note).
	m := NewManagerWithStores(nil, nil, nil, nil, nil, nil)
	m.SetAuditChain(chainStore.ChainDB())
	h := NewRPCHandler(m)
	raw, err := json.Marshal(map[string]any{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := h.handleAuditVerify(ctx, raw)
	if err != nil {
		t.Fatalf("handleAuditVerify: %v", err)
	}
	res, ok := out.(auditlog.VerifyResult)
	if !ok {
		t.Fatalf("wrong result type %T", out)
	}
	if !res.OK || res.Records != 3 {
		t.Fatalf("verify: %+v", res)
	}

	// Tamper → broken.
	if _, err := chainDB.Exec(`UPDATE audit_log_chain SET payload='{}' WHERE seq=2`); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	out, err = h.handleAuditVerify(ctx, raw)
	if err != nil {
		t.Fatalf("handleAuditVerify tampered: %v", err)
	}
	res = out.(auditlog.VerifyResult)
	if res.OK || res.BrokenAt != 2 {
		t.Fatalf("tampered: %+v", res)
	}
}

func TestHandleAuditVerify_NotConfigured(t *testing.T) {
	h := NewRPCHandler(nil)
	_, err := h.handleAuditVerify(context.Background(), json.RawMessage(`{}`))
	if err != errNotConfigured {
		t.Fatalf("nil manager: want errNotConfigured, got %v", err)
	}
	// A non-nil manager without a wired chain handle is also not configured.
	m := NewManager(nil)
	h2 := NewRPCHandler(m)
	_, err = h2.handleAuditVerify(context.Background(), json.RawMessage(`{}`))
	if err != errNotConfigured {
		t.Fatalf("nil chain: want errNotConfigured, got %v", err)
	}
}

func TestHandleAuditVerify_Registered(t *testing.T) {
	h := NewRPCHandler(nil)
	if _, ok := h.Handlers()["agents.audit.verify"]; !ok {
		t.Fatal("agents.audit.verify not registered")
	}
}
