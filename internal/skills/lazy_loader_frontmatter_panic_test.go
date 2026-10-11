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
	// A skill file with no YAML frontmatter — the exact incident shape.
	body := "# Some Skill\n\nMarkdown body only, no frontmatter block.\n"
	skillPath := filepath.Join(tmpDir, "no-frontmatter.md")
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

	skill, err := loader.Load(context.Background(), "no-frontmatter")
	if err == nil {
		t.Fatalf("Load returned (%v, nil error); a frontmatter-less skill must be an error", skill)
	}
	if skill != nil {
		t.Fatalf("Load returned a non-nil skill alongside an error: %+v", skill)
	}
	if !strings.Contains(err.Error(), "frontmatter") {
		t.Fatalf("error should name the frontmatter problem, got: %v", err)
	}
}

// TestLazyLoaderRecoversAfterNoFrontmatter proves the failure is per-skill:
// after refusing a frontmatter-less skill, the loader still serves healthy
// ones. (Before the fix this path panicked the whole daemon, so nothing
// recovered at all.)
func TestLazyLoaderRecoversAfterNoFrontmatter(t *testing.T) {
	tmpDir := t.TempDir()
	bad := filepath.Join(tmpDir, "broken.md")
	if err := os.WriteFile(bad, []byte("# no frontmatter here\n"), 0o600); err != nil {
		t.Fatalf("write bad: %v", err)
	}
	goodPath := createTestSkillFile(t, tmpDir, "good-skill",
		"---\nname: test-skill\ndescription: A test skill\n---\n\n# Test Skill\n\nBody.\n")

	idx := NewSkillIndex()
	idx.Index(&SkillIndexEntry{Name: "broken", Path: bad})
	idx.Index(&SkillIndexEntry{Name: "good-skill", Path: goodPath})
	loader := NewLazySkillLoader(idx, WithCacheSize(10))

	if _, err := loader.Load(context.Background(), "broken"); err == nil {
		t.Fatal("broken skill loaded without error")
	}

	skill, err := loader.Load(context.Background(), "good-skill")
	if err != nil {
		t.Fatalf("healthy skill failed after a frontmatter refusal: %v", err)
	}
	if skill == nil || skill.Name != "test-skill" {
		t.Fatalf("healthy skill = %+v, want name test-skill", skill)
	}
}
