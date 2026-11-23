// Package modelrating holds the frozen rubric catalog and verdict vocabulary
// for model-rating metrics (issue #168, master.md contract C1).
//
// The category and severity strings are wire values persisted in SQLite and
// returned by the HTTP surfaces; they are frozen and must not be renamed or
// reordered. Rubric IDs follow "MR-" + two-letter scope code + digit and are
// frozen once committed.
package modelrating

import "fmt"

// RubricID identifies a catalog rubric, e.g. "MR-LC1" (line corruption).
type RubricID string

// RubricScope distinguishes model defects (blame the reviewed model) from
// workflow defects (blame the orchestrator/planner).
type RubricScope string

const (
	// ScopeModel rubrics grade the reviewed model's output.
	ScopeModel RubricScope = "model"
	// ScopeWorkflow rubrics grade the planner/orchestrator process.
	ScopeWorkflow RubricScope = "workflow"
)

// Category is the frozen defect taxonomy (issue #168).
type Category string

// Frozen defect categories; "pass" marks a verdict-free row.
const (
	CategoryPass                     Category = "pass"
	CategoryWrongLanguage            Category = "wrong_language"
	CategoryWrongAPIDependency       Category = "wrong_api_or_dependency"
	CategoryLogicError               Category = "logic_error"
	CategorySecurityDefect           Category = "security_defect"
	CategoryIncompleteImplementation Category = "incomplete_implementation"
	CategoryUnrequestedChange        Category = "unrequested_change"
	CategoryBuildOrTestFailure       Category = "build_or_test_failure"
	CategoryStyleBloat               Category = "style_bloat"
	CategoryConventionViolation      Category = "convention_violation"
)

// Severity is the frozen 4-level impact scale.
type Severity string

// Frozen severities; pass rows carry SeverityNone ("").
const (
	SeverityNone       Severity = ""
	SeverityCosmetic   Severity = "cosmetic"
	SeverityFunctional Severity = "functional"
	SeverityBlocking   Severity = "blocking"
	SeverityCritical   Severity = "critical"
)

// Verdict is one rubric grade emitted by a review judge.
type Verdict struct {
	RubricID   RubricID
	Scope      RubricScope
	Category   Category
	Severity   Severity
	Detail     string
	Confidence float64
}

// defectCategories is the frozen defect set (every category except "pass").
var defectCategories = map[Category]bool{
	CategoryWrongLanguage:            true,
	CategoryWrongAPIDependency:       true,
	CategoryLogicError:               true,
	CategorySecurityDefect:           true,
	CategoryIncompleteImplementation: true,
	CategoryUnrequestedChange:        true,
	CategoryBuildOrTestFailure:       true,
	CategoryStyleBloat:               true,
	CategoryConventionViolation:      true,
}

// validSeverities is the frozen 4-level severity vocabulary.
var validSeverities = map[Severity]bool{
	SeverityCosmetic:   true,
	SeverityFunctional: true,
	SeverityBlocking:   true,
	SeverityCritical:   true,
}

// Validate enforces contract C1: the scope and category must be in the frozen
// vocabularies, pass rows must carry SeverityNone, and defect rows must carry
// one of the 4 frozen severities.
func (v Verdict) Validate() error {
	if v.Scope != ScopeModel && v.Scope != ScopeWorkflow {
		return fmt.Errorf("modelrating: invalid scope %q", v.Scope)
	}
	if v.Category != CategoryPass && !defectCategories[v.Category] {
		return fmt.Errorf("modelrating: invalid category %q", v.Category)
	}
	if v.Category == CategoryPass {
		if v.Severity != SeverityNone {
			return fmt.Errorf("modelrating: pass verdict must carry empty severity, got %q", v.Severity)
		}
		return nil
	}
	if !validSeverities[v.Severity] {
		return fmt.Errorf("modelrating: defect category %q needs severity in {cosmetic, functional, blocking, critical}, got %q", v.Category, v.Severity)
	}
	return nil
}

// Rubric is one catalog entry: an ID, its scope, and a one-sentence grading
// statement that becomes the judge's prompt material.
type Rubric struct {
	ID        RubricID
	Scope     RubricScope
	Statement string
}

// RubricCatalogVersion is the version of the rubric catalog frozen in this
// file. Verdict rows stamp the version they were graded under (the
// model_review_verdicts.rubric_version column), so a future catalog revision
// is distinguishable from stored history instead of silently mixing into
// aggregates. Bump this constant IN THE SAME COMMIT as any catalog change:
// adding, removing, re-scoping, or rewording-a-statement-of a rubric.
const RubricCatalogVersion = "1"

// catalog is the full rubric catalog in stable order. Statements are one
// sentence each; IDs are frozen once committed.
var catalog = []Rubric{
	// Model scope: defects attributable to the reviewed model's output.
	{ID: "MR-LC1", Scope: ScopeModel, Statement: "Did the model corrupt, hallucinate, or drop line numbers or other positional references when reproducing or editing code?"},
	{ID: "MR-TD1", Scope: ScopeModel, Statement: "Did the model leave stray debug artifacts, TODOs, or placeholder stubs in the delivered code?"},
	{ID: "MR-HD1", Scope: ScopeModel, Statement: "Did the model misuse heredocs or shell quoting when assembling code, breaking the assembled output?"},
	{ID: "MR-GQ1", Scope: ScopeModel, Statement: "Did the model use grep -c or grep -q in a way that aborts on zero matches instead of handling the empty result?"},
	{ID: "MR-PD1", Scope: ScopeModel, Statement: "Did the model paraphrase a requirement or interface instead of following it verbatim, drifting from the specified behavior?"},
	{ID: "MR-CV1", Scope: ScopeModel, Statement: "Did the model violate the codebase's conventions, applying inconsistent naming, formatting, or structure across the change?"},
	{ID: "MR-RL1", Scope: ScopeModel, Statement: "Did the model ignore provider rate limits, retrying or pacing calls in a way that triggers throttling or failures?"},
	// Workflow scope: defects attributable to the planner/orchestrator process.
	{ID: "MR-WF1", Scope: ScopeWorkflow, Statement: "Was a plan leaf sized outside its budget, too large to land in one pass or too small to justify dispatch?"},
	{ID: "MR-WF2", Scope: ScopeWorkflow, Statement: "Was an interface contract missing, stale, or violated between sibling leaves?"},
	{ID: "MR-WF3", Scope: ScopeWorkflow, Statement: "Was a test gate skipped, weakened, or left failing when a leaf was declared complete?"},
	{ID: "MR-WF4", Scope: ScopeWorkflow, Statement: "Did the orchestrator skip or fake a review cycle instead of running a real reviewer pass?"},
	{ID: "MR-WF5", Scope: ScopeWorkflow, Statement: "Did leaf or task status trickle updates late or reset incorrectly across cycles?"},
	{ID: "MR-WF6", Scope: ScopeWorkflow, Statement: "Did the plan create circular dependencies between leaves that blocked or deadlocked dispatch?"},
	{ID: "MR-WF7", Scope: ScopeWorkflow, Statement: "Did a leaf exceed its context budget, forcing truncation or loss of required context?"},
	{ID: "MR-WF8", Scope: ScopeWorkflow, Statement: "Were independent leaves dispatched sequentially when they could have run in parallel, or dependent ones in parallel when they required sequential order?"},
	{ID: "MR-WF9", Scope: ScopeWorkflow, Statement: "Did a leaf agent commit code directly instead of leaving the commit to the orchestrator?"},
	{ID: "MR-WF10", Scope: ScopeWorkflow, Statement: "Was a plan section missing or stale relative to the implemented state, leaving the tree out of sync?"},
	{ID: "MR-WF11", Scope: ScopeWorkflow, Statement: "Did a sibling leaf drift from an interface or design decision another leaf already committed to?"},
}

// Rubrics returns the full rubric catalog in stable order. The returned slice
// is a copy; callers cannot mutate the catalog.
func Rubrics() []Rubric {
	out := make([]Rubric, len(catalog))
	copy(out, catalog)
	return out
}
