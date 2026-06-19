package llm

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/caimlas/meept/internal/pathutil"
)

// maxLogSizeBytes is the per-file rotation cap (10 MB).
const maxLogSizeBytes int64 = 10 * 1024 * 1024

// ModelLogger emits structured per-model lifecycle events as JSON lines.
type ModelLogger struct {
	logger   *slog.Logger
	file     *os.File
	provider string
	model    string
	mu       sync.Mutex
}

// Log writes an event with arbitrary key/value pairs.
func (m *ModelLogger) Log(event string, kv ...any) {
	if m == nil {
		return
	}
	attrs := []any{slog.String("event", event)}
	attrs = append(attrs, kv...)
	m.mu.Lock()
	defer m.mu.Unlock()
	// Use Info level for all events; consumers filter by "event" attribute.
	m.logger.Info("runtime_event", attrs...)
}

// Close closes the underlying file if owned. Best-effort.
func (m *ModelLogger) Close() error {
	if m == nil || m.file == nil {
		return nil
	}
	m.mu.Lock()
	f := m.file
	m.file = nil
	m.mu.Unlock()
	return f.Close()
}

// OpenModelLogger opens (creating if needed) a per-model JSON-line log file at
// `~/.meept/logs/runtimes/<providerID>-<modelKey>.log`. On open failure it
// returns a logger backed by os.Stderr with a nil file so callers can proceed.
func OpenModelLogger(providerID, modelKey string) (*ModelLogger, error) {
	dir := filepath.Join(pathutil.ExpandPath("~/.meept"), "logs", "runtimes")
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.log", providerID, modelKey))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return newFallbackModelLogger(providerID, modelKey), err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return newFallbackModelLogger(providerID, modelKey), err
	}
	handler := slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler).With(
		slog.String("provider", providerID),
		slog.String("model", modelKey),
	)
	return &ModelLogger{logger: logger, file: f, provider: providerID, model: modelKey}, nil
}

func newFallbackModelLogger(providerID, modelKey string) *ModelLogger {
	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler).With(
		slog.String("provider", providerID),
		slog.String("model", modelKey),
	)
	return &ModelLogger{logger: logger, provider: providerID, model: modelKey}
}

// rotatingWriter wraps an *os.File and rotates after maxLogSizeBytes.
// Max one backup (.1) is kept; older history is discarded.
//
// When multiple rotatingWriter instances share the same underlying *os.File
// (e.g. ProcessLogger's stdout and stderr writers), they must share:
//   - the same *sync.Mutex via the mu field (concurrent-write serialization),
//   - the same **os.File (so rotation is visible to all writers), and
//   - the same *int64 written counter (so the rotation threshold is based on
//     cumulative bytes from both writers, not per-writer).
//
// Sharing the **os.File is critical: rotation closes and replaces the
// underlying file. If each writer kept its own *os.File copy, the partner
// writer would be left holding a closed fd after rotation, causing silent
// EBADF write failures (this was a real bug — see
// TestProcessLogger_StdoutStderrShareFile_AfterRotation).
type rotatingWriter struct {
	mu        *sync.Mutex
	file      **os.File // shared mutably so rotation is visible to partners
	path      string
	prefix    string
	written   *int64 // shared byte counter
	atLineEnd *bool  // shared line-state; true when the next byte starts a new line
}

// newSharedRotatingWriter creates a rotatingWriter that shares the same **os.File,
// *int64 counter, *sync.Mutex, and *bool line-state as its partner. This ensures
// that concurrent writes from stdout and stderr don't interleave, that rotation
// (which closes and reopens the file) is visible to both writers, and that the
// line-boundary state (used to decide when to emit the prefix) is consistent
// across partners.
func newSharedRotatingWriter(filePtr **os.File, writtenPtr *int64, atLineEndPtr *bool, path, prefix string, mu *sync.Mutex) *rotatingWriter {
	return &rotatingWriter{mu: mu, file: filePtr, path: path, prefix: prefix, written: writtenPtr, atLineEnd: atLineEndPtr}
}

// Truncate truncates the underlying file to zero length. Used when the manager
// spawns a fresh process (not when merging providers into a running process).
// If the truncate fails or the file is closed, this is a best-effort no-op.
func (w *rotatingWriter) Truncate() {
	if w == nil || w.file == nil || *w.file == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if *w.file == nil {
		return
	}
	if err := (*w.file).Truncate(0); err == nil {
		_, _ = (*w.file).Seek(0, io.SeekStart)
		*w.written = 0
		if w.atLineEnd != nil {
			*w.atLineEnd = true // truncated file starts at a line boundary
		}
	}
}

// Write writes p to the file with the configured line prefix. Each line
// (delimited by '\n') is prefixed on its first byte; line state is tracked
// across Write calls so a write that continues a partial line does not get a
// spurious prefix while a write that starts a new line does. The mutex
// serializes stdout/stderr partners so bytes never interleave within a line.
//
// Return value n is len(p) (the caller's input length), not the number of
// bytes physically written (which includes prefix bytes). This matches the
// io.Writer contract for callers like exec.Cmd that feed in the subprocess
// output stream. Rotation accounts for prefix bytes via *w.written.
func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil || *w.file == nil {
		// Fallback: write to stderr.
		fmt.Fprintf(os.Stderr, "%s %s", w.prefix, p)
		return len(p), nil
	}
	var buf bytes.Buffer
	buf.Grow(len(p))
	// atLineEnd is shared across stdout/stderr partners; nil-guard for
	// defense-in-depth (all production constructors set it non-nil).
	atLineStart := true
	if w.atLineEnd != nil {
		atLineStart = *w.atLineEnd // true → next byte begins a new line → prefix needed
	}
	for _, b := range p {
		if atLineStart {
			buf.WriteString(w.prefix)
			atLineStart = false
		}
		buf.WriteByte(b)
		if b == '\n' {
			atLineStart = true
		}
	}
	if w.atLineEnd != nil {
		*w.atLineEnd = atLineStart
	}
	out := buf.Bytes()
	n, err := (*w.file).Write(out)
	*w.written += int64(n)
	if *w.written > maxLogSizeBytes {
		w.rotateLocked()
	}
	return len(p), err
}

// rotateLocked performs the rotation: foo.log -> foo.log.1, then recreate foo.log.
// On any failure, falls back to truncating in place. Caller must hold w.mu.
//
// Because w.file is a shared **os.File, the replacement is visible to all
// partner writers (e.g. the stderr writer when stdout triggered rotation).
func (w *rotatingWriter) rotateLocked() {
	if w.file == nil || *w.file == nil {
		return
	}
	backup := w.path + ".1"
	// Best-effort remove of any pre-existing .1 backup before we rename the
	// current file into that slot. Ignore "does not exist" errors. The previous
	// implementation used a rename-then-remove dance; os.Remove is simpler and
	// avoids leaving a transient .deleting file behind on error paths.
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		// Non-fatal: continue; the rename below may still fail and we'll fall
		// back to truncating.
		_ = err
	}
	if err := os.Rename(w.path, backup); err != nil {
		// Can't rename (maybe cross-device or permission); fall back to truncating.
		if err := (*w.file).Truncate(0); err == nil {
			_, _ = (*w.file).Seek(0, io.SeekStart)
			*w.written = 0
			if w.atLineEnd != nil {
				*w.atLineEnd = true
			}
		}
		return
	}
	// Reopen the (now-truncated) primary file. Update the shared pointer so
	// partner writers see the new fd.
	_ = (*w.file).Close()
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		*w.file = nil
		return
	}
	*w.file = f
	*w.written = 0
	if w.atLineEnd != nil {
		*w.atLineEnd = true // rotated file starts at a line boundary
	}
}

// Close closes the underlying file. Only the ProcessLogger.Close path should
// call this, to avoid double-closing when both out/err writers share the file.
func (w *rotatingWriter) Close() error {
	if w == nil || w.file == nil || *w.file == nil {
		return nil
	}
	err := (*w.file).Close()
	*w.file = nil
	return err
}

// ProcessLogger wraps two rotatingWriter instances sharing the same file with
// different line prefixes ("out: " and "err: ").
type ProcessLogger struct {
	out *rotatingWriter
	err *rotatingWriter
}

// Stdout returns the writer for subprocess stdout.
func (p *ProcessLogger) Stdout() io.Writer {
	if p == nil {
		return os.Stdout
	}
	return p.out
}

// Stderr returns the writer for subprocess stderr.
func (p *ProcessLogger) Stderr() io.Writer {
	if p == nil {
		return os.Stderr
	}
	return p.err
}

// Truncate truncates the underlying file (called on fresh spawn).
func (p *ProcessLogger) Truncate() {
	if p == nil {
		return
	}
	p.out.Truncate()
}

// Close closes the underlying file.
func (p *ProcessLogger) Close() error {
	if p == nil {
		return nil
	}
	return p.out.Close()
}

// OpenProcessLogger opens (creating if needed) the per-process raw subprocess
// log at `~/.meept/logs/runtimes/<host>-<port>.process.log`. On failure,
// returns a logger whose writers fall back to os.Stderr.
//
// Both stdout and stderr writers share the same underlying *os.File (via a
// shared **os.File), the same *int64 byte counter, and the same *sync.Mutex.
// This ensures concurrent writes are serialized and rotation is visible to
// both writers — without the shared pointer, rotation on one writer would
// leave the partner writer holding a closed fd.
func OpenProcessLogger(host, port string) (*ProcessLogger, error) {
	dir := filepath.Join(pathutil.ExpandPath("~/.meept"), "logs", "runtimes")
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.process.log", host, port))
	sharedMu := &sync.Mutex{}
	var (
		filePtr    **os.File
		written    int64
		atLineEnd  bool = true
	)
	writtenPtr := &written
	atLineEndPtr := &atLineEnd
	if err := os.MkdirAll(dir, 0o700); err != nil {
		var nilFile *os.File
		filePtr = &nilFile
		return &ProcessLogger{
			out: newSharedRotatingWriter(filePtr, writtenPtr, atLineEndPtr, path, "out: ", sharedMu),
			err: newSharedRotatingWriter(filePtr, writtenPtr, atLineEndPtr, path, "err: ", sharedMu),
		}, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		var nilFile *os.File
		filePtr = &nilFile
		return &ProcessLogger{
			out: newSharedRotatingWriter(filePtr, writtenPtr, atLineEndPtr, path, "out: ", sharedMu),
			err: newSharedRotatingWriter(filePtr, writtenPtr, atLineEndPtr, path, "err: ", sharedMu),
		}, err
	}
	if info, statErr := f.Stat(); statErr == nil {
		written = info.Size()
		// Detect mid-line state: if the existing file is non-empty and does
		// not end with '\n', the next byte continues a line and should not be
		// prefixed. Otherwise (empty file or trailing newline) the next byte
		// starts a new line and gets the prefix.
		if written > 0 {
			if lastByte, readErr := readLastByte(f, int(written)); readErr == nil && lastByte != '\n' {
				atLineEnd = false
			}
		}
	}
	filePtr = &f
	return &ProcessLogger{
		out: newSharedRotatingWriter(filePtr, writtenPtr, atLineEndPtr, path, "out: ", sharedMu),
		err: newSharedRotatingWriter(filePtr, writtenPtr, atLineEndPtr, path, "err: ", sharedMu),
	}, nil
}

// readLastByte reads the last byte of f without loading the entire file into
// memory. size is the known file size (from Stat). It seeks to the last byte,
// reads it, then seeks back to the end so subsequent appends work correctly.
func readLastByte(f *os.File, size int) (byte, error) {
	if size <= 0 {
		return 0, fmt.Errorf("empty file")
	}
	_, err := f.Seek(int64(size-1), io.SeekStart)
	if err != nil {
		return 0, err
	}
	b := make([]byte, 1)
	_, err = f.Read(b)
	if err != nil {
		return 0, err
	}
	// Seek to end so subsequent writes append correctly.
	_, _ = f.Seek(0, io.SeekEnd)
	return b[0], nil
}
