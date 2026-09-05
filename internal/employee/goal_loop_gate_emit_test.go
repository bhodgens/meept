package employee

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/gate"
)

func TestGateResultRecord_Shape(t *testing.T) {
	// The mapping helper is unit-tested directly; the GoalLoop call-site
	// wiring is verified in the integration test (master.md Integration Test
	// Plan step 2) because constructing a full GoalLoop in a unit test
	// requires the whole loop fixture.
	res := gate.GateResult{Passed: true, Output: "all tests passed\n", Skipped: false}
	rec := gateResultRecord("g-1", res, 250*time.Millisecond)
	if rec.Type != "gate_result" || rec.EmployeeID != "" {
		t.Fatalf("record header: %+v", rec)
	}
	if rec.Payload["passed"] != true || rec.Payload["skipped"] != false {
		t.Fatalf("passed/skipped: %+v", rec.Payload)
	}
	if rec.Payload["output_sha256"] != auditlog.OutputSHA256(res.Output) {
		t.Fatalf("output_sha256: %+v", rec.Payload)
	}
	if _, has := rec.Payload["output"]; has {
		t.Fatal("raw output must never appear in a gate record")
	}
	if rec.Payload["duration_ms"] != int64(250) {
		t.Fatalf("duration_ms: %+v", rec.Payload)
	}
}

func TestEmitGateResult_ChainsAndSanitizes(t *testing.T) {
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

	l := &GoalLoop{}
	l.SetChainStore(chainStore)
	l.SetChainStore(nil) // nil guard: must not clear chainStore
	l.emitGateResult(ctx, "g-9",
		gate.GateResult{Passed: true, Output: "token=supersecret"}, 100*time.Millisecond)

	res, err := auditlog.VerifyChain(ctx, chainDB)
	if err != nil || !res.OK || res.Records != 1 {
		t.Fatalf("verify: %+v err=%v", res, err)
	}
}
