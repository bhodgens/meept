package sessionref

import (
	"errors"
	"strings"
	"testing"
)

// G4 gate (03-session-storage/00T-tests.md row G4): mention grammar +
// reference budget math + truncate. Activated with leaf 03/03.
//
// Note: the originally parked stub asserted `clean == text` (mentions left
// intact), which contradicts the G6 e2e pin (`clean` must NOT carry the
// `meept-session:` markup) and the dsh parseSessionReferenceText semantics
// (each match is replaced by a readable `@label` span). This gate follows
// G6 + dsh; the stub assertion was wrong.

func TestParseMentionText(t *testing.T) {
	// Explicit Markdown mention.
	uri := EncodeSessionReferenceURI("sess-b-1")
	text := "see @[Session B](" + uri + ") for details"
	clean, refs, err := ParseMentionText(text)
	if err != nil {
		t.Fatalf("ParseMentionText: %v", err)
	}
	if len(refs) != 1 || refs[0].SessionID != "sess-b-1" || refs[0].Label != "Session B" {
		t.Fatalf("refs = %+v, want [{sess-b-1 Session B}]", refs)
	}
	if strings.Contains(clean, "meept-session:") {
		t.Errorf("clean text still carries mention markup: %q", clean)
	}
	if !strings.Contains(clean, "@Session B") {
		t.Errorf("clean text lost the readable label span: %q", clean)
	}

	// Bare canonical URI is also a reference.
	_, bareRefs, err := ParseMentionText("context: " + uri)
	if err != nil {
		t.Fatalf("bare mention: %v", err)
	}
	if len(bareRefs) != 1 || bareRefs[0].SessionID != "sess-b-1" {
		t.Errorf("bare refs = %+v, want one ref to sess-b-1", bareRefs)
	}

	// Malformed explicit mention fails with ErrSessionRefInvalid.
	_, _, err = ParseMentionText("@[broken](meept-session:!!!not-base64!!!)")
	if !errors.Is(err, ErrSessionRefInvalid) {
		t.Errorf("malformed mention err = %v, want ErrSessionRefInvalid", err)
	}

	// Text without mentions parses unchanged.
	plain := "no mentions here"
	if clean, refs, err := ParseMentionText(plain); err != nil || clean != plain || len(refs) != 0 {
		t.Errorf("plain parse = (%q, %+v, %v), want unchanged", clean, refs, err)
	}

	// Escaped labels round-trip.
	clean, refs, err = ParseMentionText(`@[a\]b](meept-session:` + uri[len("meept-session:"):] + `)`)
	if err != nil {
		t.Fatalf("escaped label: %v", err)
	}
	if len(refs) != 1 || refs[0].Label != "a]b" {
		t.Errorf("escaped label refs = %+v, want label a]b", refs)
	}
	if strings.Contains(clean, `\]`) {
		t.Errorf("escaped label not unescaped in clean text: %q", clean)
	}
}

func TestReferenceBudget(t *testing.T) {
	// G4: budget = max(65536, floor(window*4*0.2)).
	if got := ReferenceBudget(16000); got != DefaultMaxReferenceBytes {
		t.Errorf("budget(16000) = %d, want the %d floor", got, DefaultMaxReferenceBytes)
	}
	// window 1_000_000 -> floor(1e6*4*0.2) = 800_000 > floor.
	if got := ReferenceBudget(1_000_000); got != 800_000 {
		t.Errorf("budget(1000000) = %d, want 800000", got)
	}
	// Exactly at the floor boundary: window*4*0.2 == 65536 -> window 81920.
	if got := ReferenceBudget(81920); got != DefaultMaxReferenceBytes {
		t.Errorf("budget(81920) = %d, want %d", got, DefaultMaxReferenceBytes)
	}
}

func TestTruncateWithNotice(t *testing.T) {
	// G4: head-tail truncate reports exact omitted bytes and the notice fits.
	text := strings.Repeat("abcdefghij", 100) // 1000 bytes
	got, omitted := truncateWithNotice(text, 200)
	if got == text {
		t.Fatal("truncateWithNotice did not truncate an over-budget text")
	}
	if len(got) > 200 {
		t.Errorf("truncated text = %d bytes, over the 200 budget", len(got))
	}
	if omitted <= 0 {
		t.Errorf("omitted = %d, want a positive exact count", omitted)
	}
	if !strings.Contains(got, "omitted") || !strings.Contains(got, "UTF-8 bytes") {
		t.Errorf("truncated text lacks the omission notice: %q", got[:min(120, len(got))])
	}
	// Exactness: omitted + retained-text bytes == original bytes.
	noticeStart := strings.Index(got, "\n[… omitted")
	if noticeStart < 0 {
		t.Fatalf("notice delimiter not found in %q", got)
	}
	if kept := noticeStart; len(text)-kept != omitted {
		t.Errorf("omitted %d != exact %d (original 1000, head+tail kept %d)", omitted, len(text)-kept, kept)
	}
	// Under-budget text passes through untouched.
	short := "fits"
	if got, omitted := truncateWithNotice(short, 100); got != short || omitted != 0 {
		t.Errorf("truncateWithNotice(%q, 100) = (%q, %d), want passthrough", short, got, omitted)
	}
	// UTF-8 safety: multi-byte runes are never split.
	uni := strings.Repeat("日本語", 200) // 1800 bytes
	gotU, _ := truncateWithNotice(uni, 300)
	if len(gotU) > 300 {
		t.Errorf("utf8 truncated text = %d bytes, over budget", len(gotU))
	}
	for _, r := range gotU {
		if r == 0xFFFD {
			t.Error("truncation produced a replacement char (split UTF-8 sequence)")
			break
		}
	}
}

func TestNormalizeReferences(t *testing.T) {
	// Self-reference rejected.
	if _, err := normalizeReferences("s1", []SessionReferenceInput{{SessionID: "s1"}}); !errors.Is(err, ErrSessionRefSelf) {
		t.Errorf("self-ref err = %v, want ErrSessionRefSelf", err)
	}
	// Duplicates dedupe; >3 distinct fails.
	dup, err := normalizeReferences("s1", []SessionReferenceInput{
		{SessionID: "a", Label: "A"}, {SessionID: "a", Label: "A2"},
	})
	if err != nil || len(dup) != 1 || dup[0].Label != "A" {
		t.Errorf("dedupe = (%+v, %v), want one ref with the first label", dup, err)
	}
	if _, err := normalizeReferences("s1", []SessionReferenceInput{
		{SessionID: "a"}, {SessionID: "b"}, {SessionID: "c"}, {SessionID: "d"},
	}); !errors.Is(err, ErrSessionRefTooMany) {
		t.Errorf("4-ref err = %v, want ErrSessionRefTooMany", err)
	}
	// Empty label defaults to the session id.
	out, err := normalizeReferences("s1", []SessionReferenceInput{{SessionID: "a"}})
	if err != nil || len(out) != 1 || out[0].Label != "a" {
		t.Errorf("label default = (%+v, %v), want label 'a'", out, err)
	}
}

func TestURICanonicality(t *testing.T) {
	uri := EncodeSessionReferenceURI("sess-1")
	id, err := DecodeSessionReferenceURI(uri)
	if err != nil || id != "sess-1" {
		t.Fatalf("round-trip = (%q, %v)", id, err)
	}
	// Non-canonical padding fails.
	if _, err := DecodeSessionReferenceURI("meept-session:c2Vzcy0x="); !errors.Is(err, ErrSessionRefInvalid) {
		t.Errorf("padded uri err = %v, want ErrSessionRefInvalid", err)
	}
	// Wrong scheme fails.
	if _, err := DecodeSessionReferenceURI("dsh-session:c2Vzcy0x"); !errors.Is(err, ErrSessionRefInvalid) {
		t.Errorf("wrong scheme err = %v, want ErrSessionRefInvalid", err)
	}
	// Mention formatting escapes label specials.
	got := FormatSessionReferenceMention(SessionReferenceInput{SessionID: "s", Label: "a]b\\c"})
	if !strings.Contains(got, `a\]b\\c`) {
		t.Errorf("FormatSessionReferenceMention = %q, want escaped label", got)
	}
}
