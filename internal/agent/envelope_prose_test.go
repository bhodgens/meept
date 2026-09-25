package agent

import (
	"strings"
	"testing"
)

// TestStripClaimsEvidenceOrProse pins the evidence-only envelope recovery
// (2026-09-25 run NKZiEl): the model emitted an envelope whose ONLY key was
// "evidence", so the claims-anchored scanner skipped it and the reply guard
// nuked the whole answer. Now: variant matched, prose recovered.
func TestStripClaimsEvidenceOrProse(t *testing.T) {
	evidenceOnly := `{"evidence":["job job-x completed by agent coder: The file 'hello.txt' has been created. The full path is /w/project/hello.txt."]}`
	got := StripClaimsEvidenceOrProse(evidenceOnly)
	if !strings.Contains(got, "hello.txt") {
		t.Errorf("prose not recovered from evidence-only envelope: %q", got)
	}
	if strings.Contains(got, `"evidence"`) {
		t.Errorf("envelope keys leaked: %q", got)
	}

	// Claims+evidence prose survives the strip.
	both := "I created the file.\n" + `{"claims":["created"],"evidence":[{"type":"file_exists","path":"/w/hello.txt"}]}`
	got2 := StripClaimsEvidenceOrProse(both)
	if !strings.Contains(got2, "created the file") {
		t.Errorf("prose lost: %q", got2)
	}

	// Plain prose untouched.
	if got3 := StripClaimsEvidenceOrProse("just an answer"); got3 != "just an answer" {
		t.Errorf("plain prose altered: %q", got3)
	}
}
