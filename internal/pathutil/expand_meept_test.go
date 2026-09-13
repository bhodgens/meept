package pathutil

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExpandMeeptPath_HonorsMeeptHome pins audit finding F15: a
// config-supplied path under the shipped "~/.meept" prefix is redirected under
// MEEPT_HOME, so a rig's runtime pid file/records land in the rig home, not the
// operator's real ~/.meept.
func TestExpandMeeptPath_HonorsMeeptHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)

	cases := map[string]string{
		"~/.meept/run/llama.pid": filepath.Join(home, "run", "llama.pid"),
		"~/.meept":               home,
		"~/.meept/meept.json5":   filepath.Join(home, "meept.json5"),
	}
	for in, want := range cases {
		if got := ExpandMeeptPath(in); got != want {
			t.Errorf("ExpandMeeptPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestExpandPath_IgnoresMeeptHome pins the public contract that must NOT change:
// ExpandPath always means the real user home, even when MEEPT_HOME is set.
func TestExpandPath_IgnoresMeeptHome(t *testing.T) {
	t.Setenv("MEEPT_HOME", t.TempDir())
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	want := filepath.Join(realHome, ".meept", "run", "llama.pid")
	if got := ExpandPath("~/.meept/run/llama.pid"); got != want {
		t.Errorf("ExpandPath must keep meaning the real home: got %q, want %q", got, want)
	}
}

// TestExpandMeeptPath_UnsetIsIdenticalToExpandPath pins that the default install
// is byte-identical: with MEEPT_HOME unset, ExpandMeeptPath == ExpandPath.
func TestExpandMeeptPath_UnsetIsIdenticalToExpandPath(t *testing.T) {
	t.Setenv("MEEPT_HOME", "")
	for _, in := range []string{"~/.meept/run/x.pid", "~/other/x", "/abs/path.pid", "relative/path.pid"} {
		if got, want := ExpandMeeptPath(in), ExpandPath(in); got != want {
			t.Errorf("ExpandMeeptPath(%q) = %q, want ExpandPath's %q", in, got, want)
		}
	}
}

// TestExpandMeeptPath_NonMeeptTildeUsesRealHome pins that only the meept home
// prefix is redirected: a plain "~" path still means the real home.
func TestExpandMeeptPath_NonMeeptTildeUsesRealHome(t *testing.T) {
	t.Setenv("MEEPT_HOME", t.TempDir())
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	if got, want := ExpandMeeptPath("~/other/x"), filepath.Join(realHome, "other", "x"); got != want {
		t.Errorf("a non-meept ~ path must expand to the real home: got %q, want %q", got, want)
	}
}
