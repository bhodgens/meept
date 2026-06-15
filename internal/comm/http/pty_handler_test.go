package http

import (
	"strings"
	"testing"
)

// TestGenerateSessionID_Unpredictable checks that 1000 consecutive session
// IDs are unique and properly prefixed.
func TestGenerateSessionID_Unpredictable(t *testing.T) {
	ids := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id := generateSessionID()
		if !strings.HasPrefix(id, "pty-") {
			t.Fatalf("session id %q missing pty- prefix", id)
		}
		if _, dup := ids[id]; dup {
			t.Fatalf("duplicate session id after %d generations: %s", i, id)
		}
		ids[id] = struct{}{}
	}
}

// TestGenerateSessionID_Length verifies the ID has the expected length:
// "pty-" (4) + 32 hex chars from 16 bytes.
func TestGenerateSessionID_Length(t *testing.T) {
	id := generateSessionID()
	if len(id) != 4+32 {
		t.Errorf("session id length = %d, want %d", len(id), 4+32)
	}
}
