package voice

// Punctuation free-match pins (echo dead-token defect 2026-11-14): a
// punctuation-only token normalizes to EMPTY under fuzzy's rule and
// Similarity then scores 1.0 against any other empty token — a whisper
// "..." in a caller final free-matched agent say-text punctuation and
// deflated real coverage, dropping genuine caller turns as echo. The fix
// drops dead tokens at STORE time and QUERY time (echoLiveTokens), so
// they never count toward echoCoverageNeed or entry size.

import (
	"testing"
)

// TestEchoRosterPunctuationDoesNotFreeMatch: pre-fix, punctuation-only
// tokens on BOTH sides normalized to empty and scored 1.0 against each
// other — pure punctuation noise ("...", "--") on a caller final
// free-matched punctuation inside agent say text and crossed the coverage
// floor without a single real word matching. Post-fix the dead tokens
// never exist on either side.
func TestEchoRosterPunctuationDoesNotFreeMatch(t *testing.T) {
	r := newTestRoster()
	// The agent says a filler line that is mostly pauses.
	r.record("cc-a", "ok ... ... ...")
	// Pre-fix this entry carried 4 raw tokens; post-fix only the live
	// word survives.
	r.mu.Lock()
	if n := len(r.entries); n != 1 {
		t.Fatalf("expected exactly one stored entry: %d", n)
	}
	if toks := r.entries[0].tokens; len(toks) != 1 || toks[0] != "ok" {
		t.Fatalf("dead tokens must be dropped at store time: %q", toks)
	}
	r.mu.Unlock()
	// A whisper final of pure punctuation noise (4 raw fields) must NOT
	// gate. Pre-fix: 4 raw query tokens, need 3, the three "..." tokens
	// matched the entry's dead "..." tokens -> false echo.
	if r.recentOther("cc-b", "... ... ... hm") {
		t.Fatal("punctuation-noise final must not free-match punctuation in recorded speech")
	}
	// A say entry of pure punctuation never stores at all ("...(pauses)"
	// keeps the live word "pauses"; a true dead-only line stores none).
	r.record("cc-a", "-- -- --")
	r.mu.Lock()
	if n := len(r.entries); n != 1 {
		t.Fatalf("dead-only say must not store a second entry: %d", n)
	}
	r.mu.Unlock()
}

// TestEchoRosterPunctuationTokensIgnoredOnBothSides: dead tokens on
// EITHER side are dropped before the scorer — a mixed entry keeps only
// its live tokens, and a query whose punctuation pad inflates its
// apparent word count is scored on its live words only.
func TestEchoRosterPunctuationTokensIgnoredOnBothSides(t *testing.T) {
	r := newTestRoster()
	// The agent says a real sentence wrapped in pauses/punctuation.
	r.record("cc-a", "... um ... invoice stap vinyl manifest nun quarx flurb ...")
	// Caller final: the real words echo, padded with whisper ellipses.
	if !r.recentOther("cc-b", "invoice stap vinyl manifest nun quarx flurb") {
		t.Fatal("genuine echo with punctuation padding must still match")
	}
	// Query-side dead tokens must not satisfy the short-prompt floor:
	// only 3 live words — below echoMinPromptWords — so the scorer
	// never runs despite 6 raw fields.
	if r.recentOther("cc-b", "... ... ... invoice stap vinyl") {
		t.Fatal("dead query tokens must not count toward the min-words floor")
	}
	// And an entry reduced to nothing by normalization stores nothing.
	r.record("cc-a", "-- -- --")
	if got := r.len(); got != 1 {
		t.Fatalf("dash-only say must not store a NEW entry: len = %d, want 1 (the live entry above)", got)
	}
}

// TestEchoLiveTokensDropsDeadTokens pins the normalization rule directly:
// tokens that collapse to empty under fuzzy.NormalizeToken never survive.
func TestEchoLiveTokensDropsDeadTokens(t *testing.T) {
	got := echoLiveTokens("... hello -- world ... !!!")
	want := []string{"hello", "world"}
	if len(got) != len(want) {
		t.Fatalf("echoLiveTokens = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("echoLiveTokens = %q, want %q", got, want)
		}
	}
	// The stored TEXT is otherwise intact: live tokens keep their raw
	// case/punctuation inside the token.
	if got := echoLiveTokens("Invoice, stap!"); len(got) != 2 || got[0] != "Invoice," || got[1] != "stap!" {
		t.Fatalf("live tokens must keep their raw text: %q", got)
	}
}
