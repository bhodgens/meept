package daemon

import (
	"strings"
	"testing"
)

// TestDaemonPath covers Contract C
// (docs/plans/20260905-dependency-visibility/master.md, issue #32):
// inherited PATH first, then guaranteed dirs appended if absent, deduped,
// pure string assembly.
func TestDaemonPath(t *testing.T) {
	t.Run("inherited PATH preserved first", func(t *testing.T) {
		t.Setenv("HOME", "/home/tester")
		t.Setenv("PATH", "/custom/dir")
		got := DaemonPath()
		if !strings.HasPrefix(got, "/custom/dir:") {
			t.Fatalf("expected result to start with inherited PATH, got %q", got)
		}
	})

	t.Run("guaranteed dirs appended in order after inherited", func(t *testing.T) {
		t.Setenv("HOME", "/home/tester")
		t.Setenv("PATH", "/custom/dir")
		want := "/custom/dir:/opt/homebrew/bin:/usr/local/bin:" +
			"/home/tester/.local/bin:/home/tester/go/bin:/home/tester/.cargo/bin:" +
			"/usr/bin:/bin:/usr/sbin:/sbin"
		if got := DaemonPath(); got != want {
			t.Fatalf("DaemonPath() = %q, want %q", got, want)
		}
	})

	t.Run("dedup: guaranteed dir already inherited is not repeated", func(t *testing.T) {
		t.Setenv("HOME", "/home/tester")
		t.Setenv("PATH", "/opt/homebrew/bin:/usr/bin:/custom")
		got := DaemonPath()
		if strings.Count(got, "/opt/homebrew/bin") != 1 {
			t.Fatalf("expected /opt/homebrew/bin exactly once, got %q", got)
		}
		if strings.Count(got, "/usr/bin:") != 1 && !strings.HasSuffix(got, "/usr/bin") {
			t.Fatalf("expected /usr/bin exactly once, got %q", got)
		}
		// Inherited order must stay at the front.
		if !strings.HasPrefix(got, "/opt/homebrew/bin:/usr/bin:/custom:") {
			t.Fatalf("expected inherited segment preserved in order, got %q", got)
		}
	})

	t.Run("empty inherited PATH yields exactly the guarantee list", func(t *testing.T) {
		t.Setenv("HOME", "/home/tester")
		t.Setenv("PATH", "")
		want := "/opt/homebrew/bin:/usr/local/bin:" +
			"/home/tester/.local/bin:/home/tester/go/bin:/home/tester/.cargo/bin:" +
			"/usr/bin:/bin:/usr/sbin:/sbin"
		if got := DaemonPath(); got != want {
			t.Fatalf("DaemonPath() = %q, want %q", got, want)
		}
	})

	t.Run("HOME expansion in guaranteed dirs", func(t *testing.T) {
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
		t.Setenv("HOME", "/home/tester")
		t.Setenv("PATH", "/custom:/opt/homebrew/bin")
		first, second := DaemonPath(), DaemonPath()
		if first != second {
			t.Fatalf("DaemonPath not deterministic: %q vs %q", first, second)
		}
	})
}
