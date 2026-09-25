package llm

// WithAssistantPrefill sets a partial assistant message appended after the
// conversation messages: llama-server continues it instead of generating
// free-form prose (prefill / assistant-prefill, server README 2026-09).
//
// Why: the LFM2.5-8B GGUF is not tool-trained. Live probes (2026-09-24,
// issue #57) measured tool_choice=required honored only 2/5 — the server
// accepts the field but the model has no tool-call format to constrain. A
// HALF-WRITTEN tool call continued to the complete, correct call 1/1; the
// platform-side prefill retry (loop.go unbacked-claims nudge) uses the same
// mechanism.
//
// Wire format: the prefill rides as a final assistant chat message. The
// server continues the assistant turn from it. When the model completes the
// call with native <|tool_call_start|> markers the existing marker parser
// recovers it; when it completes with bare JSON (the LFM2.5 template parser
// only recognizes markers, so the continuation lands in `content` as the
// tail of the prefill's opened object) parseLFMPrefillContinuation
// reconstructs the call — prefill- and shape-gated, see lfm_tool_calls.go.
func WithAssistantPrefill(content string) ChatOption {
	return func(o *chatOptions) {
		o.assistantPrefill = content
	}
}
