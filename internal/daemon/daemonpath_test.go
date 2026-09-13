package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestDaemonPath covers Contract C
// (docs/plans/20260905-dependency-visibility/master.md, issue #32):
// the meept dependency prefix first, then inherited PATH, then guaranteed
// dirs appended if absent, deduped, pure string assembly.
func TestDaemonPath(t *testing.T) {
	t.Run("meept deps prefix first, inherited PATH after it", func(t *testing.T) {
		t.Setenv("MEEPT_HOME", "")
		t.Setenv("HOME", "/home/tester")
		t.Setenv("PATH", "/custom/dir")
		got := DaemonPath()
		wantPrefix := "/home/tester/.meept/deps/llama.cpp/bin:" +
			"/home/tester/.meept/deps/llama.cpp/build/bin:/custom/dir:"
		if !strings.HasPrefix(got, wantPrefix) {
			t.Fatalf("expected result to start with %q, got %q", wantPrefix, got)
		}
	})

	t.Run("guaranteed dirs appended in order after prefix and inherited", func(t *testing.T) {
		t.Setenv("MEEPT_HOME", "")
		t.Setenv("HOME", "/home/tester")
		t.Setenv("PATH", "/custom/dir")
		want := "/home/tester/.meept/deps/llama.cpp/bin:" +
			"/home/tester/.meept/deps/llama.cpp/build/bin:" +
			"/custom/dir:" +
			"/opt/homebrew/bin:/usr/local/bin:" +
			"/home/tester/.local/bin:/home/tester/go/bin:/home/tester/.cargo/bin:" +
			"/usr/bin:/bin:/usr/sbin:/sbin"
		if got := DaemonPath(); got != want {
			t.Fatalf("DaemonPath() = %q, want %q", got, want)
		}
	})

	t.Run("dedup: guaranteed dir already inherited is not repeated", func(t *testing.T) {
		t.Setenv("MEEPT_HOME", "")
		t.Setenv("HOME", "/home/tester")
		t.Setenv("PATH", "/opt/homebrew/bin:/usr/bin:/custom")
		got := DaemonPath()
		if strings.Count(got, "/opt/homebrew/bin") != 1 {
			t.Fatalf("expected /opt/homebrew/bin exactly once, got %q", got)
		}
		if strings.Count(got, "/usr/bin:") != 1 && !strings.HasSuffix(got, "/usr/bin") {
			t.Fatalf("expected /usr/bin exactly once, got %q", got)
		}
		// Inherited order must stay intact behind the prefix.
		if !strings.HasPrefix(got, "/home/tester/.meept/deps/llama.cpp/bin:"+
			"/home/tester/.meept/deps/llama.cpp/build/bin:"+
			"/opt/homebrew/bin:/usr/bin:/custom:") {
			t.Fatalf("expected prefix then inherited segment preserved in order, got %q", got)
		}
	})

	t.Run("empty inherited PATH yields exactly the guarantee list", func(t *testing.T) {
		t.Setenv("MEEPT_HOME", "")
		t.Setenv("HOME", "/home/tester")
		t.Setenv("PATH", "")
		want := "/home/tester/.meept/deps/llama.cpp/bin:" +
			"/home/tester/.meept/deps/llama.cpp/build/bin:" +
			"/opt/homebrew/bin:/usr/local/bin:" +
			"/home/tester/.local/bin:/home/tester/go/bin:/home/tester/.cargo/bin:" +
			"/usr/bin:/bin:/usr/sbin:/sbin"
		if got := DaemonPath(); got != want {
			t.Fatalf("DaemonPath() = %q, want %q", got, want)
		}
	})

	t.Run("HOME expansion in guaranteed dirs", func(t *testing.T) {
		t.Setenv("MEEPT_HOME", "")
		t.Setenv("HOME", "/Users/someone")
		t.Setenv("PATH", "")
		got := DaemonPath()
		for _, want := range []string{
			"/Users/someone/.local/bin",
			"/Users/someone/go/bin",
			"/Users/someone/.cargo/bin",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("expected %q in result, got %q", want, got)
			}
		}
	})

	t.Run("deterministic: pure function of env", func(t *testing.T) {
		t.Setenv("MEEPT_HOME", "")
		t.Setenv("HOME", "/home/tester")
		t.Setenv("PATH", "/custom:/opt/homebrew/bin")
		first, second := DaemonPath(), DaemonPath()
		if first != second {
			t.Fatalf("DaemonPath not deterministic: %q vs %q", first, second)
		}
	})

	// MEEPT_HOME overrides the prefix location, so a dev/test prefix wins
	// over the default ~/.meept one.
	t.Run("MEEPT_HOME override moves the prefix dir", func(t *testing.T) {
		t.Setenv("MEEPT_HOME", "/opt/meept-home")
		t.Setenv("HOME", "/home/tester")
		t.Setenv("PATH", "/usr/bin")
		got := DaemonPath()
		wantPrefix := "/opt/meept-home/deps/llama.cpp/bin:" +
			"/opt/meept-home/deps/llama.cpp/build/bin:/usr/bin:"
		if !strings.HasPrefix(got, wantPrefix) {
			t.Fatalf("expected %q prefix, got %q", wantPrefix, got)
		}
		if strings.Contains(got, "/home/tester/.meept/deps") {
			t.Fatalf("expected no default prefix when MEEPT_HOME is set, got %q", got)
		}
	})
}

// TestDaemonPathPrefersMeeptLlamaServer is the precedence guarantee for the
// LFM2.5 tool-call floor: a llama-server installed into the meept dependency
// prefix must resolve before a Homebrew one, because Homebrew's build
// predates llama.cpp's LFM2.5 native tool-call parser (build floor enforced
// by `make deps-llama-check`).
func TestDaemonPathPrefersMeeptLlamaServer(t *testing.T) {
	// Both layouts the prefix can hold must win; bin/ wins over build/bin/.
	for _, layout := range []string{"bin", "build/bin"} {
		t.Run("layout "+layout, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("MEEPT_HOME", home)
			t.Setenv("PATH", "/opt/homebrew/bin:/usr/bin:/bin")

			prefixBin := filepath.Join(home, "deps", "llama.cpp", filepath.FromSlash(layout))
			if err := os.MkdirAll(prefixBin, 0o755); err != nil {
				t.Fatalf("mkdir prefix bin: %v", err)
			}
			// A stand-in for the meept-managed binary: executable, never run here.
			prefixed := filepath.Join(prefixBin, "llama-server")
			if err := os.WriteFile(prefixed, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatalf("write fake llama-server: %v", err)
			}

			got := DaemonPath()
			if strings.Index(got, prefixBin) > strings.Index(got, "/opt/homebrew/bin") {
				t.Fatalf("meept prefix %q must precede /opt/homebrew/bin, got %q", prefixBin, got)
			}

			// Functional check: PATH resolution picks the prefixed binary.
			t.Setenv("PATH", got)
			resolved, err := exec.LookPath("llama-server")
			if err != nil {
				t.Fatalf("LookPath(llama-server) with DaemonPath() = %v (path %q)", err, got)
			}
			if resolved != prefixed {
				t.Fatalf("LookPath(llama-server) = %q, want the meept prefix %q", resolved, prefixed)
			}
		})
	}

	t.Run("bin wins over build/bin when both exist", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("MEEPT_HOME", home)
		t.Setenv("PATH", "/opt/homebrew/bin")

		prefix := filepath.Join(home, "deps", "llama.cpp")
		for _, layout := range []string{"bin", "build/bin"} {
			dir := filepath.Join(prefix, filepath.FromSlash(layout))
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", dir, err)
			}
			if err := os.WriteFile(filepath.Join(dir, "llama-server"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatalf("write fake llama-server: %v", err)
			}
		}

		t.Setenv("PATH", DaemonPath())
		resolved, err := exec.LookPath("llama-server")
		if err != nil {
			t.Fatalf("LookPath(llama-server) = %v", err)
		}
		if want := filepath.Join(prefix, "bin", "llama-server"); resolved != want {
			t.Fatalf("LookPath(llama-server) = %q, want installed layout %q", resolved, want)
		}
	})

	t.Run("empty prefix dirs are inert (inherited PATH untouched)", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("MEEPT_HOME", home)
		t.Setenv("PATH", "/opt/homebrew/bin:/usr/bin:/bin")
		got := DaemonPath()
		// Nothing lives in the prefix, so the inherited PATH is what resolves;
		// it must survive intact and in order right behind the two prefix
		// entries (guaranteed dirs follow it).
		head := filepath.Join(home, "deps", "llama.cpp", "bin") + ":" +
			filepath.Join(home, "deps", "llama.cpp", "build", "bin") +
			":/opt/homebrew/bin:/usr/bin:/bin:"
		if !strings.HasPrefix(got, head) {
			t.Fatalf("expected %q prefix, got %q", head, got)
		}
	})
}
