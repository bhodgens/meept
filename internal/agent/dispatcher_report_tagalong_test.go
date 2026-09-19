package agent

import "testing"

// Pins for classifyReportTagAlong (2026-09-18 tool-boundary-hardening leaf 04,
// report tag-along arbitration). The motivating failure is e2e T1:
// "create a file named hello.txt …, then tell me the full path" — the report
// clause asks to SURFACE the first action's output, but the multi-intent
// classifier emitted code+report, DetectCompound fired, and a one-file task
// routed into a planner pair session. A report clause that references the
// action's output (path/result/output/content, demonstratives, possessive
// artifact references) is a READBACK and must report true; a report clause
// with its own work verb (F42: "write a summary and a report") is a second
// DELIVERABLE and must report false.

// TestReportTagAlong_Positive pins the readback shapes: object noun or
// demonstrative/possessive reference to the first action's output.
func TestReportTagAlong_Positive(t *testing.T) {
	const action = "create a file named hello.txt in the current directory containing the word hello"
	cases := []struct {
		name   string
		report string
	}{
		{"T1 e2e path readback", "then tell me the full path"},
		{"result readback", "and show me the result"},
		{"demonstrative it", "then tell me where it is"},
		{"demonstrative that", "then show me that"},
		{"them as output reference", "then list them for me"},
		{"no work verb at all", "the full path please"},
		{"possessive artifact reference", "then tell me the file's path"},
		{"its possessive", "then show me its content"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !classifyReportTagAlong(action, tc.report) {
				t.Fatalf("classifyReportTagAlong(action, %q) = false, want true (output readback)", tc.report)
			}
		})
	}
}

// TestReportTagAlong_IndependentDeliverable pins the F42 protection: the
// report clause has its OWN work verb and describes an independent artifact —
// two deliverables, never a readback.
func TestReportTagAlong_IndependentDeliverable(t *testing.T) {
	cases := []struct {
		name           string
		action, report string
	}{
		{"F42 summary+report", "write a summary of the doc", "then write a report about the findings"},
		{"F42 config+report", "create the config for the deployment", "and write a report"},
		{"own generate verb", "fix the flaky test", "then generate a report on the test suite"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if classifyReportTagAlong(tc.action, tc.report) {
				t.Fatalf("classifyReportTagAlong(%q, %q) = true, want false (independent second deliverable)",
					tc.action, tc.report)
			}
		})
	}
}

// TestReportTagAlong_NoWorkVerbNeeded pins that a verbless report clause still
// counts as a readback when it names the action's output ("the full path
// please" — the exact T1 tail without "tell me").
func TestReportTagAlong_NoWorkVerbNeeded(t *testing.T) {
	if !classifyReportTagAlong(
		"create a file named hello.txt in the current directory containing the word hello",
		"the full path please") {
		t.Fatal("verbless output-reference clause must classify as a readback")
	}
}

// TestReportTagAlong_NewTopic pins the new-topic negative: the report clause
// introduces a noun that has nothing to do with the action's output and
// carries no output-reference signal — it is a fresh request, not a readback.
func TestReportTagAlong_NewTopic(t *testing.T) {
	if classifyReportTagAlong("fix the flaky test", "then tell me the weather") {
		t.Fatal("new-topic clause must not classify as a readback")
	}
}
