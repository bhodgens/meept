package tools

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// The bare "no path specified" told the model nothing about why a filesystem
// call could not proceed: it retried the identical call and the cycle guard
// aborted the turn (fresh-rig daemon11, 2026-09-13). These tests pin the
// replacement: an actionable message naming the missing context, plus a
// distinct sentinel the caller can detect.

func TestNoWorkingDirError_IsActionableAndDetectable(t *testing.T) {
	err := NoWorkingDirError("list_directory")
	if !errors.Is(err, ErrNoWorkingDir) {
		t.Fatalf("NoWorkingDirError does not wrap ErrNoWorkingDir: %v", err)
	}
	if !IsNoWorkingDir(err) {
		t.Fatalf("IsNoWorkingDir(%v) = false, want true", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "no working directory for this session; pass an explicit path") {
		t.Errorf("message %q does not tell the model what to do", msg)
	}
	if !strings.HasPrefix(msg, "list_directory:") {
		t.Errorf("message %q does not name the tool", msg)
	}
}

func TestNoWorkingDirError_EmptyToolNameReturnsSentinel(t *testing.T) {
	if err := NoWorkingDirError(""); err != ErrNoWorkingDir {
		t.Errorf("NoWorkingDirError(\"\") = %v, want the sentinel itself", err)
	}
	if IsNoWorkingDir(nil) {
		t.Error("IsNoWorkingDir(nil) = true, want false")
	}
}

func TestNoPathError_IsDetectableAndDistinctFromNoWorkingDir(t *testing.T) {
	err := NoPathError("read_file")
	if !errors.Is(err, ErrNoPath) {
		t.Fatalf("NoPathError does not wrap ErrNoPath: %v", err)
	}
	// A tool that requires an explicit path must not claim the session has no
	// working directory: that would be a false diagnosis when one is bound.
	if errors.Is(err, ErrNoWorkingDir) {
		t.Errorf("NoPathError must not be detectable as ErrNoWorkingDir: %v", err)
	}
	if !strings.Contains(err.Error(), "read_file:") {
		t.Errorf("message %q does not name the tool", err.Error())
	}
}

func TestIsNoWorkingDir_WrappedChainIsDetectable(t *testing.T) {
	// Tool errors commonly get wrapped again on the way out (e.g. the step
	// result layer); detection must survive %w wrapping.
	wrapped := fmt.Errorf("file_write failed: %w", NoWorkingDirError("write_file"))
	if !IsNoWorkingDir(wrapped) {
		t.Errorf("IsNoWorkingDir(%v) = false, want true through a wrap", wrapped)
	}
}
