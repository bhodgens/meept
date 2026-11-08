package contact

// Gate tests for the C1 Store CRUD surface beyond the shape pins in
// types_test.go (00T-gate-tests.md, T1 second file). RED until leaf 01
// lands store.go. Keep each test contract-shaped: Put/Get/GetByPhone/
// GetBySignalUUID/List/Delete + ErrUnknownContact + E.164-at-write.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// TestStoreUpdatedAtBumpedOnSecondPut: rewriting a record refreshes
// UpdatedAt (C1 field semantics) without changing identity.
func TestStoreUpdatedAtBumpedOnSecondPut(t *testing.T) {
	ctx := context.Background()
	s, err := NewStore(filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	in := mustRecord("dan", "+15550000006")
	if err := s.Put(ctx, in); err != nil {
		t.Fatalf("Put 1: %v", err)
	}
	first, err := s.Get(ctx, in.ID)
	if err != nil {
		t.Fatalf("Get 1: %v", err)
	}
	time.Sleep(5 * time.Millisecond) // monotonic-ish separation on RFC3339 TEXT columns
	first.DisplayName = "danQ"
	if err := s.Put(ctx, first); err != nil {
		t.Fatalf("Put 2: %v", err)
	}
	got, err := s.Get(ctx, in.ID)
	if err != nil {
		t.Fatalf("Get 2: %v", err)
	}
	if got.DisplayName != "danQ" {
		t.Errorf("second Put did not overwrite DisplayName: %+v", got)
	}
	if got.CreatedAt.After(got.UpdatedAt) {
		t.Errorf("CreatedAt %v after UpdatedAt %v (C1 timestamps inverted)", got.CreatedAt, got.UpdatedAt)
	}
}

// TestStorePolicyEmptyMeansDefault pins the C1 rule: Policy empty =
// employee default; the store must round-trip a nil/empty policy without
// injecting one.
func TestStorePolicyEmptyMeansDefault(t *testing.T) {
	ctx := context.Background()
	s, err := NewStore(filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	rec := mustRecord("erin", "+15550000007")
	rec.Policy = nil
	if err := s.Put(ctx, rec); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(ctx, rec.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Policy) != 0 {
		t.Errorf("Policy round-trip = %v, want empty (empty = employee default, C1)", got.Policy)
	}
}

// TestStoreDeleteUnknownIsError pins Delete on an absent id surfacing an
// error (ErrUnknownContact-family), never a silent success.
func TestStoreDeleteUnknownIsError(t *testing.T) {
	ctx := context.Background()
	s, err := NewStore(filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Delete(ctx, "no-such-id"); !errors.Is(err, ErrUnknownContact) {
		t.Errorf("Delete(unknown) = %v, want ErrUnknownContact", err)
	}
}

// TestStoreListEmpty pins List on a fresh store returning an empty slice,
// not an error.
func TestStoreListEmpty(t *testing.T) {
	ctx := context.Background()
	s, err := NewStore(filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	all, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("fresh List = %d records, want 0", len(all))
	}
}

// TestStoreGetByPhoneUnknown pins GetByPhone on an absent number as
// ErrUnknownContact.
func TestStoreGetByPhoneUnknown(t *testing.T) {
	ctx := context.Background()
	s, err := NewStore(filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if _, err := s.GetByPhone(ctx, "+19999999999"); !errors.Is(err, ErrUnknownContact) {
		t.Errorf("GetByPhone(unknown) = %v, want ErrUnknownContact", err)
	}
}
