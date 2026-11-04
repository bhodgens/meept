package voicechild

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
)

// captureLogger is a thread-safe slog sink capturing everything logged,
// for the no-key-in-log audits.
type captureLogger struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func newCaptureLogger() *captureLogger { return &captureLogger{} }

// logger returns an slog.Logger writing into the capture buffer.
func (c *captureLogger) logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&captureWriter{c: c}, nil))
}

// contains reports whether s has been logged so far.
func (c *captureLogger) contains(s string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Contains(c.buf.String(), s)
}

// everything returns the full captured log text.
func (c *captureLogger) everything() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// captureWriter serializes writes into the captureLogger.
type captureWriter struct{ c *captureLogger }

func (w *captureWriter) Write(p []byte) (int, error) {
	w.c.mu.Lock()
	defer w.c.mu.Unlock()
	return w.c.buf.Write(p)
}
