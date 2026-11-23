//go:build windows

package voice

import (
	"log/slog"
	"os"

	"golang.org/x/sys/windows"
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
	ol := &windows.Overlapped{}
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		if err := windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol); err != nil {
			slog.Warn("voice daily counter: unlock failed", "path", path, "error", err)
		}
		if err := f.Close(); err != nil {
			slog.Warn("voice daily counter: lock file close failed", "path", path, "error", err)
		}
	}, nil
}
