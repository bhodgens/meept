package modelrating_test

// Gate tests for master.md contract C1 (rubric catalog) - leaf 00T-contracts.
// These tests intentionally fail RED until leaf 01-rubric-catalog lands the
// `internal/modelrating` package (package missing = the RED reason).
// TESTS ONLY: no implementation lives here.

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/modelrating"
)

// frozenCategories / frozenSeverities mirror master.md C1 verbatim. The gate
// pins the vocabulary through Verdict.Validate (the only observable C1
// surface): every frozen value must validate, non-frozen values must not.
var frozenCategories = []modelrating.Category{
	"pass",
	"wrong_language",
	"wrong_api_or_dependency",
	"logic_error",
	"security_defect",
	"incomplete_implementation",
	"unrequested_change",
	"build_or_test_failure",
	"style_bloat",
	"convention_violation",
}

var frozenSeverities = []modelrating.Severity{
	"cosmetic",
	"functional",
	"blocking",
	"critical",
}

// MR-CAT-1: every rubric ID in the catalog is unique.
func TestMR_CAT_1_RubricIDsUnique(t *testing.T) {
	rubrics := modelrating.Rubrics()
	if len(rubrics) == 0 {
		t.Fatal("Rubrics() returned an empty catalog")
	}
	seen := make(map[modelrating.RubricID]bool, len(rubrics))
	for _, r := range rubrics {
		if r.ID == "" {
			t.Fatalf("rubric with empty ID: %+v", r)
		}
		if seen[r.ID] {
			t.Fatalf("duplicate rubric ID %q", r.ID)
		}
		seen[r.ID] = true
	}
}

// MR-CAT-2: the defect-category vocabulary is the frozen set of 10 values
// (9 defect categories + "pass"): every frozen category validates on a defect
// verdict, "pass" validates with empty severity, and non-frozen categories are
// rejected.
func TestMR_CAT_2_CategoriesFrozen(t *testing.T) {
	for _, cat := range frozenCategories[1:] { // defect categories
		v := modelrating.Verdict{
			RubricID: "MR-LC1",
			Scope:    modelrating.ScopeModel,
			Category: cat,
			Severity: "functional",
		}
		if err := v.Validate(); err != nil {
			t.Errorf("frozen category %q must validate on a defect verdict: %v", cat, err)
		}
	}
	pass := modelrating.Verdict{
		RubricID: "MR-LC1",
		Scope:    modelrating.ScopeModel,
		Category: "pass",
		Severity: modelrating.SeverityNone,
	}
	if err := pass.Validate(); err != nil {
		t.Errorf("frozen category \"pass\" must validate with empty severity: %v", err)
	}
	for _, bad := range []string{"defect", "Pass", "PASS", "", "style", "other"} {
		v := modelrating.Verdict{
			RubricID: "MR-LC1",
			Scope:    modelrating.ScopeModel,
			Category: modelrating.Category(bad),
			Severity: "functional",
		}
		if err := v.Validate(); err == nil {
			t.Errorf("non-frozen category %q must be rejected", bad)
		}
	}
}

// MR-CAT-3: the severity vocabulary is the frozen 4-level set, "pass" rows
// carry the empty severity (SeverityNone), and invalid severity rules are
// rejected.
func TestMR_CAT_3_SeveritiesFrozen(t *testing.T) {
	if modelrating.SeverityNone != "" {
		t.Fatalf("SeverityNone = %q, want empty string on pass rows", modelrating.SeverityNone)
	}
	for _, sev := range frozenSeverities {
		v := modelrating.Verdict{
			RubricID: "MR-LC1",
			Scope:    modelrating.ScopeModel,
			Category: "logic_error",
			Severity: sev,
		}
		if err := v.Validate(); err != nil {
			t.Errorf("frozen severity %q must validate on a defect verdict: %v", sev, err)
		}
	}
	for _, bad := range []struct {
		category modelrating.Category
		severity modelrating.Severity
		note     string
	}{
		{modelrating.Category("pass"), modelrating.Severity("critical"), "pass row with non-empty severity"},
		{modelrating.Category("pass"), modelrating.Severity("cosmetic"), "pass row with cosmetic severity"},
		{modelrating.Category("logic_error"), modelrating.Severity(""), "defect row with empty severity"},
		{modelrating.Category("logic_error"), modelrating.Severity("minor"), "defect row with out-of-vocab severity"},
		{modelrating.Category("logic_error"), modelrating.Severity("Critical"), "defect row with case-drifted severity"},
	} {
		v := modelrating.Verdict{
			RubricID: "MR-LC1",
			Scope:    modelrating.ScopeModel,
			Category: bad.category,
			Severity: bad.severity,
		}
		if err := v.Validate(); err == nil {
			t.Errorf("invalid severity rule (%s: %q/%q) must be rejected", bad.note, bad.category, bad.severity)
		}
	}
}

// MR-CAT-4: scopes are exactly {model, workflow} and the Rubrics() catalog
// covers the issue's MODEL list and WORKFLOW list (keyword coverage).
func TestMR_CAT_4_ScopesAndCatalogCoverage(t *testing.T) {
	if modelrating.ScopeModel != "model" || modelrating.ScopeWorkflow != "workflow" {
		t.Fatalf("scope constants = %q/%q, want \"model\"/\"workflow\"",
			modelrating.ScopeModel, modelrating.ScopeWorkflow)
	}

	rubrics := modelrating.Rubrics()
	modelCount, workflowCount := 0, 0
	for _, r := range rubrics {
		switch r.Scope {
		case modelrating.ScopeModel:
			modelCount++
		case modelrating.ScopeWorkflow:
			workflowCount++
		default:
			t.Fatalf("rubric %q has scope %q outside {model, workflow}", r.ID, r.Scope)
		}
		if r.Statement == "" {
			t.Fatalf("rubric %q has an empty statement", r.ID)
		}
	}

	// Issue #168 frozen scope lists: keyword that must appear in some
	// rubric statement of the given scope.
	coverage := []struct {
		scope   modelrating.RubricScope
		keyword string
		note    string
	}{
		{modelrating.ScopeModel, "line", "line-number corruption"},
		{modelrating.ScopeModel, "todo", "stray debug artifacts/TODOs/placeholders"},
		{modelrating.ScopeModel, "heredoc", "heredocs in code assembly"},
		{modelrating.ScopeModel, "grep", "grep -c/-q zero-match aborts"},
		{modelrating.ScopeModel, "paraphras", "paraphrase drift"},
		{modelrating.ScopeModel, "convention", "inconsistent conventions"},
		{modelrating.ScopeModel, "rate limit", "provider rate limits ignored"},
		{modelrating.ScopeWorkflow, "leaf", "leaf sizing"},
		{modelrating.ScopeWorkflow, "interface contract", "interface contracts"},
		{modelrating.ScopeWorkflow, "test gate", "test gate"},
		{modelrating.ScopeWorkflow, "review", "orchestrator-review-skipped"},
		{modelrating.ScopeWorkflow, "status", "status trickle/reset"},
		{modelrating.ScopeWorkflow, "circular", "circular dependencies"},
		{modelrating.ScopeWorkflow, "context budget", "context budget"},
		{modelrating.ScopeWorkflow, "sequential", "sequential dispatch"},
		{modelrating.ScopeWorkflow, "commit", "leaf-agent-committed-code"},
		{modelrating.ScopeWorkflow, "plan", "missing/stale plan sections"},
		{modelrating.ScopeWorkflow, "sibling", "sibling design drift"},
	}
	stmtLower := func(r modelrating.Rubric) string { return strings.ToLower(r.Statement) }
	for _, c := range coverage {
		found := false
		for _, r := range rubrics {
			if r.Scope != c.scope {
				continue
			}
			if strings.Contains(stmtLower(r), c.keyword) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no %s-scope rubric statement covers %q (%s)", c.scope, c.keyword, c.note)
		}
	}

	if modelCount == 0 {
		t.Error("catalog has no model-scope rubrics")
	}
	if workflowCount == 0 {
		t.Error("catalog has no workflow-scope rubrics")
	}
}
