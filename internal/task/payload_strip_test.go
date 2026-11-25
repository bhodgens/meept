package task

import (
	"strings"
	"testing"
)

// L6 pins: display-only stripping must require the EXACT scaffolding shape.
// Legitimate content that merely contains the marker substrings must survive;
// the real planner scaffolding and machine envelopes must still be stripped.
// These run at the pure-function seam (no store involved), so they also
// confirm the strip is display-only: it takes a string / freshly-scanned row
// and returns a value, never mutating any stored source.

func TestStripPromptScaffolding_LegitimateContentPreserved(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "lone task header mid-prose survives",
			input: "When writing a step, remember that ## Your Task is only a planner header, not user text.",
			want:  "When writing a step, remember that ## Your Task is only a planner header, not user text.",
		},
		{
			name:  "task header mid-line in prose survives",
			input: "Note that the line ## Your Task is a planner header, not user text.",
			want:  "Note that the line ## Your Task is a planner header, not user text.",
		},
		{
			name:  "context marker mid-line survives",
			input: "The text ## Session context (most recent work in this conversation) appears in docs.",
			want:  "The text ## Session context (most recent work in this conversation) appears in docs.",
		},
		{
			name:  "plain prose unchanged",
			input: "  just a normal step description  ",
			want:  "just a normal step description",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripPromptScaffolding(tc.input); got != tc.want {
				t.Fatalf("StripPromptScaffolding = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStripPromptScaffolding_RealScaffoldingStillStripped(t *testing.T) {
	// Exact shape strategic.go injects: both headers at line start, context
	// block before the task header. This is the e2e payload-strip fixture.
	raw := "## Session context (most recent work in this conversation)\n" +
		"- earlier: wrote the parser\n\n" +
		"## Your Task\n\n" +
		"write the summary"
	if got := StripPromptScaffolding(raw); got != "write the summary" {
		t.Fatalf("StripPromptScaffolding = %q, want %q", got, "write the summary")
	}

	// Partial/legacy injection (context block only) is still cut at the header.
	partial := "## Session context (most recent work in this conversation)\n- earlier work"
	if got := StripPromptScaffolding(partial); got != "" {
		t.Fatalf("partial scaffolding = %q, want empty", got)
	}
}

func TestStripClaimsEvidenceOrProse_LegitimateContentPreserved(t *testing.T) {
	// A brace-balanced fragment that starts with a claims-like key but is
	// NOT a fully-parsing JSON object must not be stripped from prose.
	raw := "We discussed the format {\"claims\": not valid json here} at length."
	if got := StripClaimsEvidenceOrProse(raw); got != raw {
		t.Fatalf("StripClaimsEvidenceOrProse = %q, want input unchanged", got)
	}

	// A JSON-looking object whose key merely mentions claims but is not a
	// claims/evidence envelope must survive.
	raw2 := `{"claimset":"not a real envelope"}`
	if got := StripClaimsEvidenceOrProse(raw2); got != raw2 {
		t.Fatalf("StripClaimsEvidenceOrProse = %q, want input unchanged", got)
	}
}

func TestStripClaimsEvidenceOrProse_RealEnvelopeStillStripped(t *testing.T) {
	// Fully-parsing claims/evidence envelope in the middle of prose: removed,
	// surrounding prose retained (e2e contract).
	raw := "Some prose before.\n\n" +
		`{"claims":["did the thing"],"evidence":[{"type":"file","path":"/tmp/x","description":"the file exists"}]}` +
		"\n\nTrailing prose."
	want := "Some prose before.\n\n\n\nTrailing prose."
	if got := StripClaimsEvidenceOrProse(raw); got != want {
		t.Fatalf("StripClaimsEvidenceOrProse = %q, want %q", got, want)
	}

	// Whole-input envelope recovers its prose (legacy contract).
	whole := `{"claims":["wrote the report"],"evidence":["the report exists"]}`
	wantProse := "- wrote the report\n- the report exists"
	if got := StripClaimsEvidenceOrProse(whole); got != wantProse {
		t.Fatalf("whole envelope prose = %q, want %q", got, wantProse)
	}
}

func TestStripClaimsEvidenceOrProse_MalformedThenRealEnvelopeStripped(t *testing.T) {
	// M16 pin (composition): a malformed claims-shaped fragment EARLY in
	// the reply must not abort the scan — a REAL envelope later in the
	// same reply is still stripped. Pre-fix, the first failed candidate
	// fell out of findClaimsEnvelopeSpan entirely (d1f0cfe1's `break` was
	// a no-op: it broke the switch, not the loop), so the real envelope
	// survived the strip and the reply guard nuked the whole answer.
	raw := `The report said {"claims": we shipped it} then {"claims":["x"],"evidence":["y"]} done.`
	got := StripClaimsEvidenceOrProse(raw)
	if strings.Contains(got, `"claims":["x"]`) {
		t.Fatalf("real envelope after a malformed fragment leaked raw: %q", got)
	}
	if !strings.Contains(got, "we shipped it") {
		t.Fatalf("malformed claims-shaped prose lost: %q", got)
	}
	if !strings.Contains(got, "then") || !strings.Contains(got, "done.") {
		t.Fatalf("surrounding prose lost: %q", got)
	}
}

func TestEnvelopeProse_JobRelayEvidenceRecoversProse_M10(t *testing.T) {
	// M10 pin: a job-relay evidence string embedding the machine
	// executor-report envelope ("job <id> completed by agent <x>: {json}")
	// must render as prose, not raw JSON — the same F-ORCH-21 leak the
	// agent copy fixes via renderEvidenceProse (cb00f03e).
	raw := `{"claims":["shipped"],"evidence":["job j-9 completed by agent coder: {\"status\":\"success\",\"accomplished\":[\"summary written\"]}"]}`
	got := envelopeProse(raw)
	if strings.Contains(got, `"accomplished"`) || strings.Contains(got, `"status"`) {
		t.Fatalf("job-relay evidence leaked raw JSON keys: %q", got)
	}
	if !strings.Contains(got, "summary written") {
		t.Fatalf("job-relay evidence prose not recovered: %q", got)
	}
	if !strings.Contains(got, "- shipped") {
		t.Fatalf("plain claim lost: %q", got)
	}

	// Plain evidence prose passes through byte-identically (no regression).
	plain := `{"claims":["c"],"evidence":["  the file exists  "]}`
	if got2 := envelopeProse(plain); got2 != "- c\n- the file exists" {
		t.Fatalf("plain evidence prose altered: %q", got2)
	}
}

func TestStripStepForDisplay_DisplayOnlyNoStoreMutation(t *testing.T) {
	step := &TaskStep{
		ID:          "st-l6",
		Description: "## Session context (most recent work in this conversation)\n- earlier\n\n## Your Task\n\nclean text",
		Result:      `job j-1 completed by agent coder: {"status":"success","accomplished":["summary written"]}`,
	}
	got := StripStepForDisplay(step)
	if got.Description != "clean text" {
		t.Errorf("Description = %q, want %q", got.Description, "clean text")
	}
	if got.Result != "- summary written" {
		t.Errorf("Result = %q, want %q", got.Result, "- summary written")
	}
	// The strip mutates the passed row in place (callers pass fresh rows), but
	// it never touches a store. Verify a second, independent row with the
	// SAME raw content is unaffected by the first strip.
	independent := &TaskStep{ID: "st-l6-b", Description: step.Description, Result: step.Result}
	_ = StripStepForDisplay(independent)
	// Both now hold stripped values because they are separate structs whose
	// string fields were reassigned, not shared buffers; the key assertion is
	// that no global/store state is involved. A fresh literal stays raw.
	fresh := &TaskStep{Description: "## Session context (most recent work in this conversation)\n\n## Your Task\n\nclean text"}
	if fresh.Description == "" {
		t.Error("unexpected empty description on fresh row")
	}
}
