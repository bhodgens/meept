package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadPID_JSONRuntimePidfile guards against the strconv.Atoi regression:
// the daemon writes runtime pidfiles as {"pid":N,"token":"..."}, and a bare
// int parse misread every live pidfile as stale. The CLI then removed it and
// spawned a duplicate onto the occupied endpoint port (start), or failed with
// "invalid PID in file" (status/stop). readPID routes through
// llm.ParsePIDFile so the CLI and daemon can never drift on the format.
func TestReadPID_JSONRuntimePidfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.pid")
	if err := os.WriteFile(path, []byte(`{"pid":4321,"token":"deadbeef"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	pid, err := readPID(path)
	if err != nil {
		t.Fatalf("readPID(JSON pidfile) error = %v, want nil", err)
	}
	if pid != 4321 {
		t.Fatalf("readPID(JSON pidfile) = %d, want 4321", pid)
	}
}

// TestReadPID_LegacyBareInt keeps the pre-token pidfile format working.
func TestReadPID_LegacyBareInt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.pid")
	if err := os.WriteFile(path, []byte("6789\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pid, err := readPID(path)
	if err != nil {
		t.Fatalf("readPID(legacy pidfile) error = %v, want nil", err)
	}
	if pid != 6789 {
		t.Fatalf("readPID(legacy pidfile) = %d, want 6789", pid)
	}
}

// TestReadPID_Malformed errors rather than returning a bogus pid.
func TestReadPID_Malformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.pid")
	if err := os.WriteFile(path, []byte("not-a-pid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPID(path); err == nil {
		t.Fatal("readPID(malformed pidfile) error = nil, want error")
	}
}

// TestReadPID_Missing returns the underlying not-exist error so callers can
// distinguish "no pidfile" (not running) from "unparseable pidfile".
func TestReadPID_Missing(t *testing.T) {
	_, err := readPID(filepath.Join(t.TempDir(), "absent.pid"))
	if !os.IsNotExist(err) {
		t.Fatalf("readPID(missing pidfile) error = %v, want a not-exist error", err)
	}
}
