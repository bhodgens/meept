package constants

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadOrGenerateDevKey_HonorsMeeptHome proves the per-installation dev
// key is read from $MEEPT_HOME/dev_key when MEEPT_HOME is set, matching
// internal/config.MeeptHome(). A GUI built by the installer writes the key
// under $MEEPT_HOME; the daemon must read the same file or auth fails (418).
func TestLoadOrGenerateDevKey_HonorsMeeptHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvMeeptHome, home)

	want := "deadbeefcafef00d0123456789abcdef"
	writeDevKey(t, filepath.Join(home, devKeyFileName), want)

	if got := loadOrGenerateDevKey(); got != want {
		t.Fatalf("dev key = %q, want %q read from $MEEPT_HOME", got, want)
	}
}

// TestLoadOrGenerateDevKey_HomeFallback proves that with MEEPT_HOME unset the
// key is read from $HOME/.meept/dev_key.
func TestLoadOrGenerateDevKey_HomeFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvMeeptHome, "") // empty == unset for MeeptHome()
	t.Setenv("HOME", home)

	want := "0123456789abcdeffedcba9876543210"
	writeDevKey(t, filepath.Join(home, DefaultHomeRel, devKeyFileName), want)

	if got := loadOrGenerateDevKey(); got != want {
		t.Fatalf("dev key = %q, want %q read from $HOME/.meept", got, want)
	}
}

// TestDevKeyDir_OverrideWinsOverHome proves the override takes priority and is
// not shadowed by HOME.
func TestDevKeyDir_OverrideWinsOverHome(t *testing.T) {
	override := t.TempDir()
	otherHome := t.TempDir()
	t.Setenv(EnvMeeptHome, override)
	t.Setenv("HOME", otherHome)

	if got := devKeyDir(); got != override {
		t.Fatalf("devKeyDir() = %q, want $MEEPT_HOME %q", got, override)
	}
}

// TestLoadOrGenerateDevKey_GeneratesInMeeptHome proves a missing key file is
// created under $MEEPT_HOME (not $HOME/.meept).
func TestLoadOrGenerateDevKey_GeneratesInMeeptHome(t *testing.T) {
	home := t.TempDir()
	otherHome := t.TempDir()
	t.Setenv(EnvMeeptHome, home)
	t.Setenv("HOME", otherHome)

	got := loadOrGenerateDevKey()
	if got == "" {
		t.Fatal("generated key is empty")
	}

	raw, err := os.ReadFile(filepath.Join(home, devKeyFileName))
	if err != nil {
		t.Fatalf("generated key not written under $MEEPT_HOME: %v", err)
	}
	if string(raw) != got {
		t.Fatalf("persisted key %q != returned key %q", string(raw), got)
	}
	if _, err := os.Stat(filepath.Join(otherHome, DefaultHomeRel, devKeyFileName)); err == nil {
		t.Fatal("key unexpectedly written under $HOME/.meept while MEEPT_HOME was set")
	}
}

func writeDevKey(t *testing.T, path, key string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(key), 0o600); err != nil {
		t.Fatal(err)
	}
}
