package security

import (
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// L06 (plans/context-reduction/06-untrusted-markers.md): the chat-history
// seam fences every tool result with llm.ToolOutputMarkerEnd as its closing
// marker. security.PromptGuard.WrapToolOutput closes its fence with
// ToolOutputEndTag. The two literals must stay byte-identical, or history
// content carries two different END markers and boundary detection
// (IsWithinBoundary / ExtractToolOutput) misses one of them. A const cannot
// express cross-package identity, so a test pins it.
func TestToolOutputEndTagMatchesFirewallMarker(t *testing.T) {
	if ToolOutputEndTag != llm.ToolOutputMarkerEnd {
		t.Errorf("marker drift: security.ToolOutputEndTag = %q, llm.ToolOutputMarkerEnd = %q",
			ToolOutputEndTag, llm.ToolOutputMarkerEnd)
	}
}
