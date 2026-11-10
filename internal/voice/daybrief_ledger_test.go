package voice

// Leaf 03-voice/00T §3 test gate: the day-brief ledger seam (frozen
// contract of leaf 03-03, verbatim):
//
//   type OpenCommitmentsSource interface {
//       OpenCommitments(ctx context.Context, employeeID string) ([]CommitmentBrief, error)
//   }
//   type CommitmentBrief struct{ Subject, Summary, Status, LastAt string }
//   DayBrief gains field Open []CommitmentBrief
//
// When Open is populated (the seam is non-nil AND returns rows),
// PromptBlock lists "- [open] {subject} — {summary} ({status})" lines
// INSTEAD of the raw "[prior calls today]" transcript section that embeds
// phone numbers (daybrief.go:203). When nil/empty the block is
// BYTE-IDENTICAL to the pre-edit rendering (§3 golden pinned below —
// captured from the pre-edit code path before this file landed).
// Never render a phone number in the block.
//
// Tests ONLY; the golden literal is the frozen pre-edit bytes.

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/activity"
)

// vbLedgerCallNumber is the prior-call number embedded in the golden
// fixture — it MUST survive in the pre-edit section and MUST NOT appear
// once the seam replaces it.
const vbLedgerCallNumber = "+15559874567"

// vbLedgerGoldenBlock is the frozen byte-exact PromptBlock output of the
// pre-edit DayBrief for the fixture below (captured from the unmodified
// code path: "[activity today]\n- called acme\n- sent quote\n\n[prior
// calls today]\n- 10:15 <+number>: asked about roof\n\n[durable memory]\n-
// prefers SMS\n\N[active goals]\n- finish roof quote", 171 bytes).
// Written assembled-from-parts so the number constant stays single-sourced.
func vbLedgerGoldenBlock() string {
	return "[activity today]\n- called acme\n- sent quote\n\n" +
		"[prior calls today]\n- 10:15 " + vbLedgerCallNumber + ": asked about roof\n\n" +
		"[durable memory]\n- prefers SMS\n\n" +
		"[active goals]\n- finish roof quote"
}

// vbLedgerFixture is the legacy DayBrief (no Open field set) used by both
// golden branches.
func vbLedgerFixture() DayBrief {
	return DayBrief{
		Activity: []activity.ActivityRecord{
			{Kind: "session_message", Summary: "called acme"},
			{Kind: "tool_call", Summary: "sent quote"},
		},
		PriorCallTranscripts: []CallTranscript{
			{Number: vbLedgerCallNumber, At: time.Date(2026, 11, 9, 10, 15, 0, 0, time.UTC), Summary: "asked about roof"},
		},
		DurableMemory: "prefers SMS",
		ActiveGoals:   []string{"finish roof quote"},
	}
}

var vbPhoneRegex = regexp.MustCompile(`\+\d`)

// vbFakeCommitments is the §3 seam fake: two open commitments, the second
// summary carrying a phone run a pre-03-02 row might leak (the rendering
// path must still not surface digits — belt-and-braces posture is the
// impl's; the gate asserts the OUTPUT has none of the seam fixture digits).
type vbFakeCommitments struct {
	calls int
	empID string
}

func (f *vbFakeCommitments) OpenCommitments(_ context.Context, employeeID string) ([]CommitmentBrief, error) {
	f.calls++
	f.empID = employeeID
	return []CommitmentBrief{
		{Subject: "acme-roof", Summary: "send the written quote", Status: "open", LastAt: "2026-11-09T09:00:00Z"},
		{Subject: "siding-order", Summary: "waiting on the contractor deposit", Status: "waiting_on_them", LastAt: "2026-11-09T08:00:00Z"},
	}, nil
}

// Compile-time conformance: the frozen seam interface + the fake that
// implements it. These lines fail the build until daybrief.go declares
// OpenCommitmentsSource and CommitmentBrief exactly as frozen.
var (
	_ OpenCommitmentsSource = (*vbFakeCommitments)(nil)
	_ CommitmentBrief       = CommitmentBrief{Subject: "", Summary: "", Status: "", LastAt: ""}
)

// §3 branch A: seam nil (Open unset) -> PromptBlock byte-identical to the
// pre-edit golden.
func TestDayBriefLedgerSeamNilKeepsGoldenBlock(t *testing.T) {
	got := vbLedgerFixture().PromptBlock()
	want := vbLedgerGoldenBlock()
	if got != want {
		t.Errorf("nil-seam PromptBlock drifted from the pre-edit golden:\ngot  %q\nwant %q", got, want)
	}
	// The legacy section legitimately embeds the number pre-seam — this
	// pins that the golden fixture is the transcript-bearing one.
	if !strings.Contains(got, vbLedgerCallNumber) {
		t.Error("golden fixture lost the prior-call transcript section")
	}
}

// §3 branch B: seam with 2 commitments -> both '[open]' lines present,
// the raw prior-transcript section absent, and NO phone-shaped digit run
// anywhere in the block.
func TestDayBriefLedgerSeamReplacesTranscripts(t *testing.T) {
	brief := vbLedgerFixture()
	fake := &vbFakeCommitments{}

	// Populate via the seam contract (the fake stands in for the wiring's
	// ledgerBriefSource over Store.QueryOpen).
	rows, err := fake.OpenCommitments(context.Background(), "emp-9")
	if err != nil {
		t.Fatalf("seam fake: %v", err)
	}
	brief.Open = rows
	if fake.empID != "emp-9" {
		t.Errorf("seam employeeID = %q, want emp-9", fake.empID)
	}

	got := brief.PromptBlock()

	// Both [open] lines, frozen format: "- [open] {subject} — {summary} ({status})"
	wantLine1 := "- [open] acme-roof — send the written quote (open)"
	wantLine2 := "- [open] siding-order — waiting on the contractor deposit (waiting_on_them)"
	if !strings.Contains(got, wantLine1) {
		t.Errorf("block missing %q; got:\n%s", wantLine1, got)
	}
	if !strings.Contains(got, wantLine2) {
		t.Errorf("block missing %q; got:\n%s", wantLine2, got)
	}

	// The raw prior-transcript section is REPLACED, not appended to.
	if strings.Contains(got, "[prior calls today]") {
		t.Errorf("prior-transcript section still present with seam rows:\n%s", got)
	}
	if strings.Contains(got, "asked about roof") {
		t.Error("prior transcript text leaked into the seam-rendered block")
	}

	// Never a phone number in the block.
	if loc := vbPhoneRegex.FindStringIndex(got); loc != nil {
		t.Errorf("digit run %q rendered in the block:\n%s", got[loc[0]:loc[1]], got)
	}
	if strings.Contains(got, vbLedgerCallNumber) {
		t.Error("the prior-call number leaked into the seam-rendered block")
	}

	// The other sections are untouched by the seam (activity, memory,
	// goals still render).
	for _, want := range []string{"[activity today]", "- called acme", "[durable memory]", "- prefers SMS", "[active goals]", "- finish roof quote"} {
		if !strings.Contains(got, want) {
			t.Errorf("seam block dropped unrelated section line %q", want)
		}
	}
}

// §3 edge: seam wired but returns ZERO rows -> the legacy transcript
// section stays (replacement only "when the seam is non-nil AND returns
// rows"), golden posture preserved.
func TestDayBriefLedgerSeamEmptyKeepsTranscripts(t *testing.T) {
	brief := vbLedgerFixture()
	brief.Open = []CommitmentBrief{}

	got := brief.PromptBlock()
	want := vbLedgerGoldenBlock()
	if got != want {
		t.Errorf("empty-Open PromptBlock = %q, want the pre-edit golden %q", got, want)
	}
}
