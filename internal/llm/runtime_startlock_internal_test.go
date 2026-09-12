package llm

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAcquireStartLock_TakesOverStaleLock pins the self-healing rule: a lock
// whose recorded owner is gone must not block a later start forever.
func TestAcquireStartLock_TakesOverStaleLock(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "runtime.pid")
	path := startLockPath(pidFile)
	// Pid 999999 is not running: the lock is a leftover of a crashed process.
	if err := os.WriteFile(path, []byte("999999"), 0o600); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}

	release, err := acquireStartLock(pidFile)
	if err != nil {
		t.Fatalf("stale lock must be taken over, got: %v", err)
	}
	release()

	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("release must remove the lock file, stat err = %v", statErr)
	}
}

// TestAcquireStartLock_BlocksWhileHeld pins the serialization: while one
// acquisition holds the endpoint lock, a second one must not succeed, so two
// meept processes cannot both pass the duplicate-spawn probe.
func TestAcquireStartLock_BlocksWhileHeld(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "runtime.pid")
	release, err := acquireStartLock(pidFile)
	if err != nil {
		t.Fatalf("first acquisition: %v", err)
	}
	defer release()

	acquired := make(chan struct{})
	go func() {
		// The second acquisition blocks until the first releases; the channel
		// closes only if it ever succeeds.
		secondRelease, secondErr := acquireStartLock(pidFile)
		if secondErr == nil {
			secondRelease()
			close(acquired)
		}
	}()

	select {
	case <-acquired:
		t.Fatal("second acquisition succeeded while the lock was held")
	case <-time.After(300 * time.Millisecond):
		// Still blocked, which is the point.
	}
}

// TestStartLockSkippedWithoutPIDFile pins the no-key path: a runtime config with
// no PID file cannot be keyed, so the lock is skipped rather than guessed.
func TestStartLockSkippedWithoutPIDFile(t *testing.T) {
	if path := startLockPath(""); path != "" {
		t.Errorf("startLockPath(\"\") = %q, want empty", path)
	}
	release, err := acquireStartLock("")
	if err != nil {
		t.Fatalf("acquireStartLock(\"\") must be a no-op, got: %v", err)
	}
	release()
}
