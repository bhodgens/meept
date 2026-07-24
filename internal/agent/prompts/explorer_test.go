package prompts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExplorerPromptReadOnly(t *testing.T) {
	assert.Contains(t, ExplorerPrompt, "READ-ONLY")
	assert.Contains(t, ExplorerPrompt, "STRICTLY PROHIBITED")
	assert.True(t, strings.Contains(ExplorerPrompt, "creating, modifying, or deleting"))
}

func TestExplorerPromptStructure(t *testing.T) {
	assert.Contains(t, ExplorerPrompt, "## Files Found")
	assert.Contains(t, ExplorerPrompt, "## Key Findings")
	assert.Contains(t, ExplorerPrompt, "## Summary")
}
