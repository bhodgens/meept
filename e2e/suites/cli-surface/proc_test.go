//go:build e2e

package cisurface

import (
	"os"
	"syscall"
)

// findProcess wraps os.FindProcess for the dead-daemon probe.
func findProcess(pid int) (*os.Process, error) {
	return os.FindProcess(pid)
}

// killProcess SIGKILLs the process (crash simulation: no graceful socket
// cleanup).
func killProcess(p *os.Process) error {
	return p.Signal(syscall.SIGKILL)
}
