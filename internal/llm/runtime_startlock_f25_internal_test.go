package llm

// Internal tests pinning audit finding F25: the start-lock create-then-
// deschedule window. A lock file that exists but does not yet carry its owner
// pid (the creator was descheduled between O_EXCL create and WriteString)
// must NOT be treated as stale while fresh — a second meept process that
// judged it stale would remove it and spawn a duplicate onto the occupied
// endpoint. And releaseStartLock must verify it still owns the file before
// unlinking it, so a stale-takeover's replacement lock is never removed.

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestStaleStartLock_FreshEmptyLockIsNotStale simulates the create-then-
// deschedule window: an EMPTY lock file whose mtime is right now (the creator
// has created it but not yet written its pid) must be reported NOT stale, so
// the waiter keeps waiting instead of removing the file and spawning a
// duplicate.
func TestStaleStartLock_FreshEmptyLockIsNotStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.pid.lock")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatalf("write fresh empty lock: %v", err)
	}
	if staleStartLock(path) {
		t.Error("a fresh empty lock (create-then-deschedule window) must NOT be stale (F25)")
	}

	// Same for an unparseable content: fresh means "maybe being written",
	// never "abandoned".
	if err := os.WriteFile(path, []byte("not-a-pid"), 0o600); err != nil {
		t.Fatalf("write fresh unparseable lock: %v", err)
	}
	if staleStartLock(path) {
		t.Error("a fresh unparseable lock must NOT be stale (F25)")
	}
}

// TestStaleStartLock_OldEmptyLockIsStale pins the other half: an empty lock
// OLDER than the fresh grace is a leftover of a dead creator and must be
// taken over, or one crashed start would block the endpoint forever.
func TestStaleStartLock_OldEmptyLockIsStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.pid.lock")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatalf("write empty lock: %v", err)
	}
	old := time.Now().Add(-startLockFreshGrace - time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("backdate lock: %v", err)
	}
	if !staleStartLock(path) {
		t.Error("an empty lock older than the fresh grace MUST be stale (F25)")
	}
}

// TestStaleStartLock_FreshLiveOwnerIsNotStale makes sure the grace did not
// weaken the normal path: a fresh lock whose owner pid is written and alive
// is not stale (unchanged behaviour, and a fresh lock with a DEAD owner is
// still stale immediately — a crashed creator must not block the endpoint for
// the grace period).
func TestStaleStartLock_FreshLiveOwnerIsNotStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.pid.lock")
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatalf("write fresh lock: %v", err)
	}
	if staleStartLock(path) {
		t.Error("a fresh lock with a live owner must NOT be stale")
	}

	if err := os.WriteFile(path, []byte("999999999"), 0o600); err != nil {
		t.Fatalf("write fresh dead-owner lock: %v", err)
	}
	if !staleStartLock(path) {
		t.Error("a fresh lock with a DEAD owner must still be stale immediately")
	}
}

// TestReleaseStartLock_VerifiesOwnership pins the guarded unlink: a release
// whose lock file was REPLACED by another holder's lock (stale takeover) must
// leave the replacement alone.
func TestReleaseStartLock_VerifiesOwnership(t *testing.T) {
	t.Run("own lock is removed", func(t *testing.T) {
		pidFile := filepath.Join(t.TempDir(), "runtime.pid")
		release, err := acquireStartLock(pidFile)
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}
		path := startLockPath(pidFile)
		release()
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Errorf("own lock must be removed at release, stat err = %v", statErr)
		}
	})

	t.Run("another holder's lock is left alone", func(t *testing.T) {
		pidFile := filepath.Join(t.TempDir(), "runtime.pid")
		release, err := acquireStartLock(pidFile)
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}
		path := startLockPath(pidFile)

		// Simulate a stale takeover by another meept process: our lock
		// file was removed and recreated carrying THAT process's pid.
		if err := os.Remove(path); err != nil {
			t.Fatalf("remove own lock (takeover): %v", err)
		}
		foreign := 424242 // not this test process
		if err := os.WriteFile(path, []byte(strconv.Itoa(foreign)), 0o600); err != nil {
			t.Fatalf("write foreign lock: %v", err)
		}

		release()

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal("release must NOT remove another holder's lock file (F25)")
		}
		if string(data) != strconv.Itoa(foreign) {
			t.Errorf("foreign lock content = %q, want %q (untouched)", data, strconv.Itoa(foreign))
		}
		// Clean up the simulated foreign lock.
		_ = os.Remove(path)
	})

	t.Run("replaced-by-fresh-empty lock is left alone", func(t *testing.T) {
		pidFile := filepath.Join(t.TempDir(), "runtime.pid")
		release, err := acquireStartLock(pidFile)
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}
		path := startLockPath(pidFile)

		// The replacement is an EMPTY fresh lock (its creator is between
		// create and pid-write): ours is gone, so we own nothing.
		if err := os.Remove(path); err != nil {
			t.Fatalf("remove own lock (takeover): %v", err)
		}
		if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
			t.Fatalf("write fresh empty lock: %v", err)
		}

		release()

		if _, statErr := os.Stat(path); statErr != nil {
			t.Errorf("release must NOT remove a fresh replacement lock it does not own, stat err = %v", statErr)
		}
		_ = os.Remove(path)
	})
}
