package voice

import (
	"strings"
	"testing"
	"time"
)

// echoRoster unit pins. These run against a LOCAL roster instance so the
// process-wide crossCallEcho stays clean for the rest of the package
// suite (the talk-loop pins below own their own global-roster hygiene).

func newTestRoster() *echoRoster { return &echoRoster{} }

// TestEchoRosterRecordGate pins the basic record -> gate flow: after call
// A speaks a sentence, a prompt from call B that echoes it is flagged;
// unrelated text from B is not.
func TestEchoRosterRecordGate(t *testing.T) {
	r := newTestRoster()
	r.record("cc-a", "Flurb quarx my zibble plow invoice, stap the vinyl manifest before nun.")

	if !r.recentOther("cc-b", "flurb quarx my zibble plow invoice stap the vinyl manifest before nun") {
		t.Fatal("verbatim echo of another call's speech must gate")
	}
	// Whisper garble a few letters deep still gates (exact token match is
	// not required — single-token similarity >= the floor suffices).
	if !r.recentOther("cc-b", "flurb quarx my zibble plow invoice stap the vinyl manifest befor nun") {
		t.Fatal("garbled echo (one-letter corruption) must gate")
	}
	if r.recentOther("cc-b", "reorder the plumbing fixtures schedule tuesday morning") {
		t.Fatal("unrelated prompt must NOT gate")
	}
}

// TestEchoRosterOwnCallExcluded pins contract rule 2: A's own speech NEVER
// gates A's prompts — self-echo belongs to the child-side window. This is
// the critical same-ccid exclusion.
func TestEchoRosterOwnCallExcluded(t *testing.T) {
	r := newTestRoster()
	r.record("cc-a", "The quarterly ledger shows two open siding orders downtown.")
	if r.recentOther("cc-a", "the quarterly ledger shows two open siding orders downtown") {
		t.Fatal("own-call speech must never gate own prompts (child window owns self-echo)")
	}
	// The same text from a DIFFERENT call does gate (control).
	if !r.recentOther("cc-b", "the quarterly ledger shows two open siding orders downtown") {
		t.Fatal("other-call echo must gate")
	}
}

// TestEchoRosterExpire pins TTL aging: an entry older than the live TTL is
// pruned and stops gating. Timestamps are rewound directly (same-package
// test seam) instead of sleeping — deterministic under -race.
func TestEchoRosterExpire(t *testing.T) {
	r := newTestRoster()
	r.record("cc-a", "Vinyl manifest for the zibble plow invoice before nun stap quarx flurb.")
	if !r.recentOther("cc-b", "vinyl manifest for the zibble plow invoice before nun") {
		t.Fatal("fresh entry must gate")
	}
	r.mu.Lock()
	for i := range r.entries {
		r.entries[i].at = time.Now().Add(-2 * echoRosterTTL)
	}
	r.mu.Unlock()
	if r.recentOther("cc-b", "vinyl manifest for the zibble plow invoice before nun") {
		t.Fatal("expired entry must stop gating (and be pruned)")
	}
	if got := r.len(); got != 0 {
		t.Fatalf("expired entries must be pruned on sweep: len = %d, want 0", got)
	}
}

// TestEchoRosterClear pins the teardown hook: clear(ccid) drops exactly
// that call's entries.
func TestEchoRosterClear(t *testing.T) {
	r := newTestRoster()
	r.record("cc-a", "Flurb quarx my zibble plow invoice, stap the vinyl manifest before nun.")
	r.record("cc-b", "Drex nim the cobalt spanner, weld gasket six before dawn tonight.")
	r.clear("cc-a")
	if r.len() != 1 {
		t.Fatalf("clear must drop only cc-a's entry: len = %d, want 1", r.len())
	}
	if r.recentOther("cc-x", "flurb quarx my zibble plow invoice stap the vinyl manifest") {
		t.Fatal("cleared entry must not gate")
	}
	// cc-b's entry survives.
	if !r.recentOther("cc-x", "drex nim the cobalt spanner weld gasket six before dawn") {
		t.Fatal("other call's entry must survive clear")
	}
}

// TestEchoRosterBounded pins the FIFO entry cap via the seam (production
// cap 128; tests shrink it like silenceWindowUnit posture — sequential,
// restore in cleanup).
func TestEchoRosterBounded(t *testing.T) {
	old := echoRosterMax
	echoRosterMax = 4
	t.Cleanup(func() { echoRosterMax = old })

	r := newTestRoster()
	// Pairwise-distinct closed vocabularies: each sentence must not
	// coverage-match any other (evicted entry = no gate; newest = gates).
	sentences := []string{
		"Zibble one flurbs quarx my plow invoice stap vinyl manifest nun",
		"Drex two nimmed cobalt spanners weld gasket six dawn tonight",
		"Flim three socked puppet dances tuesday morning bakery queue",
		"Glorp four nubbin wrenches calibrate sprocket axis relay grid",
		"Blorp five pickled walnuts juggle trombone whisker plasma six",
	}
	for _, s := range sentences {
		r.record("cc-a", s)
	}
	if r.len() != 4 {
		t.Fatalf("roster must cap at the bound: len = %d, want 4", r.len())
	}
	// Oldest (index 0 sentence) evicted: it no longer gates...
	if r.recentOther("cc-b", strings.ToLower(sentences[0])) {
		t.Fatal("evicted oldest entry must not gate")
	}
	// ...while the newest still does.
	if !r.recentOther("cc-b", strings.ToLower(sentences[4])) {
		t.Fatal("newest entry must gate")
	}
}

// TestEchoRosterShortPromptFloor pins the min-words guard: prompts under
// echoMinPromptWords NEVER consult the scorer — even a verbatim echo of
// another call's short line is not gated (short finals are ambient; the
// sibling min_turn_words gate owns them once landed, see the constant's
// comment in echo_roster.go).
func TestEchoRosterShortPromptFloor(t *testing.T) {
	r := newTestRoster()
	r.record("cc-a", "Hello, how can I help you today?")
	// At/above the floor (echoMinPromptWords = 4): a 5-word echo gates.
	if !r.recentOther("cc-b", "hello how can I help") {
		t.Fatal("5-word verbatim echo must gate (floor is 4 words)")
	}
	// Below the floor: 3-word prompts skip the scorer entirely, even a
	// verbatim prefix of recorded speech.
	if r.recentOther("cc-b", "hello there friend") {
		t.Fatal("3-word prompt must skip the scorer entirely")
	}
	if r.recentOther("cc-b", "how can I") {
		t.Fatal("3-word prompt must skip even when every word is recorded")
	}
	// The prompt itself (not just the entry) drives the floor: a 4-word
	// query of unrelated words does not gate an entry it does not match.
	if r.recentOther("cc-b", "book me a table") {
		t.Fatal("unrelated 4-word prompt must not gate")
	}
}

// TestEchoRosterEmptyRecordIsNoop: the turn-final flush marker is
// say(ccid, "", true); an empty text must never become a matchable entry.
func TestEchoRosterEmptyRecordIsNoop(t *testing.T) {
	r := newTestRoster()
	r.record("cc-a", "")
	r.record("cc-a", "   ")
	r.record("", "some words here that should not land")
	if r.len() != 0 {
		t.Fatalf("empty/ccid-less records must be no-ops: len = %d, want 0", r.len())
	}
}
