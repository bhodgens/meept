package agent

import "github.com/caimlas/meept/internal/llm"

// Secondary classifier side sends (finding C21): the main turn's PII
// pre-LLM masking pass (loop.go, MaskOutboundScoped on the cloned
// provider-bound messages) never reaches the side sends the classifier
// components make — the user's raw message used to reach those prompts
// unmasked. The helpers here apply the SAME session-scoped mask lane
// (hook.MaskOutboundScoped) to those prompts before they cross to the
// provider. Every helper is nil-refusing: a nil hook or an unresolvable
// session scope returns the input byte-identically, so masking-off
// behavior is unchanged.

// sideSendSessionSource is the minimal seam for resolving the session
// scope of a side send: the owning agent loop answers SessionID.
type sideSendSessionSource interface {
	SessionID() string
}

// sideSendSessionID resolves the PII-mask session scope for a secondary
// classifier send from the component's chat scope provider (the owning
// loop, per the leaf-01 session-affinity wiring). Any other provider —
// or a nil/typed-nil loop — yields "", and the mask helpers then leave
// the content untouched: tokens minted without a session scope could
// never be restored (MaskOutboundScoped refuses scope-less masking).
func sideSendSessionID(provider chatScopeProvider) string {
	src, ok := provider.(sideSendSessionSource)
	if !ok || src == nil {
		return ""
	}
	// Typed-nil concrete pointers box non-nil into the interface
	// (typed-nil wiring rule): guard the in-tree concrete source
	// explicitly before dereferencing.
	if l, isLoop := provider.(*AgentLoop); isLoop && l == nil {
		return ""
	}
	return src.SessionID()
}

// maskSideSendMessages applies the PII mask lane to a side-send message
// slice before it crosses to the provider. Nil hook or empty session
// scope => the input is returned byte-identical (MaskOutboundScoped's own
// refusal discipline mirrors the loop's disabled path).
func maskSideSendMessages(hook *PiiRestoreHook, messages []llm.ChatMessage, sessionID string) []llm.ChatMessage {
	if hook == nil {
		return messages
	}
	return hook.MaskOutboundScoped(messages, sessionID)
}

// maskSideSendText is the single-string form for prompt builders that
// interpolate user-derived text into scaffolding before wrapping it in a
// message. Nil hook, empty text, or empty session scope => input returned
// byte-identical.
func maskSideSendText(hook *PiiRestoreHook, text, sessionID string) string {
	if hook == nil || text == "" {
		return text
	}
	masked := hook.MaskOutboundScoped([]llm.ChatMessage{{Role: llm.RoleUser, Content: text}}, sessionID)
	return masked[0].Content
}
