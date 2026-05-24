package configui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

func TestAtomicWriteJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json5")

	data := map[string]string{"key": "value"}
	err := WriteConfigFile(path, data)
	if err != nil {
		t.Fatalf("WriteConfigFile: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["key"] != "value" {
		t.Errorf("expected value, got %s", got["key"])
	}
}

func TestAtomicWriteCreatesDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "test.json5")

	err := WriteConfigFile(path, map[string]string{"k": "v"})
	if err != nil {
		t.Fatalf("WriteConfigFile with nested dir: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("file should exist")
	}
}

func TestAtomicWritePreservesPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.json5")

	err := WriteConfigFile(path, map[string]string{"key": "secret"})
	if err != nil {
		t.Fatalf("WriteConfigFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected 0600 permissions, got %o", info.Mode().Perm())
	}
}

func TestWriteMainConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")

	cfg := config.DefaultConfig()
	cfg.Daemon.LogLevel = "debug"

	err := WriteConfigFile(path, cfg)
	if err != nil {
		t.Fatalf("WriteConfigFile: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var got config.Config
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Daemon.LogLevel != "debug" {
		t.Errorf("expected debug, got %s", got.Daemon.LogLevel)
	}
}

func TestConfigFilePath(t *testing.T) {
	// Verify ConfigFilePath returns the expected paths
	p := ConfigFilePath("meept.json5")
	if p == "" {
		t.Error("expected non-empty path")
	}
}
