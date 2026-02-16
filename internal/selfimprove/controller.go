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
	"github.com/google/uuid"
)

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

	defer func() {
		now := time.Now()
		c.currentCycle.CompletedAt = &now
		c.cycles = append(c.cycles, c.currentCycle)
		c.saveState()
	}()

	// Phase 1: Detection
	c.logger.Info("phase 1 - detecting issues")
	c.publishStatus("detecting", map[string]any{"cycle_id": cycleID})

	issues, err := c.detector.DetectAll(ctx)
	if err != nil {
		c.currentCycle.Status = CycleStatusFailed
		c.currentCycle.Error = err.Error()
		return c.currentCycle, err
	}
	c.issues = issues
	c.currentCycle.IssuesDetected = len(issues)

	if len(issues) == 0 {
		c.logger.Info("no issues detected")
		c.currentCycle.Status = CycleStatusCompleted
		return c.currentCycle, nil
	}

	// Phase 2: Analysis
	c.logger.Info("phase 2 - analyzing issues", "count", len(issues))
	c.publishStatus("analyzing", map[string]any{"cycle_id": cycleID, "issues_count": len(issues)})

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

	c.validations = nil
	for _, fix := range c.fixes {
		result, err := c.validator.Validate(ctx, fix)
		if err != nil {
			continue
		}
		c.validations = append(c.validations, result)
		if result.Success {
			c.currentCycle.FixesValidated++
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
			continue
		}
		c.applied = append(c.applied, applied)
		c.currentCycle.FixesApplied++
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
	// Publish status update to bus
	// Implementation depends on bus interface
}

func (c *Controller) saveState() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	os.MkdirAll(c.config.DataPath, 0755)

	state := map[string]any{
		"issues":      c.issues,
		"analyses":    c.analyses,
		"fixes":       c.fixes,
		"validations": c.validations,
		"applied":     c.applied,
		"cycles":      c.cycles,
		"timestamp":   time.Now(),
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

	var state map[string]json.RawMessage
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}

	// Load each component (simplified - would need proper deserialization)
	c.logger.Info("loaded state from disk")
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
