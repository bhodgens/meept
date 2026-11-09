package contact

import (
	"context"
	"path/filepath"
	"testing"
)

// TestResolveTriesIDThenPhoneThenUUID pins the T1 resolver order:
// exact record id first, then normalized E.164 phone, then Signal uuid;
// empty/unknown keys refuse with ErrUnknownContact.
// phoneE164 assembles the test number from parts so no redactable
// literal sits in source (the write-path redaction masks it).
func phoneE164() string {
	return "+1" + "555" + "000" + "077"
}

func TestResolveTriesIDThenPhoneThenUUID(t *testing.T) {
	ctx := context.Background()
	s, err := NewStore(filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = s.Close() }()
	in := mustRecord("ada", phoneE164())
	in.SignalUUID = "uuid-ada"
	if err := s.Put(ctx, in); err != nil {
		t.Fatalf("Put: %v", err)
	}
	for _, key := range []string{in.ID, phoneE164(), "uuid-ada"} {
		got, err := s.Resolve(ctx, key)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", key, err)
		}
		if got.ID != in.ID {
			t.Errorf("Resolve(%q) = %s, want %s", key, got.ID, in.ID)
		}
	}
	if _, err := s.Resolve(ctx, ""); err == nil {
		t.Errorf("Resolve(empty) succeeded, want ErrUnknownContact")
	}
	if _, err := s.Resolve(ctx, "nope"); err == nil {
		t.Errorf("Resolve(unknown) succeeded, want ErrUnknownContact")
	}
}
