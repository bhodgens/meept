package agent

import (
	"fmt"
	"time"
)

// PairSessionState represents the lifecycle state of a pair session.
type PairSessionState string

const (
	PairSessionActive    PairSessionState = "active"
	PairSessionConverged PairSessionState = "converged"
	PairSessionExhausted PairSessionState = "exhausted" // max rounds reached
	PairSessionFailed    PairSessionState = "failed"
	PairSessionCancelled PairSessionState = "cancelled"
)

// IsTerminal returns true if the pair session is in a terminal state.
func (s PairSessionState) IsTerminal() bool {
	return s == PairSessionConverged || s == PairSessionExhausted ||
		s == PairSessionFailed || s == PairSessionCancelled
}

// Attempt represents a single actor->reviewer round within a pair session.
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

	// Attempts is the ordered history of all actor->reviewer rounds.
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
			// Attempt.Review is a pointer and may be nil if a round was
			// recorded without a review (reviewer error, context cancel).
			// Guard against nil deref when summarizing prior rounds.
			status := "pending"
			if a.Review != nil {
				status = string(a.Review.Status)
			}
			prompt += fmt.Sprintf("- Round %d: %s\n", a.Round, status)
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

	// MaxRounds is the maximum number of actor->reviewer rounds before giving up.
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
		ID:              fmt.Sprintf("pair-%s-%d", taskID, now.UnixNano()),
		TaskID:          taskID,
		ActorAgentID:    actorID,
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
