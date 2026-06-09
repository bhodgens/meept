# Plan-Session Association Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a first-class plan system where plan.md files are project-scoped entities with their own lifecycle, linked to sessions, visually tracked in the TUI, and synthesized into the existing task system on approval.

**Architecture:** New `internal/plan/` package with Plan models, SQLite store, plan.md parser/writer, and a PlanManager orchestrating lifecycle and synthesis. The plan system sits above the existing task system as a coordination layer — it does not re-implement execution. Plans generate tasks via the StrategicPlanner on approval. Progress flows back from task events via the bus.

**Tech Stack:** Go 1.22+, SQLite (modernc.org/sqlite), bubbletea v2 / lipgloss v2 (TUI), cobra (CLI), message bus pub/sub

**Spec:** `docs/superpowers/specs/2026-05-28-plan-session-association-design.md`

---

## File Structure

### New Files

| File | Responsibility |
|------|----------------|
| `internal/plan/plan.go` | Plan, PlanPhase, PlanSignoff models, PlanState/PhaseState enums, constructors |
| `internal/plan/store.go` | PlanStore interface |
| `internal/plan/store_sqlite.go` | SQLite implementation of PlanStore |
| `internal/plan/parser.go` | plan.md parser — extract meta, phases, steps, dependencies |
| `internal/plan/writer.go` | plan.md writer — update status annotations, sync meta |
| `internal/plan/manager.go` | PlanManager — lifecycle (create, approve, reject, revise, confirm), synthesis, progress tracking |
| `internal/plan/handler.go` | Bus event handler — subscribes to task.step.completed, task.completed, routes to PlanManager |
| `internal/plan/manager_test.go` | Tests for PlanManager, parser, writer |
| `internal/plan/store_test.go` | Tests for PlanStore |
| `internal/services/plan_service.go` | Service layer for RPC/HTTP handlers |
| `internal/rpc/plan.go` | RPC handler methods |
| `cmd/meept/plans.go` | CLI subcommands: list, show, approve, reject, confirm |
| `internal/tui/models/plans.go` | PlansModel for Plans tab |
| `docs/concepts/plans.md` | Concepts documentation |
| `docs/workflows/plans.md` | Workflow documentation |

### Modified Files

| File | Changes |
|------|---------|
| `internal/config/schema.go` | Add PlansConfig + nested structs |
| `config/meept.json5` | Add `plans` section to template |
| `internal/services/service.go` | Add PlanService to ServiceRegistry + Config |
| `internal/daemon/daemon.go` | Wire PlanStore, PlanManager, PlanHandler into daemon |
| `internal/agent/dispatcher.go` | Add plan routing logic after ClassifyAndRoute |
| `internal/agent/orchestrator.go` | Subscribe to plan events, forward task events to PlanManager |
| `internal/tui/app.go` | Add ViewPlans, Plans tab, header badges, session picker plan indicators |
| `internal/tui/modal.go` | Add plan indicators to SessionPickerModal |
| `internal/tui/styles.go` | Add plan state styles |
| `internal/tui/types/types.go` | Add Plan/PlanPhase TUI types |
| `internal/tui/events.go` | Subscribe to plan.* bus events |
| `internal/comm/http/server.go` | Register plan REST endpoints |
| `internal/comm/http/api_handlers.go` | Add plan HTTP handlers |
| `cmd/meept/root.go` | Register plans command |
| `CLAUDE.md` | Update architecture table, CLI commands |
| `docs/concepts/architecture.md` | Add Plan layer |
| `docs/reference/cli.md` | Add `meept plans` commands |
| `docs/reference/http-api.md` | Add plan endpoints |
| `mkdocs.yml` | Add plans to nav |

---

### Task 1: Plan Models and State Enums

**Files:**
- Create: `internal/plan/plan.go`

- [x] **Step 1: Create the plan models file**

```go
package plan

import (
	"fmt"
	"sync/atomic"
	"time"
)

// PlanState represents the lifecycle state of a plan.
type PlanState string

const (
	StatePlanning         PlanState = "planning"
	StateDraft            PlanState = "draft"
	StatePendingApproval  PlanState = "pending_approval"
	StateApproved         PlanState = "approved"
	StateExecuting        PlanState = "executing"
	StateCompleted        PlanState = "completed"
	StateConfirmed        PlanState = "confirmed"
	StateCancelled        PlanState = "cancelled"
	StateFailed           PlanState = "failed"
)

func (s PlanState) IsTerminal() bool {
	return s == StateConfirmed || s == StateCancelled || s == StateFailed
}

// PhaseState represents the state of a plan phase.
type PhaseState string

const (
	PhasePending   PhaseState = "pending"
	PhaseInProgress PhaseState = "in_progress"
	PhaseCompleted PhaseState = "completed"
	PhaseConfirmed PhaseState = "confirmed"
	PhaseFailed    PhaseState = "failed"
)

func (s PhaseState) IsTerminal() bool {
	return s == PhaseConfirmed || s == PhaseFailed
}

// Plan represents a project-scoped plan with a plan.md source of truth.
type Plan struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	Description   string     `json:"description,omitempty"`
	FilePath      string     `json:"file_path"`
	ProjectID     string     `json:"project_id,omitempty"`
	State         PlanState  `json:"state"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	ApprovedAt    *time.Time `json:"approved_at,omitempty"`
	ConfirmedAt   *time.Time `json:"confirmed_at,omitempty"`
	ApprovedBy    string     `json:"approved_by,omitempty"`
	ConfirmedBy   string     `json:"confirmed_by,omitempty"`
	TaskID        string     `json:"task_id,omitempty"`
	SourceSession string     `json:"source_session,omitempty"`
	RevisionCount int        `json:"revision_count,omitempty"`
	Phases        []PlanPhase `json:"phases,omitempty"`
}

// PlanPhase represents a named phase within a plan.
type PlanPhase struct {
	ID              string     `json:"id"`
	PlanID          string     `json:"plan_id"`
	Name            string     `json:"name"`
	Sequence        int        `json:"sequence"`
	TotalSteps      int        `json:"total_steps"`
	CompletedSteps  int        `json:"completed_steps"`
	FailedSteps     int        `json:"failed_steps"`
	State           PhaseState `json:"state"`
}

// PlanSignoff records an approval, rejection, or confirmation action.
type PlanSignoff struct {
	ID        string    `json:"id"`
	PlanID    string    `json:"plan_id"`
	PhaseID   string    `json:"phase_id,omitempty"`
	SessionID string    `json:"session_id"`
	By        string    `json:"by"`
	Action    string    `json:"action"` // "approved", "rejected", "confirmed", "revision_requested"
	Comment   string    `json:"comment,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

var planIDCounter uint64

func generatePlanID() string {
	seq := atomic.AddUint64(&planIDCounter, 1)
	return fmt.Sprintf("plan-%s-%04d", time.Now().UTC().Format("20060102150405"), seq)
}

var phaseIDCounter uint64

func generatePhaseID() string {
	seq := atomic.AddUint64(&phaseIDCounter, 1)
	return fmt.Sprintf("phase-%s-%04d", time.Now().UTC().Format("20060102150405"), seq)
}

var signoffIDCounter uint64

func generateSignoffID() string {
	seq := atomic.AddUint64(&signoffIDCounter, 1)
	return fmt.Sprintf("signoff-%s-%04d", time.Now().UTC().Format("20060102150405"), seq)
}

// NewPlan creates a Plan in the planning state.
func NewPlan(title, description, projectID, filePath, sourceSession string) *Plan {
	now := time.Now().UTC()
	return &Plan{
		ID:            generatePlanID(),
		Title:         title,
		Description:   description,
		FilePath:      filePath,
		ProjectID:     projectID,
		State:         StatePlanning,
		CreatedAt:     now,
		UpdatedAt:     now,
		SourceSession: sourceSession,
	}
}

// NewPlanPhase creates a PlanPhase in the pending state.
func NewPlanPhase(planID, name string, sequence, totalSteps int) *PlanPhase {
	return &PlanPhase{
		ID:         generatePhaseID(),
		PlanID:     planID,
		Name:       name,
		Sequence:   sequence,
		TotalSteps: totalSteps,
		State:      PhasePending,
	}
}

// NewPlanSignoff creates a PlanSignoff record.
func NewPlanSignoff(planID, phaseID, sessionID, by, action, comment string) *PlanSignoff {
	return &PlanSignoff{
		ID:        generateSignoffID(),
		PlanID:    planID,
		PhaseID:   phaseID,
		SessionID: sessionID,
		By:        by,
		Action:    action,
		Comment:   comment,
		CreatedAt: time.Now().UTC(),
	}
}

// TotalSteps returns the sum of all phase step counts.
func (p *Plan) TotalSteps() int {
	total := 0
	for _, ph := range p.Phases {
		total += ph.TotalSteps
	}
	return total
}

// CompletedSteps returns the sum of all phase completed step counts.
func (p *Plan) CompletedSteps() int {
	total := 0
	for _, ph := range p.Phases {
		total += ph.CompletedSteps
	}
	return total
}

// FailedSteps returns the sum of all phase failed step counts.
func (p *Plan) FailedSteps() int {
	total := 0
	for _, ph := range p.Phases {
		total += ph.FailedSteps
	}
	return total
}
```

- [x] **Step 2: Verify it compiles**

Run: `go build ./internal/plan/`
Expected: no errors

- [x] **Step 3: Commit**

```bash
git add internal/plan/plan.go
git commit -m "feat(plan): add plan models and state enums"
```

---

### Task 2: PlanStore Interface and SQLite Implementation

**Files:**
- Create: `internal/plan/store.go`
- Create: `internal/plan/store_sqlite.go`

- [x] **Step 1: Create the PlanStore interface**

```go
package plan

import "context"

// PlanStore persists plan metadata, phases, sessions, and signoffs.
type PlanStore interface {
	// Plan CRUD
	CreatePlan(ctx context.Context, p *Plan) error
	GetPlan(ctx context.Context, id string) (*Plan, error)
	UpdatePlan(ctx context.Context, p *Plan) error
	DeletePlan(ctx context.Context, id string) error
	ListPlans(ctx context.Context, projectID string, limit int) ([]*Plan, error)
	ListPlansBySession(ctx context.Context, sessionID string) ([]*Plan, error)
	ListPlansByState(ctx context.Context, state PlanState, limit int) ([]*Plan, error)
	SetPlanState(ctx context.Context, id string, state PlanState) error

	// Phase operations
	CreatePhase(ctx context.Context, p *PlanPhase) error
	GetPhases(ctx context.Context, planID string) ([]*PlanPhase, error)
	UpdatePhase(ctx context.Context, p *PlanPhase) error
	SetPhaseState(ctx context.Context, id string, state PhaseState) error
	IncrementPhaseProgress(ctx context.Context, phaseID string, field string, delta int) error

	// Session linking
	LinkSession(ctx context.Context, planID, sessionID string) error
	UnlinkSession(ctx context.Context, planID, sessionID string) error
	GetPlansForSession(ctx context.Context, sessionID string) ([]*Plan, error)

	// Signoff operations
	CreateSignoff(ctx context.Context, s *PlanSignoff) error
	GetSignoffs(ctx context.Context, planID string) ([]*PlanSignoff, error)
	GetRevisionCount(ctx context.Context, planID string) (int, error)

	// Counts
	CountPlansBySessionAndState(ctx context.Context, sessionID string) (map[PlanState]int, error)
}
```

- [x] **Step 2: Create the SQLite implementation**

Create `internal/plan/store_sqlite.go` following the exact patterns from `internal/task/store.go`:
- Constructor `NewSQLiteStore(dbPath string, logger *slog.Logger) (*SQLiteStore, error)` opens DB with WAL mode
- `migrate()` creates all 4 tables (`plans`, `plan_phases`, `plan_sessions`, `plan_signoffs`) with indexes
- All scan methods follow the `sql.NullString` + `buildPlan` / `buildPhase` / `buildSignoff` pattern
- Times stored as `time.RFC3339` strings
- `nullableString` helper for empty string → nil conversion

Schema SQL (from spec):
```sql
CREATE TABLE IF NOT EXISTS plans (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT,
    file_path TEXT NOT NULL,
    project_id TEXT,
    state TEXT NOT NULL DEFAULT 'planning',
    task_id TEXT,
    source_session TEXT,
    approved_at TEXT,
    confirmed_at TEXT,
    approved_by TEXT,
    confirmed_by TEXT,
    revision_count INTEGER DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_plans_state ON plans(state);
CREATE INDEX IF NOT EXISTS idx_plans_project ON plans(project_id);
CREATE INDEX IF NOT EXISTS idx_plans_updated_at ON plans(updated_at DESC);

CREATE TABLE IF NOT EXISTS plan_phases (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    total_steps INTEGER NOT NULL DEFAULT 0,
    completed_steps INTEGER NOT NULL DEFAULT 0,
    failed_steps INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT 'pending'
);
CREATE INDEX IF NOT EXISTS idx_plan_phases_plan ON plan_phases(plan_id);

CREATE TABLE IF NOT EXISTS plan_sessions (
    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL,
    linked_at TEXT NOT NULL,
    PRIMARY KEY (plan_id, session_id)
);
CREATE INDEX IF NOT EXISTS idx_plan_sessions_session ON plan_sessions(session_id);

CREATE TABLE IF NOT EXISTS plan_signoffs (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    phase_id TEXT REFERENCES plan_phases(id),
    session_id TEXT NOT NULL,
    by TEXT NOT NULL,
    action TEXT NOT NULL,
    comment TEXT,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_plan_signoffs_plan ON plan_signoffs(plan_id);
```

- [x] **Step 3: Verify it compiles**

Run: `go build ./internal/plan/`
Expected: no errors

- [x] **Step 4: Commit**

```bash
git add internal/plan/store.go internal/plan/store_sqlite.go
git commit -m "feat(plan): add PlanStore interface and SQLite implementation"
```

---

### Task 3: Plan.md Parser

**Files:**
- Create: `internal/plan/parser.go`

- [x] **Step 1: Create the plan.md parser**

The parser reads a plan.md file and extracts structured data:

```go
package plan

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// ParsedPlan holds the structured data extracted from a plan.md file.
type ParsedPlan struct {
	Title       string
	PlanID      string
	Project     string
	Status      string
	Summary     string
	Phases      []ParsedPhase
	Notes       []string
}

// ParsedPhase holds a phase and its steps.
type ParsedPhase struct {
	Name     string
	Sequence int
	State    PhaseState
	Steps    []ParsedStep
}

// ParsedStep holds a single step from a plan.
type ParsedStep struct {
	Number      int
	Description string
	State       StepStatus
	DependsOn   []int // Step numbers this depends on
}

// StepStatus represents the status annotation on a step.
type StepStatus string

const (
	StepPending   StepStatus = "pending"
	StepCompleted StepStatus = "completed"
	StepFailed    StepStatus = "failed"
	StepSkipped   StepStatus = "skipped"
)

var (
	// ## Phase 1: Design [pending]
	phaseHeaderRe = regexp.MustCompile(`^##\s+Phase\s+(\d+):\s+(.+?)\s*\[(\w+)\]\s*$`)
	// 1. ~~Analyze auth flow~~ [completed]
	stepCompletedRe = regexp.MustCompile(`^(\d+)\.\s+~~(.+?)~~\s*\[(\w+)\]`)
	// 4. Update auth middleware [pending] (depends: 2, 3)
	stepWithDepsRe = regexp.MustCompile(`^(\d+)\.\s+(.+?)\s*\[(\w+)\]\s*\(depends:\s*(.+?)\)`)
	// 2. Design token scheme [pending]
	stepBasicRe = regexp.MustCompile(`^(\d+)\.\s+(.+?)\s*\[(\w+)\]`)
	// - key: value
	metaLineRe = regexp.MustCompile(`^-\s+(\w+):\s+(.+)$`)
)
```

Implement `ParsePlan(filePath string) (*ParsedPlan, error)`:
1. Open file, scan line by line
2. Track current section (`meta`, `summary`, `phase`, `notes`)
3. Parse `## Meta` key-value pairs into `ParsedPlan` fields
4. Parse `## Summary` text into `Summary` field
5. Parse `## Phase N: Name [state]` into `ParsedPhase`
6. Parse numbered steps with `[status]` and optional `(depends: N, M)`
7. Parse `## Notes` into string slice
8. Return structured `ParsedPlan`

Implement `ParsePlanContent(content string) (*ParsedPlan, error)` for in-memory parsing (used by writer tests).

- [x] **Step 2: Verify it compiles**

Run: `go build ./internal/plan/`
Expected: no errors

- [x] **Step 3: Commit**

```bash
git add internal/plan/parser.go
git commit -m "feat(plan): add plan.md parser"
```

---

### Task 4: Plan.md Writer

**Files:**
- Create: `internal/plan/writer.go`

- [x] **Step 1: Create the plan.md writer**

```go
package plan

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// WritePlanMarkdown generates a plan.md file from a Plan and its phases.
func WritePlanMarkdown(filePath string, plan *Plan, phases []ParsedPhase) error
```

Implement `WritePlanMarkdown`:
1. Build the markdown content string from Plan + phases
2. Write `## Meta` section with plan_id, project, created, status
3. Write `## Summary` with plan description
4. Write each `## Phase N: Name [state]` with numbered steps
5. Completed steps wrapped in `~~strikethrough~~`
6. Dependencies annotated as `(depends: N, M)`
7. Write `## Notes` section
8. Write to file (create parent dirs if needed)

Implement `UpdatePlanStatus(filePath string, planState PlanState, phases []PlanPhase) error`:
1. Read the existing file content
2. Parse it
3. Update the `status:` meta line
4. Update phase `[state]` annotations
5. Update step `[status]` annotations based on phase progress
6. Write the updated content back

- [x] **Step 2: Verify it compiles**

Run: `go build ./internal/plan/`
Expected: no errors

- [x] **Step 3: Commit**

```bash
git add internal/plan/writer.go
git commit -m "feat(plan): add plan.md writer"
```

---

### Task 5: PlanStore Tests

**Files:**
- Create: `internal/plan/store_test.go`

- [x] **Step 1: Write PlanStore tests**

Test file `internal/plan/store_test.go` covering:
- `TestCreateAndGetPlan` — create plan, fetch by ID, verify all fields
- `TestUpdatePlanState` — transition through planning → draft → pending_approval → approved → executing → completed → confirmed
- `TestCreateAndGetPhases` — create plan with 3 phases, fetch, verify ordering
- `TestPhaseProgress` — increment completed_steps, verify counts
- `TestSessionLinking` — link plan to session, query by session, unlink
- `TestSignoffs` — create signoffs (approve, reject, confirm), query by plan
- `TestRevisionCount` — create multiple revision_requested signoffs, count
- `TestCountBySessionAndState` — link multiple plans to session, verify state counts
- `TestDeletePlan` — cascade deletes phases, sessions, signoffs

Use t.TempDir() for the SQLite DB path. Follow the table-driven test pattern from `internal/task/`.

- [x] **Step 2: Run tests to verify they pass**

Run: `go test ./internal/plan/ -v -run TestPlan`
Expected: all PASS

- [x] **Step 3: Commit**

```bash
git add internal/plan/store_test.go
git commit -m "test(plan): add PlanStore SQLite tests"
```

---

### Task 6: Parser and Writer Tests

**Files:**
- Create: `internal/plan/parser_test.go`
- Create: `internal/plan/writer_test.go`

- [x] **Step 1: Write parser tests**

Test file `internal/plan/parser_test.go` covering:
- `TestParseFullPlan` — parse the example plan.md from the spec, verify all fields, phases, steps, dependencies
- `TestParseMinimalPlan` — plan with no phases (just meta + summary)
- `TestParsePlanWithCompletedSteps` — strikethrough steps parsed correctly
- `TestParsePlanWithDependencies` — `(depends: 2, 3)` parsed into int slice
- `TestParsePlanContent` — in-memory parsing works identically to file parsing
- `TestParseMissingFile` — returns error for nonexistent file

Use `ParsePlanContent` with the spec example as test data.

- [x] **Step 2: Write writer tests**

Test file `internal/plan/writer_test.go` covering:
- `TestWritePlanMarkdown` — write plan to temp file, read it back, verify structure
- `TestUpdatePlanStatus` — write plan, update status, verify meta line changed
- `TestRoundTrip` — write plan, parse it, write again, compare content identical
- `TestUpdatePhaseStates` — update phase states in file, parse to verify

- [x] **Step 3: Run tests**

Run: `go test ./internal/plan/ -v`
Expected: all PASS

- [x] **Step 4: Commit**

```bash
git add internal/plan/parser_test.go internal/plan/writer_test.go
git commit -m "test(plan): add parser and writer tests"
```

---

### Task 7: Configuration

**Files:**
- Modify: `internal/config/schema.go`
- Modify: `config/meept.json5`

- [x] **Step 1: Add PlansConfig to schema.go**

Add these structs to `internal/config/schema.go`:

```go
// PlansConfig configures the plan system.
type PlansConfig struct {
	Mode         string                `json:"mode"         toml:"mode"`
	Threshold    PlansThresholdConfig  `json:"threshold"    toml:"threshold"`
	Storage      PlansStorageConfig    `json:"storage"      toml:"storage"`
	Approval     PlansApprovalConfig   `json:"approval"     toml:"approval"`
	Confirmation PlansConfirmationConfig `json:"confirmation" toml:"confirmation"`
}

type PlansThresholdConfig struct {
	MinSteps          int      `json:"min_steps"           toml:"min_steps"`
	ComplexityKeywords []string `json:"complexity_keywords" toml:"complexity_keywords"`
	AlwaysPlanIntents []string `json:"always_plan_intents" toml:"always_plan_intents"`
}

type PlansStorageConfig struct {
	DefaultPath      string `json:"default_path"      toml:"default_path"`
	ExternalPath     string `json:"external_path"     toml:"external_path"`
	FilenameTemplate string `json:"filename_template" toml:"filename_template"`
}

type PlansApprovalConfig struct {
	RequireApproval   bool `json:"require_approval"    toml:"require_approval"`
	AutoApproveSimple bool `json:"auto_approve_simple" toml:"auto_approve_simple"`
	AllowRevision     bool `json:"allow_revision"      toml:"allow_revision"`
	MaxRevisions      int  `json:"max_revisions"       toml:"max_revisions"`
}

type PlansConfirmationConfig struct {
	RequireSignoff     bool `json:"require_signoff"      toml:"require_signoff"`
	AutoConfirmPhases  bool `json:"auto_confirm_phases"  toml:"auto_confirm_phases"`
}
```

Add `Plans PlansConfig` field to the root `Config` struct.

Add defaults in `DefaultConfig()`:
```go
Plans: PlansConfig{
	Mode: "threshold",
	Threshold: PlansThresholdConfig{
		MinSteps: 3,
		ComplexityKeywords: []string{
			"refactor", "migrate", "implement", "redesign",
			"rewrite", "integrate", "architect",
		},
		AlwaysPlanIntents: []string{"plan", "implement", "build"},
	},
	Storage: PlansStorageConfig{
		DefaultPath:      "docs/plans",
		FilenameTemplate: "{{slug}}.md",
	},
	Approval: PlansApprovalConfig{
		RequireApproval: true,
		AllowRevision:   true,
		MaxRevisions:    3,
	},
	Confirmation: PlansConfirmationConfig{
		RequireSignoff: true,
	},
},
```

- [x] **Step 2: Update config template**

Add to `config/meept.json5` (before the closing brace):

```json5
  // Plan system configuration
  plans: {
    // Plan creation mode: "threshold" (default), "always", "off"
    mode: "threshold",

    threshold: {
      // Minimum steps from LLM decomposition to trigger plan creation
      min_steps: 3,
      // Keywords that signal complexity
      complexity_keywords: [
        "refactor", "migrate", "implement", "redesign",
        "rewrite", "integrate", "architect",
      ],
      // Intent types that always trigger plan creation
      always_plan_intents: ["plan", "implement", "build"],
    },

    storage: {
      // Default plan directory (relative to project root)
      default_path: "docs/plans",
      // Override to store plans outside project (e.g., "~/.meept/plans")
      external_path: "",
      // Filename template: {{slug}}, {{date}}, {{id}}
      filename_template: "{{slug}}.md",
    },

    approval: {
      // Require explicit approval before plan execution
      require_approval: true,
      // Auto-approve plans with <= 3 steps
      auto_approve_simple: false,
      // Allow revise/re-submit cycle
      allow_revision: true,
      // Max revision rounds before auto-reject
      max_revisions: 3,
    },

    confirmation: {
      // Require human sign-off after all steps complete
      require_signoff: true,
      // Auto-confirm individual phases
      auto_confirm_phases: false,
    },
  },
```

- [x] **Step 3: Verify it compiles**

Run: `go build ./internal/config/...` and `go build ./...`
Expected: no errors (other packages may need `Plans` field access added later)

- [x] **Step 4: Commit**

```bash
git add internal/config/schema.go config/meept.json5
git commit -m "feat(config): add PlansConfig to schema and template"
```

---

### Task 8: PlanManager — Lifecycle and Synthesis

**Files:**
- Create: `internal/plan/manager.go`

- [x] **Step 1: Create PlanManager**

```go
package plan

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"meept/internal/bus"
	"meept/internal/config"
	"meept/internal/task"
	"meept/pkg/models"
)

// PlanManager orchestrates plan lifecycle: creation, approval, synthesis, progress.
type PlanManager struct {
	store  PlanStore
	bus    *bus.MessageBus
	config config.PlansConfig
	logger *slog.Logger
}
```

Methods to implement:
- `NewPlanManager(store PlanStore, bus *bus.MessageBus, cfg config.PlansConfig, logger *slog.Logger) *PlanManager`
- `CreatePlan(ctx context.Context, title, description, projectID, sessionID string) (*Plan, error)` — creates plan record in `planning` state, creates plan.md via writer, links session, publishes `plan.created`
- `SubmitPlan(ctx context.Context, planID string) error` — moves to `pending_approval`, publishes `plan.submitting`. If `Approval.RequireApproval` is false, auto-approves.
- `ApprovePlan(ctx context.Context, planID, sessionID, by string) error` — validates state is `pending_approval`, creates signoff, moves to `approved`, calls `Synthesize()`, publishes `plan.approved`
- `RejectPlan(ctx context.Context, planID, sessionID, by, reason string) error` — creates signoff, moves to `cancelled`, publishes `plan.rejected`
- `RevisePlan(ctx context.Context, planID, sessionID, feedback string) error` — checks max revisions, increments count, moves back to `planning`, publishes `plan.revised`
- `ConfirmPlan(ctx context.Context, planID, sessionID, by string) error` — validates state is `completed`, creates signoff, moves to `confirmed`, publishes `plan.confirmed`
- `CancelPlan(ctx context.Context, planID, reason string) error` — moves to `cancelled`, publishes `plan.cancelled`
- `Synthesize(ctx context.Context, planID string) error` — creates parent Task + child Tasks per phase + TaskSteps per step, sets plan state to `executing`, publishes `plan.executing`
- `OnStepCompleted(ctx context.Context, taskID, stepID string) error` — finds the phase by taskID, increments completed_steps, updates phase state
- `OnTaskCompleted(ctx context.Context, taskID string) error` — if child task: update phase state. If parent task: update plan state to `completed`
- `GetPlansForSession(ctx context.Context, sessionID string) ([]*Plan, error)` — delegates to store
- `ShouldCreatePlan(intent string, stepCount int) bool` — checks config mode, threshold logic, keywords
- `resolvePlanDir(projectPath string) string` — returns external_path if set, otherwise projectPath + default_path

- [x] **Step 2: Verify it compiles**

Run: `go build ./internal/plan/`
Expected: no errors

- [x] **Step 3: Commit**

```bash
git add internal/plan/manager.go
git commit -m "feat(plan): add PlanManager with lifecycle and synthesis"
```

---

### Task 9: Bus Event Handler

**Files:**
- Create: `internal/plan/handler.go`

- [x] **Step 1: Create the bus event handler**

```go
package plan

import (
	"context"
	"encoding/json"
	"log/slog"

	"meept/internal/bus"
	"meept/pkg/models"
)

// PlanHandler subscribes to task events and routes progress to PlanManager.
type PlanHandler struct {
	manager *PlanManager
	bus     *bus.MessageBus
	logger  *slog.Logger
	cancel  context.CancelFunc
}
```

Methods:
- `NewPlanHandler(manager *PlanManager, bus *bus.MessageBus, logger *slog.Logger) *PlanHandler`
- `Start(ctx context.Context) error` — subscribes to `task.step.completed`, `task.completed` via bus, runs subscription goroutines (same pattern as Orchestrator)
- `Stop()` — cancels context, unsubscribes
- `handleStepCompleted(ctx context.Context, msg *models.BusMessage)` — unmarshal payload, call `manager.OnStepCompleted`
- `handleTaskCompleted(ctx context.Context, msg *models.BusMessage)` — unmarshal payload, call `manager.OnTaskCompleted`

Subscriptions follow the Orchestrator pattern: topic map → subscribe → `runSubscription` goroutine.

- [x] **Step 2: Verify it compiles**

Run: `go build ./internal/plan/`
Expected: no errors

- [x] **Step 3: Commit**

```bash
git add internal/plan/handler.go
git commit -m "feat(plan): add bus event handler for task progress"
```

---

### Task 10: PlanManager Tests

**Files:**
- Create: `internal/plan/manager_test.go`

- [x] **Step 1: Write PlanManager tests**

Test file covering:
- `TestCreatePlan` — creates plan, verifies state is `planning`, file exists on disk, session linked
- `TestApprovePlanFlow` — create → submit → approve, verify state transitions, signoff created
- `TestRejectPlan` — create → submit → reject, verify cancelled state
- `TestRevisePlan` — create → submit → revise, verify back to planning, revision count incremented
- `TestMaxRevisions` — exhaust revision limit, verify auto-reject on next attempt
- `TestConfirmPlan` — create → approve → complete all steps → confirm, verify confirmed state
- `TestShouldCreatePlanThreshold` — verify threshold logic with various intents and step counts
- `TestShouldCreatePlanAlways` — mode=always, always returns true
- `TestShouldCreatePlanOff` — mode=off, always returns false
- `TestAutoApproveSimple` — auto_approve_simple=true, plan with <=3 steps auto-approves

Use in-memory SQLite store (t.TempDir()) and a mock/noop bus.

- [x] **Step 2: Run tests**

Run: `go test ./internal/plan/ -v -run TestManager`
Expected: all PASS

- [x] **Step 3: Commit**

```bash
git add internal/plan/manager_test.go
git commit -m "test(plan): add PlanManager lifecycle tests"
```

---

### Task 11: Plan Service Layer

**Files:**
- Create: `internal/services/plan_service.go`
- Modify: `internal/services/service.go`

- [x] **Step 1: Create PlanService**

```go
package services

import (
	"context"
	"meept/internal/plan"
)

type PlanService struct {
	manager *plan.PlanManager
	store   plan.PlanStore
}

func NewPlanService(manager *plan.PlanManager, store plan.PlanStore) *PlanService {
	return &PlanService{manager: manager, store: store}
}

type CreatePlanRequest struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	SessionID   string `json:"session_id"`
}

type ApprovePlanRequest struct {
	PlanID    string `json:"plan_id"`
	SessionID string `json:"session_id"`
	By        string `json:"by"`
}

type RejectPlanRequest struct {
	PlanID    string `json:"plan_id"`
	SessionID string `json:"session_id"`
	By        string `json:"by"`
	Reason    string `json:"reason,omitempty"`
}

type ConfirmPlanRequest struct {
	PlanID    string `json:"plan_id"`
	SessionID string `json:"session_id"`
	By        string `json:"by"`
}

type RevisePlanRequest struct {
	PlanID    string `json:"plan_id"`
	SessionID string `json:"session_id"`
	Feedback  string `json:"feedback"`
}
```

Methods:
- `Create(ctx, CreatePlanRequest) (*plan.Plan, error)`
- `Get(ctx, planID string) (*plan.Plan, error)`
- `List(ctx, projectID string, limit int) ([]*plan.Plan, error)`
- `ListBySession(ctx, sessionID string) ([]*plan.Plan, error)`
- `Approve(ctx, ApprovePlanRequest) (*plan.Plan, error)`
- `Reject(ctx, RejectPlanRequest) (*plan.Plan, error)`
- `Confirm(ctx, ConfirmPlanRequest) (*plan.Plan, error)`
- `Revise(ctx, RevisePlanRequest) (*plan.Plan, error)`
- `GetPlansForSession(ctx, sessionID string) ([]*plan.Plan, error)`

All methods follow the service pattern: validate input → nil-check dependency → delegate → wrapError.

- [x] **Step 2: Add PlanService to ServiceRegistry**

In `internal/services/service.go`:
- Add `Plan *PlanService` field to `ServiceRegistry`
- Add `PlanManager *plan.PlanManager` and `PlanStore plan.PlanStore` to `Config`
- Add conditional construction in `NewRegistry`

- [x] **Step 3: Verify it compiles**

Run: `go build ./internal/services/...`
Expected: no errors

- [x] **Step 4: Commit**

```bash
git add internal/services/plan_service.go internal/services/service.go
git commit -m "feat(services): add PlanService to service layer"
```

---

### Task 12: RPC Handlers

**Files:**
- Create: `internal/rpc/plan.go`

- [x] **Step 1: Create PlanRPC handler**

```go
package rpc

import (
	"context"
	"encoding/json"
	"meept/internal/services"
)

type PlanHandler struct {
	services *services.ServiceRegistry
}

func NewPlanHandler(services *services.ServiceRegistry) *PlanHandler
func (h *PlanHandler) RegisterPlanMethods(server *Server)
```

Register methods:
- `plan.create` — calls PlanService.Create
- `plan.list` — calls PlanService.List (params: project_id, limit)
- `plan.get` — calls PlanService.Get (params: id)
- `plan.approve` — calls PlanService.Approve
- `plan.reject` — calls PlanService.Reject
- `plan.confirm` — calls PlanService.Confirm
- `plan.revise` — calls PlanService.Revise
- `plan.list_by_session` — calls PlanService.ListBySession (params: session_id)
- `plan.count_by_session` — calls PlanStore.CountPlansBySessionAndState

Each handler follows the pattern: unmarshal params → call service → return result map or error.

- [x] **Step 2: Register in daemon RPC wiring**

In the daemon's RPC setup (where other handlers like QueueHandler, DaemonRPCHandler are registered), add:
```go
planHandler := rpc.NewPlanHandler(serviceRegistry)
planHandler.RegisterPlanMethods(rpcServer)
```

- [x] **Step 3: Verify it compiles**

Run: `go build ./...`
Expected: no errors

- [x] **Step 4: Commit**

```bash
git add internal/rpc/plan.go
git commit -m "feat(rpc): add plan RPC handlers"
```

---

### Task 13: HTTP API Endpoints

**Files:**
- Modify: `internal/comm/http/server.go`
- Modify: `internal/comm/http/api_handlers.go`

- [x] **Step 1: Register plan routes**

In `internal/comm/http/server.go` `setupRESTRoutes()`:
```go
mux.HandleFunc("GET /api/v1/plans", s.handlePlanList)
mux.HandleFunc("POST /api/v1/plans", s.handlePlanCreate)
mux.HandleFunc("GET /api/v1/plans/{id}", s.handlePlanGet)
mux.HandleFunc("POST /api/v1/plans/{id}/approve", s.handlePlanApprove)
mux.HandleFunc("POST /api/v1/plans/{id}/reject", s.handlePlanReject)
mux.HandleFunc("POST /api/v1/plans/{id}/confirm", s.handlePlanConfirm)
mux.HandleFunc("POST /api/v1/plans/{id}/revise", s.handlePlanRevise)
mux.HandleFunc("GET /api/v1/sessions/{id}/plans", s.handleSessionPlans)
```

- [x] **Step 2: Implement HTTP handlers**

In `internal/comm/http/api_handlers.go`, add handlers following the existing 4-step pattern (nil-check service → decode/parse → call service → write JSON/error).

- `handlePlanList` — GET with query params: project_id, limit (default 50)
- `handlePlanCreate` — POST with JSON body: title, description, project_id, session_id
- `handlePlanGet` — GET with path param `id` via `r.PathValue("id")`
- `handlePlanApprove` — POST with JSON body: session_id, by
- `handlePlanReject` — POST with JSON body: session_id, by, reason
- `handlePlanConfirm` — POST with JSON body: session_id, by
- `handlePlanRevise` — POST with JSON body: session_id, feedback
- `handleSessionPlans` — GET with path param session `id`

- [x] **Step 3: Verify it compiles**

Run: `go build ./internal/comm/http/...`
Expected: no errors

- [x] **Step 4: Commit**

```bash
git add internal/comm/http/server.go internal/comm/http/api_handlers.go
git commit -m "feat(http): add plan REST API endpoints"
```

---

### Task 14: CLI Commands

**Files:**
- Create: `cmd/meept/plans.go`
- Modify: `cmd/meept/root.go`

- [x] **Step 1: Create plans command**

Create `cmd/meept/plans.go` following the cobra pattern from `cmd/meept/session.go`:

```go
func newPlansCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:     "plans",
        Short:   "Manage plans",
        Long:    "List, show, approve, reject, and confirm plans.",
        Aliases: []string{"plan"},
    }
    cmd.AddCommand(newPlansListCmd())
    cmd.AddCommand(newPlansShowCmd())
    cmd.AddCommand(newPlansApproveCmd())
    cmd.AddCommand(newPlansRejectCmd())
    cmd.AddCommand(newPlansConfirmCmd())
    return cmd
}
```

Subcommands:
- `plans list [--project project_id] [--json]` — calls `plan.list` RPC, tabular output or JSON
- `plans show <id>` — calls `plan.get` RPC, displays plan details with phase progress
- `plans approve <id>` — calls `plan.approve` RPC
- `plans reject <id> [--reason text]` — calls `plan.reject` RPC
- `plans confirm <id>` — calls `plan.confirm` RPC

All follow the 6-step CLI pattern: `connectDaemon()` → build params → `client.Call(method, params)` → unmarshal → check error → format output.

- [x] **Step 2: Register in root.go**

Add `cmd.AddCommand(newPlansCmd())` to the root command setup in `cmd/meept/root.go`.

- [x] **Step 3: Verify it compiles**

Run: `go build ./cmd/meept/`
Expected: no errors

- [x] **Step 4: Commit**

```bash
git add cmd/meept/plans.go cmd/meept/root.go
git commit -m "feat(cli): add plans subcommands"
```

---

### Task 15: Daemon Wiring

**Files:**
- Modify: `internal/daemon/daemon.go` (or the file that initializes all daemon components)

- [x] **Step 1: Wire PlanStore, PlanManager, PlanHandler into the daemon**

In the daemon initialization sequence (after task store, bus, and config are set up):
1. Create `PlanStore`: `plan.NewSQLiteStore(cfg.PlansStoragePath(), logger)`
2. Create `PlanManager`: `plan.NewPlanManager(planStore, messageBus, cfg.Plans, logger)`
3. Create `PlanHandler`: `plan.NewPlanHandler(planManager, messageBus, logger)`
4. Start `PlanHandler`: `planHandler.Start(ctx)`
5. Add `PlanManager` and `PlanStore` to `services.Config`
6. Wire cleanup: `planHandler.Stop()` in daemon shutdown

- [x] **Step 2: Verify full build**

Run: `go build ./...`
Expected: no errors

- [x] **Step 3: Commit**

```bash
git add internal/daemon/daemon.go
git commit -m "feat(daemon): wire plan system into daemon lifecycle"
```

---

### Task 16: Dispatcher Plan Routing

**Files:**
- Modify: `internal/agent/dispatcher.go`

- [x] **Step 1: Add plan routing to Dispatcher**

Add a `planManager *plan.PlanManager` field to the `Dispatcher` struct.

In `ClassifyAndRoute`, after step 6 (single intent classification) and before step 7 (task creation):
```go
// Check if plan creation is warranted
if d.planManager != nil && d.planManager.ShouldCreatePlan(intent.Type, 0) {
    // Route to plan creation instead of direct task creation
    return d.routeToPlan(ctx, input, intent, sessionID)
}
```

Implement `routeToPlan`:
- Creates a plan via `planManager.CreatePlan`
- Returns a DispatchResult with the plan info and a response indicating the plan is being created
- If `/plan` command is detected (slash command handling), force plan creation regardless of mode

Also update the slash command handler to recognize `/plan`:
- Parse `/plan <description>` or just `/plan` (uses current input context)
- Force `ShouldCreatePlan` to return true
- Route to plan creation

- [x] **Step 2: Verify it compiles**

Run: `go build ./internal/agent/...`
Expected: no errors

- [x] **Step 3: Commit**

```bash
git add internal/agent/dispatcher.go
git commit -m "feat(agent): add plan routing to dispatcher"
```

---

### Task 17: Orchestrator Integration

**Files:**
- Modify: `internal/agent/orchestrator.go`

- [x] **Step 1: Wire plan events into Orchestrator**

Add `planManager *plan.PlanManager` field to `Orchestrator`.

In `Start()`, add to the topics map:
```go
"task.step.completed": o.handleStepCompletedForPlans,
"task.completed":      o.handleTaskCompletedForPlans,
```

New handlers:
- `handleStepCompletedForPlans` — unmarshal task step event, call `planManager.OnStepCompleted`
- `handleTaskCompletedForPlans` — unmarshal task event, call `planManager.OnTaskCompleted`

These forward task events to the plan system for progress tracking.

- [x] **Step 2: Verify it compiles**

Run: `go build ./internal/agent/...`
Expected: no errors

- [x] **Step 3: Commit**

```bash
git add internal/agent/orchestrator.go
git commit -m "feat(orchestrator): forward task events to PlanManager"
```

---

### Task 18: TUI — Plans Tab Model

**Files:**
- Create: `internal/tui/models/plans.go`
- Modify: `internal/tui/types/types.go`

- [x] **Step 1: Add TUI plan types**

In `internal/tui/types/types.go`, add:

```go
type PlanExtended struct {
    ID              string       `json:"id"`
    Title           string       `json:"title"`
    Description     string       `json:"description,omitempty"`
    FilePath        string       `json:"file_path"`
    ProjectID       string       `json:"project_id,omitempty"`
    State           string       `json:"state"`
    CreatedAt       string       `json:"created_at"`
    UpdatedAt       string       `json:"updated_at"`
    SourceSession   string       `json:"source_session,omitempty"`
    TaskID          string       `json:"task_id,omitempty"`
    RevisionCount   int          `json:"revision_count,omitempty"`
    Phases          []PlanPhaseView `json:"phases,omitempty"`
    TotalSteps      int          `json:"total_steps"`
    CompletedSteps  int          `json:"completed_steps"`
    FailedSteps     int          `json:"failed_steps"`
}

type PlanPhaseView struct {
    ID             string `json:"id"`
    Name           string `json:"name"`
    Sequence       int    `json:"sequence"`
    TotalSteps     int    `json:"total_steps"`
    CompletedSteps int    `json:"completed_steps"`
    FailedSteps    int    `json:"failed_steps"`
    State          string `json:"state"`
}

type PlanListResponse struct {
    Plans []PlanExtended `json:"plans"`
    Err   string         `json:"err,omitempty"`
}

type PlanStateCounts struct {
    Planning        int `json:"planning"`
    Draft           int `json:"draft"`
    PendingApproval int `json:"pending_approval"`
    Approved        int `json:"approved"`
    Executing       int `json:"executing"`
    Completed       int `json:"completed"`
    Confirmed       int `json:"confirmed"`
    Failed          int `json:"failed"`
    Cancelled       int `json:"cancelled"`
}
```

- [x] **Step 2: Create PlansModel**

Create `internal/tui/models/plans.go` following the exact pattern from `internal/tui/models/tasks.go`:

```go
type PlansModel struct {
    rpc       PlansRPCClient
    plans     []types.PlanExtended
    table     table.Model
    selected  *types.PlanExtended
    width     int
    height    int
    loading   bool
    err       error
    filter    PlanFilter
    sessionID string
}

type PlanFilter string
const (
    PlanFilterAll       PlanFilter = "all"
    PlanFilterActive    PlanFilter = "active"
    PlanFilterPending   PlanFilter = "pending"
    PlanFilterCompleted PlanFilter = "completed"
)

type PlansRPCClient interface {
    ListPlans() (*types.PlanListResponse, error)
    ApprovePlan(id string) error
    RejectPlan(id string) error
    ConfirmPlan(id string) error
    IsConnected() bool
}
```

Implement:
- `NewPlansModel(rpc PlansRPCClient) *PlansModel`
- `Init() tea.Cmd` — fetch plans
- `Update(msg tea.Msg) (tea.Model, tea.Cmd)` — handle key events, data updates
- `View() string` — render plan list with phases and progress bars
- `SetSession(sessionID string)` — filter to session's plans
- `fetchPlans() tea.Msg` — RPC call to `plan.list_by_session`
- `renderPlanRow()` — render a single plan with state icon, title, phase bars
- `renderPlanDetail()` — expanded view showing all phases with step counts
- `renderEmpty()` — empty state message
- `renderHeader()` — title + filter tabs

State icons: `●` planning (blue), `●` draft (gray), `●` pending_approval (blue), `✓` approved (green), `●` executing (amber), `●` completed (green), `★` confirmed (green bold), `✗` failed (red), `○` cancelled (gray)

Key bindings: `a` approve, `r` reject, `c` confirm, `v` revise, `e` edit plan.md, `enter` detail, `/` filter, `n` new plan.

- [x] **Step 3: Verify it compiles**

Run: `go build ./internal/tui/...`
Expected: no errors

- [x] **Step 4: Commit**

```bash
git add internal/tui/models/plans.go internal/tui/types/types.go
git commit -m "feat(tui): add PlansModel for plans tab"
```

---

### Task 19: TUI — App Integration (Tab, Header, Session Picker)

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/modal.go`
- Modify: `internal/tui/styles.go`

- [x] **Step 1: Add ViewPlans to app.go**

1. Add `ViewPlans` to the `ViewType` const block (after `ViewMemory`)
2. Add `plans *models.PlansModel` field to `App` struct
3. Instantiate in `NewApp`: `plans: models.NewPlansModel(rpcClient)`
4. Add `{\"Plans\", ViewPlans}` to the `renderTabs()` tabs slice
5. Add `case ViewPlans` to all four switch statements: `Update`, `View`, `initCurrentView`
6. Add key binding for switching to plans tab (e.g., `4` or `ctrl+x 5`)

- [x] **Step 2: Add header badges** (partial: TODO in app.go line 1795-1796, plan badges rendering is stubbed out)

In `renderHeader()`, after the session name line, add a plan badges line:
- Fetch plan state counts for current session
- Render color-coded badges: `plans: N confirmed  N executing (X/Y steps)  N pending approval`
- Use the state color coding from the spec (blue/amber/green/red)

Use the existing sidebar refresh tick (2-second interval) to also refresh plan counts.

- [x] **Step 3: Update session picker** (partial: TODO in modal.go line 429-432, plan count indicators are stubbed out)

In `SessionPickerModal`, add plan indicators to each session row:
- After description, before last activity time
- Show `■ N plans: <state summary>` or `no plans`
- Add `[p] plans` to footer actions

- [x] **Step 4: Add plan state styles to styles.go**

```go
// Plan state styles
PlanStatePlanning       lipgloss.Style // blue
PlanStateDraft          lipgloss.Style // gray
PlanStatePending        lipgloss.Style // blue
PlanStateApproved       lipgloss.Style // green
PlanStateExecuting      lipgloss.Style // amber
PlanStateCompleted      lipgloss.Style // green
PlanStateConfirmed      lipgloss.Style // green bold
PlanStateFailed         lipgloss.Style // red
PlanStateCancelled      lipgloss.Style // gray
```

- [x] **Step 5: Verify it compiles**

Run: `go build ./internal/tui/...`
Expected: no errors

- [x] **Step 6: Commit**

```bash
git add internal/tui/app.go internal/tui/modal.go internal/tui/styles.go
git commit -m "feat(tui): integrate plans tab, header badges, session picker"
```

---

### Task 20: TUI — Chat Inline Notification

**Files:**
- Modify: `internal/tui/events.go`
- Modify: `internal/tui/models/chat.go`

- [x] **Step 1: Subscribe to plan bus events**

In `internal/tui/events.go`, add subscriptions for `plan.created`, `plan.approved`, `plan.rejected`, `plan.submitting` events. These map to `PlanNotificationMsg` messages.

- [x] **Step 2: Render plan notifications in chat**

In `internal/tui/models/chat.go`, handle `PlanNotificationMsg`:
- `plan.submitting` → render inline notification box:
  ```
  + plan ready for review ----------------------------------------+
  | Plan: "Add OAuth2 Token Refresh"
  | 3 phases · 8 steps · threshold: complex
  | [2] plans tab to review  ·  /approve plan-a1b2
  +--------------------------------------------------------------+
  ```
- `plan.completed` → render completion notification
- `plan.confirmed` → render confirmation notification

Use the existing `ChatTaskResultMsg` rendering pattern (box with border).

- [x] **Step 3: Verify it compiles**

Run: `go build ./internal/tui/...`
Expected: no errors

- [x] **Step 4: Commit**

```bash
git add internal/tui/events.go internal/tui/models/chat.go
git commit -m "feat(tui): add plan chat notifications and bus event handling"
```

---

### Task 21: CollaborativePlanner Cleanup

**Files:**
- Delete: `internal/agent/collaborative.go`
- Modify: Any files importing or referencing collaborative types

- [x] **Step 1: Find all references to CollaborativePlanner**

Search the codebase for imports of `collaborative` package references, `NewCollaborativePlanner`, `TaskPlan`, `PlanReview`, `PlanStatus`, and `IsProgrammingTask`.

- [x] **Step 2: Remove CollaborativePlanner code**

Delete `internal/agent/collaborative.go`. This removes:
- `TaskPlan`, `TaskStep`, `PlanReview` types (replaced by `internal/plan/` types)
- `CollaborativePlanner` struct and all methods
- `Planner` interface (now fulfilled by `PlanManager`)

- [x] **Step 3: Update references**

Update any files that import from the collaborative code. The `WorkspaceManager` in `workspace.go` is retained — only remove collaborative-specific types (`TaskPlanInfo`, `TaskStepInfo`) if they are only used by collaborative code.

- [x] **Step 4: Verify full build**

Run: `go build ./...`
Expected: no errors

- [x] **Step 5: Run all tests**

Run: `go test ./... -v`
Expected: all existing tests pass (no regressions from collaborative removal)

- [x] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor: remove deferred CollaborativePlanner, replaced by internal/plan"
```

---

### Task 22: Documentation

**Files:**
- Create: `docs/concepts/plans.md`
- Create: `docs/workflows/plans.md`
- Modify: `CLAUDE.md`
- Modify: `docs/concepts/architecture.md`
- Modify: `docs/reference/cli.md`
- Modify: `docs/reference/http-api.md`
- Modify: `mkdocs.yml`

- [x] **Step 1: Create docs/concepts/plans.md**

New page covering:
- Plan concepts and lifecycle
- Plan.md format reference
- Plan-to-task mapping architecture
- Plan states and transitions
- Configuration options

- [x] **Step 2: Create docs/workflows/plans.md**

Feature spec covering:
- Plan creation triggers
- Approval workflow
- Execution and progress tracking
- Confirmation/sign-off
- TUI views (header badges, plans tab, session picker)

- [x] **Step 3: Update CLAUDE.md**

- Add `internal/plan/` to the architecture table: `**Plans** | internal/plan (plan, store, manager, parser, writer, handler)`
- Add `meept plans` CLI commands to the Build & Development Commands section
- Update the Key Components table

- [x] **Step 4: Update docs/concepts/architecture.md**

- Add Plan layer to the request flow diagram
- Add PlanManager to the component map

- [x] **Step 5: Update docs/reference/cli.md**

- Add `meept plans list`, `meept plans show`, `meept plans approve`, `meept plans reject`, `meept plans confirm` command reference

- [x] **Step 6: Update docs/reference/http-api.md**

- Add plan endpoint documentation: GET/POST /api/v1/plans, approve/reject/confirm endpoints

- [x] **Step 7: Update mkdocs.yml** (nav handled via .nav.yml files, plans.md included in concepts and workflows)

- Add `plans.md` to nav under `concepts` and `workflows`

- [x] **Step 8: Commit**

```bash
git add docs/ CLAUDE.md mkdocs.yml
git commit -m "docs: add plan system documentation"
```

---

### Task 23: Final Integration Test

**Files:**
- No new files (integration testing)

- [x] **Step 1: Run full test suite**

Run: `go test ./... -v -race`
Expected: all tests pass with no race conditions

- [x] **Step 2: Build all binaries**

Run: `make build`
Expected: daemon, CLI, and gendoc all build successfully

- [x] **Step 3: Manual smoke test**

Run: `./bin/meept plans list`
Expected: empty list or plan output (no errors)

Run: `./bin/meept config get plans.mode`
Expected: `threshold`

- [x] **Step 4: Commit any fixes**

```bash
git add -A
git commit -m "fix: integration test fixes for plan system"
```
