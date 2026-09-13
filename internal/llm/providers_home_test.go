package llm

import (
	"os"
	"path/filepath"
	"testing"
)

// userModelsConfigPath must honor MEEPT_HOME. Before 2026-09-12 it did not:
// LoadProvidersConfigDefault used os.UserHomeDir() directly, so a daemon
// started with MEEPT_HOME=<dir> still loaded the operator's
// ~/.meept/models.json5 and spawned that file's runtimes (observed: a
// MEEPT_HOME=/tmp rig brought up the operator's prompt-router sidecar).
func TestUserModelsConfigPath_HonorsMeeptHome(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "models.json5")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write models.json5: %v", err)
	}
	t.Setenv("MEEPT_HOME", home)

	got, ok := userModelsConfigPath()
	if !ok {
		t.Fatal("userModelsConfigPath reported no config; want the MEEPT_HOME file")
	}
	if got != path {
		t.Errorf("path = %q, want %q", got, path)
	}
}

// A MEEPT_HOME without models.json5 must NOT fall back to the operator's
// ~/.meept/models.json5 - that fallback is what leaked the operator's
// providers into isolated rigs.
func TestUserModelsConfigPath_NoFallbackToOperatorHome(t *testing.T) {
	operatorHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(operatorHome, ".meept"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	operatorCfg := filepath.Join(operatorHome, ".meept", "models.json5")
	if err := os.WriteFile(operatorCfg, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write operator models.json5: %v", err)
	}
	t.Setenv("HOME", operatorHome)
	t.Setenv("MEEPT_HOME", t.TempDir()) // empty dir: no models.json5

	if got, ok := userModelsConfigPath(); ok {
		t.Errorf("userModelsConfigPath = %q, want no config when MEEPT_HOME has none", got)
	}
}

// Without MEEPT_HOME the operator home is the correct source.
func TestUserModelsConfigPath_FallsBackToHomeDir(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".meept"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(home, ".meept", "models.json5")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write models.json5: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("MEEPT_HOME", "")

	got, ok := userModelsConfigPath()
	if !ok {
		t.Fatal("userModelsConfigPath reported no config; want ~/.meept/models.json5")
	}
	if got != path {
		t.Errorf("path = %q, want %q", got, path)
	}
}

// A whitespace-only MEEPT_HOME is not a home directory; it must be treated as
// unset rather than resolving to a relative "models.json5".
func TestUserModelsConfigPath_BlankMeeptHomeIsUnset(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".meept"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(home, ".meept", "models.json5")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write models.json5: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("MEEPT_HOME", "   ")

	got, ok := userModelsConfigPath()
	if !ok {
		t.Fatal("blank MEEPT_HOME must be treated as unset, not as no config")
	}
	if got != path {
		t.Errorf("path = %q, want %q", got, path)
	}
}
