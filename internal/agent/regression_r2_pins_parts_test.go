package agent

// Regression-review R2 wave pin test additions (2026-11-11).
//
//   R2-3: cloneMessagesForTransform copied ChatMessage structs + ToolCalls
//         but NOT the Parts array, so C18 Parts masking (MaskOutboundScoped
//         writes through &Parts[j]) mutated the conversation history's shared
//         backing array. The clone now deep-copies Parts; the pinned HISTORY
//         NON-MUTATION invariant extends to Parts.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// TestCloneMessagesForTransformDeepCopiesParts (R2-3, unit): the clone's
// Parts slice must not share a backing array with the input — masking the
// clone through &Parts[j] must leave the original untouched.
func TestCloneMessagesForTransformDeepCopiesParts(t *testing.T) {
	src := []llm.ChatMessage{{
		Role:    llm.RoleUser,
		Content: "raw text",
		Parts: []llm.ContentPart{
			{Type: "text", Text: "raw part text"},
		},
	}}

	cloned := cloneMessagesForTransform(src)
	if &cloned[0].Parts[0] == &src[0].Parts[0] {
		t.Fatal("clone shares the Parts backing array with the source (R2-3 regression)")
	}
	// Mutate the clone the way MaskOutboundScoped does (through &Parts[j]).
	cloned[0].Parts[0].Text = "[PII:x1] masked"
	cloned[0].Content = "[PII:x1] masked"
	if src[0].Parts[0].Text != "raw part text" {
		t.Errorf("masking the clone wrote through to the source Parts: %q", src[0].Parts[0].Text)
	}
	if src[0].Content != "raw text" {
		t.Errorf("masking the clone wrote through to the source Content: %q", src[0].Content)
	}
}

// TestLoopPiiMask_HistoryPartsUnmutated (R2-3, loop-level): a turn entered
// with parts must reach the provider masked while the conversation
// HISTORY-stored Parts stay byte-identical to what was added — the pinned
// HISTORY NON-MUTATION invariant.
func TestLoopPiiMask_HistoryPartsUnmutated(t *testing.T) {
	hook, swapper := newPiiRestoreTestHook(t, config.PiiConfig{Enabled: true})

	msgs := []llm.ChatMessage{{
		Role:    llm.RoleUser,
		Content: "contact bob@example.com about the invoice",
		Parts: []llm.ContentPart{
			{Type: "text", Text: "also cc carol@example.com"},
			{Type: "image_url", ImageURL: &llm.ImageRef{URL: "file://img.png"}},
		},
	}}

	// Snapshot the history-stored shape BEFORE masking (JSON = byte
	// identity for the parts struct).
	before, err := json.Marshal(msgs[0].Parts)
	if err != nil {
		t.Fatalf("marshal before: %v", err)
	}

	// The loop calls MaskOutboundScoped on the CLONED provider-bound slice.
	providerBound := cloneMessagesForTransform(msgs)
	masked := hook.MaskOutboundScoped(providerBound, "sess-r23")
	if len(masked) == 0 || !strings.Contains(masked[0].Parts[0].Text, "[PII:") {
		t.Fatalf("fixture failed: provider-bound parts were not masked: %+v", masked)
	}

	// The history-stored Parts must be byte-identical.
	after, err := json.Marshal(msgs[0].Parts)
	if err != nil {
		t.Fatalf("marshal after: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("masking mutated the history-stored Parts:\n before: %s\n after:  %s", before, after)
	}
	if strings.Contains(msgs[0].Parts[0].Text, "[PII:") {
		t.Errorf("history Parts carry a mask token: %q", msgs[0].Parts[0].Text)
	}
	if strings.Contains(msgs[0].Content, "[PII:") {
		t.Errorf("history Content carries a mask token: %q", msgs[0].Content)
	}
	_ = swapper
}
