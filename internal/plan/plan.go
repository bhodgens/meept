package plan

import (
	"fmt"
	"sync/atomic"
	"time"
)

// PlanState represents the lifecycle state of a plan.
type PlanState string

const (
	StatePlanning        PlanState = "planning"
	StateDraft           PlanState = "draft"
	StatePendingApproval PlanState = "pending_approval"
	StateApproved        PlanState = "approved"
	StateExecuting       PlanState = "executing"
	StateCompleted       PlanState = "completed"
	StateConfirmed       PlanState = "confirmed"
	StateCancelled       PlanState = "cancelled"
	StateFailed          PlanState = "failed"
)

func (s PlanState) IsTerminal() bool {
	return s == StateConfirmed || s == StateCancelled || s == StateFailed
}

// PhaseState represents the state of a plan phase.
type PhaseState string

const (
	PhasePending    PhaseState = "pending"
	PhaseInProgress PhaseState = "in_progress"
	PhaseCompleted  PhaseState = "completed"
	PhaseConfirmed  PhaseState = "confirmed"
	PhaseFailed     PhaseState = "failed"
)

func (s PhaseState) IsTerminal() bool {
	return s == PhaseConfirmed || s == PhaseFailed
}

// Plan represents a project-scoped plan with a plan.md source of truth.
type Plan struct {
	ID            string      `json:"id"`
	Title         string      `json:"title"`
	Description   string      `json:"description,omitempty"`
	FilePath      string      `json:"file_path"`
	ProjectID     string      `json:"project_id,omitempty"`
	State         PlanState   `json:"state"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
	ApprovedAt    *time.Time  `json:"approved_at,omitempty"`
	ConfirmedAt   *time.Time  `json:"confirmed_at,omitempty"`
	ApprovedBy    string      `json:"approved_by,omitempty"`
	ConfirmedBy   string      `json:"confirmed_by,omitempty"`
	TaskID        string      `json:"task_id,omitempty"`
	SourceSession string      `json:"source_session,omitempty"`
	RevisionCount int         `json:"revision_count,omitempty"`
	Phases        []PlanPhase `json:"phases,omitempty"`
}

// PlanPhase represents a named phase within a plan.
type PlanPhase struct {
	ID             string     `json:"id"`
	PlanID         string     `json:"plan_id"`
	Name           string     `json:"name"`
	Sequence       int        `json:"sequence"`
	TotalSteps     int        `json:"total_steps"`
	CompletedSteps int        `json:"completed_steps"`
	FailedSteps    int        `json:"failed_steps"`
	State          PhaseState `json:"state"`
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

// PlanningContext holds the result of a Prometheus-style pre-decomposition
// interview. It captures ambiguities, constraints, and clarifications so
// that downstream agents have verified context before touching code.
type PlanningContext struct {
	// Questions asked during the interview phase.
	InterviewQuestions []string `json:"interview_questions,omitempty"`
	// User answers to interview questions.
	InterviewAnswers []string `json:"interview_answers,omitempty"`
	// Ambiguities identified before decomposition.
	Ambiguities []string `json:"ambiguities,omitempty"`
	// Constraints extracted from conversation (time, scope, dependencies).
	Constraints map[string]string `json:"constraints,omitempty"`
	// Requirements distilled from the request.
	Requirements []string `json:"requirements,omitempty"`
	// InterviewCompleted is true when the interview phase finished.
	InterviewCompleted bool `json:"interview_completed"`
	// UserApproved is true when the user explicitly confirmed the plan.
	UserApproved bool `json:"user_approved"`
	// Raw context from TrueIntentAnalysis if available.
	TrueGoal string `json:"true_goal,omitempty"`
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
