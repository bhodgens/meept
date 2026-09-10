package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/errcls"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/validator"
	"github.com/caimlas/meept/pkg/models"
)

// ErrNoExecutionSlot is returned when the semaphore blocks a step from executing.
var ErrNoExecutionSlot = errors.New("no available execution slot")

// DefaultQuotaDeferralPolicy bounds quota-aware job deferral: a
// quota-classified job failure re-queues the job instead of failing it, at
// most MaxDeferrals times and within MaxTotalDeferral of the FIRST deferral;
// past either bound the job fails with a "quota-deferred exhausted" error.
// Defaults follow the quota-resilience max-wait shape (10 attempts, 6h) —
// an agnes 5h quota park fits inside one deferral window.
func DefaultQuotaDeferralPolicy() QuotaDeferralPolicy {
	return QuotaDeferralPolicy{
		MaxDeferrals:      10,
		MaxTotalDeferral:  6 * time.Hour,
		UnknownResetDelay: 5 * time.Minute,
	}
}

// QuotaDeferralPolicy configures quota-aware job deferral in
// TacticalScheduler.OnJobFailed. A policy with MaxDeferrals<=0 or
// MaxTotalDeferral<=0 disables deferral (legacy failure behavior).
type QuotaDeferralPolicy struct {
	// MaxDeferrals caps how many times one step may be quota-deferred.
	MaxDeferrals int
	// MaxTotalDeferral caps the wall-clock span from the step's first
	// deferral; a reset scheduled past the span fails the step instead.
	MaxTotalDeferral time.Duration
	// UnknownResetDelay is the requeue delay when the quota reset time is
	// unknown (ErrAllModelsQuotaBlocked carries no schedule).
	UnknownResetDelay time.Duration
}

// tacticalSeq provides a monotonically increasing sequence counter for handoff steps,
// replacing the old time.Now().UnixNano()%1000 pattern that produced predictable IDs.
var tacticalSeq atomic.Uint64

// quotaResetPatterns extracts RFC3339 timestamps from quota error text for
// the scheduler's reset-time recovery. QuotaResetError.Error() renders
// "resets_at=RFC3339"; the daemon's job-level quota stamp renders
// "... rate-limited until RFC3339. ..."; bus JSON frequently carries the
// same timestamps in `"resets_at":"RFC3339"` shape. QuotaResetError.ResetAt
// itself never survives the message-bus stringification, so this regex IS
// the recovery path for OnJobFailed (which receives only a string).
var quotaResetPatterns = regexp.MustCompile(
	`resets_at[=:]\s*"?(\d{4}-\d{2}-\d{2}T[^\s",}]+)"?` +
		`|rate-limited until (\d{4}-\d{2}-\d{2}T[^\s",}]+)` +
		`|"resets_at"\s*:\s*"(\d{4}-\d{2}-\d{2}T[^"]+)"`)

// StepJobPayload is the payload stored in a queue job for a task step.
type StepJobPayload struct {
	StepID               string   `json:"step_id"`
	TaskID               string   `json:"task_id"`
	Description          string   `json:"description"`
	ToolHint             string   `json:"tool_hint,omitempty"`
	MemoryRefs           []string `json:"memory_refs,omitempty"`
	AccumulatedContext   string   `json:"accumulated_context,omitempty"`
	ValidationRetryCount int      `json:"validation_retry_count,omitempty"`
	// SessionID is the originating session (audit R4): recoverable only via
	// Task.LinkedSessions at schedule time, it rides the payload so the
	// interactive stamp is evaluable and auditable on the job itself.
	SessionID string `json:"session_id,omitempty"`
}

// TacticalScheduler schedules ready steps as queue jobs and handles completion callbacks.
type TacticalScheduler struct {
	stepStore              *task.StepStore
	taskStore              *task.Store
	queue                  queue.Queue
	registry               *AgentRegistry
	bus                    *bus.MessageBus
	pairManager            *PairManager
	reviewManager          *ReviewManager
	validatorManager       *validator.ValidatorManager
	escalationManager      *EscalationManager
	logger                 *slog.Logger
	globalSemaphore        chan struct{}            // Global execution limit
	agentSemaphore         map[string]chan struct{} // Per-agent concurrency slots
	semaphoreMu            sync.Mutex               // Protects agentSemaphore map
	validationGateInterval int                      // Run validation gate every N steps
	validationGateCounter  map[string]int           // Per-task validation gate counter
	validationGateMu       sync.Mutex               // Protects validationGateCounter
	maxHandoffSteps        int                      // Max handoff steps per task (0 = unlimited)
	handoffUseAmendment    bool                     // Route handoffs through amendment system
	amendmentMgr           AmendmentSubmitter       // Optional: enables amendment-based step creation

	// Quota deferral (quota-aware job deferral): when a step job's failure
	// is quota-class, the step is DEFERRED instead of failed — the job is
	// requeued to pending (no retry consumed) with a not-before gate at the
	// quota reset time, so it survives multi-hour provider quota parks.
	// Counters are per step ID; cleaned up on terminal completion/failure.
	quotaDeferrals      map[string]int       // step ID -> deferral count
	quotaDeferralFirst  map[string]time.Time // step ID -> first deferral time
	quotaDeferralPolicy QuotaDeferralPolicy

	// quotaDeferralMu protects the two quota-deferral counters.
	quotaDeferralMu sync.Mutex

	// handoffPropagator, when set, replaces propagateContextToNextStepsLegacy.
	// Set by the daemon when the orchestrator is wired with handoff deps
	// (templateReg + registry + LLM). Nil falls back to the legacy 500-char
	// truncation path.
	handoffPropagator func(ctx context.Context, completedStep *task.TaskStep) error

	// contextWindowProvider resolves the executor model's context window
	// per agent ID (allotment tree leaf 02). Set via SetContextWindowProvider.
	// Nil = unknown windows everywhere: allotment batching never engages.
	contextWindowProvider func(agentID string) int

	// allotmentCfg holds the allotment math parameters, defaulted in
	// NewTacticalScheduler to DefaultAllotmentConfig().
	allotmentCfg AllotmentConfig

	// sessionStore resolves Task.LinkedSessions → *session.Session for the
	// interactive stamp (tree 04 leaf 02, D11). Narrow interface so tests
	// don't implement the full session.Store. Nil = no session context; all
	// jobs then stamp Interactive=false by construction (R4 (c)).
	sessions sessionStoreReader

	// stepStoreReadHook, when set, is consulted before every
	// stepStore.GetByID/GetByJobID read. Returning a non-nil override
	// short-circuits the real read — the test seam that reproduces the
	// 2026-09-05 SQLITE_BUSY panic (GetByID returning (nil, err) at the
	// step-state refresh) deterministically. Production never sets it.
	stepStoreReadHook func(id string, byJob bool) (*task.TaskStep, error)
}

// sessionStoreReader is the narrow session lookup the scheduler needs.
// Mirrors handler.go's SessionStoreReader rationale: a local interface keeps
// tests free of the 30+ method session.Store.
type sessionStoreReader interface {
	Get(id string) *session.Session
}

// SetSessionStore installs the session lookup used for the interactive stamp.
// nil is ignored (leaves session-less stamping active).
func (ts *TacticalScheduler) SetSessionStore(store sessionStoreReader) {
	if store != nil {
		ts.sessions = store
	}
}

// getStepByID fetches a step by ID through the optional test read hook.
// The hook short-circuits with its override when set (test-only fault
// injection); production never sets it and always hits the real store.
func (ts *TacticalScheduler) getStepByID(id string) (*task.TaskStep, error) {
	if ts.stepStoreReadHook != nil {
		if step, err := ts.stepStoreReadHook(id, false); step != nil || err != nil {
			return step, err
		}
	}
	return ts.stepStore.GetByID(id)
}

// getStepByJobID is getStepByID's by-job-ID counterpart.
func (ts *TacticalScheduler) getStepByJobID(jobID string) (*task.TaskStep, error) {
	if ts.stepStoreReadHook != nil {
		if step, err := ts.stepStoreReadHook(jobID, true); step != nil || err != nil {
			return step, err
		}
	}
	return ts.stepStore.GetByJobID(jobID)
}

// resolveStepSession backfills step.SessionID from the task's linked sessions
// (audit R4 (b)): the job payload carries no origin today, so TaskID →
// LinkedSessions is the only recovery path. First linked session wins; tasks
// linked to no session leave the field empty (stamps false by construction).
func (ts *TacticalScheduler) resolveStepSession(step *task.TaskStep) {
	if ts.taskStore == nil || step.TaskID == "" {
		return
	}
	sessions, err := ts.taskStore.GetLinkedSessions(step.TaskID)
	if err != nil {
		ts.logger.Warn("Failed to resolve linked sessions for step session provenance",
			"step_id", step.ID,
			"task_id", step.TaskID,
			"error", err,
		)
		return
	}
	if len(sessions) == 0 {
		return
	}
	step.SessionID = sessions[0]
	// Persist so validation-retry jobs (which rebuild the payload from the
	// stored step) inherit the same origin.
	if err := ts.stepStore.SetSessionID(step.ID, step.SessionID); err != nil {
		ts.logger.Warn("Failed to persist step session provenance",
			"step_id", step.ID,
			"error", err,
		)
	}
}

// stepJobPayloadFromStep builds the queue payload for a step, carrying the
// persisted session provenance through to the job.
func stepJobPayloadFromStep(step *task.TaskStep) StepJobPayload {
	return StepJobPayload{
		StepID:               step.ID,
		TaskID:               step.TaskID,
		Description:          step.Description,
		ToolHint:             step.ToolHint,
		MemoryRefs:           step.MemoryRefs,
		AccumulatedContext:   step.AccumulatedContext,
		ValidationRetryCount: step.ValidationRetryCount,
		SessionID:            step.SessionID,
	}
}

// stampInteractive sets job.Interactive from the originating session's live
// signal (D11: recent user message within the window OR foreground flag).
// The window comes from the queue config; failures fall back to the Q1 5m
// default via the config getter, and a missing store/session stamps false.
func (ts *TacticalScheduler) stampInteractive(job *queue.Job, sessionID string) {
	if sessionID == "" || ts.sessions == nil {
		return // leaves Interactive=false (R4 (c): session-less by construction)
	}
	sess := ts.sessions.Get(sessionID)
	if sess == nil {
		return
	}
	window := config.DefaultConfig().InteractiveWindow()
	job.WithInteractive(session.IsInteractive(sess, time.Now(), window))
	if job.Interactive {
		ts.logger.Debug("Step job stamped interactive",
			"job_id", job.ID,
			"session_id", sessionID,
		)
	}
}

// SetHandoffPropagator installs a callback that replaces the legacy
// 500-char truncation propagation. nil is ignored (leaves the legacy path active).
func (ts *TacticalScheduler) SetHandoffPropagator(fn func(ctx context.Context, completedStep *task.TaskStep) error) {
	if fn != nil {
		ts.handoffPropagator = fn
	}
}

// SetContextWindowProvider installs the executor model's context-window
// lookup (allotment tree leaf 02). nil is ignored (allotment batching stays
// disabled; legacy scheduling behavior), mirroring SetHandoffPropagator.
func (ts *TacticalScheduler) SetContextWindowProvider(fn func(agentID string) int) {
	if fn != nil {
		ts.contextWindowProvider = fn
	}
}

// contextWindowFor resolves the executor model's context window for an
// agent ID. A nil provider yields 0 (unknown window = legacy behavior).
func (ts *TacticalScheduler) contextWindowFor(agentID string) int {
	if ts.contextWindowProvider == nil {
		return 0
	}
	return ts.contextWindowProvider(agentID)
}

// AmendmentSubmitter is the interface for submitting amendment requests.
// Implemented by *task.AmendmentManager.
type AmendmentSubmitter interface {
	Submit(ctx context.Context, req *task.AmendmentRequest) error
	Process(ctx context.Context, requestID string) (*task.AmendmentReply, error)
}

// TacticalSchedulerConfig holds configuration for the tactical scheduler.
type TacticalSchedulerConfig struct {
	StepStore              *task.StepStore
	TaskStore              *task.Store
	Queue                  queue.Queue
	Registry               *AgentRegistry
	Bus                    *bus.MessageBus
	PairManager            *PairManager
	ReviewManager          *ReviewManager
	ValidatorManager       *validator.ValidatorManager
	EscalationManager      *EscalationManager
	Logger                 *slog.Logger
	MaxConcurrentJobs      int                // Global concurrent job limit (default: 10)
	MaxConcurrentPerAgent  int                // Per-agent concurrent job limit (default: 3)
	ValidationGateInterval int                // Run validation gate every N steps (default: 3, 0 to disable)
	MaxHandoffSteps        int                // Max handoff steps per task (0 = unlimited, default: 5)
	HandoffUseAmendment    bool               // Route handoffs through amendment system (default: true)
	AmendmentManager       AmendmentSubmitter // Optional: enables amendment-based step creation

	// QuotaDeferral, when non-nil, overrides the default quota-deferral
	// policy (10 deferrals, 6h total). A policy with MaxDeferrals<=0 or
	// MaxTotalDeferral<=0 disables deferral entirely (legacy behavior).
	QuotaDeferral *QuotaDeferralPolicy

	// ContextWindowProvider resolves the executor model's context window
	// (tokens) for an agent ID; 0 = unknown. Wired by the daemon from the
	// LLM resolver. When nil (or the window is unknown), scheduling behaves
	// byte-identically to the pre-allotment legacy path (allotment tree
	// leaf 02).
	ContextWindowProvider func(agentID string) int

	// AllotmentCfg controls the allotment math. Zero value is defaulted to
	// DefaultAllotmentConfig() in NewTacticalScheduler.
	AllotmentCfg AllotmentConfig
}

// NewTacticalScheduler creates a new tactical scheduler.
func NewTacticalScheduler(cfg TacticalSchedulerConfig) *TacticalScheduler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	// Set defaults for concurrency limits
	maxConcurrentJobs := cfg.MaxConcurrentJobs
	if maxConcurrentJobs <= 0 {
		maxConcurrentJobs = 10
	}
	maxConcurrentPerAgent := cfg.MaxConcurrentPerAgent
	if maxConcurrentPerAgent <= 0 {
		maxConcurrentPerAgent = 3
	}
	// Set default validation gate interval (every 3 steps)
	validationGateInterval := cfg.ValidationGateInterval
	if validationGateInterval <= 0 {
		validationGateInterval = 3
	}
	// Quota-deferral policy: explicit override, else the defaults
	// (10 deferrals / 6h). MaxDeferrals<=0 or MaxTotalDeferral<=0 disables
	// deferral entirely.
	quotaDeferralPolicy := DefaultQuotaDeferralPolicy()
	if cfg.QuotaDeferral != nil {
		quotaDeferralPolicy = *cfg.QuotaDeferral
	}

	// Allotment config (allotment tree leaf 02): zero value means the pinned
	// defaults. A non-zero UsableRatio is treated as an explicit override.
	allotmentCfg := cfg.AllotmentCfg
	if allotmentCfg.UsableRatio == 0 {
		allotmentCfg = DefaultAllotmentConfig()
	}

	// Initialize semaphores
	globalSemaphore := make(chan struct{}, maxConcurrentJobs)
	agentSemaphore := make(map[string]chan struct{})

	// Pre-initialize semaphores for known agents
	knownAgents := []string{config.AgentIDCoder, config.AgentIDDebugger, config.AgentIDPlanner, config.AgentIDAnalyst, config.AgentIDCommitter, config.AgentIDScheduler, config.AgentIDChat}
	for _, agentID := range knownAgents {
		agentSemaphore[agentID] = make(chan struct{}, maxConcurrentPerAgent)
	}

	return &TacticalScheduler{
		stepStore:              cfg.StepStore,
		taskStore:              cfg.TaskStore,
		queue:                  cfg.Queue,
		registry:               cfg.Registry,
		bus:                    cfg.Bus,
		pairManager:            cfg.PairManager,
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
		quotaDeferrals:         make(map[string]int),
		quotaDeferralFirst:     make(map[string]time.Time),
		quotaDeferralPolicy:    quotaDeferralPolicy,
		maxHandoffSteps:        cfg.MaxHandoffSteps,
		handoffUseAmendment:    cfg.HandoffUseAmendment,
		amendmentMgr:           cfg.AmendmentManager,
		contextWindowProvider:  cfg.ContextWindowProvider,
		allotmentCfg:           allotmentCfg,
	}
}

// GetJobByID retrieves a job from the queue by ID.
func (ts *TacticalScheduler) GetJobByID(ctx context.Context, jobID string) (*queue.Job, error) {
	return ts.queue.Get(ctx, jobID)
}

// ScheduleReadySteps finds ready steps for a task and enqueues them as jobs.
// Steps that cannot be scheduled due to semaphore limits remain in "ready" state
// and will be retried on the next scheduling cycle.
func (ts *TacticalScheduler) ScheduleReadySteps(ctx context.Context, taskID string) error {
	readySteps, err := ts.stepStore.GetReadySteps(taskID)
	if err != nil {
		return fmt.Errorf("failed to get ready steps: %w", err)
	}

	if len(readySteps) == 0 {
		ts.logger.Debug("No ready steps to schedule", "task_id", taskID)
		return nil
	}

	ts.logger.Info("Scheduling ready steps",
		"task_id", taskID,
		"count", len(readySteps),
	)

	// Allotment batching (allotment tree leaf 02): when the executor
	// model's context window is known, split the phase's ready wave into
	// allotment-sized batches. Batches beyond the first are rewritten in
	// place as continuation steps ([continuation k/N] prefix, DependsOn
	// chained to the previous batch's last step). scheduleStep's
	// dependency gate (deps must be terminal) then holds each continuation
	// batch until the previous batch drains. With a nil provider or an
	// unknown window the allotment is 0 and this whole block is skipped:
	// legacy behavior is byte-identical.
	if agentID := allotmentAgentID(readySteps); agentID != "" {
		allot := AllotmentTokens(ts.contextWindowFor(agentID), ts.allotmentCfg)
		if allot > 0 {
			batches := SplitStepsByAllotment(readySteps, allot, ts.allotmentCfg)
			if len(batches) > 1 {
				flattenWithContinuations(batches, ts.allotmentCfg)
				if err := ts.persistContinuations(taskID, readySteps); err != nil {
					ts.logger.Error("Failed to persist continuation steps",
						"task_id", taskID,
						"error", err,
					)
					return err
				}
			}
		}
	}

	scheduledCount := 0
	semaphoreBlockedCount := 0

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
			// Check if this was a semaphore block (expected, not an error)
			if errors.Is(err, ErrNoExecutionSlot) {
				semaphoreBlockedCount++
				ts.logger.Debug("Step blocked due to execution limit",
					"step_id", step.ID,
					"task_id", taskID,
					"agent_id", step.AgentID,
				)
				continue
			}
			ts.logger.Error("Failed to schedule step",
				"step_id", step.ID,
				"task_id", taskID,
				"error", err,
			)
			continue
		}
		scheduledCount++
	}

	ts.logger.Debug("Scheduling complete",
		"task_id", taskID,
		"scheduled", scheduledCount,
		"blocked_by_semaphore", semaphoreBlockedCount,
		"total_ready", len(readySteps),
	)

	// Publish progress event with current step info (chat_visible=true so UI displays in chat)
	currentStepDesc := ""
	if scheduledCount > 0 {
		for _, step := range readySteps {
			if step.State == task.StepScheduled {
				currentStepDesc = step.Description
				break
			}
		}
	}
	ts.publishEvent("task.progress", map[string]any{
		KeyTaskID:         taskID,
		"scheduled_steps": scheduledCount,
		"current_step":    currentStepDesc,
		KeyChatVisible:    true,
		KeyTokenUsage:     0, // No token data available at scheduling time
	})

	return nil
}

// allotmentAgentID picks the agent whose context window sizes the batch
// split: the first step's explicit AgentID, else the hint-table selection
// from the first step (all steps in a ready wave share a phase's executor
// in practice). Empty when no signal is available (then no window is
// resolvable and batching is skipped).
func allotmentAgentID(steps []*task.TaskStep) string {
	for _, step := range steps {
		if step.AgentID != "" {
			return step.AgentID
		}
	}
	if len(steps) > 0 && steps[0].ToolHint != "" {
		if ts := steps[0]; ts != nil {
			// No scheduler receiver here by design: this helper must stay
			// side-effect free, so it only reports whether a hint exists.
			return hintAgentFallback(ts.ToolHint)
		}
	}
	return ""
}

// hintAgentFallback maps a tool hint to its default agent without touching
// a scheduler instance (selectAgent needs a receiver and may log).
func hintAgentFallback(toolHint string) string {
	switch toolHint {
	case "code", "git", "debug":
		return config.AgentIDCoder
	case "analysis", "research":
		return config.AgentIDAnalyst
	case "documentation", "planning":
		return config.AgentIDPlanner
	case "review":
		return config.AgentIDCoder
	default:
		return config.AgentIDChat
	}
}

// flattenWithContinuations rewrites a batched ready wave in place: batch 0
// keeps its original descriptions; batch k (1-indexed) gets its
// descriptions wrapped with ContinuationDescription(desc, k, N) where
// N = len(batches) (the full batch count; a wave of 3 batches yields
// [continuation 1/3]..[continuation 3/3] markers), and each step
// DependsOn the previous batch's last step ID. The input slice order is
// preserved so the caller's scheduling loop walks the same steps;
// Sequence numbers stay untouched (the wave was already contiguously
// numbered, and rewritings do not reorder it).
func flattenWithContinuations(batches [][]*task.TaskStep, cfg AllotmentConfig) {
	n := len(batches)
	if n < 2 {
		return // nothing beyond batch 0: no continuations to add
	}
	for k := 1; k < len(batches); k++ {
		prev := batches[k-1]
		if len(prev) == 0 {
			continue
		}
		prevLast := prev[len(prev)-1]
		for _, step := range batches[k] {
			step.Description = ContinuationDescription(step.Description, k, n)
			// Replace any prior dependency set with the chain edge: the
			// step's own original deps were already satisfied (it was
			// ready), so its only outstanding gate is the previous batch.
			step.DependsOn = []string{prevLast.ID}
		}
	}
}

// persistContinuations rewrites the ready wave's continuation markers
// (description + DependsOn edits made by flattenWithContinuations) back to
// the step store so the scheduled jobs and later scheduling cycles read the
// same chained shape.
func (ts *TacticalScheduler) persistContinuations(taskID string, steps []*task.TaskStep) error {
	store := ts.stepStore
	if store == nil {
		return fmt.Errorf("step store unavailable for continuation persistence (task %s)", taskID)
	}
	for _, step := range steps {
		if err := store.Update(step); err != nil {
			return fmt.Errorf("failed to persist continuation step %s: %w", step.ID, err)
		}
	}
	return nil
}

// scheduleStep creates a queue job for a single step.
// It validates that all dependencies are satisfied before scheduling.
// Returns errSemaphoreUnavailable if no semaphore slot is available.
func (ts *TacticalScheduler) scheduleStep(ctx context.Context, step *task.TaskStep) error {
	// Validate dependencies before scheduling (defense in depth)
	if len(step.DependsOn) > 0 {
		allSteps, err := ts.stepStore.ListByTaskID(step.TaskID)
		if err != nil {
			return fmt.Errorf("failed to list steps for dependency check: %w", err)
		}

		stateMap := make(map[string]task.StepState)
		for _, s := range allSteps {
			stateMap[s.ID] = s.State
		}

		for _, depID := range step.DependsOn {
			depState, ok := stateMap[depID]
			if !ok {
				ts.logger.Warn("Step dependency not found, skipping schedule",
					"step_id", step.ID,
					"missing_dep", depID,
				)
				return fmt.Errorf("dependency %s not found", depID)
			}
			if !depState.IsTerminal() {
				ts.logger.Warn("Step dependency not terminal, skipping schedule",
					"step_id", step.ID,
					"dep_id", depID,
					"dep_state", depState,
				)
				return fmt.Errorf("dependency %s not terminal (state: %s)", depID, depState)
			}
			if depState == task.StepFailed {
				ts.logger.Warn("Step dependency failed, skipping schedule",
					"step_id", step.ID,
					"dep_id", depID,
				)
				return fmt.Errorf("dependency %s failed", depID)
			}
		}
	}

	// Select agent based on tool hint. An explicitly assigned step.AgentID
	// (pair-session actor/reviewer steps are stamped by the strategist)
	// wins — the hint table is a fallback, not an override. (2026-09-08
	// audit LOW: scheduleStep clobbered pair-session reviewer assignments
	// because "review" has no hint-table entry and fell through to chat.)
	ts.assignStepAgent(step)
	agentID := step.AgentID

	// Acquire semaphore slots (non-blocking)
	if !ts.acquireSlots(agentID) {
		return fmt.Errorf("%w: for agent %s", ErrNoExecutionSlot, agentID)
	}

	// Recover session provenance from the persisted step (audit R4): the
	// only reliable origin source is Task.LinkedSessions, resolved at
	// schedule time and persisted on the step so validation-retry jobs can
	// rebuild it. Session-less tasks leave it empty → stamp false (R4 (c)).
	if step.SessionID == "" {
		ts.resolveStepSession(step)
	}

	payload := stepJobPayloadFromStep(step)

	job, err := queue.NewJob(queue.JobTypeProjectTask, payload)
	if err != nil {
		ts.releaseSlots(agentID)
		return fmt.Errorf("failed to create job: %w", err)
	}

	// Interactive stamp (tree 04 leaf 02, D11): evaluated ONCE at enqueue
	// from the originating session's live signal. Jobs enqueued while the
	// session is quiet never upgrade — accepted R4 semantics.
	ts.stampInteractive(job, step.SessionID)

	job.WithTaskID(step.TaskID).
		WithAgentID(agentID)

	// Enqueue the job
	if err := ts.queue.Enqueue(ctx, job); err != nil {
		ts.releaseSlots(agentID)
		return fmt.Errorf("failed to enqueue job: %w", err)
	}

	// Update step state, agent, and job reference
	if err := ts.stepStore.SetAgentID(step.ID, agentID); err != nil {
		ts.logger.Error("Failed to set step agent_id", "step_id", step.ID, "error", err)
	}
	if err := ts.stepStore.SetJobID(step.ID, job.ID); err != nil {
		ts.logger.Error("Failed to set step job_id", "step_id", step.ID, "error", err)
	}
	if err := ts.stepStore.SetState(step.ID, task.StepScheduled); err != nil {
		ts.logger.Error("Failed to set step state to scheduled", "step_id", step.ID, "error", err)
	}

	ts.logger.Info("ASSIGN step scheduled",
		"step_id", step.ID,
		"job_id", job.ID,
		"agent_id", agentID,
		"task_id", step.TaskID,
		"tool_hint", step.ToolHint,
		"description", truncateString(step.Description, 80),
	)

	return nil
}

// acquireSlots attempts to acquire both global and per-agent semaphore slots.
// Returns true if both acquired, false otherwise (no blocking).
func (ts *TacticalScheduler) acquireSlots(agentID string) bool {
	// Get or create per-agent semaphore under lock, then release the lock
	// before performing channel operations. The channels themselves are
	// concurrency-safe; holding the mutex across channel sends both violates
	// the "no I/O under mutex" rule and serializes all acquire attempts
	// behind a single holder even when slots are independently available.
	ts.semaphoreMu.Lock()
	agentSem, ok := ts.agentSemaphore[agentID]
	if !ok {
		// Create semaphore for unknown agents
		maxPerAgent := cap(ts.globalSemaphore) / 3 // Rough heuristic
		if maxPerAgent < 1 {
			maxPerAgent = 3
		}
		agentSem = make(chan struct{}, maxPerAgent)
		ts.agentSemaphore[agentID] = agentSem
	}
	ts.semaphoreMu.Unlock()

	// Try to acquire global slot (non-blocking)
	select {
	case ts.globalSemaphore <- struct{}{}:
		// Got global slot
	default:
		return false // Global semaphore full
	}

	// Try to acquire per-agent slot (non-blocking)
	select {
	case agentSem <- struct{}{}:
		// Got agent slot
	default:
		<-ts.globalSemaphore // Release global slot
		return false         // Agent semaphore full
	}

	return true
}

// releaseSlots releases both global and per-agent semaphore slots.
func (ts *TacticalScheduler) releaseSlots(agentID string) {
	ts.semaphoreMu.Lock()
	agentSem := ts.agentSemaphore[agentID]
	ts.semaphoreMu.Unlock()

	// Release per-agent slot
	if agentSem != nil {
		select {
		case <-agentSem:
		default:
		}
	}

	// Release global slot
	select {
	case <-ts.globalSemaphore:
	default:
	}
}

// clearQuotaDeferrals drops the step's quota-deferral counters. Called on
// any terminal step outcome (completion or final failure) so the map does
// not grow unbounded.
func (ts *TacticalScheduler) clearQuotaDeferrals(stepID string) {
	ts.quotaDeferralMu.Lock()
	defer ts.quotaDeferralMu.Unlock()
	delete(ts.quotaDeferrals, stepID)
	delete(ts.quotaDeferralFirst, stepID)
}

// quotaResetAtFromMessage extracts the earliest quota reset time from a
// serialized quota error message. The bus delivers OnJobFailed an error
// STRING; structured QuotaResetError.ResetAt never survives that hop, but
// QuotaResetError.Error() embeds "resets_at=<RFC3339>" and the
// daemon's quota-wait stamp embeds "until <RFC3339>". Returns the zero time
// when no future reset time can be recovered.
//
// M1: the pattern is a three-alternative regex (resets_at= / rate-limited
// until / JSON "resets_at"), so each match's timestamp lives in a different
// capture group. Iterate m[1:] and take the first non-empty group — testing
// only m[1] left the daemon stamp and JSON shapes dead (5h parks degraded to
// +5min UnknownResetDelay polls and exhausted at ~50min).
//
// M1 follow-up: the capture classes ([^\s\",}]+) do not exclude '.', so the
// daemon stamp's trailing sentence period is swallowed
// ("...until 2026-09-09T09:23:31Z."), and RFC3339 parse fails on the extra
// text. Trim trailing sentence punctuation before parsing (a fractional
// second's '.' is interior, never trailing, so this is safe).
func quotaResetAtFromMessage(errMsg string) time.Time {
	var earliest time.Time
	for _, m := range quotaResetPatterns.FindAllStringSubmatch(errMsg, -1) {
		var raw string
		for _, g := range m[1:] {
			if g != "" {
				raw = g
				break
			}
		}
		if raw == "" {
			continue
		}
		raw = strings.TrimRight(strings.TrimSpace(raw), ".,;:!?)\"'")
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
		if err != nil || !t.After(time.Now()) {
			continue // unparseable or already past — no wait worth scheduling
		}
		if earliest.IsZero() || t.Before(earliest) {
			earliest = t
		}
	}
	return earliest
}

// quotaDeferralScheduled decides whether the step may be deferred for this
// quota failure and, if so, when to requeue it. Mirrors the worker's
// provider-wait give-up logic (internal/worker requeueOnProviderWait): an
// unknown reset waits UnknownResetDelay; a reset beyond the policy's total
// span from the FIRST deferral, or an exhausted deferral count, gives up.
// Returns (resumeAt, true) to defer or (zero, false) to keep the legacy
// failure path.
func (ts *TacticalScheduler) quotaDeferralScheduled(stepID string, errMsg string, now time.Time) (time.Time, bool) {
	policy := ts.quotaDeferralPolicy
	if policy.MaxDeferrals <= 0 || policy.MaxTotalDeferral <= 0 {
		return time.Time{}, false // deferral disabled
	}

	ts.quotaDeferralMu.Lock()
	count := ts.quotaDeferrals[stepID]
	first, hasFirst := ts.quotaDeferralFirst[stepID]
	ts.quotaDeferralMu.Unlock()

	if count >= policy.MaxDeferrals {
		return time.Time{}, false
	}
	deadline := now.Add(policy.MaxTotalDeferral)
	if hasFirst && now.Sub(first) >= policy.MaxTotalDeferral {
		return time.Time{}, false
	}
	if hasFirst {
		deadline = first.Add(policy.MaxTotalDeferral)
	}

	resumeAt := quotaResetAtFromMessage(errMsg)
	if resumeAt.IsZero() {
		if policy.UnknownResetDelay <= 0 {
			return time.Time{}, false
		}
		resumeAt = now.Add(policy.UnknownResetDelay)
	}
	if resumeAt.After(deadline) {
		return time.Time{}, false
	}
	return resumeAt, true
}

// recordQuotaDeferral persists the deferral counters for a step AFTER the
// requeue has been accepted.
func (ts *TacticalScheduler) recordQuotaDeferral(stepID string, now time.Time) int {
	ts.quotaDeferralMu.Lock()
	defer ts.quotaDeferralMu.Unlock()
	ts.quotaDeferrals[stepID]++
	if _, ok := ts.quotaDeferralFirst[stepID]; !ok {
		ts.quotaDeferralFirst[stepID] = now
	}
	return ts.quotaDeferrals[stepID]
}

// OnJobCompleted handles a completed job by updating the step, promoting
// newly unblocked steps, and checking task completion.
func (ts *TacticalScheduler) OnJobCompleted(ctx context.Context, jobID string, result json.RawMessage) error {
	startTime := time.Now()

	// Find step by job ID
	step, err := ts.getStepByJobID(jobID)
	if err != nil {
		return fmt.Errorf("failed to find step for job %s: %w", jobID, err)
	}
	if step == nil {
		ts.logger.Debug("No step found for completed job", "job_id", jobID)
		return nil // Not a step-backed job, ignore
	}

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

	// Release semaphore slots for this completed job
	defer ts.releaseSlots(step.AgentID)

	// Terminal completion: drop any quota-deferral counters for this step.
	ts.clearQuotaDeferrals(step.ID)

	// Store the result and extract evidence
	resultStr := ""
	if result != nil {
		resultStr = string(result)
	}
	if err := ts.stepStore.SetResult(step.ID, resultStr); err != nil {
		ts.logger.Error("Failed to set step result", "step_id", step.ID, "error", err)
	} else {
		step.Result = resultStr
	}

	// NEW: Extract evidence from result before validation
	var execResult struct {
		Success  bool              `json:"success"`
		Result   any               `json:"result,omitempty"`
		Error    string            `json:"error,omitempty"`
		Evidence []models.Evidence `json:"evidence,omitempty"`
	}
	if err := json.Unmarshal(result, &execResult); err != nil {
		ts.logger.Debug("Failed to parse execution result", "step_id", step.ID, "error", err)
	}

	// Update step with evidence before validation
	if len(execResult.Evidence) > 0 {
		step.Evidence = execResult.Evidence
		// Persist evidence to step store
		if err := ts.stepStore.Update(step); err != nil {
			ts.logger.Error("Failed to persist step evidence", "step_id", step.ID, "error", err)
		}
		ts.logger.Debug("Extracted evidence from execution result",
			"step_id", step.ID,
			"evidence_count", len(execResult.Evidence),
		)
	}

	// NEW: Validation gate - validate evidence before proceeding
	if ts.validatorManager != nil {
		validationErr := ts.validatorManager.ValidateStep(ctx, step)
		if validationErr != nil {
			ts.logger.Error("Validation failed", "step_id", step.ID, "error", validationErr)
			step.Validated = false
			step.ValidationError = validationErr.Error()

			// Determine max validation retries from policy (default 2)
			maxRetries := 2
			if ts.reviewManager != nil {
				policy := ts.reviewManager.GetValidationPolicy()
				if policy.MaxValidationLoops > 0 {
					maxRetries = policy.MaxValidationLoops - 1 // MaxValidationLoops is total attempts; retries = attempts - 1
				}
			}
			if maxRetries < 1 {
				maxRetries = 1
			}

			if step.ValidationRetryCount < maxRetries {
				// Re-queue step for validation retry. The payload is rebuilt
				// from the step (which carries the persisted SessionID) so
				// the retry job re-stamps identically to the first schedule.
				step.ValidationRetryCount++
				if err := ts.stepStore.Update(step); err != nil {
					ts.logger.Warn("failed to persist step retry count", "step_id", step.ID, "error", err)
				}

				retryPayload := stepJobPayloadFromStep(step)
				retryJob, jobErr := queue.NewJob(queue.JobTypeProjectTask, retryPayload)
				if jobErr != nil {
					ts.logger.Error("Failed to create validation-retry job", "step_id", step.ID, "error", jobErr)
					return fmt.Errorf("validation failed and retry job creation failed: %w", validationErr)
				}
				retryJob.WithTaskID(step.TaskID).WithAgentID(step.AgentID)

				if enqueueErr := ts.queue.Enqueue(ctx, retryJob); enqueueErr != nil {
					ts.logger.Error("Failed to enqueue validation-retry job", "step_id", step.ID, "error", enqueueErr)
					return fmt.Errorf("validation failed and retry enqueue failed: %w", validationErr)
				}

				// Reset step state to scheduled for retry
				if err := ts.stepStore.SetState(step.ID, task.StepScheduled); err != nil {
					ts.logger.Error("Failed to reset step state for validation retry", "step_id", step.ID, "error", err)
				}
				if err := ts.stepStore.SetJobID(step.ID, retryJob.ID); err != nil {
					ts.logger.Error("Failed to update step job_id for validation retry", "step_id", step.ID, "error", err)
				}

				ts.logger.Info("Validation retry enqueued",
					"step_id", step.ID,
					"retry_count", step.ValidationRetryCount,
					"max_retries", maxRetries,
				)
				ts.publishEvent("task.validation_retry", map[string]any{
					KeyTaskID:                step.TaskID,
					KeyStepID:                step.ID,
					"retry_count":            step.ValidationRetryCount,
					"max_retries":            maxRetries,
					string(MessageTypeError): validationErr.Error(),
				})
				return nil // Don't proceed to completion; step will be retried
			}

			// Max retries exceeded - mark step as needs_info for human review
			ts.logger.Warn("Validation max retries exceeded",
				"step_id", step.ID,
				"retry_count", step.ValidationRetryCount,
				"max_retries", maxRetries,
			)
			if err := ts.stepStore.Update(step); err != nil {
				ts.logger.Warn("failed to persist step", "step_id", step.ID, "error", err)
			}
			return validationErr // Don't proceed to completion
		}
		step.Validated = true
		step.ValidationError = ""
	}

	// Publish step completed event with details
	ts.publishEvent("task.step_completed", map[string]any{
		KeyTaskID:     step.TaskID,
		KeyStepID:     step.ID,
		"description": step.Description,
		KeyAgentID:    step.AgentID,
		"result":      truncateString(resultStr, 200),
		"state":       string(task.StepCompleted),
		"duration":    time.Since(startTime).String(),
	})

	// Check if review is needed
	if ts.reviewManager != nil && ts.reviewManager.GetPolicy().Enabled {
		// Trigger review process
		ts.logger.Debug("Triggering review for step", "step_id", step.ID)

		// Publish review request event
		ts.publishEvent("step.review_requested", map[string]any{
			KeyStepID:   step.ID,
			KeyTaskID:   step.TaskID,
			"tool_hint": step.ToolHint,
			KeyAgentID:  step.AgentID,
		})

		// Also publish under task.* prefix for backward compatibility with
		// subscribers (TUI, ChatHandler) that subscribe to task.* but not step.*.
		ts.publishEvent("task.review_requested", map[string]any{
			KeyStepID:   step.ID,
			KeyTaskID:   step.TaskID,
			"tool_hint": step.ToolHint,
			KeyAgentID:  step.AgentID,
		})

		// Load task to extract spec for spec-driven review
		var reviewSpec *TaskSpec
		if ts.taskStore != nil {
			if t, err := ts.taskStore.GetByID(step.TaskID); err == nil && t != nil {
				reviewSpec = ExtractSpecFromTask(t)
			}
		}

		// Perform review (synchronously for now)
		reviewResult, err := ts.reviewManager.ReviewStep(ctx, step, reviewSpec)
		if err != nil {
			ts.logger.Error("Review failed", "step_id", step.ID, "error", err)
			// Continue without review - mark as completed
			if err := ts.stepStore.SetState(step.ID, task.StepCompleted); err != nil {
				ts.logger.Error("Failed to set step to completed after review failure", "error", err)
			}
		} else {
			// Handle review result
			if err := ts.handleReviewResult(ctx, step, reviewResult); err != nil {
				ts.logger.Error("Failed to handle review result", "error", err)
			}
		}
	} else {
		// No review manager or review disabled - mark completed directly
		if err := ts.stepStore.SetState(step.ID, task.StepCompleted); err != nil {
			ts.logger.Error("Failed to set step state to completed", "step_id", step.ID, "error", err)
		}
	}

	// Propagate context to next ready steps (handoff if wired, legacy fallback otherwise)
	if ts.handoffPropagator != nil {
		if err := ts.handoffPropagator(ctx, step); err != nil {
			ts.logger.Error("Handoff propagation failed",
				"step_id", step.ID, "error", err)
		}
	} else {
		if err := ts.propagateContextToNextStepsLegacy(ctx, step); err != nil {
			ts.logger.Error("Failed to propagate context to next steps",
				"step_id", step.ID,
				"error", err,
			)
		}
	}

	// NEW: Run validation gate if interval reached
	ts.runValidationGateIfDue(ctx, step.TaskID)

	// A-08 FIX: Use atomic store operations instead of Get → mutate → Update.
	// The previous RMW sequence lost increments when parallel step completions
	// both read CompletedJobs=N, both set N+1, and one overwrote the other.
	// IncrementCompletedJobs does `SET completed_jobs = completed_jobs + 1` in
	// a single SQL statement, eliminating the race.
	completedCount, err := ts.taskStore.IncrementCompletedJobs(step.TaskID)
	if err != nil {
		ts.logger.Error("Failed to increment completed jobs", "task_id", step.TaskID, "error", err)
	}

	// Aggregate token usage from step to parent task (also atomic).
	if step.TokenUsage > 0 {
		if err := ts.taskStore.AddTokenUsage(step.TaskID, step.TokenUsage); err != nil {
			ts.logger.Error("Failed to add token usage to task", "task_id", step.TaskID, "error", err)
		}
		ts.logger.Debug("Aggregated token usage from step to task",
			"step_id", step.ID,
			"step_tokens", step.TokenUsage,
		)
	}

	// Publish token progress event using the atomically incremented counter.
	if step.TokenUsage > 0 || completedCount > 0 {
		if t, err := ts.taskStore.GetByID(step.TaskID); err == nil && t != nil {
			ts.publishTokenProgress(t)
		}
	}

	// Check for newly unblocked steps (only if step was approved/completed)
	//
	// BUG FIX (2026-09-05 daemon panic): GetByID returns (nil, err) on any
	// failure — including transient SQLITE_BUSY under parallel step jobs.
	// The previous code logged step.ID *after* reassigning step, so a nil
	// step dereferenced a nil pointer and panicked the daemon. Capture the
	// ID up front and guard the nil step.
	stepID := step.ID
	step, err = ts.getStepByID(stepID) // Refresh step state
	if err != nil {
		ts.logger.Error("Failed to refresh step state", "step_id", stepID, "error", err)
		return nil
	}
	if step == nil {
		// Store guarantees non-nil on success, but never deref a nil step.
		ts.logger.Error("Refreshed step state is nil", "step_id", stepID)
		return nil
	}
	if step.State == task.StepCompleted || step.State == task.StepApproved {
		promoted, err := ts.stepStore.PromoteReadySteps(step.TaskID)
		if err != nil {
			ts.logger.Error("Failed to promote ready steps", "error", err)
		}
		if len(promoted) > 0 {
			ts.logger.Info("Promoted newly unblocked steps",
				"task_id", step.TaskID,
				"count", len(promoted),
			)
		}
		// FIX #0024: Always schedule ready steps after job completion (not just newly promoted)
		// This ensures semaphore-blocked steps get re-scheduled when slots free up
		if err := ts.ScheduleReadySteps(ctx, step.TaskID); err != nil {
			ts.logger.Error("Failed to schedule ready steps", "error", err)
		}
	}

	// Check if all steps are completed/approved. A task whose steps are all
	// in terminal states but that contains failures must still reach the
	// finalization block below — AreAllCompleted is success-only by design
	// (it returns false on any StepFailed), so a terminal-with-failures
	// check is what lets the task finalize as StateFailed.
	allDone, err := ts.stepStore.AreAllCompleted(step.TaskID)
	if err != nil {
		ts.logger.Error("Failed to check task completion", "error", err)
		return nil
	}
	if !allDone {
		if terminal, terr := ts.allStepsTerminalWithFailures(step.TaskID); terr != nil {
			ts.logger.Error("Failed to check terminal state with failures", "task_id", step.TaskID, "error", terr)
		} else if terminal {
			allDone = true
		}
	}

	// Load task for state transition / progress reporting.
	t, err := ts.taskStore.GetByID(step.TaskID)
	if err != nil || t == nil {
		ts.logger.Error("Failed to get task for completion check", "task_id", step.TaskID, "error", err)
		return nil
	}

	if allDone {
		// NEW: Task-level validation before marking complete
		steps, err := ts.stepStore.ListByTaskID(step.TaskID)
		if err != nil {
			ts.logger.Error("Failed to list steps for task validation", "error", err)
		} else {
			var validationErrors []string
			for _, s := range steps {
				// Validated is only set when a validatorManager ran. With no
				// validator, the flag stays false — do not block task
				// completion (meept-bench: heuristic auto-approve left
				// Validated=false and waitForTaskCompletion hung 600s).
				if ts.validatorManager != nil && s.State.IsSuccessfullyTerminal() && !s.Validated {
					validationErrors = append(validationErrors,
						fmt.Sprintf("step %s completed but not validated", s.ID))
				}
				if s.ValidationError != "" {
					validationErrors = append(validationErrors,
						fmt.Sprintf("step %s has validation error: %s", s.ID, s.ValidationError))
				}
			}
			if len(validationErrors) > 0 {
				ts.logger.Error("Task validation incomplete - blocking completion",
					"task_id", step.TaskID,
					"errors", strings.Join(validationErrors, ", "))
				return fmt.Errorf("task validation incomplete: %s", strings.Join(validationErrors, ", "))
			}
		}

		// Clean up validation gate counter for completed task
		ts.cleanupValidationGateCounter(step.TaskID)

		// Honest completion (2026-09-04 finding F2): any failed step fails
		// the task. The step error text becomes the user-visible result.
		// Counters are recounted from step rows first so completion events
		// carry true totals even after revision churn raced the counters
		// (2026-09-07: task finalized "2/1 completed, 200%").
		if _, _, _, recountErr := ts.taskStore.RecountJobs(step.TaskID); recountErr != nil {
			ts.logger.Error("Failed to recount jobs", "task_id", step.TaskID, "error", recountErr)
		}
		if rt, rerr := ts.taskStore.GetByID(step.TaskID); rerr == nil && rt != nil {
			t = rt
		}
		failedSteps, ferr := ts.failedStepsForTask(step.TaskID)
		if ferr != nil {
			ts.logger.Error("Failed to list failed steps", "task_id", step.TaskID, "error", ferr)
		}
		taskFailed := len(failedSteps) > 0
		if taskFailed {
			t.SetState(task.StateFailed)
		} else {
			t.SetState(task.StateCompleted)
		}
		if err := ts.taskStore.Update(t); err != nil {
			ts.logger.Error("Failed to set task completed", "error", err)
		}

		// Clear escalation tracking for completed task
		if ts.escalationManager != nil {
			ts.escalationManager.ClearEscalation(step.TaskID)
		}

		// Build step summaries for the completion event
		stepSummaries := ts.buildStepSummaries(step.TaskID)
		executionTime := t.ExecutionTime().Round(time.Second).String()
		resultSummary := ts.buildResultSummary(stepSummaries)

		// Honest completion payload: "status" tells subscribers whether the
		// task actually succeeded, and a failed task's "result" carries the
		// first failed step's error text instead of a success summary.
		completionStatus := "completed"
		if taskFailed {
			completionStatus = "failed"
			resultSummary = truncateString(firstLine(failedSteps[0].Result), 400)
		}

		// Extract unique agents used
		agentSet := make(map[string]struct{})
		for _, s := range stepSummaries {
			if agentID, ok := s["agent_id"].(string); ok && agentID != "" {
				agentSet[agentID] = struct{}{}
			}
		}
		agentsUsed := make([]string, 0, len(agentSet))
		for agent := range agentSet {
			agentsUsed = append(agentsUsed, agent)
		}

		ts.publishEvent("task.completed", map[string]any{
			KeyTaskID:         step.TaskID,
			"name":            t.Name,
			KeyCompletedJobs:  t.CompletedJobs,
			KeyTotalJobs:      t.TotalJobs,
			"linked_sessions": t.LinkedSessions,
			"steps":           stepSummaries,
			"execution_time":  executionTime,
			"result":          resultSummary,
			"status":          completionStatus,
			"agents_used":     agentsUsed,
			KeyTokenUsage:     t.TokenUsage,
		})

		ts.logger.Info("Task finalized",
			"task_id", step.TaskID,
			"status", completionStatus,
			"steps_completed", t.CompletedJobs,
			"steps_total", t.TotalJobs,
			"agents_used", agentsUsed,
			"duration", executionTime,
		)
	} else {
		// Get next step description for progress update
		nextStepDesc := ""
		readySteps, _ := ts.stepStore.GetReadySteps(step.TaskID)
		if len(readySteps) > 0 {
			nextStepDesc = readySteps[0].Description
		}

		// Publish progress update (chat_visible=true so UI shows in chat)
		ts.publishEvent("task.progress", map[string]any{
			KeyTaskID:        step.TaskID,
			KeyCompletedJobs: t.CompletedJobs,
			KeyTotalJobs:     t.TotalJobs,
			"current_step":   nextStepDesc,
			KeyChatVisible:   true,
		})
	}

	return nil
}

// handleReviewResult processes a review result and updates step state accordingly.
// It delegates the state-machine logic to ReviewManager.HandleReviewResult
// (the canonical implementation) and then adds tactical-specific side
// effects: promoting and scheduling revision steps through the proper
// dependency-checking flow, and forcing a NeedsInfo step into the completed
// state so the task can proceed while humans optionally intervene.
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
	if err != nil {
		return err
	}

	// NeedsInfo: tactical forces completion so the overall task can proceed;
	// humans can intervene out-of-band if needed.
	if result.Status == ReviewNeedsInfo {
		if err := ts.stepStore.SetState(step.ID, task.StepCompleted); err != nil {
			ts.logger.Error("Failed to set step to completed", "error", err)
		}
	}

	// If revisions were created, use proper promotion flow to respect dependencies.
	// Revision steps depend on the rejected step (now terminal) and possibly other
	// dependencies from the original step that may not yet be complete.
	if len(revisions) > 0 {
		// Promote any steps that are now ready (all dependencies terminal)
		promoted, err := ts.stepStore.PromoteReadySteps(step.TaskID)
		if err != nil {
			ts.logger.Error("Failed to promote ready steps after review",
				"task_id", step.TaskID,
				"error", err,
			)
		} else if len(promoted) > 0 {
			ts.logger.Info("Promoted steps after review",
				"task_id", step.TaskID,
				"count", len(promoted),
			)
		}

		// Schedule any newly ready steps (may include revisions)
		if err := ts.ScheduleReadySteps(ctx, step.TaskID); err != nil {
			ts.logger.Error("Failed to schedule ready steps after review",
				"task_id", step.TaskID,
				"error", err,
			)
		}
	}

	return nil
}

// OnJobFailed handles a failed job by updating the step and potentially
// marking the task as failed.
func (ts *TacticalScheduler) OnJobFailed(ctx context.Context, jobID, jobErr string) error {
	step, err := ts.stepStore.GetByJobID(jobID)
	if err != nil {
		return fmt.Errorf("failed to find step for job %s: %w", jobID, err)
	}
	if step == nil {
		return nil // Not a step-backed job
	}

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

	// Release semaphore slots for this failed job
	defer ts.releaseSlots(step.AgentID)

	// Quota-aware job deferral (quota-reset-resilience): a quota-classified
	// failure is a provider wait, NOT a step failure. If the queue supports
	// requeueing (queue.Requeueable — PersistentQueue does), reset the job
	// to pending with a not-before gate at the quota reset time without
	// consuming a retry, bounded by the deferral policy (max 10 deferrals /
	// 6h from the first). The step returns to "scheduled" and its error
	// result is cleared, so the task neither fails nor advances past work
	// that never ran. On re-claim after the gate elapses the job re-runs
	// normally. Bounds exhausted → fall through to the legacy failure path,
	// which surfaces a "quota-deferred exhausted" step result. Non-quota
	// errors never enter this branch.
	if isQuotaClassFailure(jobErr) {
		if rq, ok := ts.queue.(queue.Requeueable); ok {
			resumeAt, deferOK := ts.quotaDeferralScheduled(step.ID, jobErr, time.Now())
			if deferOK {
				if requeueErr := rq.Requeue(ctx, jobID, resumeAt); requeueErr != nil {
					ts.logger.Error("Failed to requeue job on quota wait",
						"job_id", jobID,
						"step_id", step.ID,
						"error", requeueErr,
					)
					// fall through to legacy failure path
				} else {
					deferralCount := ts.recordQuotaDeferral(step.ID, time.Now())
					// Reset step state to scheduled for the deferred re-run
					if err := ts.stepStore.SetState(step.ID, task.StepScheduled); err != nil {
						ts.logger.Error("Failed to reset step state for quota deferral", "step_id", step.ID, "error", err)
					}
					// Clear the error result — the step has not failed, it waits
					if err := ts.stepStore.SetResult(step.ID, ""); err != nil {
						ts.logger.Error("Failed to clear step result for quota deferral", "step_id", step.ID, "error", err)
					} else {
						step.Result = ""
					}
					ts.logger.Info("Quota-class failure: job deferred to quota reset",
						"job_id", jobID,
						"step_id", step.ID,
						"task_id", step.TaskID,
						"deferred_quota", true,
						"deferral_count", deferralCount,
						"resume_at", resumeAt.Format(time.RFC3339),
					)
					ts.publishEvent("queue.job.deferred_quota", map[string]any{
						"job_id":    jobID,
						"task_id":   step.TaskID,
						"step_id":   step.ID,
						"resume_at": resumeAt.Format(time.RFC3339),
						"deferrals": deferralCount,
					})
					return nil // deferred, not failed
				}
			}
			// Bounds exhausted (or the schedule exceeded them): stamp the
			// error so the surfaced step result says WHY, then fall through.
			if !deferOK {
				jobErr = "quota-deferred exhausted (" + strconv.Itoa(ts.quotaDeferralPolicy.MaxDeferrals) +
					" deferrals / " + ts.quotaDeferralPolicy.MaxTotalDeferral.String() + "): " + jobErr
			}
		}
	}

	// Publish error to chat immediately (not silent)
	ts.publishEvent("task.error", map[string]any{
		KeyTaskID:                step.TaskID,
		KeyStepID:                step.ID,
		string(MessageTypeError): jobErr,
		KeyChatVisible:           true, // Errors always visible
	})

	// Check if this is a retryable error (rate limit or transient failure)
	if ts.isRetryableError(jobErr) {
		// Get the job from queue
		job, err := ts.queue.Get(ctx, jobID)
		switch {
		case err != nil:
			ts.logger.Error("Failed to get job for retry", "job_id", jobID, "error", err)
		case job != nil && job.CanRetry():
			// Determine retry reason
			reason := "transient_error"
			if ts.isRateLimitError(jobErr) {
				reason = "rate_limit"
			}

			// Retry with exponential backoff
			ts.logger.Info("Retryable error detected, retrying job with backoff",
				"job_id", jobID,
				"step_id", step.ID,
				"retry_count", job.RetryCount+1,
				"reason", reason,
			)
			if err := ts.queue.Retry(ctx, jobID); err != nil {
				ts.logger.Error("Failed to retry job", "job_id", jobID, "error", err)
			} else {
				// Reset step state to scheduled for retry
				if err := ts.stepStore.SetState(step.ID, task.StepScheduled); err != nil {
					ts.logger.Error("Failed to reset step state for retry", "step_id", step.ID, "error", err)
				}
				// Clear error result since we're retrying
				if err := ts.stepStore.SetResult(step.ID, ""); err != nil {
					ts.logger.Error("Failed to clear step result for retry", "step_id", step.ID, "error", err)
				} else {
					step.Result = ""
				}
				// Publish retry event
				ts.publishEvent("queue.job.retry", map[string]any{
					"job_id": jobID,
					"reason": reason,
				})
				// The retry consumed the failure: a later completion (or the
				// eventual terminal outcome) re-arms the quota-deferral window.
				ts.clearQuotaDeferrals(step.ID)
				return nil // Job has been requeued, don't mark as failed
			}
		default:
			ts.logger.Warn("Job cannot be retried",
				"job_id", jobID,
				"step_id", step.ID,
				"can_retry", job != nil && job.CanRetry(),
				"error", jobErr,
			)
		}
	}

	// Mark step failed
	if err := ts.stepStore.SetResult(step.ID, jobErr); err != nil {
		ts.logger.Error("Failed to set step error result", "step_id", step.ID, "error", err)
	} else {
		step.Result = jobErr
	}
	if err := ts.stepStore.SetState(step.ID, task.StepFailed); err != nil {
		ts.logger.Error("Failed to set step state to failed", "step_id", step.ID, "error", err)
	}

	// Update parent task's failed jobs counter (atomic, A-08 pattern — the
	// previous Get→FailJob→Update RMW wrote back stale counters under
	// parallel completions).
	if err := ts.taskStore.IncrementFailedJobs(step.TaskID); err != nil {
		ts.logger.Error("Failed to update task after job failure", "task_id", step.TaskID, "error", err)
	}

	// Trigger escalation for failed step if escalation manager is configured.
	// The escalation manager may re-plan the task or request human intervention.
	if ts.escalationManager != nil {
		failureCtx := FailureContext{
			TaskID:    step.TaskID,
			StepID:    step.ID,
			AgentID:   step.AgentID,
			Error:     jobErr,
			Stage:     "execution",
			Timestamp: time.Now(),
		}
		if escalErr := ts.escalationManager.Escalate(ctx, failureCtx); escalErr != nil {
			ts.logger.Warn("Escalation failed",
				"task_id", step.TaskID,
				"step_id", step.ID,
				"error", escalErr,
			)
		}
	}

	// Check if all paths are blocked (no more pending/ready steps that don't
	// transitively depend on the failed step)
	allSteps, err := ts.stepStore.ListByTaskID(step.TaskID)
	if err != nil {
		ts.logger.Error("Failed to list steps for failure check", "error", err)
		return nil
	}

	hasLiveSteps := false
	for _, s := range allSteps {
		if s.State == task.StepRunning || s.State == task.StepScheduled ||
			s.State == task.StepReady {
			hasLiveSteps = true
			break
		}
	}

	// Check if any pending steps can still be promoted
	if !hasLiveSteps {
		promoted, _ := ts.stepStore.PromoteReadySteps(step.TaskID)
		hasLiveSteps = len(promoted) > 0
	}

	if !hasLiveSteps {
		// No more work can be done, mark task as failed. Counters are
		// recounted from the step rows first so the payload reflects
		// reality (revision churn previously left stale snapshots here).
		_, _, _, recountErr := ts.taskStore.RecountJobs(step.TaskID)
		if recountErr != nil {
			ts.logger.Error("Failed to recount jobs", "task_id", step.TaskID, "error", recountErr)
		}
		t, terr := ts.taskStore.GetByID(step.TaskID)
		if terr != nil || t == nil {
			ts.logger.Error("Failed to reload task for finalization", "task_id", step.TaskID, "error", terr)
			return nil
		}
		t.SetState(task.StateFailed)
		if err := ts.taskStore.Update(t); err != nil {
			ts.logger.Error("Failed to set task failed", "error", err)
		}

		// Clean up validation gate counter for failed task
		ts.cleanupValidationGateCounter(step.TaskID)

		ts.publishEvent("task.failed", map[string]any{
			KeyTaskID:                step.TaskID,
			"name":                   t.Name,
			"failed_jobs":            t.FailedJobs,
			KeyCompletedJobs:         t.CompletedJobs,
			KeyTotalJobs:             t.TotalJobs,
			"failed_step":            step.ID,
			string(MessageTypeError): jobErr,
			"linked_sessions":        t.LinkedSessions,
		})

		ts.logger.Info("Task failed - no remaining live steps",
			"task_id", step.TaskID,
			"failed", t.FailedJobs,
			"completed", t.CompletedJobs,
			"total", t.TotalJobs,
		)
	} else {
		// Task is still partially alive - get next step for progress
		nextStepDesc := ""
		readySteps, _ := ts.stepStore.GetReadySteps(step.TaskID)
		if len(readySteps) > 0 {
			nextStepDesc = readySteps[0].Description
		}

		// Reload counters atomically incremented above so progress events
		// don't carry stale snapshots.
		progressTask, perr := ts.taskStore.GetByID(step.TaskID)
		if perr != nil || progressTask == nil {
			ts.logger.Error("Failed to reload task for progress", "task_id", step.TaskID, "error", perr)
			return nil
		}
		ts.publishEvent("task.progress", map[string]any{
			KeyTaskID:        step.TaskID,
			"failed_jobs":    progressTask.FailedJobs,
			KeyCompletedJobs: progressTask.CompletedJobs,
			KeyTotalJobs:     progressTask.TotalJobs,
			"current_step":   nextStepDesc,
			KeyChatVisible:   true,
		})
	}

	return nil
}

// selectAgent maps a step's ToolHint to the executor agent via the shared
// config routing table (internal/config/tool_hints.go). The table covers
// every dialect-legal hint, roster intent hints, and historical aliases;
// unresolved hints fall back to chat as before. Keeping the table in
// config (not this switch) is what stops the compiler's validated hint set
// and this router from drifting apart again (2026-09-07: bash/writer/
// explore/researcher hints all fell through to the chat persona).
func (ts *TacticalScheduler) selectAgent(step *task.TaskStep) string {
	if agentID, ok := config.ToolHintAgent(step.ToolHint); ok {
		return agentID
	}
	return config.AgentIDChat
}

// assignStepAgent applies the schedule-time agent-assignment policy:
// an explicitly assigned step.AgentID (pair-session actor/reviewer steps
// are stamped by the strategist) wins; the tool-hint table is a fallback,
// not an override. (2026-09-08 audit LOW: scheduleStep clobbered
// pair-session reviewer assignments because "review" has no hint-table
// entry and fell through to chat.)
func (ts *TacticalScheduler) assignStepAgent(step *task.TaskStep) {
	if step.AgentID == "" {
		step.AgentID = ts.selectAgent(step)
	}
}

// SelectAgentForHint exports selectAgent so the tactical orchestrator (and
// other callers outside the agent package) can pick an executor agent ID for
// a tool hint without constructing a full TaskStep.
func (ts *TacticalScheduler) SelectAgentForHint(toolHint string) string {
	return ts.selectAgent(&task.TaskStep{ToolHint: toolHint})
}

func (ts *TacticalScheduler) publishEvent(topic string, data map[string]any) {
	if ts.bus == nil {
		return
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "tactical-scheduler", data)
	if err != nil {
		ts.logger.Error("Failed to create bus message", "error", err)
		return
	}

	ts.bus.Publish(topic, msg)
}

// publishTokenProgress publishes a task.progress event with token_usage data.
func (ts *TacticalScheduler) publishTokenProgress(t *task.Task) {
	ts.publishEvent("task.progress", map[string]any{
		KeyTaskID:        t.ID,
		KeyCompletedJobs: t.CompletedJobs,
		KeyTotalJobs:     t.TotalJobs,
		KeyTokenUsage:    t.TokenUsage,
	})
}

// isRateLimitError checks if an error message indicates a rate limit error.
// This is the string-based fallback used when the error has been serialized
// through the message bus (OnJobFailed receives event.Error as a string).
// For callers that have the original error value, prefer isRateLimitErrorFromErr.
// isRateLimitError checks if an error message indicates a rate limit error.
// This is the string-based fallback used when the error has been serialized
// through the message bus (OnJobFailed receives event.Error as a string).
// For callers that have the original error value, prefer isRateLimitErrorFromErr.
//
// SA1019 (deprecated) is suppressed: llm.IsRateLimitErrorMessage is explicitly
// preserved by its own deprecation notice for exactly this caller pattern —
// "errors deserialized from the message bus". Migrating to errcls.IsRateLimit
// requires threading the structured error value through the message bus, which
// is a cross-package refactor tracked in docs/20260618-checkreview.md D2.
func (ts *TacticalScheduler) isRateLimitError(errMsg string) bool {
	//lint:ignore SA1019 caller has only the serialized error string; see doc comment above
	return llm.IsRateLimitErrorMessage(errMsg)
}

// isRateLimitErrorFromErr uses structured error classification via errcls.
// Prefer this method when the original error value is available.
func (ts *TacticalScheduler) isRateLimitErrorFromErr(err error) bool {
	return errcls.IsRateLimit(err)
}

// isQuotaClassFailure classifies a serialized step-job failure as
// quota-class, the trigger for quota-aware deferral. Two quota shapes
// survive the message-bus stringification:
//
//   - *llm.QuotaResetError ("quota limit exceeded: provider=... resets_at=...")
//   - the daemon's job-level quota stamp ("quota wait: p/m is rate-limited
//     until ...; your request is saved and will need a re-ask once the limit
//     resets.") wrapping the QuotaResetError
//
// llm.ErrAllModelsQuotaBlocked never escapes a step job as an error value
// (the loop's quota branch returns the original QuotaResetError when every
// candidate is blocked, and the daemon stamps THAT), so its text is not a
// trigger here — but the "all models ... quota-blocked" resolver wrap is
// matched defensively for direct queue consumers. There is deliberately no
// bare "rate limit" match: short-cycle RateLimitErrors keep the existing
// backoff-retry path.
func isQuotaClassFailure(errMsg string) bool {
	if strings.Contains(errMsg, "quota limit exceeded") ||
		strings.Contains(errMsg, "quota wait:") ||
		strings.Contains(errMsg, "quota-blocked") {
		return true
	}
	// Structured fingerprint: any error carrying a resets_at RFC3339 stamp
	// is quota-shaped by construction (no other error class renders one).
	return quotaResetAtFromMessage(errMsg) != time.Time{} ||
		strings.Contains(errMsg, "all models in alias are quota-blocked")
}

// isRetryableError checks if an error is transient and worth retrying.
// This is the string-based fallback used when the error has been serialized
// through the message bus (OnJobFailed receives event.Error as a string).
// For callers that have the original error value, prefer isRetryableErrorFromErr.
//
// This includes rate limits, timeouts, network errors, and other temporary failures.
func (ts *TacticalScheduler) isRetryableError(errMsg string) bool {
	// Non-retryable errors should never be retried (FIX #0042)
	if strings.Contains(errMsg, "token budget exceeded") ||
		strings.Contains(errMsg, "budget exceeded") ||
		strings.Contains(errMsg, "context size") ||
		strings.Contains(errMsg, "context window") {
		return false
	}

	// Always retry rate limits
	if ts.isRateLimitError(errMsg) {
		return true
	}

	// Check for transient error patterns
	transientPatterns := []string{
		"timeout",
		"deadline", // EC-3 fix: "context deadline exceeded" was a false negative
		"temporary",
		"connection refused",
		"connection reset",
		"broken pipe",
		"network",
		"busy",
		"lock",
		"deadlock",
		"unavailable",
		"try again later",
	}

	lowerErr := strings.ToLower(errMsg)
	for _, pattern := range transientPatterns {
		if strings.Contains(lowerErr, pattern) {
			return true
		}
	}

	return false
}

// isRetryableErrorFromErr uses structured error classification via errcls.
// Prefer this method when the original error value is available. It correctly
// handles context.DeadlineExceeded (which the string method missed) and
// *llm.APIError with StatusCode 529 (overloaded).
func (ts *TacticalScheduler) isRetryableErrorFromErr(err error) bool {
	return errcls.IsRetryable(err)
}

// runValidationGateIfDue increments the validation counter for a task and runs
// the validation gate if the interval has been reached.
func (ts *TacticalScheduler) runValidationGateIfDue(ctx context.Context, taskID string) {
	ts.validationGateMu.Lock()
	defer ts.validationGateMu.Unlock()

	if ts.validationGateInterval <= 0 {
		return // Validation gate disabled
	}

	ts.validationGateCounter[taskID]++
	if ts.validationGateCounter[taskID] >= ts.validationGateInterval {
		// Run validation gate
		if err := ts.runValidationGate(ctx, taskID); err != nil {
			ts.logger.Warn("Validation gate detected issues",
				"task_id", taskID,
				"error", err,
			)
			// Don't block execution - just log warning as per design
		}
		// Reset counter after running gate
		ts.validationGateCounter[taskID] = 0
	}
}

// runValidationGate checks all completed steps for a task are validated.
// Returns error if any completed step lacks validation.
func (ts *TacticalScheduler) runValidationGate(_ context.Context, taskID string) error {
	steps, err := ts.stepStore.ListByTaskID(taskID)
	if err != nil {
		return fmt.Errorf("failed to list steps for validation gate: %w", err)
	}

	var unvalidatedSteps []string
	for _, step := range steps {
		if step.State.IsSuccessfullyTerminal() && !step.Validated {
			unvalidatedSteps = append(unvalidatedSteps, step.ID)
		}
		if step.ValidationError != "" {
			ts.logger.Warn("Step has validation error",
				"step_id", step.ID,
				"error", step.ValidationError,
			)
		}
	}

	if len(unvalidatedSteps) > 0 {
		return fmt.Errorf("validation gate: %d completed steps not validated: %v",
			len(unvalidatedSteps), unvalidatedSteps)
	}

	ts.logger.Debug("Validation gate passed",
		"task_id", taskID,
		"steps_checked", len(steps),
	)
	return nil
}

// allStepsTerminalWithFailures reports whether every step for a task is in a
// terminal state AND the task carries a failure-shaped outcome. AreAllCompleted
// is success-only (it returns false whenever any step is StepFailed), so this
// check is what lets a task with failures reach finalization — it must
// finalize as StateFailed instead of hanging in an active state forever.
//
// M8: "failure-shaped" covers TWO shapes —
//   - ≥1 StepFailed step (the original check), and
//   - ≥1 StepRejected step with no successful revision (AreAllCompleted
//     refuses such tasks too, but before this extension nothing else
//     finalized them: the task hung until RecoverStaleTasks force-marked
//     daemon_shutdown).
//
// The successful-revision lookup mirrors AreAllCompleted's rule: a revision
// (IsRevisionStep) that is successfully terminal and depends on the rejected
// step resolves it.
func (ts *TacticalScheduler) allStepsTerminalWithFailures(taskID string) (bool, error) {
	steps, err := ts.stepStore.ListByTaskID(taskID)
	if err != nil {
		return false, fmt.Errorf("failed to list steps: %w", err)
	}
	if len(steps) == 0 {
		return false, nil // Nothing to finalize here; AreAllCompleted already treats this as done.
	}
	hasFailed := false
	rejectedWithoutRevision := make(map[string]bool)
	for _, s := range steps {
		if s.State == task.StepRejected {
			rejectedWithoutRevision[s.ID] = false
		}
	}
	for _, s := range steps {
		if !s.State.IsTerminal() {
			return false, nil
		}
		if s.State == task.StepFailed {
			hasFailed = true
		}
		// A successful revision resolves the rejected step it depends on.
		if s.State.IsSuccessfullyTerminal() && task.IsRevisionStep(s.ID) && len(s.DependsOn) > 0 {
			// CreateRevision appends the original's ID as the LAST dep.
			origID := s.DependsOn[len(s.DependsOn)-1]
			if _, ok := rejectedWithoutRevision[origID]; ok {
				rejectedWithoutRevision[origID] = true
			}
		}
	}
	if hasFailed {
		return true, nil
	}
	for _, unresolved := range rejectedWithoutRevision {
		if !unresolved {
			return true, nil // all terminal, but a rejection was never revised → failure-finalizable (M8)
		}
	}
	return false, nil
}

// failedStepsForTask returns the failed steps for a task, ordered by sequence.
// An empty result means the task carried no failures.
func (ts *TacticalScheduler) failedStepsForTask(taskID string) ([]*task.TaskStep, error) {
	steps, err := ts.stepStore.ListByTaskID(taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to list steps: %w", err)
	}
	var failed []*task.TaskStep
	for _, s := range steps {
		if s.State == task.StepFailed {
			failed = append(failed, s)
		}
	}
	return failed, nil
}

// buildStepSummaries creates an array of step summaries for task completion events.
func (ts *TacticalScheduler) buildStepSummaries(taskID string) []map[string]any {
	allSteps, err := ts.stepStore.ListByTaskID(taskID)
	if err != nil {
		ts.logger.Error("Failed to list steps for summary", "error", err)
		return nil
	}

	summaries := make([]map[string]any, len(allSteps))
	for i, s := range allSteps {
		summaries[i] = map[string]any{
			"id":                  s.ID,
			"description":         s.Description,
			"state":               string(s.State),
			"result":              truncateString(s.Result, 400),
			KeyAgentID:            s.AgentID,
			"accumulated_context": truncateString(s.AccumulatedContext, 200),
		}
	}
	return summaries
}

// buildResultSummary creates a human-readable summary of what was accomplished.
func (ts *TacticalScheduler) buildResultSummary(steps []map[string]any) string {
	if len(steps) == 0 {
		return "Task completed."
	}

	var sb strings.Builder
	completedCount := 0
	for _, s := range steps {
		if s["state"] == string(task.StepCompleted) || s["state"] == string(task.StepApproved) {
			completedCount++
		}
	}

	fmt.Fprintf(&sb, "Completed %d/%d steps: ", completedCount, len(steps))

	// List the first few completed step descriptions
	shown := 0
	for _, s := range steps {
		if shown >= 3 {
			sb.WriteString("...")
			break
		}
		if s["state"] == string(task.StepCompleted) || s["state"] == string(task.StepApproved) {
			if shown > 0 {
				sb.WriteString(", ")
			}
			desc := s["description"].(string)
			sb.WriteString(truncateString(desc, 40))
			shown++
		}
	}

	return sb.String()
}

// propagateContextToNextStepsLegacy is the legacy 500-char truncation path.
// Deprecated: kept as fallback for when generateHandoff fails or handoff
// dependencies (templateReg, registry) are not wired. New code should ensure
// handoff deps are set so this is never reached in production.
func (ts *TacticalScheduler) propagateContextToNextStepsLegacy(_ context.Context, completedStep *task.TaskStep) error {
	// Get next ready steps
	readySteps, err := ts.stepStore.GetReadySteps(completedStep.TaskID)
	if err != nil {
		return fmt.Errorf("failed to get ready steps: %w", err)
	}
	if len(readySteps) == 0 {
		return nil // No steps to propagate to
	}

	// Build context content from completed step
	contextContent := fmt.Sprintf("## Step completed: %s\n\n**Result:** %s",
		completedStep.Description,
		truncateString(completedStep.Result, 500),
	)

	// Append context and copy MemoryRefs to each ready step
	for _, step := range readySteps {
		// Copy MemoryRefs from completed step
		for _, ref := range completedStep.MemoryRefs {
			step.AddMemoryRef(ref)
		}

		// Append to accumulated context
		step.AppendToContext(contextContent)

		// Persist updates
		if err := ts.stepStore.Update(step); err != nil {
			ts.logger.Error("Failed to update step context",
				"step_id", step.ID,
				"error", err,
			)
		}
	}

	ts.logger.Info("Propagated context to next steps",
		"step_id", completedStep.ID,
		"next_steps", len(readySteps),
	)

	return nil
}

// cleanupValidationGateCounter removes the validation gate counter entry for a task.
// Called when a task completes or fails to prevent unbounded map growth.
func (ts *TacticalScheduler) cleanupValidationGateCounter(taskID string) {
	ts.validationGateMu.Lock()
	defer ts.validationGateMu.Unlock()
	delete(ts.validationGateCounter, taskID)
}

// HandoffRequest represents a handoff request payload from the request_handoff tool.
type HandoffRequest struct {
	TaskID        string `json:"task_id"`
	FromStepID    string `json:"from_step_id"`
	FromAgentID   string `json:"from_agent_id"`
	ToAgentID     string `json:"to_agent_id"`
	Description   string `json:"description"`
	ToolHint      string `json:"tool_hint,omitempty"`
	Reason        string `json:"reason,omitempty"`
	PartialResult string `json:"partial_result,omitempty"`
	InjectAfter   bool   `json:"inject_after"`
}

// HandleHandoff processes a handoff request: creates a new task step for the target
// agent, optionally rewires downstream dependencies, and publishes a bus event.
func (ts *TacticalScheduler) HandleHandoff(ctx context.Context, msg *models.BusMessage) error {
	// 1. Parse payload
	var req HandoffRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		return fmt.Errorf("failed to parse handoff request: %w", err)
	}

	// 2. Validate task exists
	t, err := ts.taskStore.GetByID(req.TaskID)
	if err != nil || t == nil {
		return fmt.Errorf("task %s not found: %w", req.TaskID, err)
	}

	// 3. Rate limit: check how many handoff steps already exist for this task
	if ts.maxHandoffSteps > 0 {
		steps, err := ts.stepStore.ListByTaskID(req.TaskID)
		if err != nil {
			return fmt.Errorf("failed to list steps for rate limit check: %w", err)
		}
		handoffCount := 0
		for _, s := range steps {
			if isHandoffStep(s) {
				handoffCount++
			}
		}
		if handoffCount >= ts.maxHandoffSteps {
			ts.logger.Warn("Handoff rate limit reached",
				KeyTaskID, req.TaskID,
				"handoff_count", handoffCount,
				"max", ts.maxHandoffSteps,
			)
			return fmt.Errorf("handoff rate limit reached: task %s already has %d handoff steps (max: %d)", req.TaskID, handoffCount, ts.maxHandoffSteps)
		}
	}

	// 4. Validate originating step exists (warn but continue if not found)
	fromStep, err := ts.stepStore.GetByID(req.FromStepID)
	if err != nil {
		ts.logger.Warn("Originating step not found for handoff, continuing",
			KeyStepID, req.FromStepID,
			KeyTaskID, req.TaskID,
		)
	}

	// 5. Derive tool_hint from target agent if not provided
	toolHint := req.ToolHint
	if toolHint == "" {
		toolHint = agentIDToToolHint(req.ToAgentID)
	}

	// 6. Build accumulated context from handoff request
	var contextParts []string
	if req.FromAgentID != "" || req.PartialResult != "" {
		fromLabel := req.FromAgentID
		if fromLabel == "" {
			fromLabel = "unknown agent"
		}
		if req.PartialResult != "" {
			contextParts = append(contextParts, fmt.Sprintf("[Handoff from %s]: %s", fromLabel, req.PartialResult))
		} else {
			contextParts = append(contextParts, fmt.Sprintf("[Handoff from %s]", fromLabel))
		}
	}
	if req.Reason != "" {
		contextParts = append(contextParts, fmt.Sprintf("Reason: %s", req.Reason))
	}
	accumulatedContext := strings.Join(contextParts, "\n")

	var newStepID string

	if ts.handoffUseAmendment && ts.amendmentMgr != nil {
		// 7a. Route through amendment system for audit trail
		metadata := map[string]any{
			"description": req.Description,
			"tool_hint":   toolHint,
		}
		if req.InjectAfter && fromStep != nil {
			metadata["depends_on"] = []string{req.FromStepID}
		}
		metadata["agent_id"] = req.ToAgentID
		metadata["is_handoff"] = true

		metadataJSON, err := json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal handoff amendment metadata: %w", err)
		}
		amendReq := task.NewAmendmentRequest(req.TaskID, task.AmendmentAddStep, fmt.Sprintf("Handoff from %s to %s", req.FromAgentID, req.ToAgentID))
		amendReq.Metadata = metadataJSON

		if err := ts.amendmentMgr.Submit(ctx, amendReq); err != nil {
			return fmt.Errorf("failed to submit handoff amendment: %w", err)
		}

		reply, err := ts.amendmentMgr.Process(ctx, amendReq.ID)
		if err != nil {
			return fmt.Errorf("failed to process handoff amendment: %w", err)
		}
		if !reply.Success {
			return fmt.Errorf("handoff amendment rejected: %s", reply.Message)
		}

		// Extract created step ID from reply metadata
		var replyMeta struct {
			StepID string `json:"step_id"`
		}
		if err := json.Unmarshal(reply.Metadata, &replyMeta); err == nil && replyMeta.StepID != "" {
			newStepID = replyMeta.StepID
		}

		// Set accumulated context on the created step (amendment handler doesn't do this)
		if newStepID != "" && accumulatedContext != "" {
			createdStep, err := ts.stepStore.GetByID(newStepID)
			if err == nil && createdStep != nil {
				createdStep.AccumulatedContext = accumulatedContext
				if err := ts.stepStore.Update(createdStep); err != nil {
					ts.logger.Error("Failed to set accumulated context on handoff step",
						KeyStepID, newStepID,
						KeyTaskID, req.TaskID,
						"error", err,
					)
				}
			}
		}

		// The amendment handler already updates TotalJobs, so skip step 8.
		// But if we need rewiring (inject_after), do it with the fromStep.
		// Note: the amendment handler sets DependsOn on the step, so rewiring
		// still needs to shift downstream dependencies from fromStep to newStepID.
	} else {
		// 7b. Direct step creation (no amendment system)
		sequence := int(9000 + tacticalSeq.Add(1)%1000)
		newStep := task.NewTaskStep(req.TaskID, req.Description, sequence)
		newStep.ToolHint = toolHint
		newStep.AccumulatedContext = accumulatedContext
		newStep.IsHandoff = true

		if req.InjectAfter && fromStep != nil {
			newStep.DependsOn = []string{req.FromStepID}
		}

		if err := ts.stepStore.Create(newStep); err != nil {
			return fmt.Errorf("failed to create handoff step: %w", err)
		}

		newStepID = newStep.ID

		// 8. Update task's TotalJobs count. Atomic increment (H12): the
		// full-row Update wrote back the whole snapshot fetched at step 2,
		// erasing any counter increment that landed concurrently between
		// the Get and this write (lost TotalJobs/completed counters).
		if err := ts.taskStore.IncrementTotalJobs(req.TaskID); err != nil {
			ts.logger.Error("Failed to update task TotalJobs after handoff",
				KeyTaskID, req.TaskID,
				"error", err,
			)
		}
	}

	// 9. Rewire downstream dependencies (Task 5)
	if fromStep != nil && req.InjectAfter && newStepID != "" {
		if err := ts.rewireDownstreamDeps(req.TaskID, req.FromStepID, newStepID); err != nil {
			ts.logger.Error("Failed to rewire downstream dependencies",
				KeyTaskID, req.TaskID,
				KeyStepID, newStepID,
				"error", err,
			)
		}
	}

	// 10. Promote ready steps
	promoted, err := ts.stepStore.PromoteReadySteps(req.TaskID)
	if err != nil {
		ts.logger.Error("Failed to promote ready steps after handoff",
			KeyTaskID, req.TaskID,
			"error", err,
		)
	}

	// 11. Schedule if steps were promoted
	if len(promoted) > 0 {
		if err := ts.ScheduleReadySteps(ctx, req.TaskID); err != nil {
			ts.logger.Error("Failed to schedule ready steps after handoff",
				KeyTaskID, req.TaskID,
				"error", err,
			)
		}
	}

	// 12. Publish event
	ts.publishEvent("task.handoff_created", map[string]any{
		KeyTaskID:    req.TaskID,
		KeyStepID:    newStepID,
		KeyAgentID:   req.ToAgentID,
		"from_agent": req.FromAgentID,
	})

	ts.logger.Info("Handoff step created",
		KeyTaskID, req.TaskID,
		KeyStepID, newStepID,
		KeyAgentID, req.ToAgentID,
		"from_step", req.FromStepID,
		"description", req.Description,
	)

	return nil
}

// rewireDownstreamDeps replaces the fromStepID dependency with newStepID in all
// downstream steps. This ensures that steps that previously depended on the
// from step now depend on the injected step instead.
func (ts *TacticalScheduler) rewireDownstreamDeps(taskID, fromStepID, newStepID string) error {
	allSteps, err := ts.stepStore.ListByTaskID(taskID)
	if err != nil {
		return fmt.Errorf("failed to list steps for rewiring: %w", err)
	}

	rewired := 0
	for _, ds := range allSteps {
		// Skip the new step and the from step
		if ds.ID == newStepID || ds.ID == fromStepID {
			continue
		}

		// Check if this step depends on the from step
		found := false
		for i, dep := range ds.DependsOn {
			if dep == fromStepID {
				ds.DependsOn[i] = newStepID
				found = true
				break
			}
		}

		if found {
			if err := ts.stepStore.Update(ds); err != nil {
				ts.logger.Error("Failed to update downstream step dependency",
					KeyStepID, ds.ID,
					"error", err,
				)
				continue
			}
			rewired++
			ts.logger.Info("Rewired downstream dependency",
				KeyStepID, ds.ID,
				"old_dep", fromStepID,
				"new_dep", newStepID,
				"task_id", taskID,
			)
		}
	}

	if rewired > 0 {
		ts.logger.Info("Rewired downstream dependencies",
			KeyTaskID, taskID,
			"count", rewired,
			"from_step", fromStepID,
			"new_step", newStepID,
		)
	}

	return nil
}

// agentIDToToolHint maps an agent ID to the corresponding tool hint/intent type.
// isHandoffStep reports whether a step was created by the handoff system.
// It checks the dedicated IsHandoff field first, with a fallback to the
// legacy "[Handoff from" sentinel in AccumulatedContext for backwards
// compatibility with steps stored before the field was added.
func isHandoffStep(s *task.TaskStep) bool {
	return s.IsHandoff || (s.AccumulatedContext != "" && strings.Contains(s.AccumulatedContext, "[Handoff from"))
}

func agentIDToToolHint(agentID string) string {
	switch agentID {
	case config.AgentIDCoder:
		return string(IntentCode)
	case config.AgentIDDebugger:
		return string(IntentDebug)
	case config.AgentIDAnalyst:
		return string(IntentAnalyze)
	case config.AgentIDResearcher:
		return string(IntentResearch)
	case config.AgentIDCommitter:
		return string(IntentGit)
	case config.AgentIDScheduler:
		return string(IntentSchedule)
	case config.AgentIDPlanner:
		return string(IntentPlan)
	default:
		return "chat"
	}
}
