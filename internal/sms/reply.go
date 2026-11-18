package sms

// Reply/consent keyword classifier + pending-confirmation registry
// (leaf 04-replies-consent/01, contract master.md §7; gate: §K1/§K2 in
// reply_test.go).
//
// The STOP/START/HELP families are ports of strategist-usa
// pkg/services/consent_service.go (owner's own repo, no third-party
// license — branch provenance header). Every regex below cites the
// source line of the compiled pattern; the citations match the ones the
// gate uses. ParseCommand ordering is retained: reaction pre-check,
// then the eight STOP-family patterns, then START, then HELP
// (consent_service.go:177-202).
//
// MANDATORY adaptations from the strategist original (each asserted by
// the gate):
//
//	A1. YES splits out of the START family. strategist startRegex
//	    (consent_service.go:143) lists YES inside START; meept's #132
//	    dispatch-ack needs YES as its OWN ReplyYes action (master §7:
//	    "YES = exact yes/confirm/done/ok"), so the ported START set
//	    drops YES and ClassifyReply("yes") returns ReplyYes.
//	A2. "noneed" is a master §7 token with no strategist regex — added
//	    as an exact STOP token.
//	A3. Anchored full-string match posture retained: consent keywords
//	    are whole-message commands, so prose that merely CONTAINS a
//	    keyword ("I want to stop later") stays ReplyOther.
//	A4. The §K2 number compare normalizes through a LOCAL copy of
//	    strategist normalizeE164US (consent_service.go:297-325) rather
//	    than importing internal/voice/telnyx — sms must not import
//	    voice (daemon cycle risk, 01-reply.md contract §7 note).
//
// stdlib only (regexp/strings/sync/time); pure classifier: never
// mutates input, never panics on junk.

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

// ReplyAction is the classified intent of an inbound SMS reply
// (master §7). The string VALUES are deliberately unpinned — daemon
// glue compares against the identifiers, never the literals.
type ReplyAction string

const (
	ReplyStop  ReplyAction = "stop"
	ReplyStart ReplyAction = "start"
	ReplyHelp  ReplyAction = "help"
	ReplyYes   ReplyAction = "yes"
	ReplyOther ReplyAction = "other"
)

// Ported STOP-family patterns, verbatim from strategist consent keyword
// block (consent_service.go var block, lines per the gate citations).
var (
	// stopRegex — strategist consent_service.go:100. The seven
	// 47 CFR 64.1200(a)(10) per-se keywords (stop, quit, end, revoke,
	// opt out, cancel, unsubscribe) plus strategist extensions
	// (STOPALL/STOP ALL/UNSUB/REMOVE/REMOVE ME), with a trailing
	// punctuation/🛑 tail. A2 adds NONEED as an exact STOP token
	// (master §7 token, no strategist regex).
	stopRegex = regexp.MustCompile(`(?i)^\s*(STOP|UNSUBSCRIBE|CANCEL|END|QUIT|STOPALL|STOP\s+ALL|UNSUB|REMOVE|REMOVE\s+ME|REVOKE|OPT\s*OUT|OPTOUT|NONEED)\s*[.!?\x{1F6D1}]*\s*$`)

	// stopMisspellingRegex — strategist consent_service.go:104
	// (common misspellings of STOP).
	stopMisspellingRegex = regexp.MustCompile(`(?i)^\s*(STIP|STOPP|STO|STOP|SOTP|DTOP|ATOP)\s*[.!?]*\s*$`)

	// stopObjectNoun — strategist consent_service.go:112; shared
	// object-noun sub-expression for the sentence-shaped STOP regexes.
	stopObjectNoun = `(?:THESE\s+|THOSE\s+|ALL\s+(?:THE\s+)?|ANY\s+(?:MORE\s+|OF\s+)?|THE\s+)*(?:MESSAGES?|TEXTS?|MSGS?|THEM|ALL|EVERYTHING|YOUR\s+(?:MESSAGES?|TEXTS?))`

	// stopWithContextRegex — strategist consent_service.go:120.
	// Natural-sentence revocations ("please stop sending me messages",
	// wave-T C5).
	stopWithContextRegex = regexp.MustCompile(`(?i)^\s*(?:(?:FUCK(?:ING|IN)?|DAMN|JUST|PLEASE)\s+|(?:HELL|FUCK)\s+NO\s+)*(?:STOP|END|QUIT|UNSUBSCRIBE|CANCEL|REVOKE|OPT\s+OUT\s+OF|OPTOUT\s+OF|OPT\s*OUT|OPTOUT)(?:\s+(?:IT|NOW|THIS|ALREADY|(?:TEXTING|MESSAGING|SENDING|CONTACTING)(?:\s+ME)?|ME|US))?(?:\s+(?:FROM\s+)?` + stopObjectNoun + `)?(?:\s+(?:ASSHOLES?|IDIOTS?|MFS?))?(?:\s+(?:AND\s+)?FUCK\s+(?:YOU|OFF))?(?:\s+(?:PLEASE|THANKS?|NOW|AGAIN|FOREVER))?\s*[.!?]*\s*$`)

	// neverAgainRegex — strategist consent_service.go:125 (NEVER /
	// DO-NOT-contact family with its own trailing adverb + object).
	neverAgainRegex = regexp.MustCompile(`(?i)^\s*(?:NEVER|DO\s+NOT|DON'?T)\s+(?:(?:EVER|AGAIN)\s+)*(?:TEXT|SEND|MESSAGE|CONTACT|CALL|EMAIL)(?:\s+(?:ME|US))?(?:\s+` + stopObjectNoun + `)?(?:\s+(?:AGAIN|EVER|ANYMORE|ANY\s+MORE|PLEASE))*\s*[.!?]*\s*$`)

	// stopEmojiRegex — strategist consent_service.go:128 (🛑 run only;
	// an interior space breaks the run, so "🛑🛑 🛑" is not a command).
	stopEmojiRegex = regexp.MustCompile(`^\s*\x{1F6D1}+\s*$`)

	// stopRepeatedRegex — strategist consent_service.go:132
	// ("STOP STOP", punctuation/symbol separators between repeats).
	stopRepeatedRegex = regexp.MustCompile(`(?i)^\s*STOP[\s,;:!.\-—–_/\\|]+(?:STOP\s*[\s,;:!.\-—–_/\\|]*)+[.!?]*\s*$`)

	// implicitOptOutRegex — strategist consent_service.go:137 (leave
	// me alone / take me off this list / report spam / BLOCKED…).
	implicitOptOutRegex = regexp.MustCompile(`(?i)^\s*(LEAVE\s+ME\s+ALONE|DO\s+NOT\s+(EVER\s+)?CONTACT|DON'?T\s+(EVER\s+)?CONTACT|NEVER\s+(EVER\s+)?(SEND|TEXT|CONTACT)|TAKE\s+ME\s+OFF(?:\s+(?:THIS|THESE|THE|YOUR)\s*(?:\w+\s*)?)?|LOSE\s+MY\s+NUMBER|PUT\s+ME\s+ON\s+(YOUR\s+)?DO\s+NOT|REPORT\s+SPAM|BLOCKED)\s*[.!?]*\s*$`)

	// hostileOptOutRegex — strategist consent_service.go:140 (hostile
	// opt-outs count as revocation).
	hostileOptOutRegex = regexp.MustCompile(`(?i)^\s*(FUCK\s+(OFF|YOU|U|YALL|Y'?ALL)|F\s+OFF|GO\s+(FUCK\s+(YOURSELF|YOUR\s*SELF)|TO\s+HELL)|EAT\s+(SHIT|A\s+DICK)|GET\s+BENT|KISS\s+MY\s+ASS)\s*[.!?]*\s*$`)

	// imessageReactionRegex — strategist consent_service.go:149.
	// iMessage tapbacks are skipped as commands there (ParseCommand
	// "" pre-check :179); meept maps that to ReplyOther.
	imessageReactionRegex = regexp.MustCompile(`(?i)^\s*(LIKED|LOVED|REACTED|LAUGHED\s+AT|EMPHASIZED|QUESTIONED|DISLIKED|REMOVED\s+A\s+\w+\s+FROM|LE\s+DIO\s+RISA)\s+"`)

	// dupSuffixRe — strategist consent_service.go:155; trailing
	// #dup_N dispatch-dedup suffix stripped before digit normalization
	// (faithful port: the suffix digits would otherwise join the run).
	dupSuffixRe = regexp.MustCompile(`#dup_[0-9]+$`)
)

// Exact-token families (anchored whole-message match, A3) on the
// upper-trimmed text.
var (
	// startTokens — strategist startRegex consent_service.go:143 MINUS
	// YES (adaptation A1: YES is its own ReplyYes action).
	startTokens = map[string]bool{"START": true, "SUBSCRIBE": true, "UNSTOP": true, "RESUME": true, "REJOIN": true}

	// yesTokens — master §7 exact list (dispatch ack #132). confirm/
	// done/ok are meept additions; same exact-token posture as YES.
	yesTokens = map[string]bool{"YES": true, "CONFIRM": true, "DONE": true, "OK": true}

	// helpTokens — strategist helpRegex consent_service.go:146.
	helpTokens = map[string]bool{"HELP": true, "INFO": true, "SUPPORT": true, "ASSISTANCE": true}
)

// ClassifyReply maps an inbound message body to a consent reply action
// (master §7). Posture: upper(trim) before matching; whitespace around
// and inside keywords is handled by the ported \s* regex shapes. Pure —
// never mutates input, never panics on junk; anything not an exact
// command shape falls through to ReplyOther.
func ClassifyReply(text string) ReplyAction {
	// iMessage tapback pre-check — strategist ParseCommand :177-181
	// returns "" (skipped as a command); meept has no "" action, so
	// tapbacks are ReplyOther.
	if imessageReactionRegex.MatchString(text) {
		return ReplyOther
	}

	// STOP family — strategist ParseCommand :183-195, same eight
	// patterns, same order.
	if stopRegex.MatchString(text) ||
		stopMisspellingRegex.MatchString(text) ||
		implicitOptOutRegex.MatchString(text) ||
		stopEmojiRegex.MatchString(text) ||
		stopRepeatedRegex.MatchString(text) ||
		hostileOptOutRegex.MatchString(text) ||
		stopWithContextRegex.MatchString(text) ||
		neverAgainRegex.MatchString(text) {
		return ReplyStop
	}

	// Exact-token families on upper(trim) per master §7 "upper(trim)".
	switch upper := strings.ToUpper(strings.TrimSpace(text)); {
	case startTokens[upper]: // consent_service.go:143 minus YES (A1)
		return ReplyStart
	case yesTokens[upper]: // master §7 exact list (dispatch ack #132)
		return ReplyYes
	case helpTokens[upper]: // consent_service.go:146
		return ReplyHelp
	}
	return ReplyOther
}

// Consent reply WORDING defaults (config gate pins stop_reply/help_reply
// zero defaults: "wording defaults live in sms pkg", messaging_texting_
// test.go:42). Ported verbatim from strategist-usa go-shard-runner
// internal/workers/incoming_message.go:386 (opt_out_confirmation) and
// :395 (opt_in_confirmation) — owner's own repo, no third-party license.
// The STOP default references START to resubscribe, so a STOP confirmation
// always names the re-subscribe path even with no operator config.
const (
	// ConsentDefaultStopReply is the STOP/START confirmation text used
	// when cfg [voice.messaging].stop_reply is empty (master §7: STOP and
	// START share the one key; no separate start_reply exists).
	ConsentDefaultStopReply = "You have been opted out. Reply START to resubscribe."
	// ConsentDefaultStartReply is the START confirmation used when
	// stop_reply is empty — the symmetric opt-in wording (:395).
	ConsentDefaultStartReply = "You have been subscribed. Reply STOP to opt out."
)

// ---------------------------------------------------------------------------
// PendingConfirms — in-memory one-shot confirmation registry (v1; the
// dispatch tree persists later). Daemon glue registers a pending when it
// asks a contact to confirm; a ReplyYes resolves (and consumes) it.
// ---------------------------------------------------------------------------

// Pending is one outstanding confirmation request.
type Pending struct {
	Ref          string
	To           string
	Text         string
	RegisteredAt time.Time
}

// PendingConfirms is a TTL'd, one-shot, number-keyed registry of pending
// confirmations. Safe for concurrent use (Resolve consumes under the
// mutex; the daemon ingests webhooks and dispatches from different
// goroutines).
type PendingConfirms struct {
	mu      sync.Mutex
	clock   func() time.Time // nil-safe: normalized to time.Now in New
	ttl     time.Duration
	entries map[string]*Pending // key: normalizeE164US(To)
}

// NewPendingConfirms builds a registry whose entries expire ttl after
// their own RegisteredAt (per-entry, evaluated against clock at
// Resolve). A nil clock means time.Now.
func NewPendingConfirms(clock func() time.Time, ttl time.Duration) *PendingConfirms {
	if clock == nil {
		clock = time.Now
	}
	return &PendingConfirms{
		clock:   clock,
		ttl:     ttl,
		entries: make(map[string]*Pending),
	}
}

// Register stores (or replaces) a pending confirmation for the
// E.164-normalized form of to, stamped with the current clock value.
func (p *PendingConfirms) Register(ref, to, text string) {
	key := normalizeE164US(to)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries[key] = &Pending{
		Ref:          ref,
		To:           to,
		Text:         text,
		RegisteredAt: p.clock(),
	}
}

// Resolve consumes the pending confirmation for the E.164-normalized
// form of to. One-shot: a hit deletes the entry, so the second Resolve
// returns (nil, false). Expired entries (age > ttl at Resolve time) are
// dropped and reported as a miss.
func (p *PendingConfirms) Resolve(to string) (*Pending, bool) {
	key := normalizeE164US(to)
	p.mu.Lock()
	defer p.mu.Unlock()
	pd, ok := p.entries[key]
	if !ok {
		return nil, false
	}
	if p.clock().Sub(pd.RegisteredAt) > p.ttl {
		delete(p.entries, key) // lazy TTL expiry, per-entry
		return nil, false
	}
	delete(p.entries, key) // one-shot consume
	return pd, true
}

// normalizeE164US is an sms-local port of strategist
// normalizeE164US (consent_service.go:297-325), kept stdlib-only and
// LOCAL by mandate: internal/sms must not import internal/voice/telnyx
// (daemon cycle risk — gate §K2 adaptation A4). Semantics: strip a
// trailing #dup_N first (its digits would otherwise join the run), keep
// digits only, 10 bare digits => +1XXXXXXXXXX, 11 digits with leading 1
// => +XXXXXXXXXXX, longer runs => bare + prefix; unrecognized/short
// inputs return the dup-stripped raw unchanged.
func normalizeE164US(raw string) string {
	raw = dupSuffixRe.ReplaceAllString(raw, "")
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, raw)
	switch {
	case len(digits) == 10:
		return "+1" + digits
	case len(digits) == 11 && digits[0] == '1':
		return "+" + digits
	case len(digits) > 10:
		return "+" + digits
	default:
		return raw
	}
}
