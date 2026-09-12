package daemon

import "testing"

// TestStartupOrphanSweepWithoutManagerIsNoOp pins the precondition of the
// sweep: it reads endpoint configs off the runtime manager, so a daemon built
// without components (tests, partial init) must return instead of panicking.
func TestStartupOrphanSweepWithoutManagerIsNoOp(t *testing.T) {
	d := &Daemon{}
	d.StartupOrphanSweep()
}
