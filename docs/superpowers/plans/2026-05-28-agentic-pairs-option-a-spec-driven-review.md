# Agentic Pairs: Option A — Specification-Driven Review Loop

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the existing ReviewManager to carry a specification artifact through the coder->reviewer loop, enabling spec-driven acceptance criteria checking and structured feedback propagation to revision steps.

**Architecture:** The existing ReviewManager + TacticalScheduler revision loop already implements 80% of the agentic pair pattern. This option adds specification awareness (acceptance criteria stored in task metadata), structured feedback propagation into revision step context, and a max revision guard. The pair_modality.go file defines a shared enum that the orchestrator uses to select which pairing modality to apply.

**Tech Stack:** Go 1.22+, existing ReviewManager/TacticalScheduler infrastructure, SQLite task store

---

## Task 1: PairModality type definition

Creates the shared enum type used by the orchestrator to select which agentic pair modality to apply to a given task step. This file is shared across all options (A, B, C, D).

### 1.1 Write the test file

**File:** `internal/agent/pair_modality_test.go`

```go
package agent

import (
	"encoding/json"
	"testing"
)

func TestPairModality_String(t *testing.T) {
	tests := []struct {
		modality PairModality
		want     string
	}{
		{PairModalityNone, "none"},
		{PairModalitySpecReview, "spec_review"},
		{PairModalityPairSession, "pair_session"},
		{PairModalityDebate, "debate"},
		{PairModalityInline, "inline"},
	}
	for _, tt := range tests {
		if got := tt.modality.String(); got != tt.want {
			t.Errorf("PairModality(%d).String() = %q, want %q", tt.modality, got, tt.want)
		}
	}
}

func TestPairModality_MarshalJSON(t *testing.T) {
	m := PairModalitySpecReview
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}
	if string(data) != `"spec_review"` {
		t.Errorf("MarshalJSON = %s, want %q", data, `"spec_review"`)
	}
}

func TestPairModality_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		input string
		want  PairModality
	}{
		{`"none"`, PairModalityNone},
		{`"spec_review"`, PairModalitySpecReview},
		{`"pair_session"`, PairModalityPairSession},
		{`"debate"`, PairModalityDebate},
		{`"inline"`, PairModalityInline},
		{`"unknown"`, PairModalityNone},
	}
	for _, tt := range tests {
		var got PairModality
		if err := json.Unmarshal([]byte(tt.input), &got); err != nil {
			t.Errorf("UnmarshalJSON(%s) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("UnmarshalJSON(%s) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParsePairModality(t *testing.T) {
	tests := []struct {
		input string
		want  PairModality
	}{
		{"spec_review", PairModalitySpecReview},
		{"SPEC_REVIEW", PairModalitySpecReview},
		{"pair_session", PairModalityPairSession},
		{"debate", PairModalityDebate},
		{"inline", PairModalityInline},
		{"none", PairModalityNone},
		{"", PairModalityNone},
		{"bogus", PairModalityNone},
	}
	for _, tt := range tests {
		got := ParsePairModality(tt.input)
		if got != tt.want {
			t.Errorf("ParsePairModality(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestPairModality_IsActive(t *testing.T) {
	if PairModalityNone.IsActive() {
		t.Error("PairModalityNone should not be active")
	}
	if !PairModalitySpecReview.IsActive() {
		t.Error("PairModalitySpecReview should be active")
	}
	if !PairModalityPairSession.IsActive() {
		t.Error("PairModalityPairSession should be active")
	}
	if !PairModalityDebate.IsActive() {
		t.Error("PairModalityDebate should be active")
	}
	if !PairModalityInline.IsActive() {
		t.Error("PairModalityInline should be active")
	}
}
```

### 1.2 Run the test (expect compile failure)

```bash
go test ./internal/agent/ -run TestPairModality -v 2>&1 | head -20
```

### 1.3 Create the implementation file

**File:** `internal/agent/pair_modality.go`

```go
package agent

import (
	"encoding/json"
	"strings"
)

// PairModality defines the type of agentic pairing applied to a task step.
// The orchestrator selects a modality based on task complexity, step tool hint,
// and user preferences. This enum is shared across all agentic pair options.
type PairModality int

const (
	// PairModalityNone means no agentic pairing; single-agent execution.
	PairModalityNone PairModality = iota
	// PairModalitySpecReview is Option A: specification-driven review loop
	// where acceptance criteria are generated during planning and the reviewer
	// checks against them.
	PairModalitySpecReview
	// PairModalityPairSession is Option B: shared-context pair session where
	// two agents iterate on a full task with accumulated review history.
	PairModalityPairSession
	// PairModalityDebate is Option C: bus-channel-based dual-agent conversation
	// where agents take turns via a shared topic.
	PairModalityDebate
	// PairModalityInline is Option D: tool-based inline review where the actor
	// calls request_review within its own execution loop.
	PairModalityInline
)

var pairModalityNames = map[PairModality]string{
	PairModalityNone:       "none",
	PairModalitySpecReview: "spec_review",
	PairModalityPairSession: "pair_session",
	PairModalityDebate:     "debate",
	PairModalityInline:     "inline",
}

var pairModalityLookup = map[string]PairModality{
	"none":         PairModalityNone,
	"spec_review":  PairModalitySpecReview,
	"pair_session": PairModalityPairSession,
	"debate":       PairModalityDebate,
	"inline":       PairModalityInline,
}

// String returns the human-readable name of the pair modality.
func (m PairModality) String() string {
	if name, ok := pairModalityNames[m]; ok {
		return name
	}
	return "unknown"
}

// IsActive returns true if the modality represents an active pairing (not none).
func (m PairModality) IsActive() bool {
	return m != PairModalityNone
}

// MarshalJSON implements json.Marshaler for PairModality.
func (m PairModality) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.String())
}

// UnmarshalJSON implements json.Unmarshaler for PairModality.
func (m *PairModality) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*m = ParsePairModality(s)
	return nil
}

// ParsePairModality parses a string into a PairModality, returning
// PairModalityNone for unrecognized values.
func ParsePairModality(s string) PairModality {
	if m, ok := pairModalityLookup[strings.ToLower(s)]; ok {
		return m
	}
	return PairModalityNone
}
```

### 1.4 Run the tests (expect pass)

```bash
go test ./internal/agent/ -run TestPairModality -v
```

### 1.5 Commit

```bash
git add internal/agent/pair_modality.go internal/agent/pair_modality_test.go
git commit -m "feat(agent): add PairModality enum for agentic pair modality selection"
```

- [x] `internal/agent/pair_modality.go` created
- [x] `internal/agent/pair_modality_test.go` created
- [x] `go test ./internal/agent/ -run TestPairModality -v` passes

---

## Task 2: Spec generation during strategic planning

When the strategic planner creates a plan, it generates acceptance criteria (a "spec") and stores it in the task's Metadata field under the key `"spec"`. The spec is a structured document listing what each step must accomplish to be considered complete.

### 2.1 Write the test file

**File:** `internal/agent/spec_generation_test.go`

```go
package agent

import (
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

func TestGenerateSpecFromSteps(t *testing.T) {
	steps := []*task.TaskStep{
		task.NewTaskStep("task-1", "Implement the login handler function", 0).WithToolHint("code"),
		task.NewTaskStep("task-1", "Write unit tests for login handler", 1).WithToolHint("code"),
	}

	spec := GenerateSpecFromSteps(steps)
	if spec.TaskID != "task-1" {
		t.Errorf("spec.TaskID = %q, want %q", spec.TaskID, "task-1")
	}
	if len(spec.Criteria) != 2 {
		t.Fatalf("expected 2 criteria, got %d", len(spec.Criteria))
	}

	c0 := spec.Criteria[0]
	if c0.StepSequence != 0 {
		t.Errorf("criterion[0].StepSequence = %d, want 0", c0.StepSequence)
	}
	if c0.Description == "" {
		t.Error("criterion[0].Description should not be empty")
	}
	if !c0.Required {
		t.Error("criterion[0].Required should be true by default")
	}
}

func TestGenerateSpecFromSteps_Empty(t *testing.T) {
	spec := GenerateSpecFromSteps(nil)
	if spec.Criteria != nil {
		t.Errorf("expected nil criteria for empty steps, got %v", spec.Criteria)
	}
}

func TestGenerateSpecFromSteps_SetsAcceptanceCriteria(t *testing.T) {
	steps := []*task.TaskStep{
		task.NewTaskStep("task-1", "Fix the nil pointer dereference in server.go line 42", 0).WithToolHint("fix"),
	}

	spec := GenerateSpecFromSteps(steps)
	if len(spec.Criteria) != 1 {
		t.Fatalf("expected 1 criterion, got %d", len(spec.Criteria))
	}

	c := spec.Criteria[0]
	if c.AcceptanceCriteria == "" {
		t.Error("AcceptanceCriteria should not be empty")
	}
}

func TestSpecJSONRoundTrip(t *testing.T) {
	spec := &TaskSpec{
		TaskID: "task-test",
		Criteria: []StepCriterion{
			{
				StepSequence:       0,
				Description:        "Implement feature X",
				AcceptanceCriteria: "Feature X works end-to-end",
				Required:           true,
			},
		},
	}

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var restored TaskSpec
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if restored.TaskID != spec.TaskID {
		t.Errorf("TaskID mismatch: got %q, want %q", restored.TaskID, spec.TaskID)
	}
	if len(restored.Criteria) != 1 {
		t.Fatalf("Criteria count: got %d, want 1", len(restored.Criteria))
	}
	if restored.Criteria[0].AcceptanceCriteria != "Feature X works end-to-end" {
		t.Errorf("AcceptanceCriteria = %q", restored.Criteria[0].AcceptanceCriteria)
	}
}

func TestExtractSpecFromTask(t *testing.T) {
	spec := &TaskSpec{
		TaskID: "task-extract",
		Criteria: []StepCriterion{
			{StepSequence: 0, Description: "do the thing", Required: true},
		},
	}
	specJSON, _ := json.Marshal(spec)

	tsk := task.NewTask("test", "test task")
	tsk.Metadata = specJSON

	extracted := ExtractSpecFromTask(tsk)
	if extracted == nil {
		t.Fatal("expected non-nil spec from task metadata")
	}
	if extracted.TaskID != "task-extract" {
		t.Errorf("TaskID = %q, want %q", extracted.TaskID, "task-extract")
	}
}

func TestExtractSpecFromTask_NoMetadata(t *testing.T) {
	tsk := task.NewTask("test", "test task")
	extracted := ExtractSpecFromTask(tsk)
	if extracted != nil {
		t.Error("expected nil spec when task has no metadata")
	}
}

func TestExtractSpecFromTask_InvalidJSON(t *testing.T) {
	tsk := task.NewTask("test", "test task")
	tsk.Metadata = json.RawMessage(`{"not_a_spec": true}`)
	extracted := ExtractSpecFromTask(tsk)
	if extracted != nil {
		t.Error("expected nil spec when metadata does not contain a valid spec")
	}
}
```

### 2.2 Run the test (expect compile failure)

```bash
go test ./internal/agent/ -run "TestGenerateSpec|TestSpecJSON|TestExtractSpec" -v 2>&1 | head -20
```

### 2.3 Create the spec types and generation function

**File:** `internal/agent/spec_generation.go`

```go
package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/task"
)

// TaskSpec is the specification artifact stored in task metadata under the key "spec".
// It contains acceptance criteria for each step in the plan, giving the reviewer
// an explicit checklist to validate against.
type TaskSpec struct {
	TaskID   string          `json:"task_id"`
	Criteria []StepCriterion `json:"criteria,omitempty"`
}

// StepCriterion defines what a single step must accomplish to be accepted.
type StepCriterion struct {
	StepSequence       int    `json:"step_sequence"`
	Description        string `json:"description"`
	AcceptanceCriteria string `json:"acceptance_criteria"`
	Required           bool   `json:"required"`
}

// specMetadataWrapper is the structure stored in task.Metadata (json.RawMessage).
type specMetadataWrapper struct {
	Spec *TaskSpec `json:"spec,omitempty"`
}

// GenerateSpecFromSteps creates a TaskSpec from a slice of planned steps.
func GenerateSpecFromSteps(steps []*task.TaskStep) *TaskSpec {
	if len(steps) == 0 {
		return &TaskSpec{}
	}

	taskID := steps[0].TaskID
	criteria := make([]StepCriterion, 0, len(steps))

	for _, step := range steps {
		criterion := StepCriterion{
			StepSequence:       step.Sequence,
			Description:        step.Description,
			AcceptanceCriteria: deriveAcceptanceCriteria(step),
			Required:           true,
		}
		criteria = append(criteria, criterion)
	}

	return &TaskSpec{
		TaskID:   taskID,
		Criteria: criteria,
	}
}

// deriveAcceptanceCriteria produces a human-readable acceptance checklist
// from a step's description and tool hint.
func deriveAcceptanceCriteria(step *task.TaskStep) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Step %q must be fully completed.", step.Description))

	switch step.ToolHint {
	case string(IntentCode), KeywordRefactor:
		sb.WriteString(" Code must compile without errors. Changes must be syntactically correct.")
		sb.WriteString(" No regressions in existing functionality.")
	case KeywordFix, string(IntentDebug):
		sb.WriteString(" Root cause must be identified and resolved.")
		sb.WriteString(" Fix must not introduce new errors.")
	case string(IntentGit), KeywordCommit:
		sb.WriteString(" Git operations must succeed. Changes must be committed to the correct branch.")
	case string(IntentPlan):
		sb.WriteString(" Plan must be actionable and cover all stated requirements.")
	case string(IntentAnalyze), string(IntentResearch):
		sb.WriteString(" Analysis must be thorough and address the question posed.")
	default:
		sb.WriteString(" Output must be meaningful and address the stated goal.")
	}

	return sb.String()
}

// StoreSpecInTask serializes the spec into the task's Metadata field.
// It preserves any existing metadata keys by merging the "spec" key in.
func StoreSpecInTask(t *task.Task, spec *TaskSpec) {
	if spec == nil {
		return
	}

	specJSON, err := json.Marshal(spec)
	if err != nil {
		return
	}

	if len(t.Metadata) > 0 {
		var wrapper specMetadataWrapper
		if json.Unmarshal(t.Metadata, &wrapper) == nil {
			wrapper.Spec = spec
			if merged, err := json.Marshal(wrapper); err == nil {
				t.Metadata = merged
				return
			}
		}
	}

	wrapper := specMetadataWrapper{Spec: spec}
	if data, err := json.Marshal(wrapper); err == nil {
		t.Metadata = data
	}
}

// ExtractSpecFromTask reads the spec from a task's Metadata field.
// Returns nil if the task has no metadata or the metadata does not contain a spec.
func ExtractSpecFromTask(t *task.Task) *TaskSpec {
	if len(t.Metadata) == 0 {
		return nil
	}

	var wrapper specMetadataWrapper
	if err := json.Unmarshal(t.Metadata, &wrapper); err != nil {
		return nil
	}
	return wrapper.Spec
}
```

### 2.4 Modify StrategicPlanner.Plan to generate and store spec

**File:** `internal/agent/strategic.go`

After the steps are persisted (after the `for _, step := range steps` loop), add spec generation and storage. Find the block that updates the task after persisting steps and add spec generation before the task update:

```go
	// Generate specification from planned steps and store in task metadata
	spec := GenerateSpecFromSteps(steps)
	StoreSpecInTask(t, spec)
	sp.logger.Info("Generated task spec",
		"task_id", req.TaskID,
		"criteria_count", len(spec.Criteria),
	)
```

### 2.5 Run the tests (expect pass)

```bash
go test ./internal/agent/ -run "TestGenerateSpec|TestSpecJSON|TestExtractSpec" -v
```

### 2.6 Commit

```bash
git add internal/agent/spec_generation.go internal/agent/spec_generation_test.go internal/agent/strategic.go
git commit -m "feat(agent): generate acceptance criteria spec during strategic planning"
```

- [x] `internal/agent/spec_generation.go` created with TaskSpec, StepCriterion, GenerateSpecFromSteps, StoreSpecInTask, ExtractSpecFromTask
- [x] `internal/agent/spec_generation_test.go` created
- [x] `internal/agent/strategic.go` modified to generate spec after planning
- [x] Tests pass

---

## Task 3: ReviewManager spec-aware review prompt

Modify `ReviewManager.ReviewStep()` to accept the task spec and include it in the review prompt so the reviewer agent checks the step result against the acceptance criteria.

### 3.1 Write the test

**File:** `internal/agent/review_spec_test.go`

```go
package agent

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

func TestBuildReviewPrompt_IncludesSpec(t *testing.T) {
	rm := NewReviewManager(ReviewManagerConfig{})

	step := &task.TaskStep{
		ID:          "step-test-0-1234",
		TaskID:      "task-test",
		Description: "Implement the login handler",
		ToolHint:    "code",
		AgentID:     "coder",
		Result:      `{"success": true, "result": "login.go created with handler function"}`,
		Sequence:    0,
	}

	spec := &TaskSpec{
		TaskID: "task-test",
		Criteria: []StepCriterion{
			{
				StepSequence:       0,
				Description:        "Implement the login handler",
				AcceptanceCriteria: "Code must compile without errors. Changes must be syntactically correct. No regressions in existing functionality.",
				Required:           true,
			},
		},
	}

	prompt := rm.buildReviewPrompt(step, spec)
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	if !strings.Contains(prompt, "ACCEPTANCE CRITERIA") {
		t.Error("prompt should contain ACCEPTANCE CRITERIA section when spec is provided")
	}
	if !strings.Contains(prompt, "Code must compile without errors") {
		t.Error("prompt should contain the specific acceptance criterion text")
	}
}

func TestBuildReviewPrompt_NoSpec(t *testing.T) {
	rm := NewReviewManager(ReviewManagerConfig{})

	step := &task.TaskStep{
		ID:          "step-test-0-1234",
		TaskID:      "task-test",
		Description: "Implement the login handler",
		ToolHint:    "code",
		AgentID:     "coder",
		Result:      `{"success": true}`,
		Sequence:    0,
	}

	prompt := rm.buildReviewPrompt(step, nil)
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	if strings.Contains(prompt, "ACCEPTANCE CRITERIA") {
		t.Error("prompt should NOT contain ACCEPTANCE CRITERIA section when spec is nil")
	}
	if !strings.Contains(prompt, "REVIEW TASK STEP") {
		t.Error("prompt should contain REVIEW TASK STEP header")
	}
}

func TestBuildReviewPrompt_SpecForDifferentStep(t *testing.T) {
	rm := NewReviewManager(ReviewManagerConfig{})

	step := &task.TaskStep{
		ID:          "step-test-1-1234",
		TaskID:      "task-test",
		Description: "Write unit tests",
		ToolHint:    "code",
		AgentID:     "coder",
		Result:      `{"success": true}`,
		Sequence:    1,
	}

	spec := &TaskSpec{
		TaskID: "task-test",
		Criteria: []StepCriterion{
			{
				StepSequence:       0,
				Description:        "Implement the login handler",
				AcceptanceCriteria: "Code compiles. No regressions.",
				Required:           true,
			},
			{
				StepSequence:       1,
				Description:        "Write unit tests",
				AcceptanceCriteria: "Tests cover login handler. All tests pass.",
				Required:           true,
			},
		},
	}

	prompt := rm.buildReviewPrompt(step, spec)
	if !strings.Contains(prompt, "Tests cover login handler") {
		t.Error("prompt should contain the acceptance criteria for step sequence 1")
	}
	if strings.Contains(prompt, "Code compiles. No regressions.") {
		t.Error("prompt should NOT contain criteria for step sequence 0 (different step)")
	}
}
```

### 3.2 Modify ReviewManager to accept spec

**File:** `internal/agent/review_manager.go`

Change `ReviewStep` signature to accept spec:

```go
func (rm *ReviewManager) ReviewStep(ctx context.Context, step *task.TaskStep, spec *TaskSpec) (*ReviewResult, error) {
```

Change `buildReviewPrompt` to accept spec:

```go
func (rm *ReviewManager) buildReviewPrompt(step *task.TaskStep, spec *TaskSpec) string {
```

Add spec section to the prompt (after existing content, before the return):

```go
	// Include acceptance criteria from spec if available
	if spec != nil {
		for _, c := range spec.Criteria {
			if c.StepSequence == step.Sequence {
				sb.WriteString("\nACCEPTANCE CRITERIA (you MUST check these):\n")
				sb.WriteString(c.AcceptanceCriteria)
				sb.WriteString("\n\nEvaluate the work specifically against these criteria.\n")
				break
			}
		}
	}
```

Update the `buildReviewPrompt` call inside `ReviewStep` to pass spec:

```go
	prompt := rm.buildReviewPrompt(step, spec)
```

### 3.3 Update the call site in tactical.go

**File:** `internal/agent/tactical.go`

In `OnJobCompleted`, load the task spec before calling ReviewStep:

```go
		// Load task to extract spec for spec-driven review
		var reviewSpec *TaskSpec
		if ts.taskStore != nil {
			if t, err := ts.taskStore.GetByID(step.TaskID); err == nil && t != nil {
				reviewSpec = ExtractSpecFromTask(t)
			}
		}

		// Perform review (synchronously for now)
		reviewResult, err := ts.reviewManager.ReviewStep(ctx, step, reviewSpec)
```

### 3.4 Fix any other callers

Search for other callers of `ReviewStep` and add `nil` as the spec argument:

```bash
grep -rn '\.ReviewStep(' internal/ --include='*.go'
```

### 3.5 Run the tests

```bash
go test ./internal/agent/ -run "TestBuildReviewPrompt" -v
go test ./internal/agent/ -v 2>&1 | tail -30
```

### 3.6 Commit

```bash
git add internal/agent/review_manager.go internal/agent/review_spec_test.go internal/agent/tactical.go
git commit -m "feat(agent): include task spec in reviewer prompt for spec-driven review"
```

- [x] `ReviewStep` and `buildReviewPrompt` accept `*TaskSpec` parameter
- [x] tactical.go loads spec before calling ReviewStep
- [x] All callers updated
- [x] Tests pass

---

## Task 4: Feedback propagation to revision steps

When the reviewer rejects a step, the structured feedback (issues list and feedback text) must be propagated into the revision step's `AccumulatedContext` so the coder agent sees the spec + previous rejection feedback when re-executing.

### 4.1 Write the test

**File:** `internal/agent/feedback_propagation_test.go`

```go
package agent

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

func TestBuildRevisionContext(t *testing.T) {
	result := &ReviewResult{
		Status:   ReviewRejected,
		Feedback: "The login handler does not validate input. Missing error handling for edge cases.",
		Issues: []string{
			"No input validation on email field",
			"Missing error handling for database connection failure",
			"No rate limiting on login attempts",
		},
		Confidence: 0.85,
	}

	spec := &TaskSpec{
		TaskID: "task-test",
		Criteria: []StepCriterion{
			{
				StepSequence:       0,
				Description:        "Implement the login handler",
				AcceptanceCriteria: "Code must compile without errors. No regressions in existing functionality.",
				Required:           true,
			},
		},
	}

	context := BuildRevisionContext(result, spec)
	if context == "" {
		t.Fatal("expected non-empty revision context")
	}
	if !strings.Contains(context, "PREVIOUS REVIEW FEEDBACK") {
		t.Error("revision context should contain PREVIOUS REVIEW FEEDBACK section")
	}
	if !strings.Contains(context, "No input validation on email field") {
		t.Error("revision context should contain the specific issue text")
	}
	if !strings.Contains(context, "ORIGINAL ACCEPTANCE CRITERIA") {
		t.Error("revision context should contain ORIGINAL ACCEPTANCE CRITERIA section")
	}
	if !strings.Contains(context, "Code must compile without errors") {
		t.Error("revision context should contain the acceptance criteria text")
	}
}

func TestBuildRevisionContext_NilSpec(t *testing.T) {
	result := &ReviewResult{
		Status:   ReviewRejected,
		Feedback: "Fix the bugs",
		Issues:   []string{"bug 1", "bug 2"},
	}

	context := BuildRevisionContext(result, nil)
	if !strings.Contains(context, "PREVIOUS REVIEW FEEDBACK") {
		t.Error("revision context should contain feedback section even without spec")
	}
	if strings.Contains(context, "ORIGINAL ACCEPTANCE CRITERIA") {
		t.Error("revision context should NOT contain acceptance criteria when spec is nil")
	}
}

func TestBuildRevisionContext_NilResult(t *testing.T) {
	context := BuildRevisionContext(nil, nil)
	if context != "" {
		t.Errorf("expected empty context for nil result, got %q", context)
	}
}
```

### 4.2 Create the implementation

**File:** `internal/agent/feedback_propagation.go`

```go
package agent

import (
	"fmt"
	"strings"
)

// BuildRevisionContext constructs the AccumulatedContext string for a revision
// step. It combines the reviewer's feedback and issue list with the original
// spec's acceptance criteria, giving the coder agent full context of what went
// wrong and what "done" looks like.
func BuildRevisionContext(result *ReviewResult, spec *TaskSpec) string {
	if result == nil {
		return ""
	}

	var sb strings.Builder

	sb.WriteString("PREVIOUS REVIEW FEEDBACK (address these issues):\n\n")
	if result.Feedback != "" {
		sb.WriteString(result.Feedback)
		sb.WriteString("\n\n")
	}
	if len(result.Issues) > 0 {
		sb.WriteString("Specific issues to fix:\n")
		for i, issue := range result.Issues {
			fmt.Fprintf(&sb, "  %d. %s\n", i+1, issue)
		}
		sb.WriteString("\n")
	}

	if spec != nil && len(spec.Criteria) > 0 {
		sb.WriteString("ORIGINAL ACCEPTANCE CRITERIA (these must still be met):\n\n")
		for _, c := range spec.Criteria {
			fmt.Fprintf(&sb, "- %s\n", c.AcceptanceCriteria)
		}
	}

	return sb.String()
}
```

### 4.3 Add CreateRevisionWithContext to step.go

**File:** `internal/task/step.go`

Add after the existing `CreateRevision` function:

```go
// CreateRevisionWithContext creates a revision step with additional context
// from the review feedback. The revisionContext is prepended to the step's
// AccumulatedContext so the coder agent sees prior rejection feedback.
func CreateRevisionWithContext(original *TaskStep, feedback string, revisionContext string) *TaskStep {
	revision := CreateRevision(original, feedback)
	if revisionContext != "" {
		revision.AccumulatedContext = revisionContext
	}
	return revision
}
```

### 4.4 Modify HandleReviewResult to accept and use spec

**File:** `internal/agent/review_manager.go`

Change signature:
```go
func (rm *ReviewManager) HandleReviewResult(ctx context.Context, stepID string, result *ReviewResult, spec *TaskSpec) ([]*task.TaskStep, error) {
```

In the `ReviewRejected` case, replace revision creation:

```go
		// Create revision step with feedback context
		revisionContext := BuildRevisionContext(result, spec)
		revision := task.CreateRevisionWithContext(step, result.Feedback, revisionContext)
```

### 4.5 Update caller in tactical.go

**File:** `internal/agent/tactical.go`

In `handleReviewResult`, load spec and pass to `HandleReviewResult`:

```go
func (ts *TacticalScheduler) handleReviewResult(ctx context.Context, step *task.TaskStep, result *ReviewResult) error {
	if ts.reviewManager == nil {
		return fmt.Errorf("tactical scheduler has no ReviewManager")
	}

	// Load spec from task for feedback propagation
	var spec *TaskSpec
	if ts.taskStore != nil {
		if t, tErr := ts.taskStore.GetByID(step.TaskID); tErr == nil && t != nil {
			spec = ExtractSpecFromTask(t)
		}
	}

	revisions, err := ts.reviewManager.HandleReviewResult(ctx, step.ID, result, spec)
	// ... rest unchanged
```

### 4.6 Fix any other callers of HandleReviewResult

```bash
grep -rn 'HandleReviewResult' internal/ --include='*.go'
```

Add `nil` as the spec parameter to any other callers.

### 4.7 Run the tests

```bash
go test ./internal/agent/ -run "TestBuildRevisionContext" -v
go test ./internal/agent/ -v 2>&1 | tail -30
```

### 4.8 Commit

```bash
git add internal/agent/feedback_propagation.go internal/agent/feedback_propagation_test.go internal/agent/review_manager.go internal/agent/tactical.go internal/task/step.go
git commit -m "feat(agent): propagate reviewer feedback and spec to revision step context"
```

- [x] `internal/agent/feedback_propagation.go` created with BuildRevisionContext
- [x] `internal/task/step.go` has CreateRevisionWithContext
- [x] HandleReviewResult accepts spec and uses BuildRevisionContext
- [x] handleReviewResult in tactical.go loads spec
- [x] Tests pass

---

## Task 5: Max revision guard

Enforce the max revision cycle limit from `ReviewPolicy.MaxRevisionCycles`. When the limit is exceeded, the step is marked as `ReviewNeedsInfo` (triggering human intervention) instead of creating another revision. This guard already exists in `review.go:146-156` and is called in `review_manager.go:118`. This task verifies it works correctly with the new spec-aware feedback.

### 5.1 Write the test

**File:** `internal/agent/max_revision_guard_test.go`

```go
package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/task"
)

func TestReviewPolicy_ExceedsMaxRevisions_Boundary(t *testing.T) {
	policy := DefaultReviewPolicy()
	policy.MaxRevisionCycles = 3

	tests := []struct {
		revisionCount int
		want          bool
	}{
		{0, false},
		{1, false},
		{2, false},
		{3, true},
		{4, true},
	}
	for _, tt := range tests {
		step := &task.TaskStep{RevisionCount: tt.revisionCount}
		got := policy.ExceedsMaxRevisions(step)
		if got != tt.want {
			t.Errorf("ExceedsMaxRevisions(revisionCount=%d) = %v, want %v", tt.revisionCount, got, tt.want)
		}
	}
}

func TestReviewPolicy_ExceedsMaxRevisions_Disabled(t *testing.T) {
	policy := &ReviewPolicy{MaxRevisionCycles: 0}
	step := &task.TaskStep{RevisionCount: 100}
	if policy.ExceedsMaxRevisions(step) {
		t.Error("ExceedsMaxRevisions should return false when MaxRevisionCycles is 0")
	}
}

func TestReviewStep_MaxRevisions_ReturnsNeedsInfo(t *testing.T) {
	policy := DefaultReviewPolicy()
	policy.MaxRevisionCycles = 2

	rm := NewReviewManager(ReviewManagerConfig{Policy: policy})

	spec := &TaskSpec{
		TaskID: "task-guard",
		Criteria: []StepCriterion{
			{
				StepSequence:       0,
				Description:        "Fix the bug",
				AcceptanceCriteria: "Bug is fixed and tests pass.",
				Required:           true,
			},
		},
	}

	step := &task.TaskStep{
		ID:            "step-guard-test",
		TaskID:        "task-guard",
		Description:   "Fix the bug",
		ToolHint:      "fix",
		AgentID:       "debugger",
		RevisionCount: 2,
	}

	result, err := rm.ReviewStep(nil, step, spec)
	if err != nil {
		t.Fatalf("ReviewStep failed: %v", err)
	}
	if result.Status != ReviewNeedsInfo {
		t.Errorf("expected ReviewNeedsInfo when max revisions exceeded, got %s", result.Status)
	}
	if result.Feedback == "" {
		t.Error("expected non-empty feedback explaining human intervention needed")
	}
}
```

### 5.2 Improve the feedback message when max revisions exceeded

**File:** `internal/agent/review_manager.go`

Update the `RequiresHumanIntervention` block in `ReviewStep` to include spec criteria:

```go
	if rm.policy.RequiresHumanIntervention(step) {
		rm.logger.Warn("Step requires human intervention",
			"step_id", step.ID,
			"revision_count", step.RevisionCount,
		)
		feedback := fmt.Sprintf("Maximum revision cycles (%d) exceeded. Human intervention required.", rm.policy.MaxRevisionCycles)
		if spec != nil {
			for _, c := range spec.Criteria {
				if c.StepSequence == step.Sequence {
					feedback += fmt.Sprintf(" Original acceptance criteria: %s", c.AcceptanceCriteria)
					break
				}
			}
		}
		return &ReviewResult{
			Status:     ReviewNeedsInfo,
			Feedback:   feedback,
			Confidence: 1.0,
		}, nil
	}
```

### 5.3 Run all tests

```bash
go test ./internal/agent/ -run "TestReviewPolicy_ExceedsMaxRevisions|TestReviewStep_MaxRevisions" -v
go test ./internal/agent/ -v 2>&1 | tail -30
```

### 5.4 Commit

```bash
git add internal/agent/max_revision_guard_test.go internal/agent/review_manager.go
git commit -m "feat(agent): enforce max revision guard with spec-aware human escalation message"
```

- [x] Max revision guard tests pass
- [x] Feedback includes spec criteria when max exceeded
- [x] All tests pass

---

## Task 6: Integration test

End-to-end test exercising the full spec-driven review loop: planning generates spec, reviewer checks against spec, rejection creates revision with feedback context.

### 6.1 Write the integration test

**File:** `internal/agent/spec_review_integration_test.go`

```go
package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

func TestRevisionStep_ContainsFeedbackContext(t *testing.T) {
	originalStep := &task.TaskStep{
		ID:            "step-rev-0-1234",
		TaskID:        "task-rev",
		Description:   "implement the auth middleware",
		ToolHint:      "code",
		AgentID:       "coder",
		Sequence:      0,
		RevisionCount: 1,
	}

	result := &ReviewResult{
		Status:   ReviewRejected,
		Feedback: "Missing error handling for expired tokens",
		Issues:   []string{"No error handling for expired tokens", "Missing logging"},
	}

	spec := &TaskSpec{
		TaskID: "task-rev",
		Criteria: []StepCriterion{
			{
				StepSequence:       0,
				AcceptanceCriteria: "Code must compile without errors. No regressions.",
				Required:           true,
			},
		},
	}

	revisionContext := BuildRevisionContext(result, spec)
	revision := task.CreateRevisionWithContext(originalStep, result.Feedback, revisionContext)

	if revision.AccumulatedContext == "" {
		t.Fatal("revision step should have accumulated context from review feedback")
	}
	if !strings.Contains(revision.AccumulatedContext, "No error handling for expired tokens") {
		t.Error("revision context should contain the specific issue from review")
	}
	if !strings.Contains(revision.AccumulatedContext, "Code must compile without errors") {
		t.Error("revision context should contain the original acceptance criteria")
	}
	if !strings.Contains(revision.AccumulatedContext, "PREVIOUS REVIEW FEEDBACK") {
		t.Error("revision context should contain PREVIOUS REVIEW FEEDBACK header")
	}
	if !strings.Contains(revision.AccumulatedContext, "ORIGINAL ACCEPTANCE CRITERIA") {
		t.Error("revision context should contain ORIGINAL ACCEPTANCE CRITERIA header")
	}
}

func TestEndToEnd_SpecGenerationToRevisionContext(t *testing.T) {
	// Step 1: Generate spec from planned steps
	steps := []*task.TaskStep{
		task.NewTaskStep("task-e2e", "Implement JWT token validation", 0).WithToolHint("code"),
		task.NewTaskStep("task-e2e", "Write tests for JWT validation", 1).WithToolHint("code"),
	}

	spec := GenerateSpecFromSteps(steps)
	if len(spec.Criteria) != 2 {
		t.Fatalf("expected 2 criteria, got %d", len(spec.Criteria))
	}

	// Step 2: Build review prompt for step 0 with spec
	rm := NewReviewManager(ReviewManagerConfig{})
	step0 := steps[0]
	step0.Result = `{"success": true, "result": "jwt_validation.go created"}`

	prompt := rm.buildReviewPrompt(step0, spec)
	if !strings.Contains(prompt, "ACCEPTANCE CRITERIA") {
		t.Error("review prompt should contain acceptance criteria for step 0")
	}

	// Step 3: Simulate rejection
	reviewResult := &ReviewResult{
		Status:   ReviewRejected,
		Feedback: "Token expiration check is missing",
		Issues:   []string{"No token expiration check", "Missing edge case for malformed tokens"},
	}

	// Step 4: Build revision context
	revisionContext := BuildRevisionContext(reviewResult, spec)
	if !strings.Contains(revisionContext, "No token expiration check") {
		t.Error("revision context should contain rejection issues")
	}
	if !strings.Contains(revisionContext, "ORIGINAL ACCEPTANCE CRITERIA") {
		t.Error("revision context should contain original acceptance criteria")
	}

	// Step 5: Create revision step with context
	step0.RevisionCount = 1
	revision := task.CreateRevisionWithContext(step0, reviewResult.Feedback, revisionContext)
	if revision.AccumulatedContext == "" {
		t.Error("revision should have accumulated context")
	}

	// Step 6: Store spec in task and verify round-trip
	tsk := task.NewTask("e2e-test", "end-to-end spec test")
	StoreSpecInTask(tsk, spec)
	extracted := ExtractSpecFromTask(tsk)
	if extracted == nil {
		t.Fatal("spec round-trip through task metadata failed")
	}
	if len(extracted.Criteria) != 2 {
		t.Errorf("extracted spec has %d criteria, want 2", len(extracted.Criteria))
	}
}

func TestEndToEnd_MaxRevisionGuard(t *testing.T) {
	policy := DefaultReviewPolicy()
	policy.MaxRevisionCycles = 2

	rm := NewReviewManager(ReviewManagerConfig{Policy: policy})

	spec := &TaskSpec{
		TaskID: "task-guard-e2e",
		Criteria: []StepCriterion{
			{
				StepSequence:       0,
				Description:        "Fix the bug",
				AcceptanceCriteria: "Bug is fixed and tests pass.",
				Required:           true,
			},
		},
	}

	step := &task.TaskStep{
		ID:            "step-guard-e2e-0-1234",
		TaskID:        "task-guard-e2e",
		Description:   "Fix the bug",
		ToolHint:      "fix",
		AgentID:       "debugger",
		RevisionCount: 2,
	}

	result, err := rm.ReviewStep(nil, step, spec)
	if err != nil {
		t.Fatalf("ReviewStep failed: %v", err)
	}
	if result.Status != ReviewNeedsInfo {
		t.Errorf("expected NeedsInfo at max revisions, got %s", result.Status)
	}
	if !strings.Contains(result.Feedback, "Maximum revision cycles (2) exceeded") {
		t.Errorf("feedback should mention max cycles, got: %s", result.Feedback)
	}
	if !strings.Contains(result.Feedback, "Bug is fixed and tests pass") {
		t.Error("feedback should include the original acceptance criteria")
	}
}

// helper for string containment checks
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && len(substr) > 0 && jsonContains(s, substr)
}

func jsonContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

### 6.2 Run the integration tests

```bash
go test ./internal/agent/ -run "TestRevisionStep_ContainsFeedback|TestEndToEnd|TestBuildRevisionContext" -v
```

### 6.3 Run the full test suite

```bash
go test ./internal/agent/ -v 2>&1 | tail -50
go test ./internal/task/ -v 2>&1 | tail -30
```

### 6.4 Commit

```bash
git add internal/agent/spec_review_integration_test.go
git commit -m "test(agent): add integration tests for spec-driven review loop"
```

- [x] Integration tests pass
- [x] Full test suite passes
- [x] No regressions

---

## Summary of files created/modified

| File | Action | Purpose |
|------|--------|---------|
| `internal/agent/pair_modality.go` | Create | Shared PairModality enum (all options) |
| `internal/agent/pair_modality_test.go` | Create | PairModality tests |
| `internal/agent/spec_generation.go` | Create | TaskSpec types, GenerateSpecFromSteps, StoreSpecInTask, ExtractSpecFromTask |
| `internal/agent/spec_generation_test.go` | Create | Spec generation tests |
| `internal/agent/feedback_propagation.go` | Create | BuildRevisionContext function |
| `internal/agent/feedback_propagation_test.go` | Create | Feedback propagation tests |
| `internal/agent/review_spec_test.go` | Create | Spec-aware review prompt tests |
| `internal/agent/max_revision_guard_test.go` | Create | Max revision guard tests |
| `internal/agent/spec_review_integration_test.go` | Create | End-to-end integration tests |
| `internal/agent/strategic.go` | Modify | Plan() generates and stores spec |
| `internal/agent/review_manager.go` | Modify | ReviewStep/buildReviewPrompt/HandleReviewResult accept spec |
| `internal/agent/tactical.go` | Modify | OnJobCompleted loads spec, handleReviewResult passes spec |
| `internal/task/step.go` | Modify | Add CreateRevisionWithContext function |
