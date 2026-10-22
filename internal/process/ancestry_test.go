package process

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

// supportedPlatform reports whether ParentPID has a real implementation
// (linux, darwin) versus the always-fail stub (everywhere else).
func supportedPlatform() bool {
	switch runtime.GOOS {
	case "linux", "darwin":
		return true
	default:
		return false
	}
}

// TestParentPID_Self pins the platform primitive on a live process: our own
// PID's parent must resolve and equal os.Getppid().
func TestParentPID_Self(t *testing.T) {
	if !supportedPlatform() {
		t.Skip("ParentPID unsupported on " + runtime.GOOS)
	}
	ppid, ok := ParentPID(os.Getpid())
	if !ok {
		t.Fatal("ParentPID(self) failed on a supported platform")
	}
	if want := os.Getppid(); ppid != want {
		t.Fatalf("ParentPID(self) = %d, want os.Getppid() = %d", ppid, want)
	}
}

// TestParentPID_Dead pins the miss path: a PID that does not exist reports
// (0, false) rather than a stale or zero-parented match. Uses a
// spawned-then-reaped child so the PID is provably dead (no magic number:
// Linux pid_max defaults allow 7-digit PIDs to exist).
func TestParentPID_Dead(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start probe child: %v", err)
	}
	dead := cmd.Process.Pid
	if err := cmd.Wait(); err != nil { // reaps: /proc/<pid> is gone
		t.Fatalf("wait probe child: %v", err)
	}
	if ppid, ok := ParentPID(dead); ok && ppid > 0 {
		t.Fatalf("ParentPID(reaped %d) = (%d, true), want (0, false)", dead, ppid)
	}
	if IsDescendantOf(dead, os.Getppid()) {
		t.Fatalf("IsDescendantOf(reaped %d, parent) = true, want false", dead)
	}
}

// TestIsDescendantOf_Chain pins the walk on the real process tree:
// self -> parent -> ... -> init. Self is a descendant of its parent and of
// its grandparent (when resolvable); the parent is NOT a descendant of self.
func TestIsDescendantOf_Chain(t *testing.T) {
	self := os.Getpid()
	parent := os.Getppid()
	if !IsDescendantOf(self, parent) {
		if _, ok := ParentPID(self); !ok {
			t.Skip("ParentPID unsupported on this platform")
		}
		t.Fatalf("IsDescendantOf(self=%d, parent=%d) = false, want true", self, parent)
	}
	if IsDescendantOf(parent, self) {
		t.Fatalf("IsDescendantOf(parent=%d, self=%d) = true, want false (no cycles)", parent, self)
	}
	if gp, ok := ParentPID(parent); ok && gp > 1 {
		if !IsDescendantOf(self, gp) {
			t.Fatalf("IsDescendantOf(self, grandparent=%d) = false, want true", gp)
		}
	}
}

// TestIsDescendantOf_Edges pins the guard rails: PID 1 is never an
// "ancestor" answer (walk terminator), non-positive inputs are rejected,
// and a dead PID is not a descendant of anything.
func TestIsDescendantOf_Edges(t *testing.T) {
	self := os.Getpid()
	if IsDescendantOf(self, 1) {
		t.Error("IsDescendantOf(self, 1) = true, want false (init terminates the walk)")
	}
	if IsDescendantOf(self, 0) || IsDescendantOf(self, -5) {
		t.Error("non-positive ancestor must be false")
	}
	if IsDescendantOf(0, self) || IsDescendantOf(-1, self) {
		t.Error("non-positive pid must be false")
	}
}
