package agent

import (
	"regexp"
	"strings"
)

// Named-agent routing (acp-employees follow-up): a chat message that
// addresses a roster agent BY NAME ("have bob introduce himself",
// "ask bob to ...", "@bob ...", "let bob ...", "bob should ...") must route
// the FULL turn to that agent's loop directly — no planner decomposition,
// no capability/keyword re-assignment. The live defect (2026-10-23 smoke):
// "have bob introduce himself and list two files in this repo" was
// classified and planned by the orchestrator; the planner prose said "hand
// the task to bob via request_handoff" but the capability table assigned
// committer, the handoff never fired, and committer's canned chat line
// answered the turn.
//
// The matcher is bounded and deterministic: it resolves a candidate token
// against the AgentRegistry's known spec ids/names ONLY (a registry lookup
// miss falls through to the normal classification chain), so a false
// positive requires the word to be a real roster agent name. executor: acp
// agents are first-class here — the ACP executor is transparent to routing;
// it binds at the loop's runTurnProducer seam, not here.

// namedAgentVerbPatterns are the imperative "address an agent" frames. The
// agent name must sit in the verb's object slot ("have bob do X", never
// "have the bob in the room ..."): the slot is bounded to at most three
// short words. The subject frames ("bob should ...", "bob: ...") are
// ANCHORED to the message start — mid-sentence subjects ("the planner
// should be faster") are observations, not assignments, and must not
// hijack the turn.
//
// The have/get frames additionally require a SENTENCE BOUNDARY before the
// verb ("do you have coder available?" is a status question, not an
// assignment; "have bob introduce himself" and "...and have bob list two
// files" are). The ask/tell/send/assign/let/make frames need no boundary
// gate: their interrogative forms ("could you ask bob to ...") ARE
// assignments.
// namedAgentFrame is one imperative-address frame: a regex whose group 1
// captures the candidate agent token, plus an optional boundary gate
// applied to the text preceding the match (used by have/get, see above).
type namedAgentFrame struct {
	re       *regexp.Regexp
	boundary bool
}

var namedAgentVerbPatterns = []namedAgentFrame{
	// have bob <do X> / get bob <do X> (boundary-gated, see above)
	{re: regexp.MustCompile(`(?i)\b(?:have|get)\s+([a-z][a-z0-9_-]{1,31})\s+(?:to\s+)?(?:[a-z']{1,20}\s){0,3}`), boundary: true},
	// let bob <do X> / make bob <do X> / ask bob <to do X>
	{re: regexp.MustCompile(`(?i)\b(?:let|make|ask)\s+([a-z][a-z0-9_-]{1,31})\s+(?:to\s+)?(?:[a-z']{1,20}\s){0,3}`)},
	// send bob <to do X> / tell bob <to do X> / assign bob <the task>
	{re: regexp.MustCompile(`(?i)\b(?:send|tell|assign)\s+([a-z][a-z0-9_-]{1,31})\s+(?:to\s+)?(?:[a-z']{1,20}\s){0,2}`)},
	// anchored subject: "bob, run Y" / "bob: do Z" / "bob should X"
	{re: regexp.MustCompile(`(?i)^\s*([a-z][a-z0-9_-]{1,31})\s*(?:,|:)\s+\S`)},
	{re: regexp.MustCompile(`(?i)^\s*([a-z][a-z0-9_-]{1,31})\s+(?:should|please)\s+`)},
}

// haveGetBoundaryRe is tested against everything BEFORE the have/get verb:
// the frame only counts as an assignment when the verb follows a sentence
// boundary / connector (an empty prefix = the verb opens the message and
// always qualifies). The match starts at the verb (\b before the verb
// alternative), so the gate slices [0, matchStart).
var haveGetBoundaryRe = regexp.MustCompile(`(?i)(?:[,.;:!?]\s+|\b(?:and|then|so|but|also|please|first|next|now)\s+)$`)

func haveGetBoundary(prefix string) bool {
	return prefix == "" || haveGetBoundaryRe.MatchString(prefix)
}

// namedAgentMentionRe matches an @name mention anywhere in the text
// (strongest signal — the user typed the handle).
var namedAgentMentionRe = regexp.MustCompile(`(?i)(^|\s)@([a-z][a-z0-9_-]{1,31})\b`)

// namedAgentStopWords are high-frequency function words that can sit in a
// verb frame's name slot ("make sure ...", "ask me ..."). They never reach
// the registry resolver — the registry gate is the correctness guarantee
// regardless; this list only keeps common-English false candidates cheap.
var namedAgentStopWords = map[string]bool{
	"the": true, "a": true, "an": true, "this": true, "that": true,
	"these": true, "those": true, "it": true, "its": true, "him": true,
	"her": true, "them": true, "us": true, "you": true, "me": true,
	"my": true, "our": true, "your": true, "someone": true, "somebody": true,
	"anyone": true, "anybody": true, "everyone": true, "everybody": true,
	"please": true, "just": true, "also": true, "then": true, "now": true,
	"again": true, "still": true, "always": true, "never": true,
	"here": true, "there": true, "where": true, "what": true, "who": true,
	"how": true, "why": true, "when": true, "which": true,
	"himself": true, "herself": true, "themselves": true, "myself": true,
	"sure": true, "itself": true, "everything": true, "nothing": true,
}

// namedAgentCandidate extracts the roster-agent name a user message
// addresses, if any. Returns (agentID, ok): agentID is the canonical
// registry spec id (not the surface token — a spec named "Bob" addressed as
// "bob" resolves to its id). ok=false means no addressed roster agent was
// detected and the caller must fall through to the normal classification
// chain untouched.
func namedAgentCandidate(input string, resolve func(token string) (string, bool)) (string, bool) {
	if input == "" || resolve == nil {
		return "", false
	}
	// Never hijack slash commands — they have their own dispatch chain.
	trimmed := strings.TrimSpace(input)
	if strings.HasPrefix(trimmed, "/") {
		return "", false
	}

	// 1. @mention anywhere.
	if m := namedAgentMentionRe.FindStringSubmatch(trimmed); m != nil {
		if id, ok := resolve(strings.ToLower(m[2])); ok {
			return id, true
		}
	}

	// 2. Imperative / subject frames, in pattern order. A candidate token
	//    only wins when it resolves to a real roster agent. The have/get
	//    frame additionally needs a sentence boundary before its verb so
	//    "do you have coder available?" reads as a question, not an
	//    assignment.
	for _, frame := range namedAgentVerbPatterns {
		for _, m := range frame.re.FindAllStringSubmatchIndex(trimmed, 8) {
			if frame.boundary && !haveGetBoundary(trimmed[:m[0]]) {
				continue
			}
			tok := strings.ToLower(trimmed[m[2]:m[3]])
			if namedAgentStopWords[tok] {
				continue
			}
			if id, ok := resolve(tok); ok {
				return id, true
			}
		}
	}
	return "", false
}

// namedAgentStepActorRe matches the actor slot in plan-step prose:
// "hand/route/pass/delegate ... to bob", "ask bob to ...", or an @bob
// mention. The registry gate behind the capture is what keeps "commit the
// change to main" harmless — "main" only wins if a roster agent actually
// carries that id.
var namedAgentStepActorRe = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:hand|pass|route|delegate|assign|give|send|escalate)(?:\s+[a-z'0-9._-]{1,30}){0,6}\s+to\s+(?:the\s+)?([a-z][a-z0-9_-]{1,31})\b`),
	regexp.MustCompile(`(?i)\b(?:ask|have|let|tell|get|make)\s+([a-z][a-z0-9_-]{1,31})\s+(?:to\s+)?(?:[a-z']{1,20}\s){0,3}`),
}

// namedAgentInStepDescription resolves the actor a plan step names, using
// the same registry-gated determinism as namedAgentCandidate. Used to stamp
// step.AgentID when the strategic planner decomposes a turn whose steps
// name a roster agent ("hand the task to bob via request_handoff").
func namedAgentInStepDescription(description string, resolve func(token string) (string, bool)) (string, bool) {
	if description == "" || resolve == nil {
		return "", false
	}
	if m := namedAgentMentionRe.FindStringSubmatch(description); m != nil {
		if id, ok := resolve(strings.ToLower(m[2])); ok {
			return id, true
		}
	}
	for _, re := range namedAgentStepActorRe {
		for _, m := range re.FindAllStringSubmatch(description, 4) {
			tok := strings.ToLower(m[1])
			if namedAgentStopWords[tok] {
				continue
			}
			if id, ok := resolve(tok); ok {
				return id, true
			}
		}
	}
	return "", false
}

// resolveNamedRosterAgent builds the token→agent-id resolver over a
// registry snapshot. Matches on spec ID and spec Name (both
// case-insensitive, whitespace-trimmed). Disabled specs never match. ID
// wins a collision with another spec's Name: dispatch keys are spec ids.
func resolveNamedRosterAgent(specs []*AgentSpec) func(string) (string, bool) {
	byToken := make(map[string]string, len(specs)*2)
	for _, spec := range specs {
		if spec == nil || !spec.Enabled || spec.ID == "" {
			continue
		}
		byToken[strings.ToLower(strings.TrimSpace(spec.ID))] = spec.ID
		if n := strings.ToLower(strings.TrimSpace(spec.Name)); n != "" {
			if _, exists := byToken[n]; !exists {
				byToken[n] = spec.ID
			}
		}
	}
	return func(token string) (string, bool) {
		token = strings.ToLower(strings.TrimSpace(token))
		if token == "" {
			return "", false
		}
		id, ok := byToken[token]
		return id, ok
	}
}
