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

// TestStripClaimsEvidenceOrProse_MalformedClaimsShapedProseSurvives pins the
// bughunt-wave2 finding-5 guard port (mirrors internal/task's
// isClaimsEnvelopeJSON tightening, cccf5b64): a brace-balanced fragment
// that merely STARTS with a claims/evidence key but is not a fully-parsing
// JSON envelope must survive — the leading-key prefix check used to
// truncate the conversational reply at the fragment while the task
// boundary kept it.
func TestStripClaimsEvidenceOrProse_MalformedClaimsShapedProseSurvives(t *testing.T) {
	// Brace-balanced but NOT valid JSON (bare identifiers, unquoted keys
	// mixed with prose): the prefix check matched, the full parse must not.
	malformed := `The report said {"claims": we shipped it, "evidence": see the log} and then continued.`
	got := StripClaimsEvidenceOrProse(malformed)
	if !strings.Contains(got, "and then continued") {
		t.Errorf("trailing prose truncated by malformed claims-shaped fragment: %q", got)
	}
	if !strings.Contains(got, "The report said") {
		t.Errorf("leading prose lost: %q", got)
	}

	// A REAL envelope is still stripped/prose-recovered (guard must not
	// over-tighten): claims + evidence keys with valid JSON.
	real := `Done. {"claims":["created"],"evidence":[{"type":"file_exists","path":"/w/hello.txt"}]}`
	got2 := StripClaimsEvidenceOrProse(real)
	if strings.Contains(got2, `"claims"`) {
		t.Errorf("real envelope keys leaked: %q", got2)
	}
	if !strings.Contains(got2, "Done.") {
		t.Errorf("prose before real envelope lost: %q", got2)
	}
}
