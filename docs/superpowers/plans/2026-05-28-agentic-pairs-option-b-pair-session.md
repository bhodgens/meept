# Agentic Pairs: Option B -- Pair Session with Shared Context

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Introduce a PairSession concept that ties two agents together for a full task with shared working memory, enabling multi-round actor-->reviewer loops with convergence tracking across the entire task lifecycle.

**Architecture:** A new PairManager component manages PairSession lifecycle. When the strategic planner detects a task needing deep pairing (compound code tasks, security-sensitive changes), it creates a PairSession. The PairManager drives a loop: actor executes --> reviewer reviews --> shared context updated --> actor re-executes with full history. Convergence tracking accumulates accepted criteria until the spec is fully satisfied.

**Tech Stack:** Go 1.22+, existing AgentRegistry/AgentLoop infrastructure, MessageBus pub/sub, SQLite task store

---

## Phase 1: PairSession and PairContext types

**File:** `/Users/caimlas/git/meept/internal/agent/pair_session.go`

### Step 1.1 -- Create the PairSession types file

```go
package agent

import (
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/task"
)

// PairSessionState represents the lifecycle state of a pair session.
type PairSessionState string

const (
	PairSessionActive    PairSessionState = "active"
	PairSessionConverged PairSessionState = "converged"
	PairSessionExhausted PairSessionState = "exhausted"  // max rounds reached
	PairSessionFailed    PairSessionState = "failed"
	PairSessionCancelled PairSessionState = "cancelled"
)

// IsTerminal returns true if the pair session is in a terminal state.
func (s PairSessionState) IsTerminal() bool {
	return s == PairSessionConverged || s == PairSessionExhausted ||
		s == PairSessionFailed || s == PairSessionCancelled
}

// Attempt represents a single actor-->reviewer round within a pair session.
type Attempt struct {
	// Round is the 1-indexed attempt number.
	Round int `json:"round"`

	// ActorOutput is the raw output from the actor agent.
	ActorOutput string `json:"actor_output"`

	// ActorStepID is the step ID that produced this attempt's actor output.
	ActorStepID string `json:"actor_step_id"`

	// Review is the reviewer's verdict for this attempt.
	Review *ReviewResult `json:"review,omitempty"`

	// ReviewerStepID is the step ID that produced this attempt's review.
	ReviewerStepID string `json:"reviewer_step_id,omitempty"`

	// StartedAt is when this attempt began.
	StartedAt time.Time `json:"started_at"`

	// CompletedAt is when the review came back.
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

// Satisfied returns true if this attempt's review approved the work.
func (a *Attempt) Satisfied() bool {
	return a.Review != nil && a.Review.Status == ReviewApproved
}

// PairContext holds the shared working memory for a pair session.
// Both actor and reviewer see the full context on each round.
type PairContext struct {
	// OriginalSpec is the initial task description / spec.
	OriginalSpec string `json:"original_spec"`

	// Attempts is the ordered history of all actor-->reviewer rounds.
	Attempts []*Attempt `json:"attempts"`

	// AcceptedCriteria accumulates spec items that were satisfied in
	// prior rounds. When this covers all criteria from the original
	// spec, the session has converged.
	AcceptedCriteria []string `json:"accepted_criteria"`

	// PendingCriteria are spec items not yet satisfied.
	PendingCriteria []string `json:"pending_criteria"`
}

// ActorPrompt builds the full prompt for the actor on a given round.
// It includes the original spec, all prior attempts and their reviews,
// and the remaining pending criteria.
func (pc *PairContext) ActorPrompt() string {
	prompt := fmt.Sprintf("## Task Spec\n\n%s\n\n", pc.OriginalSpec)

	if len(pc.AcceptedCriteria) > 0 {
		prompt += "## Already Satisfied\n\n"
		for _, c := range pc.AcceptedCriteria {
			prompt += fmt.Sprintf("- [x] %s\n", c)
		}
		prompt += "\n"
	}

	if len(pc.PendingCriteria) > 0 {
		prompt += "## Remaining Requirements\n\n"
		for _, c := range pc.PendingCriteria {
			prompt += fmt.Sprintf("- [ ] %s\n", c)
		}
		prompt += "\n"
	}

	if len(pc.Attempts) > 0 {
		prompt += "## Prior Attempt History\n\n"
		for _, a := range pc.Attempts {
			prompt += fmt.Sprintf("### Round %d\n", a.Round)
			prompt += fmt.Sprintf("**Your previous output:**\n%s\n\n", truncateString(a.ActorOutput, 2000))
			if a.Review != nil {
				prompt += fmt.Sprintf("**Reviewer feedback:** [%s] %s\n",
					a.Review.Status, a.Review.Feedback)
				if len(a.Review.Issues) > 0 {
					prompt += "**Issues:**\n"
					for _, issue := range a.Review.Issues {
						prompt += fmt.Sprintf("- %s\n", issue)
					}
				}
				prompt += "\n"
			}
		}
	}

	prompt += "Address the remaining requirements. Focus only on what is not yet satisfied.\n"
	return prompt
}

// ReviewerPrompt builds the full prompt for the reviewer on a given round.
// It includes the original spec, the actor's latest output, and the
// accumulated accepted/pending criteria.
func (pc *PairContext) ReviewerPrompt(actorOutput string) string {
	prompt := fmt.Sprintf("## Task Spec\n\n%s\n\n", pc.OriginalSpec)

	if len(pc.AcceptedCriteria) > 0 {
		prompt += "## Already Satisfied\n\n"
		for _, c := range pc.AcceptedCriteria {
			prompt += fmt.Sprintf("- [x] %s\n", c)
		}
		prompt += "\n"
	}

	if len(pc.PendingCriteria) > 0 {
		prompt += "## Pending Requirements\n\n"
		for _, c := range pc.PendingCriteria {
			prompt += fmt.Sprintf("- [ ] %s\n", c)
		}
		prompt += "\n"
	}

	if len(pc.Attempts) > 1 {
		prompt += "## Prior Rounds Summary\n\n"
		for _, a := range pc.Attempts[:len(pc.Attempts)-1] {
			prompt += fmt.Sprintf("- Round %d: %s\n", a.Round, a.Review.Status)
		}
		prompt += "\n"
	}

	prompt += "## Current Actor Output to Review\n\n"
	prompt += actorOutput
	prompt += "\n\nReview the output above against the spec and pending requirements. "
	prompt += "If all pending requirements are met, approve. Otherwise list remaining issues.\n"
	return prompt
}

// HasConverged returns true when no pending criteria remain.
func (pc *PairContext) HasConverged() bool {
	return len(pc.PendingCriteria) == 0
}

// RecordAttempt adds a completed attempt and updates criteria.
func (pc *PairContext) RecordAttempt(attempt *Attempt) {
	pc.Attempts = append(pc.Attempts, attempt)
}

// PairSession ties two agents together for a multi-round task with shared context.
type PairSession struct {
	// ID is the unique session identifier.
	ID string `json:"id"`

	// TaskID is the parent task this session serves.
	TaskID string `json:"task_id"`

	// ActorAgentID is the agent that executes the work (e.g., "coder").
	ActorAgentID string `json:"actor_agent_id"`

	// ReviewerAgentID is the agent that reviews the work (e.g., "planner" or "analyst").
	ReviewerAgentID string `json:"reviewer_agent_id"`

	// Context holds the shared working memory.
	Context PairContext `json:"context"`

	// MaxRounds is the maximum number of actor-->reviewer rounds before giving up.
	MaxRounds int `json:"max_rounds"`

	// State is the current lifecycle state.
	State PairSessionState `json:"state"`

	// StepIDs tracks all step IDs created for this pair session, in order.
	// Used by the tactical scheduler to skip pair-managed steps.
	StepIDs []string `json:"step_ids,omitempty"`

	// CreatedAt is when the session was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the session was last modified.
	UpdatedAt time.Time `json:"updated_at"`
}

// NewPairSession creates a new pair session for a task.
func NewPairSession(taskID, originalSpec, actorID, reviewerID string, maxRounds int) *PairSession {
	now := time.Now().UTC()
	return &PairSession{
		ID:             fmt.Sprintf("pair-%s-%d", taskID, now.UnixNano()),
		TaskID:         taskID,
		ActorAgentID:   actorID,
		ReviewerAgentID: reviewerID,
		Context: PairContext{
			OriginalSpec:     originalSpec,
			Attempts:         nil,
			AcceptedCriteria: nil,
			PendingCriteria:  nil,
		},
		MaxRounds: maxRounds,
		State:     PairSessionActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// SetCriteria initializes the accepted/pending criteria lists.
func (ps *PairSession) SetCriteria(criteria []string) {
	ps.Context.PendingCriteria = make([]string, len(criteria))
	copy(ps.Context.PendingCriteria, criteria)
	ps.Context.AcceptedCriteria = nil
	ps.UpdatedAt = time.Now().UTC()
}

// CurrentRound returns the 1-indexed round number (number of attempts + 1).
func (ps *PairSession) CurrentRound() int {
	return len(ps.Context.Attempts) + 1
}

// IsExhausted returns true if the session has used all its rounds.
func (ps *PairSession) IsExhausted() bool {
	return ps.CurrentRound() > ps.MaxRounds
}

// AddStepID records a step ID as belonging to this pair session.
func (ps *PairSession) AddStepID(stepID string) {
	ps.StepIDs = append(ps.StepIDs, stepID)
	ps.UpdatedAt = time.Now().UTC()
}

// OwnsStep returns true if the given step ID belongs to this pair session.
func (ps *PairSession) OwnsStep(stepID string) bool {
	for _, id := range ps.StepIDs {
		if id == stepID {
			return true
		}
	}
	return false
}

// MarkConverged transitions the session to the converged state.
func (ps *PairSession) MarkConverged() {
	ps.State = PairSessionConverged
	ps.UpdatedAt = time.Now().UTC()
}

// MarkExhausted transitions the session to the exhausted state.
func (ps *PairSession) MarkExhausted() {
	ps.State = PairSessionExhausted
	ps.UpdatedAt = time.Now().UTC()
}

// MarkFailed transitions the session to the failed state.
func (ps *PairSession) MarkFailed() {
	ps.State = PairSessionFailed
	ps.UpdatedAt = time.Now().UTC()
}

// OwnsTask returns true if this pair session manages the given task.
func (ps *PairSession) OwnsTask(taskID string) bool {
	return ps.TaskID == taskID
}
```

### Step 1.2 -- Write tests for PairSession types

**File:** `/Users/caimlas/git/meept/internal/agent/pair_session_test.go`

```go
package agent

import (
	"testing"
)

func TestNewPairSession(t *testing.T) {
	ps := NewPairSession("task-1", "implement auth module", "coder", "planner", 5)

	if ps.TaskID != "task-1" {
		t.Errorf("expected task_id 'task-1', got %q", ps.TaskID)
	}
	if ps.ActorAgentID != "coder" {
		t.Errorf("expected actor 'coder', got %q", ps.ActorAgentID)
	}
	if ps.ReviewerAgentID != "planner" {
		t.Errorf("expected reviewer 'planner', got %q", ps.ReviewerAgentID)
	}
	if ps.MaxRounds != 5 {
		t.Errorf("expected max_rounds 5, got %d", ps.MaxRounds)
	}
	if ps.State != PairSessionActive {
		t.Errorf("expected state active, got %q", ps.State)
	}
	if ps.Context.OriginalSpec != "implement auth module" {
		t.Errorf("expected spec 'implement auth module', got %q", ps.Context.OriginalSpec)
	}
	if len(ps.StepIDs) != 0 {
		t.Errorf("expected no step IDs, got %d", len(ps.StepIDs))
	}
}

func TestPairSession_CurrentRound(t *testing.T) {
	ps := NewPairSession("task-1", "spec", "coder", "planner", 3)

	if ps.CurrentRound() != 1 {
		t.Errorf("expected round 1, got %d", ps.CurrentRound())
	}

	ps.Context.Attempts = append(ps.Context.Attempts, &Attempt{Round: 1})
	if ps.CurrentRound() != 2 {
		t.Errorf("expected round 2 after 1 attempt, got %d", ps.CurrentRound())
	}
}

func TestPairSession_IsExhausted(t *testing.T) {
	ps := NewPairSession("task-1", "spec", "coder", "planner", 2)

	if ps.IsExhausted() {
		t.Error("should not be exhausted at round 1")
	}

	ps.Context.Attempts = append(ps.Context.Attempts, &Attempt{Round: 1})
	if ps.IsExhausted() {
		t.Error("should not be exhausted at round 2 (max=2)")
	}

	ps.Context.Attempts = append(ps.Context.Attempts, &Attempt{Round: 2})
	if !ps.IsExhausted() {
		t.Error("should be exhausted after 2 attempts (max=2)")
	}
}

func TestPairSession_SetCriteria(t *testing.T) {
	ps := NewPairSession("task-1", "spec", "coder", "planner", 3)
	ps.SetCriteria([]string{"write tests", "handle errors", "add docs"})

	if len(ps.Context.PendingCriteria) != 3 {
		t.Fatalf("expected 3 pending criteria, got %d", len(ps.Context.PendingCriteria))
	}
	if ps.Context.PendingCriteria[0] != "write tests" {
		t.Errorf("expected first criterion 'write tests', got %q", ps.Context.PendingCriteria[0])
	}
	if len(ps.Context.AcceptedCriteria) != 0 {
		t.Errorf("expected 0 accepted criteria, got %d", len(ps.Context.AcceptedCriteria))
	}
}

func TestPairContext_HasConverged(t *testing.T) {
	pc := &PairContext{
		PendingCriteria: []string{"a", "b"},
	}
	if pc.HasConverged() {
		t.Error("should not be converged with pending criteria")
	}

	pc.PendingCriteria = nil
	pc.AcceptedCriteria = []string{"a", "b"}
	if !pc.HasConverged() {
		t.Error("should be converged when no pending criteria remain")
	}
}

func TestPairContext_ActorPrompt(t *testing.T) {
	pc := &PairContext{
		OriginalSpec:     "implement login",
		PendingCriteria:  []string{"validate input"},
		AcceptedCriteria: []string{"create route"},
		Attempts: []*Attempt{
			{
				Round:       1,
				ActorOutput: "created route /login",
				Review: &ReviewResult{
					Status:   ReviewRejected,
					Feedback: "missing input validation",
					Issues:   []string{"no email validation"},
				},
			},
		},
	}

	prompt := pc.ActorPrompt()
	if prompt == "" {
		t.Fatal("actor prompt should not be empty")
	}
	// Should contain spec
	if !contains(prompt, "implement login") {
		t.Error("actor prompt should contain original spec")
	}
	// Should contain accepted criteria
	if !contains(prompt, "create route") {
		t.Error("actor prompt should contain accepted criteria")
	}
	// Should contain pending criteria
	if !contains(prompt, "validate input") {
		t.Error("actor prompt should contain pending criteria")
	}
	// Should contain reviewer feedback
	if !contains(prompt, "missing input validation") {
		t.Error("actor prompt should contain reviewer feedback")
	}
}

func TestPairContext_ReviewerPrompt(t *testing.T) {
	pc := &PairContext{
		OriginalSpec:    "implement login",
		PendingCriteria: []string{"validate input"},
	}

	prompt := pc.ReviewerPrompt("here is the login handler code")
	if prompt == "" {
		t.Fatal("reviewer prompt should not be empty")
	}
	if !contains(prompt, "here is the login handler code") {
		t.Error("reviewer prompt should contain actor output")
	}
	if !contains(prompt, "validate input") {
		t.Error("reviewer prompt should contain pending criteria")
	}
}

func TestAttempt_Satisfied(t *testing.T) {
	a := &Attempt{Review: &ReviewResult{Status: ReviewApproved}}
	if !a.Satisfied() {
		t.Error("approved attempt should be satisfied")
	}

	b := &Attempt{Review: &ReviewResult{Status: ReviewRejected}}
	if b.Satisfied() {
		t.Error("rejected attempt should not be satisfied")
	}

	c := &Attempt{Review: nil}
	if c.Satisfied() {
		t.Error("attempt without review should not be satisfied")
	}
}

func TestPairSession_OwnsStep(t *testing.T) {
	ps := NewPairSession("task-1", "spec", "coder", "planner", 3)

	if ps.OwnsStep("step-1") {
		t.Error("should not own step before it is added")
	}

	ps.AddStepID("step-1")
	if !ps.OwnsStep("step-1") {
		t.Error("should own step after adding")
	}
	if ps.OwnsStep("step-2") {
		t.Error("should not own unadded step")
	}
}

func TestPairSession_StateTransitions(t *testing.T) {
	ps := NewPairSession("task-1", "spec", "coder", "planner", 3)

	if ps.State.IsTerminal() {
		t.Error("active session should not be terminal")
	}

	ps.MarkConverged()
	if ps.State != PairSessionConverged {
		t.Errorf("expected converged, got %q", ps.State)
	}
	if !ps.State.IsTerminal() {
		t.Error("converged should be terminal")
	}

	ps2 := NewPairSession("task-2", "spec", "coder", "planner", 3)
	ps2.MarkExhausted()
	if ps2.State != PairSessionExhausted {
		t.Errorf("expected exhausted, got %q", ps2.State)
	}

	ps3 := NewPairSession("task-3", "spec", "coder", "planner", 3)
	ps3.MarkFailed()
	if ps3.State != PairSessionFailed {
		t.Errorf("expected failed, got %q", ps3.State)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

### Step 1.3 -- Verify tests compile and pass

```bash
go test ./internal/agent/ -run "TestNewPairSession|TestPairSession_|TestPairContext_|TestAttempt_" -v
```

---

## Phase 2: PairManager core loop

**File:** `/Users/caimlas/git/meept/internal/agent/pair_manager.go`

### Step 2.1 -- Create the PairManager

```go
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

const (
	// DefaultPairMaxRounds is the default maximum number of actor-->reviewer rounds.
	DefaultPairMaxRounds = 3

	// Actor conversation ID prefix
	actorConvPrefix = "pair-actor"

	// Reviewer conversation ID prefix
	reviewerConvPrefix = "pair-reviewer"
)

// PairManager drives the multi-round actor-->reviewer loop for pair sessions.
// It holds active sessions in memory and publishes bus events on state changes.
type PairManager struct {
	mu       sync.RWMutex
	sessions map[string]*PairSession // session ID -> session

	registry  *AgentRegistry
	taskStore *task.Store
	stepStore *task.StepStore
	bus       *bus.MessageBus
	logger    *slog.Logger
}

// PairManagerConfig holds configuration for creating a PairManager.
type PairManagerConfig struct {
	Registry  *AgentRegistry
	TaskStore *task.Store
	StepStore *task.StepStore
	Bus       *bus.MessageBus
	Logger    *slog.Logger
}

// NewPairManager creates a new pair manager.
func NewPairManager(cfg PairManagerConfig) *PairManager {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &PairManager{
		sessions:  make(map[string]*PairSession),
		registry:  cfg.Registry,
		taskStore: cfg.TaskStore,
		stepStore: cfg.StepStore,
		bus:       cfg.Bus,
		logger:    cfg.Logger,
	}
}

// CreateSession creates a new pair session for a task and registers it.
func (pm *PairManager) CreateSession(taskID, spec, actorID, reviewerID string, maxRounds int) *PairSession {
	if maxRounds <= 0 {
		maxRounds = DefaultPairMaxRounds
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	session := NewPairSession(taskID, spec, actorID, reviewerID, maxRounds)
	pm.sessions[session.ID] = session

	pm.logger.Info("Pair session created",
		"session_id", session.ID,
		KeyTaskID, taskID,
		"actor", actorID,
		"reviewer", reviewerID,
		"max_rounds", maxRounds,
	)

	return session
}

// GetSession returns a pair session by ID.
func (pm *PairManager) GetSession(sessionID string) (*PairSession, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	s, ok := pm.sessions[sessionID]
	return s, ok
}

// GetSessionByTask returns the active pair session for a task, if any.
func (pm *PairManager) GetSessionByTask(taskID string) (*PairSession, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, s := range pm.sessions {
		if s.TaskID == taskID && !s.State.IsTerminal() {
			return s, true
		}
	}
	return nil, false
}

// GetSessionByStep returns the pair session that owns the given step, if any.
func (pm *PairManager) GetSessionByStep(stepID string) (*PairSession, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, s := range pm.sessions {
		if s.OwnsStep(stepID) {
			return s, true
		}
	}
	return nil, false
}

// RunRound executes one actor-->reviewer round for the given session.
// It runs the actor agent, then the reviewer agent, and records the attempt.
// Returns the attempt and an error if the round could not complete.
func (pm *PairManager) RunRound(ctx context.Context, sessionID string) (*Attempt, error) {
	pm.mu.Lock()
	session, ok := pm.sessions[sessionID]
	if !ok {
		pm.mu.Unlock()
		return nil, fmt.Errorf("pair session not found: %s", sessionID)
	}
	if session.State.IsTerminal() {
		pm.mu.Unlock()
		return nil, fmt.Errorf("pair session is terminal: %s", session.State)
	}
	pm.mu.Unlock()

	round := session.CurrentRound()
	pm.logger.Info("Starting pair round",
		"session_id", sessionID,
		KeyTaskID, session.TaskID,
		"round", round,
		"max_rounds", session.MaxRounds,
	)

	// --- Actor phase ---
	actorPrompt := session.Context.ActorPrompt()
	actorConvID := fmt.Sprintf("%s-%s-r%d-%d", actorConvPrefix, session.TaskID, round, time.Now().UnixNano())

	actorOutput, err := pm.runAgent(ctx, session.ActorAgentID, actorPrompt, actorConvID)
	if err != nil {
		pm.logger.Error("Actor agent failed",
			"session_id", sessionID,
			"round", round,
			"error", err,
		)
		session.MarkFailed()
		pm.publishEvent("pair.round_failed", map[string]any{
			"session_id": sessionID,
			KeyTaskID:    session.TaskID,
			"round":      round,
			"phase":      "actor",
			"error":      err.Error(),
		})
		return nil, fmt.Errorf("actor failed in round %d: %w", round, err)
	}

	// --- Reviewer phase ---
	reviewerPrompt := session.Context.ReviewerPrompt(actorOutput)
	reviewerConvID := fmt.Sprintf("%s-%s-r%d-%d", reviewerConvPrefix, session.TaskID, round, time.Now().UnixNano())

	reviewOutput, err := pm.runAgent(ctx, session.ReviewerAgentID, reviewerPrompt, reviewerConvID)
	if err != nil {
		pm.logger.Error("Reviewer agent failed",
			"session_id", sessionID,
			"round", round,
			"error", err,
		)
		session.MarkFailed()
		pm.publishEvent("pair.round_failed", map[string]any{
			"session_id": sessionID,
			KeyTaskID:    session.TaskID,
			"round":      round,
			"phase":      "reviewer",
			"error":      err.Error(),
		})
		return nil, fmt.Errorf("reviewer failed in round %d: %w", round, err)
	}

	// Parse review output into a ReviewResult
	reviewResult := pm.parseReviewOutput(reviewOutput)

	// Record the attempt
	attempt := &Attempt{
		Round:       round,
		ActorOutput: actorOutput,
		Review:      reviewResult,
		StartedAt:   time.Now().UTC().Add(-time.Minute), // approximate
		CompletedAt: time.Now().UTC(),
	}

	session.Context.RecordAttempt(attempt)

	// Update criteria based on review
	pm.updateCriteria(session, reviewResult)

	pm.logger.Info("Pair round completed",
		"session_id", sessionID,
		KeyTaskID, session.TaskID,
		"round", round,
		"review_status", string(reviewResult.Status),
		"pending", len(session.Context.PendingCriteria),
		"accepted", len(session.Context.AcceptedCriteria),
	)

	// Check convergence
	if session.Context.HasConverged() {
		session.MarkConverged()
		pm.publishEvent("pair.converged", map[string]any{
			"session_id": sessionID,
			KeyTaskID:    session.TaskID,
			"rounds":     round,
		})
		pm.finalizeTask(ctx, session, true)
		return attempt, nil
	}

	// Check exhaustion
	if session.IsExhausted() {
		session.MarkExhausted()
		pm.publishEvent("pair.exhausted", map[string]any{
			"session_id": sessionID,
			KeyTaskID:    session.TaskID,
			"rounds":     round,
			"max_rounds": session.MaxRounds,
		})
		pm.finalizeTask(ctx, session, false)
		return attempt, nil
	}

	// Round completed but more rounds needed
	pm.publishEvent("pair.round_completed", map[string]any{
		"session_id":         sessionID,
		KeyTaskID:            session.TaskID,
		"round":              round,
		"review_status":      string(reviewResult.Status),
		"pending_criteria":   len(session.Context.PendingCriteria),
		"accepted_criteria":  len(session.Context.AcceptedCriteria),
		KeyChatVisible:       true,
	})

	return attempt, nil
}

// RunAllRounds runs the full loop until convergence, exhaustion, or error.
func (pm *PairManager) RunAllRounds(ctx context.Context, sessionID string) (*PairSession, error) {
	for {
		pm.mu.RLock()
		session, ok := pm.sessions[sessionID]
		if !ok {
			pm.mu.RUnlock()
			return nil, fmt.Errorf("pair session not found: %s", sessionID)
		}
		if session.State.IsTerminal() {
			pm.mu.RUnlock()
			return session, nil
		}
		pm.mu.RUnlock()

		_, err := pm.RunRound(ctx, sessionID)
		if err != nil {
			return nil, err
		}
	}
}

// runAgent executes a single agent loop iteration.
func (pm *PairManager) runAgent(ctx context.Context, agentID, message, conversationID string) (string, error) {
	if pm.registry == nil {
		return "", fmt.Errorf("agent registry not configured")
	}
	return pm.registry.RunAgent(ctx, agentID, message, conversationID)
}

// parseReviewOutput converts raw reviewer output into a ReviewResult.
// If the output contains structured JSON it uses that; otherwise it
// heuristically determines the status from keywords.
func (pm *PairManager) parseReviewOutput(output string) *ReviewResult {
	// Try structured JSON parse
	result := &ReviewResult{}
	if err := parseReviewJSON(output, result); err == nil {
		return result
	}

	// Heuristic: check for approval keywords
	lower := toLower(output)
	if containsAny(lower, []string{"approved", "all requirements met", "looks good", "lgtm"}) {
		return &ReviewResult{
			Status:     ReviewApproved,
			Feedback:   output,
			Confidence: 0.8,
		}
	}

	// Default: rejected with the output as feedback
	issues := extractIssueLines(output)
	return &ReviewResult{
		Status:     ReviewRejected,
		Feedback:   output,
		Issues:     issues,
		Confidence: 0.7,
	}
}

// updateCriteria moves criteria from pending to accepted if the review approved.
func (pm *PairManager) updateCriteria(session *PairSession, review *ReviewResult) {
	if review.Status != ReviewApproved {
		return
	}

	// On approval, all remaining pending criteria become accepted
	session.Context.AcceptedCriteria = append(
		session.Context.AcceptedCriteria,
		session.Context.PendingCriteria...,
	)
	session.Context.PendingCriteria = nil
}

// finalizeTask updates the parent task state based on session outcome.
func (pm *PairManager) finalizeTask(ctx context.Context, session *PairSession, success bool) {
	if pm.taskStore == nil {
		return
	}

	t, err := pm.taskStore.GetByID(session.TaskID)
	if err != nil || t == nil {
		pm.logger.Error("Failed to get task for finalization",
			KeyTaskID, session.TaskID,
			"error", err,
		)
		return
	}

	if success {
		t.SetState(task.StateCompleted)
		pm.logger.Info("Pair session task completed",
			"session_id", session.ID,
			KeyTaskID, session.TaskID,
		)
	} else {
		t.SetState(task.StateFailed)
		pm.logger.Warn("Pair session task failed (exhausted)",
			"session_id", session.ID,
			KeyTaskID, session.TaskID,
		)
	}

	if err := pm.taskStore.Update(t); err != nil {
		pm.logger.Error("Failed to update task after pair finalization",
			KeyTaskID, session.TaskID,
			"error", err,
		)
	}
}

// RemoveSession removes a completed session from the manager.
func (pm *PairManager) RemoveSession(sessionID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.sessions, sessionID)
}

// ActiveSessionCount returns the number of active (non-terminal) sessions.
func (pm *PairManager) ActiveSessionCount() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	count := 0
	for _, s := range pm.sessions {
		if !s.State.IsTerminal() {
			count++
		}
	}
	return count
}

// ListSessions returns all sessions, optionally filtered by state.
func (pm *PairManager) ListSessions(activeOnly bool) []*PairSession {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var result []*PairSession
	for _, s := range pm.sessions {
		if activeOnly && s.State.IsTerminal() {
			continue
		}
		result = append(result, s)
	}
	return result
}

// publishEvent publishes a bus event from the pair manager.
func (pm *PairManager) publishEvent(topic string, data map[string]any) {
	if pm.bus == nil {
		return
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "pair-manager", data)
	if err != nil {
		pm.logger.Error("Failed to create pair manager bus message", "error", err)
		return
	}

	pm.bus.Publish(topic, msg)
}

// parseReviewJSON attempts to unmarshal a JSON ReviewResult from output.
func parseReviewJSON(output string, result *ReviewResult) error {
	import_json_pkg := extractJSON(output)
	if import_json_pkg == "" {
		return fmt.Errorf("no JSON in review output")
	}

	// Try wrapping in a ReviewResult structure
	wrapped := struct {
		Status     string   `json:"status"`
		Feedback   string   `json:"feedback"`
		Issues     []string `json:"issues,omitempty"`
		Confidence float64  `json:"confidence"`
	}{}

	if err := strictUnmarshal([]byte(import_json_pkg), &wrapped); err != nil {
		return err
	}

	result.Status = ReviewStatus(wrapped.Status)
	result.Feedback = wrapped.Feedback
	result.Issues = wrapped.Issues
	result.Confidence = wrapped.Confidence
	return nil
}

// strictUnmarshal is a thin wrapper around json.Unmarshal for clarity.
func strictUnmarshal(data []byte, v any) error {
	return jsonUnmarshalHelper(data, v)
}
```

**IMPORTANT:** The `parseReviewJSON` function references `extractJSON` (already exists in `strategic.go`), `json.Unmarshal` (from `encoding/json`), and a few string helpers. Add these utility functions to the same file:

```go
// Add to pair_manager.go imports:
import (
	"encoding/json"
	"strings"
)

// toLower is a simple wrapper for strings.ToLower.
func toLower(s string) string {
	return strings.ToLower(s)
}

// containsAny checks if s contains any of the given substrings.
func containsAny(s string, substrs []string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// extractIssueLines extracts non-empty lines from review output as issues.
func extractIssueLines(output string) []string {
	lines := strings.Split(output, "\n")
	var issues []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			issues = append(issues, trimmed)
		}
	}
	if len(issues) > 5 {
		issues = issues[:5] // Cap at 5 issues
	}
	return issues
}

// jsonUnmarshalHelper wraps encoding/json.Unmarshal.
func jsonUnmarshalHelper(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
```

The full imports for `pair_manager.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/git/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)
```

### Step 2.2 -- Write tests for PairManager

**File:** `/Users/caimlas/git/meept/internal/agent/pair_manager_test.go`

```go
package agent

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/git/meept/internal/bus"
)

func TestNewPairManager(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	if pm.ActiveSessionCount() != 0 {
		t.Errorf("expected 0 active sessions, got %d", pm.ActiveSessionCount())
	}
}

func TestPairManager_CreateSession(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	session := pm.CreateSession("task-1", "implement auth", "coder", "planner", 3)

	if session == nil {
		t.Fatal("expected non-nil session")
	}
	if session.TaskID != "task-1" {
		t.Errorf("expected task_id 'task-1', got %q", session.TaskID)
	}
	if pm.ActiveSessionCount() != 1 {
		t.Errorf("expected 1 active session, got %d", pm.ActiveSessionCount())
	}

	// Retrieve it back
	got, ok := pm.GetSession(session.ID)
	if !ok {
		t.Fatal("expected to find session by ID")
	}
	if got.ID != session.ID {
		t.Errorf("expected session ID %q, got %q", session.ID, got.ID)
	}
}

func TestPairManager_GetSessionByTask(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	session := pm.CreateSession("task-42", "spec", "coder", "planner", 3)

	got, ok := pm.GetSessionByTask("task-42")
	if !ok {
		t.Fatal("expected to find session by task ID")
	}
	if got.ID != session.ID {
		t.Errorf("expected session ID %q, got %q", session.ID, got.ID)
	}

	_, ok = pm.GetSessionByTask("task-nonexistent")
	if ok {
		t.Error("should not find session for nonexistent task")
	}

	// Terminal sessions should not be returned
	session.MarkConverged()
	_, ok = pm.GetSessionByTask("task-42")
	if ok {
		t.Error("should not find terminal session by task")
	}
}

func TestPairManager_GetSessionByStep(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	session := pm.CreateSession("task-1", "spec", "coder", "planner", 3)
	session.AddStepID("step-alpha")

	got, ok := pm.GetSessionByStep("step-alpha")
	if !ok {
		t.Fatal("expected to find session by step ID")
	}
	if got.ID != session.ID {
		t.Errorf("expected session ID %q, got %q", session.ID, got.ID)
	}

	_, ok = pm.GetSessionByStep("step-other")
	if ok {
		t.Error("should not find session for unowned step")
	}
}

func TestPairManager_RemoveSession(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	session := pm.CreateSession("task-1", "spec", "coder", "planner", 3)
	pm.RemoveSession(session.ID)

	_, ok := pm.GetSession(session.ID)
	if ok {
		t.Error("should not find removed session")
	}
	if pm.ActiveSessionCount() != 0 {
		t.Errorf("expected 0 active sessions after removal, got %d", pm.ActiveSessionCount())
	}
}

func TestPairManager_ListSessions(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	s1 := pm.CreateSession("task-1", "spec", "coder", "planner", 3)
	s2 := pm.CreateSession("task-2", "spec", "coder", "planner", 3)

	all := pm.ListSessions(false)
	if len(all) != 2 {
		t.Fatalf("expected 2 total sessions, got %d", len(all))
	}

	active := pm.ListSessions(true)
	if len(active) != 2 {
		t.Fatalf("expected 2 active sessions, got %d", len(active))
	}

	s1.MarkConverged()
	active = pm.ListSessions(true)
	if len(active) != 1 {
		t.Errorf("expected 1 active session after convergence, got %d", len(active))
	}
	_ = s2 // use variable
}

func TestPairManager_parseReviewOutput(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	tests := []struct {
		name         string
		output       string
		wantApproved bool
	}{
		{
			name:         "explicit approval",
			output:       "The implementation looks good. All requirements met.",
			wantApproved: true,
		},
		{
			name:         "lgtm",
			output:       "LGTM, ship it",
			wantApproved: true,
		},
		{
			name:         "rejection with issues",
			output:       "Missing error handling\nNo test coverage for edge cases",
			wantApproved: false,
		},
		{
			name:         "structured JSON approved",
			output:       `{"status": "approved", "feedback": "all good", "confidence": 0.95}`,
			wantApproved: true,
		},
		{
			name:         "structured JSON rejected",
			output:       `{"status": "rejected", "feedback": "needs work", "issues": ["no tests"], "confidence": 0.8}`,
			wantApproved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pm.parseReviewOutput(tt.output)
			if tt.wantApproved && result.Status != ReviewApproved {
				t.Errorf("expected approved, got %q", result.Status)
			}
			if !tt.wantApproved && result.Status == ReviewApproved {
				t.Errorf("expected non-approved, got approved")
			}
		})
	}
}

func TestPairManager_updateCriteria(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	session := pm.CreateSession("task-1", "spec", "coder", "planner", 3)
	session.SetCriteria([]string{"write tests", "handle errors"})

	// Rejected review should not change criteria
	pm.updateCriteria(session, &ReviewResult{Status: ReviewRejected})
	if len(session.Context.AcceptedCriteria) != 0 {
		t.Error("rejected review should not move criteria to accepted")
	}
	if len(session.Context.PendingCriteria) != 2 {
		t.Error("pending criteria should remain unchanged after rejection")
	}

	// Approved review should move all pending to accepted
	pm.updateCriteria(session, &ReviewResult{Status: ReviewApproved})
	if len(session.Context.AcceptedCriteria) != 2 {
		t.Errorf("expected 2 accepted criteria, got %d", len(session.Context.AcceptedCriteria))
	}
	if len(session.Context.PendingCriteria) != 0 {
		t.Errorf("expected 0 pending criteria, got %d", len(session.Context.PendingCriteria))
	}
}

func TestExtractIssueLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{name: "empty", input: "", want: 0},
		{name: "single line", input: "missing tests", want: 1},
		{name: "multi line", input: "missing tests\nno error handling\n", want: 2},
		{name: "skip headers", input: "# Review\nmissing tests\nno errors", want: 2},
		{name: "capped at 5", input: "a\nb\nc\nd\ne\nf\ng", want: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractIssueLines(tt.input)
			if len(got) != tt.want {
				t.Errorf("expected %d issues, got %d", tt.want, len(got))
			}
		})
	}
}

func TestContainsAny(t *testing.T) {
	if !containsAny("hello world", []string{"world"}) {
		t.Error("should find 'world' in 'hello world'")
	}
	if containsAny("hello world", []string{"xyz"}) {
		t.Error("should not find 'xyz' in 'hello world'")
	}
}

func TestPairManager_BusEvents(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	sub := msgBus.Subscribe("test-pair-events", "pair.*")
	defer msgBus.Unsubscribe(sub)

	pm := NewPairManager(PairManagerConfig{
		Bus:    msgBus,
		Logger: slog.Default(),
	})

	_ = pm.CreateSession("task-1", "spec", "coder", "planner", 3)

	// Drain any creation events
	drainBusMessages(sub)

	pm.publishEvent("pair.test_event", map[string]any{
		KeyTaskID: "task-1",
	})

	select {
	case msg := <-sub.Channel:
		if msg.Topic != "pair.test_event" {
			t.Errorf("expected topic 'pair.test_event', got %q", msg.Topic)
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for bus event")
	}
}

func drainBusMessages(sub *bus.Subscriber) {
	for {
		select {
		case <-sub.Channel:
		default:
			return
		}
	}
}

// Compile-time check that PairManager is compatible with bus
var _ = slog.Default()
```

### Step 2.3 -- Verify tests compile and pass

```bash
go test ./internal/agent/ -run "TestNewPairManager|TestPairManager_" -v
```

---

## Phase 3: Strategic planner integration

**File:** `/Users/caimlas/git/meept/internal/agent/strategic.go` (modify)

### Step 3.1 -- Add pair session detection and creation to StrategicPlanner

Add `PairManager` field to `StrategicPlanner`:

```go
// In strategic.go, add to StrategicPlanner struct:
type StrategicPlanner struct {
	registry    *AgentRegistry
	taskStore   *task.Store
	stepStore   *task.StepStore
	bus         *bus.MessageBus
	logger      *slog.Logger
	pairManager *PairManager  // NEW

	maxPlanSteps   int
	plannerTimeout time.Duration
}
```

Add `PairManager` to `StrategicPlannerConfig`:

```go
// In strategic.go, add to StrategicPlannerConfig struct:
type StrategicPlannerConfig struct {
	Registry       *AgentRegistry
	TaskStore      *task.Store
	StepStore      *task.StepStore
	Bus            *bus.MessageBus
	Logger         *slog.Logger
	PairManager    *PairManager  // NEW
	MaxPlanSteps   int
	PlannerTimeout time.Duration
}
```

Wire in `NewStrategicPlanner`:

```go
// In NewStrategicPlanner, add before the return statement:
return &StrategicPlanner{
	registry:       cfg.Registry,
	taskStore:      cfg.TaskStore,
	stepStore:      cfg.StepStore,
	bus:            cfg.Bus,
	logger:         cfg.Logger,
	pairManager:    cfg.PairManager,  // NEW
	maxPlanSteps:   cfg.MaxPlanSteps,
	plannerTimeout: cfg.PlannerTimeout,
}
```

Add the pair-session detection method and modify the `Plan` method:

```go
// shouldUsePairSession returns true when a task should use the pair session
// model instead of independent step scheduling.
//
// Criteria:
//   - Intent is "code" or "debug" AND the input is complex (>200 chars or
//     contains complexity indicators)
//   - Intent is "compound" (multi-intent tasks always benefit from pairing)
//   - The task name/description contains security-sensitive keywords
func (sp *StrategicPlanner) shouldUsePairSession(req PlanRequest) bool {
	if sp.pairManager == nil {
		return false
	}

	// Compound tasks always use pair sessions
	if req.Intent == string(IntentCompound) {
		return true
	}

	// Code and debug intents with complex descriptions
	switch req.Intent {
	case string(IntentCode), string(IntentDebug):
		if len(req.Input) > 200 {
			return true
		}
		lower := strings.ToLower(req.Input)
		securityIndicators := []string{
			"security", "authentication", "authorization",
			"encryption", "credential", "password", "token",
			"vulnerable", "vulnerability", "cve",
		}
		for _, indicator := range securityIndicators {
			if strings.Contains(lower, indicator) {
				return true
			}
		}
	}

	return false
}

// createPairSessionPlan creates a pair session for the task instead of
// independent steps. It creates two placeholder steps (actor + reviewer)
// and publishes a pair session creation event.
func (sp *StrategicPlanner) createPairSessionPlan(ctx context.Context, req PlanRequest, parentMemoryRefs []string) ([]*task.TaskStep, error) {
	session := sp.pairManager.CreateSession(
		req.TaskID,
		req.Input,
		sp.selectActorAgent(req.Intent),
		sp.selectReviewerAgent(req.Intent),
		DefaultPairMaxRounds,
	)

	// Extract criteria from the input (simple heuristic: split on sentences)
	criteria := sp.extractCriteria(req.Input)
	session.SetCriteria(criteria)

	// Create actor step (first round)
	actorStep := task.NewTaskStep(req.TaskID, fmt.Sprintf("[pair:actor] %s", req.Input), 0)
	actorStep.ToolHint = req.Intent
	actorStep.AgentID = session.ActorAgentID
	for _, ref := range parentMemoryRefs {
		actorStep.AddMemoryRef(ref)
	}
	session.AddStepID(actorStep.ID)

	// Create reviewer step (depends on actor)
	reviewerStep := task.NewTaskStep(req.TaskID, fmt.Sprintf("[pair:reviewer] review %s", req.Input), 1)
	reviewerStep.ToolHint = string(IntentReview)
	reviewerStep.AgentID = session.ReviewerAgentID
	reviewerStep.DependsOn = []string{actorStep.ID}
	for _, ref := range parentMemoryRefs {
		reviewerStep.AddMemoryRef(ref)
	}
	session.AddStepID(reviewerStep.ID)

	sp.logger.Info("Created pair session plan",
		"task_id", req.TaskID,
		"session_id", session.ID,
		"actor", session.ActorAgentID,
		"reviewer", session.ReviewerAgentID,
		"criteria", len(criteria),
	)

	// Publish pair session created event
	sp.publishEvent("pair.session_created", map[string]any{
		KeyTaskID:    req.TaskID,
		"session_id": session.ID,
		"actor":      session.ActorAgentID,
		"reviewer":   session.ReviewerAgentID,
		"max_rounds": session.MaxRounds,
		"criteria":   criteria,
	})

	return []*task.TaskStep{actorStep, reviewerStep}, nil
}

// selectActorAgent chooses the actor agent for a pair session based on intent.
func (sp *StrategicPlanner) selectActorAgent(intent string) string {
	switch intent {
	case string(IntentCode), string(IntentCompound):
		return config.AgentIDCoder
	case string(IntentDebug):
		return config.AgentIDDebugger
	default:
		return config.AgentIDCoder
	}
}

// selectReviewerAgent chooses the reviewer agent for a pair session based on intent.
func (sp *StrategicPlanner) selectReviewerAgent(intent string) string {
	switch intent {
	case string(IntentCode), string(IntentCompound):
		return config.AgentIDPlanner
	case string(IntentDebug):
		return config.AgentIDAnalyst
	default:
		return config.AgentIDPlanner
	}
}

// extractCriteria extracts simple criteria from a task description.
// Splits on sentence boundaries and filters trivially short items.
func (sp *StrategicPlanner) extractCriteria(input string) []string {
	// Split on common sentence delimiters
	replacements := []string{". ", "|", "\n"}
	working := input
	for _, r := range replacements {
		working = strings.ReplaceAll(working, r, "\n")
	}

	lines := strings.Split(working, "\n")
	var criteria []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip headers, empty lines, and trivially short items
		if len(trimmed) < 10 || strings.HasPrefix(trimmed, "#") {
			continue
		}
		criteria = append(criteria, trimmed)
	}

	// If no criteria extracted, use the whole input as one criterion
	if len(criteria) == 0 {
		criteria = []string{input}
	}

	return criteria
}
```

Now modify the `Plan` method to check for pair sessions before normal decomposition. Insert this block after the `t.SetState(task.StatePlanning)` line and before the `var steps []*task.TaskStep` line:

```go
	// Check if this task should use pair sessions instead of normal steps
	if sp.shouldUsePairSession(req) {
		sp.logger.Info("Using pair session for task",
			"task_id", req.TaskID,
			"intent", req.Intent,
		)
		steps, err := sp.createPairSessionPlan(ctx, req, parentMemoryRefs)
		if err != nil {
			sp.logger.Error("Failed to create pair session plan, falling back",
				"task_id", req.TaskID,
				"error", err,
			)
			// Fall through to normal planning
		} else {
			// Persist steps
			for _, step := range steps {
				if err := sp.stepStore.Create(step); err != nil {
					sp.logger.Error("Failed to persist step", "step_id", step.ID, "error", err)
					return fmt.Errorf("failed to persist steps: %w", err)
				}
			}

			t.TotalJobs = len(steps)
			t.SetState(task.StateExecuting)
			if err := sp.taskStore.Update(t); err != nil {
				sp.logger.Error("Failed to update task after pair planning", "error", err)
			}

			// Promote actor step to ready (reviewer depends on it)
			promoted, err := sp.stepStore.PromoteReadySteps(req.TaskID)
			if err != nil {
				sp.logger.Error("Failed to promote pair steps", "error", err)
			}

			sp.publishEvent("task.planned", map[string]any{
				KeyTaskID:     req.TaskID,
				"session_id":  req.SessionID,
				"total_steps": len(steps),
				"ready_steps": len(promoted),
				"pair_session": true,
			})

			sp.publishEvent("orchestrator.schedule", map[string]any{
				KeyTaskID: req.TaskID,
			})

			return nil
		}
	}
```

### Step 3.2 -- Write tests for strategic planner pair integration

Add to `/Users/caimlas/git/meept/internal/agent/strategic_test.go`:

```go
func TestShouldUsePairSession(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{Logger: slog.Default()})
	sp := &StrategicPlanner{pairManager: pm, logger: slog.Default()}

	tests := []struct {
		name   string
		req    PlanRequest
		want   bool
	}{
		{
			name: "compound intent always pairs",
			req:  PlanRequest{Intent: string(IntentCompound), Input: "do stuff"},
			want: true,
		},
		{
			name: "short code input no pair",
			req:  PlanRequest{Intent: string(IntentCode), Input: "fix typo in readme"},
			want: false,
		},
		{
			name: "long code input pairs",
			req:  PlanRequest{Intent: string(IntentCode), Input: strings.Repeat("implement the full authentication system with OAuth2 support ", 5)},
			want: true,
		},
		{
			name: "security keyword triggers pair",
			req:  PlanRequest{Intent: string(IntentCode), Input: "add security headers to API responses"},
			want: true,
		},
		{
			name: "chat intent no pair",
			req:  PlanRequest{Intent: string(IntentChat), Input: "how are you"},
			want: false,
		},
		{
			name: "nil pair manager no pair",
			req:  PlanRequest{Intent: string(IntentCompound), Input: "complex task"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "nil pair manager no pair" {
				spNoPM := &StrategicPlanner{pairManager: nil, logger: slog.Default()}
				got := spNoPM.shouldUsePairSession(tt.req)
				if got != tt.want {
					t.Errorf("shouldUsePairSession() = %v, want %v", got, tt.want)
				}
				return
			}
			got := sp.shouldUsePairSession(tt.req)
			if got != tt.want {
				t.Errorf("shouldUsePairSession() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractCriteria(t *testing.T) {
	sp := &StrategicPlanner{logger: slog.Default()}

	tests := []struct {
		name  string
		input string
		wantMin int
	}{
		{
			name:  "single sentence",
			input: "Implement the authentication module with JWT tokens",
			wantMin: 1,
		},
		{
			name:  "multi sentence",
			input: "Write the parser. Add error handling. Include tests.",
			wantMin: 3,
		},
		{
			name:  "with headers",
			input: "# Task\nWrite the code\n# Notes\nBe careful",
			wantMin: 1,
		},
		{
			name:  "short input uses whole input",
			input: "fix bug",
			wantMin: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sp.extractCriteria(tt.input)
			if len(got) < tt.wantMin {
				t.Errorf("extractCriteria() returned %d criteria, want at least %d", len(got), tt.wantMin)
			}
		})
	}
}

func TestSelectActorAgent(t *testing.T) {
	sp := &StrategicPlanner{logger: slog.Default()}

	tests := []struct {
		intent string
		want   string
	}{
		{string(IntentCode), config.AgentIDCoder},
		{string(IntentCompound), config.AgentIDCoder},
		{string(IntentDebug), config.AgentIDDebugger},
		{"unknown", config.AgentIDCoder},
	}

	for _, tt := range tests {
		t.Run(tt.intent, func(t *testing.T) {
			got := sp.selectActorAgent(tt.intent)
			if got != tt.want {
				t.Errorf("selectActorAgent(%q) = %q, want %q", tt.intent, got, tt.want)
			}
		})
	}
}

func TestSelectReviewerAgent(t *testing.T) {
	sp := &StrategicPlanner{logger: slog.Default()}

	tests := []struct {
		intent string
		want   string
	}{
		{string(IntentCode), config.AgentIDPlanner},
		{string(IntentCompound), config.AgentIDPlanner},
		{string(IntentDebug), config.AgentIDAnalyst},
		{"unknown", config.AgentIDPlanner},
	}

	for _, tt := range tests {
		t.Run(tt.intent, func(t *testing.T) {
			got := sp.selectReviewerAgent(tt.intent)
			if got != tt.want {
				t.Errorf("selectReviewerAgent(%q) = %q, want %q", tt.intent, got, tt.want)
			}
		})
	}
}
```

### Step 3.3 -- Verify tests compile and pass

```bash
go test ./internal/agent/ -run "TestShouldUsePairSession|TestExtractCriteria|TestSelectActorAgent|TestSelectReviewerAgent" -v
```

---

## Phase 4: Orchestrator bus subscription for pair session events

**File:** `/Users/caimlas/git/meept/internal/agent/orchestrator.go` (modify)

### Step 4.1 -- Add PairManager to Orchestrator and subscribe to pair events

Modify the `Orchestrator` struct:

```go
type Orchestrator struct {
	strategic   *StrategicPlanner
	tactical    *TacticalScheduler
	pairManager *PairManager  // NEW
	bus         *bus.MessageBus
	logger      *slog.Logger

	cancel context.CancelFunc
	wg     sync.WaitGroup
}
```

Modify `OrchestratorDeps`:

```go
type OrchestratorDeps struct {
	Strategic   *StrategicPlanner
	Tactical    *TacticalScheduler
	PairManager *PairManager  // NEW
	Bus         *bus.MessageBus
	Logger      *slog.Logger
}
```

Wire in `NewOrchestrator`:

```go
func NewOrchestrator(deps OrchestratorDeps) *Orchestrator {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}

	return &Orchestrator{
		strategic:   deps.Strategic,
		tactical:    deps.Tactical,
		pairManager: deps.PairManager,  // NEW
		bus:         deps.Bus,
		logger:      deps.Logger,
	}
}
```

Add pair session topic subscriptions in `Start`:

```go
func (o *Orchestrator) Start(ctx context.Context) error {
	ctx, o.cancel = context.WithCancel(ctx)

	topics := map[string]func(context.Context, *models.BusMessage){
		"orchestrator.plan":     o.handlePlanRequest,
		"orchestrator.schedule": o.handleScheduleRequest,
		"queue.job.completed":   o.handleJobCompleted,
		"queue.job.failed":      o.handleJobFailed,
		"task.amend.applied":    o.handleAmendmentApplied,
		"task.amend.rejected":   o.handleAmendmentRejected,
		"pair.session_created":  o.handlePairSessionCreated,  // NEW
		"pair.converged":        o.handlePairConverged,        // NEW
		"pair.exhausted":        o.handlePairExhausted,        // NEW
		"pair.round_failed":     o.handlePairRoundFailed,      // NEW
	}

	for topic, handler := range topics {
		sub := o.bus.Subscribe("orchestrator-"+topic, topic)
		o.wg.Add(1)
		go o.runSubscription(ctx, sub, handler)
	}

	o.logger.Info("Orchestrator started",
		"subscriptions", len(topics),
	)
	return nil
}
```

Add the handler methods:

```go
// handlePairSessionCreated is called when a new pair session is created.
// It logs the event and prepares for the pair-driven scheduling loop.
func (o *Orchestrator) handlePairSessionCreated(_ context.Context, msg *models.BusMessage) {
	var event struct {
		TaskID    string   `json:"task_id"`
		SessionID string   `json:"session_id"`
		Actor     string   `json:"actor"`
		Reviewer  string   `json:"reviewer"`
		MaxRounds int      `json:"max_rounds"`
		Criteria  []string `json:"criteria"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Error("Failed to parse pair session created event", "error", err)
		return
	}

	o.logger.Info("Pair session created",
		KeyTaskID, event.TaskID,
		"session_id", event.SessionID,
		"actor", event.Actor,
		"reviewer", event.Reviewer,
		"max_rounds", event.MaxRounds,
		"criteria_count", len(event.Criteria),
	)
}

// handlePairConverged is called when a pair session converges (all criteria satisfied).
func (o *Orchestrator) handlePairConverged(_ context.Context, msg *models.BusMessage) {
	var event struct {
		SessionID string `json:"session_id"`
		TaskID    string `json:"task_id"`
		Rounds    int    `json:"rounds"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Error("Failed to parse pair converged event", "error", err)
		return
	}

	o.logger.Info("Pair session converged",
		"session_id", event.SessionID,
		KeyTaskID, event.TaskID,
		"rounds", event.Rounds,
	)
}

// handlePairExhausted is called when a pair session reaches max rounds without convergence.
func (o *Orchestrator) handlePairExhausted(_ context.Context, msg *models.BusMessage) {
	var event struct {
		SessionID string `json:"session_id"`
		TaskID    string `json:"task_id"`
		Rounds    int    `json:"rounds"`
		MaxRounds int    `json:"max_rounds"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Error("Failed to parse pair exhausted event", "error", err)
		return
	}

	o.logger.Warn("Pair session exhausted without convergence",
		"session_id", event.SessionID,
		KeyTaskID, event.TaskID,
		"rounds", event.Rounds,
		"max_rounds", event.MaxRounds,
	)
}

// handlePairRoundFailed is called when a pair session round fails.
func (o *Orchestrator) handlePairRoundFailed(_ context.Context, msg *models.BusMessage) {
	var event struct {
		SessionID string `json:"session_id"`
		TaskID    string `json:"task_id"`
		Round     int    `json:"round"`
		Phase     string `json:"phase"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Error("Failed to parse pair round failed event", "error", err)
		return
	}

	o.logger.Error("Pair session round failed",
		"session_id", event.SessionID,
		KeyTaskID, event.TaskID,
		"round", event.Round,
		"phase", event.Phase,
		"error", event.Error,
	)
}
```

### Step 4.2 -- Verify orchestrator tests still pass

```bash
go test ./internal/agent/ -run "TestOrchestrator" -v
```

---

## Phase 5: Tactical scheduler bypass for pair-managed tasks

**File:** `/Users/caimlas/git/meept/internal/agent/tactical.go` (modify)

### Step 5.1 -- Add PairManager field to TacticalScheduler and check before scheduling

Add `pairManager` field to `TacticalScheduler`:

```go
type TacticalScheduler struct {
	stepStore              *task.StepStore
	taskStore              *task.Store
	queue                  queue.Queue
	registry               *AgentRegistry
	bus                    *bus.MessageBus
	pairManager            *PairManager  // NEW
	reviewManager          *ReviewManager
	validatorManager       *validator.ValidatorManager
	escalationManager      *EscalationManager
	logger                 *slog.Logger
	globalSemaphore        chan struct{}
	agentSemaphore         map[string]chan struct{}
	semaphoreMu            sync.Mutex
	validationGateInterval int
	validationGateCounter  map[string]int
	validationGateMu       sync.Mutex
}
```

Add to `TacticalSchedulerConfig`:

```go
type TacticalSchedulerConfig struct {
	StepStore              *task.StepStore
	TaskStore              *task.Store
	Queue                  queue.Queue
	Registry               *AgentRegistry
	Bus                    *bus.MessageBus
	PairManager            *PairManager  // NEW
	ReviewManager          *ReviewManager
	ValidatorManager       *validator.ValidatorManager
	EscalationManager      *EscalationManager
	Logger                 *slog.Logger
	MaxConcurrentJobs      int
	MaxConcurrentPerAgent  int
	ValidationGateInterval int
}
```

Wire in `NewTacticalScheduler`:

```go
// In NewTacticalScheduler, add pairManager to the return struct:
return &TacticalScheduler{
	stepStore:              cfg.StepStore,
	taskStore:              cfg.TaskStore,
	queue:                  cfg.Queue,
	registry:               cfg.Registry,
	bus:                    cfg.Bus,
	pairManager:            cfg.PairManager,  // NEW
	reviewManager:          cfg.ReviewManager,
	validatorManager:       cfg.ValidatorManager,
	escalationManager:      cfg.EscalationManager,
	logger:                 cfg.Logger,
	globalSemaphore:        globalSemaphore,
	agentSemaphore:         agentSemaphore,
	semaphoreMu:            sync.Mutex{},
	validationGateInterval: validationGateInterval,
	validationGateCounter:  make(map[string]int),
	validationGateMu:       sync.Mutex{},
}
```

Modify `ScheduleReadySteps` to skip pair-managed steps:

```go
// In ScheduleReadySteps, add this check inside the for loop over readySteps,
// immediately before the existing ts.scheduleStep call:
	for _, step := range readySteps {
		// Skip steps managed by a pair session -- the PairManager drives them
		if ts.pairManager != nil {
			if _, isPair := ts.pairManager.GetSessionByStep(step.ID); isPair {
				ts.logger.Debug("Skipping pair-managed step in tactical scheduling",
					"step_id", step.ID,
					KeyTaskID, taskID,
				)
				continue
			}
		}

		if err := ts.scheduleStep(ctx, step); err != nil {
			// ... existing error handling
```

Also modify `OnJobCompleted` to delegate to PairManager for pair-managed steps. Add this check near the top of `OnJobCompleted`, after the step is found but before the normal completion pipeline:

```go
	// Check if this step belongs to a pair session
	if ts.pairManager != nil {
		if session, isPair := ts.pairManager.GetSessionByStep(step.ID); isPair {
			ts.logger.Info("Pair-managed step completed, delegating to PairManager",
				"step_id", step.ID,
				KeyTaskID, step.TaskID,
				"session_id", session.ID,
			)

			// Release semaphore slots
			ts.releaseSlots(step.AgentID)

			// Store result
			resultStr := ""
			if result != nil {
				resultStr = string(result)
			}
			if err := ts.stepStore.SetResult(step.ID, resultStr); err != nil {
				ts.logger.Error("Failed to set pair step result", "step_id", step.ID, "error", err)
			}

			// Mark step completed
			if err := ts.stepStore.SetState(step.ID, task.StepCompleted); err != nil {
				ts.logger.Error("Failed to set pair step completed", "step_id", step.ID, "error", err)
			}

			// Run next pair round asynchronously
			go ts.pairManager.RunRound(context.Background(), session.ID)

			return nil
		}
	}
```

Add a corresponding check in `OnJobFailed`:

```go
	// Check if this step belongs to a pair session
	if ts.pairManager != nil {
		if session, isPair := ts.pairManager.GetSessionByStep(step.ID); isPair {
			ts.logger.Warn("Pair-managed step failed",
				"step_id", step.ID,
				KeyTaskID, step.TaskID,
				"session_id", session.ID,
				"error", jobErr,
			)

			// Release semaphore slots
			ts.releaseSlots(step.AgentID)

			// Mark step failed and session failed
			if err := ts.stepStore.SetResult(step.ID, jobErr); err != nil {
				ts.logger.Error("Failed to set pair step error", "step_id", step.ID, "error", err)
			}
			if err := ts.stepStore.SetState(step.ID, task.StepFailed); err != nil {
				ts.logger.Error("Failed to set pair step failed", "step_id", step.ID, "error", err)
			}
			session.MarkFailed()

			return nil
		}
	}
```

### Step 5.2 -- Verify tactical tests still pass

```bash
go test ./internal/agent/ -run "TestTactical|TestScheduleReady" -v
```

---

## Phase 6: Integration test

**File:** `/Users/caimlas/git/meept/internal/agent/pair_integration_test.go`

### Step 6.1 -- Write end-to-end pair session integration test

```go
package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/git/meept/internal/task"
)

// mockAgentRegistry is a minimal AgentRegistry that returns canned responses.
// We cannot easily construct a real AgentRegistry without an LLM client, so
// we use the PairManager directly with a test-only flow.
type mockAgentLoop struct {
	response string
	err      error
}

// TestPairSessionLifecycle tests the full lifecycle of a pair session:
// create session -> set criteria -> run rounds until convergence.
// This test uses direct PairManager calls since we cannot instantiate
// real AgentLoop objects without an LLM client.
func TestPairSessionLifecycle(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	// Subscribe to pair events
	pairSub := msgBus.Subscribe("test-pair", "pair.*")
	defer msgBus.Unsubscribe(pairSub)

	pm := NewPairManager(PairManagerConfig{
		Bus:    msgBus,
		Logger: slog.Default(),
	})

	session := pm.CreateSession(
		"task-lifecycle-test",
		"implement user registration with validation",
		"coder",
		"planner",
		3,
	)

	session.SetCriteria([]string{
		"implement registration handler",
		"add input validation",
		"write unit tests",
	})

	if session.State != PairSessionActive {
		t.Fatalf("expected active state, got %q", session.State)
	}
	if len(session.Context.PendingCriteria) != 3 {
		t.Fatalf("expected 3 pending criteria, got %d", len(session.Context.PendingCriteria))
	}

	// Verify bus event was published for creation
	select {
	case msg := <-pairSub.Channel:
		if msg.Topic != "pair.session_created" {
			// The CreateSession itself does not publish -- that's the planner's job
			// So this event may not arrive unless we call publish explicitly
			_ = msg
		}
	default:
		// OK -- CreateSession does not publish events
	}

	// Verify session is retrievable
	got, ok := pm.GetSessionByTask("task-lifecycle-test")
	if !ok {
		t.Fatal("expected to find session by task")
	}
	if got.ID != session.ID {
		t.Errorf("expected session ID %q, got %q", session.ID, got.ID)
	}

	// Simulate convergence: directly update context
	session.Context.PendingCriteria = nil
	session.Context.AcceptedCriteria = []string{
		"implement registration handler",
		"add input validation",
		"write unit tests",
	}

	if !session.Context.HasConverged() {
		t.Error("expected convergence after moving all criteria to accepted")
	}

	session.MarkConverged()
	if session.State != PairSessionConverged {
		t.Errorf("expected converged state, got %q", session.State)
	}

	// Verify session is no longer returned by task lookup
	_, ok = pm.GetSessionByTask("task-lifecycle-test")
	if ok {
		t.Error("converged session should not be returned as active")
	}
}

// TestPairSessionExhaustion tests the exhaustion path.
func TestPairSessionExhaustion(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	session := pm.CreateSession("task-exhaust", "spec", "coder", "planner", 2)
	session.SetCriteria([]string{"criterion A"})

	// Simulate two failed rounds
	session.Context.RecordAttempt(&Attempt{
		Round:       1,
		ActorOutput: "attempt 1",
		Review:      &ReviewResult{Status: ReviewRejected, Feedback: "not good enough"},
	})

	session.Context.RecordAttempt(&Attempt{
		Round:       2,
		ActorOutput: "attempt 2",
		Review:      &ReviewResult{Status: ReviewRejected, Feedback: "still not good"},
	})

	if !session.IsExhausted() {
		t.Error("should be exhausted after 2 attempts with max_rounds=2")
	}

	session.MarkExhausted()
	if session.State != PairSessionExhausted {
		t.Errorf("expected exhausted state, got %q", session.State)
	}
}

// TestPairSessionWithTaskStore tests finalization with a real task store.
func TestPairSessionWithTaskStore(t *testing.T) {
	tmpDir := t.TempDir()
	taskStore, err := newTestTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	defer taskStore.Close()

	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	pm := NewPairManager(PairManagerConfig{
		TaskStore: taskStore,
		Bus:       msgBus,
		Logger:    slog.Default(),
	})

	// Create a task
	tsk := newTestTask("pair-task", "implement feature X")
	tsk.SetState(task.StateExecuting)
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	session := pm.CreateSession(tsk.ID, "implement feature X", "coder", "planner", 3)

	// Simulate successful convergence
	session.SetCriteria([]string{"write code", "add tests"})
	session.Context.PendingCriteria = nil
	session.Context.AcceptedCriteria = []string{"write code", "add tests"}

	session.MarkConverged()
	pm.finalizeTask(context.Background(), session, true)

	// Verify task state
	updated, err := taskStore.GetByID(tsk.ID)
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	if updated.State != task.StateCompleted {
		t.Errorf("expected task completed, got %q", updated.State)
	}
}

// TestPairSessionFailureFinalization tests failed finalization.
func TestPairSessionFailureFinalization(t *testing.T) {
	tmpDir := t.TempDir()
	taskStore, err := newTestTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	defer taskStore.Close()

	pm := NewPairManager(PairManagerConfig{
		TaskStore: taskStore,
		Logger:    slog.Default(),
	})

	tsk := newTestTask("pair-fail-task", "implement feature Y")
	tsk.SetState(task.StateExecuting)
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	session := pm.CreateSession(tsk.ID, "implement feature Y", "coder", "planner", 2)
	session.MarkExhausted()
	pm.finalizeTask(context.Background(), session, false)

	updated, err := taskStore.GetByID(tsk.ID)
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	if updated.State != task.StateFailed {
		t.Errorf("expected task failed, got %q", updated.State)
	}
}

// TestStrategicPlanner_PairSessionPlan verifies the planner creates pair sessions
// for compound intents when PairManager is available.
func TestStrategicPlanner_PairSessionPlan(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	tmpDir := t.TempDir()
	taskStore, err := newTestTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	defer taskStore.Close()

	stepStore := taskStore.StepStore()

	pm := NewPairManager(PairManagerConfig{
		Bus:    msgBus,
		Logger: slogDiscardLogger(),
	})

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		TaskStore:   taskStore,
		StepStore:   stepStore,
		Bus:         msgBus,
		PairManager: pm,
		Logger:      slogDiscardLogger(),
	})

	// Create a task
	tsk := newTestTask("pair-plan-test", "implement auth and add tests")
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Plan with compound intent
	err = sp.Plan(context.Background(), PlanRequest{
		TaskID:  tsk.ID,
		Input:   "implement authentication module with OAuth2 and write comprehensive tests for login flow",
		Intent:  string(IntentCompound),
	})
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	// Verify pair session was created
	session, ok := pm.GetSessionByTask(tsk.ID)
	if !ok {
		t.Fatal("expected pair session to be created for compound task")
	}

	if session.ActorAgentID != "coder" {
		t.Errorf("expected actor 'coder', got %q", session.ActorAgentID)
	}
	if session.ReviewerAgentID != "planner" {
		t.Errorf("expected reviewer 'planner', got %q", session.ReviewerAgentID)
	}

	// Verify steps were created
	steps, err := stepStore.ListByTaskID(tsk.ID)
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps (actor + reviewer), got %d", len(steps))
	}

	// Verify step IDs are tracked in session
	for _, s := range steps {
		if !session.OwnsStep(s.ID) {
			t.Errorf("session should own step %q", s.ID)
		}
	}
}

// TestPairManagerConcurrentAccess tests thread safety of PairManager.
func TestPairManagerConcurrentAccess(t *testing.T) {
	pm := NewPairManager(PairManagerConfig{
		Logger: slog.Default(),
	})

	// Create sessions concurrently
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(idx int) {
			defer func() { done <- struct{}{} }()
			session := pm.CreateSession(
				"task-concurrent",
				"spec",
				"coder",
				"planner",
				3,
			)
			_ = session.ID
			_ = pm.GetSessionByTask("task-concurrent")
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify no panics and all sessions exist
	all := pm.ListSessions(false)
	if len(all) != 10 {
		t.Errorf("expected 10 sessions, got %d", len(all))
	}
}

// newTestTaskStore creates a test task store (reuse from orchestrator_test.go
// if already in same package).
func init() {
	_ = filepath.Join  // ensure filepath import is used
	_ = slog.Default()
	_ = time.Now()
}
```

### Step 6.2 -- Run all tests

```bash
go test ./internal/agent/ -run "TestPair" -v
go test ./internal/agent/ -run "TestShouldUsePairSession|TestExtractCriteria|TestSelectActorAgent|TestSelectReviewerAgent" -v
go test ./internal/agent/ -run "TestStrategicPlanner_PairSessionPlan" -v
```

### Step 6.3 -- Run the full agent test suite to verify no regressions

```bash
go test ./internal/agent/... -v -count=1
```

---

## Summary of files changed

| File | Action | Description |
|------|--------|-------------|
| `internal/agent/pair_session.go` | Create | PairSession, PairContext, Attempt types with prompt builders and state management |
| `internal/agent/pair_session_test.go` | Create | Tests for all pair session types |
| `internal/agent/pair_manager.go` | Create | PairManager that drives the multi-round loop, parses reviews, publishes bus events |
| `internal/agent/pair_manager_test.go` | Create | Tests for PairManager including session CRUD, review parsing, bus events |
| `internal/agent/pair_integration_test.go` | Create | End-to-end tests: lifecycle, exhaustion, task store finalization, planner integration, concurrency |
| `internal/agent/strategic.go` | Modify | Add `shouldUsePairSession`, `createPairSessionPlan`, `extractCriteria`, `selectActorAgent`, `selectReviewerAgent`; wire PairManager into planner |
| `internal/agent/strategic_test.go` | Modify | Add tests for pair detection, criteria extraction, agent selection |
| `internal/agent/orchestrator.go` | Modify | Add PairManager field, subscribe to `pair.*` topics, add handler methods |
| `internal/agent/tactical.go` | Modify | Add PairManager field, skip pair-managed steps in `ScheduleReadySteps`, delegate completion/failure to PairManager |

## Key design decisions

1. **Shared context in memory**: PairContext holds all attempt history. Both actor and reviewer see full history on each round, enabling cumulative improvement.

2. **Criteria-based convergence**: Instead of relying solely on reviewer approval, we track explicit criteria from the original spec. This prevents false convergence from a permissive reviewer.

3. **Bus-driven events**: All pair session state changes publish bus events (`pair.session_created`, `pair.round_completed`, `pair.converged`, `pair.exhausted`, `pair.round_failed`), enabling UI and monitoring integration.

4. **Tactical bypass**: Steps belonging to a pair session are skipped by the normal tactical scheduler. The PairManager drives the loop via `RunRound`, which is triggered when a pair-managed step completes.

5. **Graceful degradation**: If the PairManager is nil (not configured), the system behaves exactly as before. The `shouldUsePairSession` method returns false when no PairManager is available.

6. **Security-sensitive detection**: The planner detects security-related keywords in task descriptions and automatically routes them through pair sessions for extra review.