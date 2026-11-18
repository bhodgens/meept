package voice

// CROSS-CALL ECHO CONTRACT (2026-11-11):
//
//  1. Two local voice calls on ONE machine share the room: call A's agent
//     TTS plays out the speakers, call B's mic captures it, whisper
//     finals it, and B dispatches a turn ANSWERING A — the operator heard
//     "two agents answering each other" in two different TTS voices
//     (audit .hermes/audits/2026-11-11-voice-fix-regression-review.md
//     symptom (b): secretary local-496a3fd0d2ab3fc6 vs companion
//     local-8f64d5265ba98660 overlapping 14:58-15:01 in calls.db). This
//     process-wide roster is the ONLY cross-call filter: every say the
//     daemon talk loop emits is recorded here per ccid, and a FINAL
//     prompt that fuzzy-matches agent speech recorded for a DIFFERENT
//     call inside the TTL is dropped at the talk loop's prompt intake
//     instead of dispatching a turn.
//  2. It COMPLEMENTS — never replaces — the child-side per-call echo
//     window (internal/voicechild rmsEchoMargin + dispatch watermark):
//     the child only knows its OWN call's outbound audio, so cross-call
//     echo is invisible to it. The roster likewise excludes same-ccid
//     matches: A's own speech NEVER gates A's prompts (self-echo stays
//     the child window's job). Pinned by TestEchoRosterOwnCallExcluded.
//     LINK calls are EXEMPT from the gate (talkloop checks
//     deps.HoldPrompt == nil): a link is a DELIBERATE two-agent
//     conversation whose documented seed is A's greeting arriving in
//     B's mic as B's first final prompt, so gating there would break
//     the turn-taking contract. Link speech is still RECORDED at the
//     say choke, so a plain local/phone call that overhears a link
//     conversation still drops the echo.
//  3. Dropping a verbatim human quote of another agent is an ACCEPTED
//     policy loss: humans paraphrase, so a real caller rarely reaches
//     the coverage floor, and the child-side echo window plus the
//     short-prompt floor below catch the remaining ambient cases.
//  4. Do NOT weaken this gate to silence "missed turn" complaints
//     without first adding a config knob. A future
//     voice.cross_call_echo=true|false (default true) is the sanctioned
//     escape hatch; it is deliberately NOT added now because it would
//     touch internal/config/schema.go, and any such knob must be
//     byte-identical to pre-fix behavior when flipped off.
//
// Scoring posture: the fuzzy primitive is REUSED, not re-implemented —
// internal/util/fuzzy (commit aa0888c1, stdlib-only, already inside this
// package's dependency closure; voicechild never imports voice, so there
// is no cycle). The package's FuzzyFind/Similarity formula averages only
// the TOP TWO query tokens (a proper-noun name matcher: "Meadow Rich" vs
// "Meadow Ridge"); applied to whole sentences that shape false-positives
// badly (two shared exact words in long unrelated sentences score 1.0).
// So the roster applies the exported per-token Similarity at TOKEN
// granularity and gates on COVERAGE: the fraction of prompt tokens with
// at least one close token in another call's recorded speech must clear
// echoTokenCoverageFloor. A real speaker->mic echo (same words, whisper
// retranscription garble) lands near 1.0; unrelated speech stays near 0.
//
// Concurrency: mutex-guarded slice, pure in-memory bookkeeping — the repo
// mutex-scope rule (never hold a mutex across I/O) holds trivially since
// the roster performs no I/O at all. Entry count is bounded
// (echoRosterMaxEntries) and entries age out by TTL, with an explicit
// per-ccid clear on call teardown.

import (
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/util/fuzzy"
)

const (
	// echoRosterTTLDefault: how long recorded agent speech keeps gating
	// other calls' prompts. Echoes arrive seconds after the say (playback
	// tail + whisper flush), so 30 s is generous; a stale entry only costs
	// a rare over-drop inside an ongoing same-room conversation.
	echoRosterTTLDefault = 30 * time.Second
	// echoRosterMaxEntries bounds the roster (FIFO by age, oldest evicted).
	// A two-sentence reply per turn across a handful of concurrent calls
	// is a few dozen entries; 128 is roomy for a busy machine.
	echoRosterMaxEntries = 128
	// echoMinPromptWords is the short-prompt floor: prompts with fewer
	// than this many words NEVER consult the roster. Two independent
	// reasons, both load-bearing: (a) at very low token counts the
	// coverage floor cannot separate an echo from coincidence (a 2-word
	// "hello there" echoes everywhere); (b) short finals are ambient
	// noise, and the in-flight sibling leaf 9f860991 min_turn_words gate
	// (voice.min_turn_words, present in the voice-microfrag worktree but
	// NOT yet on this tree's config schema — grep internal/config/schema.go
	// shows no key) is the designed owner of short ambient finals. Once
	// that gate lands the two floors overlap HARMLESSLY: min_turn_words
	// drops short finals by LENGTH for every call; this floor merely
	// declines to run the echo scorer on them. Do not "fix" the overlap
	// by lowering this constant — it protects against false drops.
	echoMinPromptWords = 4
	// echoTokenSimFloor is the per-token similarity (fuzzy.Similarity on
	// single tokens) at which a prompt token counts as matched by a
	// recorded token. 0.75 admits whisper garble ("redy" ~ ready,
	// 1-1/4=0.75; "rep" vs "report" 0.5 stays unmatched — coverage floor
	// absorbs isolated splits) while rejecting lookalike coincidence.
	echoTokenSimFloor = 0.75
	// echoTokenCoverageFloor: the fraction of prompt tokens that must
	// match before a prompt is called an echo. A verbatim-ish speaker
	// echo scores ~1.0; unrelated conversation pairs measured <0.4 on
	// realistic sentences.
	echoTokenCoverageFloor = 0.62
)

// Seams (mirrors silenceWindowUnit's posture: production constants, test
// suite shrinks them; package tests run sequentially — no test in the
// echo path calls t.Parallel).
var (
	echoRosterTTL = echoRosterTTLDefault
	echoRosterMax = echoRosterMaxEntries
)

// echoEntry is one recorded agent utterance: the speaking call's id and
// its pre-tokenized words (fields of the raw say text, normalized the
// same way fuzzy.Similarity normalizes: tokens that collapse to nothing
// under fuzzy's lower/alphanumeric-only rule are dropped at STORE time —
// "..." and "--" never become matchable entries).
type echoEntry struct {
	ccid   string
	tokens []string
	at     time.Time
}

// echoLiveTokens splits text into the tokens the scorer actually sees:
// fuzzy.Similarity lowercases and strips non-alphanumerics inside each
// token, and two punctuation-only tokens both normalize to empty and
// score 1.0 (fuzzy.go Similarity maxLen==0 branch) — a whisper "..."
// free-matched agent say-text punctuation and deflated real coverage.
// Dead tokens are dropped HERE, at both store and query time, so they
// never count toward echoCoverageNeed or entry size; the same rule the
// fuzzy primitive applies is applied once, up front.
func echoLiveTokens(text string) []string {
	var out []string
	for tok := range strings.FieldsSeq(text) {
		if fuzzy.NormalizeToken(tok) == "" {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// echoRoster is the process-wide registry of recently SPOKEN agent text
// across ALL live calls. See the CROSS-CALL ECHO CONTRACT block above.
type echoRoster struct {
	mu      sync.Mutex
	entries []echoEntry // chronological append order (oldest first)
}

// crossCallEcho is the single daemon-process roster (package-level state
// keyed by call id — registry-free wiring, as contracted in the task).
var crossCallEcho = &echoRoster{}

// RecordSpoken appends one agent utterance spoken by ccid to the
// process-wide cross-call echo roster. The talk loop calls it at its
// single say choke (greeting, filler, replies, fallback — every audible
// agent sentence). Safe on empty text/ccid (no-op).
//
// CROSS-CALL ECHO CONTRACT (2026-11-11): see the block comment atop this
// file — this roster is the ONLY cross-call filter; it complements the
// child-side per-call echo window, never replaces it.
func RecordSpoken(ccid, text string) { crossCallEcho.record(ccid, text) }

// RecentOtherSpeech reports whether the transcript fuzzy-matches agent
// speech recorded for a DIFFERENT live call within the roster TTL. The
// talk loop gates final prompts with it: true = drop the prompt instead
// of dispatching (kills "two agents answering each other"). ccid's own
// speech NEVER matches (rule 2: self-echo belongs to the child window).
func RecentOtherSpeech(ccid, text string) bool {
	return crossCallEcho.recentOther(ccid, text)
}

// ClearSpoken drops every roster entry for a call at teardown (hygiene —
// TTL aging makes leaks self-healing within 30 s).
func ClearSpoken(ccid string) { crossCallEcho.clear(ccid) }

// record appends one agent utterance for ccid. Empty text is a no-op:
// the talk loop's turn-final flush marker is say(ccid, "", true), which
// carries no speech and must not become a matchable entry.
func (r *echoRoster) record(ccid, text string) {
	if ccid == "" || strings.TrimSpace(text) == "" {
		return
	}
	tokens := echoLiveTokens(text)
	if len(tokens) == 0 {
		return
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(now)
	r.entries = append(r.entries, echoEntry{ccid: ccid, tokens: tokens, at: now})
	// Bound the roster FIFO (age order == append order after prune).
	if over := len(r.entries) - echoRosterMax; over > 0 {
		r.entries = append(r.entries[:0], r.entries[over:]...)
	}
}

// recentOther reports whether text fuzzy-matches agent speech recorded
// for a call OTHER than ccid within echoRosterTTL. Same-ccid entries are
// invisible here by contract (rule 2: self-echo belongs to the child
// window); prompts under echoMinPromptWords skip the scorer entirely
// (see the constant's two-part rationale).
func (r *echoRoster) recentOther(ccid, text string) bool {
	query := echoLiveTokens(text)
	if ccid == "" || len(query) < echoMinPromptWords {
		return false
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(now)
	for _, e := range slices.Backward(r.entries) {

		if e.ccid == ccid {
			continue // own call never gates own prompts (rule 2)
		}
		// An entry with fewer tokens than the coverage need can never match.
		if len(e.tokens) < echoCoverageNeed(len(query)) {
			continue
		}
		if echoCoverageMet(query, e.tokens) {
			return true
		}
	}
	return false
}

// echoCoverageNeed is the ceil of coverageFloor * n query tokens — the
// minimum number of matched tokens for an echo verdict.
func echoCoverageNeed(n int) int {
	need := max(int(math.Ceil(float64(n)*echoTokenCoverageFloor)), 1)
	return need
}

// clear drops every entry belonging to a call (teardown hook; TTL aging
// makes this hygiene, not correctness — a leaked entry simply expires).
func (r *echoRoster) clear(ccid string) {
	if ccid == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := r.entries[:0]
	for _, e := range r.entries {
		if e.ccid != ccid {
			kept = append(kept, e)
		}
	}
	// Clear the tail slots so evicted entries don't pin their token
	// slices, then truncate.
	for i := len(kept); i < len(r.entries); i++ {
		r.entries[i] = echoEntry{}
	}
	r.entries = kept
}

// len is a test seam for the bounded-entry and teardown pins.
func (r *echoRoster) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

// pruneLocked drops entries older than the TTL. Caller holds r.mu. Pure
// bookkeeping — no I/O (repo mutex-scope rule).
func (r *echoRoster) pruneLocked(now time.Time) {
	cutoff := now.Add(-echoRosterTTL)
	drop := 0
	for _, e := range r.entries {
		if e.at.Before(cutoff) {
			drop++
			continue
		}
		break
	}
	if drop > 0 {
		r.entries = append(r.entries[:0], r.entries[drop:]...)
	}
}

// echoCoverageMet reports whether enough query tokens have a close match
// in the entry's tokens. Per-token closeness REUSES fuzzy.Similarity
// (single-token inputs collapse its top-two average to the raw token
// similarity — the aa0888c1 primitive, not a second scorer).
func echoCoverageMet(query, entry []string) bool {
	matched := 0
	need := echoCoverageNeed(len(query))
	for _, q := range query {
		for _, s := range entry {
			if fuzzy.Similarity(q, s) >= echoTokenSimFloor {
				matched++
				break
			}
		}
	}
	return matched >= need
}
