package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/memvid"
	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/templates"
)

// anaphoraForRegex matches "do the same for X" patterns for anaphora resolution.
var anaphoraForRegex = regexp.MustCompile(`do the same for (.+)`)

// SteeringHeuristicTable defines which intent types should interrupt (steer)
// vs wait for a natural stopping point (follow-up) when an agent loop is
// already running for the conversation.
var SteeringHeuristicTable = map[IntentType]bool{
	// HIGH URGENCY - Steer (interrupt immediately)
	IntentCode:     true, // User is redirecting coding approach
	IntentDebug:    true, // User spotted a bug mid-execution
	IntentSecurity: true, // Security concern needs immediate attention
	IntentToolUse:  true, // Explicit tool redirection
	IntentGit:      true, // Git operations are action-oriented
	IntentPlan:     true, // Plan changes redirect execution

	// MEDIUM/LOW URGENCY - Follow-up (wait for natural stop)
	IntentChat:     false, // General chat can wait
	IntentRecall:   false, // Memory recall is not urgent
	IntentResearch: false, // Research extensions follow naturally
	IntentReport:   false, // Reporting status/information
	IntentPlatform: false, // Platform events are informational
	IntentStatus:   false, // Status inquiries
	IntentReview:   false, // Review requests build on completion
	IntentSchedule: false, // Scheduling is not urgent
	IntentAnalyze:  false, // Analysis extends naturally
	IntentSearch:   false, // Search queries are not urgent
	IntentSkill:    false, // Skill operations can wait
	IntentCompound: false, // Compound intents default to follow-up
	IntentUnknown:  false,
}

// shouldSteer determines if a message should interrupt the current flow.
// Returns true for steering, false for follow-up.
func shouldSteer(intentType IntentType, explicitSteerMode bool) bool {
	// Explicit user override (ctrl+s) always wins
	if explicitSteerMode {
		return true
	}

	// Intent-based heuristic
	if shouldSteer, exists := SteeringHeuristicTable[intentType]; exists {
		return shouldSteer
	}

	// Default: follow-up (safer, less disruptive)
	return false
}

// Intent represents the classified intent of a user message.
type Intent struct {
	// Type is the high-level intent category.
	Type string `json:"type"`
	// Confidence is the confidence score [0.0, 1.0].
	Confidence float64 `json:"confidence"`
	// AgentType is the specialist agent to route to.
	AgentType string `json:"agent_type"`
	// MemoryRefs are relevant memory IDs to pass along.
	MemoryRefs []string `json:"memory_refs,omitempty"`
	// RequiresPlanning indicates if the task needs planning first.
	RequiresPlanning bool `json:"requires_planning"`
	// Summary is a brief description of the intent.
	Summary string `json:"summary,omitempty"`
}

// MemoryContext wraps memory results with conversation metadata.
type MemoryContext struct {
	Results      []memory.MemoryResult `json:"results"`
	LastIntent   *Intent               `json:"last_intent,omitempty"`
	LastAgent    string                `json:"last_agent,omitempty"`
	IntentCounts map[string]int        `json:"intent_counts,omitempty"`
}

// DispatchResult is the result of dispatching a request.
type DispatchResult struct {
	// Task is the created task if any.
	Task *task.Task `json:"task,omitempty"`
	// AgentID is the agent that will handle the request.
	AgentID string `json:"agent_id"`
	// Intent is the classified intent.
	Intent *Intent `json:"intent"`
	// Response is the direct response if no agent delegation needed.
	Response string `json:"response,omitempty"`
	// MemoryContext are memories retrieved for context.
	MemoryContext []memory.MemoryResult `json:"memory_context,omitempty"`
	// Steps are step summaries for the ACK message.
	Steps []TaskStepSummary `json:"steps,omitempty"`
	// ExplicitSteerMode indicates the user pressed ctrl+s to force steering.
	ExplicitSteerMode bool `json:"explicit_steer_mode,omitempty"`
}

// Dispatcher handles intake classification and routing of requests.
type Dispatcher struct {
	registry          *AgentRegistry
	memvid            *memvid.Client
	memoryMgr         *memory.Manager
	taskStore         *task.Store
	taskRegistry      *task.Registry
	amendmentMgr      *task.AmendmentManager
	skillRegistry     *skills.Registry
	skillExecutor     *skills.Executor
	templateRegistry  *templates.Registry
	logger            *slog.Logger
	llmClassifier     *LLMClassifier
	keywordClassifier *KeywordClassifier
	capabilityMatcher *CapabilityMatcher
	semanticIndex     *SemanticIndex
	sessionTracker    *SessionTracker
	stats             *DispatcherStats
	router            *ReportRouter
}

// IntentClassifier is an interface for classifying intents.
type IntentClassifier interface {
	Classify(ctx context.Context, input string, memCtx *MemoryContext) (*Intent, error)
}

// DispatcherConfig holds configuration for creating a Dispatcher.
type DispatcherConfig struct {
	Registry          *AgentRegistry
	MemvidClient      *memvid.Client
	MemoryMgr         *memory.Manager
	TaskStore         *task.Store
	TaskRegistry      *task.Registry
	AmendmentManager  *task.AmendmentManager
	SkillRegistry     *skills.Registry
	SkillExecutor     *skills.Executor
	TemplateRegistry  *templates.Registry
	Logger            *slog.Logger
	LLMClient         *llm.Client
	ClassifierClient  *llm.Client // Separate client for classification (nil = use LLMClient)
	ClassifierModel   string
	ClassifierTimeout time.Duration // Per-classification timeout; 0 = defaultClassifierTimeout (10s).
	CapabilityMatcher *CapabilityMatcher
	EmbeddingClient   EmbeddingClient
	SessionMaxAge     time.Duration
}

// NewDispatcher creates a new dispatcher.
func NewDispatcher(cfg DispatcherConfig) *Dispatcher {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	d := &Dispatcher{
		registry:          cfg.Registry,
		memvid:            cfg.MemvidClient,
		memoryMgr:         cfg.MemoryMgr,
		taskStore:         cfg.TaskStore,
		taskRegistry:      cfg.TaskRegistry,
		amendmentMgr:      cfg.AmendmentManager,
		skillRegistry:     cfg.SkillRegistry,
		skillExecutor:     cfg.SkillExecutor,
		templateRegistry:  cfg.TemplateRegistry,
		logger:            cfg.Logger,
		capabilityMatcher: cfg.CapabilityMatcher,
	}

	// Add keyword-based classifier
	d.keywordClassifier = &KeywordClassifier{}

	// Add LLM-based classifier if a client is provided.
	// Prefer the dedicated ClassifierClient; fall back to the main LLMClient.
	classifierClient := cfg.ClassifierClient
	if classifierClient == nil {
		classifierClient = cfg.LLMClient
	}
	if classifierClient != nil {
		d.llmClassifier = NewLLMClassifier(LLMClassifierConfig{
			Client:  classifierClient,
			Model:   cfg.ClassifierModel,
			Timeout: cfg.ClassifierTimeout,
			Logger:  cfg.Logger,
		})
	}

	// Initialize semantic index if embedding client is provided
	if cfg.EmbeddingClient != nil {
		d.semanticIndex = NewSemanticIndex(cfg.EmbeddingClient)
		// Build index in background
		go func() { //nolint:gosec // background goroutine outlives request context
			if err := d.semanticIndex.BuildIndex(context.Background()); err != nil {
				d.logger.Warn("Failed to build semantic index", "error", err)
			}
		}()
	}

	// Initialize session tracker
	maxAge := cfg.SessionMaxAge
	if maxAge == 0 {
		maxAge = 30 * time.Minute
	}
	d.sessionTracker = NewSessionTracker(maxAge)

	// Initialize stats tracking
	d.stats = &DispatcherStats{
		ByMethod: make(map[string]int),
		ByAgent:  make(map[string]int),
		ByIntent: make(map[string]int),
	}

	// Initialize report router for multi-agent handoff
	d.router = NewReportRouter(ReportRouterConfig{
		Registry:   d.registry,
		Dispatcher: d,
		Logger:     cfg.Logger,
	})

	return d
}

// ClassifyAndRoute is the main entry point for the dispatcher.
func (d *Dispatcher) ClassifyAndRoute(ctx context.Context, input, sessionID string) (*DispatchResult, error) {
	d.logger.Debug("Dispatching request", "session", sessionID, "input_len", len(input))

	// Check for explicit skill invocation (/skill-name)
	if strings.HasPrefix(input, "/") {
		skillName, skillInput := d.parseSkillInvocation(input)
		if skill := d.getSkill(skillName); skill != nil {
			d.logger.Info("Skill invocation detected",
				"skill", skillName,
				"session", sessionID,
			)
			return d.executeSkill(ctx, skill, skillInput, sessionID)
		}
		// Check template registry if no skill matched
		if d.templateRegistry != nil {
			if tmpl := d.templateRegistry.Get(skillName); tmpl != nil {
				d.logger.Info("Template invocation detected",
					"template", skillName,
					"session", sessionID,
				)
				substituted := d.substituteTemplate(tmpl, skillInput)
				// Treat substituted text as normal user input
				input = substituted
				// Fall through to normal intent classification
			}
		}
		// Not a valid skill or template, fall through to normal routing
	}

	// 1. Build memory context with session history
	memCtx := d.buildMemoryContext(ctx, input, sessionID)

	// 2. Resolve anaphora (context references)
	resolvedInput := d.resolveAnaphora(input, memCtx)

	// 3. Check for compound (multi-intent) requests
	multiIntent := d.classifyMultiIntent(ctx, resolvedInput, memCtx)
	if multiIntent.IsCompound {
		return d.routeCompound(ctx, multiIntent, input, sessionID)
	}

	// 4. Classify primary intent
	intent := d.classifyIntent(ctx, resolvedInput, memCtx)

	// 5. Extract memory refs for context continuity
	intent.MemoryRefs = d.extractMemoryRefs(memCtx.Results)

	// 6. Create task if needed (for trackable work)
	var createdTask *task.Task
	if d.shouldCreateTask(intent) && d.taskStore != nil {
		createdTask = d.createTask(ctx, input, intent, sessionID)
	}

	// 7. Determine routing
	result := &DispatchResult{
		Task:          createdTask,
		AgentID:       intent.AgentType,
		Intent:        intent,
		MemoryContext: memCtx.Results,
	}

	d.logger.Info("Dispatched request",
		"agent", intent.AgentType,
		"intent_type", intent.Type,
		"confidence", intent.Confidence,
		"memory_refs", len(intent.MemoryRefs),
		"has_task", createdTask != nil,
	)

	// Record intent in session tracker
	d.sessionTracker.RecordIntent(sessionID, intent, intent.AgentType)

	return result, nil
}

// classifyIntent uses classifiers to determine intent with fallback chain:
// 1. Try capability matcher (fast, no LLM) if available and confident
// 2. Try LLM classifier (if available)
// 3. If LLM fails OR confidence < threshold → try Keyword classifier
// 4. If Keyword fails → return Chat fallback
func (d *Dispatcher) classifyIntent(ctx context.Context, input string, memCtx *MemoryContext) *Intent {
	d.recordTotalDispatch()

	// Step 1: Try capability matcher first (fast, no LLM)
	if d.capabilityMatcher != nil {
		result := d.capabilityMatcher.Match(input)
		if result != nil && result.Confidence >= 0.7 {
			d.logger.Debug("Capability matcher succeeded",
				"agent", result.AgentID,
				"intent", result.IntentType,
				"confidence", result.Confidence,
				"match_type", result.MatchType,
			)
			intent := &Intent{
				Type:       result.IntentType,
				Confidence: result.Confidence,
				AgentType:  result.AgentID,
				Summary:    extractSummary(input),
			}
			d.recordClassificationMethod("capability_matcher")
			d.recordAgent(result.AgentID)
			d.recordIntentType(result.IntentType)
			return d.applyContextWeighting(intent, memCtx, input)
		}
		if result != nil {
			d.logger.Debug("Capability matcher result below threshold",
				"agent", result.AgentID,
				"confidence", result.Confidence,
				"threshold", 0.7,
			)
		}
	}

	// Step 2: Try LLM classifier if available
	if d.llmClassifier != nil {
		intent, err := d.llmClassifier.Classify(ctx, input, memCtx)
		if err == nil && intent != nil {
			if ShouldUseLLMResult(intent) {
				d.logger.Debug("LLM classifier succeeded",
					"intent", intent.Type,
					"confidence", intent.Confidence,
				)
				d.recordClassificationMethod("llm")
				d.recordAgent(intent.AgentType)
				d.recordIntentType(intent.Type)
				return d.applyContextWeighting(intent, memCtx, input)
			}
			d.logger.Debug("LLM classifier result below threshold",
				"intent", intent.Type,
				"confidence", intent.Confidence,
				"threshold", GetThresholdForIntent(intent.Type),
			)
		} else if err != nil {
			d.logger.Warn("LLM classifier failed, trying keyword", "error", err)
		}
	}

	// Step 3: Try Keyword classifier
	if d.keywordClassifier != nil {
		intent, err := d.keywordClassifier.Classify(ctx, input, memCtx)
		if err == nil && intent != nil {
			d.logger.Debug("Keyword classifier succeeded",
				"intent", intent.Type,
				"confidence", intent.Confidence,
			)
			d.recordClassificationMethod("keyword")
			d.recordAgent(intent.AgentType)
			d.recordIntentType(intent.Type)
			return d.applyContextWeighting(intent, memCtx, input)
		}
		d.logger.Warn("Keyword classifier failed", "error", err)
	}

	// Step 3.5: Semantic matching (before fallback)
	if d.semanticIndex != nil {
		match := d.semanticIndex.Match(input, 0.6)
		if match != nil {
			d.logger.Debug("Semantic classifier succeeded",
				"intent", match.IntentType,
				"confidence", match.Confidence,
			)
			intent := &Intent{
				Type:       string(match.IntentType),
				Confidence: match.Confidence,
				AgentType:  match.IntentType.DefaultAgent(),
				Summary:    extractSummary(input),
			}
			d.recordClassificationMethod("semantic")
			d.recordAgent(intent.AgentType)
			d.recordIntentType(intent.Type)
			return d.applyContextWeighting(intent, memCtx, input)
		}
	}

	// Step 4: Fallback to Chat for clarification
	d.recordFallback(input, "all_classifiers_failed", 0.0, config.AgentIDChat)
	d.recordClassificationMethod("fallback")
	d.recordAgent(config.AgentIDChat)
	d.recordIntentType(string(IntentChat))
	return &Intent{
		Type:       string(IntentChat),
		Confidence: 0.3,
		AgentType:  config.AgentIDChat,
		Summary:    "Could not determine intent, clarifying with user",
	}
}

// buildMemoryContext builds memory context with session history.
func (d *Dispatcher) buildMemoryContext(ctx context.Context, input, sessionID string) *MemoryContext {
	if d.memoryMgr == nil {
		return &MemoryContext{
			Results:      []memory.MemoryResult{},
			IntentCounts: make(map[string]int),
		}
	}

	// Search for relevant memories
	results, err := d.memoryMgr.Search(ctx, memory.MemoryQuery{
		Query: input,
		Limit: 5,
	})
	if err != nil {
		d.logger.Debug("Memory search failed", "error", err)
		results = []memory.MemoryResult{}
	}

	// Build context from session tracker
	memCtx := &MemoryContext{
		Results:      results,
		IntentCounts: make(map[string]int),
	}

	// Get session history if available
	if d.sessionTracker != nil {
		state := d.sessionTracker.GetSession(sessionID)
		if state != nil {
			// Get last intent
			if lastIntent := d.sessionTracker.GetLastIntent(sessionID); lastIntent != nil {
				memCtx.LastIntent = lastIntent
				memCtx.LastAgent = lastIntent.AgentType
			}
			// Get intent counts
			memCtx.IntentCounts = d.sessionTracker.GetIntentCounts(sessionID)
		}
	}

	return memCtx
}

// extractMemoryRefs extracts memory IDs from search results.
func (d *Dispatcher) extractMemoryRefs(results []memory.MemoryResult) []string {
	refs := make([]string, 0, len(results))
	for _, r := range results {
		if r.RelevanceScore > 0.3 { // Only include reasonably relevant memories
			refs = append(refs, r.Memory.ID)
		}
	}
	return refs
}

// shouldCreateTask determines if a task should be created.
func (d *Dispatcher) shouldCreateTask(intent *Intent) bool {
	it := IntentType(intent.Type)
	if it.ShouldCreateTask() {
		return true
	}
	// Fallback for unknown intents with RequiresPlanning flag
	return intent.RequiresPlanning
}

// createTask creates a new task for the request.
func (d *Dispatcher) createTask(_ context.Context, input string, intent *Intent, sessionID string) *task.Task {
	// Create task summary
	summary := intent.Summary
	if summary == "" {
		summary = truncateString(input, 100)
	}

	t := task.NewTask(summary, input)
	t.LinkSession(sessionID)

	// Store task
	if d.taskStore != nil {
		if err := d.taskStore.Create(t); err != nil {
			d.logger.Error("Failed to create task", "error", err)
			return nil
		}
		// Persist the session-task link to the DB
		if err := d.taskStore.LinkSession(t.ID, sessionID); err != nil {
			d.logger.Warn("Failed to link session", "error", err)
		}
	}

	return t
}

// MultiIntent represents multiple detected intents in a single request.
type MultiIntent struct {
	Intents      []*Intent `json:"intents"`
	IsCompound   bool      `json:"is_compound"`
	CompoundType string    `json:"compound_type,omitempty"` // "sequential" or "parallel"
	Summary      string    `json:"summary"`
}

// DetectCompound analyzes intents and determines if they're compound.
func (m *MultiIntent) DetectCompound() bool {
	if len(m.Intents) < 2 {
		m.IsCompound = false
		return false
	}
	m.IsCompound = true
	for _, intent := range m.Intents {
		if intent.RequiresPlanning {
			m.CompoundType = "sequential"
			return true
		}
	}
	m.CompoundType = "parallel"
	return true
}

// routeCompound handles compound (multi-intent) request routing.
func (d *Dispatcher) routeCompound(ctx context.Context, multi *MultiIntent, _, sessionID string) (*DispatchResult, error) {
	// Cap intents at 5 for safety
	if len(multi.Intents) > 5 {
		multi.Intents = multi.Intents[:5]
	}

	d.logger.Info("Compound intent detected",
		"intents", len(multi.Intents),
		"type", multi.CompoundType,
	)

	// Create a parent task to track the compound request
	parentTask := d.createTask(ctx, multi.Summary, &Intent{
		Type:    string(IntentCompound),
		Summary: multi.Summary,
	}, sessionID)

	if parentTask == nil {
		return nil, fmt.Errorf("failed to create parent task for compound request")
	}

	// Record compound metadata with individual intent types
	intentTypes := make([]string, 0, len(multi.Intents))
	for _, intent := range multi.Intents {
		intentTypes = append(intentTypes, intent.Type)
	}
	meta, err := json.Marshal(map[string]any{
		"compound_type":         multi.CompoundType,
		"compound_intents":      len(multi.Intents),
		"compound_intent_types": intentTypes,
	})
	if err == nil {
		parentTask.Metadata = json.RawMessage(meta)
	}
	if d.taskStore != nil {
		if err := d.taskStore.Update(parentTask); err != nil {
			d.logger.Warn("Failed to update compound task metadata", "error", err)
		}
	}

	// Record compound stats
	d.recordCompoundDispatch(len(multi.Intents))

	// Build step summaries from compound intents
	steps := make([]TaskStepSummary, 0, len(multi.Intents))
	for _, intent := range multi.Intents {
		steps = append(steps, TaskStepSummary{
			Description: intent.Summary,
			AgentID:     intent.AgentType,
		})
	}

	return &DispatchResult{
		Task:    parentTask,
		AgentID: "orchestrator",
		Intent: &Intent{
			Type:    string(IntentCompound),
			Summary: multi.Summary,
		},
		Steps: steps,
	}, nil
}

// classifyMultiIntent runs classification to detect all potential intents.
func (d *Dispatcher) classifyMultiIntent(ctx context.Context, input string, memCtx *MemoryContext) *MultiIntent {
	var intents []*Intent

	// Run keyword classifier for all matches
	if d.keywordClassifier != nil {
		keywordIntents := d.keywordClassifier.ClassifyAll(ctx, input, memCtx)
		intents = append(intents, keywordIntents...)
	}

	// Run LLM multi-intent classifier if available
	if d.llmClassifier != nil {
		llmIntents := d.llmClassifier.ClassifyMulti(ctx, input, nil)
		intents = append(intents, llmIntents...)
	}

	// Deduplicate
	intents = deduplicateIntents(intents)

	multi := &MultiIntent{
		Intents: intents,
		Summary: extractSummary(input),
	}
	multi.DetectCompound()

	return multi
}

// RouteToAgent routes a dispatch result to the appropriate agent.
// If an active agent loop exists for this conversation, it injects
// the message into the queue (steer or follow-up) based on the
// SteeringHeuristicTable. Otherwise, it runs the agent synchronously.
func (d *Dispatcher) RouteToAgent(ctx context.Context, result *DispatchResult, conversationID string) (string, error) {
	if d.registry == nil {
		return "", fmt.Errorf("no agent registry configured")
	}

	// Handle platform introspection directly without LLM
	if result.Intent != nil && result.Intent.Type == string(IntentPlatform) {
		return d.handlePlatformIntrospection(ctx, result.Intent.Summary)
	}

	// Check if there's an active agent loop for this conversation
	queue, generation := d.registry.GetActiveQueue(conversationID)
	if queue != nil {
		// Check if queue is still active
		if queue.IsClosed() {
			d.logger.Info("Queue is closed, running new agent",
				"conversation", conversationID,
			)
			// Fall through to normal execution
		} else {
			d.logger.Info("Steering active agent",
				"conversation", conversationID,
				"agent", result.AgentID,
				"generation", generation,
			)

			// Determine steering vs follow-up based on heuristic
			isSteer := shouldSteer(IntentType(result.Intent.Type), result.ExplicitSteerMode)

			if isSteer {
				if err := queue.Steer(ctx, result.Intent.Summary, config.AgentIDDispatcher); err != nil {
					if errors.Is(err, ErrQueueClosed) || errors.Is(err, ErrQueueFull) {
						d.logger.Warn("Queue injection failed, starting new agent",
							"conversation", conversationID,
							"error", err,
						)
						// Fall through to new agent
					} else {
						return "", err
					}
				} else {
					return "message queued (steer)", nil
				}
			} else {
				if err := queue.FollowUp(ctx, result.Intent.Summary, config.AgentIDDispatcher); err != nil {
					if errors.Is(err, ErrQueueClosed) || errors.Is(err, ErrQueueFull) {
						d.logger.Warn("Queue injection failed, starting new agent",
							"conversation", conversationID,
							"error", err,
						)
						// Fall through to new agent
					} else {
						return "", err
					}
				} else {
					return "message queued (follow-up)", nil
				}
			}
		}
	}

	// No active loop, or queue closed/full -- run normally
	// Build context message with memory refs
	contextMsg := d.buildContextMessage(result)

	// Get the agent
	agent, err := d.registry.Get(result.AgentID)
	if err != nil {
		d.logger.Warn("Agent not found, falling back to chat", "agent", result.AgentID, "error", err)
		agent, err = d.registry.Get(config.AgentIDChat)
		if err != nil {
			return "", fmt.Errorf("fallback agent not found: %w", err)
		}
	}

	// Run the agent
	response, err := agent.RunOnce(ctx, contextMsg, conversationID)
	if err != nil {
		return "", fmt.Errorf("agent execution failed: %w", err)
	}

	// Parse structured report and route through report router
	report := ExtractReport(response)
	action := DetermineRouteAction(report)
	d.logger.Info("Agent completed",
		"action", action.String(),
		"agent", result.AgentID,
		"has_report", report != nil,
	)
	displayResponse := StripReport(response)

	// Use report router to determine next action
	routeResult := d.router.Route(ctx, RouteParams{
		Report:  report,
		Action:  action,
		AgentID: result.AgentID,
		Depth:   0,
	})

	// If routing suggests a next agent, handle the handoff
	if action == RouteActionRoute && !routeResult.ForceNotify && report != nil {
		nextAgentID := report.SuggestedNextAgent
		d.logger.Info("Routing to next agent",
			"from", result.AgentID,
			"to", nextAgentID,
			"depth", routeResult.Depth,
		)
		// Build accumulated context from previous agent's work
		accumulatedContext := d.buildAccumulatedContext(report, displayResponse)
		nextResult := &DispatchResult{
			AgentID: nextAgentID,
			Intent:  result.Intent,
		}
		_ = accumulatedContext // used for context enrichment in recursive call
		// Recursively route to the next agent
		return d.RouteToAgent(ctx, nextResult, conversationID)
	}

	// Record memory of this interaction
	if d.memvid != nil && d.memvid.IsAvailable(ctx) {
		go d.recordInteraction(context.Background(), result, displayResponse) //nolint:gosec // background goroutine outlives request context
	}

	// Use route result's response if available, otherwise use display response
	finalResponse := displayResponse
	if routeResult.FinalResponse != "" && routeResult.ForceNotify {
		finalResponse = routeResult.FinalResponse + "\n\n" + displayResponse
	}

	return finalResponse, nil
}

// handlePlatformIntrospection returns platform capabilities directly.
// This bypasses the LLM for reliable introspection responses.
func (d *Dispatcher) handlePlatformIntrospection(ctx context.Context, input string) (string, error) {
	// Check for stats-specific queries
	lower := strings.ToLower(input)
	if strings.Contains(lower, "dispatcher stats") || strings.Contains(lower, "routing stats") {
		return d.handleStatsQuery(ctx)
	}

	var sb strings.Builder

	sb.WriteString("## Platform Capabilities\n\n")

	// List available agents
	if d.registry != nil {
		specs := d.registry.ListSpecs()
		sb.WriteString("### Available Agents\n\n")
		for _, spec := range specs {
			fmt.Fprintf(&sb, "- **%s** (%s): %s\n", spec.Name, spec.ID, truncateString(spec.Purpose, 100))
		}
		sb.WriteString("\n")
	}

	// List baseline tools available to all agents
	sb.WriteString("### Baseline Tools (available to all agents)\n\n")
	for _, tool := range BaselineTools {
		fmt.Fprintf(&sb, "- %s\n", tool)
	}
	sb.WriteString("\n")

	// List available skills
	if d.skillRegistry != nil {
		skillList := d.skillRegistry.List()
		if len(skillList) > 0 {
			sb.WriteString("### Available Skills\n\n")
			for _, skill := range skillList {
				fmt.Fprintf(&sb, "- **/%s**: %s\n", skill.Name, truncateString(skill.Description, 80))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("### How to Use\n\n")
	sb.WriteString("- Ask me to do something and I'll route it to the right specialist agent\n")
	sb.WriteString("- Use `/skill-name` to invoke a specific skill directly\n")
	sb.WriteString("- Complex tasks are automatically decomposed and tracked\n")

	return sb.String(), nil
}

// handleStatsQuery returns dispatcher statistics as JSON.
func (d *Dispatcher) handleStatsQuery(_ context.Context) (string, error) {
	stats := d.GetStats()

	result := map[string]any{
		"total_dispatched": stats.TotalDispatched,
		"by_method":        stats.ByMethod,
		"by_agent":         stats.ByAgent,
		"by_intent":        stats.ByIntent,
		"fallback_count":   stats.FallbackCount,
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal stats: %w", err)
	}
	return string(data), nil
}

// buildContextMessage builds a message with memory context injected.
func (d *Dispatcher) buildContextMessage(result *DispatchResult) string {
	var parts []string

	// Add relevant memory context
	if len(result.MemoryContext) > 0 {
		parts = append(parts, "## Relevant Context\n")
		for i, m := range result.MemoryContext {
			if i >= 5 { // Limit context
				break
			}
			parts = append(parts, fmt.Sprintf("- %s\n", truncateString(m.Memory.Content, 200)))
		}
		parts = append(parts, "\n---\n\n")
	}

	// Add task context if available
	if result.Task != nil {
		parts = append(parts, fmt.Sprintf("Task ID: %s\n", result.Task.ID))
	}

	// Add the original input
	parts = append(parts, result.Intent.Summary)

	return strings.Join(parts, "")
}

// buildAccumulatedContext creates context from a previous agent's report for the next agent.
func (d *Dispatcher) buildAccumulatedContext(report *AgentReport, displayResponse string) string {
	var parts []string
	if len(report.Accomplished) > 0 {
		parts = append(parts, "accomplished: "+strings.Join(report.Accomplished, "; "))
	}
	if len(report.Issues) > 0 {
		parts = append(parts, "issues: "+strings.Join(report.Issues, "; "))
	}
	if len(report.Observations) > 0 {
		parts = append(parts, "observations: "+strings.Join(report.Observations, "; "))
	}
	if report.DecisionContext != "" {
		parts = append(parts, "decision context: "+report.DecisionContext)
	}
	return strings.Join(parts, "\n")
}

// recordInteraction records the interaction to memory.
func (d *Dispatcher) recordInteraction(ctx context.Context, result *DispatchResult, response string) {
	if d.memvid == nil {
		return
	}

	content := fmt.Sprintf("User intent: %s\nAgent: %s\nResponse summary: %s",
		result.Intent.Summary,
		result.AgentID,
		truncateString(response, 500),
	)

	metadata := map[string]any{
		"intent_type": result.Intent.Type,
		KeyAgentID:    result.AgentID,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}

	if result.Task != nil {
		metadata["task_id"] = result.Task.ID
	}

	// Use episodic zone
	episodicClient := d.memvid.WithZone("episodic")
	_, err := episodicClient.Store(ctx, content, metadata)
	if err != nil {
		d.logger.Warn("Failed to record interaction", "error", err)
	}
}

// keywordPattern defines a keyword-to-intent mapping.
type keywordPattern struct {
	keywords   []string
	intentType string
	agentType  string
	confidence float64
	planning   bool
}

// keywordPatterns is the shared table of keyword patterns for intent classification.
// Used by both Classify (best match) and ClassifyAll (all matches).
var keywordPatterns = []keywordPattern{
	// Platform introspection (highest priority - matches first)
	{[]string{"what are your capabilities", "what can you do", "what tools", "what agents", "what kind of systems", "help me understand", "system access", "platform status",
		"internal capabilities", "your capabilities", "tell me about your", "built into", "agent harness", "memory system", "tool system",
		"what models", "what agents are", "available tools", "your tools", "your features", "how are you built", "your architecture",
		"what are you aware of", "what do you have access to", "platform capabilities", "system capabilities"}, string(IntentPlatform), config.AgentIDChat, 0.9, false},

	// Report/Summary requests (high priority - handle inline, not async)
	{[]string{"give me a report", "report on", "what did you do", "what have you done", "what did you accomplish", "summarize what", "summary of work", "work summary", "status report", "progress report", "what happened"}, string(IntentReport), config.AgentIDChat, 0.9, false},

	// Recall/Memory requests (high priority - handle inline)
	{[]string{"remember when", string(IntentRecall), "what do you remember", "do you remember", "last time we"}, string(IntentRecall), config.AgentIDChat, 0.85, false},

	// Code-related
	{[]string{KeywordFix + " bug", string(IntentDebug), "error", "exception", "crash", "not working"}, string(IntentDebug), config.AgentIDDebugger, 0.8, false},
	{[]string{"write code", "implement", "create function", "add feature", KeywordRefactor}, string(IntentCode), config.AgentIDCoder, 0.8, false},
	{[]string{"code review", "review pr", "check code"}, string(IntentReview), config.AgentIDCoder, 0.75, false},

	// Git operations
	{[]string{KeywordCommit, "push", "pull", "merge", "branch", string(IntentGit)}, string(IntentGit), config.AgentIDCommitter, 0.8, false},

	// Scheduling
	{[]string{"remind", string(IntentSchedule), "alarm", "timer", "at ", "tomorrow", "next week"}, string(IntentSchedule), config.AgentIDScheduler, 0.8, false},

	// Planning
	{[]string{string(IntentPlan), KeywordDesign, "architect", "how should i", "break down", "decompose"}, string(IntentPlan), config.AgentIDPlanner, 0.8, true},

	// Analysis/Research ("summarize" alone stays here for document summarization;
	// "summarize what" and "summary of work" are captured by report intent above)
	{[]string{"research", string(IntentAnalyze), "summarize", KeywordExplain, "what is", "how does"}, string(IntentAnalyze), config.AgentIDAnalyst, 0.7, false},
	{[]string{string(IntentSearch), "find", "look up", "google"}, string(IntentSearch), config.AgentIDAnalyst, 0.7, false},

	// General chat (lower priority)
	{[]string{"hello", "hi", "hey", "thanks", "thank you", "help"}, string(IntentChat), config.AgentIDChat, 0.6, false},
}

// KeywordClassifier is a simple keyword-based intent classifier.
type KeywordClassifier struct{}

// Classify classifies intent based on keywords.
func (c *KeywordClassifier) Classify(ctx context.Context, input string, memCtx *MemoryContext) (*Intent, error) {
	lower := strings.ToLower(input)

	var bestMatch *Intent
	bestScore := 0.0

	for _, p := range keywordPatterns {
		for _, kw := range p.keywords {
			if strings.Contains(lower, kw) {
				// Score based on keyword length and position
				score := p.confidence * (float64(len(kw)) / float64(len(input)+1))
				if strings.HasPrefix(lower, kw) {
					score *= 1.2 // Boost for prefix matches
				}

				if score > bestScore {
					bestScore = score
					adjustedConfidence := math.Min(score, 1.0)
					bestMatch = &Intent{
						Type:             p.intentType,
						Confidence:       adjustedConfidence,
						AgentType:        p.agentType,
						RequiresPlanning: p.planning,
						Summary:          extractSummary(input),
					}
				}
			}
		}
	}

	return bestMatch, nil
}

// ClassifyAll returns ALL keyword matches (not just best match).
func (c *KeywordClassifier) ClassifyAll(ctx context.Context, input string, memCtx *MemoryContext) []*Intent {
	lower := strings.ToLower(input)
	var intents []*Intent

	for _, p := range keywordPatterns {
		for _, kw := range p.keywords {
			if strings.Contains(lower, kw) {
				intents = append(intents, &Intent{
					Type:             p.intentType,
					Confidence:       p.confidence * 0.5,
					AgentType:        p.agentType,
					RequiresPlanning: p.planning,
					Summary:          extractSummary(input),
				})
				break // one match per pattern is enough
			}
		}
	}

	return deduplicateIntents(intents)
}

// deduplicateIntents keeps only the highest confidence intent per type.
func deduplicateIntents(intents []*Intent) []*Intent {
	seen := make(map[string]*Intent)
	for _, intent := range intents {
		existing, ok := seen[intent.Type]
		if !ok || intent.Confidence > existing.Confidence {
			seen[intent.Type] = intent
		}
	}
	result := make([]*Intent, 0, len(seen))
	for _, intent := range seen {
		result = append(result, intent)
	}
	return result
}

// applyContextWeighting adjusts confidence based on conversation context.
func (d *Dispatcher) applyContextWeighting(intent *Intent, memCtx *MemoryContext, input string) *Intent {
	// Skip context weighting if memCtx is nil (e.g., in tests)
	if memCtx == nil {
		return intent
	}

	boost := 0.0

	if memCtx.LastIntent != nil && memCtx.LastIntent.Type == intent.Type {
		boost += 0.15
	}

	if memCtx.LastAgent != "" && memCtx.LastAgent == intent.AgentType {
		boost += 0.1
	}

	if count, ok := memCtx.IntentCounts[intent.Type]; ok && count >= 2 {
		boost += 0.05 * float64(count)
	}

	if hasAnaphora(input) && memCtx.LastIntent != nil {
		if intent.Type == memCtx.LastIntent.Type {
			boost += 0.2
		}
	}

	if boost > 0.3 {
		boost = 0.3
	}

	intent.Confidence = math.Min(intent.Confidence+boost, 1.0)
	return intent
}

// hasAnaphora checks if input contains context-referring language.
func hasAnaphora(input string) bool {
	lower := strings.ToLower(input)
	anaphora := []string{
		"do the same", "same thing", "also", "too", "as well",
		"this", "that", "these", "those",
		"continue", "keep going", "next",
	}
	for _, word := range anaphora {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// resolveAnaphora replaces context references with actual content.
func (d *Dispatcher) resolveAnaphora(input string, memCtx *MemoryContext) string {
	if memCtx == nil || memCtx.LastIntent == nil {
		return input
	}

	lower := strings.ToLower(input)

	if strings.Contains(lower, "do the same") {
		lastSummary := memCtx.LastIntent.Summary
		forMatch := anaphoraForRegex
		if match := forMatch.FindStringSubmatch(lower); match != nil {
			return fmt.Sprintf("%s for %s", lastSummary, match[1])
		}
	}

	return input
}

// extractSummary extracts a brief summary from input.
func extractSummary(input string) string {
	// Take first sentence or first 100 chars
	if idx := strings.IndexAny(input, ".!?"); idx > 0 && idx < 100 {
		return input[:idx+1]
	}
	return truncateString(input, 100)
}

// recordClassificationMethod records which method classified the intent.
func (d *Dispatcher) recordClassificationMethod(method string) {
	if d.stats == nil {
		return
	}
	d.stats.mu.Lock()
	defer d.stats.mu.Unlock()
	if d.stats.ByMethod == nil {
		d.stats.ByMethod = make(map[string]int)
	}
	d.stats.ByMethod[method]++
}

// recordAgent records which agent handled the request.
func (d *Dispatcher) recordAgent(agentID string) {
	if d.stats == nil {
		return
	}
	d.stats.mu.Lock()
	defer d.stats.mu.Unlock()
	if d.stats.ByAgent == nil {
		d.stats.ByAgent = make(map[string]int)
	}
	d.stats.ByAgent[agentID]++
}

// recordIntentType records the intent type.
func (d *Dispatcher) recordIntentType(intentType string) {
	if d.stats == nil {
		return
	}
	d.stats.mu.Lock()
	defer d.stats.mu.Unlock()
	if d.stats.ByIntent == nil {
		d.stats.ByIntent = make(map[string]int)
	}
	d.stats.ByIntent[intentType]++
}

// recordCompoundDispatch records a compound dispatch with all relevant stats.
func (d *Dispatcher) recordCompoundDispatch(_ int) {
	d.recordClassificationMethod("compound")
	d.recordAgent("orchestrator")
	d.recordIntentType(string(IntentCompound))
}

// recordFallback records a fallback to chat agent with details.
func (d *Dispatcher) recordFallback(input, method string, confidence float64, routedTo string) {
	if d.stats == nil {
		return
	}
	d.stats.mu.Lock()
	defer d.stats.mu.Unlock()
	d.stats.FallbackCount++
	d.stats.FallbackDetails = append(d.stats.FallbackDetails, FallbackEntry{
		Timestamp:  time.Now().UTC(),
		Input:      truncateString(input, 200),
		Method:     method,
		Confidence: confidence,
		RoutedTo:   routedTo,
	})
	// Keep only last 100 fallbacks
	if len(d.stats.FallbackDetails) > 100 {
		d.stats.FallbackDetails = d.stats.FallbackDetails[len(d.stats.FallbackDetails)-100:]
	}
}

// recordTotalDispatch increments the total dispatch counter.
func (d *Dispatcher) recordTotalDispatch() {
	if d.stats == nil {
		return
	}
	d.stats.mu.Lock()
	defer d.stats.mu.Unlock()
	d.stats.TotalDispatched++
}

// GetStats returns a copy of dispatcher statistics.
func (d *Dispatcher) GetStats() DispatcherStats {
	if d.stats == nil {
		return DispatcherStats{}
	}
	d.stats.mu.RLock()
	defer d.stats.mu.RUnlock()
	fallbackDetails := make([]FallbackEntry, len(d.stats.FallbackDetails))
	copy(fallbackDetails, d.stats.FallbackDetails)
	return DispatcherStats{
		TotalDispatched: d.stats.TotalDispatched,
		ByMethod:        d.stats.ByMethod,
		ByAgent:         d.stats.ByAgent,
		ByIntent:        d.stats.ByIntent,
		FallbackCount:   d.stats.FallbackCount,
		FallbackDetails: fallbackDetails,
	}
}

// GetFallbackDetails returns recent fallback entries for analysis.
func (d *Dispatcher) GetFallbackDetails(limit int) []FallbackEntry {
	if d.stats == nil {
		return nil
	}
	d.stats.mu.RLock()
	defer d.stats.mu.RUnlock()
	if limit <= 0 || limit > len(d.stats.FallbackDetails) {
		limit = len(d.stats.FallbackDetails)
	}
	if limit == 0 {
		return nil
	}
	return d.stats.FallbackDetails[len(d.stats.FallbackDetails)-limit:]
}

// DispatcherStats returns statistics about the dispatcher.
type DispatcherStats struct {
	mu              sync.RWMutex
	TotalDispatched int             `json:"total_dispatched"`
	ByMethod        map[string]int  `json:"by_method"`
	ByAgent         map[string]int  `json:"by_agent"`
	ByIntent        map[string]int  `json:"by_intent"`
	FallbackCount   int             `json:"fallback_count"`
	FallbackDetails []FallbackEntry `json:"fallback_details,omitempty"`
}

// FallbackEntry captures details about a fallback routing decision.
type FallbackEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	Input      string    `json:"input"`
	Method     string    `json:"method"`
	Confidence float64   `json:"confidence"`
	RoutedTo   string    `json:"routed_to"`
}

// MarshalJSON implements json.Marshaler for Intent.
func (i *Intent) MarshalJSON() ([]byte, error) {
	type Alias Intent
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(i),
	})
}

// parseSkillInvocation extracts skill name and input from a /skill-name invocation.
func (d *Dispatcher) parseSkillInvocation(input string) (skillName, skillInput string) {
	// Remove leading slash
	input = strings.TrimPrefix(input, "/")

	// Split on first whitespace
	parts := strings.SplitN(input, " ", 2)
	skillName = parts[0]
	skillInput = ""
	if len(parts) > 1 {
		skillInput = strings.TrimSpace(parts[1])
	}

	return skillName, skillInput
}

// getSkill retrieves a skill by name from the registry.
func (d *Dispatcher) getSkill(name string) *skills.Skill {
	if d.skillRegistry == nil {
		return nil
	}
	return d.skillRegistry.Get(name)
}

// substituteTemplate substitutes a template body with arguments parsed from
// the raw skill input string. The input is split on whitespace to produce
// positional arguments for the template substitution engine.
func (d *Dispatcher) substituteTemplate(tmpl *templates.Template, input string) string {
	var args []string
	if input != "" {
		args = strings.Fields(input)
	}
	return templates.Substitute(tmpl.Body, args)
}

// executeSkill executes a skill and returns a dispatch result.
func (d *Dispatcher) executeSkill(ctx context.Context, skill *skills.Skill, input, _ string) (*DispatchResult, error) {
	if d.skillExecutor == nil {
		return nil, fmt.Errorf("skill executor not configured")
	}

	// Execute the skill
	result, err := d.skillExecutor.Execute(ctx, skill, input)
	if err != nil {
		d.logger.Error("Skill execution failed",
			"skill", skill.Name,
			"error", err,
		)
		return nil, fmt.Errorf("skill execution failed: %w", err)
	}

	// Build dispatch result with skill response
	intent := &Intent{
		Type:       string(IntentSkill),
		Confidence: 1.0,
		AgentType:  "skill:" + skill.Name,
		Summary:    fmt.Sprintf("Executed skill: %s", skill.Name),
	}

	return &DispatchResult{
		AgentID:  "skill:" + skill.Name,
		Intent:   intent,
		Response: result.Content,
	}, nil
}

// ShouldDispatchAsync returns true if the dispatch result should be handled
// asynchronously via the orchestrator pipeline rather than inline.
func (d *Dispatcher) ShouldDispatchAsync(result *DispatchResult) bool {
	if result == nil || result.Intent == nil {
		return false
	}

	// Skills are always handled inline
	if result.Response != "" {
		return false
	}

	it := IntentType(result.Intent.Type)
	if it.ShouldDispatchAsync(result.Intent.RequiresPlanning) {
		return true
	}
	// Fallback for unknown intents with RequiresPlanning flag
	return result.Intent.RequiresPlanning
}

// RoutingValidation checks if a task was routed correctly.
type RoutingValidation struct {
	TaskID         string `json:"task_id"`
	OriginalIntent string `json:"original_intent"`
	RoutedAgent    string `json:"routed_agent"`
	IsValid        bool   `json:"is_valid"`
	ExpectedAgent  string `json:"expected_agent,omitempty"`
	Feedback       string `json:"feedback,omitempty"`
}

// ValidateRouting compares the routed agent against expected.
func (d *Dispatcher) ValidateRouting(taskID, originalIntent, routedAgent string) *RoutingValidation {
	it := IntentType(originalIntent)

	if !IsValidIntentType(originalIntent) {
		return &RoutingValidation{
			TaskID:   taskID,
			Feedback: fmt.Sprintf("Unknown intent type: %s", originalIntent),
		}
	}

	expectedAgent := it.DefaultAgent()
	isValid := routedAgent == expectedAgent

	// Special case: chat agent can handle inline intents
	if routedAgent == config.AgentIDChat && it.Category() == CategoryInline {
		isValid = true
	}

	feedback := "Correct routing"
	if !isValid {
		feedback = fmt.Sprintf("Expected agent '%s' for intent '%s'", expectedAgent, originalIntent)
	}

	return &RoutingValidation{
		TaskID:         taskID,
		OriginalIntent: originalIntent,
		RoutedAgent:    routedAgent,
		IsValid:        isValid,
		ExpectedAgent:  expectedAgent,
		Feedback:       feedback,
	}
}

// GetSkillRegistry returns the skill registry for external access.
func (d *Dispatcher) GetSkillRegistry() *skills.Registry {
	return d.skillRegistry
}

// GetSkillExecutor returns the skill executor for external access.
func (d *Dispatcher) GetSkillExecutor() *skills.Executor {
	return d.skillExecutor
}

// GetCapabilityMatcher returns the capability matcher for external access.
func (d *Dispatcher) GetCapabilityMatcher() *CapabilityMatcher {
	return d.capabilityMatcher
}

// SetCapabilityMatcher sets the capability matcher for fast routing.
func (d *Dispatcher) SetCapabilityMatcher(matcher *CapabilityMatcher) {
	d.capabilityMatcher = matcher
}

// GetActiveTasks returns all active tasks from the task store.
func (d *Dispatcher) GetActiveTasks(ctx context.Context) ([]*task.Task, error) {
	if d.taskStore == nil {
		return nil, fmt.Errorf("task store not configured")
	}
	return d.taskStore.ListActive()
}

// GetInterruptStatus returns the interrupt status for a task.
// Returns (isInterrupted, reason, message) or an error if the task is not found.
func (d *Dispatcher) GetInterruptStatus(ctx context.Context, taskID string) (ok bool, reason, message string, err error) {
	if d.taskStore == nil {
		return false, "", "", fmt.Errorf("task store not configured")
	}

	t, err := d.taskStore.GetByID(taskID)
	if err != nil {
		return false, "", "", err
	}
	if t == nil {
		return false, "", "", fmt.Errorf("task not found: %s", taskID)
	}

	// Check task state first
	if t.State == task.StateCancelled {
		return true, string(task.ReasonUserCancelled), "Task was cancelled", nil
	}

	return false, "", "", nil
}

// SubmitAmendment submits an amendment request for a task.
func (d *Dispatcher) SubmitAmendment(ctx context.Context, taskID string, amendmentType task.AmendmentType, content string, metadata map[string]any) (*task.AmendmentRequest, error) {
	if d.taskStore == nil {
		return nil, fmt.Errorf("task store not configured")
	}
	if d.amendmentMgr == nil {
		return nil, fmt.Errorf("amendment manager not configured")
	}

	// Verify task exists
	t, err := d.taskStore.GetByID(taskID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}

	// Marshal metadata if provided
	var metadataJSON json.RawMessage
	if metadata != nil {
		metadataJSON, err = json.Marshal(metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal metadata: %w", err)
		}
	}

	// Create amendment request
	req := task.NewAmendmentRequest(taskID, amendmentType, content)
	req.Metadata = metadataJSON

	// Submit through amendment manager
	if err := d.amendmentMgr.Submit(ctx, req); err != nil {
		return nil, err
	}

	return req, nil
}

// ProcessAmendment processes a pending amendment request.
func (d *Dispatcher) ProcessAmendment(ctx context.Context, requestID string) (*task.AmendmentReply, error) {
	if d.amendmentMgr == nil {
		return nil, fmt.Errorf("amendment manager not configured")
	}
	return d.amendmentMgr.Process(ctx, requestID)
}

// GetPendingAmendments returns all pending amendments for a task.
func (d *Dispatcher) GetPendingAmendments(taskID string) []*task.AmendmentRequest {
	if d.amendmentMgr == nil {
		return nil
	}
	return d.amendmentMgr.GetPendingForTask(taskID)
}

// GetTask returns a task by ID.
func (d *Dispatcher) GetTask(ctx context.Context, taskID string) (*task.Task, error) {
	if d.taskStore == nil {
		return nil, fmt.Errorf("task store not configured")
	}
	return d.taskStore.GetByID(taskID)
}

// SteerActiveAgent sends a steering message to an active agent for the given conversation.
// This interrupts the current flow. Returns ErrQueueNotFound if no active queue exists.
func (d *Dispatcher) SteerActiveAgent(ctx context.Context, conversationID, content, source string) error {
	if d.registry == nil {
		return ErrQueueNotFound
	}
	queue, _ := d.registry.GetActiveQueue(conversationID)
	if queue == nil || queue.IsClosed() {
		return ErrQueueNotFound
	}
	return queue.Steer(ctx, content, source)
}

// FollowUpActiveAgent sends a follow-up message to an active agent for the given conversation.
// This waits for the agent to reach a natural stopping point. Returns ErrQueueNotFound
// if no active queue exists.
func (d *Dispatcher) FollowUpActiveAgent(ctx context.Context, conversationID, content, source string) error {
	if d.registry == nil {
		return ErrQueueNotFound
	}
	queue, _ := d.registry.GetActiveQueue(conversationID)
	if queue == nil || queue.IsClosed() {
		return ErrQueueNotFound
	}
	return queue.FollowUp(ctx, content, source)
}
