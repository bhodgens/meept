package auditlog

import (
	"context"
	"database/sql"
	"testing"
)

// chainOfThree builds a verified 3-record chain in a fresh DB and returns
// the handle (tests tamper directly via SQL).
func chainOfThree(t *testing.T) *sql.DB {
	t.Helper()
	db := openTestDB(t)
	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s, err := OpenStoreFromDB(db, nil)
	if err != nil {
		t.Fatalf("OpenStoreFromDB: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	prev := ""
	for i := uint64(1); i <= 3; i++ {
		got, err := s.Append(ctx, rec(i, prev, "audit_finding"))
		if err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		prev = got.RecordHash
	}
	return db
}
