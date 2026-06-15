// Package tui provides the terminal user interface for meept.
package tui

import (
	"fmt"
	"os"
	"sync/atomic"
)

// debugCounter is incremented atomically because DebugLog is called from
// multiple goroutines (event stream handlers, slash autocomplete, tea.Cmd closures).
var debugCounter uint64

// DebugLog writes debug messages to /tmp/meept_slash.log
func DebugLog(msg string) {
	//nolint:gosec // user config directory/file permissions
	f, err := os.OpenFile("/tmp/meept_slash.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		n := atomic.AddUint64(&debugCounter, 1)
		fmt.Fprintf(f, "[%d] %s\n", n, msg)
		f.Close()
	}
}
