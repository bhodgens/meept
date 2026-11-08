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
