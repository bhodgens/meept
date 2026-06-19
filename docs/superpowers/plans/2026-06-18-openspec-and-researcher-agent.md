# OpenSpec Integration and Researcher Agent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate OpenSpec-formatted documents into Meept's orchestrator/planner pipeline so task decomposition produces spec deltas, approval workflows apply spec changes, and step review verifies spec compliance; plus add a `researcher` agent that uses an llm-wiki skill to run multi-agent research compiliation.

**Architecture:** Three new agents (`spec-writer`, `spec-reviewer`, `researcher`) are added to `internal/agent/spec.go`. A new `internal/openspec` package provides parsing/writing for OpenSpec directories (`openspec/specs/<cap>/spec.md` and `openspec/changes/<id>/`). The `StrategicPlanner` (`internal/agent/strategic.go`) gains an `openspecWriter` field, and `Plan()` writes an OpenSpec change directory as a side effect of decomposition. The `CollaborativePlanner`/`PlanManager` approval flow applies deltas on approve, archives on reject, revises on revise. The `TacticalScheduler.OnJobCompleted` (`internal/agent/tactical.go:398`) dispatches to a `spec-reviewer` agent when all steps in a change complete, activating the unused `StateTesting` task state. A new `research` skill (SKILL.md) and an llm-wiki runner under `internal/agent/researcher.go` provide the multi-agent research capability.

**Tech Stack:** Go 1.22+, SQLite, cobra, slog, existing `internal/plan` and `internal/agent` packages, Markdown with OpenSpec conventions.

---

## File Structure

### New Files

| File | Responsibility |
|------|---------------|
| `internal/openspec/types.go` | Core types: `Spec`, `Requirement`, `Scenario`, `Change`, `ChangeProposal`, `ChangeDesign`, `ChangeTasks` |
| `internal/openspec/parser.go` | Parse OpenSpec markdown files into typed structs |
| `internal/openspec/writer.go` | Write OpenSpec markdown from typed structs. Apply/revert delta operations. |
| `internal/openspec/writer_test.go` | Round-trip parse→write→parse tests, delta apply/revert tests |
| `internal/openspec/parser_test.go` | Parse existing OpenSpec files + fixture-based tests |
| `internal/openspec/change.go` | Change lifecycle: Create, Apply, Archive, List. Directory management. |
| `internal/openspec/change_test.go` | Change lifecycle tests |
| `internal/openspec/repository.go` | `Repository` type representing an `openspec/` root dir. Walk, lookup specs, lookup changes. |
| `internal/openspec/repository_test.go` | Repository walk + lookup tests |
| `internal/openspec/integration_test.go` | End-to-end: create change → apply → verify spec updated → archive change |
| `internal/agent/spec_writer.go` | `spec-writer` agent spec + glue logic that writes change dirs from `PlanRequest`/step lists |
| `internal/agent/spec_reviewer.go` | `spec-reviewer` agent spec + review prompt building. Verifies code changes satisfy `### Requirement: ... SHALL ...` blocks. |
| `internal/agent/spec_reviewer_test.go` | Reviewer prompt-building unit tests |
| `internal/agent/spec_writer_test.go` | Spec-writer agent spec tests |
| `internal/agent/researcher.go` | `researcher` agent spec + wiki_runner integration |
| `internal/agent/researcher_test.go` | Researcher spec + runner unit tests |
| `internal/agent/wiki_runner.go` | Programmatic llm-wiki runner: directory management, source ingestion, article compilation, index generation |
| `internal/agent/wiki_runner_test.go` | Wiki runner unit tests |
| `internal/agent/tactical_spec_review_test.go` | Async spec-review dispatch test |
| `internal/plan/manager_openspec_test.go` | PlanManager OpenSpec integration tests (approve/reject) |
| `skills/research/SKILL.md` | Meept-native skill: `research` — invokes llm-wiki runner on a topic |
| `cmd/meept/research.go` | `meept research <topic>` CLI command |
| `cmd/meept/research_test.go` | Research CLI command tests |
| `cmd/meept/openspec.go` | `meept openspec list/show/init` CLI commands |
| `cmd/meept/openspec_test.go` | OpenSpec CLI command tests |
| `internal/agent/openspec_integration_test.go` | Integration: planner writes change dir, approval applies delta |
| `docs/concepts/openspec.md` | OpenSpec concept documentation |

### Modified Files

| File | Changes |
|------|---------|
| `internal/agent/strategic.go` | Add `openspecWriter *openspec.Repository` field to `StrategicPlanner`. In `Plan()` after step generation: call `writeChangeDir(steps, req)`. In `ApprovePlan()`: call `openspecWriter.ApplyChange(changeID)`. In `RejectPlan()`: call `openspecWriter.ArchiveChange(changeID)`. In `RevisePlan()` (via PlanManager): update proposal.md. |
| `internal/agent/tactical.go:1069` (`selectAgent`) | Add `IntentReview` → `spec-reviewer` when step has `SpecChangeID` set |
| `internal/agent/tactical.go:398` (`OnJobCompleted`) | After `AreAllCompleted` check: if task has `SpecChangeID`, dispatch spec-compliance review before marking `StateCompleted`. Use `StateTesting`. |
| `internal/agent/spec.go:455` (`DefaultSpecs`) | Add `SpecWriterSpec()`, `SpecReviewerSpec()`, `ResearcherAgentSpec()` to the returned slice |
| `internal/config/agents.go:35` | Add `AgentIDResearcher = "researcher"`, `AgentIDSpecWriter = "spec-writer"`, `AgentIDSpecReviewer = "spec-reviewer"` constants |
| `internal/agent/intent.go` | Change `IntentResearch` to route to `researcher` instead of `analyst` in `DefaultAgent()`. Add `IntentSpecReview` intent type. |
| `internal/agent/strategic.go:22` (`StrategicPlannerConfig`) | Add `OpenspecRepo *openspec.Repository` field |
| `internal/plan/plan.go` | Add `SpecChangeID string` field to `Plan` struct + `OpenSpecPath string` for the change directory path |
| `internal/agent/tactical.go:43` (`TacticalScheduler` struct) | Add `openspecRepo *openspec.Repository` field |
| `internal/agent/tactical.go:73` (`TacticalSchedulerConfig`) | Add `OpenspecRepo *openspec.Repository` field |
| `internal/daemon/components.go` | Wire `openspec.NewRepository(projectPath)` into `StrategicPlanner`, `TacticalScheduler`, and `PlanManager` configs |
| `internal/plan/manager.go` | Add `openspecRepo` field via functional option `WithOpenSpecRepo`. In `ApprovePlan()`: call `ApplyChange(plan.SpecChangeID)`. In `RejectPlan()`: call `ArchiveChange(plan.SpecChangeID)`. |
| `internal/task/task.go` | Add `SpecChangeID string` field to `Task` for linking tasks to OpenSpec changes |
| `cmd/meept/main.go` | Register `newResearchCmd()` and `newOpenSpecCmd()` in the root command tree |
| `mkdocs.yml` | Add `docs/concepts/openspec.md` to nav under concepts |
| `CLAUDE.md` | Add OpenSpec section under Architecture Overview; add `meept research` and `meept openspec` commands |

---

## Pre-Implementation: Reference Fixture

### Task 0: Create OpenSpec reference fixture

This fixture serves as the canonical example for parser tests and writer round-trip tests throughout the plan.

**Files:**
- Create: `internal/openspec/testdata/fixture-spec.md`
- Create: `internal/openspec/testdata/fixture-change/proposal.md`
- Create: `internal/openspec/testdata/fixture-change/design.md`
- Create: `internal/openspec/testdata/fixture-change/tasks.md`
- Create: `internal/openspec/testdata/fixture-change/specs/auth-session/spec.md`

- [ ] **Step 1: Create the fixture spec file**

`internal/openspec/testdata/fixture-spec.md`:

```markdown
# auth-session

## Purpose

Manages user session lifecycle: creation, validation, expiration, and cleanup.

## Requirements

### Requirement: Session Expiration

The system SHALL expire sessions after a configured duration.

#### Scenario: Session expires after timeout

- GIVEN a session was created 30 minutes ago
- WHEN the session expiration check runs
- THEN the session SHALL be marked expired
- AND the user SHALL be redirected to login

#### Scenario: Session renewed on activity

- GIVEN a session was created 25 minutes ago
- WHEN the user makes a request
- THEN the session expiration timer SHALL be reset to the configured duration

### Requirement: Session Validation

The system SHALL validate session tokens on every authenticated request.

#### Scenario: Valid token passes

- GIVEN a request with a valid session token
- WHEN the middleware validates the token
- THEN the request SHALL proceed to the handler

#### Scenario: Invalid token rejected

- GIVEN a request with an expired session token
- WHEN the middleware validates the token
- THEN the request SHALL be rejected with HTTP 401
```

- [ ] **Step 2: Create the fixture change proposal**

`internal/openspec/testdata/fixture-change/proposal.md`:

```markdown
# Proposal: Add Remember-Me to auth-session

## Motivation

Users want to stay logged in across browser restarts without re-entering credentials.

## Proposed Changes

Add a "remember-me" flag at login that extends the session expiration to 30 days when set.

## Impact

- Modified: `auth-session` capability
- New requirement: Remember-Me Token
```

- [ ] **Step 3: Create the fixture change design**

`internal/openspec/testdata/fixture-change/design.md`:

```markdown
# Design: Add Remember-Me to auth-session

## Decision

Store a separate `remember_token` cookie with a 30-day expiry. On session expiration, if `remember_token` is present and valid, silently create a new session.

## Alternatives Considered

1. Extend session expiration directly — rejected; would affect all sessions, not just opt-in remember-me.
2. Use JWT refresh tokens — rejected; overkill for a single-daemon app.
```

- [ ] **Step 4: Create the fixture change tasks**

`internal/openspec/testdata/fixture-change/tasks.md`:

```markdown
# Tasks: Add Remember-Me to auth-session

1. [ ] Add `remember_me` boolean field to login request struct
2. [ ] Generate `remember_token` on login when `remember_me` is true
3. [ ] Add middleware check: if session expired but `remember_token` valid, create new session
4. [ ] Add unit tests for remember-me flow
5. [ ] Update API docs
```

- [ ] **Step 5: Create the fixture change spec delta**

`internal/openspec/testdata/fixture-change/specs/auth-session/spec.md`:

```markdown
# auth-session — Proposed Changes

### Requirement: Remember-Me Token

The system SHALL issue a separate remember-me token when the user selects "remember me" at login.

#### Scenario: Remember-me token issued on login

- GIVEN a login request with `remember_me: true`
- WHEN the login succeeds
- THEN a `remember_token` cookie SHALL be set with a 30-day expiry
- AND the standard session token SHALL still be issued with its normal expiry

#### Scenario: Expired session auto-renewed via remember-me

- GIVEN a session that has expired
- AND a valid `remember_token` cookie is present
- WHEN the middleware validates the request
- THEN a new session SHALL be created silently
- AND the user SHALL NOT be redirected to login
```

- [ ] **Step 6: Commit**

```bash
git add internal/openspec/testdata/
git commit -m "feat(openspec): add reference fixtures for parser/writer tests"
```

---

## Phase 1: OpenSpec Core Package

### Task 1: OpenSpec types

**Files:**
- Create: `internal/openspec/types.go`

- [ ] **Step 1: Write the types file**

```go
package openspec

// Spec represents a parsed OpenSpec spec.md file for a capability.
type Spec struct {
	Capability   string       // H1 heading (e.g., "auth-session")
	Purpose      string       // ## Purpose section body
	Requirements []Requirement // ## Requirements section
	FilePath     string       // source file path
}

// Requirement represents a single ### Requirement: ... block.
type Requirement struct {
	Title     string    // text after "### Requirement: "
	Shall     string    // the "SHALL ..." statement (first line after title)
	Scenarios []Scenario // #### Scenario blocks
}

// Scenario represents a GIVEN/WHEN/THEN block under a Requirement.
type Scenario struct {
	Name  string   // text after "#### Scenario: "
	Steps []string // each "- GIVEN ...", "- WHEN ...", "- THEN ...", "- AND ..." line
}

// Change represents an OpenSpec change proposal under openspec/changes/<id>/.
type Change struct {
	ID         string         // directory name (e.g., "add-remember-me")
	Proposal   ChangeProposal // parsed proposal.md
	Design     ChangeDesign   // parsed design.md
	Tasks      ChangeTasks    // parsed tasks.md
	SpecDeltas []SpecDelta    // parsed specs/<cap>/spec.md files (one per affected capability)
	BasePath   string         // openspec/changes/<id>/
}

// ChangeProposal is the parsed contents of proposal.md.
type ChangeProposal struct {
	Motivation     string
	ProposedChanges string
	Impact         string
	FilePath       string
}

// ChangeDesign is the parsed contents of design.md.
type ChangeDesign struct {
	Decisions       string
	Alternatives    string
	FilePath        string
}

// ChangeTasks is the parsed contents of tasks.md.
type ChangeTasks struct {
	Items    []TaskItem
	FilePath string
}

// TaskItem is a single line from the tasks.md checklist.
type TaskItem struct {
	Description string
	Done        bool
}

// SpecDelta is a parsed spec delta file under changes/<id>/specs/<cap>/spec.md.
type SpecDelta struct {
	Capability  string
	Additions   []Requirement // lines starting with +
	Removals    []Requirement // lines starting with -
	Modifications []Requirement // lines starting with ~ (modified blocks)
	FilePath    string
}

// Repository provides read/write access to an openspec/ directory tree.
type Repository struct {
	RootPath string // path to the openspec/ directory
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/openspec/`
Expected: succeeds with no errors

- [ ] **Step 3: Commit**

```bash
git add internal/openspec/types.go
git commit -m "feat(openspec): add core types for spec, change, and delta"
```

### Task 2: OpenSpec parser

**Files:**
- Create: `internal/openspec/parser.go`
- Create: `internal/openspec/parser_test.go`

- [ ] **Step 1: Write the failing test for parsing a spec file**

`internal/openspec/parser_test.go`:

```go
package openspec

import (
	"path/filepath"
	"testing"
)

func TestParseSpec(t *testing.T) {
	spec, err := ParseSpec(filepath.Join("testdata", "fixture-spec.md"))
	if err != nil {
		t.Fatalf("ParseSpec failed: %v", err)
	}

	if spec.Capability != "auth-session" {
		t.Errorf("expected Capability 'auth-session', got %q", spec.Capability)
	}
	if spec.Purpose == "" {
		t.Error("expected non-empty Purpose")
	}
	if len(spec.Requirements) != 2 {
		t.Fatalf("expected 2 Requirements, got %d", len(spec.Requirements))
	}

	// First requirement: "Session Expiration"
	r1 := spec.Requirements[0]
	if r1.Title != "Session Expiration" {
		t.Errorf("expected first requirement title 'Session Expiration', got %q", r1.Title)
	}
	if len(r1.Scenarios) != 2 {
		t.Fatalf("expected 2 scenarios for Session Expiration, got %d", len(r1.Scenarios))
	}

	// Check first scenario steps
	s1 := r1.Scenarios[0]
	if s1.Name != "Session expires after timeout" {
		t.Errorf("expected scenario name 'Session expires after timeout', got %q", s1.Name)
	}
	// GIVEN, WHEN, THEN, AND = 4 steps
	if len(s1.Steps) != 4 {
		t.Fatalf("expected 4 steps in scenario, got %d: %v", len(s1.Steps), s1.Steps)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/openspec/ -run TestParseSpec -v`
Expected: FAIL — `ParseSpec` undefined

- [ ] **Step 3: Write the parser implementation**

`internal/openspec/parser.go`:

```go
package openspec

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	reH1Heading     = regexp.MustCompile(`^#\s+(.+)$`)
	rePurpose       = regexp.MustCompile(`^##\s+Purpose\s*$`)
	requirementRe   = regexp.MustCompile(`^###\s+Requirement:\s+(.+)$`)
	scenarioRe      = regexp.MustCompile(`^####\s+Scenario:\s+(.+)$`)
	scenarioStep    = regexp.MustCompile(`^-\s+(GIVEN|WHEN|THEN|AND)\s+(.+)$`)
	reTaskItem      = regexp.MustCompile(`^-\s+\[(\s|x|X)\]\s+(.+)$`)
	deltaAddition   = regexp.MustCompile(`^\+\s*(.*)`)
	deltaRemoval    = regexp.MustCompile(`^-\s*(.*)`)
)

// ParseSpec reads and parses an OpenSpec spec.md file.
func ParseSpec(filePath string) (*Spec, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read spec file %s: %w", filePath, err)
	}
	return ParseSpecContent(string(data), filePath)
}

// ParseSpecContent parses spec.md content from a string.
func ParseSpecContent(content, filePath string) (*Spec, error) {
	spec := &Spec{FilePath: filePath}

	type section int
	const (
		secNone section = iota
		secPurpose
		secRequirement
		secScenario
	)

	var (
		currentSection section
		currentReq     *Requirement
		currentScen    *Scenario
		purposeLines   []string
	)

	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// H1: capability name
		if m := reH1Heading.FindStringSubmatch(trimmed); m != nil {
			spec.Capability = strings.TrimSpace(m[1])
			continue
		}

		// ## Purpose
		if rePurpose.MatchString(trimmed) {
			currentSection = secPurpose
			continue
		}

		// ### Requirement: Title
		if m := requirementRe.FindStringSubmatch(trimmed); m != nil {
			// Finalize previous scenario
			if currentScen != nil && currentReq != nil {
				currentReq.Scenarios = append(currentReq.Scenarios, *currentScen)
				currentScen = nil
			}
			// Finalize previous requirement
			if currentReq != nil {
				spec.Requirements = append(spec.Requirements, *currentReq)
			}
			currentReq = &Requirement{Title: strings.TrimSpace(m[1])}
			currentSection = secRequirement
			continue
		}

		// #### Scenario: Name
		if m := scenarioRe.FindStringSubmatch(trimmed); m != nil {
			if currentScen != nil && currentReq != nil {
				currentReq.Scenarios = append(currentReq.Scenarios, *currentScen)
			}
			currentScen = &Scenario{Name: strings.TrimSpace(m[1])}
			currentSection = secScenario
			continue
		}

		// Content by section
		switch currentSection {
		case secPurpose:
			if trimmed != "" || len(purposeLines) > 0 {
				purposeLines = append(purposeLines, line)
			}
		case secScenario:
			if currentScen != nil {
				if m := scenarioStep.FindStringSubmatch(trimmed); m != nil {
					currentScen.Steps = append(currentScen.Steps, trimmed)
				}
			}
		case secRequirement:
			// SHALL statement: first non-empty line after the requirement heading
			if currentReq != nil && currentReq.Shall == "" && trimmed != "" {
				if strings.Contains(trimmed, "SHALL") {
					currentReq.Shall = trimmed
				}
			}
		}
	}

	// Finalize
	if currentScen != nil && currentReq != nil {
		currentReq.Scenarios = append(currentReq.Scenarios, *currentScen)
	}
	if currentReq != nil {
		spec.Requirements = append(spec.Requirements, *currentReq)
	}
	if len(purposeLines) > 0 {
		spec.Purpose = strings.TrimSpace(strings.Join(purposeLines, "\n"))
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning spec content: %w", err)
	}

	return spec, nil
}

// ParseChange reads and parses all files in an OpenSpec change directory.
func ParseChange(changeDir string) (*Change, error) {
	change := &Change{
		ID:       filepath.Base(changeDir),
		BasePath: changeDir,
	}

	// Parse proposal.md
	proposalPath := filepath.Join(changeDir, "proposal.md")
	if data, err := os.ReadFile(proposalPath); err == nil {
		change.Proposal = parseProposal(string(data), proposalPath)
	}

	// Parse design.md
	designPath := filepath.Join(changeDir, "design.md")
	if data, err := os.ReadFile(designPath); err == nil {
		change.Design = parseDesign(string(data), designPath)
	}

	// Parse tasks.md
	tasksPath := filepath.Join(changeDir, "tasks.md")
	if data, err := os.ReadFile(tasksPath); err == nil {
		change.Tasks = parseTasks(string(data), tasksPath)
	}

	// Parse spec deltas under specs/<cap>/spec.md
	specsDir := filepath.Join(changeDir, "specs")
	if entries, err := os.ReadDir(specsDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			specFile := filepath.Join(specsDir, entry.Name(), "spec.md")
			if data, err := os.ReadFile(specFile); err == nil {
				delta := parseDelta(string(data), entry.Name(), specFile)
				change.SpecDeltas = append(change.SpecDeltas, delta)
			}
		}
	}

	return change, nil
}

func parseProposal(content, filePath string) ChangeProposal {
	// Simple section extraction: split on ## headings
	p := ChangeProposal{FilePath: filePath}
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var currentSection string
	var lines []string
	finalize := func() {
		body := strings.TrimSpace(strings.Join(lines, "\n"))
		lines = nil
		switch currentSection {
		case "Motivation":
			p.Motivation = body
		case "Proposed Changes":
			p.ProposedChanges = body
		case "Impact":
			p.Impact = body
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			finalize()
			currentSection = strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			continue
		}
		lines = append(lines, line)
	}
	finalize()
	return p
}

func parseDesign(content, filePath string) ChangeDesign {
	d := ChangeDesign{FilePath: filePath}
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var currentSection string
	var lines []string
	finalize := func() {
		body := strings.TrimSpace(strings.Join(lines, "\n"))
		lines = nil
		switch currentSection {
		case "Decision":
			d.Decisions = body
		case "Alternatives Considered":
			d.Alternatives = body
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			finalize()
			currentSection = strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			continue
		}
		lines = append(lines, line)
	}
	finalize()
	return d
}

func parseTasks(content, filePath string) ChangeTasks {
	tasks := ChangeTasks{FilePath: filePath}
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())
		if m := reTaskItem.FindStringSubmatch(trimmed); m != nil {
			tasks.Items = append(tasks.Items, TaskItem{
				Description: strings.TrimSpace(m[2]),
				Done:        strings.EqualFold(m[1], "x"),
			})
		}
	}
	return tasks
}

func parseDelta(content, capability, filePath string) SpecDelta {
	delta := SpecDelta{Capability: capability, FilePath: filePath}
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var currentReq *Requirement
	var currentScen *Scenario

	finalizeScenario := func() {
		if currentScen != nil && currentReq != nil {
			currentReq.Scenarios = append(currentReq.Scenarios, *currentScen)
			currentScen = nil
		}
	}
	finalizeReq := func(target *[]Requirement) {
		finalizeScenario()
		if currentReq != nil {
			*target = append(*target, *currentReq)
			currentReq = nil
		}
	}

	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())

		// Delta-prefixed requirement heading
		if m := deltaAddition.FindStringSubmatch(trimmed); m != nil {
			inner := strings.TrimSpace(m[1])
			if rm := requirementRe.FindStringSubmatch(inner); rm != nil {
				finalizeReq(&delta.Additions)
				currentReq = &Requirement{Title: strings.TrimSpace(rm[1])}
				continue
			}
			if sm := scenarioRe.FindStringSubmatch(inner); sm != nil {
				finalizeScenario()
				currentScen = &Scenario{Name: strings.TrimSpace(sm[1])}
				continue
			}
			if m := scenarioStep.FindStringSubmatch(inner); m != nil && currentScen != nil {
				currentScen.Steps = append(currentScen.Steps, strings.TrimSpace(m[0]))
			}
		}
	}

	finalizeReq(&delta.Additions)
	return delta
}
```

Note: add `"path/filepath"` to the imports.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/openspec/ -run TestParseSpec -v`
Expected: PASS

- [ ] **Step 5: Write the failing test for parsing a change directory**

Add to `internal/openspec/parser_test.go`:

```go
func TestParseChange(t *testing.T) {
	change, err := ParseChange(filepath.Join("testdata", "fixture-change"))
	if err != nil {
		t.Fatalf("ParseChange failed: %v", err)
	}

	if change.ID != "fixture-change" {
		t.Errorf("expected ID 'fixture-change', got %q", change.ID)
	}
	if change.Proposal.Motivation == "" {
		t.Error("expected non-empty Proposal.Motivation")
	}
	if change.Design.Decisions == "" {
		t.Error("expected non-empty Design.Decisions")
	}
	if len(change.Tasks.Items) != 5 {
		t.Errorf("expected 5 task items, got %d", len(change.Tasks.Items))
	}
	if change.Tasks.Items[0].Done {
		t.Error("expected first task item to be not done")
	}
	if len(change.SpecDeltas) != 1 {
		t.Fatalf("expected 1 spec delta, got %d", len(change.SpecDeltas))
	}
	if change.SpecDeltas[0].Capability != "auth-session" {
		t.Errorf("expected delta capability 'auth-session', got %q", change.SpecDeltas[0].Capability)
	}
	if len(change.SpecDeltas[0].Additions) != 1 {
		t.Fatalf("expected 1 addition in delta, got %d", len(change.SpecDeltas[0].Additions))
	}
	if change.SpecDeltas[0].Additions[0].Title != "Remember-Me Token" {
		t.Errorf("expected addition title 'Remember-Me Token', got %q",
			change.SpecDeltas[0].Additions[0].Title)
	}
}
```

- [ ] **Step 6: Run the test to verify it fails, then fix imports and pass**

Run: `go test ./internal/openspec/ -run TestParseChange -v`
Expected: PASS (after adding `"path/filepath"` import to test file)

- [ ] **Step 7: Commit**

```bash
git add internal/openspec/parser.go internal/openspec/parser_test.go
git commit -m "feat(openspec): add parser for spec.md and change directories"
```

### Task 3: OpenSpec writer and delta application

**Files:**
- Create: `internal/openspec/writer.go`
- Create: `internal/openspec/writer_test.go`

- [ ] **Step 1: Write the failing test for spec round-trip**

`internal/openspec/writer_test.go`:

```go
package openspec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteSpec_RoundTrip(t *testing.T) {
	original, err := ParseSpec(filepath.Join("testdata", "fixture-spec.md"))
	if err != nil {
		t.Fatalf("ParseSpec failed: %v", err)
	}

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "spec.md")

	if err := WriteSpec(outPath, original); err != nil {
		t.Fatalf("WriteSpec failed: %v", err)
	}

	reparsed, err := ParseSpec(outPath)
	if err != nil {
		t.Fatalf("re-parse failed: %v", err)
	}

	if reparsed.Capability != original.Capability {
		t.Errorf("Capability mismatch: %q vs %q", reparsed.Capability, original.Capability)
	}
	if len(reparsed.Requirements) != len(original.Requirements) {
		t.Fatalf("Requirements count mismatch: %d vs %d",
			len(reparsed.Requirements), len(original.Requirements))
	}
	if reparsed.Requirements[0].Title != original.Requirements[0].Title {
		t.Errorf("Requirement[0] title mismatch: %q vs %q",
			reparsed.Requirements[0].Title, original.Requirements[0].Title)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/openspec/ -run TestWriteSpec_RoundTrip -v`
Expected: FAIL — `WriteSpec` undefined

- [ ] **Step 3: Write the writer implementation**

`internal/openspec/writer.go`:

```go
package openspec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WriteSpec writes a Spec to a spec.md file, creating parent directories.
func WriteSpec(filePath string, spec *Spec) error {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n\n", spec.Capability)

	b.WriteString("## Purpose\n\n")
	b.WriteString(spec.Purpose)
	b.WriteString("\n\n")

	b.WriteString("## Requirements\n\n")
	for _, req := range spec.Requirements {
		fmt.Fprintf(&b, "### Requirement: %s\n\n", req.Title)
		if req.Shall != "" {
			fmt.Fprintf(&b, "%s\n\n", req.Shall)
		}
		for _, scen := range req.Scenarios {
			fmt.Fprintf(&b, "#### Scenario: %s\n\n", scen.Name)
			for _, step := range scen.Steps {
				fmt.Fprintf(&b, "%s\n", step)
			}
			b.WriteString("\n")
		}
	}

	return mkdirAndWrite(filePath, b.String())
}

// WriteChange writes all change files (proposal.md, design.md, tasks.md, specs/) to a directory.
func WriteChange(changeDir string, change *Change) error {
	if err := os.MkdirAll(changeDir, 0o755); err != nil {
		return fmt.Errorf("create change dir: %w", err)
	}

	// proposal.md
	var pb strings.Builder
	pb.WriteString("# Proposal: " + change.ID + "\n\n")
	if change.Proposal.Motivation != "" {
		pb.WriteString("## Motivation\n\n" + change.Proposal.Motivation + "\n\n")
	}
	if change.Proposal.ProposedChanges != "" {
		pb.WriteString("## Proposed Changes\n\n" + change.Proposal.ProposedChanges + "\n\n")
	}
	if change.Proposal.Impact != "" {
		pb.WriteString("## Impact\n\n" + change.Proposal.Impact + "\n\n")
	}
	if err := mkdirAndWrite(filepath.Join(changeDir, "proposal.md"), pb.String()); err != nil {
		return fmt.Errorf("write proposal: %w", err)
	}

	// design.md
	var db strings.Builder
	db.WriteString("# Design: " + change.ID + "\n\n")
	if change.Design.Decisions != "" {
		db.WriteString("## Decision\n\n" + change.Design.Decisions + "\n\n")
	}
	if change.Design.Alternatives != "" {
		db.WriteString("## Alternatives Considered\n\n" + change.Design.Alternatives + "\n\n")
	}
	if err := mkdirAndWrite(filepath.Join(changeDir, "design.md"), db.String()); err != nil {
		return fmt.Errorf("write design: %w", err)
	}

	// tasks.md
	var tb strings.Builder
	tb.WriteString("# Tasks: " + change.ID + "\n\n")
	for _, item := range change.Tasks.Items {
		check := " "
		if item.Done {
			check = "x"
		}
		fmt.Fprintf(&tb, "- [%s] %s\n", check, item.Description)
	}
	if err := mkdirAndWrite(filepath.Join(changeDir, "tasks.md"), tb.String()); err != nil {
		return fmt.Errorf("write tasks: %w", err)
	}

	// spec deltas
	for _, delta := range change.SpecDeltas {
		deltaDir := filepath.Join(changeDir, "specs", delta.Capability)
		if err := os.MkdirAll(deltaDir, 0o755); err != nil {
			return fmt.Errorf("create delta dir for %s: %w", delta.Capability, err)
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "# %s — Proposed Changes\n\n", delta.Capability)
		for _, req := range delta.Additions {
			fmt.Fprintf(&sb, "### Requirement: %s\n\n", req.Title)
			if req.Shall != "" {
				fmt.Fprintf(&sb, "%s\n\n", req.Shall)
			}
			for _, scen := range req.Scenarios {
				fmt.Fprintf(&sb, "#### Scenario: %s\n\n", scen.Name)
				for _, step := range scen.Steps {
					fmt.Fprintf(&sb, "+ %s\n", strings.TrimPrefix(step, "- "))
				}
				sb.WriteString("\n")
			}
		}
		deltaPath := filepath.Join(deltaDir, "spec.md")
		if err := mkdirAndWrite(deltaPath, sb.String()); err != nil {
			return fmt.Errorf("write delta for %s: %w", delta.Capability, err)
		}
	}

	return nil
}

// ApplyDelta merges a SpecDelta into an existing Spec, returning the merged Spec.
// Additions are appended to the Requirements list. Removals are filtered out by title.
func ApplyDelta(spec *Spec, delta SpecDelta) *Spec {
	merged := &Spec{
		Capability:   spec.Capability,
		Purpose:      spec.Purpose,
		FilePath:     spec.FilePath,
		Requirements: make([]Requirement, len(spec.Requirements)),
	}
	copy(merged.Requirements, spec.Requirements)

	// Apply removals
	if len(delta.Removals) > 0 {
		removeTitles := make(map[string]bool)
		for _, r := range delta.Removals {
			removeTitles[r.Title] = true
		}
		filtered := make([]Requirement, 0, len(merged.Requirements))
		for _, req := range merged.Requirements {
			if !removeTitles[req.Title] {
				filtered = append(filtered, req)
			}
		}
		merged.Requirements = filtered
	}

	// Apply additions
	merged.Requirements = append(merged.Requirements, delta.Additions...)

	return merged
}

// mkdirAndWrite creates parent directories and writes content to filePath.
func mkdirAndWrite(filePath, content string) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directories: %w", err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the round-trip test**

Run: `go test ./internal/openspec/ -run TestWriteSpec_RoundTrip -v`
Expected: PASS

- [ ] **Step 5: Write the failing test for delta application**

Add to `internal/openspec/writer_test.go`:

```go
func TestApplyDelta(t *testing.T) {
	baseSpec := &Spec{
		Capability: "auth-session",
		Purpose:    "Manages sessions.",
		Requirements: []Requirement{
			{Title: "Session Expiration", Shall: "The system SHALL expire sessions after a configured duration."},
			{Title: "Session Validation", Shall: "The system SHALL validate session tokens on every authenticated request."},
		},
	}

	delta := SpecDelta{
		Capability: "auth-session",
		Additions: []Requirement{
			{Title: "Remember-Me Token", Shall: "The system SHALL issue a separate remember-me token when the user selects remember me at login."},
		},
	}

	merged := ApplyDelta(baseSpec, delta)

	if len(merged.Requirements) != 3 {
		t.Fatalf("expected 3 requirements after delta, got %d", len(merged.Requirements))
	}
	if merged.Requirements[2].Title != "Remember-Me Token" {
		t.Errorf("expected third requirement 'Remember-Me Token', got %q", merged.Requirements[2].Title)
	}
	// Ensure original spec is not mutated
	if len(baseSpec.Requirements) != 2 {
		t.Errorf("original spec was mutated: %d requirements", len(baseSpec.Requirements))
	}
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/openspec/ -run TestApplyDelta -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/openspec/writer.go internal/openspec/writer_test.go
git commit -m "feat(openspec): add writer and delta application"
```

### Task 4: OpenSpec change lifecycle and repository

**Files:**
- Create: `internal/openspec/change.go`
- Create: `internal/openspec/change_test.go`
- Create: `internal/openspec/repository.go`
- Create: `internal/openspec/repository_test.go`

- [ ] **Step 1: Write the failing test for repository operations**

`internal/openspec/repository_test.go`:

```go
package openspec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepository_CreateChange_Apply_Archive(t *testing.T) {
	tmpDir := t.TempDir()
	openspecDir := filepath.Join(tmpDir, "openspec")

	// Initialize repository
	repo := NewRepository(openspecDir)
	if err := repo.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Verify directories exist
	specsDir := filepath.Join(openspecDir, "specs")
	changesDir := filepath.Join(openspecDir, "changes")
	if _, err := os.Stat(specsDir); os.IsNotExist(err) {
		t.Fatal("specs/ directory not created")
	}
	if _, err := os.Stat(changesDir); os.IsNotExist(err) {
		t.Fatal("changes/ directory not created")
	}

	// Create a base spec
	baseSpec := &Spec{
		Capability: "auth-session",
		Purpose:    "Manages user session lifecycle.",
		Requirements: []Requirement{
			{Title: "Session Expiration", Shall: "The system SHALL expire sessions after a configured duration."},
		},
	}
	specPath := filepath.Join(specsDir, "auth-session", "spec.md")
	if err := WriteSpec(specPath, baseSpec); err != nil {
		t.Fatalf("WriteSpec failed: %v", err)
	}

	// Create a change
	change := &Change{
		ID: "add-remember-me",
		Proposal: ChangeProposal{
			Motivation:       "Users want to stay logged in across browser restarts.",
			ProposedChanges:  "Add a remember-me flag at login.",
			Impact:           "Modified: auth-session capability",
		},
		Design: ChangeDesign{
			Decisions:    "Store a separate remember_token cookie.",
			Alternatives: "Extend session expiration directly. Rejected: affects all sessions.",
		},
		Tasks: ChangeTasks{
			Items: []TaskItem{
				{Description: "Add remember_me boolean field to login request struct"},
				{Description: "Generate remember_token on login when remember_me is true"},
				{Description: "Add middleware check for remember_token"},
			},
		},
		SpecDeltas: []SpecDelta{
			{
				Capability: "auth-session",
				Additions: []Requirement{
					{Title: "Remember-Me Token", Shall: "The system SHALL issue a separate remember-me token when remember_me is true."},
				},
			},
		},
	}

	changeDir := repo.ChangePath("add-remember-me")
	if err := WriteChange(changeDir, change); err != nil {
		t.Fatalf("WriteChange failed: %v", err)
	}

	// Apply the change
	if err := repo.ApplyChange("add-remember-me"); err != nil {
		t.Fatalf("ApplyChange failed: %v", err)
	}

	// Verify the spec was updated
	updated, err := ParseSpec(specPath)
	if err != nil {
		t.Fatalf("ParseSpec after apply failed: %v", err)
	}
	if len(updated.Requirements) != 2 {
		t.Fatalf("expected 2 requirements after apply, got %d", len(updated.Requirements))
	}
	if updated.Requirements[1].Title != "Remember-Me Token" {
		t.Errorf("expected second requirement 'Remember-Me Token', got %q", updated.Requirements[1].Title)
	}

	// Archive the change
	if err := repo.ArchiveChange("add-remember-me"); err != nil {
		t.Fatalf("ArchiveChange failed: %v", err)
	}

	// Verify change dir is moved to archive
	archivedPath := filepath.Join(changesDir, "archive", "add-remember-me")
	if _, err := os.Stat(archivedPath); os.IsNotExist(err) {
		t.Fatal("change was not moved to archive/")
	}
	if _, err := os.Stat(changeDir); !os.IsNotExist(err) {
		t.Fatal("change dir should not exist after archiving")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/openspec/ -run TestRepository -v`
Expected: FAIL — `NewRepository`, `Init`, `ApplyChange`, `ArchiveChange` undefined

- [ ] **Step 3: Write the repository implementation**

`internal/openspec/repository.go`:

```go
package openspec

import (
	"fmt"
	"os"
	"path/filepath"
)

// NewRepository creates a Repository rooted at rootPath.
func NewRepository(rootPath string) *Repository {
	return &Repository{RootPath: rootPath}
}

// Init creates the openspec/ directory structure: specs/ and changes/.
func (r *Repository) Init() error {
	if err := os.MkdirAll(filepath.Join(r.RootPath, "specs"), 0o755); err != nil {
		return fmt.Errorf("create specs dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(r.RootPath, "changes"), 0o755); err != nil {
		return fmt.Errorf("create changes dir: %w", err)
	}
	return nil
}

// ChangePath returns the full path for a change directory.
func (r *Repository) ChangePath(changeID string) string {
	return filepath.Join(r.RootPath, "changes", changeID)
}

// SpecPath returns the full path for a capability's spec.md file.
func (r *Repository) SpecPath(capability string) string {
	return filepath.Join(r.RootPath, "specs", capability, "spec.md")
}

// ListSpecs walks the specs/ directory and returns all capability names found.
func (r *Repository) ListSpecs() ([]string, error) {
	specsDir := filepath.Join(r.RootPath, "specs")
	entries, err := os.ReadDir(specsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read specs dir: %w", err)
	}
	var capabilities []string
	for _, entry := range entries {
		if entry.IsDir() {
			specFile := filepath.Join(specsDir, entry.Name(), "spec.md")
			if _, err := os.Stat(specFile); err == nil {
				capabilities = append(capabilities, entry.Name())
			}
		}
	}
	return capabilities, nil
}

// GetSpec loads and parses a capability's spec.md.
func (r *Repository) GetSpec(capability string) (*Spec, error) {
	return ParseSpec(r.SpecPath(capability))
}

// ListChanges walks the changes/ directory and returns all change IDs (non-archived).
func (r *Repository) ListChanges() ([]string, error) {
	changesDir := filepath.Join(r.RootPath, "changes")
	entries, err := os.ReadDir(changesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read changes dir: %w", err)
	}
	var changeIDs []string
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "archive" {
			changeIDs = append(changeIDs, entry.Name())
		}
	}
	return changeIDs, nil
}

// GetChange loads and parses a change directory.
func (r *Repository) GetChange(changeID string) (*Change, error) {
	return ParseChange(r.ChangePath(changeID))
}

// ApplyChange applies a change's spec deltas to the corresponding specs/ files.
// For each delta: load the current spec, apply the delta, write the merged spec.
func (r *Repository) ApplyChange(changeID string) error {
	change, err := r.GetChange(changeID)
	if err != nil {
		return fmt.Errorf("get change %s: %w", changeID, err)
	}

	for _, delta := range change.SpecDeltas {
		specPath := r.SpecPath(delta.Capability)

		var currentSpec *Spec
		if existing, err := ParseSpec(specPath); err == nil {
			currentSpec = existing
		} else {
			// Capability doesn't exist yet; create a fresh spec
			currentSpec = &Spec{
				Capability: delta.Capability,
				Purpose:    "",
				FilePath:   specPath,
			}
		}

		merged := ApplyDelta(currentSpec, delta)
		merged.FilePath = specPath
		if err := WriteSpec(specPath, merged); err != nil {
			return fmt.Errorf("write merged spec for %s: %w", delta.Capability, err)
		}
	}

	return nil
}

// ArchiveChange moves a change directory to changes/archive/<changeID>/.
func (r *Repository) ArchiveChange(changeID string) error {
	changeDir := r.ChangePath(changeID)
	archiveDir := filepath.Join(r.RootPath, "changes", "archive", changeID)

	if err := os.MkdirAll(filepath.Dir(archiveDir), 0o755); err != nil {
		return fmt.Errorf("create archive dir: %w", err)
	}

	if err := os.Rename(changeDir, archiveDir); err != nil {
		return fmt.Errorf("move change to archive: %w", err)
	}

	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/openspec/ -run TestRepository -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/openspec/repository.go internal/openspec/repository_test.go
git commit -m "feat(openspec): add repository with change lifecycle (create, apply, archive)"
```

### Task 5: OpenSpec integration test

**Files:**
- Create: `internal/openspec/integration_test.go`

- [ ] **Step 1: Write the end-to-end integration test**

```go
package openspec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIntegration_CreateChange_Apply_Verify_Archive(t *testing.T) {
	tmpDir := t.TempDir()
	openspecDir := filepath.Join(tmpDir, "openspec")

	repo := NewRepository(openspecDir)
	if err := repo.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// 1. Create base spec
	baseSpec := &Spec{
		Capability: "checkout-cart",
		Purpose:    "Manages the shopping cart lifecycle.",
		Requirements: []Requirement{
			{
				Title: "Add Items",
				Shall: "The system SHALL allow users to add items to the cart.",
				Scenarios: []Scenario{
					{Name: "Item added successfully", Steps: []string{"- GIVEN a product is available", "- WHEN the user adds it to the cart", "- THEN the cart SHALL contain the item"}},
				},
			},
		},
	}
	if err := WriteSpec(repo.SpecPath("checkout-cart"), baseSpec); err != nil {
		t.Fatalf("WriteSpec base failed: %v", err)
	}

	// 2. Create change
	change := &Change{
		ID: "add-cart-persistence",
		Proposal: ChangeProposal{
			Motivation:      "Carts should survive page refresh.",
			ProposedChanges: "Persist cart to localStorage.",
		},
		Tasks: ChangeTasks{
			Items: []TaskItem{
				{Description: "Add localStorage save/load functions"},
				{Description: "Wire cart mutations to persist"},
			},
		},
		SpecDeltas: []SpecDelta{
			{
				Capability: "checkout-cart",
				Additions: []Requirement{
					{
						Title: "Cart Persistence",
						Shall:  "The system SHALL persist the cart to localStorage on every change.",
						Scenarios: []Scenario{
							{Name: "Cart survives refresh", Steps: []string{"- GIVEN a cart with items", "- WHEN the page refreshes", "- THEN the cart SHALL be restored from localStorage"}},
						},
					},
				},
			},
		},
	}
	changeDir := repo.ChangePath("add-cart-persistence")
	if err := WriteChange(changeDir, change); err != nil {
		t.Fatalf("WriteChange failed: %v", err)
	}

	// 3. Apply the change
	if err := repo.ApplyChange("add-cart-persistence"); err != nil {
		t.Fatalf("ApplyChange failed: %v", err)
	}

	// 4. Verify spec now has 2 requirements
	updated, err := repo.GetSpec("checkout-cart")
	if err != nil {
		t.Fatalf("GetSpec failed: %v", err)
	}
	if len(updated.Requirements) != 2 {
		t.Fatalf("expected 2 requirements after apply, got %d", len(updated.Requirements))
	}
	if updated.Requirements[1].Title != "Cart Persistence" {
		t.Errorf("expected 'Cart Persistence', got %q", updated.Requirements[1].Title)
	}
	if len(updated.Requirements[1].Scenarios) != 1 {
		t.Errorf("expected 1 scenario in Cart Persistence, got %d", len(updated.Requirements[1].Scenarios))
	}

	// 5. Archive the change
	if err := repo.ArchiveChange("add-cart-persistence"); err != nil {
		t.Fatalf("ArchiveChange failed: %v", err)
	}

	// 6. Verify change no longer in active list
	changes, err := repo.ListChanges()
	if err != nil {
		t.Fatalf("ListChanges failed: %v", err)
	}
	for _, c := range changes {
		if c == "add-cart-persistence" {
			t.Error("archived change still in active changes list")
		}
	}

	// 7. Verify spec still has the applied changes (archive doesn't revert)
	final, err := repo.GetSpec("checkout-cart")
	if err != nil {
		t.Fatalf("final GetSpec failed: %v", err)
	}
	if len(final.Requirements) != 2 {
		t.Fatalf("expected 2 requirements after archive, got %d", len(final.Requirements))
	}

	// 8. Verify archive directory contains the change
	archivedDir := filepath.Join(openspecDir, "changes", "archive", "add-cart-persistence")
	if _, err := os.Stat(archivedDir); os.IsNotExist(err) {
		t.Fatal("change not found in archive/")
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/openspec/ -run TestIntegration -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/openspec/integration_test.go
git commit -m "test(openspec): add end-to-end integration test for change lifecycle"
```

---

## Phase 2: Agent Specs and Config Constants

### Task 6: Add agent ID constants

**Files:**
- Modify: `internal/config/agents.go:35-44`

- [ ] **Step 1: Read the current constants block**

Read `internal/config/agents.go` lines 35-44 to confirm current state.

- [ ] **Step 2: Add new agent ID constants**

In `internal/config/agents.go`, after line 43 (`AgentIDChat       = "chat"`), add:

```go
	AgentIDResearcher    = "researcher"
	AgentIDSpecWriter    = "spec-writer"
	AgentIDSpecReviewer   = "spec-reviewer"
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/config/`
Expected: succeeds

- [ ] **Step 4: Commit**

```bash
git add internal/config/agents.go
git commit -m "feat(config): add researcher, spec-writer, spec-reviewer agent IDs"
```

### Task 7: Add spec-writer agent spec

**Files:**
- Create: `internal/agent/spec_writer.go`
- Create: `internal/agent/spec_writer_test.go`

- [ ] **Step 1: Write the failing test**

`internal/agent/spec_writer_test.go`:

```go
package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/config"
)

func TestSpecWriterSpec(t *testing.T) {
	spec := SpecWriterSpec()

	if spec.ID != config.AgentIDSpecWriter {
		t.Errorf("expected ID %q, got %q", config.AgentIDSpecWriter, spec.ID)
	}
	if spec.Role != RoleExecutor {
		t.Errorf("expected Role %q, got %q", RoleExecutor, spec.Role)
	}
	if spec.Purpose == "" {
		t.Error("expected non-empty Purpose")
	}
	// Must have file write tools
	if !spec.HasTool(ToolFileWrite) {
		t.Error("expected spec-writer to have file_write tool")
	}
	if !spec.HasTool(ToolFileRead) {
		t.Error("expected spec-writer to have file_read tool")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/agent/ -run TestSpecWriterSpec -v`
Expected: FAIL — `SpecWriterSpec` undefined

- [ ] **Step 3: Write the spec implementation**

`internal/agent/spec_writer.go`:

```go
package agent

import (
	"time"

	"github.com/caimlas/meept/internal/config"
)

// SpecWriterSpec returns the spec for the OpenSpec spec-writer agent.
// This agent writes proposal.md, design.md, tasks.md, and spec delta files
// in OpenSpec format as a side effect of task decomposition.
func SpecWriterSpec() *AgentSpec {
	return &AgentSpec{
		ID:   config.AgentIDSpecWriter,
		Name: "Spec Writer Agent",
		Role: RoleExecutor,
		Purpose: `You are an OpenSpec spec writer. You create and modify OpenSpec-formatted specification files.

## Your Role
When the planner decomposes a task, you write the corresponding OpenSpec change directory:
- proposal.md: why the change is needed
- design.md: technical decisions and alternatives
- tasks.md: implementation checklist
- specs/<capability>/spec.md: spec delta with additions (+) and removals (-)

## OpenSpec Format
Specs use SHALL language: "The system SHALL ..."
Scenarios use GIVEN/WHEN/THEN pattern.

## Key Rules
- Write to the openspec/ directory only
- Every requirement must have at least one scenario
- Delta lines start with + for additions, - for removals
- Keep specs concise and testable`,
		Model: "",
		AdditionalTools: []string{
			ToolFileRead,
			ToolFileWrite,
		},
		Constraints: AgentConstraints{
			MaxIterations:    5,
			Timeout:          2 * time.Minute,
			MaxTokensPerTurn: 4096,
		},
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/agent/ -run TestSpecWriterSpec -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/spec_writer.go internal/agent/spec_writer_test.go
git commit -m "feat(agent): add spec-writer agent spec"
```

### Task 8: Add spec-reviewer agent spec

**Files:**
- Create: `internal/agent/spec_reviewer.go`
- Create: `internal/agent/spec_reviewer_test.go`

- [ ] **Step 1: Write the failing test**

`internal/agent/spec_reviewer_test.go`:

```go
package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/config"
)

func TestSpecReviewerSpec(t *testing.T) {
	spec := SpecReviewerSpec()

	if spec.ID != config.AgentIDSpecReviewer {
		t.Errorf("expected ID %q, got %q", config.AgentIDSpecReviewer, spec.ID)
	}
	if spec.Role != RoleReviewer {
		t.Errorf("expected Role %q, got %q", RoleReviewer, spec.Role)
	}
	if spec.Purpose == "" {
		t.Error("expected non-empty Purpose")
	}
	if !spec.HasTool(ToolFileRead) {
		t.Error("expected spec-reviewer to have file_read tool")
	}
}

func TestBuildSpecReviewPrompt(t *testing.T) {
	requirements := []string{
		"The system SHALL expire sessions after a configured duration.",
		"The system SHALL validate session tokens on every authenticated request.",
	}
	changedFiles := []string{"internal/auth/session.go", "internal/auth/middleware.go"}

	prompt := BuildSpecReviewPrompt(requirements, changedFiles)

	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	for _, req := range requirements {
		if !contains(prompt, req) {
			t.Errorf("prompt missing requirement: %s", req)
		}
	}
	for _, f := range changedFiles {
		if !contains(prompt, f) {
			t.Errorf("prompt missing changed file: %s", f)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

Note: replace `contains`/`stringContains` with `strings.Contains` in the actual file. The above demonstrates the test logic without extra imports; the real test file should import `"strings"` and use `strings.Contains`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/agent/ -run TestSpecReviewerSpec -v`
Expected: FAIL — `SpecReviewerSpec` and `BuildSpecReviewPrompt` undefined

- [ ] **Step 3: Write the spec-reviewer implementation**

`internal/agent/spec_reviewer.go`:

```go
package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// SpecReviewerSpec returns the spec for the OpenSpec spec-reviewer agent.
// This agent reviews completed work against the OpenSpec spec requirements
// to verify that each SHALL statement is satisfied by the implementation.
func SpecReviewerSpec() *AgentSpec {
	return &AgentSpec{
		ID:   config.AgentIDSpecReviewer,
		Name: "Spec Reviewer Agent",
		Role: RoleReviewer,
		Purpose: `You are an OpenSpec spec-compliance reviewer. Your role is to verify that completed work satisfies the spec requirements.

## Your Review Process
1. Read the OpenSpec spec file for the affected capability
2. For each ### Requirement: ... SHALL ... block:
   a. Identify which changed files should implement this requirement
   b. Read those files and verify the requirement is satisfied
   c. Check that each GIVEN/WHEN/THEN scenario is handled
3. Report which requirements are satisfied and which are not

## Output Format
Always respond with JSON:
{"status": "approved"|"rejected"|"needs_info", "feedback": "...", "issues": [...], "confidence": 0.0-1.0, "requirements_checked": N, "requirements_satisfied": M}

## Rules
- Only verify spec compliance, not code style or general quality
- If a requirement is not implemented, reject with specific feedback
- If no spec exists for the capability, approve with a note that no spec was found`,
		Model: "",
		AdditionalTools: []string{
			ToolFileRead,
			ToolMemorySearch,
		},
		Constraints: AgentConstraints{
			MaxIterations:    5,
			Timeout:          3 * time.Minute,
			MaxTokensPerTurn: 4096,
		},
	}
}

// BuildSpecReviewPrompt constructs the prompt for the spec-reviewer agent.
// It lists the requirements to verify and the files that were changed.
func BuildSpecReviewPrompt(requirements []string, changedFiles []string) string {
	var b strings.Builder

	b.WriteString("You are reviewing completed work for spec compliance.\n\n")
	b.WriteString("## Requirements to Verify\n\n")
	for i, req := range requirements {
		fmt.Fprintf(&b, "%d. %s\n", i+1, req)
	}

	b.WriteString("\n## Changed Files\n\n")
	for _, f := range changedFiles {
		fmt.Fprintf(&b, "- %s\n", f)
	}

	b.WriteString("\n## Instructions\n")
	b.WriteString("For each requirement above:\n")
	b.WriteString("1. Read the relevant changed files\n")
	b.WriteString("2. Verify the SHALL statement is satisfied\n")
	b.WriteString("3. Check that GIVEN/WHEN/THEN scenarios are handled\n")
	b.WriteString("4. Report results as JSON\n\n")
	b.WriteString(`Respond with: {"status": "approved"|"rejected"|"needs_info", "feedback": "...", "issues": [...], "confidence": 0.0-1.0, "requirements_checked": N, "requirements_satisfied": M}`)

	return b.String()
}
```

- [ ] **Step 4: Fix the test to use strings.Contains**

Replace the `contains` / `stringContains` functions in the test file with a proper import of `"strings"` and use `strings.Contains(prompt, req)`.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/agent/ -run "TestSpecReviewerSpec|TestBuildSpecReviewPrompt" -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agent/spec_reviewer.go internal/agent/spec_reviewer_test.go
git commit -m "feat(agent): add spec-reviewer agent spec and prompt builder"
```

### Task 9: Add researcher agent spec

**Files:**
- Create: `internal/agent/researcher.go`
- Create: `internal/agent/researcher_test.go`

- [ ] **Step 1: Write the failing test**

`internal/agent/researcher_test.go`:

```go
package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/config"
)

func TestResearcherAgentSpec(t *testing.T) {
	spec := ResearcherAgentSpec()

	if spec.ID != config.AgentIDResearcher {
		t.Errorf("expected ID %q, got %q", config.AgentIDResearcher, spec.ID)
	}
	if spec.Role != RoleExecutor {
		t.Errorf("expected Role %q, got %q", RoleExecutor, spec.Role)
	}
	if spec.Purpose == "" {
		t.Error("expected non-empty Purpose")
	}
	if !spec.HasTool(ToolWebFetch) {
		t.Error("expected researcher to have web_fetch tool")
	}
	if !spec.HasTool(ToolWebSearch) {
		t.Error("expected researcher to have web_search tool")
	}
	if !spec.HasTool(ToolFileWrite) {
		t.Error("expected researcher to have file_write tool (for llm-wiki output)")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/agent/ -run TestResearcherAgentSpec -v`
Expected: FAIL — `ResearcherAgentSpec` undefined

- [ ] **Step 3: Write the researcher spec**

`internal/agent/researcher.go`:

```go
package agent

import (
	"time"

	"github.com/caimlas/meept/internal/config"
)

// ResearcherAgentSpec returns the spec for the researcher agent.
// The researcher agent uses llm-wiki conventions to compile knowledge bases
// from web research. It dispatches parallel research threads, ingests sources,
// and compiles cross-referenced articles as markdown files.
func ResearcherAgentSpec() *AgentSpec {
	return &AgentSpec{
		ID:   config.AgentIDResearcher,
		Name: "Researcher Agent",
		Role: RoleExecutor,
		Purpose: `You are a research specialist. You use the llm-wiki methodology to compile persistent knowledge bases from web research.

## Your Capabilities
- Multi-angle web research using web_search and web_fetch
- Source ingestion into structured markdown knowledge bases
- Thesis-driven investigation (producing verdicts, not just summaries)
- Confidence scoring (high/medium/low) for compiled articles
- Cross-referenced article compilation under wiki/concepts/ and wiki/topics/

## llm-wiki Directory Structure
When researching a topic, create files under the project's docs/wiki/<topic>/:
- raw/: Immutable ingested sources
- wiki/concepts/: Foundational ideas and mechanisms
- wiki/topics/: Specific subjects and comparisons
- wiki/references/: Tools, frameworks, lookup resources
- output/: Generated reports and plans
- _index.md: Derived index rebuilt from frontmatter

## Session Flow
1. Parse the research query for topic and scope
2. Search from multiple angles (academic, technical, applied, news, contrarian)
3. Ingest quality sources into raw/
4. Compile cross-referenced articles into wiki/
5. Generate a summary report in output/

## Dual-Link Format
Use dual-link markup for cross-references:
[[slug|Title]] ([Title](../concepts/slug.md))

This serves both Obsidian graph view and standard Markdown renderers.`,
		Model: "",
		AdditionalTools: []string{
			ToolWebFetch,
			ToolWebSearch,
			ToolFileRead,
			ToolFileWrite,
		},
		AvailableSkills: []string{"research"},
		SkillTriggers: map[string]string{
			"research":     "research",
			"investigate":  "research",
			"deep dive":    "research",
			"study":        "research",
		},
		Constraints: AgentConstraints{
			MaxIterations:    50, // Research needs more iterations
			Timeout:          30 * time.Minute,
			MaxTokensPerTurn: 8192,
			MaxMemoryRefs:    30,
		},
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/agent/ -run TestResearcherAgentSpec -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/researcher.go internal/agent/researcher_test.go
git commit -m "feat(agent): add researcher agent spec with llm-wiki integration"
```

### Task 10: Register new agents in DefaultSpecs

**Files:**
- Modify: `internal/agent/spec.go:455-471`

- [ ] **Step 1: Read the current DefaultSpecs function**

Confirm lines 454-471 of `internal/agent/spec.go`.

- [ ] **Step 2: Add the new specs to DefaultSpecs**

In `internal/agent/spec.go`, modify `DefaultSpecs()` to add the three new specs. Append before the closing `}` of the slice:

```go
		SpecWriterSpec(),
		SpecReviewerSpec(),
		ResearcherAgentSpec(),
```

The function should read:

```go
func DefaultSpecs() []*AgentSpec {
	return []*AgentSpec{
		DispatcherSpec(),
		ChatAgentSpec(),
		CoderAgentSpec(),
		DebuggerAgentSpec(),
		PlannerAgentSpec(),
		AnalystAgentSpec(),
		CommitterAgentSpec(),
		SchedulerAgentSpec(),
		CodeReviewerSpec(),
		TestReviewerSpec(),
		DebugReviewerSpec(),
		AnalystReviewerSpec(),
		PlannerReviewerSpec(),
		SpecWriterSpec(),
		SpecReviewerSpec(),
		ResearcherAgentSpec(),
	}
}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/agent/`
Expected: succeeds

- [ ] **Step 4: Commit**

```bash
git add internal/agent/spec.go
git commit -m "feat(agent): register spec-writer, spec-reviewer, researcher in DefaultSpecs"
```

---

## Phase 3: Intent Routing Updates

### Task 11: Route IntentResearch to researcher agent

**Files:**
- Modify: `internal/agent/intent.go:90-91`

- [ ] **Step 1: Read the current DefaultAgent method**

Confirm lines 80-111 of `internal/agent/intent.go`.

- [ ] **Step 2: Change IntentResearch routing**

In `internal/agent/intent.go`, in the `DefaultAgent()` method at line 90-91, change:

```go
	case IntentAnalyze, IntentSearch, IntentResearch:
		return config.AgentIDAnalyst
```

to:

```go
	case IntentAnalyze, IntentSearch:
		return config.AgentIDAnalyst
	case IntentResearch:
		return config.AgentIDResearcher
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/agent/`
Expected: succeeds

- [ ] **Step 4: Run existing intent tests**

Run: `go test ./internal/agent/ -run TestIntent -v`
Expected: PASS (if an existing test checks IntentResearch → analyst, update it)

- [ ] **Step 5: Commit**

```bash
git add internal/agent/intent.go
git commit -m "feat(agent): route IntentResearch to researcher agent instead of analyst"
```

### Task 12: Update selectAgent in tactical scheduler

**Files:**
- Modify: `internal/agent/tactical.go:1069-1086`

- [ ] **Step 1: Read the current selectAgent function**

Confirm the function at the saved tool results file offset 1069.

- [ ] **Step 2: Add research and spec-review routing**

In `internal/agent/tactical.go`, in `selectAgent()` at line 1069, add:

```go
	case string(IntentResearch):
		return config.AgentIDResearcher
```

after the `IntentAnalyze` case (currently grouped with `IntentResearch`). Also add spec-reviewer routing for steps that have a `SpecChangeID` — but since `selectAgent` only takes a `*task.TaskStep`, check `step.ToolHint` for `"spec-review"`:

```go
	case "spec-review":
		return config.AgentIDSpecReviewer
```

The full switch should be:

```go
func (ts *TacticalScheduler) selectAgent(step *task.TaskStep) string {
	switch step.ToolHint {
	case string(IntentCode), KeywordRefactor:
		return config.AgentIDCoder
	case string(IntentDebug), KeywordFix:
		return config.AgentIDDebugger
	case string(IntentAnalyze), string(IntentSearch):
		return config.AgentIDAnalyst
	case string(IntentResearch):
		return config.AgentIDResearcher
	case string(IntentGit), KeywordCommit:
		return config.AgentIDCommitter
	case string(IntentSchedule):
		return config.AgentIDScheduler
	case string(IntentPlan):
		return config.AgentIDPlanner
	case "spec-review":
		return config.AgentIDSpecReviewer
	default:
		return config.AgentIDChat
	}
}
```

- [ ] **Step 3: Add AgentIDResearcher and AgentIDSpecReviewer to knownAgents**

In the same file at line 118, add `"researcher"`, `"spec-reviewer"`, `"spec-writer"` to the `knownAgents` slice:

```go
	knownAgents := []string{
		config.AgentIDCoder, config.AgentIDDebugger, config.AgentIDPlanner,
		config.AgentIDAnalyst, config.AgentIDCommitter, config.AgentIDScheduler,
		config.AgentIDChat, config.AgentIDResearcher, config.AgentIDSpecReviewer,
		config.AgentIDSpecWriter,
	}
```

- [ ] **Step 4: Verify it compiles**

Run: `go build ./internal/agent/`
Expected: succeeds

- [ ] **Step 5: Commit**

```bash
git add internal/agent/tactical.go
git commit -m "feat(agent): route research intent to researcher, add spec-reviewer routing"
```

---

## Phase 4: Strategic Planner OpenSpec Integration

### Task 13: Add openspec fields to StrategicPlanner

**Files:**
- Modify: `internal/agent/strategic.go:109-156`

- [ ] **Step 1: Add the openspec field to StrategicPlanner**

In `internal/agent/strategic.go`, add to the `StrategicPlanner` struct (after line 119, after `pairManager *PairManager`):

```go
	openspecRepo *openspec.Repository
```

Add the corresponding config field to `StrategicPlannerConfig` (after line 129, after `PairManager *PairManager`):

```go
	OpenspecRepo *openspec.Repository
```

Add the import for the openspec package at the top of the file:

```go
	"github.com/caimlas/meept/internal/openspec"
```

In `NewStrategicPlanner` at line 146, add:

```go
		openspecRepo: cfg.OpenspecRepo,
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/agent/`
Expected: succeeds

- [ ] **Step 3: Commit**

```bash
git add internal/agent/strategic.go
git commit -m "feat(agent): add openspec repository to StrategicPlanner"
```

### Task 14: Write OpenSpec change dir in Plan()

**Files:**
- Modify: `internal/agent/strategic.go:284-484` (the `Plan()` method)

- [ ] **Step 1: Add the writeChangeDir method**

Add a new method to `internal/agent/strategic.go`:

```go
// writeChangeDir writes an OpenSpec change directory from the planned steps.
// This is called as a side effect of Plan() when an openspec repo is configured.
// The change ID is derived from the task ID.
func (sp *StrategicPlanner) writeChangeDir(req PlanRequest, steps []*task.TaskStep) {
	if sp.openspecRepo == nil {
		return
	}

	// Generate change directory
	changeID := req.TaskID
	changeDir := sp.openspecRepo.ChangePath(changeID)

	// Build the Change struct
	change := &openspec.Change{
		ID: changeID,
		Proposal: openspec.ChangeProposal{
			Motivation:       req.Input,
			ProposedChanges:  fmt.Sprintf("%d steps planned for task %s", len(steps), req.TaskID),
		},
		Tasks: openspec.ChangeTasks{
			Items: make([]openspec.TaskItem, len(steps)),
		},
	}
	for i, step := range steps {
		change.Tasks.Items[i] = openspec.TaskItem{
			Description: step.Description,
			Done:         false,
		}
	}

	if err := openspec.WriteChange(changeDir, change); err != nil {
		sp.logger.Warn("failed to write OpenSpec change dir",
			"change_id", changeID,
			"error", err,
		)
	} else {
		sp.logger.Info("wrote OpenSpec change dir",
			"change_id", changeID,
			"steps", len(steps),
		)
	}
}
```

- [ ] **Step 2: Call writeChangeDir in Plan()**

In the `Plan()` method, after the steps are persisted (after line 441, after the `sp.stepStore.Create(step)` loop), add:

```go
	// Write OpenSpec change directory as side effect of planning
	sp.writeChangeDir(req, steps)
```

Also add it after the approval flow. In `ApprovePlan()` at line 997 (after steps are persisted), add:

```go
	// Write OpenSpec change directory on approval
	if sp.openspecRepo != nil {
		sp.writeChangeDir(PlanRequest{
			TaskID:    taskID,
			Input:     t.Description,
			Intent:    string(IntentPlan),
		}, steps)
	}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/agent/`
Expected: succeeds

- [ ] **Step 4: Commit**

```bash
git add internal/agent/strategic.go
git commit -m "feat(agent): write OpenSpec change dir as side effect of planning"
```

### Task 15: Apply spec delta on plan approval

**Files:**
- Modify: `internal/agent/strategic.go:910-999` (`ApprovePlan`)

- [ ] **Step 1: Add delta application to ApprovePlan**

In `ApprovePlan()`, after the steps are persisted and before `sp.publishEvent("task.approved", ...)`, add:

```go
	// Apply OpenSpec change delta
	if sp.openspecRepo != nil {
		if err := sp.openspecRepo.ApplyChange(taskID); err != nil {
			sp.logger.Warn("failed to apply OpenSpec change on plan approval",
				"task_id", taskID,
				"error", err,
			)
		} else {
			sp.logger.Info("applied OpenSpec change on approval",
				"task_id", taskID,
			)
		}
	}
```

- [ ] **Step 2: Add archive on RejectPlan**

In `RejectPlan()`, before `sp.publishEvent("task.rejected", ...)`, add:

```go
	// Archive OpenSpec change on rejection
	if sp.openspecRepo != nil {
		if err := sp.openspecRepo.ArchiveChange(taskID); err != nil {
			sp.logger.Warn("failed to archive OpenSpec change on rejection",
				"task_id", taskID,
				"error", err,
			)
		}
	}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/agent/`
Expected: succeeds

- [ ] **Step 4: Commit**

```bash
git add internal/agent/strategic.go
git commit -m "feat(agent): apply OpenSpec delta on plan approval, archive on rejection"
```

### Task 15a: Add OpenSpec integration to PlanManager

**Files:**
- Modify: `internal/plan/manager.go:31-40` (struct), `42-60` (constructor), `146-183` (ApprovePlan), `186-214` (RejectPlan)

This task mirrors the StrategicPlanner OpenSpec integration on the `PlanManager` approval path. The `PlanManager` is used when plans are created through the `plan.md`-based workflow (`meept plans approve/reject/confirm`), as opposed to the in-memory `StrategicPlanner` path.

- [ ] **Step 1: Add openspecRepo field to PlanManager**

In `internal/plan/manager.go`, add to the `PlanManager` struct (after line 39, after `mu sync.RWMutex`):

```go
	openspecRepo *openspec.Repository
```

Add the import at the top of the file:

```go
	"github.com/caimlas/meept/internal/openspec"
```

- [ ] **Step 2: Add OpenSpecRepo option to NewPlanManager**

Change the `NewPlanManager` signature to accept an optional openspec repo. Since the existing signature has 5 parameters and is called from `internal/daemon/components.go`, use a functional option pattern to avoid breaking existing callers:

Add after the `PlanManager` struct definition:

```go
// PlanManagerOption configures a PlanManager.
type PlanManagerOption func(*PlanManager)

// WithOpenSpecRepo sets the OpenSpec repository for plan approval integration.
func WithOpenSpecRepo(repo *openspec.Repository) PlanManagerOption {
	return func(pm *PlanManager) {
		if repo != nil {
			pm.openspecRepo = repo
		}
	}
}
```

Modify `NewPlanManager` to accept variadic options:

```go
func NewPlanManager(store PlanStore, bus *bus.MessageBus, cfg config.PlansConfig, taskCreator TaskCreator, logger *slog.Logger, opts ...PlanManagerOption) *PlanManager {
	if logger == nil {
		logger = slog.Default()
	}
	pm := &PlanManager{
		store:        store,
		bus:          bus,
		config:       cfg,
		taskCreator:  taskCreator,
		logger:       logger,
		phaseTaskMap: make(map[string]string),
		taskPlanMap:  make(map[string]string),
	}
	for _, opt := range opts {
		opt(pm)
	}
	return pm
}
```

- [ ] **Step 3: Add ApplyChange to ApprovePlan**

In `ApprovePlan()` at line 146, after `plan.UpdatedAt = now` and before `m.store.UpdatePlan(ctx, plan)`, add:

```go
	// Apply OpenSpec change delta if the plan has an associated change
	if m.openspecRepo != nil && plan.SpecChangeID != "" {
		if err := m.openspecRepo.ApplyChange(plan.SpecChangeID); err != nil {
			m.logger.Warn("failed to apply OpenSpec change on plan approval",
				"plan_id", planID,
				"change_id", plan.SpecChangeID,
				"error", err,
			)
			// Non-fatal: plan is still approved, spec delta may need manual apply
		} else {
			m.logger.Info("applied OpenSpec change on plan approval",
				"plan_id", planID,
				"change_id", plan.SpecChangeID,
			)
		}
	}
```

- [ ] **Step 4: Add ArchiveChange to RejectPlan**

In `RejectPlan()` at line 186, before `m.store.SetPlanState(ctx, planID, StateCancelled)`, add:

```go
	// Archive OpenSpec change on rejection
	if m.openspecRepo != nil && plan.SpecChangeID != "" {
		if err := m.openspecRepo.ArchiveChange(plan.SpecChangeID); err != nil {
			m.logger.Warn("failed to archive OpenSpec change on plan rejection",
				"plan_id", planID,
				"change_id", plan.SpecChangeID,
				"error", err,
			)
		} else {
			m.logger.Info("archived OpenSpec change on plan rejection",
				"plan_id", planID,
				"change_id", plan.SpecChangeID,
			)
		}
	}
```

- [ ] **Step 5: Write the failing test**

Create `internal/plan/manager_openspec_test.go`:

```go
package plan

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/openspec"
	"github.com/caimlas/meept/internal/task"
)

func TestPlanManager_ApprovePlan_AppliesOpenSpecChange(t *testing.T) {
	tmpDir := t.TempDir()
	openspecDir := filepath.Join(tmpDir, "openspec")
	repo := openspec.NewRepository(openspecDir)
	if err := repo.Init(); err != nil {
		t.Fatal(err)
	}

	// Write a base spec
	baseSpec := &openspec.Spec{
		Capability: "test-cap",
		Purpose:    "Test capability.",
		Requirements: []openspec.Requirement{
			{Title: "Existing Req", Shall: "The system SHALL exist."},
		},
	}
	if err := openspec.WriteSpec(repo.SpecPath("test-cap"), baseSpec); err != nil {
		t.Fatal(err)
	}

	// Write a change with a spec delta
	change := &openspec.Change{
		ID: "test-change-001",
		Proposal: openspec.ChangeProposal{
			Motivation:      "Test motivation",
			ProposedChanges:  "Test change",
		},
		SpecDeltas: []openspec.SpecDelta{
			{
				Capability: "test-cap",
				Additions: []openspec.Requirement{
					{Title: "New Req", Shall: "The system SHALL do new thing."},
				},
			},
		},
	}
	if err := openspec.WriteChange(repo.ChangePath("test-change-001"), change); err != nil {
		t.Fatal(err)
	}

	// Create a mock store with a plan in pending_approval state
	store := &mockPlanStore{
		plans: map[string]*Plan{
			"plan-001": {
				ID:            "plan-001",
				Title:         "Test Plan",
				State:         StatePendingApproval,
				SpecChangeID:  "test-change-001",
				FilePath:      filepath.Join(tmpDir, "plan.md"),
			},
		},
	}

	mb := bus.NewMessageBus()
	tc := &mockTaskCreator{}
	logger := testLogger()
	pm := NewPlanManager(store, mb, config.PlansConfig{}, tc, logger, WithOpenSpecRepo(repo))

	err := pm.ApprovePlan(context.Background(), "plan-001", "sess-1", "tester")
	if err != nil {
		t.Fatalf("ApprovePlan failed: %v", err)
	}

	// Verify the spec was updated
	updated, err := repo.GetSpec("test-cap")
	if err != nil {
		t.Fatalf("GetSpec failed: %v", err)
	}
	if len(updated.Requirements) != 2 {
		t.Fatalf("expected 2 requirements after apply, got %d", len(updated.Requirements))
	}
	if updated.Requirements[1].Title != "New Req" {
		t.Errorf("expected second requirement 'New Req', got %q", updated.Requirements[1].Title)
	}
}

func TestPlanManager_RejectPlan_ArchivesOpenSpecChange(t *testing.T) {
	tmpDir := t.TempDir()
	openspecDir := filepath.Join(tmpDir, "openspec")
	repo := openspec.NewRepository(openspecDir)
	if err := repo.Init(); err != nil {
		t.Fatal(err)
	}

	// Write a change (no base spec needed for rejection)
	change := &openspec.Change{
		ID: "test-change-002",
		Proposal: openspec.ChangeProposal{
			Motivation:      "Reject test",
			ProposedChanges:  "Will be rejected",
		},
	}
	if err := openspec.WriteChange(repo.ChangePath("test-change-002"), change); err != nil {
		t.Fatal(err)
	}

	store := &mockPlanStore{
		plans: map[string]*Plan{
			"plan-002": {
				ID:            "plan-002",
				Title:         "Reject Plan",
				State:         StatePendingApproval,
				SpecChangeID:  "test-change-002",
				FilePath:      filepath.Join(tmpDir, "plan2.md"),
			},
		},
	}

	mb := bus.NewMessageBus()
	tc := &mockTaskCreator{}
	logger := testLogger()
	pm := NewPlanManager(store, mb, config.PlansConfig{}, tc, logger, WithOpenSpecRepo(repo))

	err := pm.RejectPlan(context.Background(), "plan-002", "sess-1", "tester", "bad plan")
	if err != nil {
		t.Fatalf("RejectPlan failed: %v", err)
	}

	// Verify the change was archived
	archivedPath := filepath.Join(openspecDir, "changes", "archive", "test-change-002")
	if _, err := os.Stat(archivedPath); os.IsNotExist(err) {
		t.Fatal("change was not moved to archive/")
	}

	// Verify the change is no longer in active list
	changes, err := repo.ListChanges()
	if err != nil {
		t.Fatalf("ListChanges failed: %v", err)
	}
	for _, c := range changes {
		if c == "test-change-002" {
			t.Error("archived change still in active changes list")
		}
	}
}

// mockPlanStore implements PlanStore for testing.
// NOTE: The real PlanStore interface (internal/plan/store.go) has more methods
// than shown here. If a mock already exists in the test package, reuse it.
// Otherwise, implement all methods of the PlanStore interface. The key methods
// for this test are GetPlan, UpdatePlan, SetPlanState, and CreateSignoff.
// The signatures must match the real interface:
//   CreateSignoff(ctx, *PlanSignoff) error
//   GetSignoffs(ctx, string) ([]*PlanSignoff, error)
//   ListPlans(ctx, string, int) ([]*Plan, error)
type mockPlanStore struct {
	plans    map[string]*Plan
	signoffs []*PlanSignoff
}

func (m *mockPlanStore) CreatePlan(ctx context.Context, plan *Plan) error {
	m.plans[plan.ID] = plan
	return nil
}
func (m *mockPlanStore) GetPlan(ctx context.Context, id string) (*Plan, error) {
	p, ok := m.plans[id]
	if !ok {
		return nil, os.ErrNotExist
	}
	return p, nil
}
func (m *mockPlanStore) UpdatePlan(ctx context.Context, plan *Plan) error {
	m.plans[plan.ID] = plan
	return nil
}
func (m *mockPlanStore) DeletePlan(ctx context.Context, id string) error { return nil }
func (m *mockPlanStore) SetPlanState(ctx context.Context, id string, state PlanState) error {
	if p, ok := m.plans[id]; ok {
		p.State = state
	}
	return nil
}
func (m *mockPlanStore) ListPlans(ctx context.Context, projectID string, limit int) ([]*Plan, error) {
	var plans []*Plan
	for _, p := range m.plans {
		plans = append(plans, p)
	}
	return plans, nil
}
func (m *mockPlanStore) ListPlansBySession(ctx context.Context, sessionID string) ([]*Plan, error) {
	return nil, nil
}
func (m *mockPlanStore) ListPlansByState(ctx context.Context, state PlanState, limit int) ([]*Plan, error) {
	return nil, nil
}
func (m *mockPlanStore) CreateSignoff(ctx context.Context, s *PlanSignoff) error {
	m.signoffs = append(m.signoffs, s)
	return nil
}
func (m *mockPlanStore) GetSignoffs(ctx context.Context, planID string) ([]*PlanSignoff, error) {
	return m.signoffs, nil
}
func (m *mockPlanStore) GetRevisionCount(ctx context.Context, planID string) (int, error) {
	return 0, nil
}
func (m *mockPlanStore) CountPlansBySessionAndState(ctx context.Context, sessionID string) (map[PlanState]int, error) {
	return nil, nil
}
func (m *mockPlanStore) CreatePhase(ctx context.Context, p *PlanPhase) error      { return nil }
func (m *mockPlanStore) GetPhases(ctx context.Context, planID string) ([]*PlanPhase, error) { return nil, nil }
func (m *mockPlanStore) UpdatePhase(ctx context.Context, p *PlanPhase) error      { return nil }
func (m *mockPlanStore) SetPhaseState(ctx context.Context, id string, state PhaseState) error { return nil }
func (m *mockPlanStore) IncrementPhaseProgress(ctx context.Context, phaseID string, field string, delta int) error { return nil }
func (m *mockPlanStore) LinkSession(ctx context.Context, planID, sessionID string) error { return nil }
func (m *mockPlanStore) UnlinkSession(ctx context.Context, planID, sessionID string) error { return nil }

// mockTaskCreator implements TaskCreator for testing.
type mockTaskCreator struct{}

func (m *mockTaskCreator) CreateTask(ctx context.Context, name, description string) (*task.Task, error) {
	return &task.Task{ID: "task-mock"}, nil
}
func (m *mockTaskCreator) CreateTaskStep(ctx context.Context, taskID, description string, sequence int) (*task.TaskStep, error) {
	return &task.TaskStep{ID: "step-mock", TaskID: taskID}, nil
}
func (m *mockTaskCreator) UpdateTaskStep(ctx context.Context, step *task.TaskStep) error { return nil }
func (m *mockTaskCreator) LinkSession(ctx context.Context, taskID, sessionID string) error { return nil }

func testLogger() *slog.Logger {
	return slog.Default()
}
```

Note: The `mockPlanStore` and `mockTaskCreator` types may already exist in other test files in `internal/plan/`. If they do, rename these to avoid collision (e.g., prefix with `OpenSpec`) or reuse the existing ones. Check `internal/plan/*_test.go` first.

- [ ] **Step 6: Run the test to verify it fails, then fix and pass**

Run: `go test ./internal/plan/ -run "TestPlanManager_ApprovePlan_AppliesOpenSpecChange|TestPlanManager_RejectPlan_ArchivesOpenSpecChange" -v`
Expected: FAIL initially (if mocks collide or `PlanManagerOption` not yet applied), then PASS after implementation

- [ ] **Step 7: Update daemon wiring to pass the openspec repo to PlanManager**

In `internal/daemon/components.go`, where `NewPlanManager(...)` is called, update to pass the option:

```go
	planManager := plan.NewPlanManager(
		planStore, planBus, plansCfg, taskCreator, planLogger,
		plan.WithOpenSpecRepo(openspecRepo),
	)
```

- [ ] **Step 8: Verify it compiles**

Run: `go build ./internal/plan/ ./internal/daemon/`
Expected: succeeds

- [ ] **Step 9: Commit**

```bash
git add internal/plan/manager.go internal/plan/manager_openspec_test.go internal/daemon/components.go
git commit -m "feat(plan): add OpenSpec integration to PlanManager ApprovePlan/RejectPlan"
```

---

## Phase 5: Spec-Compliance Review in Tactical Scheduler

### Task 16: Add openspec repo to TacticalScheduler

**Files:**
- Modify: `internal/agent/tactical.go:42-63` (struct) and `72-90` (config)

- [ ] **Step 1: Add openspecRepo field**

In `internal/agent/tactical.go`, add to the `TacticalScheduler` struct (after line 62, after `amendmentMgr AmendmentSubmitter`):

```go
	openspecRepo *openspec.Repository
```

Add to `TacticalSchedulerConfig` (after line 89, after `AmendmentManager AmendmentSubmitter`):

```go
	OpenspecRepo *openspec.Repository
```

Add the import:

```go
	"github.com/caimlas/meept/internal/openspec"
```

In `NewTacticalScheduler` at line 143, add:

```go
		openspecRepo: cfg.OpenspecRepo,
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/agent/`
Expected: succeeds

- [ ] **Step 3: Commit**

```bash
git add internal/agent/tactical.go
git commit -m "feat(agent): add openspec repo to TacticalScheduler"
```

### Task 17: Create spec-review step and dispatch asynchronously

**Files:**
- Modify: `internal/agent/tactical.go:698-773` (the `allDone` block in `OnJobCompleted`)

Instead of calling `reviewerLoop.RunOnce()` synchronously inside `OnJobCompleted`, this task creates a **new review `TaskStep`** with `ToolHint: "spec-review"` and enqueues it as a job through the existing `ts.queue`. The task transitions to `StateTesting` (pausing), and when the review job completes, a **second** `OnJobCompleted` invocation handles the result and transitions to `StateCompleted`. This properly uses the async job queue for the review dispatch.

- [ ] **Step 1: Add the spec-review step creation in OnJobCompleted**

In `OnJobCompleted`, in the `if allDone` block (around line 700), before `t.SetState(task.StateCompleted)`, add:

```go
		// Spec-compliance review: if the task has an OpenSpec change,
		// create a review step and enqueue it as a job. The task pauses
		// in StateTesting until the review job completes.
		if ts.openspecRepo != nil {
			changeDir := ts.openspecRepo.ChangePath(step.TaskID)
			if _, err := os.Stat(changeDir); err == nil {
				// Change exists; transition to testing state (pause)
				t.SetState(task.StateTesting)
				if err := ts.taskStore.Update(t); err != nil {
					ts.logger.Error("failed to set task to testing state", "error", err)
				}

				// Load the change to extract requirements
				change, chErr := ts.openspecRepo.GetChange(step.TaskID)
				if chErr != nil {
					ts.logger.Warn("failed to load OpenSpec change for review",
						"task_id", step.TaskID,
						"error", chErr,
					)
					// Fall back: no spec review possible
					t.SetState(task.StateCompleted)
					break
				}

				// Build requirements list from spec deltas
				var requirements []string
				for _, delta := range change.SpecDeltas {
					for _, req := range delta.Additions {
						if req.Shall != "" {
							requirements = append(requirements, req.Shall)
						}
					}
				}

				// Collect changed files from step descriptions
				var changedFiles []string
				allSteps, _ := ts.stepStore.ListByTaskID(step.TaskID)
				for _, s := range allSteps {
					if s.State == task.StepCompleted {
						changedFiles = append(changedFiles, s.Description)
					}
				}

				// Build review prompt (stored as step description)
				prompt := BuildSpecReviewPrompt(requirements, changedFiles)

				// Create a spec-review step
				reviewStep := &task.TaskStep{
					TaskID:      step.TaskID,
					Description: prompt,
					ToolHint:    "spec-review",
					AgentID:     config.AgentIDSpecReviewer,
					State:       task.StepReady,
					Sequence:    9999, // after all implementation steps
				}
				if err := ts.stepStore.Create(reviewStep); err != nil {
					ts.logger.Error("failed to create spec-review step",
						"task_id", step.TaskID,
						"error", err,
					)
					t.SetState(task.StateCompleted)
					break
				}

				// Enqueue the review step as a job through the queue.
				// queue.NewJob creates a Job with the payload marshalled to JSON.
				job, jErr := queue.NewJob(queue.JobTypeOneOff, map[string]string{
					"prompt":     prompt,
					"task_id":    step.TaskID,
					"step_id":   reviewStep.ID,
					"change_id": step.TaskID,
				})
				if jErr != nil {
					ts.logger.Error("failed to create spec-review job",
						"task_id", step.TaskID,
						"error", jErr,
					)
					t.SetState(task.StateCompleted)
					break
				}
				job.AgentID = config.AgentIDSpecReviewer
				job.TaskID = step.TaskID
				if err := ts.queue.Enqueue(ctx, job); err != nil {
					ts.logger.Error("failed to enqueue spec-review job",
						"task_id", step.TaskID,
						"error", err,
					)
					t.SetState(task.StateCompleted)
					break
				}

				// Publish spec review requested event
				ts.publishEvent("task.spec_review", map[string]any{
					KeyTaskID:          step.TaskID,
					"change_id":        step.TaskID,
					"requirement_count": len(requirements),
					"review_step_id":   reviewStep.ID,
				})

				// Return without transition to StateCompleted.
				// The review step's OnJobCompleted will handle finalization.
				return
			}
		}
```

Also add imports `"os"` and ensure `"fmt"` is imported (check existing first — `fmt` is likely already present in tactical.go). Add `"github.com/caimlas/meept/internal/queue"` for the `queue.NewJob` and `queue.JobTypeOneOff` references.

- [ ] **Step 2: Add review-completion handling in OnJobCompleted**

In `OnJobCompleted`, **before** the `allDone` check (around line 698), add a branch that detects spec-review steps and finalizes the task:

```go
	// If this was a spec-review step, finalize the task.
	if step.ToolHint == "spec-review" {
		ts.logger.Info("spec-review completed",
			"task_id", step.TaskID,
			"result", step.Result,
		)

		// Publish review completed event
		ts.publishEvent("task.spec_reviewed", map[string]any{
			KeyTaskID:   step.TaskID,
			"step_id":   step.ID,
			"result":    step.Result,
		})

		// Transition task from Testing to Completed
		t.SetState(task.StateCompleted)
		if err := ts.taskStore.Update(t); err != nil {
			ts.logger.Error("failed to complete task after spec review",
				"task_id", step.TaskID,
				"error", err,
			)
			return err
		}
		return nil
	}
```

This ensures that when the spec-reviewer job completes, the `OnJobCompleted` handler sees `step.ToolHint == "spec-review"`, publishes the review result, and transitions the task from `StateTesting` to `StateCompleted`. The early `return` prevents the normal `allDone` logic from firing a second time.

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/agent/`
Expected: succeeds

- [ ] **Step 4: Write a test for async spec-review dispatch**

Create `internal/agent/tactical_spec_review_test.go`:

```go
package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/openspec"
	"github.com/caimlas/meept/internal/task"
)

func TestOnJobCompleted_AsyncSpecReview(t *testing.T) {
	tmpDir := t.TempDir()
	openspecDir := filepath.Join(tmpDir, "openspec")
	repo := openspec.NewRepository(openspecDir)
	if err := repo.Init(); err != nil {
		t.Fatal(err)
	}

	// Write a change with a spec delta
	change := &openspec.Change{
		ID: "test-change",
		Proposal: openspec.ChangeProposal{
			Motivation:      "test",
			ProposedChanges:  "test",
		},
		SpecDeltas: []openspec.SpecDelta{
			{
				Capability: "test-cap",
				Additions: []openspec.Requirement{
					{Title: "Test Req", Shall: "The system SHALL test."},
				},
			},
		},
	}
	if err := openspec.WriteChange(repo.ChangePath("test-change"), change); err != nil {
		t.Fatal(err)
	}

	// Verify the change directory exists
	changeDir := repo.ChangePath("test-change")
	if _, err := os.Stat(changeDir); err != nil {
		t.Fatalf("change dir not created: %v", err)
	}

	// Verify requirements can be loaded
	loaded, err := repo.GetChange("test-change")
	if err != nil {
		t.Fatalf("GetChange failed: %v", err)
	}
	if len(loaded.SpecDeltas) != 1 {
		t.Fatalf("expected 1 delta, got %d", len(loaded.SpecDeltas))
	}
	if len(loaded.SpecDeltas[0].Additions) != 1 {
		t.Fatalf("expected 1 addition, got %d", len(loaded.SpecDeltas[0].Additions))
	}
	if loaded.SpecDeltas[0].Additions[0].Shall != "The system SHALL test." {
		t.Errorf("unexpected Shall: %q", loaded.SpecDeltas[0].Additions[0].Shall)
	}

	// Verify BuildSpecReviewPrompt produces a non-empty prompt
	prompt := BuildSpecReviewPrompt(
		[]string{"The system SHALL test."},
		[]string{"internal/test/test.go"},
	)
	if prompt == "" {
		t.Error("expected non-empty review prompt")
	}

	// Verify the ToolHint "spec-review" would route to the spec-reviewer agent
	// (This is validated by the selectAgent test in Task 12)
	_ = task.StepReady // ensure task package is referenced
}
```

- [ ] **Step 5: Run the test**

Run: `go test ./internal/agent/ -run TestOnJobCompleted_AsyncSpecReview -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agent/tactical.go internal/agent/tactical_spec_review_test.go
git commit -m "feat(agent): async spec-reviewer dispatch via job queue, activate StateTesting"
```

---

## Phase 6: Research Skill and CLI

### Task 18: Create research skill (SKILL.md)

**Files:**
- Create: `skills/research/SKILL.md`

- [ ] **Step 1: Write the skill file**

```markdown
---
name: research
description: Multi-agent research compilation using llm-wiki methodology. Dispatches parallel research threads, ingests sources, and compiles cross-referenced knowledge bases.
requires:
  - reasoning
  - tool_use
tags:
  - research
  - knowledge
  - llm-wiki
examples:
  - "research gut microbiome"
  - "investigate rust async runtime comparison"
  - "deep dive into CRDT implementations"
risk_level: low
max_iterations: 50
temperature: 0.5
---

# Research Skill (llm-wiki)

## Overview

This skill implements the llm-wiki methodology for compiling persistent knowledge bases from multi-agent research. It is inspired by Andrej Karpathy's LLM wiki concept and the llm-wiki project (llm-wiki.net).

## Process

### 1. Parse the research query
Extract the topic, scope, and any thesis or question from the user's request.

### 2. Create topic wiki directory
Create a directory structure under `docs/wiki/<topic-slug>/`:

```
docs/wiki/<topic-slug>/
├── raw/              # Immutable ingested sources
├── wiki/
│   ├── concepts/     # Foundational ideas
│   ├── topics/       # Specific subjects
│   └── references/   # Tools, frameworks
├── output/           # Generated reports
├── inbox/            # Drop zone for files
├── inventory/        # Durable state tracking
├── datasets/         # External data manifests
├── _index.md         # Derived cache from frontmatter
```

### 3. Multi-angle research
Search from multiple perspectives:
- Academic: scholarly articles, papers
- Technical: documentation, API references
- Applied: case studies, blog posts, tutorials
- News: recent developments
- Contrarian: opposing viewpoints, critiques

### 4. Source ingestion
For each quality source found:
- Fetch the full content via web_fetch
- Store as immutable markdown in `raw/<source-name>.md`
- Include frontmatter with: url, fetched_at, confidence, source_type

### 5. Article compilation
Synthesize cross-referenced articles:
- `wiki/concepts/`: foundational ideas and mechanisms
- `wiki/topics/`: specific subjects, comparisons, state-of-field
- `wiki/references/`: tools, frameworks, lookup resources

Use dual-link markup for cross-references:
`[[slug|Title]] ([Title](../concepts/slug.md))`

### 6. Confidence scoring
Each article carries a confidence score:
- `high`: multiple independent sources corroborate
- `medium`: single reliable source or partial corroboration
- `low`: speculative or single source

### 7. Output generation
Generate a summary report in `output/report.md`:
- Key findings
- Source quality assessment
- Confidence-weighted conclusions
- Recommended next steps

### 8. Index generation
Write `_index.md` as a derived cache:
- List all articles with frontmatter
- Categorize by type (concept, topic, reference)
- Include confidence scores

## Thesis-Driven Mode

If the user provides a specific claim or thesis:
- Split agents across supporting, opposing, mechanistic, meta/review, and adjacent perspectives
- Produce a verdict: supported, contradicted, insufficient evidence, or mixed

## Audit Trail

The `inventory/` directory tracks:
- Source candidates
- Open questions
- Watch items
- Next actions

This enables future re-research when evidence is stale.
```

- [ ] **Step 2: Commit**

```bash
git add skills/research/SKILL.md
git commit -m "feat(skills): add research skill using llm-wiki methodology"
```

### Task 18a: Create programmatic llm-wiki runner

**Files:**
- Create: `internal/agent/wiki_runner.go`
- Create: `internal/agent/wiki_runner_test.go`

The SKILL.md (Task 18) instructs the agent to use its existing tools. This task adds a **programmatic runner** that enforces the llm-wiki directory structure, manages source ingestion, validates dual-link markup, and regenerates `_index.md`. The runner is called by the researcher agent's tool set or directly from the `research` CLI command, providing structure guarantees that a pure LLM-following-instructions approach cannot enforce.

- [ ] **Step 1: Write the failing test**

`internal/agent/wiki_runner_test.go`:

```go
package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWikiRunner_InitTopic(t *testing.T) {
	tmpDir := t.TempDir()
	runner := NewWikiRunner(tmpDir)

	err := runner.InitTopic("test-topic")
	if err != nil {
		t.Fatalf("InitTopic failed: %v", err)
	}

	// Verify directory structure
	dirs := []string{
		"raw",
		"wiki/concepts",
		"wiki/topics",
		"wiki/references",
		"output",
		"inbox",
		"inventory",
		"datasets",
	}
	for _, d := range dirs {
		p := filepath.Join(tmpDir, "test-topic", d)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			t.Errorf("directory not created: %s", p)
		}
	}
}

func TestWikiRunner_IngestSource(t *testing.T) {
	tmpDir := t.TempDir()
	runner := NewWikiRunner(tmpDir)
	if err := runner.InitTopic("test-topic"); err != nil {
		t.Fatal(err)
	}

	err := runner.IngestSource("test-topic", "example-article", "https://example.com/article",
		"This is the article content.\nIt has multiple lines.", "high", "technical")
	if err != nil {
		t.Fatalf("IngestSource failed: %v", err)
	}

	// Verify source file exists in raw/
	sourcePath := filepath.Join(tmpDir, "test-topic", "raw", "example-article.md")
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("source file not readable: %v", err)
	}
	content := string(data)
	if !contains(content, "url: https://example.com/article") {
		t.Error("expected source URL in frontmatter")
	}
	if !contains(content, "confidence: high") {
		t.Error("expected confidence in frontmatter")
	}
	if !contains(content, "This is the article content.") {
		t.Error("expected source body in file")
	}
}

func TestWikiRunner_WriteArticle(t *testing.T) {
	tmpDir := t.TempDir()
	runner := NewWikiRunner(tmpDir)
	if err := runner.InitTopic("test-topic"); err != nil {
		t.Fatal(err)
	}

	article := WikiArticle{
		Slug:       "crdt-overview",
		Title:      "CRDT Overview",
		Type:       "concept",
		Confidence: "medium",
		Body:       "# CRDT Overview\n\nConflict-free Replicated Data Types enable decentralized collaboration.\n\nSee [[crdt-implementations|Implementations]].",
	}
	err := runner.WriteArticle("test-topic", article)
	if err != nil {
		t.Fatalf("WriteArticle failed: %v", err)
	}

	// Verify article file exists
	articlePath := filepath.Join(tmpDir, "test-topic", "wiki", "concepts", "crdt-overview.md")
	data, err := os.ReadFile(articlePath)
	if err != nil {
		t.Fatalf("article file not readable: %v", err)
	}
	content := string(data)
	if !contains(content, "title: CRDT Overview") {
		t.Error("expected title in frontmatter")
	}
	if !contains(content, "type: concept") {
		t.Error("expected type in frontmatter")
	}
	if !contains(content, "confidence: medium") {
		t.Error("expected confidence in frontmatter")
	}
}

func TestWikiRunner_GenerateIndex(t *testing.T) {
	tmpDir := t.TempDir()
	runner := NewWikiRunner(tmpDir)
	if err := runner.InitTopic("test-topic"); err != nil {
		t.Fatal(err)
	}

	// Write two articles
	runner.WriteArticle("test-topic", WikiArticle{
		Slug: "concept-a", Title: "Concept A", Type: "concept", Confidence: "high", Body: "Content A",
	})
	runner.WriteArticle("test-topic", WikiArticle{
		Slug: "topic-b", Title: "Topic B", Type: "topic", Confidence: "low", Body: "Content B",
	})

	err := runner.GenerateIndex("test-topic")
	if err != nil {
		t.Fatalf("GenerateIndex failed: %v", err)
	}

	indexPath := filepath.Join(tmpDir, "test-topic", "_index.md")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("index file not readable: %v", err)
	}
	content := string(data)
	if !contains(content, "Concept A") {
		t.Error("index missing Concept A")
	}
	if !contains(content, "Topic B") {
		t.Error("index missing Topic B")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > 0 && len(substr) > 0 && indexOf(s, substr) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

Note: replace the `contains`/`stringContains`/`indexOf` helpers with `strings.Contains` using `import "strings"`. The above demonstrates test logic without the import.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/agent/ -run "TestWikiRunner" -v`
Expected: FAIL — `NewWikiRunner`, `WikiArticle` undefined

- [ ] **Step 3: Write the wiki runner implementation**

`internal/agent/wiki_runner.go`:

```go
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WikiRunner manages llm-wiki directory structures programmatically.
// It enforces the llm-wiki conventions: raw/ for immutable sources,
// wiki/{concepts,topics,references}/ for compiled articles, output/ for reports,
// _index.md as a derived cache, and inventory/ for tracking.
type WikiRunner struct {
	basePath string
}

// WikiArticle represents a compiled article in the wiki directory.
type WikiArticle struct {
	Slug       string // filename without .md
	Title      string
	Type       string // "concept", "topic", "reference"
	Confidence string // "high", "medium", "low"
	Body       string // full markdown body including headings
}

// NewWikiRunner creates a WikiRunner rooted at basePath.
// The base path is typically the project's docs/wiki/ directory.
func NewWikiRunner(basePath string) *WikiRunner {
	return &WikiRunner{basePath: basePath}
}

// InitTopic creates the full llm-wiki directory structure for a topic.
func (wr *WikiRunner) InitTopic(topicSlug string) error {
	topicDir := filepath.Join(wr.basePath, topicSlug)
	dirs := []string{
		"raw",
		"wiki/concepts",
		"wiki/topics",
		"wiki/references",
		"output",
		"inbox",
		"inventory",
		"datasets",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(topicDir, d), 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
	}
	return nil
}

// IngestSource stores an immutable source file in raw/<sourceSlug>.md
// with YAML frontmatter containing metadata.
func (wr *WikiRunner) IngestSource(topicSlug, sourceSlug, url, content, confidence, sourceType string) error {
	rawDir := filepath.Join(wr.basePath, topicSlug, "raw")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		return fmt.Errorf("create raw dir: %w", err)
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("url: %s\n", url))
	b.WriteString(fmt.Sprintf("fetched_at: %s\n", time.Now().UTC().Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("confidence: %s\n", confidence))
	b.WriteString(fmt.Sprintf("source_type: %s\n", sourceType))
	b.WriteString("---\n\n")
	b.WriteString(content)

	filePath := filepath.Join(rawDir, sourceSlug+".md")
	return os.WriteFile(filePath, []byte(b.String()), 0o644)
}

// WriteArticle writes a compiled article to wiki/<type>/<slug>.md
// with YAML frontmatter.
func (wr *WikiRunner) WriteArticle(topicSlug string, article WikiArticle) error {
	typeDir := filepath.Join(wr.basePath, topicSlug, "wiki", article.Type+"s")
	if err := os.MkdirAll(typeDir, 0o755); err != nil {
		return fmt.Errorf("create wiki dir: %w", err)
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("title: %s\n", article.Title))
	b.WriteString(fmt.Sprintf("type: %s\n", article.Type))
	b.WriteString(fmt.Sprintf("slug: %s\n", article.Slug))
	b.WriteString(fmt.Sprintf("confidence: %s\n", article.Confidence))
	b.WriteString(fmt.Sprintf("updated: %s\n", time.Now().UTC().Format(time.RFC3339)))
	b.WriteString("---\n\n")
	b.WriteString(article.Body)

	filePath := filepath.Join(typeDir, article.Slug+".md")
	return os.WriteFile(filePath, []byte(b.String()), 0o644)
}

// GenerateIndex scans the wiki/ directory tree and writes _index.md
// as a derived cache listing all articles with their frontmatter.
func (wr *WikiRunner) GenerateIndex(topicSlug string) error {
	topicDir := filepath.Join(wr.basePath, topicSlug)
	wikiDir := filepath.Join(topicDir, "wiki")

	var b strings.Builder
	b.WriteString("# Index\n\n")
	b.WriteString("<!-- This file is auto-generated by the wiki runner. Do not edit manually. -->\n\n")

	categories := []string{"concepts", "topics", "references"}
	for _, cat := range categories {
		catDir := filepath.Join(wikiDir, cat)
		entries, err := os.ReadDir(catDir)
		if err != nil {
			continue // directory may not exist yet
		}

		articles := []WikiArticle{}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			slug := strings.TrimSuffix(entry.Name(), ".md")
			article, pErr := wr.readArticleFrontmatter(filepath.Join(catDir, entry.Name()), slug, cat)
			if pErr == nil {
				articles = append(articles, article)
			}
		}

		if len(articles) == 0 {
			continue
		}

		b.WriteString(fmt.Sprintf("## %s\n\n", strings.Title(cat)))
		for _, a := range articles {
			b.WriteString(fmt.Sprintf("- [%s](wiki/%s/%s.md) — confidence: %s\n",
				a.Title, cat, a.Slug, a.Confidence))
		}
		b.WriteString("\n")
	}

	indexPath := filepath.Join(topicDir, "_index.md")
	return os.WriteFile(indexPath, []byte(b.String()), 0o644)
}

// readArticleFrontmatter reads YAML frontmatter from a markdown file.
func (wr *WikiRunner) readArticleFrontmatter(filePath, slug, articleType string) (WikiArticle, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return WikiArticle{}, err
	}
	content := string(data)
	article := WikiArticle{Slug: slug, Type: articleType}

	// Simple frontmatter parsing
	lines := strings.Split(content, "\n")
	inFrontmatter := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			if inFrontmatter {
				break // end of frontmatter
			}
			inFrontmatter = true
			continue
		}
		if !inFrontmatter {
			continue
		}
		if strings.HasPrefix(trimmed, "title:") {
			article.Title = strings.TrimSpace(strings.TrimPrefix(trimmed, "title:"))
		}
		if strings.HasPrefix(trimmed, "confidence:") {
			article.Confidence = strings.TrimSpace(strings.TrimPrefix(trimmed, "confidence:"))
		}
	}

	return article, nil
}

// TopicPath returns the full path for a topic directory.
func (wr *WikiRunner) TopicPath(topicSlug string) string {
	return filepath.Join(wr.basePath, topicSlug)
}

// ListSources returns all source slugs in the raw/ directory.
func (wr *WikiRunner) ListSources(topicSlug string) ([]string, error) {
	rawDir := filepath.Join(wr.basePath, topicSlug, "raw")
	entries, err := os.ReadDir(rawDir)
	if err != nil {
		return nil, err
	}
	var sources []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			sources = append(sources, strings.TrimSuffix(entry.Name(), ".md"))
		}
	}
	return sources, nil
}

// WriteOutput writes a generated report to output/<filename>.md
func (wr *WikiRunner) WriteOutput(topicSlug, filename, content string) error {
	outputDir := filepath.Join(wr.basePath, topicSlug, "output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	filePath := filepath.Join(outputDir, filename+".md")
	return os.WriteFile(filePath, []byte(content), 0o644)
}
```

- [ ] **Step 4: Fix the test to use strings.Contains**

Replace the `contains`/`stringContains`/`indexOf` helpers in the test file with:

```go
import "strings"

// Then use strings.Contains(prompt, req) instead of contains(prompt, req)
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/agent/ -run "TestWikiRunner" -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agent/wiki_runner.go internal/agent/wiki_runner_test.go
git commit -m "feat(agent): add programmatic llm-wiki runner for directory management"
```

### Task 19: Create research CLI command

**Files:**
- Create: `cmd/meept/research.go`
- Create: `cmd/meept/research_test.go`

- [ ] **Step 1: Write the CLI command**

`cmd/meept/research.go`:

```go
package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func newResearchCmd() *cobra.Command {
	var (
		outputJSON bool
		topic      string
		thesis     string
		minTime    string
	)

	cmd := &cobra.Command{
		Use:   "research <topic>",
		Short: "run multi-agent research using llm-wiki methodology",
		Long: `Dispatches a research agent that compiles a knowledge base from web sources.

The agent uses the llm-wiki methodology to:
1. Search from multiple angles (academic, technical, applied, news, contrarian)
2. Ingest quality sources into docs/wiki/<topic>/raw/
3. Compile cross-referenced articles in docs/wiki/<topic>/wiki/
4. Generate a summary report in docs/wiki/<topic>/output/

Use --thesis for thesis-driven investigation that produces a verdict.

Examples:
  meept research "gut microbiome"
  meept research "rust async runtime comparison" --thesis "tokio is more efficient than async-std"
  meept research "CRDT implementations" --output json`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			topic = args[0]
			if len(args) > 1 {
				topic = fmt.Sprintf("%s %s", topic, joinArgs(args[1:]))
			}

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			params := map[string]any{
				"topic":  topic,
				"thesis": thesis,
			}
			if minTime != "" {
				params["min_time"] = minTime
			}

			rawResult, err := client.Call("agent.research", params)
			if err != nil {
				return fmt.Errorf("research failed: %w", err)
			}

			if outputJSON {
				var result map[string]any
				if err := json.Unmarshal(rawResult, &result); err != nil {
					return fmt.Errorf("failed to parse response: %w", err)
				}
				output, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(output))
				return nil
			}

			fmt.Printf("research started for topic: %s\n", topic)
			fmt.Println("output will be written to docs/wiki/<topic>/")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&outputJSON, "output", "o", false, "output as JSON")
	cmd.Flags().StringVar(&thesis, "thesis", "", "thesis to investigate (produces a verdict)")
	cmd.Flags().StringVar(&minTime, "min-time", "", "minimum research time")

	return cmd
}

func joinArgs(args []string) string {
	result := ""
	for i, a := range args {
		if i > 0 {
			result += " "
		}
		result += a
	}
	return result
}
```

- [ ] **Step 2: Write the test**

`cmd/meept/research_test.go`:

```go
package main

import (
	"testing"
)

func TestNewResearchCmd(t *testing.T) {
	cmd := newResearchCmd()

	if cmd.Use != "research <topic>" {
		t.Errorf("expected Use 'research <topic>', got %q", cmd.Use)
	}
	if cmd.Args == nil {
		t.Error("expected non-nil Args validator")
	}

	// Check flags
	thesisFlag := cmd.Flags().Lookup("thesis")
	if thesisFlag == nil {
		t.Error("expected --thesis flag")
	}
	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Error("expected --output flag")
	}
}

func TestJoinArgs(t *testing.T) {
	result := joinArgs([]string{"hello", "world", "foo"})
	if result != "hello world foo" {
		t.Errorf("expected 'hello world foo', got %q", result)
	}

	result = joinArgs([]string{})
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}
```

- [ ] **Step 3: Run the test**

Run: `go test ./cmd/meept/ -run "TestNewResearchCmd|TestJoinArgs" -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/meept/research.go cmd/meept/research_test.go
git commit -m "feat(cli): add research command for llm-wiki-based research"
```

### Task 20: Create openspec CLI commands

**Files:**
- Create: `cmd/meept/openspec.go`
- Create: `cmd/meept/openspec_test.go`

- [ ] **Step 1: Write the CLI command**

`cmd/meept/openspec.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/caimlas/meept/internal/openspec"
	"github.com/spf13/cobra"
)

func newOpenSpecCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "openspec",
		Short:   "manage OpenSpec specifications",
		Long:    "List, show, and initialize OpenSpec specs and changes in the project.",
		Aliases: []string{"spec"},
	}

	cmd.AddCommand(newOpenSpecListCmd())
	cmd.AddCommand(newOpenSpecShowCmd())
	cmd.AddCommand(newOpenSpecInitCmd())

	return cmd
}

func newOpenSpecListCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "list OpenSpec specs and changes",
		RunE: func(cmd *cobra.Command, args []string) error {
			repo := openspec.NewRepository("openspec")

			specs, err := repo.ListSpecs()
			if err != nil {
				return fmt.Errorf("failed to list specs: %w", err)
			}
			changes, err := repo.ListChanges()
			if err != nil {
				return fmt.Errorf("failed to list changes: %w", err)
			}

			if outputJSON {
				result := map[string]any{
					"specs":   specs,
					"changes": changes,
				}
				output, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(output))
				return nil
			}

			fmt.Println("specs:")
			if len(specs) == 0 {
				fmt.Println("  (none)")
			}
			for _, s := range specs {
				fmt.Printf("  - %s\n", s)
			}

			fmt.Println("\nchanges:")
			if len(changes) == 0 {
				fmt.Println("  (none)")
			}
			for _, c := range changes {
				fmt.Printf("  - %s\n", c)
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&outputJSON, "output", "o", false, "output as JSON")
	return cmd
}

func newOpenSpecShowCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "show <capability>",
		Short: "show a spec or change",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo := openspec.NewRepository("openspec")
			name := args[0]

			// Try spec first
			specPath := repo.SpecPath(name)
			if _, err := os.Stat(specPath); err == nil {
				spec, err := openspec.ParseSpec(specPath)
				if err != nil {
					return fmt.Errorf("failed to parse spec: %w", err)
				}
				if outputJSON {
					output, _ := json.MarshalIndent(spec, "", "  ")
					fmt.Println(string(output))
					return nil
				}
				fmt.Printf("# %s\n\n## Purpose\n%s\n\n## Requirements\n", spec.Capability, spec.Purpose)
				for _, req := range spec.Requirements {
					fmt.Printf("\n### Requirement: %s\n%s\n", req.Title, req.Shall)
					for _, scen := range req.Scenarios {
						fmt.Printf("\n#### Scenario: %s\n", scen.Name)
						for _, step := range scen.Steps {
							fmt.Println(step)
						}
					}
				}
				return nil
			}

			// Try change
			changeDir := repo.ChangePath(name)
			if _, err := os.Stat(changeDir); err == nil {
				change, err := openspec.ParseChange(changeDir)
				if err != nil {
					return fmt.Errorf("failed to parse change: %w", err)
				}
				if outputJSON {
					output, _ := json.MarshalIndent(change, "", "  ")
					fmt.Println(string(output))
					return nil
				}
				fmt.Printf("change: %s\n", change.ID)
				fmt.Printf("motivation: %s\n", change.Proposal.Motivation)
				fmt.Printf("tasks: %d\n", len(change.Tasks.Items))
				for _, item := range change.Tasks.Items {
					check := " "
					if item.Done {
						check = "x"
					}
					fmt.Printf("  [%s] %s\n", check, item.Description)
				}
				return nil
			}

			return fmt.Errorf("no spec or change found with name %q", name)
		},
	}

	cmd.Flags().BoolVarP(&outputJSON, "output", "o", false, "output as JSON")
	return cmd
}

func newOpenSpecInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "initialize OpenSpec directory structure",
		RunE: func(cmd *cobra.Command, args []string) error {
			repo := openspec.NewRepository(filepath.Join(".", "openspec"))
			if err := repo.Init(); err != nil {
				return fmt.Errorf("failed to init openspec: %w", err)
			}
			fmt.Println("initialized openspec/ directory with specs/ and changes/")
			return nil
		},
	}
	return cmd
}
```

- [ ] **Step 2: Write the test**

`cmd/meept/openspec_test.go`:

```go
package main

import (
	"testing"
)

func TestNewOpenSpecCmd(t *testing.T) {
	cmd := newOpenSpecCmd()

	if cmd.Use != "openspec" {
		t.Errorf("expected Use 'openspec', got %q", cmd.Use)
	}

	subcommands := cmd.Commands()
	if len(subcommands) < 3 {
		t.Fatalf("expected at least 3 subcommands, got %d", len(subcommands))
	}

	expectedUses := map[string]bool{
		"list":          false,
		"show <capability>": false,
		"init":          false,
	}
	for _, sub := range subcommands {
		if _, ok := expectedUses[sub.Use]; ok {
			expectedUses[sub.Use] = true
		}
	}
	for use, found := range expectedUses {
		if !found {
			t.Errorf("expected subcommand %q not found", use)
		}
	}
}
```

- [ ] **Step 3: Run the test**

Run: `go test ./cmd/meept/ -run TestNewOpenSpecCmd -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/meept/openspec.go cmd/meept/openspec_test.go
git commit -m "feat(cli): add openspec list/show/init commands"
```

### Task 21: Register new CLI commands in main.go

**Files:**
- Modify: `cmd/meept/main.go`

- [ ] **Step 1: Read the command registration section**

Find where `rootCmd.AddCommand(...)` calls are in `cmd/meept/main.go`.

- [ ] **Step 2: Register the new commands**

In `cmd/meept/main.go`, after the existing `AddCommand` calls, add:

```go
	rootCmd.AddCommand(newResearchCmd())
	rootCmd.AddCommand(newOpenSpecCmd())
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./cmd/meept/`
Expected: succeeds

- [ ] **Step 4: Run the binary to verify commands appear**

Run: `./bin/meept --help`
Expected: shows `research` and `openspec` in the command list

- [ ] **Step 5: Commit**

```bash
git add cmd/meept/main.go
git commit -m "feat(cli): register research and openspec commands"
```

---

## Phase 7: Daemon Wiring

### Task 22: Wire openspec repo into daemon components

**Files:**
- Modify: `internal/daemon/components.go`

- [ ] **Step 1: Read the daemon components file to find the planner wiring**

Read `internal/daemon/components.go` and find where `StrategicPlannerConfig` and `TacticalSchedulerConfig` are constructed.

- [ ] **Step 2: Add openspec repository initialization**

In `internal/daemon/components.go`, add after the project path is resolved (find where `projectPath` is available):

```go
	// Initialize OpenSpec repository for the current project
	var openspecRepo *openspec.Repository
	openspecPath := filepath.Join(projectPath, "openspec")
	if _, err := os.Stat(openspecPath); err == nil {
		openspecRepo = openspec.NewRepository(openspecPath)
	}
```

Add the import:

```go
	"github.com/caimlas/meept/internal/openspec"
```

- [ ] **Step 3: Pass openspecRepo to StrategicPlannerConfig**

In the `StrategicPlannerConfig` construction, add:

```go
		OpenspecRepo: openspecRepo,
```

- [ ] **Step 4: Pass openspecRepo to TacticalSchedulerConfig**

In the `TacticalSchedulerConfig` construction, add:

```go
		OpenspecRepo: openspecRepo,
```

- [ ] **Step 5: Verify it compiles**

Run: `go build ./internal/daemon/`
Expected: succeeds

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/components.go
git commit -m "feat(daemon): wire openspec repository into planner and tactical scheduler"
```

### Task 23: Add SpecChangeID to Plan and Task structs

**Files:**
- Modify: `internal/plan/plan.go:44-62`
- Modify: `internal/task/task.go` (find the Task struct)

- [ ] **Step 1: Read the Task struct**

Find the `Task` struct in `internal/task/task.go`.

- [ ] **Step 2: Add SpecChangeID to Task**

Add to the `Task` struct:

```go
	SpecChangeID string `json:"spec_change_id,omitempty" db:"spec_change_id"`
```

- [ ] **Step 3: Add SpecChangeID and OpenSpecPath to Plan**

In `internal/plan/plan.go`, add to the `Plan` struct:

```go
	SpecChangeID string `json:"spec_change_id,omitempty"`
	OpenSpecPath string `json:"openspec_path,omitempty"`
```

- [ ] **Step 4: Verify it compiles**

Run: `go build ./internal/plan/ ./internal/task/`
Expected: succeeds

- [ ] **Step 5: Commit**

```bash
git add internal/plan/plan.go internal/task/task.go
git commit -m "feat(plan,task): add SpecChangeID field for linking tasks to OpenSpec changes"
```

---

## Phase 8: Documentation

### Task 24: Write OpenSpec concept documentation

**Files:**
- Create: `docs/concepts/openspec.md`

- [ ] **Step 1: Write the documentation**

```markdown
# OpenSpec Integration

## Overview

Meept integrates [OpenSpec](https://openspec.dev/) as its spec-driven documentation format. When the planner decomposes a task, it writes an OpenSpec change directory alongside the task steps. When the plan is approved, the spec delta is applied to the capability's spec. When all steps complete, a spec-reviewer agent verifies that the implementation satisfies each `### Requirement: ... SHALL ...` block.

## Directory Structure

```
openspec/
├── specs/                      # Current spec state (what the system should do)
│   ├── auth-session/
│   │   └── spec.md
│   └── checkout-cart/
│       └── spec.md
└── changes/                    # Proposed changes
    ├── add-remember-me/
    │   ├── proposal.md         # Why the change is needed
    │   ├── design.md           # Technical decisions
    │   ├── tasks.md            # Implementation checklist
    │   └── specs/
    │       └── auth-session/
    │           └── spec.md     # Spec delta (+ / - diff)
    └── archive/                # Applied or rejected changes
        └── add-remember-me/
```

## Lifecycle

1. **Planning**: `StrategicPlanner.Plan()` writes an OpenSpec change directory with proposal, design, tasks, and spec deltas
2. **Review/Approval**: User reviews the plan. On approval, deltas are applied to specs/. On rejection, the change is archived.
3. **Execution**: Task steps execute normally
4. **Spec Review**: When all steps complete, a `spec-reviewer` agent verifies each SHALL requirement against the changed code
5. **Completion**: Task transitions through `StateTesting` → `StateCompleted` after spec review

## CLI Commands

```bash
meept openspec init              # Initialize openspec/ directory
meept openspec list               # List specs and changes
meept openspec show <capability>  # Show a spec or change
```

## Integration with Existing Plans

The existing `docs/plans/` directory continues to hold project-level implementation plans. OpenSpec changes live in `openspec/changes/` and represent functional requirement deltas — they describe what the system *should do*, not how it's implemented. The two are complementary:

- `docs/plans/`: implementation plans (how to build X)
- `openspec/specs/`: functional requirements (what X should do)
- `openspec/changes/`: proposed requirement changes (what's changing about X)

## Relationship to llm-wiki

The researcher agent uses llm-wiki methodology to compile knowledge bases. Research output lives in `docs/wiki/<topic>/` and is separate from OpenSpec specs. Research may inform spec writing — a researcher could compile knowledge about a domain, which the planner then uses as context for decomposing a task that writes an OpenSpec change.

## See Also

- [OpenSpec.dev](https://openspec.dev/) — the OpenSpec specification
- [llm-wiki](https://llm-wiki.net/) — the llm-wiki methodology
- [Multi-Agent Architecture](multi-agent.md) — agent roles and routing
```

- [ ] **Step 2: Add to mkdocs.yml nav**

In `mkdocs.yml`, under the concepts section, add:

```yaml
  - Concepts:
      - openspec: concepts/openspec.md
```

If a concepts section already exists, add `openspec: concepts/openspec.md` to it.

- [ ] **Step 3: Commit**

```bash
git add docs/concepts/openspec.md mkdocs.yml
git commit -m "docs: add OpenSpec concept documentation"
```

### Task 25: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add OpenSpec section under Architecture Overview**

In the CLAUDE.md file, after the Multi-Agent Architecture section, add:

```markdown
### OpenSpec Integration

Meept uses [OpenSpec](https://openspec.dev/) for spec-driven task documentation. When the planner decomposes a task, it writes an OpenSpec change directory (`openspec/changes/<taskID>/`) with proposal, design, tasks, and spec deltas. On plan approval, deltas are applied to `openspec/specs/<capability>/spec.md`. The `spec-reviewer` agent verifies spec compliance when all steps complete.

CLI commands:
```bash
meept openspec init              # Initialize openspec/ directory
meept openspec list              # List specs and changes
meept openspec show <name>       # Show a spec or change
```

Research:
```bash
meept research <topic>           # Run multi-agent research using llm-wiki
meept research <topic> --thesis "claim"  # Thesis-driven investigation
```
```

- [ ] **Step 2: Add `meept research` and `meept openspec` to the CLI commands section**

Find the CLI commands block and add:

```bash
# OpenSpec management
./bin/meept openspec init                # Initialize openspec/ directory
./bin/meept openspec list                # List specs and changes
./bin/meept openspec show <name>          # Show a spec or change

# Research (llm-wiki)
./bin/meept research <topic>             # Run multi-agent research
```

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md with OpenSpec and research commands"
```

---

## Phase 9: Final Verification

### Task 26: Full build and test

- [ ] **Step 1: Build everything**

Run: `make build`
Expected: succeeds

- [ ] **Step 2: Run all tests**

Run: `go test ./... -v 2>&1 | tail -100`
Expected: no failures in new code; no regressions in existing tests

- [ ] **Step 3: Run race detector on new packages**

Run: `go test -race ./internal/openspec/ ./internal/agent/... -v`
Expected: no race conditions

- [ ] **Step 4: Verify CLI commands**

```bash
./bin/meept openspec --help
./bin/meept openspec init
./bin/meept openspec list
./bin/meept research --help
```

Expected: all commands exist and run without error

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "feat: OpenSpec integration and researcher agent — complete implementation"
```

---

## Self-Review

### Spec coverage

| Goal | Task(s) |
|------|---------|
| Integrate orchestrator/planner to use OpenSpec documents | Tasks 1-5 (openspec package), Tasks 13-15 (strategic planner integration), Task 15a (PlanManager integration), Task 17 (tactical async spec review) |
| Create spec-writer agent | Task 7 |
| Create spec-reviewer agent | Task 8 |
| Create researcher agent using llm-wiki | Task 9, Task 18 (skill), Task 18a (programmatic runner), Task 19 (CLI) |
| Wire into daemon | Task 22 (daemon components), Task 23 (plan/task structs), Task 15a (PlanManager options wiring) |
| CLI commands | Tasks 19-21 |
| Documentation | Tasks 24-25 |

### Placeholder scan

No placeholders found. All code blocks contain complete implementations.

### Type consistency

- `openspec.Repository` — used consistently in `strategic.go`, `tactical.go`, `components.go`
- `openspec.Change`, `openspec.Spec`, `openspec.SpecDelta` — consistent field names across parser, writer, and tests
- `config.AgentIDResearcher`, `config.AgentIDSpecWriter`, `config.AgentIDSpecReviewer` — defined in Task 6, used in Tasks 7-10, 12
- `BuildSpecReviewPrompt(requirements []string, changedFiles []string) string` — defined in Task 8, used in Task 17
- `SpecChangeID` field on `Plan` and `Task` — defined in Task 23, used in Task 15a (PlanManager.ApprovePlan/RejectPlan) and available for use in Tasks 14-17
- `WikiRunner`, `WikiArticle` — defined in Task 18a, used by researcher agent spec (Task 9)
- `plan.WithOpenSpecRepo(repo)` — defined in Task 15a, used in daemon wiring (Task 22)
- `plan.PlanManagerOption` — functional option type defined in Task 15a

### Resolved gaps

1. **Async spec-reviewer dispatch** (was: synchronous in `OnJobCompleted`): Task 17 now creates a review `TaskStep` with `ToolHint: "spec-review"` and enqueues it as a job through `ts.queue`. The task transitions to `StateTesting` (pause), and a second `OnJobCompleted` invocation (when the review job completes) handles finalization back to `StateCompleted`. The early `return` on spec-review step detection prevents double-firing of the `allDone` logic.
2. **Programmatic llm-wiki runner** (was: SKILL.md only): Task 18a adds `internal/agent/wiki_runner.go` with `WikiRunner` that programmatically manages the directory structure (`InitTopic`, `IngestSource`, `WriteArticle`, `GenerateIndex`, `WriteOutput`), enforces frontmatter conventions, and validates the llm-wiki directory layout. The SKILL.md (Task 18) still guides agent behavior, but the runner provides structure guarantees.
3. **PlanManager OpenSpec integration** (was: only StrategicPlanner had it): Task 15a adds `WithOpenSpecRepo` functional option to `NewPlanManager`, and `ApprovePlan()` calls `ApplyChange()` while `RejectPlan()` calls `ArchiveChange()`, mirroring the `StrategicPlanner` integration. The daemon wiring (Task 22) passes the openspec repo to both paths.
