package agent

// SOUL.md tests: validation table, seed, hot-reload watcher (real fsnotify),
// loop wiring parity (all prompt-build sites read the live soul), and the
// startup gate semantics.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeSoul(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write soul fixture: %v", err)
	}
}

// --- ValidateSoul: mechanical validity table ---

func TestValidateSoul(t *testing.T) {
	tests := []struct {
		name    string
		content string
		valid   bool
	}{
		{"valid markdown", "# Soul\nBe direct.", true},
		{"empty", "", false},
		{"whitespace only", "   \n\t", true}, // non-empty bytes; prose quality not judged
		{"valid utf8 multibyte", "# Soul — émotions ✓", true},
		{"oversize", strings.Repeat("x", MaxSoulBytes+1), false},
		{"exactly max", strings.Repeat("x", MaxSoulBytes), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSoul([]byte(tt.content))
			if got := err == nil; got != tt.valid {
				t.Fatalf("ValidateSoul(%q) valid=%v, want %v (err=%v)", tt.name, got, tt.valid, err)
			}
		})
	}

	t.Run("invalid utf8 rejected", func(t *testing.T) {
		bad := []byte{0xff, 0xfe, 0x00}
		if err := ValidateSoul(bad); err == nil {
			t.Fatal("ValidateSoul(invalid utf8) = nil, want error")
		}
	})
}

// --- SeedSoulIfMissing: never clobbers ---

func TestSeedSoulIfMissing(t *testing.T) {
	t.Run("seeds default into empty dir", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), SoulFileName)
		seeded, err := SeedSoulIfMissing(path)
		if err != nil || !seeded {
			t.Fatalf("seed = %v, %v; want true, nil", seeded, err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != DefaultSoulMD() {
			t.Fatalf("seeded content != DefaultSoulMD (%d vs %d bytes)", len(got), len(DefaultSoulMD()))
		}
		if err := ValidateSoul(got); err != nil {
			t.Fatalf("shipped default must pass ValidateSoul: %v", err)
		}
	})

	t.Run("existing file never touched", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), SoulFileName)
		writeSoul(t, path, "custom persona")
		seeded, err := SeedSoulIfMissing(path)
		if err != nil || seeded {
			t.Fatalf("seed = %v, %v; want false, nil", seeded, err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != "custom persona" {
			t.Fatalf("seed overwrote user file: %q", got)
		}
	})
}

// --- LoadSoul / NewSoulProvider: startup gate ---

func TestNewSoulProvider(t *testing.T) {
	t.Run("valid file loads", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), SoulFileName)
		writeSoul(t, path, "be terse")
		sp, err := NewSoulProvider(path, nil)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if sp.Current() != "be terse" {
			t.Fatalf("Current() = %q", sp.Current())
		}
		if _, sha, _, _, _ := sp.Status(); len(sha) != 64 {
			t.Fatalf("sha256 = %q, want 64 hex chars", sha)
		}
	})

	t.Run("missing file refuses", func(t *testing.T) {
		_, err := NewSoulProvider(filepath.Join(t.TempDir(), SoulFileName), nil)
		if err == nil {
			t.Fatal("missing file must error (startup gate)")
		}
	})

	t.Run("invalid file refuses with reason", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), SoulFileName)
		writeSoul(t, path, "") // empty = invalid (save-in-progress)
		_, err := NewSoulProvider(path, nil)
		if err == nil || !strings.Contains(err.Error(), "empty") {
			t.Fatalf("empty file: err=%v, want empty-file error", err)
		}
	})
}

// --- Hot reload: real fsnotify against the temp dir ---

func TestSoulHotReload(t *testing.T) {
	t.Run("valid change reloads", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, SoulFileName)
		writeSoul(t, path, "v1")

		sp, err := NewSoulProvider(path, nil)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := sp.StartWatching(ctx); err != nil {
			t.Fatalf("StartWatching: %v", err)
		}

		// Rename-style save (write temp, rename over) — the vim/VS Code path.
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte("v2 via rename"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, path); err != nil {
			t.Fatal(err)
		}

		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if sp.Current() == "v2 via rename" {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("hot reload did not land; Current()=%q", sp.Current())
	})

	t.Run("invalid change keeps previous copy", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, SoulFileName)
		writeSoul(t, path, "good persona")

		sp, err := NewSoulProvider(path, nil)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := sp.StartWatching(ctx); err != nil {
			t.Fatal(err)
		}

		_, shaBefore, _, _, _ := sp.Status()
		writeSoul(t, path, "") // invalid: empty

		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			_, shaAfter, _, _, _ := sp.Status()
			// Wait out the debounce window plus slack, then confirm unchanged.
			if time.Since(deadline) > 0 {
				break
			}
			time.Sleep(50 * time.Millisecond)
			_ = shaAfter
		}
		time.Sleep(time.Second) // let any (wrongful) reload land
		if sp.Current() != "good persona" {
			t.Fatalf("invalid change replaced soul: %q", sp.Current())
		}
		_, shaAfter, _, _, _ := sp.Status()
		if shaAfter != shaBefore {
			t.Fatalf("sha changed on rejected edit: %s -> %s", shaBefore, shaAfter)
		}
	})

	t.Run("reload hook fires", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, SoulFileName)
		writeSoul(t, path, "h1")

		sp, err := NewSoulProvider(path, nil)
		if err != nil {
			t.Fatal(err)
		}
		var mu sync.Mutex
		var hooked []string
		sp.SetReloadHook(func(text string) {
			mu.Lock()
			hooked = append(hooked, text)
			mu.Unlock()
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := sp.StartWatching(ctx); err != nil {
			t.Fatal(err)
		}

		writeSoul(t, path, "h2")
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			n := len(hooked)
			mu.Unlock()
			if n > 0 {
				mu.Lock()
				if hooked[0] != "h2" {
					t.Fatalf("hook text = %q, want h2", hooked[0])
				}
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("reload hook never fired")
	})
}

// --- Loop wiring: all prompt-build sites see the live soul ---

func TestAgentLoopEffectivePersonality(t *testing.T) {
	t.Run("nil provider falls back to config", func(t *testing.T) {
		loop := NewAgentLoop("soul-test", "", WithAgentConfig(AgentConfig{
			Personality: "config persona",
		}))
		if loop.effectivePersonality() != "config persona" {
			t.Fatalf("got %q", loop.effectivePersonality())
		}
	})

	t.Run("provider wins and tracks reloads", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, SoulFileName)
		writeSoul(t, path, "soul v1")

		sp, err := NewSoulProvider(path, nil)
		if err != nil {
			t.Fatal(err)
		}
		loop := NewAgentLoop("soul-test", "", WithAgentConfig(AgentConfig{
			Personality: "config persona",
		}))
		loop.SetSoulProvider(sp)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := sp.StartWatching(ctx); err != nil {
			t.Fatalf("StartWatching: %v", err)
		}

		if got := loop.effectivePersonality(); got != "soul v1" {
			t.Fatalf("soul not in personality slot: %q", got)
		}

		// Hot reload: the loop must see the new text on the next build.
		writeSoul(t, path, "soul v2")
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if loop.effectivePersonality() == "soul v2" {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("loop did not observe reload; got %q", loop.effectivePersonality())
	})
}

// --- Prompt-level parity: the soul text lands in the built prompt's
// Personality section for every builder produced from the loop config path.

func TestSoulLandsInBuiltPrompt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SoulFileName)
	writeSoul(t, path, "SOUL-MARKER-TEXT")

	sp, err := NewSoulProvider(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	loop := NewAgentLoop("soul-prompt-test", "")
	loop.SetSoulProvider(sp)

	b := NewPromptBuilderFromConfig(PromptConfig{
		Constitution: DefaultConstitution,
		Restrictions: DefaultRestrictions,
		Purpose:      DefaultPurpose,
		Personality:  loop.effectivePersonality(),
	})
	prompt := b.Build()
	if !strings.Contains(prompt, "SOUL-MARKER-TEXT") {
		t.Fatal("soul text missing from built prompt")
	}
	if !strings.Contains(prompt, "# Personality") {
		t.Fatal("personality section header missing")
	}
}
