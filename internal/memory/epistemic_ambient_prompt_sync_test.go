package memory

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestAmbientExtractionPromptMatchesEvalHarness guards the verbatim sync
// between the production ambient extraction prompt (this package) and the
// mirrored copy in tools/memory-eval/grade.go. The eval only measures what
// production would say if the two templates match exactly; drift silently
// invalidates every measured metric.
func TestAmbientExtractionPromptMatchesEvalHarness(t *testing.T) {
	const gradePath = "../../tools/memory-eval/grade.go"
	src, err := os.ReadFile(gradePath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("eval harness source not present: %s", gradePath)
		}
		t.Fatalf("read %s: %v", gradePath, err)
	}

	re := regexp.MustCompile(`const ambientPrompt = ` + "`" + `([^` + "`" + `]+)` + "`")
	m := re.FindStringSubmatch(string(src))
	if m == nil {
		t.Fatalf("ambientPrompt const not found in %s", gradePath)
	}
	harness := strings.TrimSpace(m[1])
	prod := strings.TrimSpace(ambientExtractionPromptTemplate)
	if harness != prod {
		t.Fatalf(`ambientPrompt in %s has drifted from ambientExtractionPromptTemplate in internal/memory/epistemic_ambient.go.
--- harness (tools/memory-eval/grade.go) ---
%s
--- production (internal/memory/epistemic_ambient.go) ---
%s
Apply the changed wording to BOTH copies identically.`, gradePath, harness, prod)
	}
}
