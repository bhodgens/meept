// Package agent provides the agent loop and related components.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/llm"
	"github.com/google/uuid"
)

// Programming task detection keywords.
var programmingKeywords = []string{
	"write", "code", "script", "program", "implement", "build", "deploy",
	"automate", "cron", "service", "daemon", "api", "endpoint", "database",
	"server", "docker", "container", "pipeline", "ci/cd", "terraform",
	"ansible", "kubernetes", "config", "infrastructure", "migration",
	"refactor", "debug", "fix bug", "patch", "update code", "git",
	"compile", "test", "unit test", "integration",
}

// Approval/rejection/revision patterns.
var (
	approvalPatterns  = []string{"approved", "approve", "go ahead", "execute", "proceed", "lgtm", "looks good", "do it", "yes"}
	rejectionPatterns = []string{"reject", "no", "stop", "cancel", "don't", "redo"}
	revisionPatterns  = []string{"but", "change", "modify", "add", "remove", "instead", "also", "what about"}
)

// Analysis system prompt for LLM review.
const analysisSystemPrompt = `You are a senior technical reviewer. Given a task plan, analyse it for:
1. Missing steps or prerequisites
2. Design flaws or questionable approaches
3. Dependency conflicts
4. Security concerns
5. Edge cases that should be handled
6. Resource requirements or constraints

Be concise but thorough. List each finding as a bullet point. If the plan
looks solid, say so briefly and note any minor suggestions.`

// PlanStatus represents the status of a plan review.
type PlanStatus string

const (
	PlanStatusPendingApproval PlanStatus = "pending_approval"
	PlanStatusApproved        PlanStatus = "approved"
	PlanStatusRejected        PlanStatus = "rejected"
	PlanStatusRevised         PlanStatus = "revised"
)

// TaskStatus represents the status of a task.
type TaskStatus string

const (
	TaskStatusPending         TaskStatus = "pending"
	TaskStatusPendingApproval TaskStatus = "pending_approval"
	TaskStatusInProgress      TaskStatus = "in_progress"
	TaskStatusCompleted       TaskStatus = "completed"
	TaskStatusCancelled       TaskStatus = "cancelled"
)

// TaskPlan represents a decomposed task plan.
type TaskPlan struct {
	ID            string
	Description   string
	Steps         []TaskStep
	Status        TaskStatus
	WorkspacePath string
	Analysis      string
	Approved      bool
}

// TaskStep represents a step in a task plan.
type TaskStep struct {
	ID          string
	Description string
	DependsOn   []string
	ToolHint    string
	Status      TaskStatus
}

// PlanReview is the result of a collaborative planning + review cycle.
type PlanReview struct {
	TaskID           string
	Plan             *TaskPlan
	Analysis         string
	Status           PlanStatus
	WorkspacePath    string
	FormattedSummary string
}

// Planner is an interface for task decomposition.
type Planner interface {
	Decompose(ctx context.Context, message string) ([]TaskStep, error)
}

// CollaborativePlanner wraps a Planner with workspace tracking and interactive review.
type CollaborativePlanner struct {
	mu sync.RWMutex

	planner   Planner
	llm       *llm.Client
	workspace *WorkspaceManager
	logger    *slog.Logger

	// conversation_id -> PlanReview (tracks pending reviews)
	pending map[string]*PlanReview
}

// NewCollaborativePlanner creates a new collaborative planner.
func NewCollaborativePlanner(
	planner Planner,
	llmClient *llm.Client,
	workspace *WorkspaceManager,
	logger *slog.Logger,
) *CollaborativePlanner {
	if logger == nil {
		logger = slog.Default()
	}
	return &CollaborativePlanner{
		planner:   planner,
		llm:       llmClient,
		workspace: workspace,
		logger:    logger,
		pending:   make(map[string]*PlanReview),
	}
}

// IsProgrammingTask checks whether a message describes a programming or
// automation task that should go through collaborative review.
func (c *CollaborativePlanner) IsProgrammingTask(message string) bool {
	lower := strings.ToLower(message)
	hits := 0
	for _, kw := range programmingKeywords {
		if strings.Contains(lower, kw) {
			hits++
		}
	}
	return hits >= 2
}

// HasPendingReview returns whether the conversation has a pending plan review.
func (c *CollaborativePlanner) HasPendingReview(conversationID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	review, ok := c.pending[conversationID]
	return ok && review.Status == PlanStatusPendingApproval
}

// ClassifyResponse classifies a follow-up message as approval, rejection, or revision.
func (c *CollaborativePlanner) ClassifyResponse(message string) string {
	lower := strings.ToLower(strings.TrimSpace(message))

	// Check approval first (more specific phrases)
	for _, pat := range approvalPatterns {
		if strings.Contains(lower, pat) {
			// But if revision indicators are also present, treat as revision
			for _, rev := range revisionPatterns {
				if strings.Contains(lower, rev) {
					return "revise"
				}
			}
			return "approve"
		}
	}

	for _, pat := range rejectionPatterns {
		if strings.Contains(lower, pat) {
			return "reject"
		}
	}

	// Default: treat unrecognised follow-ups as revision feedback
	return "revise"
}

// PlanAndReview decomposes, analyses, and prepares a plan for user review.
func (c *CollaborativePlanner) PlanAndReview(ctx context.Context, message, conversationID string) (*PlanReview, error) {
	taskID := uuid.New().String()[:16]

	// 1. Create workspace
	workspacePath, err := c.workspace.Create(ctx, taskID, message)
	if err != nil {
		return nil, fmt.Errorf("failed to create workspace: %w", err)
	}

	// 2. Decompose
	steps, err := c.planner.Decompose(ctx, message)
	if err != nil {
		return nil, fmt.Errorf("failed to decompose task: %w", err)
	}

	plan := &TaskPlan{
		ID:            taskID,
		Description:   message,
		Steps:         steps,
		Status:        TaskStatusPendingApproval,
		WorkspacePath: workspacePath,
	}

	// 3. Write plan to workspace
	planInfo := TaskPlanInfo{
		ID:          taskID,
		Description: message,
	}
	for _, s := range steps {
		planInfo.Steps = append(planInfo.Steps, TaskStepInfo{
			ID:          s.ID,
			Description: s.Description,
			DependsOn:   s.DependsOn,
			ToolHint:    s.ToolHint,
		})
	}
	if _, err := c.workspace.WritePlan(ctx, taskID, planInfo); err != nil {
		c.logger.Warn("failed to write plan", "error", err)
	}

	// 4. LLM analysis pass
	analysis := c.analysePlan(ctx, plan)
	plan.Analysis = analysis

	// 5. Write review to workspace
	if _, err := c.workspace.WriteReview(ctx, taskID, analysis); err != nil {
		c.logger.Warn("failed to write review", "error", err)
	}

	// 6. Build formatted summary
	summary := c.formatSummary(plan, analysis)

	review := &PlanReview{
		TaskID:           taskID,
		Plan:             plan,
		Analysis:         analysis,
		Status:           PlanStatusPendingApproval,
		WorkspacePath:    workspacePath,
		FormattedSummary: summary,
	}

	// Track for follow-up messages
	c.mu.Lock()
	c.pending[conversationID] = review
	c.mu.Unlock()

	c.workspace.AppendLog(ctx, taskID, "Plan created and pending approval")
	return review, nil
}

// Approve marks the pending plan as approved.
func (c *CollaborativePlanner) Approve(ctx context.Context, conversationID string) (*TaskPlan, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	review, ok := c.pending[conversationID]
	if !ok || review.Status != PlanStatusPendingApproval {
		return nil, fmt.Errorf("no pending plan for conversation %s", conversationID)
	}

	review.Status = PlanStatusApproved
	review.Plan.Approved = true
	review.Plan.Status = TaskStatusPending

	c.workspace.AppendLog(ctx, review.TaskID, "Plan approved by user")
	c.workspace.Commit(ctx, review.TaskID, "Plan approved", nil)

	// Remove from pending
	delete(c.pending, conversationID)

	return review.Plan, nil
}

// Reject rejects the pending plan.
func (c *CollaborativePlanner) Reject(ctx context.Context, conversationID, reason string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	review, ok := c.pending[conversationID]
	if !ok {
		return nil
	}

	review.Status = PlanStatusRejected
	review.Plan.Status = TaskStatusCancelled

	logMsg := "Plan rejected"
	if reason != "" {
		logMsg = fmt.Sprintf("Plan rejected: %s", reason)
	}
	c.workspace.AppendLog(ctx, review.TaskID, logMsg)
	c.workspace.Commit(ctx, review.TaskID, "Plan rejected", nil)

	delete(c.pending, conversationID)
	return nil
}

// Revise revises the pending plan based on user feedback.
func (c *CollaborativePlanner) Revise(ctx context.Context, conversationID, feedback string) (*PlanReview, error) {
	c.mu.Lock()
	review, ok := c.pending[conversationID]
	c.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("no pending plan for conversation %s", conversationID)
	}

	taskID := review.TaskID
	originalDesc := review.Plan.Description

	// Re-decompose with feedback incorporated
	revisedPrompt := fmt.Sprintf("%s\n\nAdditional requirements/feedback:\n%s", originalDesc, feedback)
	steps, err := c.planner.Decompose(ctx, revisedPrompt)
	if err != nil {
		return nil, fmt.Errorf("failed to decompose revised task: %w", err)
	}

	plan := &TaskPlan{
		ID:            taskID,
		Description:   originalDesc,
		Steps:         steps,
		Status:        TaskStatusPendingApproval,
		WorkspacePath: review.WorkspacePath,
	}

	// Write revised plan
	planInfo := TaskPlanInfo{
		ID:          taskID,
		Description: originalDesc,
	}
	for _, s := range steps {
		planInfo.Steps = append(planInfo.Steps, TaskStepInfo{
			ID:          s.ID,
			Description: s.Description,
			DependsOn:   s.DependsOn,
			ToolHint:    s.ToolHint,
		})
	}
	c.workspace.WritePlan(ctx, taskID, planInfo)

	// Re-analyse
	analysis := c.analysePlan(ctx, plan)
	plan.Analysis = analysis
	c.workspace.WriteReview(ctx, taskID, analysis)

	summary := c.formatSummary(plan, analysis)

	review.Plan = plan
	review.Analysis = analysis
	review.Status = PlanStatusPendingApproval
	review.FormattedSummary = summary

	c.mu.Lock()
	c.pending[conversationID] = review
	c.mu.Unlock()

	truncatedFeedback := feedback
	if len(truncatedFeedback) > 80 {
		truncatedFeedback = truncatedFeedback[:80]
	}
	c.workspace.AppendLog(ctx, taskID, fmt.Sprintf("Plan revised based on feedback: %s", truncatedFeedback))
	c.workspace.Commit(ctx, taskID, "Plan revised", nil)

	return review, nil
}

// analysePlan runs the LLM analysis pass over the plan.
func (c *CollaborativePlanner) analysePlan(ctx context.Context, plan *TaskPlan) string {
	if c.llm == nil {
		return "Analysis unavailable (no LLM client)."
	}

	planText := c.formatPlanForAnalysis(plan)

	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: analysisSystemPrompt},
		{Role: llm.RoleUser, Content: fmt.Sprintf(
			"Task: %s\n\nProposed plan:\n%s\n\nPlease review this plan and identify any issues.",
			plan.Description, planText,
		)},
	}

	resp, err := c.llm.Chat(ctx, messages)
	if err != nil {
		c.logger.Warn("LLM analysis pass failed", "error", err)
		return "Analysis unavailable (LLM call failed)."
	}

	return resp.Content
}

// formatPlanForAnalysis formats a plan's steps as numbered text for the analysis prompt.
func (c *CollaborativePlanner) formatPlanForAnalysis(plan *TaskPlan) string {
	var sb strings.Builder
	for i, step := range plan.Steps {
		deps := ""
		if len(step.DependsOn) > 0 {
			deps = fmt.Sprintf(" [depends on: %s]", strings.Join(step.DependsOn, ", "))
		}
		hint := ""
		if step.ToolHint != "" {
			hint = fmt.Sprintf(" (tool: %s)", step.ToolHint)
		}
		sb.WriteString(fmt.Sprintf("%d. %s%s%s\n", i+1, step.Description, hint, deps))
	}
	return sb.String()
}

// formatSummary builds a human-readable summary of the plan + review.
func (c *CollaborativePlanner) formatSummary(plan *TaskPlan, analysis string) string {
	var sb strings.Builder

	desc := plan.Description
	if len(desc) > 100 {
		desc = desc[:100]
	}
	sb.WriteString(fmt.Sprintf("## Task Plan: %s\n\n", desc))
	sb.WriteString("### Steps\n")

	for i, step := range plan.Steps {
		deps := ""
		if len(step.DependsOn) > 0 {
			deps = fmt.Sprintf(" *(after: %s)*", strings.Join(step.DependsOn, ", "))
		}
		sb.WriteString(fmt.Sprintf("%d. %s%s\n", i+1, step.Description, deps))
	}

	sb.WriteString("\n### Review\n")
	sb.WriteString(analysis)
	sb.WriteString("\n\n---\n")
	sb.WriteString("Reply **approve** / **go ahead** to execute, ")
	sb.WriteString("**reject** to cancel, or provide feedback to revise.")

	return sb.String()
}
