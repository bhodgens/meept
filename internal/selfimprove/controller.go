// Package selfimprove provides the self-improvement system for meept.
package selfimprove

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/models"
	"github.com/google/uuid"
)

// statusTopic is the bus topic where self-improve cycle status updates are
// published. Subscribers observe the full lifecycle of a cycle
// (started, detecting, analyzing, generating, validating, applying, completed).
const statusTopic = "selfimprove.status"

// ProgressCallback is invoked during cycle execution to report progress.
// phase is one of "detecting", "analyzing", "generating", "validating",
// "applying". progress is 0.0-1.0. message is a human-readable description.
type ProgressCallback func(phase string, progress float64, message string)

// Controller orchestrates the full self-improvement cycle.
type Controller struct {
	mu sync.RWMutex

	config      Config
	bus         *bus.MessageBus
	llmClient   *llm.Client
	projectRoot string
	logger      *slog.Logger

	// Components
	detector  *IssueDetector
	analyzer  *RootCauseAnalyzer
	generator *PatchGenerator
	validator *FixValidator
	applier   *ChangeApplier

	// State
	currentCycle *ImprovementCycle
	cycles       []*ImprovementCycle
	issues       []Issue
	analyses     []*RootCauseAnalysis
	fixes        []*ProposedFix
	validations  []*ValidationResult
	applied      []*AppliedFix
	initialized  bool

	// Error tracking for circuit breaker
	failureCounts        map[string]int // issue_id -> failure count
	consecutiveFailures  int

	// Optional progress callback for external observers (TUI, RPC).
	progressCallback ProgressCallback
}

// NewController creates a new Controller.
func NewController(cfg Config, msgBus *bus.MessageBus, llmClient *llm.Client, projectRoot string, logger *slog.Logger) *Controller {
	if projectRoot == "" {
		projectRoot, _ = os.Getwd()
	}
	if logger == nil {
		logger = slog.Default()
	}

	// Validate config
	cfg.Validate()

	c := &Controller{
		config:        cfg,
		bus:           msgBus,
		llmClient:     llmClient,
		projectRoot:   projectRoot,
		logger:        logger,
		failureCounts: make(map[string]int),
	}

	// Initialize components
	c.detector = NewIssueDetector(cfg.Detection, projectRoot, logger)
	c.analyzer = NewRootCauseAnalyzer(cfg.AIInfra, llmClient, projectRoot, logger)
	c.generator = NewPatchGenerator(cfg.AIInfra, cfg.Safety, llmClient, projectRoot, logger)
	c.validator = NewFixValidator(cfg.Sandbox, cfg.Safety, projectRoot, logger)
	c.applier = NewChangeApplier(cfg.Safety, projectRoot, msgBus, logger)

	return c
}

// Initialize loads persisted state.
func (c *Controller) Initialize(ctx context.Context) error {
	if c.initialized {
		return nil
	}

	if err := c.loadState(); err != nil {
		c.logger.Warn("failed to load state", "error", err)
	}

	c.initialized = true
	c.logger.Info("controller initialized",
		"cycles", len(c.cycles),
		"issues", len(c.issues),
		"fixes", len(c.fixes))

	return nil
}

// RunFullCycle runs a complete improvement cycle.
func (c *Controller) RunFullCycle(ctx context.Context, interactive bool) (*ImprovementCycle, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}

	cycleID := fmt.Sprintf("cycle-%s", uuid.New().String()[:8])
	c.currentCycle = &ImprovementCycle{
		ID:        cycleID,
		Status:    CycleStatusRunning,
		StartedAt: time.Now(),
	}

	c.logger.Info("starting improvement cycle", "cycle_id", cycleID)
	c.publishStatus("started", map[string]any{"cycle_id": cycleID})
	c.emitProgress("started", 0.0, "cycle "+cycleID+" starting")

	defer func() {
		now := time.Now()
		c.currentCycle.CompletedAt = &now
		c.cycles = append(c.cycles, c.currentCycle)
		c.saveState()
	}()

	// Phase 1: Detection
	c.logger.Info("phase 1 - detecting issues")
	c.publishStatus("detecting", map[string]any{"cycle_id": cycleID})
	c.emitProgress("detecting", 0.1, "detecting issues in codebase")

	issues, err := c.detector.DetectAll(ctx)
	if err != nil {
		c.currentCycle.Status = CycleStatusFailed
		c.currentCycle.Error = err.Error()
		return c.currentCycle, err
	}
	c.issues = issues
	c.currentCycle.IssuesDetected = len(issues)
	c.emitProgress("detecting", 0.2, fmt.Sprintf("detected %d issues", len(issues)))

	if len(issues) == 0 {
		c.logger.Info("no issues detected")
		c.currentCycle.Status = CycleStatusCompleted
		return c.currentCycle, nil
	}

	// Phase 2: Analysis
	c.logger.Info("phase 2 - analyzing issues", "count", len(issues))
	c.publishStatus("analyzing", map[string]any{"cycle_id": cycleID, "issues_count": len(issues)})
	c.emitProgress("analyzing", 0.3, fmt.Sprintf("analyzing %d issues", len(issues)))

	c.analyses = nil
	for _, issue := range issues[:min(len(issues), c.config.MaxIterationsPerCycle)] {
		if c.checkCircuitBreaker() {
			c.logger.Warn("stopping analysis due to circuit breaker")
			break
		}

		if c.shouldSkipIssue(issue.ID) {
			continue
		}

		analysis, err := c.analyzer.Analyze(ctx, issue)
		if err != nil {
			c.recordFailure(issue.ID)
			continue
		}
		c.analyses = append(c.analyses, analysis)
		c.currentCycle.IssuesAnalyzed++
		c.recordSuccess(issue.ID)
	}

	if len(c.analyses) == 0 {
		c.logger.Warn("no analyses completed")
		c.currentCycle.Status = CycleStatusCompleted
		return c.currentCycle, nil
	}

	// Phase 3: Generation
	c.logger.Info("phase 3 - generating fixes", "analyses_count", len(c.analyses))
	c.publishStatus("generating", map[string]any{"cycle_id": cycleID, "analyses_count": len(c.analyses)})
	c.emitProgress("generating", 0.5, fmt.Sprintf("generating fixes for %d analyses", len(c.analyses)))

	c.fixes = nil
	issueMap := make(map[string]Issue)
	for _, issue := range issues {
		issueMap[issue.ID] = issue
	}

	for _, analysis := range c.analyses[:min(len(c.analyses), c.config.MaxFixesPerCycle)] {
		if c.checkCircuitBreaker() {
			break
		}

		if c.shouldSkipIssue(analysis.IssueID) {
			continue
		}

		issue := issueMap[analysis.IssueID]
		fix, err := c.generator.Generate(ctx, analysis, issue)
		if err != nil {
			c.recordFailure(analysis.IssueID)
			continue
		}
		if fix != nil {
			c.fixes = append(c.fixes, fix)
			c.currentCycle.FixesGenerated++
			c.recordSuccess(analysis.IssueID)
		}
	}

	if len(c.fixes) == 0 {
		c.logger.Warn("no fixes generated")
		c.currentCycle.Status = CycleStatusCompleted
		return c.currentCycle, nil
	}

	// Phase 4: Validation
	c.logger.Info("phase 4 - validating fixes", "fixes_count", len(c.fixes))
	c.publishStatus("validating", map[string]any{"cycle_id": cycleID, "fixes_count": len(c.fixes)})
	c.emitProgress("validating", 0.7, fmt.Sprintf("validating %d fixes", len(c.fixes)))

	c.validations = nil
	for _, fix := range c.fixes {
		result, err := c.validator.Validate(ctx, fix)
		if err != nil {
			c.recordFailure(fix.IssueID)
			continue
		}
		c.validations = append(c.validations, result)
		if result.Success {
			c.currentCycle.FixesValidated++
			c.recordSuccess(fix.IssueID)
		} else {
			c.recordFailure(fix.IssueID)
		}
	}

	// Phase 5: Application
	validatedFixes := c.getValidatedFixes()
	if len(validatedFixes) == 0 {
		c.logger.Warn("no fixes passed validation")
		c.currentCycle.Status = CycleStatusCompleted
		return c.currentCycle, nil
	}

	c.logger.Info("phase 5 - applying fixes", "validated_count", len(validatedFixes))
	c.publishStatus("applying", map[string]any{"cycle_id": cycleID, "validated_count": len(validatedFixes)})
	c.emitProgress("applying", 0.9, fmt.Sprintf("applying %d validated fixes", len(validatedFixes)))

	for _, pair := range validatedFixes {
		approvedBy := "auto"
		if interactive {
			approvedBy = "human"
		}

		if c.config.Safety.RequireHumanApproval && !interactive {
			c.logger.Info("fix requires human approval", "fix_id", pair.fix.ID)
			continue
		}

		applied, err := c.applier.Apply(ctx, pair.fix, pair.validation, approvedBy)
		if err == ErrApprovalRequired {
			c.logger.Info("fix pending approval", "fix_id", pair.fix.ID)
			continue
		}
		if err != nil {
			c.logger.Warn("failed to apply fix", "fix_id", pair.fix.ID, "error", err)
			c.recordFailure(pair.fix.IssueID)
			continue
		}
		c.applied = append(c.applied, applied)
		c.currentCycle.FixesApplied++
		c.recordSuccess(pair.fix.IssueID)
	}

	c.currentCycle.Status = CycleStatusCompleted

	c.logger.Info("cycle completed",
		"cycle_id", cycleID,
		"detected", c.currentCycle.IssuesDetected,
		"analyzed", c.currentCycle.IssuesAnalyzed,
		"generated", c.currentCycle.FixesGenerated,
		"validated", c.currentCycle.FixesValidated,
		"applied", c.currentCycle.FixesApplied)

	c.publishStatus("completed", c.currentCycle)
	c.emitProgress("completed", 1.0, fmt.Sprintf("cycle completed: %d applied", c.currentCycle.FixesApplied))
	return c.currentCycle, nil
}

type fixValidationPair struct {
	fix        *ProposedFix
	validation *ValidationResult
}

func (c *Controller) getValidatedFixes() []fixValidationPair {
	fixMap := make(map[string]*ProposedFix)
	for _, fix := range c.fixes {
		fixMap[fix.ID] = fix
	}

	var pairs []fixValidationPair
	for _, v := range c.validations {
		if v.Success {
			if fix, ok := fixMap[v.FixID]; ok {
				pairs = append(pairs, fixValidationPair{fix: fix, validation: v})
			}
		}
	}
	return pairs
}

// Detect runs only the detection phase.
func (c *Controller) Detect(ctx context.Context) ([]Issue, error) {
	issues, err := c.detector.DetectAll(ctx)
	if err != nil {
		return nil, err
	}
	c.issues = issues
	c.saveState()
	return issues, nil
}

// GetApplier returns the change applier for direct testing access.
func (c *Controller) GetApplier() *ChangeApplier {
	return c.applier
}

// GetStatus returns the current status.
func (c *Controller) GetStatus() *ControllerStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	pendingApprovals := make([]string, 0)
	for id := range c.applier.PendingApprovals() {
		pendingApprovals = append(pendingApprovals, id)
	}

	return &ControllerStatus{
		CurrentCycle:          c.currentCycle,
		IssuesCount:           len(c.issues),
		AnalysesCount:         len(c.analyses),
		FixesCount:            len(c.fixes),
		ValidationsCount:      len(c.validations),
		AppliedCount:          len(c.applied),
		ConsecutiveFailures:   c.consecutiveFailures,
		CircuitBreakerTripped: c.checkCircuitBreaker(),
		FailedIssues:          c.failureCounts,
		PendingApprovals:      pendingApprovals,
		CyclesCompleted:       len(c.cycles),
	}
}

// SetProgressCallback sets an optional callback invoked during cycle execution
// to report phase progress. Safe to call before or after Initialize.
func (c *Controller) SetProgressCallback(cb ProgressCallback) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.progressCallback = cb
}

// emitProgress invokes the progress callback if one is registered.
func (c *Controller) emitProgress(phase string, progress float64, message string) {
	c.mu.RLock()
	cb := c.progressCallback
	c.mu.RUnlock()
	if cb != nil {
		cb(phase, progress, message)
	}
}

// ApproveFix approves a pending fix and applies it.
func (c *Controller) ApproveFix(ctx context.Context, fixID string) (*AppliedFix, error) {
	if c.applier == nil {
		return nil, fmt.Errorf("controller not initialized")
	}
	applied, err := c.applier.Approve(ctx, fixID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.applied = append(c.applied, applied)
	c.mu.Unlock()
	c.logger.Info("fix approved and applied", "fix_id", fixID)
	return applied, nil
}

// RejectFix rejects a pending fix.
func (c *Controller) RejectFix(fixID, reason string) error {
	if c.applier == nil {
		return fmt.Errorf("controller not initialized")
	}
	return c.applier.Reject(fixID, reason)
}

// Stop stops the controller.
func (c *Controller) Stop() error {
	c.validator.Cleanup()
	c.analyzer.Close()
	c.generator.Close()
	return nil
}

func (c *Controller) recordFailure(issueID string) {
	c.failureCounts[issueID]++
	c.consecutiveFailures++
}

func (c *Controller) recordSuccess(issueID string) {
	c.consecutiveFailures = 0
}

func (c *Controller) shouldSkipIssue(issueID string) bool {
	return c.failureCounts[issueID] >= c.config.Safety.MaxFailuresPerIssue
}

func (c *Controller) checkCircuitBreaker() bool {
	return c.consecutiveFailures >= c.config.Safety.MaxConsecutiveFailures
}

func (c *Controller) publishStatus(phase string, data any) {
	if c.bus == nil {
		return
	}
	cycleID := ""
	if c.currentCycle != nil {
		cycleID = c.currentCycle.ID
	}
	msg, err := models.NewBusMessage(
		models.MessageTypeStatusUpdate,
		"selfimprove."+cycleID,
		map[string]any{
			"phase":    phase,
			"cycle_id": cycleID,
			"data":     data,
		},
	)
	if err != nil {
		c.logger.Warn("failed to build status bus message", "phase", phase, "error", err)
		return
	}
	c.bus.Publish(statusTopic, msg)
}

// persistedState is the on-disk shape of the controller state. It is kept
// as a distinct type so that loadState can deserialize it deterministically
// and populate the controller's in-memory fields.
type persistedState struct {
	Issues              []Issue                `json:"issues"`
	Analyses            []*RootCauseAnalysis   `json:"analyses"`
	Fixes               []*ProposedFix         `json:"fixes"`
	Validations         []*ValidationResult    `json:"validations"`
	Applied             []*AppliedFix          `json:"applied"`
	Cycles              []*ImprovementCycle    `json:"cycles"`
	FailureCounts       map[string]int         `json:"failure_counts"`
	ConsecutiveFailures int                    `json:"consecutive_failures"`
	Timestamp           time.Time              `json:"timestamp"`
}

func (c *Controller) saveState() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	os.MkdirAll(c.config.DataPath, 0755)

	state := persistedState{
		Issues:              c.issues,
		Analyses:            c.analyses,
		Fixes:               c.fixes,
		Validations:         c.validations,
		Applied:             c.applied,
		Cycles:              c.cycles,
		FailureCounts:       c.failureCounts,
		ConsecutiveFailures: c.consecutiveFailures,
		Timestamp:           time.Now(),
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	statePath := filepath.Join(c.config.DataPath, "state.json")
	return os.WriteFile(statePath, data, 0644)
}

func (c *Controller) loadState() error {
	statePath := filepath.Join(c.config.DataPath, "state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		return err
	}

	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}

	c.mu.Lock()
	c.issues = state.Issues
	c.analyses = state.Analyses
	c.fixes = state.Fixes
	c.validations = state.Validations
	c.applied = state.Applied
	c.cycles = state.Cycles
	if state.FailureCounts != nil {
		c.failureCounts = state.FailureCounts
	}
	c.consecutiveFailures = state.ConsecutiveFailures
	c.mu.Unlock()

	c.logger.Info("loaded state from disk",
		"issues", len(c.issues),
		"analyses", len(c.analyses),
		"fixes", len(c.fixes),
		"applied", len(c.applied),
		"cycles", len(c.cycles))
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
