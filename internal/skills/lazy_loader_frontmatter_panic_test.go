package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The 2026-10-10 daemon crash. The running meept-daemon panicked with SIGSEGV
// in LazySkillLoader.loadAndCache (lazy_loader.go:161) the first time an agent
// loaded a skill whose file had no YAML frontmatter:
//
//	panic: runtime error: invalid memory address or nil pointer dereference
//	[signal SIGSEGV: segmentation violation code=0x2 addr=0x0]
//	github.com/caimlas/meept/internal/skills.(*LazySkillLoader).loadAndCache
//	    internal/skills/lazy_loader.go:161
//
// CAUSE: ParseSkillFile returns (nil, ErrNoFrontmatter) when a file has no
// frontmatter. loadAndCache's ErrNoFrontmatter branch logged a warning and then
// FELL THROUGH to the cache-insert code, dereferencing the nil skill.
//
// The comment in the old branch claimed "still usable, just warn" — that was
// true for ParseSkillMetadataOnly (which returns a slug-derived entry alongside
// the error) but not for ParseSkillFile.

// TestLazyLoaderNoFrontmatterReturnsErrorNotPanic is the regression pin: a
// frontmatter-less skill must come back as an ERROR, never a nil skill that
// the caller dereferences.
func TestLazyLoaderNoFrontmatterReturnsErrorNotPanic(t *testing.T) {
	tmpDir := t.TempDir()
	// A skill with no YAML frontmatter, in the real <dir>/SKILL.md layout —
	// the exact incident shape. (slugFromPath uses the parent DIRECTORY name,
	// so a flat file's slug would be the temp dir, not the skill.)
	body := "# Some Skill\n\nMarkdown body only, no frontmatter block.\n"
	skillDir := filepath.Join(tmpDir, "no-frontmatter")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Double-check the fixture really produces ErrNoFrontmatter.
	if _, err := ParseSkillFile(skillPath); !errors.Is(err, ErrNoFrontmatter) {
		t.Fatalf("fixture does not produce ErrNoFrontmatter: %v", err)
	}

	idx := NewSkillIndex()
	idx.Index(&SkillIndexEntry{
		Name:        "no-frontmatter",
		Description: "indexed but unloadable",
		Path:        skillPath,
	})
	loader := NewLazySkillLoader(idx, WithCacheSize(10))

	// Restored lenient contract: the skill LOADS, with the slug as its name and
	// ErrNoFrontmatter reported alongside. (When ParseSkillFile discarded the
	// skill, this call crashed the daemon with a nil dereference — that panic
	// is what this file originally pinned.)
	skill, err := loader.Load(context.Background(), "no-frontmatter")
	if err != nil {
		t.Fatalf("Load failed under the lenient contract: %v", err)
	}
	if skill == nil {
		t.Fatal("Load returned nil skill")
	}
	if skill.Name != "no-frontmatter" {
		t.Fatalf("name = %q, want the slug", skill.Name)
	}
	if !strings.Contains(skill.Body, "Markdown body only") {
		t.Fatalf("body = %q, want the full text as body", skill.Body)
	}
}

// TestLazyLoaderRecoversAfterNoFrontmatter proves the failure is per-skill:
// after refusing a frontmatter-less skill, the loader still serves healthy
// ones. (Before the fix this path panicked the whole daemon, so nothing
// recovered at all.)
func TestLazyLoaderRecoversAfterNoFrontmatter(t *testing.T) {
	tmpDir := t.TempDir()
	badDir := filepath.Join(tmpDir, "broken")
	if err := os.MkdirAll(badDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	bad := filepath.Join(badDir, "SKILL.md")
	if err := os.WriteFile(bad, []byte("# no frontmatter here\n"), 0o600); err != nil {
		t.Fatalf("write bad: %v", err)
	}
	goodDir := filepath.Join(tmpDir, "good-skill")
	if err := os.MkdirAll(goodDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	goodPath := filepath.Join(goodDir, "SKILL.md")
	if err := os.WriteFile(goodPath,
		[]byte("---\nname: test-skill\ndescription: A test skill\n---\n\n# Test Skill\n\nBody.\n"), 0o600); err != nil {
		t.Fatalf("write good: %v", err)
	}

	idx := NewSkillIndex()
	idx.Index(&SkillIndexEntry{Name: "broken", Path: bad})
	idx.Index(&SkillIndexEntry{Name: "good-skill", Path: goodPath})
	loader := NewLazySkillLoader(idx, WithCacheSize(10))

	// The broken skill now loads leniently (slug name) instead of refusing.
	broken, err := loader.Load(context.Background(), "broken")
	if err != nil {
		t.Fatalf("broken skill failed to load leniently: %v", err)
	}
	if broken.Name != "broken" {
		t.Fatalf("broken skill name = %q, want slug", broken.Name)
	}

	// And the healthy skill still loads with its declared frontmatter name.
	skill, err := loader.Load(context.Background(), "good-skill")
	if err != nil {
		t.Fatalf("healthy skill failed: %v", err)
	}
	if skill == nil || skill.Name != "test-skill" {
		t.Fatalf("healthy skill = %+v, want name test-skill", skill)
	}
}

// TestParseSkillFileLenientFrontmatterContract pins the ORIGINAL documented
// behaviour (7fff4e72, 2026-05-14): "YAML frontmatter is optional." A
// frontmatter-less file must come back as a usable skill PLUS ErrNoFrontmatter,
// with the slug as the name — not nil.
//
// ParseSkillFile (added later, in the Go rewrite 557d9ecf) discarded the skill
// on any error, silently breaking that contract for every caller. That is what
// made all frontmatter-less Hermes skills unloadable, and it culminated in the
// nil-dereference panic pinned above: lazy_loader had been written against the
// documented contract, then patched around the wrapper's actual behaviour.
// This test holds the wrapper to the contract so the two cannot drift apart
// again.
func TestParseSkillFileLenientFrontmatterContract(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "my-plain-skill")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte("# Just markdown\n\nNo frontmatter.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	skill, err := ParseSkillFile(path)
	if skill == nil {
		t.Fatalf("ParseSkillFile returned nil skill with %v; the lenient contract is broken", err)
	}
	if !errors.Is(err, ErrNoFrontmatter) {
		t.Fatalf("err = %v, want ErrNoFrontmatter alongside the usable skill", err)
	}
	if skill.Name != "my-plain-skill" {
		t.Fatalf("name = %q, want the directory slug", skill.Name)
	}
	if skill.Body == "" {
		t.Fatal("body is empty; the full text should become the body")
	}
}
