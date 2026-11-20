package agent

// Trivial-question fast lane (04-clutter/02, F-ORCH-5, gate T5).
//
// Frozen defect: "What is 2+2? Answer briefly." took the FULL task
// machinery (intent=quickplan @0.8575 -> orchestrator task -> planner
// loop -> reply guard -> repair retry -> step job -> review ->
// task.completed relay): a 5s wall and ~15k tokens for a one-token
// answer, double-rendered by the TUI as card + bubble.
//
// The lane is a CLASSIFICATION shortcut, never a bypass: it runs at the
// tail of LLMClassifier.Classify, INSIDE the same
// analyze -> classify -> RouteToAgent chain, so sanitization (the PII
// mask applied before the send), thread resolution, and session binding
// (RouteToAgent resolves the session-bound loop/working dir exactly as
// for any chat verdict) all stay intact. It rewrites a TRIVIAL-PATTERN
// input's task-creating verdict (quickplan/code/...) to the chat lane --
// no task row (chat does not ShouldCreateTask), synchronous dispatch,
// one brief reply.
//
// Guards against over-reach:
//   - only inputs matching trivialQuestionRe AND whose question subject
//     is a bare arithmetic expression qualify;
//   - trailing brevity instructions ("Answer briefly.") are stripped
//     before the subject check (bounded list, no general NLP);
//   - compound signals, file-path/code tokens, fact-recall phrasing, or
//     a leading execution imperative veto the lane, so artifact verbs
//     ("create a file named notes.txt ...") always keep task routing;
//   - the deterministic short_message_guard / media guards upstream run
//     BEFORE this, and every non-matching verdict passes through
//     byte-identically.
//
// Config: orchestrator.trivial_fast_lane (Q2 ratified: default ON).
// The Dispatcher-side latch follows the SessionStateUpgrade precedent
// (DispatcherConfig mirror); the classifier-side copy is inverted
// (trivialFastLaneDisabled) so the zero value means "enabled" -- the
// gate's newTrivialLaneDispatcher constructs through the protected
// NewDispatcher and must be default-on without touching it.

import (
	"regexp"
	"strings"

	"github.com/caimlas/meept/internal/config"
)

// trivialQuestionRe matches an interrogative lead whose subject is a
// short question body: "what is 2+2", "whats 3*4", "how much is 10/2",
// "calculate 7 minus 3", "is 9 prime". The body must then pass
// trivialArithmeticSubject for the lane to fire.
var trivialQuestionRe = regexp.MustCompile(
	`^(?:what(?:'s| is| are)?|how much is|how many is|calculate|compute|solve|evaluate|is)\s+(.+)$`)

// trivialBrevityRe matches TRAILING brevity instructions -- the "keep it
// short" tail of the frozen incident input. Bounded list on purpose: a
// general imperative strip could swallow real task clauses.
var trivialBrevityRe = regexp.MustCompile(
	`\s*(?:but\s+)?(?:answer|reply|respond|explain|say)\s+(?:it\s+)?(?:briefly|concisely|short|in short|one (?:word|line|sentence)|as (?:a )?short \w+)\s*[.!?]*$`)

// trivialPathLikeRe rejects inputs carrying path/code/artifact tokens --
// a "/"-containing subject is a file reference, not arithmetic
// (AR-1 lesson: "what is the bug in src/parser.go?" must reach the
// classifier).
var trivialPathLikeRe = regexp.MustCompile("[/\\\\~`]")

// isTrivialQuestion reports whether input is a trivial arithmetic-style
// question that the fast lane may answer inline. Purely structural --
// nothing is evaluated here.
func isTrivialQuestion(input string) bool {
	s := strings.ToLower(strings.TrimSpace(input))
	if s == "" || len(s) > 160 {
		return false
	}
	// Compound multi-work phrasing is real work -- never trivial.
	if hasCompoundSignalWords(s) {
		return false
	}
	// A conversation-fact question ("what was the codeword I told you")
	// belongs to the memory-recall lane, not this one.
	if isFactRecallQuestion(s) {
		return false
	}
	// A leading execution imperative is an instruction, not a question.
	if hasLeadingImperativeVerb(s) {
		return false
	}
	m := trivialQuestionRe.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	subject := strings.TrimSpace(m[1])
	// Strip trailing brevity instructions ("2+2? answer briefly"), then
	// trailing sentence punctuation.
	subject = strings.TrimSpace(trivialBrevityRe.ReplaceAllString(subject, ""))
	subject = strings.Trim(strings.TrimSpace(subject), "?!. ")
	if subject == "" {
		return false
	}
	if trivialPathLikeRe.MatchString(subject) {
		return false
	}
	// The subject must be a bare arithmetic expression ("2+2"). The
	// classifier's task-creating verdict for THIS shape is the
	// over-classification the lane pre-empts; anything else keeps the
	// verdict untouched.
	return isArithmeticExpression(subject)
}

// applyTrivialFastLane rewrites a task-creating classifier verdict to
// the chat lane when (and only when) the input is a trivial question.
// Non-matching (verdict, input) pairs return the intent untouched --
// byte-identical legacy behavior, including when the flag is off (the
// caller gates on the latch).
func applyTrivialFastLane(intent *Intent, input string) *Intent {
	if intent == nil {
		return nil
	}
	it := IntentType(intent.Type)
	// Only task-creating verdicts are candidates; an inline chat verdict
	// needs no rewrite and a clarify/platform verdict stays as-is.
	if !it.ShouldCreateTask() && !it.ShouldDispatchAsync(it.RequiresPlanning()) {
		return intent
	}
	if !isTrivialQuestion(input) {
		return intent
	}
	rewritten := *intent
	rewritten.Type = string(IntentChat)
	rewritten.AgentType = config.AgentIDChat
	rewritten.RequiresPlanning = false
	rewritten.Method = "trivial_fast_lane"
	if rewritten.Confidence < 0.85 {
		// Match the chat-lane confidence the short_message_guard uses so
		// the async gate and downstream consumers see the documented
		// inline-chat shape.
		rewritten.Confidence = 0.85
	}
	return &rewritten
}

// SetTrivialFastLane mirrors orchestrator.trivial_fast_lane onto the
// dispatcher's classification lane (04-clutter/02). It exists as a
// method -- not a DispatcherConfig field -- because the NewDispatcher
// body lives in dispatcher.go (sibling-protected this wave) and the
// shipped default is ON via the classifier's inverted latch, so the
// only wiring the flag needs is THIS call when config says otherwise.
// enabled=false disables the lane on the LLM classifier (byte-identical
// legacy classification); the daemon wires it from
// cfg.Orchestrator.TrivialFastLane in components.go.
func (d *Dispatcher) SetTrivialFastLane(enabled bool) {
	if d == nil {
		return
	}
	if d.llmClassifier != nil {
		d.llmClassifier.SetTrivialFastLane(enabled)
	}
}
