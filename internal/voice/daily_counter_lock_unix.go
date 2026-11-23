//go:build !windows

package voice

import (
	"log/slog"
	"os"

	"golang.org/x/sys/unix"
)

// lockDailyCounterFile serializes read-check-write across processes for the
// durable daily counter. The caller holds NO sync.Mutex while this runs:
// file I/O must stay outside in-memory locks. The returned function releases
// the OS lock and closes the file; call it exactly once.
func lockDailyCounterFile(path string) (release func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		if err := unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil {
			slog.Warn("voice daily counter: unlock failed", "path", path, "error", err)
		}
		if err := f.Close(); err != nil {
			slog.Warn("voice daily counter: lock file close failed", "path", path, "error", err)
		}
	}, nil
}
