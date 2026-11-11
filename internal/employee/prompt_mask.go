package employee

import "github.com/caimlas/meept/internal/llm"

// Reflector-bound send masking (bughunt C20): the goal loop's ASSESS and
// REFLECT sends and the enforcement auditors' post-turn / periodic audit
// sends carry the employee's constitution + RAW execution results to the
// reflector model with no PII masking — the main chat turn's mask lane
// (agent.PiiRestoreHook.MaskOutboundScoped, applied in loop.go) never
// reached these employee-side sends. The helpers here apply the same
// session-scoped mask lane to those prompts before they cross to the
// provider.
//
// PromptMaskHook is a local interface (not the concrete *agent.PiiRestoreHook)
// so enforcement.go needs no new import and tests can stub the lane. The
// daemon wires the real hook through SetPromptMaskHook /
// SetPiiPromptMaskHook; every seam is nil-guarded so masking-off behavior
// stays byte-identical.
type PromptMaskHook interface {
	// MaskOutboundScoped mirrors agent.PiiRestoreHook.MaskOutboundScoped:
	// replaces detected PII spans in message text with session-scoped
	// tokens, returning the messages mutated in place. A disabled hook
	// returns the input unchanged.
	MaskOutboundScoped(messages []llm.ChatMessage, sessionID string) []llm.ChatMessage
}

// maskPromptText masks user-derived text bound for a reflector/auditor
// prompt. Nil hook, empty text, or empty session scope => the input is
// returned byte-identically (tokens minted without a session scope could
// never be restored, so scope-less masking is refused, matching the
// agent-side discipline).
func maskPromptText(hook PromptMaskHook, text, sessionID string) string {
	if hook == nil || text == "" || sessionID == "" {
		return text
	}
	masked := hook.MaskOutboundScoped([]llm.ChatMessage{{Role: llm.RoleUser, Content: text}}, sessionID)
	return masked[0].Content
}

// maskPromptMessages masks a message slice bound for a reflector/auditor.
// Nil hook or empty session scope => input byte-identical.
func maskPromptMessages(hook PromptMaskHook, messages []llm.ChatMessage, sessionID string) []llm.ChatMessage {
	if hook == nil || sessionID == "" {
		return messages
	}
	return hook.MaskOutboundScoped(messages, sessionID)
}
