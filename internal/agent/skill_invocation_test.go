package agent

// 04-skill-invocation leaf (T1): the /name gesture regex. `/name`,
// ` use /x-y`, ` /a /b` match; `/usr/bin`, `5/8`, `foo/bar`,
// `word/slashend` never do. Kebab-case names only, unique in
// first-seen order (dsh tool-skill/src/index.ts:429 grammar).

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/skills"
)

func TestSkillInvocation_ExtractsKebabCaseNames(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"bare gesture", "/review-code", []string{"review-code"}},
		{"mid-sentence", "use /review-code please", []string{"review-code"}},
		{"two gestures", " /a /b", []string{"a", "b"}},
		{"dedup first-seen", "/a then /a again", []string{"a"}},
		{"multi-segment kebab", "/fix-the-bug now", []string{"fix-the-bug"}},
		{"path not gesture", "run /usr/bin/ls", nil},
		{"fraction not gesture", "5/8 of the work", nil},
		{"word slash word not gesture", "foo/bar", nil},
		{"slash at word end not gesture", "word/slashend", nil},
		{"trailing slash not gesture", "check /review-code/", nil},
		{"uppercase not gesture", "/Review-Code", nil},
		{"empty", "", nil},
		{"url not gesture", "see https://example.com/x", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := invokedSkillNames(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("invokedSkillNames(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// fakeLoaderForInvocation is a minimal loader double for the gesture
// resolution tests: the leaf contract is "the LOADED-body lookup decides",
// so the double returns pre-built *skills.Skill values or not-found.
type fakeLoaderForInvocation struct {
	skills map[string]*skills.Skill
}

func (f *fakeLoaderForInvocation) Load(ctx context.Context, name string) (*skills.Skill, error) {
	if s, ok := f.skills[name]; ok {
		return s, nil
	}
	return nil, skills.ErrSkillNotFound
}

func TestSkillInvocation_ResolvedBodiesInOrder(t *testing.T) {
	// T2: the loaded-body lookup decides. A name that resolves to a known,
	// user-invocable skill yields an injection block; unknown names and
	// disable-model-invocation names stay prose (no block, no error).
	loader := &fakeLoaderForInvocation{skills: map[string]*skills.Skill{
		"review-code": {Name: "review-code", Description: "review", Body: "DO THE REVIEW"},
		"private":     {Name: "private", DisableModelInvocation: true, Body: "SECRET"},
	}}

	blocks := resolveSkillInvocationBlocks(context.Background(), loader, []string{"review-code", "missing-skill", "private"})
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks, want 1 (unknown and disable-model-invocation excluded)", len(blocks))
	}
	if blocks[0].Name != "review-code" {
		t.Errorf("block name = %q, want review-code", blocks[0].Name)
	}
	if !strings.Contains(blocks[0].Body, "DO THE REVIEW") {
		t.Errorf("block body missing rendered skill content: %q", blocks[0].Body)
	}
	if !strings.Contains(blocks[0].Body, "user asked for this skill") {
		t.Errorf("block body missing explicit-invocation framing: %q", blocks[0].Body)
	}
}

// TestSkillInvocation_DurableCatalogResolution (T4): the gesture resolves
// names against the durable catalog / skill index via the LOADED-body lookup.
// A skill present in the index (discovery) but never cache-loaded — i.e. one
// that never had a fuzzy hit — is still directly invokable by /name. The
// disable-model-invocation and unknown cases stay plain prose, decided on the
// LOADED definition, not an unparsed index token. This exercises the real
// *skills.LazySkillLoader (not a double), backed by on-disk SKILL.md files
// exactly as the durable catalog pipeline produces them.
func TestSkillInvocation_DurableCatalogResolution(t *testing.T) {
	tmpDir := t.TempDir()

	reviewPath := createTestSkillFile(t, tmpDir, "review-code",
		`---
name: review-code
description: Review code from the durable catalog
---

# Review Code

DO THE REVIEW FROM CATALOG
`)
	privatePath := createTestSkillFile(t, tmpDir, "private-skill",
		`---
name: private-skill
description: Internal-only skill
disable-model-invocation: true
---

SECRET BODY
`)

	// The durable catalog: an index whose entries carry the load path from
	// discovery. No skill has been cache-loaded yet.
	idx := skills.NewSkillIndex()
	idx.Index(&skills.SkillIndexEntry{Name: "review-code", Path: reviewPath})
	idx.Index(&skills.SkillIndexEntry{Name: "private-skill", Path: privatePath})
	loader := skills.NewLazySkillLoader(idx)

	// T4 premise: not cached (never fuzzy-hit / never loaded), yet Load must
	// still resolve the body from the catalog-backed index.
	if loader.IsCached("review-code") {
		t.Fatal("durable-catalog premis violated: review-code should start uncached")
	}

	// The gesture scan sees all three tokens; only review-code is invokable.
	names := invokedSkillNames("use /review-code and /private-skill plus /nope")
	if !reflect.DeepEqual(names, []string{"review-code", "private-skill", "nope"}) {
		t.Fatalf("invokedSkillNames = %v, want [review-code private-skill nope]", names)
	}

	blocks := resolveSkillInvocationBlocks(context.Background(), loader, names)
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks, want exactly 1 (only the user-invocable catalog skill)", len(blocks))
	}
	if blocks[0].Name != "review-code" {
		t.Errorf("block name = %q, want review-code", blocks[0].Name)
	}
	if !strings.Contains(blocks[0].Body, "DO THE REVIEW FROM CATALOG") {
		t.Errorf("block body missing the loaded skill body (durable-catalog lookup failed): %q", blocks[0].Body)
	}
	if !strings.Contains(blocks[0].Body, "Review code from the durable catalog") {
		t.Errorf("block body missing the loaded description: %q", blocks[0].Body)
	}
	if !strings.Contains(blocks[0].Body, "user asked for this skill: review-code") {
		t.Errorf("block body missing explicit-invocation provenance: %q", blocks[0].Body)
	}

	// T2 durable analog: the disable-model-invocation skill decoded from disk
	// frontmatter stays prose — Load resolved it (and cached the body), the
	// gate excluded the block. Its body must not leak into any injection.
	if strings.Contains(blocks[0].Body, "SECRET BODY") {
		t.Error("disable-model-invocation skill body leaked into a block")
	}
}

// TestSkillInvocation_UnknownNameStaysProse ensures an /name with no indexed
// skill (absent from the durable catalog) yields no block and no error,
// leaving the token as plain prose in the user's message.
func TestSkillInvocation_UnknownNameStaysProse(t *testing.T) {
	loader := skills.NewLazySkillLoader(skills.NewSkillIndex()) // empty durable catalog
	blocks := resolveSkillInvocationBlocks(context.Background(), loader, []string{"no-such-skill"})
	if len(blocks) != 0 {
		t.Fatalf("got %d blocks for an unknown name, want 0", len(blocks))
	}
}
