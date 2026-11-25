package agent

// M5 gate pins: the reviewer lane must PARSE the model-emitted verdicts array
// out of the reviewer JSON into ReviewResult.Verdicts (frozen C1 vocab via
// modelrating.Verdict.Validate, invalid rows dropped, unknown rubric ids
// kept), and buildReviewPrompt must ASK for verdicts (optionally) so the
// model_review_verdicts table stops being ~100% synthetic pass rows.
//
// Legacy posture (pin iii): a reviewer that returns the old 4-key shape
// parses byte-identically to before — Verdicts stays empty and the payload
// carries exactly the synthetic pass row (verdict_emission_gate_test.go
// pins the emission side).

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/modelrating"
	"github.com/caimlas/meept/internal/task"
)

func newM5ReviewManager(t *testing.T) *ReviewManager {
	t.Helper()
	return NewReviewManager(ReviewManagerConfig{Logger: slogDiscardLogger()})
}

// payloadVerdicts extracts the frozen "verdicts" wire array from an
// enriched review payload map.
func payloadVerdicts(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()
	raw, err := json.Marshal(payload["verdicts"])
	if err != nil {
		t.Fatalf("marshal verdicts payload: %v", err)
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("verdicts wire = %s, want an array", raw)
	}
	return out
}

// M5(i): reviewer JSON carrying a verdicts array populates result.Verdicts
// with exact field mapping, and the enriched payload carries them with NO
// pass row when defects are present (passVerdictFor contract).
func TestM5_ParseVerdicts_PopulatesResultAndPayload(t *testing.T) {
	rm := newM5ReviewManager(t)
	output := `{
		"status": "approved",
		"feedback": "works, with findings",
		"issues": ["off-by-one"],
		"confidence": 0.8,
		"verdicts": [
			{"rubric_id": "MR-LC1", "scope": "model", "category": "logic_error",
			 "severity": "functional", "detail": "off-by-one in the retry loop", "confidence": 0.9},
			{"rubric_id": "MR-WF3", "scope": "workflow", "category": "build_or_test_failure",
			 "severity": "blocking", "detail": "test gate skipped", "confidence": 0.7}
		]
	}`

	result := rm.parseReviewResult(output)
	if len(result.Verdicts) != 2 {
		t.Fatalf("Verdicts = %d rows, want 2 (reviewer-emitted verdicts never parsed? got %+v)", len(result.Verdicts), result.Verdicts)
	}
	want := []modelrating.Verdict{
		{RubricID: "MR-LC1", Scope: modelrating.ScopeModel, Category: modelrating.CategoryLogicError,
			Severity: modelrating.SeverityFunctional, Detail: "off-by-one in the retry loop", Confidence: 0.9},
		{RubricID: "MR-WF3", Scope: modelrating.ScopeWorkflow, Category: modelrating.CategoryBuildOrTestFailure,
			Severity: modelrating.SeverityBlocking, Detail: "test gate skipped", Confidence: 0.7},
	}
	for i, w := range want {
		if result.Verdicts[i] != w {
			t.Errorf("verdict %d = %+v, want %+v", i, result.Verdicts[i], w)
		}
	}

	// Emission half: the payload verdicts key carries the parsed rows and
	// NO synthetic pass row is appended (approved-with-findings).
	payload := rm.enrichedReviewPayload("step-m5-1", "task-m5-1", result, 0)
	rows := payloadVerdicts(t, payload)
	if len(rows) != 2 {
		t.Fatalf("payload verdicts = %d rows, want 2 (defects only, no pass row)", len(rows))
	}
	for _, row := range rows {
		if row["category"] == string(modelrating.CategoryPass) {
			t.Errorf("payload carries a synthetic pass row alongside defects: %v", rows)
		}
	}
	if rows[0]["rubric_id"] != "MR-LC1" || rows[0]["scope"] != "model" ||
		rows[0]["category"] != "logic_error" || rows[0]["severity"] != "functional" ||
		rows[0]["detail"] != "off-by-one in the retry loop" || rows[0]["confidence"] != 0.9 {
		t.Errorf("wire row 0 mismatch: %v", rows[0])
	}
}

// M5(ii): rows violating the frozen C1 vocab (unknown category, pass with a
// severity, unknown scope) are DROPPED at parse; valid siblings survive.
func TestM5_ParseVerdicts_DropsInvalidKeepsValid(t *testing.T) {
	rm := newM5ReviewManager(t)
	output := `{
		"status": "rejected",
		"feedback": "nope",
		"issues": ["bad"],
		"confidence": 0.9,
		"verdicts": [
			{"rubric_id": "MR-X9", "scope": "model", "category": "totally_made_up",
			 "severity": "functional", "detail": "invalid category", "confidence": 0.5},
			{"rubric_id": "", "scope": "model", "category": "pass",
			 "severity": "cosmetic", "detail": "pass with severity is invalid", "confidence": 0.5},
			{"rubric_id": "MR-ZZ", "scope": "chaos", "category": "logic_error",
			 "severity": "functional", "detail": "invalid scope", "confidence": 0.5},
			{"rubric_id": "MR-CV1", "scope": "model", "category": "convention_violation",
			 "severity": "cosmetic", "detail": "naming drift", "confidence": 0.6}
		]
	}`

	result := rm.parseReviewResult(output)
	if len(result.Verdicts) != 1 {
		t.Fatalf("Verdicts = %d rows, want exactly 1 (3 vocab violations dropped): %+v", len(result.Verdicts), result.Verdicts)
	}
	v := result.Verdicts[0]
	if v.RubricID != "MR-CV1" || v.Category != modelrating.CategoryConventionViolation ||
		v.Severity != modelrating.SeverityCosmetic || v.Scope != modelrating.ScopeModel {
		t.Errorf("surviving row = %+v, want the valid convention_violation sibling", v)
	}
}

// M5(iii): the legacy 4-key reviewer JSON stays byte-identical — no parsed
// verdicts, and the payload is exactly the synthetic pass row produced by
// passVerdictFor today.
func TestM5_LegacyFourKeyJSON_StaysSyntheticPassOnly(t *testing.T) {
	rm := newM5ReviewManager(t)
	output := `{"status": "approved", "feedback": "looks good", "issues": [], "confidence": 0.9}`

	result := rm.parseReviewResult(output)
	if len(result.Verdicts) != 0 {
		t.Fatalf("legacy 4-key JSON parsed %d verdicts, want 0: %+v", len(result.Verdicts), result.Verdicts)
	}
	if result.Status != ReviewApproved || result.Feedback != "looks good" || result.Confidence != 0.9 {
		t.Errorf("legacy scalar fields changed: %+v", result)
	}

	payload := rm.enrichedReviewPayload("step-m5-legacy", "task-m5-legacy", result, 0)
	rows := payloadVerdicts(t, payload)
	if len(rows) != 1 {
		t.Fatalf("legacy payload verdicts = %d rows, want exactly 1 synthetic pass row: %v", len(rows), rows)
	}
	got, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatalf("marshal row: %v", err)
	}
	const wantRow = `{"category":"pass","confidence":0.9,"detail":"approved: no defects found","rubric_id":"","scope":"model","severity":""}`
	if string(got) != wantRow {
		t.Errorf("legacy synthetic pass row drifted:\n got %s\nwant %s", got, wantRow)
	}
}

// M5(iv): a model-emitted rubric_id unknown to the catalog is KEPT — the
// emitter is lenient by contract (verdictsToWire doc); only the vocab
// (scope/category/severity) is enforced.
func TestM5_ParseVerdicts_UnknownRubricIDKept(t *testing.T) {
	rm := newM5ReviewManager(t)
	output := `{
		"status": "approved",
		"feedback": "fine",
		"issues": [],
		"confidence": 0.8,
		"verdicts": [
			{"rubric_id": "MR-NOPE-99", "scope": "model", "category": "style_bloat",
			 "severity": "cosmetic", "detail": "not a real catalog rubric", "confidence": 0.4}
		]
	}`

	result := rm.parseReviewResult(output)
	if len(result.Verdicts) != 1 {
		t.Fatalf("Verdicts = %d rows, want 1 (unknown rubric_id must survive)", len(result.Verdicts))
	}
	if result.Verdicts[0].RubricID != modelrating.RubricID("MR-NOPE-99") {
		t.Errorf("RubricID = %q, want the model's id verbatim", result.Verdicts[0].RubricID)
	}
}

// M5 empty/absent verdicts array posture: "verdicts": [] parses clean and
// leaves Verdicts nil (legacy synthetic-pass behavior preserved).
func TestM5_EmptyVerdictsArray_LegacyBehavior(t *testing.T) {
	rm := newM5ReviewManager(t)
	output := `{"status": "approved", "feedback": "ok", "issues": [], "confidence": 0.7, "verdicts": []}`
	result := rm.parseReviewResult(output)
	if len(result.Verdicts) != 0 {
		t.Fatalf("empty verdicts array parsed %d rows, want 0", len(result.Verdicts))
	}
	payload := rm.enrichedReviewPayload("step-m5-empty", "task-m5-empty", result, 0)
	if rows := payloadVerdicts(t, payload); len(rows) != 1 || rows[0]["category"] != string(modelrating.CategoryPass) {
		t.Errorf("empty verdicts array lost the synthetic pass row: %v", rows)
	}
}

// M5 prompt half: buildReviewPrompt must ask (optionally) for the verdicts
// array with the frozen vocab so the reviewer actually emits it.
func TestM5_ReviewPrompt_AsksForVerdicts(t *testing.T) {
	rm := newM5ReviewManager(t)
	step := &task.TaskStep{
		ID:          "step-m5-prompt",
		TaskID:      "task-m5",
		Description: "Fix the parser",
		ToolHint:    "code",
		AgentID:     "coder",
		Result:      `{"success": true}`,
		Sequence:    0,
	}
	prompt := rm.buildReviewPrompt(step, nil)

	// The addition must read as OPTIONAL so a legacy 4-key output still
	// satisfies the contract (pins iii's posture).
	if !strings.Contains(strings.ToLower(prompt), "optional") {
		t.Error("verdicts ask must be phrased optional")
	}
	for _, frag := range []string{
		"verdicts", "rubric_id",
		"wrong_language", "wrong_api_or_dependency", "logic_error", "security_defect",
		"incomplete_implementation", "unrequested_change", "build_or_test_failure",
		"style_bloat", "convention_violation",
		"cosmetic", "functional", "blocking", "critical",
		"model", "workflow",
	} {
		if !strings.Contains(prompt, frag) {
			t.Errorf("review prompt missing verdict contract fragment %q", frag)
		}
	}
	// pass is a status, not a defect grade — the prompt must steer defect-only.
	if strings.Contains(prompt, `"category": "pass"`) {
		t.Error(`prompt must not offer "pass" as a verdict category`)
	}
}
