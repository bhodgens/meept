//go:build e2e

// step-pipeline-06 (S): the doc-only auto-approve matcher must bind to an
// EXACT filename token. The pre-fix matcher searched description substrings,
// so a code change described as "fix README.md and update auth.py" could
// ride the low-risk shortcut. This pins the tightened predicate directly
// against the compiled ReviewPolicy (no daemon needed: the step description
// is the only input, and the gate is pure).
package steppipeline

import (
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/task"
)

func autoApproveStep(desc string) *task.TaskStep {
	return &task.TaskStep{Description: desc}
}

func TestDocOnlyAutoApproveExactToken(t *testing.T) {
	policy := agent.DefaultReviewPolicy()

	// Positive: exactly one doc token with a low-risk action verb.
	for _, desc := range []string{
		"update README.md with the new install steps",
		"fix docs/CHANGELOG.md typo",
		"update LICENSE year",
	} {
		if !policy.ShouldAutoApprove(autoApproveStep(desc)) {
			t.Errorf("step-pipeline-06: expected auto-approve for doc-only step %q", desc)
		}
	}

	// Negative: no matching filename token (the substring "*.md" appeared
	// only as prose in the pre-fix form), so no approval.
	for _, desc := range []string{
		"update the handler to use *.md style docs",
		"refresh documentation without touching files",
	} {
		if policy.ShouldAutoApprove(autoApproveStep(desc)) {
			t.Errorf("step-pipeline-06: must NOT auto-approve step %q (no exact doc token)", desc)
		}
	}

	// Negative: a code token anywhere vetoes the whole step, even when a
	// doc token is also present.
	for _, desc := range []string{
		"fix README.md and update auth.py",
		"update CHANGELOG.md after refactor of server.go",
		"fix docs/guide.md plus internal/agent/loop.go",
	} {
		if policy.ShouldAutoApprove(autoApproveStep(desc)) {
			t.Errorf("step-pipeline-06: must NOT auto-approve mixed code/doc step %q", desc)
		}
	}

	// Negative: nil step and empty description fail closed.
	if policy.ShouldAutoApprove(nil) {
		t.Error("step-pipeline-06: nil step must fail closed")
	}
	if policy.ShouldAutoApprove(autoApproveStep("")) {
		t.Error("step-pipeline-06: empty step must fail closed")
	}
}
