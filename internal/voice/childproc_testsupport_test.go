package voice

// Extra test-only plumbing for ChildProc: the locked log buffer, the pid
// accessor shim, and small helpers kept out of the main test file for
// readability.

import (
	"bytes"
	"fmt"
	"sync"
)

// lockedBuffer is a mutex-guarded bytes.Buffer for concurrent log writes
// (the RSS logger runs on other goroutines).
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// currentPIDForTest reads the live child pid (test-only accessor).
func (p *ChildProc) currentPIDForTest() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currentPIDLocked()
}

// fmtSscanfInt parses one integer.
func fmtSscanfInt(s string, out *int) (int, error) {
	return fmt.Sscanf(s, "%d", out)
}
