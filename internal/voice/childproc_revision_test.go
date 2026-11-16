package voice

// Pins the voice-child revision gate: a stale meept-voice must not be
// spawned beside a newer daemon. The comparer is pure so the mismatch
// and empty-revision cases do not need two real binaries.

import (
	"errors"
	"strings"
	"testing"
)

// TestChildRevisionMismatch pins the refusal: two non-empty, unequal
// revision strings are a named error that carries both sides.
func TestChildRevisionMismatch(t *testing.T) {
	const (
		daemonRev = "deadbeefcafebabe000000000000000000000001"
		childRev  = "994c3c445e510000000000000000000000000002"
	)
	err := compareChildRevisions(daemonRev, childRev)
	var mismatch *ChildRevisionMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("compareChildRevisions(%q, %q) = %v, want *ChildRevisionMismatchError", daemonRev, childRev, err)
	}
	if mismatch.Daemon != daemonRev || mismatch.Child != childRev {
		t.Fatalf("mismatch fields = daemon %q child %q, want %q and %q", mismatch.Daemon, mismatch.Child, daemonRev, childRev)
	}
	msg := err.Error()
	if !strings.Contains(msg, daemonRev) || !strings.Contains(msg, childRev) {
		t.Fatalf("error %q does not include both revisions", msg)
	}
}

// TestChildRevisionEmptyAllow pins the allow: an empty revision on either
// side (test stub, dirty build with no vcs) must not refuse. Equal
// non-empty revisions are the same binary and must not refuse either.
func TestChildRevisionEmptyAllow(t *testing.T) {
	const rev = "994c3c445e510000000000000000000000000002"
	cases := []struct {
		name   string
		daemon string
		child  string
	}{
		{name: "both empty", daemon: "", child: ""},
		{name: "daemon empty", daemon: "", child: rev},
		{name: "child empty", daemon: rev, child: ""},
		{name: "equal", daemon: rev, child: rev},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := compareChildRevisions(tc.daemon, tc.child); err != nil {
				t.Fatalf("compareChildRevisions(%q, %q) = %v, want nil", tc.daemon, tc.child, err)
			}
		})
	}
}
