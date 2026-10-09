// Package logrotate provides a size-capped writer for the daemon log file.
//
// Why this exists: on 2026-10-08 a runaway task-creation loop drove the daemon
// log to 145 GB and filled a 927 GB disk to 95%. Both writers that open
// $MEEPT_HOME/meept.log used a bare os.OpenFile(O_APPEND), so nothing ever
// capped or reclaimed it:
//
//	cmd/meept/daemon.go:171
//	internal/services/daemon_service.go:149
//
// CapSize wraps such a file so the log can never exceed a configured byte
// budget. When the file grows past the cap the writer rotates: the current file
// is renamed to <name>.1 (replacing any previous .1) and a fresh file is
// created in its place. One backup generation is kept, so the steady-state disk
// cost is at most 2x the cap rather than unbounded.
//
// CapSize is not safe for concurrent use by multiple writers; each spawning site
// owns its own instance. A single os.File write never interleaves, so the
// rename is the only window and it is guarded by mu.
package logrotate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// DefaultMaxBytes is the cap applied when a caller passes MaxBytes <= 0. It is
// deliberately far below a typical free-disk floor: a single runaway must not be
// able to consume the machine.
const DefaultMaxBytes int64 = 256 << 20 // 256 MiB

// maxBackups is how many rotated generations are retained alongside the live
// file. Each retained generation costs at most MaxBytes, so the worst-case
// on-disk cost is (maxBackups+1) x MaxBytes.
const maxBackups = 2

// CapSize is an io.WriteCloser that rotates its backing file at MaxBytes.
// A zero or negative MaxBytes selects DefaultMaxBytes.
type CapSize struct {
	path     string
	maxBytes int64

	mu sync.Mutex
	f  *os.File
}

// New opens path for appending and returns a size-capped writer. The parent
// directory must already exist; this does not create it, matching the behaviour
// of the os.OpenFile call sites this replaces.
func New(path string, maxBytes int64) (*CapSize, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	c := &CapSize{path: path, maxBytes: maxBytes}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	c.f = f
	return c, nil
}

// Write implements io.Writer. It rotates before writing whenever the backing
// file has reached MaxBytes, so a single write can exceed the cap by at most one
// Write's worth of bytes.
func (c *CapSize) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.f == nil {
		return 0, os.ErrClosed
	}
	if fi, err := c.f.Stat(); err == nil && fi.Size()+int64(len(p)) > c.maxBytes {
		if err := c.rotateLocked(); err != nil {
			return 0, err
		}
	}
	return c.f.Write(p)
}

// rotateLocked renames the live file to <path>.1 and opens a fresh <path>.
// Caller must hold c.mu.
func (c *CapSize) rotateLocked() error {
	if err := c.f.Close(); err != nil {
		return fmt.Errorf("logrotate: close before rotate: %w", err)
	}
	// Shift the retained generations down before installing a new .1, so bytes
	// older than the newest two generations are NOT destroyed. A plain rename
	// onto .1 would silently discard the previous .1 on every rotation, which
	// loses history (caught by TestCapSizePreservesContentAcrossRotate: 8
	// rotations over a 512-byte cap kept 656 of 2624 bytes).
	for i := maxBackups - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", c.path, i)
		dst := fmt.Sprintf("%s.%d", c.path, i+1)
		if _, err := os.Stat(src); err != nil {
			continue // nothing to shift
		}
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("logrotate: shift %s -> %s: %w", src, dst, err)
		}
	}
	if err := os.Rename(c.path, c.path+".1"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("logrotate: rename %s: %w", c.path, err)
	}
	f, err := os.OpenFile(c.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("logrotate: reopen %s: %w", c.path, err)
	}
	c.f = f
	return nil
}

// Close closes the underlying file. It is idempotent.
func (c *CapSize) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.f == nil {
		return nil
	}
	err := c.f.Close()
	c.f = nil
	return err
}

// Size reports the current size of the live log file in bytes. It returns an
// error only when the file cannot be stat'ed.
func (c *CapSize) Size() (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.f == nil {
		return 0, os.ErrClosed
	}
	fi, err := c.f.Stat()
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// RotatedBackup returns the path of the single retained backup generation.
func (c *CapSize) RotatedBackup() string { return c.path + ".1" }

var _ io.WriteCloser = (*CapSize)(nil)
