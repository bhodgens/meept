package prompts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildVerifierPrompt(t *testing.T) {
	prompt := BuildVerifierPrompt("coder", "implement feature X", []string{"foo.go", "bar.go"}, "added new handler")

	t.Run("contains adversarial framing", func(t *testing.T) {
		assert.Contains(t, prompt, "Your job is not to confirm the implementation works")
		assert.Contains(t, prompt, "it's to try to break it")
	})

	t.Run("contains anti-rationalization", func(t *testing.T) {
		assert.Contains(t, prompt, "Anti-Rationalization")
		assert.Contains(t, prompt, "reading is not verification")
	})

	t.Run("contains adversarial probes", func(t *testing.T) {
		assert.Contains(t, prompt, "Adversarial Probes")
		assert.Contains(t, prompt, "Concurrency")
		assert.Contains(t, prompt, "Boundary values")
		assert.Contains(t, prompt, "Idempotency")
		assert.Contains(t, prompt, "Nil/zero values")
	})

	t.Run("contains evidence requirements", func(t *testing.T) {
		assert.Contains(t, prompt, "Evidence Requirements")
		assert.Contains(t, prompt, "file_exists")
	})

	t.Run("contains VERDICT format", func(t *testing.T) {
		assert.Contains(t, prompt, "VERDICT: PASS")
		assert.Contains(t, prompt, "VERDICT: FAIL")
		assert.Contains(t, prompt, "VERDICT: PARTIAL")
	})

	t.Run("contains task context", func(t *testing.T) {
		assert.Contains(t, prompt, "implement feature X")
		assert.Contains(t, prompt, "foo.go")
		assert.Contains(t, prompt, "bar.go")
		assert.Contains(t, prompt, "added new handler")
	})

	t.Run("contains strict prohibitions", func(t *testing.T) {
		assert.Contains(t, prompt, "STRICTLY PROHIBITED")
		assert.Contains(t, prompt, "Modifying any files")
	})

	t.Run("contains CHECK format", func(t *testing.T) {
		assert.Contains(t, prompt, "CHECK:")
		assert.Contains(t, prompt, "COMMAND:")
		assert.Contains(t, prompt, "OUTPUT:")
		assert.Contains(t, prompt, "RESULT:")
	})
}

func TestBuildVerifierPromptRoleSpecific(t *testing.T) {
	tests := []struct {
		name     string
		role     string
		contains []string
	}{
		{
			name:     "coder role",
			role:     "coder",
			contains: []string{"Coder Verification", "compile", "-race"},
		},
		{
			name:     "executor role maps to coder",
			role:     "executor",
			contains: []string{"Coder Verification"},
		},
		{
			name:     "debugger role",
			role:     "debugger",
			contains: []string{"Debugger Verification", "root cause", "regressions"},
		},
		{
			name:     "planner role",
			role:     "planner",
			contains: []string{"Planner Verification", "edge cases", "rollback"},
		},
		{
			name:     "architect role maps to planner",
			role:     "architect",
			contains: []string{"Planner Verification"},
		},
		{
			name:     "default role",
			role:     "writer",
			contains: []string{"General Verification", "completeness"},
		},
		{
			name:     "empty role uses default",
			role:     "",
			contains: []string{"General Verification"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := BuildVerifierPrompt(tt.role, "test task", nil, "")
			require.NotEmpty(t, prompt)
			for _, s := range tt.contains {
				assert.Contains(t, prompt, s, "prompt should contain %q for role %q", s, tt.role)
			}
		})
	}
}

func TestBuildVerifierPromptEmptyContext(t *testing.T) {
	t.Run("no files changed", func(t *testing.T) {
		prompt := BuildVerifierPrompt("coder", "task", nil, "")
		assert.NotContains(t, prompt, "Files Changed")
		assert.Contains(t, prompt, "task")
	})

	t.Run("no approach", func(t *testing.T) {
		prompt := BuildVerifierPrompt("coder", "task", []string{"a.go"}, "")
		assert.NotContains(t, prompt, "Implementation Approach")
	})

	t.Run("all fields populated", func(t *testing.T) {
		prompt := BuildVerifierPrompt("debugger", "fix bug", []string{"x.go"}, "patched nil check")
		assert.Contains(t, prompt, "fix bug")
		assert.Contains(t, prompt, "x.go")
		assert.Contains(t, prompt, "patched nil check")
		assert.Contains(t, prompt, "Debugger Verification")
	})
}

func TestBuildVerifierPromptContainsAdversarialSelfCheck(t *testing.T) {
	prompt := BuildVerifierPrompt("coder", "task", nil, "")
	// EvidenceRequirements is fused, which now includes the adversarial self-check
	assert.True(t, strings.Contains(prompt, "Adversarial Self-Check"),
		"verifier prompt should include the adversarial self-check from EvidenceRequirements")
}
