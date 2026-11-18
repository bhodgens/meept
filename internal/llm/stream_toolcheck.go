package llm

// Streamed + assembled tool-call validation (Contract C9,
// sovereign-router-parity plan 20261115 leaf 03; source reference:
// Sovereign Router src/validation.py:78-126 validate_response /
// ToolCallAssembler / validate_streamed_text_outcome).
//
// meept catches an EMPTY completion everywhere (ErrEmptyResponse, classified
// by every client). It did not catch a MALFORMED tool call: a model emitting
// {"name": "shel", "arguments": "{not json"} — or naming a function it was
// never offered — reached the tool executor, which then failed or silently
// did nothing. This file rejects both shapes BEFORE the executor sees them.
//
// Contract (frozen semantics, master.md C9):
//   - no tool calls            -> nil (nothing to validate)
//   - offered list EMPTY       -> nil for ANY call
//   - name not in offered      -> ErrToolCallInvalid
//   - arguments a JSON object  -> pass
//   - valid JSON, NOT an object-> ErrToolCallInvalid
//   - arguments unparseable    -> ErrToolCallInvalid
//   - arguments empty string   -> pass (some providers send "" for a
//     no-argument call)
//
// The "offered list empty -> nil" rule is deliberate: meept has many no-tool
// turns, and a call arriving there is a DIFFERENT defect class (the model
// hallucinating a tool it was never told about) that this check must not
// conflate with a malformed call. nil offered therefore means "no contract
// to validate against", never "everything passes by inspection".
//
// Validation runs AFTER a successful parse and BEFORE the executor. It does
// NOT replace ErrEmptyResponse classification: empty-completion handling
// stays byte-identical at its existing sites (every client, before the
// empty-retry; see internal/llm/AGENTS.md), and the refusal/quota/overflow
// early-exit ordering above it is untouched.
//
// WIRING SPLIT (regression fix, 2026-11-17 — sovereign-parity leaf 03 over-
// reach): the exported ValidateToolCalls keeps the FULL frozen table above
// (strict verdict — a tool call naming an unoffered function is invalid by
// contract; the sovereign-parity gate T19 pins it). The CLIENT TRANSPORT
// wiring (openai + anthropic + codex, streaming and non-streaming) enforces
// ONLY the arguments-shape half: malformed/non-object args are fatal there
// because no downstream can execute them. Name-membership decisions stay
// with the executor's audited, RECOVERABLE gates, which predate C9 and are
// pinned by their own suites: the group-deny verdict (agent-groups F1/F7 —
// a group-denied tool is BY DESIGN absent from the offered list, and its
// call must reach the executor to land the rule_source=group_deny decision
// line), the unknown-tool error result (fed back to the model), and the
// dynamic-exposure row-4 recovery directive ("call tool_view{x} first" —
// meaningless if the transport refuses the call fatally before the executor
// can answer it). A transport-layer fatal on an unoffered name killed all
// three flows (agent-groups F1/F7 + the golden-capture scripted unknown-tool
// regression this fix closes).
//
// Streaming rule (validation.py:113-126, verbatim intent): streamed
// tool-call chunks are buffered to completion and validated BEFORE anything
// is sent to the client. In meept terms: the delta path assembles tool calls
// into the final Response (client.go doStreamRequest's accumulator build,
// codex_sse.go's codexStreamAccumulator, anthropic.go's buildResponseFromBlocks
// block accumulation); validation runs ONCE on the ASSEMBLED response at
// stream end — never per chunk, so a fragment split across SSE events can
// never produce a false pass or a false fail.

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrToolCallInvalid is returned when an assembled tool call names a
// function absent from the request's offered list, or carries arguments
// that do not parse as a JSON object (Contract C9, sovereign-router-parity
// leaf 03).
//
// Like ErrEmptyResponse, it is a SENTINEL and every return path surfaces the
// BARE value: callers pin it by POINTER identity (err == ErrToolCallInvalid),
// never errors.Is through a wrap. The client retry loops classify it as a
// non-retryable terminal error (Classify(0) -> FailureNone -> immediate
// return): retrying the SAME model cannot repair a call it already produced,
// so the verdict belongs to the alias-rotation layer, not the short-retry
// budget. It never parks a turn and never arms a failure record of its own —
// the generic failure branch records it like any fatal error.
var ErrToolCallInvalid = errors.New("llm: tool call failed validation")

// OfferedToolNames extracts the function names from the request's tool
// definitions, in declaration order, de-duplicated. The nil-tools case
// returns nil (not an empty slice): nil is the caller's "no contract"
// signal that disables name validation entirely (see ValidateToolCalls).
func OfferedToolNames(tools []ToolDefinition) []string {
	if len(tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(tools))
	seen := make(map[string]struct{}, len(tools))
	for _, t := range tools {
		name := t.Function.Name
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

// ValidateToolCalls returns ErrToolCallInvalid (the BARE sentinel, pointer
// identity) when any tool call in resp names a function absent from offered,
// or when its arguments do not parse as a JSON object. A response with no
// tool calls returns nil; an EMPTY offered list returns nil for ANY call
// (the caller offered no tools, so there is no contract to violate — a call
// there is the model hallucinating a tool it was never told about, a
// different defect this check deliberately does not own).
func ValidateToolCalls(resp *Response, offered []string) error {
	if resp == nil {
		return nil
	}
	return validateToolCalls(resp.ToolCalls, offered)
}

// validateToolCalls is ValidateToolCalls over an explicit call slice (the
// client wiring validates pre-assembly slices too, so the failure name can
// be reported per call before the Response object exists).
func validateToolCalls(toolCalls []ToolCall, offered []string) error {
	if len(toolCalls) == 0 {
		return nil
	}
	if offered == nil {
		// Offered list empty: no contract to validate against (see the
		// package comment). A call on a no-tool turn is a different bug
		// class; this check must not conflate it with a malformed call.
		return nil
	}
	valid := make(map[string]struct{}, len(offered))
	for _, name := range offered {
		valid[name] = struct{}{}
	}
	for i := range toolCalls {
		call := &toolCalls[i]
		if _, ok := valid[call.Function.Name]; !ok {
			return ErrToolCallInvalid
		}
		if !toolCallArgsAreObject(call.Function.Arguments) {
			return ErrToolCallInvalid
		}
	}
	return nil
}

// toolCallArgsAreObject reports whether arguments is a valid JSON object,
// the frozen empty-string pass (providers send "" for a no-argument call),
// or anything else. Only {"...": ...} objects pass: a JSON array, scalar,
// or string is a malformed payload the executor cannot parameterize.
func toolCallArgsAreObject(args string) bool {
	trimmed := trimJSONSpace(args)
	if trimmed == "" {
		// Frozen: empty arguments pass — some providers send "" for a
		// no-argument call.
		return true
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return false
	}
	return true
}

// trimJSONSpace trims ASCII whitespace the provider may pad around the
// arguments payload (hand-rolled only over space/tab/newline/CR — the
// audit-utf8-byte-arithmetic rule forbids byte-level case/space arithmetic
// that could corrupt multibyte content inside the payload; json.Unmarshal
// itself is whitespace-tolerant, so this only guards the empty check).
func trimJSONSpace(s string) string {
	start := 0
	for start < len(s) {
		switch s[start] {
		case ' ', '\t', '\n', '\r':
			start++
		default:
			return trimJSONSpaceEnd(s[start:])
		}
	}
	return ""
}

func trimJSONSpaceEnd(s string) string {
	end := len(s)
	for end > 0 {
		switch s[end-1] {
		case ' ', '\t', '\n', '\r':
			end--
		default:
			return s[:end]
		}
	}
	return ""
}

// validateOfferedToolCalls is the client-wiring helper (Contract C9 leaf 03,
// narrowed by the 2026-11-17 wiring-split fix above): it enforces ONLY the
// arguments-shape half of the frozen table — malformed/non-object args are
// fatal because nothing downstream can execute them. NAME membership is
// deliberately NOT checked here: the executor owns the audited, recoverable
// verdicts for a call to a tool absent from the offered list (group_deny
// decision line, unknown-tool error result, dynamic-exposure recovery
// directive) — a transport-layer fatal there killed agent-groups F1/F7 and
// the golden-capture scripted flow (see the package comment).
//
// offered == nil — the request offered no tools, or the transport never
// records one (a bare client reconfigured onto a codex backend, an executor
// calling without WithTools) — still disables the check entirely, keeping
// no-tool turns byte-identical to pre-C9 legacy.
//
// Returns the BARE ErrToolCallInvalid sentinel on the first malformed call.
func validateOfferedToolCalls(toolCalls []ToolCall, offered []string) error {
	if len(toolCalls) == 0 || offered == nil {
		return nil
	}
	for i := range toolCalls {
		if !toolCallArgsAreObject(toolCalls[i].Function.Arguments) {
			return ErrToolCallInvalid
		}
	}
	return nil
}

// firstInvalidToolCall renders the first offending call for the client Warn
// log (the incident shape: truncated/non-object arguments reaching the
// executor). Best-effort diagnostics only — the verdict is always the bare
// sentinel. Matches the client wiring's args-only scope: an unoffered NAME
// is not reported here because the transport no longer rejects on names
// (validateOfferedToolCalls); the exported ValidateToolCalls verdict is
// used by contract-level callers directly.
func firstInvalidToolCall(toolCalls []ToolCall, offered []string) string {
	if offered == nil {
		return ""
	}
	for i := range toolCalls {
		call := &toolCalls[i]
		if !toolCallArgsAreObject(call.Function.Arguments) {
			return fmt.Sprintf("tool_args_invalid_json:%s", call.Function.Name)
		}
	}
	return ""
}
