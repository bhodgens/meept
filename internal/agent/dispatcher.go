package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/memvid"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/plan"
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
	IntentChat:        false, // General chat can wait
	IntentRecall:      false, // Memory recall is not urgent
	IntentResearch:    false, // Research extensions follow naturally
	IntentReport:      false, // Reporting status/information
	IntentPlatform:    false, // Platform events are informational
	IntentStatus:      false, // Status inquiries
	IntentReview:      false, // Review requests build on completion
	IntentSchedule:    false, // Scheduling is not urgent
	IntentAnalyze:     false, // Analysis extends naturally
	IntentSearch:      false, // Search queries are not urgent
	IntentSkill:       false, // Skill operations can wait
	IntentPair:        false, // Pair tasks are not urgent
	IntentCollaborate: false, // Collaboration tasks are not urgent
	IntentCompound:    false, // Compound intents default to follow-up
	IntentUnknown:     false,
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
	// TrueAnalysis holds the IntentGate-style pre-classification analysis if available.
	TrueAnalysis *TrueIntentAnalysis `json:"true_analysis,omitempty"`
}

// MemoryContext wraps memory results with conversation metadata.
type MemoryContext struct {
	Results      []memory.MemoryResult `json:"results"`
	LastIntent   *Intent               `json:"last_intent,omitempty"`
	LastAgent    string                `json:"last_agent,omitempty"`
	IntentCounts map[string]int        `json:"intent_counts,omitempty"`
}

// ModelReassignmentDirective captures a user's model reassignment instruction.
type ModelReassignmentDirective struct {
	// Instruction is the raw user instruction text (e.g., "use GLM models for coding")
	Instruction string `json:"instruction"`

	// TargetScope - which intent type this applies to
	// Examples: "synthesis"→IntentPlan, "coding"→IntentCode, "research"→IntentResearch
	TargetScope string `json:"target_scope,omitempty"`

	// TargetIntent - resolved intent type from scope keyword
	TargetIntent *IntentType `json:"target_intent,omitempty"`

	// ModelReferences - one or more model specs from user input
	// Can be: "zai/glm-4.7", "glm-*", "provider:zai", "opus"
	ModelReferences []string `json:"model_references"`

	// ResolvedModels - after resolver processes references
	ResolvedModels []*llm.ModelConfig `json:"resolved_models,omitempty"`

	// ClarificationNeeded - set true if instruction is ambiguous
	ClarificationNeeded bool `json:"clarification_needed,omitempty"`

	// ClarificationQuestions - questions to ask user if ambiguous
	ClarificationQuestions []string `json:"clarification_questions,omitempty"`
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

	// OriginalInput is the full, untruncated user input. Preserved so that
	// agents receive the complete message rather than a ~100-char summary.
	OriginalInput string `json:"original_input,omitempty"`

	// ModelDirective is the model reassignment directive if user specified one
	ModelDirective *ModelReassignmentDirective `json:"model_directive,omitempty"`
	// ClarificationReply is the clarification question if directive is ambiguous
	ClarificationReply string `json:"clarification_reply,omitempty"`
	// ClarificationNeeded indicates the directive needs user clarification
	ClarificationNeeded bool `json:"clarification_needed,omitempty"`
	// Plan is the created plan if plan routing was triggered.
	Plan *plan.Plan `json:"plan,omitempty"`
	// ClassificationNotice is a user-facing notice about classification degradation
	// (e.g., LLM classifier failed and fallback was used). Empty when classification
	// succeeded normally.
	ClassificationNotice string `json:"classification_notice,omitempty"`

	// Parts carries multimodal content parts (e.g. image attachments) from the
	// original request through the dispatcher so that RouteToAgent can forward
	// them to the specialist agent's RunOnceWithParts. Text-only requests leave
	// this nil, preserving the existing RunOnce path.
	Parts []llm.ContentPart `json:"-"`

	// SuggestedReasoningTier is populated by the intent-classifier hook per
	// LLM Reasoning Effort spec §7.5. It is ONLY set when (a) no explicit
	// user directive was parsed AND (b) the intent type has a defined
	// mapping in suggestReasoningForIntent. Consumers should treat an empty
	// value as "no suggestion". The agent's own AllowSelfModulation /
	// MinEffort / MaxEffort bounds gate whether the suggestion is actually
	// applied at the AgentLoop layer.
	SuggestedReasoningTier string `json:"-"`

	// ReasoningOverride carries the parsed user reasoning directive (if any)
	// so downstream code can forward it to the agent loop. When non-nil, it
	// takes precedence over SuggestedReasoningTier per spec §7.5. Tagged
	// json:"-" because it is operational metadata not meant for
	// user-facing JSON serialization.
	ReasoningOverride *llm.ReasoningConfig `json:"-"`
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
	intentAnalyzer    *IntentAnalyzer
	keywordClassifier *KeywordClassifier
	capabilityMatcher *CapabilityMatcher
	semanticIndex     *SemanticIndex
	sessionTracker    *SessionTracker
	stats             *DispatcherStats
	router            *ReportRouter
	modelParser       *ModelReassignmentParser
	planManager       *plan.PlanManager
	metricsStore      *metrics.Store

	// threadRouter handles thread-aware routing of conversations per
	// Thread-Based Context Partitioning spec. When nil, the dispatcher
	// operates in legacy mode (single conversation per session). Wired via
	// SetThreadRouter by daemon composition; not exposed in
	// DispatcherConfig to avoid forcing all callers to construct a
	// ThreadRouter.
	threadRouter *ThreadRouter

	// lastClassifierMethod tracks the most recent classifier that succeeded,
	// for audit logging. Updated by recordClassificationMethod under stats.mu.
	lastClassifierMethod string

	// Lifecycle management for background goroutines spawned by NewDispatcher.
	// indexCtx is cancelled by Stop(); indexWG tracks the BuildIndex goroutine
	// so Stop can confirm it has exited before returning.
	indexCtx    context.Context
	indexCancel context.CancelFunc
	indexWG     sync.WaitGroup
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
	PlanManager       *plan.PlanManager
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
		planManager:       cfg.PlanManager,
	}

	// Initialize model reassignment parser
	d.modelParser = NewModelReassignmentParser()

	// Add keyword-based classifier
	d.keywordClassifier = &KeywordClassifier{}

	// Add LLM-based classifier if a client is provided.
	// Prefer the dedicated ClassifierClient; fall back to the main LLMClient.
	classifierClient := cfg.ClassifierClient
	if classifierClient == nil {
		classifierClient = cfg.LLMClient
	}
	if classifierClient != nil {
		d.llmClassifier = NewLLMClassifier(
			LLMClassifierConfig{
				Client:  classifierClient,
				Model:   cfg.ClassifierModel,
				Timeout: cfg.ClassifierTimeout,
			},
			cfg.Logger,
		)
		// Initialize intent analyzer using same classifier client
		d.intentAnalyzer = NewIntentAnalyzer(classifierClient, cfg.Logger)
	}

	// Initialize semantic index if embedding client is provided
	if cfg.EmbeddingClient != nil {
		d.semanticIndex = NewSemanticIndex(cfg.EmbeddingClient)
		// Tie the background BuildIndex goroutine to a cancellable context so
		// Stop() can interrupt it at shutdown; track via WaitGroup so callers
		// can confirm exit.
		d.indexCtx, d.indexCancel = context.WithCancel(context.Background())
		d.indexWG.Add(1)
		go func() { //nolint:gosec // background goroutine outlives request context
			defer d.indexWG.Done()
			if err := d.semanticIndex.BuildIndex(d.indexCtx); err != nil {
				if d.indexCtx.Err() == nil {
					d.logger.Warn("Failed to build semantic index", "error", err)
				}
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

// Stop gracefully shuts down background goroutines spawned by NewDispatcher
// (currently the semantic-index BuildIndex goroutine). It is safe to call
// multiple times: a second call is a no-op once indexCancel has fired.
//
// Callers that construct a Dispatcher with an EmbeddingClient should defer
// Stop() so the BuildIndex goroutine does not outlive the dispatcher in
// tests or short-lived processes. Long-lived daemons can rely on process
// exit, but wiring Stop into the daemon shutdown path is recommended.
//
// TODO: wire Stop() into Components.Stop() in internal/daemon/components.go
// for a fully clean shutdown. Today the dispatcher lives for the daemon
// lifetime so the leak is benign, but adding the call closes the gap.
func (d *Dispatcher) Stop() {
	if d.indexCancel != nil {
		d.indexCancel()
	}
	d.indexWG.Wait()
}

// SetThreadRouter wires a ThreadRouter onto the dispatcher, enabling
// thread-aware routing. Pass nil to disable thread routing (legacy mode).
// Nil guard at top of setter prevents typed-nil interface panics per
// CLAUDE.md Setter methods rule.
func (d *Dispatcher) SetThreadRouter(tr *ThreadRouter) {
	if tr == nil {
		return
	}
	d.threadRouter = tr
}

// suggestReasoningForIntent returns the suggested reasoning tier for a given
// intent type per LLM Reasoning Effort spec §7.5. Returns empty string when
// the intent has no defined mapping (meaning "no suggestion"), so callers
// can distinguish "no suggestion" from a valid tier value.
//
// The suggestion is ONLY applied when:
//   - no explicit user reasoning directive was parsed, AND
//   - the agent's AllowSelfModulation flag is true (enforced at AgentLoop).
//
// Callers should stash the return value on DispatchResult.SuggestedReasoningTier
// for downstream consumers.
func suggestReasoningForIntent(intentType string) string {
	switch IntentType(intentType) {
	case IntentPlan:
		return llm.ReasoningXHigh
	case IntentDebug, IntentResearch, IntentAnalyze:
		return llm.ReasoningHigh
	case IntentCode:
		return llm.ReasoningMedium
	case IntentChat:
		return llm.ReasoningLow
	default:
		return ""
	}
}

// ClassifyAndRoute is the main entry point for the dispatcher.
//
// parts carries optional multimodal content (e.g. image attachments). When
// non-empty, the parts are attached to the returned DispatchResult so that
// RouteToAgent can forward them to the specialist agent's RunOnceWithParts.
// Text-only callers may pass nil.
func (d *Dispatcher) ClassifyAndRoute(ctx context.Context, input, sessionID string, parts []llm.ContentPart) (*DispatchResult, error) {
	d.logger.Debug("Dispatching request",
		"session", sessionID,
		"input_len", len(input),
		"parts_count", len(parts),
	)

	// Check for clarification follow-up: if the last intent for this session
	// was IntentClarify, treat the current input as the user's response to
	// the clarification questions.
	if d.isPendingClarification(sessionID) && !strings.HasPrefix(input, "/") {
		pending := d.getPendingClarification(sessionID)
		if pending != nil {
			d.logger.Info("Detected clarification follow-up, resuming classification",
				"session", sessionID,
			)
			return d.ResumeAfterClarification(ctx, pending.OriginalInput, input, sessionID)
		}
	}

	// Check for explicit skill invocation (/skill-name)
	if strings.HasPrefix(input, "/") {
		// Handle /plan command — force plan creation
		if strings.HasPrefix(input, "/plan") && d.planManager != nil {
			desc := strings.TrimPrefix(input, "/plan")
			desc = strings.TrimSpace(desc)
			if desc == "" {
				desc = input
			}
			// Build a minimal intent for plan routing
			planIntent := &Intent{
				Type:       string(IntentPlan),
				Confidence: 1.0,
				AgentType:  config.AgentIDPlanner,
				Summary:    extractSummary(desc),
			}
			return d.routeToPlan(ctx, desc, planIntent, sessionID)
		}
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

	// 1. Parse model reassignment directive (if user specified model preferences)
	parseResult := d.modelParser.Parse(input)

	// 2. Handle clarification if needed
	if parseResult.Found && parseResult.Directive.ClarificationNeeded {
		// Build intent for session tracking (so follow-up inputs are
		// recognized as clarification responses)
		intent := &Intent{
			Type:         string(IntentClarify),
			Confidence:   1.0,
			AgentType:    config.AgentIDChat,
			Summary:      extractSummary(input),
			TrueAnalysis: nil, // Model directive clarification, not intent analysis
		}
		// Record intent for pending clarification detection
		if d.sessionTracker != nil {
			d.sessionTracker.RecordIntent(sessionID, intent, intent.AgentType)
		}
		return &DispatchResult{
			ModelDirective:      parseResult.Directive,
			Intent:              intent,
			ClarificationReply:  d.buildClarificationQuestion(parseResult.Directive),
			ClarificationNeeded: true,
			AgentID:             config.AgentIDChat,
		}, nil
	}

	// 3. Build memory context with session history
	memCtx := d.buildMemoryContext(ctx, input, sessionID)

	// 3.5. IntentGate-style true intent analysis
	if d.intentAnalyzer != nil {
		analysis, err := d.intentAnalyzer.AnalyzeTrueIntent(ctx, input)
		if err == nil && analysis != nil {
			if analysis.IsAmbiguous(d.intentAnalyzer.ambiguityThreshold) {
				return d.buildClarificationResult(input, analysis, sessionID)
			}
			// Store analysis for downstream use (e.g. planner interview mode)
			memCtx.LastIntent = &Intent{
				Type:         analysis.Category,
				Confidence:   analysis.Confidence,
				AgentType:    analysis.Category,
				Summary:      analysis.Goal,
				TrueAnalysis: analysis,
			}
		}
	}

	// 4. Resolve anaphora (context references)
	resolvedInput := d.resolveAnaphora(input, memCtx)

	// 3. Check for compound (multi-intent) requests
	multiIntent := d.classifyMultiIntent(ctx, resolvedInput, memCtx)
	if multiIntent.IsCompound {
		return d.routeCompoundWithModel(ctx, multiIntent, input, sessionID, parseResult.Directive)
	}

	// 4. Classify primary intent
	intent, _ := d.classifyIntent(ctx, resolvedInput, memCtx)

	// 5. Extract memory refs for context continuity
	intent.MemoryRefs = d.extractMemoryRefs(memCtx.Results)

	// 5.5. Check if plan creation is warranted (before task creation)
	if d.planManager != nil && d.planManager.ShouldCreatePlan(intent.Type, 0) {
		return d.routeToPlan(ctx, input, intent, sessionID)
	}

	// 6. Create task if needed (for trackable work)
	var createdTask *task.Task
	if d.shouldCreateTask(intent) && d.taskStore != nil {
		createdTask = d.createTask(ctx, input, intent, sessionID)
	}

	// 7. Determine routing
	result := &DispatchResult{
		Task:           createdTask,
		AgentID:        intent.AgentType,
		Intent:         intent,
		MemoryContext:  memCtx.Results,
		ModelDirective: parseResult.Directive,
		OriginalInput:  input,
		Parts:          parts,
	}

	// Attach model override to task metadata if task was created
	if createdTask != nil && parseResult.Found && parseResult.Directive != nil {
		// Store model override in task metadata for AgentLoop to use
		modelRef := ""
		if len(parseResult.Directive.ResolvedModels) > 0 {
			mc := parseResult.Directive.ResolvedModels[0]
			modelRef = fmt.Sprintf("%s/%s", mc.ProviderID, mc.ModelID)
		} else if len(parseResult.Directive.ModelReferences) > 0 {
			modelRef = parseResult.Directive.ModelReferences[0]
		}

		if modelRef != "" {
			meta := map[string]any{
				"model_override":      modelRef,
				"model_scope":         parseResult.Directive.TargetScope,
				"model_target_intent": "",
			}
			if parseResult.Directive.TargetIntent != nil {
				meta["model_target_intent"] = string(*parseResult.Directive.TargetIntent)
			}

			metaJSON, err := json.Marshal(meta)
			if err == nil {
				// Merge with existing metadata
				if len(createdTask.Metadata) > 0 {
					var existing map[string]any
					if json.Unmarshal(createdTask.Metadata, &existing) == nil {
						maps.Copy(existing, meta)
						metaJSON, _ = json.Marshal(existing)
					}
				}
				createdTask.Metadata = json.RawMessage(metaJSON)
				if d.taskStore != nil {
					if err := d.taskStore.Update(createdTask); err != nil {
						d.logger.Warn("Failed to update task model metadata", "error", err)
					}
				}
			}
		}
	}

	d.logger.Info("Dispatched request",
		"agent", intent.AgentType,
		"intent_type", intent.Type,
		"confidence", intent.Confidence,
		"memory_refs", len(intent.MemoryRefs),
		"has_task", createdTask != nil,
		"has_model_override", parseResult.Found,
	)

	// Record intent in session tracker
	d.sessionTracker.RecordIntent(sessionID, intent, intent.AgentType)

	return result, nil
}

// 1. Short-message guard: brief inputs route directly to chat (Issues 0006, 0029, 0036)
// 2. Try capability matcher (fast, no LLM) if available and confident
// 3. Try LLM classifier (if available)
// 4. If LLM fails OR confidence < threshold → try Keyword classifier
// 5. If Keyword fails AND no strong keyword signal → improved heuristic fallback (Issue 0036)
// 6. Final fallback to Chat for clarification
func (d *Dispatcher) classifyIntent(ctx context.Context, input string, memCtx *MemoryContext) (*Intent, error) {
	d.recordTotalDispatch()
	// --- Guard: short/simple messages skip the full classifier chain ---
	// Tiny models over-classify simple greetings, arithmetic, and short phrases
	// as compound multi-agent tasks. Short inputs are overwhelmingly chat.
	if isShortSimpleMessage(input) {
		d.logger.Debug("Short/simple message guard: routing to chat",
			"input", input,
			"length", len(input),
		)
		d.recordClassificationMethod("short_message_guard")
		d.recordAgent(config.AgentIDChat)
		d.recordIntentType(string(IntentChat))
		return &Intent{
			Type:       string(IntentChat),
			Confidence: 0.9,
			AgentType:  config.AgentIDChat,
			Summary:    extractSummary(input),
		}, nil
	}

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
			return d.applyContextWeighting(intent, memCtx, input), nil
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
				return d.applyContextWeighting(intent, memCtx, input), nil
			}
			d.logger.Debug("LLM classifier result below threshold",
				"intent", intent.Type,
				"confidence", intent.Confidence,
				"threshold", GetThresholdForIntent(intent.Type),
			)
		} else if err != nil {
			kind := llm.ClassifyClassificationFailure(err)
			d.logger.Warn("LLM classifier failed, trying keyword",
				"error", err,
				"failure_kind", kind,
			)
		}
	}

	// Step 3: Try Keyword classifier (with minimum confidence threshold)
	if d.keywordClassifier != nil {
		intent, err := d.keywordClassifier.Classify(ctx, input, memCtx)
		if err == nil && intent != nil && intent.Confidence >= 0.3 {
			d.logger.Debug("Keyword classifier succeeded",
				"intent", intent.Type,
				"confidence", intent.Confidence,
			)
			d.recordClassificationMethod("keyword")
			d.recordAgent(intent.AgentType)
			d.recordIntentType(intent.Type)
			return d.applyContextWeighting(intent, memCtx, input), nil
		}
		if intent != nil {
			d.logger.Debug("Keyword classifier result below threshold",
				"intent", intent.Type,
				"confidence", intent.Confidence,
				"threshold", 0.3,
			)
		}
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
			return d.applyContextWeighting(intent, memCtx, input), nil
		}
	}

	// Step 4: Improved heuristic fallback (Issue 0036)
	// Use targeted keyword rules with proper agent routing, avoiding the
	// previous behavior where code tasks were routed to scheduler/committer.
	if heuristic := heuristicFallback(input); heuristic != nil {
		d.logger.Debug("Heuristic fallback succeeded",
			"intent", heuristic.Type,
			"confidence", heuristic.Confidence,
		)
		d.recordClassificationMethod("heuristic_fallback")
		d.recordAgent(heuristic.AgentType)
		d.recordIntentType(heuristic.Type)
		return d.applyContextWeighting(heuristic, memCtx, input), nil
	}

	// Step 5: Final fallback to Chat for clarification
	d.recordFallback(input, "all_classifiers_failed", 0.0, config.AgentIDChat)
	d.recordClassificationMethod("fallback")
	d.recordAgent(config.AgentIDChat)
	d.recordIntentType(string(IntentChat))
	return &Intent{
		Type:       string(IntentChat),
		Confidence: 0.3,
		AgentType:  config.AgentIDChat,
		Summary:    "Could not determine intent, clarifying with user",
	}, nil

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

// routeToPlan creates a plan via the PlanManager and returns a DispatchResult
// with the created plan. This is used for plan-eligible requests that should
// go through the planning workflow instead of direct task creation.
func (d *Dispatcher) routeToPlan(ctx context.Context, input string, intent *Intent, sessionID string) (*DispatchResult, error) {
	summary := intent.Summary
	if summary == "" {
		summary = truncateString(input, 100)
	}

	p, err := d.planManager.CreatePlan(ctx, summary, input, "", "", sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to create plan: %w", err)
	}

	d.logger.Info("Routed request to plan",
		"plan_id", p.ID,
		"title", p.Title,
		"state", p.State,
		"session", sessionID,
	)

	return &DispatchResult{
		AgentID:  config.AgentIDPlanner,
		Intent:   intent,
		Response: fmt.Sprintf("plan created: %s (status: %s)", p.Title, p.State),
		Plan:     p,
	}, nil
}

// buildClarificationResult creates a DispatchResult for ambiguous input
// that asks clarifying questions before proceeding with routing.
func (d *Dispatcher) buildClarificationResult(input string, analysis *TrueIntentAnalysis, sessionID string) (*DispatchResult, error) {
	var questions []string
	if len(analysis.SuggestedQuestions) > 0 {
		questions = analysis.SuggestedQuestions
	} else {
		questions = []string{"Could you provide more details about what you'd like to do?"}
	}

	// Build a single clarifying message
	var sb strings.Builder
	sb.WriteString("I'm not quite sure what you're asking for. ")
	if analysis.Goal != "" {
		sb.WriteString(fmt.Sprintf("It seems like you want to %s, but ", analysis.Goal))
	}
	sb.WriteString("I need a bit more clarity:\n\n")
	for i, q := range questions {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, q))
	}

	intent := &Intent{
		Type:         string(IntentClarify),
		Confidence:   analysis.Confidence,
		AgentType:    config.AgentIDChat,
		Summary:      extractSummary(input),
		TrueAnalysis: analysis,
	}

	// Record for analytics
	d.recordClassificationMethod("intent_analyzer")
	d.recordAgent(config.AgentIDChat)
	d.recordIntentType(string(IntentClarify))

	d.logger.Info("Requesting clarification",
		"ambiguity", analysis.Ambiguity,
		"category", analysis.Category,
		"questions", len(questions),
	)

	return &DispatchResult{
		AgentID:            config.AgentIDChat,
		Intent:             intent,
		ClarificationReply: sb.String(),
		ClarificationNeeded: true,
	}, nil
}

// pendingClarification holds the state needed to resume after a clarification.
type pendingClarification struct {
	OriginalInput string              `json:"original_input"`
	Analysis      *TrueIntentAnalysis `json:"analysis"`
	SessionID     string              `json:"session_id"`
}

// ResumeAfterClarification re-classifies a user input that is a response to a
// previous clarification request. It combines the original input with the user's
// response and re-runs intent analysis. If the combined input is still ambiguous,
// it asks follow-up questions. Otherwise, it routes normally.
func (d *Dispatcher) ResumeAfterClarification(ctx context.Context, originalInput, userResponse, sessionID string) (*DispatchResult, error) {
	combinedInput := originalInput + "\n\nUser clarification: " + userResponse

	d.logger.Info("Resuming after clarification",
		"session", sessionID,
		"combined_len", len(combinedInput),
	)

	// Re-analyze the combined input with the intent analyzer.
	if d.intentAnalyzer != nil {
		analysis, err := d.intentAnalyzer.AnalyzeTrueIntent(ctx, combinedInput)
		if err == nil && analysis != nil {
			if analysis.IsAmbiguous(d.intentAnalyzer.ambiguityThreshold) {
				d.logger.Info("Still ambiguous after clarification, asking follow-up",
					"ambiguity", analysis.Ambiguity,
				)
				return d.buildClarificationResult(combinedInput, analysis, sessionID)
			}

			// Clear the pending clarification from the session.
			d.clearPendingClarification(sessionID)

			// Build memory context with the combined input.
			memCtx := d.buildMemoryContext(ctx, combinedInput, sessionID)

			// Propagate the analysis as the last intent.
			memCtx.LastIntent = &Intent{
				Type:         analysis.Category,
				Confidence:   analysis.Confidence,
				AgentType:    analysis.Category,
				Summary:      analysis.Goal,
				TrueAnalysis: analysis,
			}

			// Resolve anaphora.
			resolvedInput := d.resolveAnaphora(combinedInput, memCtx)

			// Classify and route normally.
			intent, _ := d.classifyIntent(ctx, resolvedInput, memCtx)
			intent.MemoryRefs = d.extractMemoryRefs(memCtx.Results)
			intent.TrueAnalysis = analysis

			// Check for plan routing.
			if d.planManager != nil && d.planManager.ShouldCreatePlan(intent.Type, 0) {
				return d.routeToPlan(ctx, combinedInput, intent, sessionID)
			}

			// Create task if needed.
			var createdTask *task.Task
			if d.shouldCreateTask(intent) && d.taskStore != nil {
				createdTask = d.createTask(ctx, combinedInput, intent, sessionID)
			}

			result := &DispatchResult{
				Task:          createdTask,
				AgentID:       intent.AgentType,
				Intent:        intent,
				MemoryContext: memCtx.Results,
				OriginalInput: combinedInput,
			}

			d.logger.Info("Clarification resolved, routed normally",
				"agent", intent.AgentType,
				"intent_type", intent.Type,
				"confidence", intent.Confidence,
			)

			// Record intent in session tracker.
			d.sessionTracker.RecordIntent(sessionID, intent, intent.AgentType)

			return result, nil
		}
	}

	// Fallback: if intent analysis is unavailable or fails, proceed with normal
	// classification of the combined input. Parts are not propagated through
	// the clarification flow — multimodal attachments only attach to the
	// original user turn.
	d.clearPendingClarification(sessionID)
	return d.ClassifyAndRoute(ctx, combinedInput, sessionID, nil)
}

// isPendingClarification checks if the previous intent for a session was a
// clarification request, indicating that the current user input is a response.
func (d *Dispatcher) isPendingClarification(sessionID string) bool {
	if d.sessionTracker == nil {
		return false
	}
	lastIntent := d.sessionTracker.GetLastIntent(sessionID)
	if lastIntent == nil {
		return false
	}
	return lastIntent.Type == string(IntentClarify)
}

// getPendingClarification retrieves stored clarification state from the session
// tracker. Returns nil if no pending clarification exists.
func (d *Dispatcher) getPendingClarification(sessionID string) *pendingClarification {
	if d.sessionTracker == nil {
		return nil
	}
	state := d.sessionTracker.GetSession(sessionID)
	if state == nil || state.TotalRequests == 0 || len(state.IntentHistory) == 0 {
		return nil
	}
	lastIntent := state.IntentHistory[len(state.IntentHistory)-1]
	if lastIntent == nil || lastIntent.Type != string(IntentClarify) {
		return nil
	}
	// Reconstruct the original input from the intent summary (best-effort).
	// TrueAnalysis may be nil for model directive clarifications.
	return &pendingClarification{
		OriginalInput: lastIntent.Summary,
		Analysis:      lastIntent.TrueAnalysis,
		SessionID:     sessionID,
	}
}

// clearPendingClarification removes the clarification state for a session.
// This is called when the clarification flow is resolved.
func (d *Dispatcher) clearPendingClarification(sessionID string) {
	// The clarification state is implicitly cleared by recording a new
	// non-clarify intent in the session tracker. No explicit clearing needed
	// since the next RecordIntent call in ClassifyAndRoute will overwrite
	// the last intent.
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
// Adds confidence and complexity guards (Issues 0006, 0029):
// - Requires at least 2 intents with confidence >= 0.5
// - Both must be non-chat intents to qualify as compound
func (m *MultiIntent) DetectCompound() bool {
	if len(m.Intents) < 2 {
		m.IsCompound = false
		return false
	}

	// Filter to high-confidence intents only
	var strongIntents []*Intent
	for _, intent := range m.Intents {
		if intent.Confidence >= 0.5 {
			strongIntents = append(strongIntents, intent)
		}
	}

	// Need at least 2 high-confidence intents (Issue 0029)
	if len(strongIntents) < 2 {
		m.IsCompound = false
		return false
	}

	// At least one must be non-chat (Issue 0029: "thanks, that's all for now"
	// matched chat + scheduler, but that's not truly compound)
	hasNonChat := false
	for _, intent := range strongIntents {
		if intent.Type != string(IntentChat) && intent.Type != string(IntentPlatform) {
			hasNonChat = true
			break
		}
	}
	if !hasNonChat {
		m.IsCompound = false
		return false
	}

	m.IsCompound = true
	for _, intent := range strongIntents {
		if intent.RequiresPlanning {
			m.CompoundType = "sequential"
			return true
		}
	}
	m.CompoundType = "parallel"
	return true
}

// routeCompoundWithModel handles compound routing with model override support.
func (d *Dispatcher) routeCompoundWithModel(ctx context.Context, multi *MultiIntent, input, sessionID string, modelDirective *ModelReassignmentDirective) (*DispatchResult, error) {
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

	// Build step summaries from compound intents with model overrides
	steps := make([]TaskStepSummary, 0, len(multi.Intents))
	for _, intent := range multi.Intents {
		step := TaskStepSummary{
			Description: intent.Summary,
			AgentID:     intent.AgentType,
		}

		// Attach model override if directive matches this step's intent
		if modelDirective != nil && modelDirective.TargetIntent != nil {
			intentType := IntentType(intent.Type)
			if *modelDirective.TargetIntent == intentType {
				// Use first resolved model or first reference
				if len(modelDirective.ResolvedModels) > 0 {
					mc := modelDirective.ResolvedModels[0]
					step.ModelOverride = fmt.Sprintf("%s/%s", mc.ProviderID, mc.ModelID)
				} else if len(modelDirective.ModelReferences) > 0 {
					step.ModelOverride = modelDirective.ModelReferences[0]
				}
				d.logger.Debug("Attached model override to step",
					"step", intent.Summary,
					"model", step.ModelOverride,
				)
			}
		}

		steps = append(steps, step)
	}

	return &DispatchResult{
		Task:    parentTask,
		AgentID: "orchestrator",
		Intent: &Intent{
			Type:    string(IntentCompound),
			Summary: multi.Summary,
		},
		OriginalInput: input,
		Steps:         steps,
	}, nil
}

// classifyMultiIntent runs classification to detect all potential intents.
// Adds complexity heuristics (Issue 0029): short messages without compound
// signal words are skipped to avoid false positive compound detection.
func (d *Dispatcher) classifyMultiIntent(ctx context.Context, input string, memCtx *MemoryContext) *MultiIntent {
	// Early exit: short messages without compound signal words should not
	// be considered compound tasks (Issue 0029).
	if !hasCompoundSignalWords(input) {
		return &MultiIntent{
			IsCompound: false,
			Summary:    extractSummary(input),
		}
	}

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
	if result == nil || result.Intent == nil {
		return "", fmt.Errorf("dispatch result has no intent to route")
	}

	// Consult the thread router before any other routing logic so that the
	// conversation ID is resolved to the active thread's conversation ID
	// (performing silent migration of legacy sessions if needed). When no
	// thread router is wired (legacy mode), this block is skipped and the
	// original conversationID is used as-is.
	if d.threadRouter != nil {
		resolved, err := d.threadRouter.GetThreadConversationID(ctx, conversationID, result.Intent.Summary)
		if err != nil {
			d.logger.Warn("thread router lookup failed, falling back to conversation ID",
				"conversation", conversationID,
				"error", err)
		} else if resolved != "" {
			conversationID = resolved
		}
	}

	if d.registry == nil {
		return "", fmt.Errorf("no agent registry configured")
	}

	// Handle platform introspection directly without LLM
	if result.Intent.Type == string(IntentPlatform) {
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

	// Run the agent. When the dispatcher is carrying multimodal parts
	// (e.g. image attachments), route them through RunOnceWithParts so the
	// provider serializer emits native image blocks. Otherwise use the
	// plain RunOnce path — the two are equivalent for text-only turns.
	var response string
	if len(result.Parts) > 0 {
		response, err = agent.RunOnceWithParts(ctx, contextMsg, result.Parts, conversationID)
	} else {
		response, err = agent.RunOnce(ctx, contextMsg, conversationID)
	}
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
			AgentID:       nextAgentID,
			Intent:        result.Intent,
			OriginalInput: result.OriginalInput,
			// Preserve multimodal parts for the next hop so attachments are
			// not silently dropped during report-router handoffs.
			Parts: result.Parts,
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
			desc := extractBriefDescription(spec.Purpose)
			if desc == "" {
				desc = truncateString(spec.Purpose, 100)
			}
			fmt.Fprintf(&sb, "- **%s** (`%s`): %s\n", spec.Name, spec.ID, truncateString(desc, 120))
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
// The primary content is the full original user input (result.OriginalInput);
// a brief summary is prepended only when it differs from the full input.
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

	// Use the full original user input, falling back to the intent summary
	// when OriginalInput is not populated (e.g. programmatic dispatch results).
	content := result.OriginalInput
	if content == "" {
		content = result.Intent.Summary
	}
	parts = append(parts, content)

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
		"internal capabilities", "your capabilities", "tell me about your", "tell me about capabilities", "built into", "agent harness", "memory system", "tool system",
		"what models", "what agents are", "available tools", "your tools", "your features", "how are you built", "your architecture",
		"what are you aware of", "what do you have access to", "platform capabilities", "system capabilities", "capabilities"}, string(IntentPlatform), config.AgentIDChat, 0.9, false},

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

	// Collaboration (pair programming, differential analysis)
	{[]string{"collaborate", "pair program", "differential", "a/b test", "compare approaches", "work together", "collaborative"}, string(IntentCollaborate), config.AgentIDAnalyst, 0.8, true},

	// Analysis/Research ("summarize" alone stays here for document summarization;
	// "summarize what" and "summary of work" are captured by report intent above).
	// Pure research/investigation intents route to the dedicated researcher agent;
	// synthesis/explanation intents stay with the analyst.
	{[]string{"research", "investigate", "deep dive", "study"}, string(IntentResearch), config.AgentIDResearcher, 0.7, false},
	{[]string{string(IntentAnalyze), "summarize", KeywordExplain, "what is", "how does"}, string(IntentAnalyze), config.AgentIDAnalyst, 0.7, false},
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
	d.lastClassifierMethod = method
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
	byMethod := make(map[string]int, len(d.stats.ByMethod))
	maps.Copy(byMethod, d.stats.ByMethod)
	byAgent := make(map[string]int, len(d.stats.ByAgent))
	maps.Copy(byAgent, d.stats.ByAgent)
	byIntent := make(map[string]int, len(d.stats.ByIntent))
	maps.Copy(byIntent, d.stats.ByIntent)
	return DispatcherStats{
		TotalDispatched: d.stats.TotalDispatched,
		ByMethod:        byMethod,
		ByAgent:         byAgent,
		ByIntent:        byIntent,
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
	result := make([]FallbackEntry, limit)
	copy(result, d.stats.FallbackDetails[len(d.stats.FallbackDetails)-limit:])
	return result
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

// ShouldRouteToPair returns true if the dispatch result should use channel-based
// pairing instead of the step-based orchestrator.
func (d *Dispatcher) ShouldRouteToPair(result *DispatchResult) bool {
	if result == nil || result.Intent == nil {
		return false
	}
	return IntentType(result.Intent.Type) == IntentPair
}

// ShouldRouteToCollaborate returns true if the dispatch result should be
// routed to the CollaborationEngine for a collaboration session.
func (d *Dispatcher) ShouldRouteToCollaborate(result *DispatchResult) bool {
	if result == nil || result.Intent == nil {
		return false
	}
	return IntentType(result.Intent.Type) == IntentCollaborate
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
	if matcher != nil {
		d.capabilityMatcher = matcher
	}
}

// SetMetricsStore wires the metrics store for persistent dispatch logging.
func (d *Dispatcher) SetMetricsStore(store *metrics.Store) {
	if store != nil {
		d.metricsStore = store
	}
}

// RecordDispatch logs a dispatch routing decision from the handler switch for
// debugging and persistent audit trail. This is the public entry point called
// by ChatHandler after it determines which case handled the result.
func (d *Dispatcher) RecordDispatch(sessionID, handlerCase, inputSummary string, result *DispatchResult, hasParts bool, dispatchErr error) {
	d.recordDispatch(sessionID, handlerCase, inputSummary, result, hasParts, dispatchErr)
}

// recordDispatch logs a dispatch routing decision to both the structured logger
// (debug level) and the persistent metrics store (if wired).
func (d *Dispatcher) recordDispatch(sessionID, handlerCase, inputSummary string, result *DispatchResult, hasParts bool, dispatchErr error) {
	intentType := ""
	agentID := ""
	confidence := 0.0
	classifierMethod := ""
	taskID := ""

	if result != nil {
		agentID = result.AgentID
		if result.Intent != nil {
			intentType = result.Intent.Type
			confidence = result.Intent.Confidence
		}
		if result.Task != nil {
			taskID = result.Task.ID
		}
	}
	// Extract the classification method from the dispatcher's last-recorded method.
	if d.stats != nil {
		d.stats.mu.RLock()
		classifierMethod = d.lastClassifierMethod
		d.stats.mu.RUnlock()
	}

	errStr := ""
	if dispatchErr != nil {
		errStr = dispatchErr.Error()
	}

	d.logger.Debug("routing decision",
		"case", handlerCase,
		"session", sessionID,
		"intent", intentType,
		"agent", agentID,
		"confidence", confidence,
		"classifier", classifierMethod,
		"has_task", taskID != "",
		"has_parts", hasParts,
		"error", errStr,
	)

	if d.metricsStore != nil {
		d.metricsStore.RecordDispatch(metrics.DispatchEntry{
			SessionID:        sessionID,
			InputSummary:     inputSummary,
			IntentType:       intentType,
			AgentID:          agentID,
			Confidence:       confidence,
			ClassifierMethod: classifierMethod,
			HandlerCase:      handlerCase,
			TaskID:           taskID,
			HasParts:         hasParts,
			Error:            errStr,
		})
	}
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

// --- Dispatcher heuristics (Issues 0006, 0029, 0036) ---

const (
	// shortMessageThreshold is the character length below which messages
	// are considered simple and routed directly to the chat agent.
	shortMessageThreshold = 50

	// compoundKeywordThreshold is the min message length that must contain
	// compound signal words to qualify for multi-intent analysis.
	compoundKeywordThreshold = 80
)

// isShortSimpleMessage returns true if the input is too short or too simple
// to warrant more than a single chat agent response.
// Guards against tiny-model over-classification (Issues 0006, 0029, 0036).
func isShortSimpleMessage(input string) bool {
	trimmed := strings.TrimSpace(input)
	if len(trimmed) == 0 {
		return true
	}
	lower := strings.ToLower(trimmed)

	// Pure arithmetic or math expressions
	if strings.Contains(lower, "what is ") && (strings.Contains(lower, "+") || strings.Contains(lower, "-") || strings.Contains(lower, "*") || strings.Contains(lower, "/")) {
		return true
	}

	// Very short messages (under 10 chars) are trivially chat unless they
	// contain keyword indicators for specialist agents.
	if len(trimmed) < 10 {
		// Even very short messages should not be blocked if they contain
		// keyword-level intent signals (e.g. "commit these changes" -> git)
		if len(trimmed) < 5 {
			return true
		}
		// Check if keywords match specialist intents -- if so, don't guard
		if hasKeywordMatch(trimmed) {
			return false
		}
		return true
	}

	// For messages up to shortMessageThreshold chars: block only if they
	// are purely conversational / greetings with no domain-specific keywords.
	if len(trimmed) < shortMessageThreshold {
		// If the message contains keyword indicators for any specialist,
		// let the classifier handle it.
		if hasKeywordMatch(trimmed) {
			return false
		}
		// Pure conversational patterns that don't warrant specialist routing
		simplePatterns := []string{
			"what's up", "how are you", "hey", "hello", "hi there",
			"is this working", "are you there", "can you hear me",
			"hello world", "hi", "hey there",
		}
		for _, p := range simplePatterns {
			if lower == p {
				return true
			}
		}
		// Single-word or two-word questions that are clearly chat
		words := strings.Fields(lower)
		return len(words) <= 3
	}

	return false
}

// hasKeywordMatch returns true if the input contains keywords that match
// specialist intent patterns, indicating the message should not be
// short-circuited to chat.
func hasKeywordMatch(input string) bool {
	lower := strings.ToLower(strings.TrimSpace(input))
	// Keywords mapped to specialist agents (from keywordPatterns)
	specialistKeywords := []string{
		// Git
		"commit", "push", "pull", "merge", "branch", "rebase",
		// Debug
		"bug", "error", "exception", "crash", "debug", "fix ",
		// Code
		"write", "create", "implement", "add feature", "refactor",
		// Review
		"code review", "review pr", "check code",
		// Schedule
		"remind", "alarm", "timer",
		// Plan
		"plan", "design", "architect",
		// Research/Analysis
		"research", "analyze", "explain",
		// Search
		"search", "find", "look up",
		// Security
		"security", "vulnerability", "exploit",
		// Platform introspection
		"capabilities", "what can you do", "your tools", "your agents",
		// Git
		"git",
	}
	for _, kw := range specialistKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// compoundSignalWords lists conjunctions and multi-word signal phrases that
// indicate a request genuinely contains multiple distinct intents. These use
// simple substring match (they already contain surrounding spaces in the
// literal so they won't false-match inside words).
var compoundSignalWords = []string{
	" and also", " as well as ", " plus ", " while ", " then ",
	" and do", " and create", " and write", " and fix",
	" and implement", " and also create", " and also write",
	" and also fix", " and also implement", " and also add",
	" and then", " but also", " at the same time",
	"after that",
}

// shortCompoundWords are single-letter/signal words that need word-boundary
// matching so they don't false-match inside larger words (e.g. "next" in
// "packet", "socket", "context").
var shortCompoundWords = []string{"first", "second", "next"}

// compoundSignalRegex is a pre-compiled alternation of the short compound words
// with word boundaries. Using a single regex is more efficient than compiling
// a separate pattern per word.
func buildCompoundRegex() *regexp.Regexp {
	quoted := make([]string, len(shortCompoundWords))
	for i, w := range shortCompoundWords {
		quoted[i] = regexp.QuoteMeta(w)
	}
	return regexp.MustCompile(`\b(?:` + strings.Join(quoted, "|") + `)\b`)
}

var compoundSignalRegex = buildCompoundRegex()

// hasCompoundSignalWords returns true if the input is long enough and contains
// at least one compound signal word, suggesting multiple distinct intents.
func hasCompoundSignalWords(input string) bool {
	if len(input) < compoundKeywordThreshold {
		return false
	}
	lower := strings.ToLower(input)
	for _, word := range compoundSignalWords {
		if strings.Contains(lower, word) {
			return true
		}
	}
	// Short single words need word-boundary matching to avoid false positives
	// inside compound words (e.g. "next" in "packet", "first" in "aircraft").
	return compoundSignalRegex.MatchString(lower)
}

// heuristicFallback provides targeted keyword-based routing when all other
// classifiers fail (Issue 0036). Rules are ordered by specificity and
// confidence to avoid misrouting code tasks to scheduler/committer.
func heuristicFallback(input string) *Intent {
	lower := strings.ToLower(strings.TrimSpace(input))

	// Code-related rules with explicit confidence >= 0.3
	// These address the bug where "write a Go function" was routed to chat
	// instead of coder.
	simpleCodeKeywords := []string{
		"write a", "write some", "write code", "write a function",
		"create a file", "create a new", "create a function",
		"implement a", "implement the", "implement new",
		"add a function", "add a new", "add a feature", "add a method",
		"add an endpoint", "add a route",
		"build a", "build me a",
		"make a", "make me a",
		"generate a", "generate the",
		"code a",
	}
	for _, kw := range simpleCodeKeywords {
		if strings.Contains(lower, kw) {
			return &Intent{
				Type:       string(IntentCode),
				Confidence: 0.55,
				AgentType:  config.AgentIDCoder,
				Summary:    extractSummary(input),
			}
		}
	}

	// More granular code indicators
	codeIndicators := []string{
		"function", "method", "class", "struct", "interface",
		"type def", "import ", "package ", "def ", "fn ",
	}
	if hasCodeVerb(lower) {
		for _, ind := range codeIndicators {
			if strings.Contains(lower, ind) {
				return &Intent{
					Type:       string(IntentCode),
					Confidence: 0.5,
					AgentType:  config.AgentIDCoder,
					Summary:    extractSummary(input),
				}
			}
		}
	}

	// Debug-related
	debugKeywords := []string{
		"fix ", "bug", "error:", "exception", "crash",
		"panic", "segfault", "not working", "broken",
		"debug", "trace", "stack trace",
	}
	for _, kw := range debugKeywords {
		if strings.Contains(lower, kw) {
			return &Intent{
				Type:       string(IntentDebug),
				Confidence: 0.55,
				AgentType:  config.AgentIDDebugger,
				Summary:    extractSummary(input),
			}
		}
	}

	// Git-related
	gitKeywords := []string{
		"commit", "push", "pull", "merge", "branch",
		"rebase", "revert", "checkout",
	}
	for _, kw := range gitKeywords {
		if strings.Contains(lower, kw) {
			return &Intent{
				Type:       string(IntentGit),
				Confidence: 0.55,
				AgentType:  config.AgentIDCommitter,
				Summary:    extractSummary(input),
			}
		}
	}

	// Analysis/explanation
	analysisKeywords := []string{
		"what is ", "what does ", "explain ", "how does ",
		"how to ", "what's the difference", "compare",
	}
	for _, kw := range analysisKeywords {
		if strings.Contains(lower, kw) {
			return &Intent{
				Type:       string(IntentAnalyze),
				Confidence: 0.45,
				AgentType:  config.AgentIDAnalyst,
				Summary:    extractSummary(input),
			}
		}
	}

	return nil
}

// hasCodeVerb checks if input contains a verb commonly associated with code tasks.
func hasCodeVerb(lower string) bool {
	codeVerbs := []string{"write", "create", "implement", "build", "add", "make", "generate", "code", "develop"}
	for _, verb := range codeVerbs {
		if strings.Contains(lower, verb) {
			return true
		}
	}
	return false
}

// buildClarificationQuestion generates a clarification dialog for ambiguous model directives.
func (d *Dispatcher) buildClarificationQuestion(directive *ModelReassignmentDirective) string {
	// Check for specific ambiguity types
	if len(directive.ModelReferences) == 0 {
		// No models parsed - list available options
		return d.buildModelListQuestion(directive.TargetScope)
	}

	if directive.TargetScope == "" {
		// No scope parsed - ask what the models should handle
		return d.buildScopeQuestion(directive.ModelReferences)
	}

	// Check for provider-level references that need specific model selection
	var providerRefs []string
	for _, ref := range directive.ModelReferences {
		if strings.HasPrefix(ref, "provider:") {
			providerRefs = append(providerRefs, strings.TrimPrefix(ref, "provider:"))
		}
	}

	if len(providerRefs) > 0 {
		return d.buildProviderClarification(providerRefs, directive.TargetScope)
	}

	// Generic fallback
	return fmt.Sprintf(
		"I want to make sure I use the right model. You mentioned '%s' - could you clarify which model and what it should handle?",
		directive.Instruction,
	)
}

// buildModelListQuestion asks the user to specify which model when none were parsed.
func (d *Dispatcher) buildModelListQuestion(scope string) string {
	if scope != "" {
		return fmt.Sprintf(
			"I can use specific models for %s. Which model would you prefer? You can specify:\n"+
				"- A specific model (e.g., 'glm-4.7', 'claude-opus', 'qwen-coder')\n"+
				"- A provider (e.g., 'zai', 'anthropic', 'ollama', 'local')",
			scope,
		)
	}
	return "I couldn't identify specific model names. Which model would you like to use? " +
		"You can specify a model name (e.g., 'glm-4.7', 'claude-opus') or a provider (e.g., 'zai', 'local')."
}

// buildScopeQuestion asks the user to specify what scope the models should handle.
func (d *Dispatcher) buildScopeQuestion(modelRefs []string) string {
	models := strings.Join(modelRefs, ", ")
	return fmt.Sprintf(
		"I can use %s for your task. What should these models handle?\n"+
			"- coding/implementation\n"+
			"- research/analysis\n"+
			"- planning/synthesis\n"+
			"- debugging\n"+
			"- the entire task",
		models,
	)
}

// buildProviderClarification asks the user to specify which model from a provider.
func (d *Dispatcher) buildProviderClarification(providers []string, scope string) string {
	if len(providers) == 1 {
		provider := providers[0]
		providerModels := map[string][]string{
			"zai":       {"glm-4.7 (most capable)", "glm-4.5-air (faster)"},
			"anthropic": {"claude-3-opus (most capable)", "claude-3-sonnet (balanced)", "claude-3-haiku (fastest)"},
			"ollama":    {"llama3.2", "qwen2.5-coder"},
			"local":     {"lfm-code (1.2B, code-optimized)", "lfm-24b (largest)", "lfm-thinking-claude (reasoning)"},
		}

		if models, ok := providerModels[provider]; ok {
			return fmt.Sprintf(
				"I can use %s models for %s. Which would you prefer?\n%s",
				provider, scope, strings.Join(models, "\n"),
			)
		}
	}

	return fmt.Sprintf(
		"You mentioned %s models for %s. Could you specify which exact model(s) you'd like to use?",
		strings.Join(providers, ", "), scope,
	)
}
