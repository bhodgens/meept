package pii

import (
	"regexp"
	"strings"
	"testing"
)

var tokenShape = regexp.MustCompile(`^\[PII:[0-9a-f-]{36}\]$`)

// TestSwapper_TokenFormat covers leaf T3: tokens match
// ^\[PII:[0-9a-f-]{36}\]$ and carry UUIDv4 version/variant bits.
func TestSwapper_TokenFormat(t *testing.T) {
	tok := NewToken()
	if !tokenShape.MatchString(tok) {
		t.Fatalf("token %q does not match the leaf token shape", tok)
	}
	uuid := strings.TrimSuffix(strings.TrimPrefix(tok, "[PII:"), "]")
	if uuid[14] != '4' {
		t.Fatalf("token %q is not UUIDv4 (version nibble %q)", tok, string(uuid[14]))
	}
	if seen := map[string]bool{}; true {
		for i := 0; i < 1000; i++ {
			u := NewToken()
			if seen[u] {
				t.Fatalf("duplicate token minted: %s", u)
			}
			seen[u] = true
		}
	}
}

// TestSwapper_MaskRoundTrip covers leaf T3: Mask then Restore returns the
// original text for the same session, and Mask removes all PII.
func TestSwapper_MaskRoundTrip(t *testing.T) {
	st, err := NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	defer st.Close()

	sw := NewSwapper(st)
	orig := "mail alice@example.com or 555-0100, card 4111111111111111, ip 10.0.0.1"

	masked, count := sw.Mask(orig, "sess-1")
	if count == 0 {
		t.Fatal("Mask replaced nothing")
	}
	for _, leak := range []string{"alice@example.com", "555-0100", "4111111111111111", "10.0.0.1", "@"} {
		if strings.Contains(masked, leak) {
			t.Fatalf("masked text still contains %q: %q", leak, masked)
		}
	}
	for _, tok := range tokenRe.FindAllString(masked, -1) {
		if !tokenShape.MatchString(tok) {
			t.Fatalf("malformed token in masked text: %q", tok)
		}
	}

	restored, rcount := sw.Restore(masked, "sess-1")
	if rcount != count {
		t.Fatalf("restore count = %d; mask count = %d", rcount, count)
	}
	if restored != orig {
		t.Fatalf("round trip mismatch:\n got  %q\n want %q", restored, orig)
	}
}

// TestSwapper_RestoreOtherSessionUnchanged covers leaf T3: restoring with
// a different session returns the masked text unchanged.
func TestSwapper_RestoreOtherSessionUnchanged(t *testing.T) {
	st, err := NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	defer st.Close()

	sw := NewSwapper(st)
	masked, _ := sw.Mask("mail alice@example.com now", "sess-1")

	restored, count := sw.Restore(masked, "sess-2")
	if count != 0 || restored != masked {
		t.Fatalf("cross-session restore: count=%d restored=%q", count, restored)
	}
}

// TestSwapper_DeterministicWithinSession pins D3: the same value maps to
// the same token within a session (stable entity across turns), while a
// different session mints an independent token.
func TestSwapper_DeterministicWithinSession(t *testing.T) {
	st, err := NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	defer st.Close()

	sw := NewSwapper(st)
	text := "mail alice@example.com then alice@example.com again"

	masked, count := sw.Mask(text, "sess-1")
	toks := tokenRe.FindAllString(masked, -1)
	if len(toks) != 2 || toks[0] != toks[1] {
		t.Fatalf("same value produced different tokens: %q (%v)", masked, toks)
	}
	if count != 2 {
		t.Fatalf("count = %d; want 2", count)
	}

	maskedOther, _ := sw.Mask(text, "sess-2")
	if maskedOther == masked {
		t.Fatal("tokens must not be shared across sessions")
	}
}

// TestSwapper_MaskNoPII is a no-op guard: PII-free text passes through
// with count 0.
func TestSwapper_MaskNoPII(t *testing.T) {
	st, err := NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	defer st.Close()

	sw := NewSwapper(st)
	const text = "plain text with no sensitive spans at all"
	masked, count := sw.Mask(text, "sess-1")
	if count != 0 || masked != text {
		t.Fatalf("Mask changed PII-free text: count=%d masked=%q", count, masked)
	}
}

// tokenRe is the unanchored token finder used by the tests above.
var tokenRe = regexp.MustCompile(`\[PII:[0-9a-f-]{36}\]`)
