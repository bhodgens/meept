package skills

import (
	"os"
	"testing"
)

// TestLearnFromVideoSkillParses verifies the shipped learn-from-video
// composition skill (skill-authoring-and-media-ingest leaf 05) parses with
// meept's SKILL.md parser and carries the contract-required frontmatter.
func TestLearnFromVideoSkillParses(t *testing.T) {
	if _, err := os.Stat("../../config/skills/learn-from-video/SKILL.md"); err != nil {
		t.Skip("skill file not present")
	}
	entry, err := ParseSkillMetadataOnly("../../config/skills/learn-from-video/SKILL.md")
	if err != nil {
		t.Fatalf("ParseSkillMetadataOnly: %v", err)
	}
	if entry.Name != "learn-from-video" {
		t.Errorf("name = %q, want learn-from-video", entry.Name)
	}
	if entry.Description == "" {
		t.Error("description empty")
	}
	skill, err := ParseSkillFile("../../config/skills/learn-from-video/SKILL.md")
	if err != nil {
		t.Fatalf("ParseSkillFile: %v", err)
	}
	if len(skill.Body) == 0 {
		t.Error("body empty")
	}
	for _, want := range []string{"when to use", "workflow", "decision rules", "verification"} {
		if !containsFold(skill.Body, want) {
			t.Errorf("body missing required section %q", want)
		}
	}
	if !containsFold(skill.Body, "transcript_fetch") ||
		!containsFold(skill.Body, "skills_create") ||
		!containsFold(skill.Body, "skills_patch") {
		t.Error("body must reference the transcript_fetch -> skills_create/skills_patch composition")
	}
	if !containsFold(skill.Body, "SHOW THE DRAFT") {
		t.Error("body missing the user-confirmation step (step 5 must not be softened)")
	}
	// transcript-large-corpus leaf 03: the learn workflow is file-backed.
	// The skill must teach output_path + file_read paging as the primary
	// path and summarize=true for gist requests — never the impossible
	// in-context chunk summarization.
	for _, want := range []string{"output_path", "file_read", "summarize=true"} {
		if !containsFold(skill.Body, want) {
			t.Errorf("body missing file-backed workflow marker %q", want)
		}
	}
	if containsFold(skill.Body, "40k") {
		t.Error("body still teaches in-context chunk summarization (\"40k\"); must use the file-backed workflow")
	}
}

// containsFold reports whether s contains substr (ASCII case-insensitive).
func containsFold(s, substr string) bool {
	n := len(substr)
	if n == 0 {
		return true
	}
	for i := 0; i+n <= len(s); i++ {
		if equalFoldASCII(s[i:i+n], substr) {
			return true
		}
	}
	return false
}

func equalFoldASCII(a, b string) bool {
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
