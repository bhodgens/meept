package auditlog

import (
	"context"
	"testing"
)

func TestStoreEmit_AppendsRecord(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s, err := OpenStoreFromDB(db, nil)
	if err != nil {
		t.Fatalf("OpenStoreFromDB: %v", err)
	}
	defer s.Close()

	var ce interface{ Emit(Record) error } = s // structural check
	if err := ce.Emit(Record{
		Type: "audit_finding", EmployeeID: "e1",
		Payload: SanitizePayload(map[string]any{"finding_id": "f-1"}),
	}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	res, err := VerifyChain(ctx, db)
	if err != nil || !res.OK || res.Records != 1 {
		t.Fatalf("verify after Emit: %+v err=%v", res, err)
	}
}
