package validator

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Pins the L20 fix: with an empty explicit binary the filter resolves
// python3 first, then falls back to python — matching DefaultFilters,
// which adds lint_python when EITHER exists.
func TestNewPythonLintFilter_FallbackToPython(t *testing.T) {
	binDir := t.TempDir()
	name := "python"
	if runtime.GOOS == "windows" {
		name = "python.exe"
	}
	// A fake interpreter: it only needs to exist on PATH for LookPath;
	// the fallback resolution happens at construction, not execution.
	if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake python: %v", err)
	}
	t.Setenv("PATH", binDir)

	f := NewPythonLintFilter("")
	if f.pythonBin == "" || f.pythonBin == "python3" {
		t.Errorf("pythonBin = %q, want resolved python fallback in %s", f.pythonBin, binDir)
	}
}

// On a host where python3 exists, the constructor keeps python3 (no
// behavior change from the fallback logic).
func TestNewPythonLintFilter_PrefersPython3(t *testing.T) {
	binDir := t.TempDir()
	for _, n := range []string{"python3", "python"} {
		if err := os.WriteFile(filepath.Join(binDir, n), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write fake %s: %v", n, err)
		}
	}
	t.Setenv("PATH", binDir)

	f := NewPythonLintFilter("")
	if got := filepath.Base(f.pythonBin); got != "python3" {
		t.Errorf("pythonBin = %q, want python3 preferred", f.pythonBin)
	}
}

// Explicit config is never second-guessed (host override preserved).
func TestNewPythonLintFilter_ExplicitBinWins(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty PATH
	f := NewPythonLintFilter("/custom/venv/bin/python")
	if f.pythonBin != "/custom/venv/bin/python" {
		t.Errorf("pythonBin = %q, want explicit value untouched", f.pythonBin)
	}
}
