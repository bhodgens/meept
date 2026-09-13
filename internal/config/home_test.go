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

func TestExpandMeeptPathRedirectsMeeptPrefix(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(EnvMeeptHome, tmp)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"home itself", "~/.meept", tmp},
		{"repomap cache", "~/.meept/repomap_cache", filepath.Join(tmp, "repomap_cache")},
		{"nested", "~/.meept/a/b", filepath.Join(tmp, "a", "b")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExpandMeeptPath(tc.in); got != tc.want {
				t.Errorf("ExpandMeeptPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestExpandMeeptPathNonMeeptPathsUnchanged(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	tmp := t.TempDir()
	t.Setenv(EnvMeeptHome, tmp)

	// A non-meept ~ path still expands to the real home, not the override.
	if got, want := ExpandMeeptPath("~/other/dir"), filepath.Join(home, "other", "dir"); got != want {
		t.Errorf("ExpandMeeptPath(~/other/dir) = %q, want %q", got, want)
	}
	// A sibling directory that merely starts with "meept" is not the meept
	// home and expands to the real home too.
	if got, want := ExpandMeeptPath("~/meeptx/cache"), filepath.Join(home, "meeptx", "cache"); got != want {
		t.Errorf("ExpandMeeptPath(~/meeptx/cache) = %q, want %q", got, want)
	}
	// Absolute and relative paths pass through untouched.
	for _, p := range []string{"/abs/path", "rel/path", ""} {
		if got := ExpandMeeptPath(p); got != p {
			t.Errorf("ExpandMeeptPath(%q) = %q, want unchanged", p, got)
		}
	}
}

func TestExpandMeeptPathDefaultIsOperatorHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	// MEEPT_HOME unset: behavior must be byte-identical to plain ~ expansion.
	t.Setenv(EnvMeeptHome, "")
	want := filepath.Join(home, ".meept", "repomap_cache")
	if got := ExpandMeeptPath("~/.meept/repomap_cache"); got != want {
		t.Errorf("ExpandMeeptPath() = %q, want %q (default unchanged)", got, want)
	}
}
