package config

// Tests for the config-sync exclusion guard on the env resolver script
// (<meept home>/env, see internal/llm/envscript.go). Sentinel content only —
// the guard is about the FILENAME, never real keys.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeRefusesEnvScript(t *testing.T) {
	tmpDir := t.TempDir()
	nodeID := "fake"

	// A shared checkout that tries to ship an executable env script.
	sharedDir := filepath.Join(tmpDir, "checkout", "config", "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The merger walks only .json5/.toml today, so also verify the guard at
	// the applyConfigFile layer with the literal name `env`.
	m := NewMerger(tmpDir, filepath.Join(tmpDir, "checkout"), nodeID, &testLogger{})

	src := filepath.Join(sharedDir, "env")
	if err := os.WriteFile(src, []byte("#!/bin/sh\necho MEEPT_SENTINEL\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmpDir, "env")
	result := &MergeResult{FilesApplied: []string{}, FilesSkipped: []string{}}

	if err := m.applyConfigFile(src, dst, result); err != nil {
		t.Fatalf("guard must refuse (skip), not error: %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Error("env script was written to the meept home; sync refusal failed")
	}
	found := false
	for _, f := range result.FilesSkipped {
		if f == EnvScriptName {
			found = true
		}
	}
	if !found {
		t.Errorf("env script not recorded in FilesSkipped: %v", result.FilesSkipped)
	}
}

func TestMergeSharedEnvScriptNeverLands(t *testing.T) {
	// Even if the walked extension set ever grows to include extensionless
	// files, a shared file named `env` must never reach the meept home.
	tmpDir := t.TempDir()
	sharedDir := filepath.Join(tmpDir, "checkout", "config", "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sharedDir, "env"),
		[]byte("#!/bin/sh\necho MEEPT_SENTINEL\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	// A real config so the merge pass itself runs.
	if err := os.WriteFile(filepath.Join(sharedDir, "meept.json5"),
		[]byte(`{"daemon": {"log_level": "info"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewMerger(tmpDir, filepath.Join(tmpDir, "checkout"), "fake", &testLogger{})
	result, err := m.Merge("abc123")
	if err != nil {
		t.Fatal(err)
	}

	for _, applied := range result.FilesApplied {
		if applied == EnvScriptName {
			t.Error("env script applied by shared merge")
		}
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "env")); !os.IsNotExist(err) {
		t.Error("env script exists in the meept home after merge")
	}
}

func TestEnvScriptNameConstantMatchesLLM(t *testing.T) {
	// Both packages declare the name because internal/config cannot import
	// internal/llm; this pins them together.
	if EnvScriptName != "env" {
		t.Errorf("EnvScriptName = %q, want env", EnvScriptName)
	}
	if strings.ContainsAny(EnvScriptName, "/\\") {
		t.Errorf("EnvScriptName must be a bare filename, got %q", EnvScriptName)
	}
}
