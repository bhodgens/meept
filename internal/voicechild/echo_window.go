package voicechild

import (
	"strings"

	"github.com/caimlas/meept/internal/util/fuzzy"
)

// BARGE-IN CONTRACT (2026-11-11): this file is the text-provenance lane of
// the local leg's fast barge-in. Read it before "fixing" a phantom
// barge-in by deleting anything here:
//
//  1. Energy (RMS) barge-in trips are NEVER allowed on bare loudness. A
//     trip must be authorized either by a whisper final that POST-DATES
//     the current turn's dispatch (the e0dbce12 watermark — that
//     authorization source stays) or by a recent final whose text is
//     PROVEN NOT to be the agent's own voice (the echo-window rejection
//     implemented here — the second authorization source).
//  2. The echo window exists because the dispatch watermark unmasked the
//     self-answer loop: a whisper final that is the agent's own TTS
//     (speaker -> mic) used to dispatch a full employee turn every time
//     (exact-match dedupe only). Do not delete the window to "fix"
//     phantom barge-in again; the pre-watermark fast lane was fast
//     because the prompt's own final auto-authorized 10 s of echo
//     windows — this window is what makes the fast lane safe to restore.
//  3. (FlushNow contract lives with the whisper adapter: it must never
//     fire recognition on an empty buffered window.)

// echoWindowMaxSentences bounds the rolling agent-speech deque: say
// frames arrive sentence-granular from the daemon talk loop, so 40
// entries cover roughly the last minute-plus of agent speech — well past
// the whisper finalization latency of the sentence currently echoing.
const echoWindowMaxSentences = 40

// echoFuzzyThreshold is the containment score at/above which a final's
// provenance-query is judged to BE the agent's own speech (fuzzy on
// purpose: the mic/whisper path garbles words, drops endings, re-segments
// sentences; a faithful echo never scores a clean 1.0).
const echoFuzzyThreshold = 0.6

// echoMinContentTokens is the provenance floor: a final with fewer than
// this many content tokens (letters-only, >= 4 chars) carries too little
// text to PROVE it is an echo, so it is treated as human speech and
// dispatched. Short echoes ("okay.", "sure.") fall through to dispatch by
// this rule — accepted: the exact-dup dedupe catches their repeats, the
// daemon-side roster leaf and the voice.min_turn_words gate (9f860991)
// carry the short-tail policy. Erring toward hearing the caller is the
// house rule (see isRealSpeech).
const echoMinContentTokens = 2

// echoWindow is the bounded rolling deque of normalized recent AGENT
// speech: one entry per say frame the child is asked to synthesize (the
// child receives every say frame's text — it speaks them), so the
// pipeline always knows what the agent's own voice currently sounds like.
// Guarded by the owning pipeline's playMu (mutated from enqueueSay, read
// from consumeTranscripts and rmsBargeInCheck — both under the same lock).
//
// BARGE-IN CONTRACT (2026-11-11): see the file header — this is
// authorization source #2 for energy trips AND the filter that keeps
// agent-echo finals from dispatching self-answer turns. Cap-only retention
// (no TTL): 40 sentences is already longer than any plausible echo
// round-trip, and a cap cannot grow memory.
type echoWindow struct {
	entries []string // normalized per-say-frame text, oldest first
}

// add records one say frame's text. Empty (post-normalization) text is
// ignored. Beyond the cap the OLDEST entry is evicted (ring semantics via
// slice copy — the deque is tiny).
func (w *echoWindow) add(text string) {
	norm := strings.ToLower(strings.TrimSpace(text))
	if norm == "" {
		return
	}
	w.entries = append(w.entries, norm)
	if len(w.entries) > echoWindowMaxSentences {
		w.entries = append(w.entries[:0:0], w.entries[len(w.entries)-echoWindowMaxSentences:]...)
	}
}

// matches reports whether text fuzzy-MATCHES any entry of the window —
// i.e. its provenance query (the final's content tokens, see
// echoMinContentTokens) is recognizably contained in some sentence the
// agent recently spoke. Caller holds playMu (the pipeline routes through
// echoWindowMatchesLocked which also enforces the lock regime).
func (w *echoWindow) matches(text string) bool {
	query := provenanceQuery(text)
	if query == "" {
		return false // below the provenance floor: cannot prove echo
	}
	for _, e := range w.entries {
		if fuzzy.ContainsRatio(query, e) >= echoFuzzyThreshold {
			return true
		}
	}
	return false
}

// provenanceQuery extracts the content-token skeleton of a transcript for
// echo matching: lowercased tokens of >= 4 letters-only characters,
// space-joined. Function words ("the", "is", "to") carry no provenance
// signal — they appear in every sentence, so a ratio over them
// false-matches a human reply against unrelated agent speech. Garbled
// content words survive (ContainsRatio's per-token fuzzy match);
// punctuation is stripped by the normalize inside ContainsRatio, but
// pure-punct tokens are dropped here so they cannot pad the count.
// Returns "" when fewer than echoMinContentTokens content tokens remain.
func provenanceQuery(text string) string {
	var kept []string
	for _, tok := range strings.Fields(strings.ToLower(text)) {
		tok = strings.TrimFunc(tok, func(r rune) bool {
			return !(r >= 'a' && r <= 'z')
		})
		if len(tok) < 4 {
			continue
		}
		allLetters := true
		for _, r := range tok {
			if r < 'a' || r > 'z' {
				allLetters = false
				break
			}
		}
		if allLetters {
			kept = append(kept, tok)
		}
	}
	if len(kept) < echoMinContentTokens {
		return ""
	}
	return strings.Join(kept, " ")
}

// echoWindowMatchesLocked reports whether trimmed final text is the
// agent's own recent speech. Caller holds playMu.
func (p *pipeline) echoWindowMatchesLocked(text string) bool {
	return p.echoWin.matches(text)
}

// echoWindowTextLocked returns a normalized copy of the current window
// entries for tests/telemetry. Caller holds playMu.
func (p *pipeline) echoWindowTextLocked() []string {
	out := make([]string, len(p.echoWin.entries))
	copy(out, p.echoWin.entries)
	return out
}
