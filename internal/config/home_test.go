package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMeeptHomeDefault(t *testing.T) {
	t.Setenv(EnvMeeptHome, "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	want := filepath.Join(home, ".meept")
	if got := MeeptHome(); got != want {
		t.Errorf("MeeptHome() = %q, want %q", got, want)
	}
}

func TestMeeptHomeOverride(t *testing.T) {
	t.Setenv(EnvMeeptHome, "/tmp/meept-alt")
	if got := MeeptHome(); got != "/tmp/meept-alt" {
		t.Errorf("MeeptHome() = %q, want /tmp/meept-alt", got)
	}
	if got := MeeptPath("skills"); got != "/tmp/meept-alt/skills" {
		t.Errorf("MeeptPath(skills) = %q, want /tmp/meept-alt/skills", got)
	}
}

func TestMeeptHomeOverrideTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	t.Setenv(EnvMeeptHome, "~/.meept-test")
	want := filepath.Join(home, ".meept-test")
	if got := MeeptHome(); got != want {
		t.Errorf("MeeptHome() = %q, want %q", got, want)
	}
}

func TestMeeptHomeEmptyOverrideFallsBack(t *testing.T) {
	// Empty MEEPT_HOME must fall back to $HOME/.meept, not become "".
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	t.Setenv(EnvMeeptHome, "")
	want := filepath.Join(home, ".meept")
	if got := MeeptHome(); got != want {
		t.Errorf("MeeptHome() = %q, want %q", got, want)
	}
}
