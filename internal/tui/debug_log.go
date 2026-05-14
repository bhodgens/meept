// Package tui provides the terminal user interface for meept.
package tui

import (
	"fmt"
	"os"
)

var debugCounter int

// DebugLog writes debug messages to /tmp/meept_slash.log
func DebugLog(msg string) {
	//nolint:gosec // user config directory/file permissions
	f, err := os.OpenFile("/tmp/meept_slash.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		debugCounter++
		fmt.Fprintf(f, "[%d] %s\n", debugCounter, msg)
		f.Close()
	}
}
