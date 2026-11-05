package skills

// F11 gate T3 watcher arm (04-tool-surface/00T-tests.md): fsnotify watcher
// over the two discovery roots (project .meept/skills + ~/.meept/skills,
// discovery.go:31-53 DefaultTiers). On add/change/remove of
// <root>/<skill>/SKILL.md or <root>/<skill>.md the watcher must:
//   - invalidate the loader for that skill (lazy_loader.go Invalidate:238 /
//     Clear:260)
//   - re-Discover (discovery.go:136 DiscoverWithContext) and SetIndex
//     (lazy_loader.go:307)
//   - publish the skills/change bus event with payload {source:"skills"}
//
// Pins contract B3 (.hermes/contracts/04-tool-surface.md).
// RED until leaf 03-skill-catalog lands (NewSkillWatcher etc. undefined).

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
)

func TestSkillWatcher_InvalidatesAndPublishesOnSKILLMDAdd(t *testing.T) {
	root := t.TempDir()
	skillsRoot := filepath.Join(root, "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	msgBus := bus.New(nil, slog.Default())
	sub := msgBus.Subscribe("watcher-test", "skills/change")
	defer msgBus.Unsubscribe(sub)

	loader := NewLazySkillLoader(NewSkillIndex(), WithLoaderLogger(slog.Default()))
	w, err := NewSkillWatcher(SkillWatcherConfig{
		Roots:  []string{skillsRoot},
		Loader: loader,
		Bus:    msgBus,
		Logger: slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewSkillWatcher: %v", err)
	}
	defer w.Close()

	// Add a brand-new skill bundle: <root>/hot-skill/SKILL.md
	bundle := filepath.Join(skillsRoot, "hot-skill")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("MkdirAll bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "SKILL.md"),
		[]byte("---\nname: hot-skill\ndescription: hot reloaded\n---\nbody\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// The loader cache must be invalidated/reindexed and the bus event fired.
	deadline := time.After(5 * time.Second)
	select {
	case msg := <-sub.Channel:
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("payload: %v", err)
		}
		if payload["source"] != "skills" {
			t.Errorf("payload[source] = %v, want skills", payload["source"])
		}
	case <-deadline:
		t.Fatal("timeout: no skills/change event after SKILL.md add")
	}

	if _, err := loader.Load(t.Context(), "hot-skill"); err != nil {
		t.Errorf("hot-skill not discoverable after watcher reload: %v", err)
	}
}

func TestSkillWatcher_FlatMarkdownFileChange(t *testing.T) {
	// <root>/<skill>.md flat files are discovered too (dsh 1b parity).
	root := t.TempDir()
	skillsRoot := filepath.Join(root, "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	msgBus := bus.New(nil, slog.Default())
	sub := msgBus.Subscribe("watcher-flat", "skills/change")
	defer msgBus.Unsubscribe(sub)

	w, err := NewSkillWatcher(SkillWatcherConfig{
		Roots:  []string{skillsRoot},
		Loader: NewLazySkillLoader(NewSkillIndex()),
		Bus:    msgBus,
		Logger: slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewSkillWatcher: %v", err)
	}
	defer w.Close()

	if err := os.WriteFile(filepath.Join(skillsRoot, "flat-skill.md"),
		[]byte("---\nname: flat-skill\ndescription: flat\n---\nbody\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	select {
	case <-sub.Channel:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: no skills/change event after flat .md add")
	}
}

func TestSkillWatcher_IgnoresUnrelatedFiles(t *testing.T) {
	// A file that is not SKILL.md / <skill>.md (e.g. README.md inside a
	// bundle, or a tmp editor file) must NOT fire a reload event.
	root := t.TempDir()
	skillsRoot := filepath.Join(root, "skills")
	bundle := filepath.Join(skillsRoot, "quiet-skill")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	msgBus := bus.New(nil, slog.Default())
	sub := msgBus.Subscribe("watcher-quiet", "skills/change")
	defer msgBus.Unsubscribe(sub)

	w, err := NewSkillWatcher(SkillWatcherConfig{
		Roots:  []string{skillsRoot},
		Loader: NewLazySkillLoader(NewSkillIndex()),
		Bus:    msgBus,
		Logger: slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewSkillWatcher: %v", err)
	}
	defer w.Close()

	if err := os.WriteFile(filepath.Join(bundle, "README.md"), []byte("notes"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	select {
	case msg := <-sub.Channel:
		t.Fatalf("unexpected skills/change event for an unrelated file: %s", msg.Payload)
	case <-time.After(500 * time.Millisecond):
		// correct: no event
	}
}
