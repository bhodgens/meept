package agent

import (
	"context"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

// Voice-spoken guard (defect A, 2026-11-10 link recording /tmp/link-rec-
// 1794333239 transcript b): the SECRETARY's SPOKEN reply contained the
// loop's honesty label plus the evidence-template vocabulary verbatim —
// "[unverified: no tools were executed this turn] Correction: ...
// tool_call_id call_ad75... success=true ... memory_id 3a0dae..." — and
// TTS read it to the caller.
//
// WHY THIS IS A VOICE-LANE EXTENSION AND NOT A CHANGE TO THE SHARED
// MACHINE-SHAPED GUARD: the chat lane DELIBERATELY ships the
// "[unverified: ...]" label to the user (TestChainStability_
// NudgeBudgetRecovery pins it: the labelled delivery is the honest
// text-lane posture, not a leak). The same text is garbage when spoken —
// the voice persona forbids bracketed labels, identifiers, and code, and
// there is no reading that makes "[unverified colon..." sound like an
// answer. So the shared detectors (classifyReplyGuard, reply_guard.go)
// stay byte-identical for every consumer, and this file adds the
// spoken-lane SUPERSET: the shared shapes plus the machine-shaped forms
// the text lane tolerates for honesty reasons.
//
// SEAM: the voice TurnRunner adapter (internal/daemon/voice_employee.go
// loopTurnRunner.Run) — NOT RunOnceWithParts. RunOnceWithParts is shared
// with the chat lane, and rejecting the label there would change the
// pinned chat behavior; internal/voice may not import internal/agent
// (import_test.go pin), so the adapter is the only legal shared seam that
// sits between the loop reply and the say frames. The adapter applies the
// same posture as the loop seam's guard: one bounded rewrite nudge, then
// a canned spoken line (never a second rewrite).

// VoiceSpokenRewriteNudge is the user-role nudge the voice adapter sends
// when a reply trips the spoken guard: same bracketed [system: ...]
// convention as replyGuardRewriteNudge, spoken-specific remedy.
const VoiceSpokenRewriteNudge = "[system: your previous reply reached the caller as machine-shaped text (tool identifiers, evidence templates, bracketed status labels) read aloud through text-to-speech. rewrite your answer as plain spoken language: short sentences, no identifiers, no JSON, no code blocks, no bracketed labels. say only what the caller needs to hear.]"

// VoiceSpokenFallbackLine replaces a spoken reply when the ONE bounded
// rewrite also trips (or errors): an honest, speakable line — never the
// machine-shaped original. Lowercase per the repo UI-text convention,
// matching the chat guard's canned lines.
const VoiceSpokenFallbackLine = "sorry, that answer came out tangled — could you ask me one more time?"

// spokenLeakTokens are the machine-shaped markers that must never reach a
// say frame. Chosen for zero false positives on the short spoken prose the
// voice personas demand: snake_case platform identifiers, bracketed
// machine labels, fenced code, and quoted JSON keys are never things a
// person says out loud. The recorded leak matched two of them
// ("[unverified:" and "memory_id") before reaching TTS.
var spokenLeakTokens = []string{
	"[unverified:",              // loop honesty label (chat-pinned, spoken-leak)
	"[reasoning-only output",    // reasoning give-up label, same class
	"tool_call_id", "memory_id", // platform result identifiers
	"```",                    // fenced code (persona-forbidden, unreadable aloud)
	`"claims"`, `"evidence"`, // evidence-format JSON keys (template echo)
	"success=true", // tool-result narration shape
	"[system:",     // a nudge echoed back as an answer
}

// SpokenReplyViolation reports whether a reply is machine-shaped for the
// SPOKEN lane, naming the trigger for the WARN. It classifies the shared
// reply-guard shapes FIRST (a raw dump is garbage aloud too — defense in
// depth for adapters that receive a reply before the loop seam ran) plus
// the spoken-lane extension above. Pure and idempotent: a clean prose
// reply returns ("", false) with no side effects, so calling it twice on
// the same text is safe (the "guard runs exactly once" posture at the
// adapter is about the rewrite budget, not classification).
func SpokenReplyViolation(reply string) (string, bool) {
	if strings.TrimSpace(reply) == "" {
		return "", false
	}
	if _, match, replaced := classifyReplyGuard(reply); replaced {
		return "shared:" + match.Rule, true
	}
	for _, tok := range spokenLeakTokens {
		if strings.Contains(reply, tok) {
			return tok, true
		}
	}
	return "", false
}

// RunVoiceSpokenRewriteRetry is the voice lane's bounded rewrite retry —
// the adapter-facing twin of the loop seam's guardRetried retry
// (loop.go response assembly). The voice TurnRunner adapter calls it when
// SpokenReplyViolation trips on a completed turn's reply: the conversation
// already holds the assistant's leaked reply (RunOnceWithParts appends the
// shipped text before returning), so appending the user-role nudge keeps
// the loop protocol and re-entering the turn producer sends it. ONE retry
// only (the caller owns the budget); the re-entered cycle re-runs the
// shared machine-shaped guard at its own seam. A retry that trips the
// spoken guard AGAIN, or errors, ships VoiceSpokenFallbackLine — the same
// "second trip ships the canned line" posture as the chat lane, bounded.
//
// Why the method lives on the loop: the retry must re-enter runTurnProducer
// (the LLM reasoning cycle OR the ACP harness lane), which is loop-private.
// The conversation is fetched by id from the loop's own manager; a missing
// conversation (cleared mid-call) re-seeds the anchor so the rewrite turn
// still carries the persona's validation prose.
func (l *AgentLoop) RunVoiceSpokenRewriteRetry(ctx context.Context, conversationID string) (string, error) {
	conv, err := l.conversations.GetOrRestore(conversationID, func() ([]llm.ChatMessage, error) {
		if l.conversationRestoreFn == nil {
			return nil, nil
		}
		return l.conversationRestoreFn(conversationID)
	})
	if err != nil {
		l.logRestoreFailure(conversationID, err)
	}
	if conv.Len() == 0 {
		// Defensive re-seed: an anchor-only-fresh conversation still needs
		// the validation prose for the rewrite turn.
		if v := l.buildValidationAnchorInstructions(); v != "" {
			conv.AddAnchorMessage(llm.RoleSystem, v)
		}
	}
	conv.AddUserMessage(VoiceSpokenRewriteNudge)
	conv.Truncate()
	retry, retryErr := l.runTurnProducer(ctx, conv, conversationID, VoiceSpokenRewriteNudge)
	// reasoningCycle does NOT run RunOnceWithParts' StripThinking defer on
	// a direct re-entry (the 2026-11-09 thinking-token leak fix lives at
	// the outer seam); the spoken lane was the reason that fix shipped, so
	// this seam owns it for the rewrite text.
	retry = llm.StripThinking(retry)
	// C23 (bughunt 2026-11-10): the rewrite text must pass through the SAME
	// output-side lanes the chat lane's rewrite posture runs at its seam
	// (loop.go response assembly): PII/secret restore FIRST (the rewrite can
	// parrot [PII:*] tokens minted by the masking lane — placeholders must
	// never be persisted raw or read aloud), then the credential scan
	// (restored credentials are redacted before the adapter persists and
	// speaks the text). Nil/disabled hook keeps the restore a passthrough.
	if hook := l.PiiRestoreHook(); hook != nil {
		retry = hook.RestoreOutput(retry, l.SessionID())
	}
	if l.securityOrch != nil {
		if scannedText, hasCredentials, warnings := l.securityOrch.ScanOutput(retry); hasCredentials {
			l.logger.Warn("Credentials detected in voice spoken rewrite",
				"conversation_id", conversationID,
				"warnings", len(warnings),
			)
			retry = scannedText
		}
	}
	final := retry
	_, secondTrip := SpokenReplyViolation(retry)
	switch {
	case retryErr != nil:
		final = VoiceSpokenFallbackLine
	case strings.TrimSpace(retry) == "":
		// Parked retry (provider wait) or blank completion: ship the
		// honest speakable line — silence on a live call reads as a
		// dropped connection, and a voice call never quota-parks.
		final = VoiceSpokenFallbackLine
	case secondTrip:
		// Second trip: the rewrite came back machine-shaped again — the
		// budget is spent (the chat lane's rule), canned line ships.
		final = VoiceSpokenFallbackLine
	}
	// reasoningCycle does NOT append its final text (RunOnceWithParts'
	// response assembly does, loop.go); calling the producer directly means
	// this seam owns the append, or the next turn lands user->user and
	// strict providers reject the transcript shape. StripThinking (the
	// 2026-11-09 thinking-token leak fix) likewise lives at the
	// RunOnceWithParts defer, so this seam applies it to the rewrite too —
	// the spoken lane was the reason that fix shipped.
	conv.AddAssistantMessage(llm.StripThinking(final))
	return final, nil
}
