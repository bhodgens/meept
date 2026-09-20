package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/memvid"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/preferences"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/templates"
)

// anaphoraForRegex matches "do the same for X" patterns for anaphora resolution.
var anaphoraForRegex = regexp.MustCompile(`do the same for (.+)`)

// secretShape matches obvious API-key/token shapes in error text so they can
// be scrubbed before persisting (classifier-observability S4): sk- prefixed
// keys, api_key/api-key/apikey, bearer tokens, and generic tokens followed
// by 8+ non-space chars. \s* after the keyword is a deliberate deviation
// from the leaf's literal `[^\s]{8,}` so the spec's own test case ("Bearer
// abc123def456...") matches despite the space between keyword and secret.
var secretShape = regexp.MustCompile(`(?i)(sk-|api[_-]?key|bearer|token)\s*[^\s]{8,}`)

// reRouteWindow bounds the re-route detector (classifier-outcome-loop leaf 03,
// Signal A; design.md S2): a classified dispatch is marked 'corrected' only
// when the same session lands on a DIFFERENT agent within this many turns.
// Agent switches further apart are not treated as corrections.
const reRouteWindow = 3

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
	// Quickplan is a plan-execution flow that runs without check-ins
	// (adjudication record: docs/plans/classifier-iteration); mid-flow
	// steering defaults to follow-up like compound.
	IntentQuickPlan: false,
	IntentUnknown:   false,
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
	// OriginalInput preserves the full untruncated input this intent was
	// classified from (bughunt 2026-09-10 M4). Set on clarify intents so
	// ResumeAfterClarification can re-analyze the user's actual request
	// instead of the first-100-chars Summary. Empty for all other intents.
	OriginalInput string `json:"original_input,omitempty"`
	// TrueAnalysis holds the IntentGate-style pre-classification analysis if available.
	TrueAnalysis *TrueIntentAnalysis `json:"true_analysis,omitempty"`
	// SuggestedMode is the synthesized planning mode (Thread D complexity routing).
	// Populated by suggestMode in ClassifyAndRoute. Empty means no suggestion.
	SuggestedMode string `json:"suggested_mode,omitempty"`
	// Method records which classifier produced this intent (e.g.
	// "capability_matcher", "llm", "keyword", "semantic",
	// "heuristic_fallback", "short_message_guard", "fallback",
	// "compound", "instruction_parser"). Empty when the intent did not
	// come from a classifier branch (e.g. clarification requests). Set at
	// each classify branch next to recordClassificationMethod and logged
	// in the "Dispatched request" line for regression tracking.
	Method string `json:"classification_method,omitempty"`
	// Model is the resolved "provider/model" of the classifier/analyzer
	// LLM that actually served this classification (provenance, leaf 01
	// of classifier-observability). Set only at LLM-served classify
	// branches, from the model the serving component resolved (including
	// any alias-failover rotation). Empty for deterministic branches
	// (keyword/heuristic/guard/etc.) and when all LLM candidates failed —
	// honest provenance: no model, no attribution.
	Model string `json:"model,omitempty"`
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

	// Isolation records the context-isolation level this dispatch result was
	// spawned under. Always ArtifactOnly today; SharedTranscript would be an
	// explicit opt-in. Zero value means ArtifactOnly by fail-closed contract.
	Isolation ContextIsolation `json:"isolation,omitempty"`
	// Brief is the structured handoff brief produced via BuildSpawnContext
	// (report summary + artifact refs), replacing raw parent-transcript
	// propagation. Empty for fresh user-initiated dispatches.
	Brief string `json:"brief,omitempty"`
	// MemoryIDs are memory references attached to the handoff brief.
	MemoryIDs []string `json:"memory_ids,omitempty"`
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

	// SuggestedMode is the complexity-routing mode from Thread D (direct/plan/
	// spec_plan/spec_pair). Forwarded to PlanRequest.Mode for the strategic
	// planner. Empty means no suggestion (planner uses its own heuristics).
	SuggestedMode string `json:"suggested_mode,omitempty"`

	// ExecutorModelRef is the "provider/model-id" ref of the executor model
	// resolved for this dispatch (allotment tree leaf 02). Sourced from the
	// first resolved model of a user model directive when present; raw
	// directive references are carried verbatim (the provider downstream
	// re-resolves through the LLM resolver, which is authoritative).
	// Forwarded to PlanRequest.ExecutorModelRef. Empty = no directive.
	ExecutorModelRef string `json:"executor_model_ref,omitempty"`

	// RequestModel is a client-supplied per-request model ref (chat.request
	// "model" field: alias name or "provider/model-id"). RouteToAgent applies
	// it to the executor loop through the ONE-SHOT SetModelOverride seam —
	// the same precedence slot as a parsed user model directive — so it
	// serves exactly this turn and auto-clears. Empty = no request-level
	// model; the agent's alias/default chain runs unchanged. Tagged json:"-"
	// (operational metadata, not user-facing serialization).
	RequestModel string `json:"-"`

	// ReasoningOverride carries the parsed user reasoning directive (if any)
	// so downstream code can forward it to the agent loop. When non-nil, it
	// takes precedence over SuggestedReasoningTier per spec §7.5. Tagged
	// json:"-" because it is operational metadata not meant for
	// user-facing JSON serialization.
	ReasoningOverride *llm.ReasoningConfig `json:"-"`

	// Instruction is the parsed instruction when user provides automation request.
	// Only populated when intent type is IntentInstruction.
	Instruction *preferences.ParsedInstruction `json:"-"`

	// PrefilterVerdict carries the Door-1 kNN verdict (with margin) that
	// produced this result, when the embedding prefilter ran for this
	// dispatch (classifier-outcome-loop leaf 02). Present for routed AND
	// abstained Door-1 dispatches — an abstain yields a nil Intent but a
	// non-nil verdict, which is how the margin survives to recordDispatch.
	// nil for every other classification door. Tagged json:"-" (operational
	// metadata, not user-facing serialization).
	PrefilterVerdict *PrefilterVerdict `json:"-"`

	// AgentOverrideApplied records that the client explicitly named this
	// agent (chat.request agent_id → dispatcher agentOverride) and the
	// override was applied at step 5.3. RouteToAgent reads this to skip
	// intent-Type shortcuts (e.g. the platform-introspection canned dump)
	// that would otherwise swallow the turn before the overridden agent
	// runs (researcher-extract e2e, 2026-09-12). Tagged json:"-" —
	// operational metadata.
	AgentOverrideApplied bool `json:"-"`
}

// executorModelRefFromDirective extracts the "provider/model-id" ref of the
// executor model from a parsed model directive (allotment tree leaf 02):
// the first resolver-resolved model when available, else the first raw
// reference verbatim (the downstream provider re-resolves through the LLM
// resolver, which is authoritative). Empty when no directive/models.
func executorModelRefFromDirective(directive *ModelReassignmentDirective) string {
	if directive == nil {
		return ""
	}
	if len(directive.ResolvedModels) > 0 {
		mc := directive.ResolvedModels[0]
		return fmt.Sprintf("%s/%s", mc.ProviderID, mc.ModelID)
	}
	if len(directive.ModelReferences) > 0 {
		return directive.ModelReferences[0]
	}
	return ""
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
	prefilter         *EmbeddingPrefilter
	sessionTracker    *SessionTracker
	stats             *DispatcherStats
	router            *ReportRouter
	modelParser       *ModelReassignmentParser
	planManager       *plan.PlanManager
	metricsStore      *metrics.Store

	// inputHasher, when non-nil, derives the salted input_hash persisted in
	// dispatch_log (classifier-observability S4). The dispatcher stays
	// crypto-agnostic: the daemon owns the salt and injects a closure over
	// metrics.HashInput via SetInputHasher. Nil => InputHash "" (the
	// multi-user-disabled path stays untouched).
	inputHasher func(message string) string

	// Door-1 prefilter verdict capture (classifier-outcome-loop leaf 02).
	// stashVerdict stores the verdict emitted by the LAST prefilter Match
	// call; takeStashedPrefilterVerdict pops it for recordDispatch. The
	// prefilter block in ClassifyAndRoute runs synchronously before
	// recordDispatch on the same goroutine — the mutex is belt-and-braces
	// for tests (the public RecordDispatch path), not for concurrency.
	prefilterMu     sync.Mutex
	stashVerdict    PrefilterVerdict
	stashHasVerdict bool

	// sessionDrift is the per-session intent-embedding drift detector
	// (issue #41). Wired via SetDriftDetector by daemon composition;
	// nil/disabled => Observe is never called and routing is unchanged
	// (the detector is log-only at this call site until precision is
	// measured on real traffic).
	sessionDrift *SessionDriftDetector

	// burstDetector is the tool-failure burst detector (issue #43),
	// fed by resolved dispatch outcomes in recordDispatch. Wired via
	// SetBurstDetector by daemon composition; nil/disabled => outcomes
	// are not observed and routing is unchanged (log-only at the call
	// site).
	burstDetector *metrics.BurstDetector

	// gitVerbAgreementVeto enables the git-verb agreement veto (issue
	// #46): a git verdict on a git-verb-free imperative input is
	// discarded so the chain continues. Set in NewDispatcher; default
	// true.
	gitVerbAgreementVeto bool

	// toolRegistry provides structural tool gating by depth.
	// When non-nil, the dispatcher uses it to gate depth-sensitive
	// tools (like subagent spawn) so agents at maxDepth simply
	// don't have them in their registry.
	toolRegistry *DepthToolRegistry

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

	// instructionStore handles user instructions for automation (Phase 1).
	// When nil, instruction-based automation is disabled.
	instructionStore *preferences.Store

	// instructionParser parses natural language into structured instructions.
	instructionParser *InstructionParser

	// loopManager resolves per-session AgentLoops so that dispatched
	// agents execute with the session's project_path as their working
	// directory. When nil, RouteToAgent falls back to the singleton
	// registry agent (legacy behaviour).
	loopManager *Manager

	// sessionStore looks up session project paths for loopManager.
	sessionStore SessionStoreReader

	// defaultWorkingDir is the configured last resort
	// (daemon.default_working_dir), consulted only after every session-bound
	// source has come up empty. There is NO global active-project fallback:
	// projects are scoped per session. Empty means none.
	defaultWorkingDir string

	// fenceController configures the shared fence sandbox per-session
	// (root = project path, plus --nofence override). Optional; nil-safe.
	fenceController FenceController
}

// mediaURLPattern matches YouTube URL forms (hostnames + path shapes).
// It mirrors transcript_fetch's parseVideoID acceptance set, so anything
// this guard routes to the analyst is something transcript_fetch can
// ingest. URL forms are UNGATED — a literal URL is an unambiguous signal.
var mediaURLPattern = regexp.MustCompile(`(?i)(?:youtube\.com/(?:watch\?v=|shorts/|embed/|live/)|youtu\.be/)[A-Za-z0-9_-]{11}`)

// bareVideoIDPattern matches a bare 11-char video ID delimited by
// whitespace or message boundaries.
var bareVideoIDPattern = regexp.MustCompile(`(?:^|\s)[A-Za-z0-9_-]{11}(?:\s|$)`)

// mediaContextPattern detects media co-occurrence for the bare-ID form.
// An 11-char token alone is far too weak a signal (any 11-character word —
// "development", "application", "handleClick" — matches), so the bare-ID
// alternation only fires when the message also talks about media.
var mediaContextPattern = regexp.MustCompile(`(?i)\b(?:video|videos|youtube|watch|transcript|transcripts|clip|clips|subtitle|subtitles|caption|captions|footage|recording|stream|vlog|shorts)\b`)

// detectMediaURL reports the media URL (or bare video ID) carried by a
// user message, or "" when none is present. Only YouTube is detected —
// the deterministic routing exists specifically because transcript_fetch
// is the ingest tool for these targets.
//
// URL forms match unconditionally in this DETECTOR (the raw signal);
// the GATE that consumes it — mediaGuardVerdict below — applies the
// media-consumption-operation requirement (AR-2, routing-repair leaf
// 04): a coding request that merely cites a YouTube URL as fixture/data
// must fall through to the LLM chain, while "summarize this video
// <url>" keeps the deterministic analyst route. The bare 11-char-ID
// form requires media-context co-occurrence (H5) here, unchanged.
func detectMediaURL(input string) string {
	if m := mediaURLPattern.FindString(input); m != "" {
		return m
	}
	if mediaContextPattern.MatchString(input) {
		return strings.TrimSpace(bareVideoIDPattern.FindString(input))
	}
	return ""
}

// mediaGuardVerdict reports the media target when the message carries
// media-CONSUMPTION operation evidence — the condition under which the
// dispatcher's media guard routes deterministically to the analyst.
// Operation class, not phrase class: a leading ingest verb (summarize,
// watch, transcribe, recap) whose object is the media, or an
// object-anchored transitive construction ("transcript of/for the
// video", "get the transcript for <url>"). A URL cited as DATA inside
// a coding/testing request returns "" and the normal chain runs.
func mediaGuardVerdict(input string) string {
	target := detectMediaURL(input)
	if target == "" {
		return ""
	}
	if mediaConsumptionPattern.MatchString(input) {
		return target
	}
	return ""
}

// mediaConsumptionPattern is the media-consumption OPERATION evidence
// consumed by mediaGuardVerdict (AR-2, routing-repair leaf 04).
var mediaConsumptionPattern = regexp.MustCompile(`(?i)^\s*(?:please\s+)?(?:can|could|would)?\s*(?:you\s+)?(?:please\s+)?(?:summar\w*|transcribe|transcribing|watch\w*|rewatch|recap\w*|vlog)\b|` +
	`\btranscripts?\b(?:\s+(?:of|for))?\s+(?:this |that |the )?(?:video|clip|recording|youtube)|` +
	`\b(?:get|fetch|pull|grab|obtain)\b[^.!?]{0,40}\b(?:transcript|subtitles?|captions?)\b|` +
	`\bsubtitles?\s+(?:for|of)\b`)

// IntentClassifier is an interface for classifying intents.
type IntentClassifier interface {
	Classify(ctx context.Context, input string, memCtx *MemoryContext) (*Intent, error)
}

// DispatcherConfig holds configuration for creating a Dispatcher.
type DispatcherConfig struct {
	Registry         *AgentRegistry
	MemvidClient     *memvid.Client
	MemoryMgr        *memory.Manager
	TaskStore        *task.Store
	TaskRegistry     *task.Registry
	AmendmentManager *task.AmendmentManager
	SkillRegistry    *skills.Registry
	SkillExecutor    *skills.Executor
	TemplateRegistry *templates.Registry
	Logger           *slog.Logger
	LLMClient        *llm.Client
	ClassifierClient *llm.Client // Separate client for classification (nil = use LLMClient)
	ClassifierModel  string
	// ClassifierModelConfig is the resolved model configuration for the
	// classifier endpoint (from the resolver). When non-nil, classification
	// token caps are derived from its declared max_output.
	ClassifierModelConfig *llm.ModelConfig
	// Resolver enables alias failover for classification and intent analysis
	// (leaf 03 of classifier-reliability). When non-nil (and ClassifierClient
	// is set), failed Chat attempts rotate to the next candidate in the
	// ClassifierAlias chain and retry once. Nil = no failover. Ignored when
	// ClassifierClient is nil (main-LLMClient fallback path).
	Resolver *llm.Resolver
	// ClassifierAlias is the resolver alias name used with Resolver.
	// Empty defaults to "classifier" when Resolver is set.
	ClassifierAlias   string
	ClassifierTimeout time.Duration // Per-classification timeout; 0 = defaultClassifierTimeout (10s).
	// ClassifierFailFast disables classifier alias rotation: when true, the
	// intent analyzer returns the primary classifier's error immediately
	// instead of rotating to weaker alias members. Default false —
	// production keeps rotation; for testing/iteration
	// (classifier-observability leaf 02).
	ClassifierFailFast bool
	CapabilityMatcher  *CapabilityMatcher
	EmbeddingClient    EmbeddingClient
	// PrefilterConfig enables the Stage-0 embedding prefilter
	// (classifier-observability follow-up). When Enabled, the dispatcher
	// constructs an EmbeddingPrefilter (OpenAI-compatible embeddings
	// client + centroid store) that direct-routes confident inputs before
	// the analyzer + router LLM calls.
	PrefilterConfig config.ClassifierPrefilterConfig
	SessionMaxAge   time.Duration
	PlanManager     *plan.PlanManager
	// AmbiguityThreshold configures the IntentAnalyzer's gate for blocking
	// routing on high-ambiguity inputs. 0 means use the legacy const
	// (defaultAmbiguityThreshold = 0.6 in intent_analyzer.go).
	AmbiguityThreshold float64

	// ToolRegistry provides structural depth-based tool gating.
	// When non-nil, tools are gated so agents at maxDepth don't
	// see spawn capabilities ("make illegal states unrepresentable").
	ToolRegistry *DepthToolRegistry

	// GitVerbAgreementVeto gates the git-verb agreement veto (issue #46):
	// a classifier git/committer verdict on an input that opens with an
	// execution imperative but carries NO git verb is a lexical bias of
	// small classifiers (the 350M prompt-router routes "create a file in
	// the repository root" to git because of the word "repository"), and
	// the verdict is discarded so the chain continues. Nil = default true
	// (veto active); false disables it (escape hatch while measuring).
	GitVerbAgreementVeto *bool
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
		toolRegistry:      cfg.ToolRegistry,
	}

	// Git-verb agreement veto (issue #46). Nil = default on.
	if cfg.GitVerbAgreementVeto != nil {
		d.gitVerbAgreementVeto = *cfg.GitVerbAgreementVeto
	} else {
		d.gitVerbAgreementVeto = true
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
		// Alias failover is wired only for the dedicated ClassifierClient
		// path: when falling back to the main LLMClient, failure handling is
		// a different concern and failover stays disabled.
		failoverResolver := cfg.Resolver
		failoverAlias := cfg.ClassifierAlias
		if failoverAlias == "" {
			failoverAlias = "classifier"
		}
		if cfg.ClassifierClient == nil {
			failoverResolver = nil // main-client fallback: no alias failover
		}
		d.llmClassifier = NewLLMClassifier(
			LLMClassifierConfig{
				Client:      classifierClient,
				Model:       cfg.ClassifierModel,
				Timeout:     cfg.ClassifierTimeout,
				ModelConfig: cfg.ClassifierModelConfig,
				Resolver:    failoverResolver,
				AliasName:   failoverAlias,
			},
			cfg.Logger,
		)
		// Initialize intent analyzer using same classifier client.
		// Apply the configured ambiguity threshold when non-zero;
		// otherwise NewIntentAnalyzer uses its built-in default.
		ia := newIntentAnalyzerWithConfig(IntentAnalyzerConfig{
			ModelConfig: cfg.ClassifierModelConfig,
			Resolver:    failoverResolver,
			AliasName:   failoverAlias,
			FailFast:    cfg.ClassifierFailFast,
		}, classifierClient, cfg.Logger)
		if cfg.AmbiguityThreshold > 0 {
			ia = ia.WithAmbiguityThreshold(cfg.AmbiguityThreshold)
		}
		d.intentAnalyzer = ia
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

	// Initialize the Stage-0 embedding prefilter when configured
	// (classifier-observability follow-up). Construction never blocks:
	// centroid load is lazy and a missing store leaves the prefilter
	// inert (Match returns nil → LLM chain runs as before).
	if cfg.PrefilterConfig.Enabled && cfg.PrefilterConfig.BaseURL != "" {
		// Normalize the embed-call timeout the same way NewEmbeddingPrefilter
		// does (<=0 → defaultPrefilterTimeout) so both layers honor the
		// configured PrefilterConfig.TimeoutSeconds (M15).
		embTimeout := defaultPrefilterTimeout
		if cfg.PrefilterConfig.TimeoutSeconds > 0 {
			embTimeout = time.Duration(cfg.PrefilterConfig.TimeoutSeconds) * time.Second
		}
		embClient := NewOpenAIEmbedClient(
			cfg.PrefilterConfig.BaseURL,
			cfg.PrefilterConfig.Model,
			embTimeout,
		)
		d.prefilter = NewEmbeddingPrefilter(embClient, cfg.PrefilterConfig, cfg.Logger)
		// Door-1 verdict capture (classifier-outcome-loop leaf 02): one
		// observer, registered once at construction (SetMetricsStore
		// precedent), stashes the last verdict for recordDispatch. The
		// prefilter block runs synchronously before recordDispatch, so
		// the stash is always THIS dispatch's verdict when read.
		d.prefilter.SetVerdictObserver(d.stashPrefilterVerdict)
		d.logger.Info("Stage-0 classifier prefilter enabled",
			"base_url", cfg.PrefilterConfig.BaseURL,
			"model", cfg.PrefilterConfig.Model,
			"threshold", cfg.PrefilterConfig.Threshold,
			"timeout", embTimeout,
		)
	} else if cfg.PrefilterConfig.Enabled {
		// Enabled without base_url would otherwise be silently ignored —
		// surface the misconfiguration so operators see why Stage-0 is
		// inert.
		d.logger.Warn("prefilter enabled but base_url empty; Stage-0 prefilter disabled",
			"centroids_path", cfg.PrefilterConfig.CentroidsPath,
		)
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
// Stop cancels the semantic index BuildIndex goroutine and waits for it to
// finish. Called from Components.Stop() during daemon shutdown.
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

// SetAgentLoopManager wires the per-session AgentLoop manager so that
// RouteToAgent can resolve a session-scoped loop (with the correct
// project working directory) instead of always using the singleton
// registry agent. Nil is a no-op.
func (d *Dispatcher) SetAgentLoopManager(m *Manager) {
	if m != nil {
		d.loopManager = m
	}
}

// SetSessionStore wires a session store for project-path lookup in
// SetSessionStore wires the session store used to resolve a dispatched
// turn's working directory. Nil is a no-op.
func (d *Dispatcher) SetSessionStore(s SessionStoreReader) {
	if s != nil {
		d.sessionStore = s
	}
}

// resolveSessionWorkingDir returns the working directory for a dispatched
// turn and the source it came from, using the ONE precedence documented in
// AGENTS.md and implemented by session.ResolveWorkingDir:
//
//	WorktreePath > ProjectPath > DetectionContext.CWD
//
// One session-bound resolution follows because the dispatch path reaches
// sessions the chat path never sees (HTTP requests with a synthetic or
// project-less session): the session's project ID resolved through the loop
// manager. Project scoping is PER-SESSION — there is no global
// active-project fallback. Nothing here invents a directory; when every
// session-bound source comes up empty the caller gets
// ("", WorkingDirFromNone) and the tools fail actionably instead of writing
// relative paths into the daemon's own CWD.
func (d *Dispatcher) resolveSessionWorkingDir(sess *session.Session) (string, session.WorkingDirSource) {
	dir, source := session.ResolveWorkingDir(sess)
	if dir != "" {
		return dir, source
	}
	if sess != nil && sess.ProjectID != "" && d.loopManager != nil {
		if p := d.loopManager.ResolveProjectPath(context.Background(), sess.ProjectID); p != "" {
			return p, session.WorkingDirFromProject
		}
	}
	if d.defaultWorkingDir != "" {
		return d.defaultWorkingDir, session.WorkingDirFromDefault
	}
	return "", session.WorkingDirFromNone
}

// SetDefaultWorkingDir wires the configured last-resort working directory
// (daemon.default_working_dir). Empty is a no-op: "no last resort" is the
// documented default. Nil-safe.
func (d *Dispatcher) SetDefaultWorkingDir(dir string) {
	if d != nil && dir != "" {
		d.defaultWorkingDir = dir
	}
}

// SetFenceController wires the shared fence checker so dispatched sessions
// bind their project path as the sandbox root and honor --nofence.
func (d *Dispatcher) SetFenceController(fc FenceController) {
	if d != nil {
		d.fenceController = fc
	}
}

// configureFence updates the shared fence sandbox for this session's
// working directory and no-fence override. Nil-safe; failures are logged
// and leave fencing blocked rather than silently widening it.
func (d *Dispatcher) configureFence(sessionID, workingPath string, noFence bool) {
	if d.fenceController == nil || workingPath == "" {
		return
	}
	if err := d.fenceController.SetRootPath(workingPath); err != nil {
		d.logger.Warn("fence: failed to set session root; fencing stays blocked until a valid root is set",
			"session", sessionID,
			"root", workingPath,
			"error", err,
		)
	}
	d.fenceController.SetNoFence(noFence)
}

// SetInstructionStore wires the instruction store for intent-based action attachment.
func (d *Dispatcher) SetInstructionStore(store *preferences.Store) {
	if store == nil {
		return
	}
	d.instructionStore = store
}

// SetInstructionParser wires the instruction parser for parsing NL automation requests.
func (d *Dispatcher) SetInstructionParser(parser *InstructionParser) {
	if parser == nil {
		return
	}
	d.instructionParser = parser
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
	case IntentPlan, IntentQuickPlan:
		// quickplan is autonomous plan-execution: same depth bar as plan
		// (bughunt 2026-09-10 L2).
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
//
// requestModel carries an optional client-supplied per-request model ref
// (chat.request "model": alias name or "provider/model-id"). It lands on
// DispatchResult.RequestModel and flows to the executor loop via
// RouteToAgent's one-shot SetModelOverride application. Empty = the turn
// runs the alias/default chain unchanged.
func (d *Dispatcher) ClassifyAndRoute(ctx context.Context, input, sessionID string, parts []llm.ContentPart, agentOverride, requestModel string) (*DispatchResult, error) {
	d.logger.Debug("Dispatching request",
		"session", sessionID,
		"input_len", len(input),
		"parts_count", len(parts),
	)

	// Resolve thread-specific conversation ID.
	// The thread router converts the session-level sessionID into a
	// thread-scoped conversationID based on topic detection. Downstream
	// memory retrieval and routing use the thread-specific ID, while
	// session-level operations (session tracker) continue using sessionID.
	conversationID := sessionID
	if d.threadRouter != nil {
		convID, err := d.threadRouter.GetThreadConversationID(ctx, sessionID, input)
		if err != nil {
			d.logger.Debug("Thread routing failed, falling back to session ID",
				"session", sessionID, "error", err)
		} else {
			conversationID = convID
		}
	}

	if conversationID != sessionID {
		d.logger.Debug("Thread routing resolved conversation ID",
			"session", sessionID, "conversation", conversationID)
	}

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
			return d.routeToPlan(ctx, desc, planIntent, conversationID)
		}
		skillName, skillInput := d.parseSkillInvocation(input)
		if skill := d.getSkill(skillName); skill != nil {
			d.logger.Info("Skill invocation detected",
				"skill", skillName,
				"session", sessionID,
			)
			return d.executeSkill(ctx, skill, skillInput, conversationID)
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

	// agentOverrideApplied tracks whether step 5.3 applied a client agent
	// override. RouteToAgent reads the flag on DispatchResult so that
	// intent-Type shortcuts (platform introspection) don't swallow
	// explicitly-overridden turns (researcher-extract e2e, 2026-09-12).
	agentOverrideApplied := false

	// 2. Handle clarification if needed
	if parseResult.Found && parseResult.Directive.ClarificationNeeded {
		// Build intent for session tracking (so follow-up inputs are
		// recognized as clarification responses)
		intent := &Intent{
			Type:          string(IntentClarify),
			Confidence:    1.0,
			AgentType:     config.AgentIDChat,
			Summary:       extractSummary(input),
			OriginalInput: input, // full input for lossless resume (M4)
			TrueAnalysis:  nil,   // Model directive clarification, not intent analysis
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

	// 3. Build memory context with thread-aware conversation ID
	memCtx := d.buildMemoryContext(ctx, input, conversationID)

	// 3.25. STAGE-0 embedding prefilter (classifier-observability
	// follow-up): unanimous kNN vote over labeled example vectors routes
	// directly and skips the analyzer + router LLM calls entirely
	// (~0.3s pre-agent path). Any miss or error falls through unchanged,
	// so the prefilter can only skip work, never degrade routing.
	// Compound-signal inputs and skill invocations stay excluded —
	// multi-intent and /-commands need the full chain.
	//
	// AssertOnly mode never routes: the verdict is logged (agreement data
	// vs whatever the LLM chain classifies) and the chain always runs.
	if d.prefilter != nil && agentOverride == "" && !hasCompoundSignalWords(input) {
		pi := d.prefilter.MatchForSession(ctx, input, sessionID)
		// Door-1 verdict capture (classifier-outcome-loop leaf 02): Match
		// stashed its verdict synchronously above. PEEK it (do not
		// consume): recordDispatch — invoked by the handler after this
		// call returns — is the single consume point that persists the
		// margin. The closure below re-stashes the (marked) verdict when
		// a gate suppresses the route, so the marked verdict is what
		// recordDispatch later takes.
		pfVerdict := d.peekStashedPrefilterVerdict()
		// markSuppressed records that a ROUTED vote was held back by a
		// dispatcher gate: Suppressed=true and Routed=false (the vote did
		// not route), keeping the margin for the near-miss harvest. The
		// cue-guard case arrives already Suppressed from Match.
		markSuppressed := func() {
			if pfVerdict != nil {
				pfVerdict.Suppressed = true
				pfVerdict.Routed = false
				d.stashPrefilterVerdict(*pfVerdict)
			}
		}
		// pfMargin guards log lines when no verdict was stashed (a test
		// replaced the prefilter without an observer).
		pfMargin := func() float64 {
			if pfVerdict != nil {
				return pfVerdict.Margin
			}
			return 0
		}
		if pi != nil && d.prefilter.assertOnly {
			d.logger.Info("prefilter assert (not routing; assert_only)",
				"asserted_intent", pi.Type,
				"asserted_agent", pi.AgentType,
				"confidence", pi.Confidence,
				"margin", pfMargin(),
			)
			markSuppressed()
			pi = nil
		}
		// H6 safety gate: the direct route bypasses instruction parsing,
		// multi-intent detection, agentOverride resolution, planning and
		// task creation. Verdicts whose intent would create a task or
		// dispatch async (code/debug/git/plan/write/…) must run through
		// the full chain, so only INLINE intents (chat/status/etc.) are
		// honored here; everything else falls through unchanged.
		if pi != nil && IntentType(pi.Type).ShouldCreateTask() {
			d.logger.Info("prefilter verdict suppressed (task-creating intent; full chain required)",
				"asserted_intent", pi.Type,
				"asserted_agent", pi.AgentType,
				"confidence", pi.Confidence,
				"margin", pfMargin(),
			)
			markSuppressed()
			pi = nil
		}
		if pi != nil && IntentType(pi.Type).ShouldDispatchAsync(IntentType(pi.Type).RequiresPlanning()) {
			d.logger.Info("prefilter verdict suppressed (async-dispatch intent; full chain required)",
				"asserted_intent", pi.Type,
				"asserted_agent", pi.AgentType,
				"confidence", pi.Confidence,
				"margin", pfMargin(),
			)
			markSuppressed()
			pi = nil
		}
		if pi != nil {
			pi.SuggestedMode = suggestMode(IntentType(pi.Type), pi.TrueAnalysis, input)
			d.recordTotalDispatch()
			d.recordClassificationMethod(pi.Method)
			d.recordAgent(pi.AgentType)
			d.recordIntentType(pi.Type)
			d.sessionTracker.RecordIntent(sessionID, pi, pi.AgentType)
			d.logger.Info("Dispatched request (prefilter direct route)",
				"agent", pi.AgentType,
				"intent_type", pi.Type,
				"confidence", pi.Confidence,
				"classification_method", pi.Method,
				"margin", pfMargin(),
				"asserted", pi.Type,
				"memory_refs", len(pi.MemoryRefs),
				"has_task", false,
				"has_model_override", parseResult.Found,
			)
			return &DispatchResult{
				AgentID:          pi.AgentType,
				Intent:           pi,
				MemoryContext:    memCtx.Results,
				ModelDirective:   parseResult.Directive,
				OriginalInput:    input,
				Parts:            parts,
				SuggestedMode:    pi.SuggestedMode,
				PrefilterVerdict: pfVerdict,
			}, nil
		}
		if pfVerdict != nil && !pfVerdict.Suppressed {
			// Abstained with a real vote nearby (the margin this leaf
			// exists to keep): mirror it at dispatcher level with session
			// context — Match's own debug line carries no session.
			d.logger.Debug("prefilter abstain (full chain)",
				"session", sessionID,
				"margin", pfVerdict.Margin,
				"asserted", pfVerdict.AssertedIntent,
			)
		}
	}

	// 3.5. IntentGate-style true intent analysis. The session digest is
	// built BEFORE the call so the analyzer can resolve references against
	// recent session activity (leaf 02 of session-aware-intent-gate).
	if d.intentAnalyzer != nil {
		digest := d.buildSessionContextDigest(sessionID)
		analysis, err := d.intentAnalyzer.AnalyzeTrueIntent(ctx, input, digest)
		if err == nil && analysis != nil {
			// Ambiguity short-circuit (session-continuity A5): when the
			// session carries context (digest non-empty), a terse or
			// reference-heavy input is plausibly a legitimate follow-up —
			// clarification-looping it discards the context the analyzer
			// just resolved references against. Let history-aware
			// classification stand; only genuinely context-less inputs
			// (empty digest) get the clarification prompt.
			if analysis.IsAmbiguous(d.intentAnalyzer.ambiguityThreshold) && digest.IsEmpty() {
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

	// 4.5. Check for instruction input (user creating/defining automation)
	// Per User Instructions spec Phase 2.4
	if d.isInstructionInput(resolvedInput) && d.instructionParser != nil {
		parsed, err := d.instructionParser.Parse(ctx, resolvedInput)
		if err == nil && parsed.Confidence >= 0.5 {
			return &DispatchResult{
				Intent: &Intent{
					Type:       string(IntentInstruction),
					Confidence: parsed.Confidence,
					AgentType:  config.AgentIDChat,
					Summary:    extractSummary(input),
					Method:     "instruction_parser",
				},
				AgentID:     config.AgentIDChat,
				Instruction: parsed,
			}, nil
		}
	}

	// 5. Check for compound (multi-intent) requests. This MUST run before
	// the media-URL guard: a compound-signal message ("summarize the
	// video <url> and then write the summary into notes.md") is genuine
	// multi-work; deterministic ingestion would drop the second intent
	// (routing-repair leaf 04, AR-2).
	multiIntent := d.classifyMultiIntent(ctx, resolvedInput, memCtx)
	if multiIntent.IsCompound {
		return d.routeCompoundWithModel(ctx, multiIntent, input, conversationID, parseResult.Directive)
	}

	// 4.5b. Media-URL guard: a media-CONSUMPTION request carrying a
	// YouTube URL (or a context-qualified bare video ID) is a
	// media-ingest request regardless of how a small model reads the
	// surrounding words — "summarize this video" is analysis, not code.
	// Deterministic structural signal, checked before the LLM
	// classifier: the analyst agent holds the transcript_fetch grant,
	// and the coder (the LLM's habitual choice for anything tool-shaped)
	// does not. mediaGuardVerdict demands media-consumption operation
	// evidence (AR-2), so a coding request that merely cites a URL as
	// fixture/data falls through to the chain below.
	// The SAME gate also runs inside classifyIntent (see above), which
	// the clarification-resume path calls directly; here it runs BEFORE
	// compound detection so a compound-signal message is never swallowed
	// into single-intent ingestion.
	if mediaTarget := mediaGuardVerdict(resolvedInput); mediaTarget != "" {
		d.recordTotalDispatch()
		d.recordClassificationMethod("media_url_guard")
		d.recordAgent(config.AgentIDAnalyst)
		d.recordIntentType(string(IntentAnalyze))
		mediaIntent := &Intent{
			Type:       string(IntentAnalyze),
			Confidence: 0.9,
			AgentType:  config.AgentIDAnalyst,
			Summary:    extractSummary(input),
			Method:     "media_url_guard",
		}
		mediaIntent.SuggestedMode = suggestMode(IntentAnalyze, mediaIntent.TrueAnalysis, input)
		if d.sessionTracker != nil {
			d.sessionTracker.RecordIntent(sessionID, mediaIntent, mediaIntent.AgentType)
		}
		return &DispatchResult{
			AgentID:       mediaIntent.AgentType,
			Intent:        mediaIntent,
			MemoryContext: memCtx.Results,
			OriginalInput: input,
			Parts:         parts,
			SuggestedMode: mediaIntent.SuggestedMode,
		}, nil
	}

	// 4. Classify primary intent
	intent, _ := d.classifyIntent(ctx, resolvedInput, memCtx)

	// 5. Extract memory refs for context continuity
	intent.MemoryRefs = d.extractMemoryRefs(memCtx.Results)

	// 5.1. Propagate TrueAnalysis from IntentGate (if classifyIntent didn't
	// already carry it through). classifyIntent constructs fresh Intent
	// structs via classifiers that don't read memCtx.LastIntent.TrueAnalysis.
	if intent.TrueAnalysis == nil && memCtx.LastIntent != nil {
		intent.TrueAnalysis = memCtx.LastIntent.TrueAnalysis
	}

	// 5.2. Synthesize planning mode (Thread D complexity routing).
	intent.SuggestedMode = suggestMode(IntentType(intent.Type), intent.TrueAnalysis, input)

	// 5.3. Client-specified agent override: if the client explicitly named an
	// agent and it exists in the registry, use it instead of the classified
	// intent's agent. Unknown agents fall back to normal classification.
	// Nil-registry guard: dispatchers built without a Registry must not
	// panic on the override path.
	if agentOverride != "" && d.registry != nil {
		if _, ok := d.registry.GetSpec(agentOverride); ok {
			d.logger.Info("Client agent override applied",
				"override", agentOverride,
				"classified_agent", intent.AgentType,
				"session", sessionID,
			)
			intent.AgentType = agentOverride
			agentOverrideApplied = true
		} else {
			d.logger.Warn("Client agent override ignored: agent not found in registry",
				"override", agentOverride,
				"session", sessionID,
			)
		}
	}

	// 5.5. Check if plan creation is warranted (before task creation)
	if d.planManager != nil && d.planManager.ShouldCreatePlan(intent.Type, 0) {
		return d.routeToPlanWithOverride(ctx, input, intent, conversationID, agentOverrideApplied)
	}

	// 6. Create task if needed (for trackable work). Task creation is
	// gated on the dispatch being asynchronous: the handler's async branch
	// (handler.go:770: ShouldDispatchAsync(result) && result.Task != nil) is
	// the ONLY consumer of the created task, so a synchronous intent that
	// also sets ShouldCreateTask() — IntentSchedule is the standing example:
	// ShouldCreateTask()=true but ShouldDispatchAsync(schedule,
	// RequiresPlanning())=false — would leave an orphaned task row that the
	// gate never picks up and the scheduler never runs (the quickplan
	// dead-end the C-0 wave closed had the same shape). Pair/collaborate
	// routes own their task via the same DispatchResult (they are not
	// async-dispatch but must still be tracked).
	var createdTask *task.Task
	if d.shouldCreateTask(intent) && d.taskStore != nil && d.dispatchConsumesTask(intent) {
		createdTask = d.createTask(ctx, input, intent, sessionID, intent.AgentType)
	}

	// 7. Determine routing
	result := &DispatchResult{
		Task:             createdTask,
		AgentID:          intent.AgentType,
		Intent:           intent,
		MemoryContext:    memCtx.Results,
		ModelDirective:   parseResult.Directive,
		OriginalInput:    input,
		Parts:            parts,
		SuggestedMode:    intent.SuggestedMode,
		ExecutorModelRef: executorModelRefFromDirective(parseResult.Directive),
		// Client-supplied per-request model (chat.request "model").
		// Rides the result so RouteToAgent applies it to the executor
		// loop's one-shot override seam. Empty = no request-level model.
		RequestModel: requestModel,

		AgentOverrideApplied: agentOverrideApplied,
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
		"classification_method", intent.Method,
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

// isInstructionInput detects whether input is a user instruction (automation request).
// Matches keywords like "always", "never", "every time", "whenever", etc.
// Per User Instructions spec Phase 2.4.
func (d *Dispatcher) isInstructionInput(input string) bool {
	instructionKeywords := []string{
		"always", "never", "every time", "whenever",
		"from now on", "remember to", "make sure to",
		"automatically", "auto-", "auto_",
	}
	lower := strings.ToLower(input)
	for _, kw := range instructionKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

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
			Method:     "short_message_guard",
		}, nil
	}

	// Media-URL guard: a media-CONSUMPTION request carrying a YouTube URL
	// (or a context-qualified bare video ID) is a media-ingest request
	// regardless of how a small model reads the surrounding words —
	// "summarize this video" is analysis, not code. Deterministic
	// structural signal, checked before the LLM classifier: the analyst
	// agent holds the transcript_fetch grant, and the coder (the LLM's
	// habitual choice for anything tool-shaped) does not.
	// (AR-2, routing-repair leaf 04: mediaGuardVerdict requires
	// media-consumption operation evidence, so a coding request that
	// merely cites a URL as fixture/data falls through to the chain
	// below instead of being hijacked to the analyst.)
	if mediaTarget := mediaGuardVerdict(input); mediaTarget != "" {
		d.recordClassificationMethod("media_url_guard")
		d.recordAgent(config.AgentIDAnalyst)
		d.recordIntentType(string(IntentAnalyze))
		return &Intent{
			Type:       string(IntentAnalyze),
			Confidence: 0.9,
			AgentType:  config.AgentIDAnalyst,
			Summary:    extractSummary(input),
			Method:     "media_url_guard",
		}, nil
	}

	// Platform-vs-action arbitration (e2e run 2, 2026-09-10): the 8B
	// classifier scored "create a file named hello.txt …" as intent=platform
	// @0.9, and the platform branch answers with the static agent-roster
	// dump — an execution request "completed" without executing anything
	// (A2 roster-dump failure). An IMPERATIVE cannot be an introspection
	// question: introspection ASKS about the platform ("what can you do"),
	// it never INSTRUCTS ("create", "write", "make", "fix", "add"). When the
	// LLM says platform but the input carries an imperative execution verb,
	// skip the platform short-circuit and let the chain continue — the LLM
	// result is not consumed, so the capability matcher / keyword /
	// heuristic routes route the verb to an executor agent.
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
				Method:     "capability_matcher",
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
			// Platform-vs-action arbitration (e2e run 2, 2026-09-10): the
			// 8B classifier scored "create a file named hello.txt …" as
			// intent=platform @0.9, and a platform verdict short-circuits
			// to the static agent-roster dump — an execution request
			// "completed" without executing anything (A2 roster failure).
			// An IMPERATIVE cannot be an introspection question:
			// introspection ASKS about the platform ("what can you do"),
			// it never INSTRUCTS ("create a file", "write", "make",
			// "fix"). When the LLM says platform but the input opens with
			// an imperative execution verb, discard the platform verdict
			// and let the chain continue — the keyword/heuristic routes
			// send the verb to an executor agent. Log the override so
			// classifier-observability can measure how often the 8B makes
			// this mistake.
			//
			// Schedule-extension (e2e run 8, 2026-09-10): the same 8B also
			// scored the identical phrasing intent=schedule @0.8 — run-8
			// variant of the same mistake. A schedule verdict demands TIME
			// signals ("tomorrow", "at 5pm", "remind me", "timer"); an
			// imperative with none is not a schedule request. Both
			// non-executable verdicts share one arbitration.
			// Branch ORDER is load-bearing (bughunt 2026-09-12 F41): a
			// status/recall question OUTRANKS an imperative-looking first
			// token. "update me: did the file get created?" satisfies
			// BOTH matchers (hasLeadingImperativeVerb on "update",
			// isWorkStatusRecall on "did … file"), and while the
			// imperative branch was tested first it fired, cleared the
			// intent, and the session-aware recall route added by
			// 16f1f8a2/c6e6f336 never ran. The recall matchers run FIRST;
			// a genuine imperative that is not a work-status question
			// ("create a file named hello.txt") still falls through to
			// the imperative override below.
			// The recall matchers are POSITION-INDEPENDENT (isWorkStatusRecall
			// scans every field for a status predicate plus a nearby work
			// noun), so evaluated against the whole input they also fire on
			// an imperative work request whose status clause sits in the
			// tail ("implement the endpoint and check that the response is
			// this format" reads as a work-status question because "is …
			// this" appears after the " and "). So when — and ONLY when — the
			// input opens with an execution imperative we narrow the match to
			// the leading clause. Every other input keeps its WHOLE-INPUT
			// match, because a genuine recall question frequently puts a
			// conversational separator in front of the status phrase
			// ("hey, did the change get made?", "ok, what files did you
			// create?", "check the log at /tmp/x,y.log, did the change get
			// made?"). Narrowing unconditionally — the wave-2 regression —
			// reversed the arbitration for exactly those inputs: any
			// separator before the status phrase killed the recall match and
			// let the untrusted platform/git/schedule verdict survive (the
			// F40/run-10 contextless-committer failure and the run-5 A2
			// roster failure the guard exists to close).
			// ...and ONLY then narrow, and only for an input whose head is a
			// plain execution imperative. Two head shapes must NOT narrow,
			// because the imperative-looking first token IS the recall
			// request's own lead-in (wave-3 regression review of 9af23f86):
			//
			//  1. an imperative whose OBJECT is a pronoun ("update me, did
			//     the file get created?", "run me through it, …", "build me a
			//     summary, …", "please update me, …"). The old gate keyed on
			//     hasLeadingImperativeVerb alone, and update/run/build/make/
			//     fix/add/set are simultaneously the natural lead-ins for a
			//     recall request, so narrowing to "update me" killed the
			//     recall match and let the untrusted verdict survive. For
			//     IntentGit that is worse than a misroute: the imperative
			//     salvage below covers only platform/schedule, so a surviving
			//     git verdict reaches async git dispatch — the contextless
			//     committer this guard exists to prevent.
			//  2. an input with a later clause that is ITSELF a work-status
			//     question ("fix the test, did the file get created?", "set
			//     up the report, is the task done?"). The status question
			//     lives after the boundary, so the leading clause legitimately
			//     holds no recall predicate; narrowing there deletes the real
			//     question. The control still narrows because its tail clause
			//     opens with an instruction, not a status predicate ("implement
			//     the endpoint. check that the response is this format").
			recall := isWorkStatusRecall(input) || isSecondPersonWorkRecall(input)
			if recall && narrowsRecallToLeadingClause(input) {
				recall = isWorkStatusRecall(leadingRecallClause(input)) ||
					isSecondPersonWorkRecall(leadingRecallClause(input))
			}
			if (intent.Type == string(IntentPlatform) ||
				(intent.Type == string(IntentSchedule) && !hasTimeSignal(input)) ||
				(intent.Type == string(IntentGit) && !inputContainsGitVerb(input))) && recall {
				// Platform-vs-recall arbitration (e2e run 5, 2026-09-10):
				// "what files did you make for me?" scored platform @0.9 →
				// roster dump. A question about the ASSISTANT'S OWN past
				// actions is recall/report material; route it to chat with
				// the recall flavor so the session context answers it.
				// Schedule extension (e2e run 9, 2026-09-10): "did the
				// change get made? where is the file?" scored schedule on
				// the 1.2B SFT — a time-signal-free schedule verdict on a
				// work-status question is the same credibility failure.
				d.logger.Info("Platform verdict overridden by second-person work recall",
					"llm_confidence", intent.Confidence,
					"input_len", len(input),
				)
				d.recordClassificationMethod("platform_recall_arbitration")
				// Shared recording tail (bughunt 2026-09-12 F95): every
				// sibling return path records the agent + intent so
				// by_agent/by_intent count this arbitration; without it
				// the recall override was invisible to the
				// classifier-observability counters the wave added for
				// exactly this path.
				d.recordAgent(config.AgentIDChat)
				d.recordIntentType(string(IntentRecall))
				// Confidence 0.85 is a DOCUMENTED constant, not the
				// discarded verdict's number: this path REPLACES an
				// untrusted platform/schedule/git costume (the 8B scored
				// it 0.9+) with the inline recall route, which is answered
				// from session context and is not threshold-gated.
				// Reusing the over-stated verdict value would launder the
				// misclassification into the confidence.
				recall := &Intent{
					Type:       string(IntentRecall),
					Confidence: 0.85,
					AgentType:  config.AgentIDChat,
					Summary:    extractSummary(input),
					Method:     "platform_recall_arbitration",
					Model:      d.llmClassifier.ResolvedModel(),
				}
				return d.applyContextWeighting(recall, memCtx, input), nil
			} else if (intent.Type == string(IntentPlatform) || (intent.Type == string(IntentSchedule) && !hasTimeSignal(input))) &&
				(hasLeadingImperativeVerb(input) || isInterrogativeNonImperativeQuestion(input)) {
				d.logger.Info("Classifier verdict overridden by imperative execution phrasing",
					"verdict", intent.Type,
					"llm_confidence", intent.Confidence,
					"input_len", len(input),
				)
				d.recordClassificationMethod("platform_action_arbitration")
				intent = nil
			} else if d.gitVerbAgreementVeto &&
				intent.Type == string(IntentGit) &&
				!inputContainsGitVerb(input) &&
				hasLeadingImperativeVerb(input) {
				// Git-verb agreement veto (issue #46, bench gate
				// 2026-09-15): the 350M prompt-router scored "Create a
				// file named X in the repository root …" intent=git
				// @0.91 — the word "repository" pulls the prompt into
				// the git lane (confirmed by direct encoder probing:
				// "current directory" routes tooluse, "repository root"
				// routes git, deterministic). A git verdict on an
				// imperative that names NO git action is a lexical
				// bias, and the committer runs contextless — worse
				// than a misroute. Discard the verdict so the chain
				// continues; the recall branch above keeps its
				// priority (F41 branch order).
				d.logger.Info("git verdict vetoed (no git verb in imperative input)",
					"verdict", intent.Type,
					"llm_confidence", intent.Confidence,
					"input_len", len(input),
				)
				d.recordClassificationMethod("git_verb_agreement_veto")
				intent = nil
			} else if intent.Type == string(IntentChat) &&
				hasLeadingImperativeVerb(input) &&
				inputMentionsWorkArtifact(input) {
				// Chat-vs-imperative arbitration (e2e run 3, 2026-09-19):
				// the 8B scored "create a file named hello.txt …, then tell
				// me the full path" intent=chat @0.9 — the catch-all lane
				// with the LOWEST threshold (0.50) wins on raw confidence
				// and the turn deflected ("the tool ran, but the result
				// came back as raw data") with zero work done. A leading
				// imperative verb plus a concrete work artifact is not
				// small talk: discard the chat verdict so the chain
				// continues (keyword "create a file" → code @0.8 routes to
				// the coder). Threshold hardening alone cannot fix this —
				// the 8B emits 0.9+ on every lane it lands in (see also
				// platform/git/schedule arbitrations above; same family).
				d.logger.Info("Chat verdict overridden by imperative work phrasing",
					"verdict", intent.Type,
					"llm_confidence", intent.Confidence,
					"input_len", len(input),
				)
				d.recordClassificationMethod("chat_imperative_arbitration")
				intent = nil
			} else if ShouldUseLLMResult(intent) &&
				quickPlanCueUpgradeApplies(intent.Type, input) {
				// QuickPlan cue upgrade (#52, campaign 20260918 phase 2):
				// the 8B scattered 17/20 adjudicated quickplan replay cases
				// across debug/review/analyze/plan/code — the input's
				// orchestration evidence ("using subagents, review … and
				// correct them as you find them", "implement the plan")
				// lexically pulls toward those lanes while the quickplan
				// description carries no surface forms. When the verdict
				// lands in the scatter set AND the input carries STRONG
				// adjudicated quickplan cues (QuickPlanCuePattern plus the
				// strong-form requirement shared with heuristicFallback),
				// the verdict is a lexical costume: upgrade to quickplan.
				// Positive-signal gated on the ADJUDICATED forms only —
				// the same discipline as the git-verb veto's
				// inputContainsGitVerb.
				d.logger.Info("LLM verdict upgraded to quickplan by orchestration cue evidence",
					"verdict", intent.Type,
					"llm_confidence", intent.Confidence,
					"input_len", len(input),
				)
				d.recordClassificationMethod("quickplan_cue_upgrade")
				upgraded := &Intent{
					Type:             string(IntentQuickPlan),
					Confidence:       0.85,
					AgentType:        "orchestrator",
					RequiresPlanning: true,
					Summary:          extractSummary(input),
					Method:           "quickplan_cue_upgrade",
					Model:            d.llmClassifier.ResolvedModel(),
				}
				return d.applyContextWeighting(upgraded, memCtx, input), nil
			} else if ShouldUseLLMResult(intent) {
				d.logger.Debug("LLM classifier succeeded",
					"intent", intent.Type,
					"confidence", intent.Confidence,
				)
				d.recordClassificationMethod("llm")
				d.recordAgent(intent.AgentType)
				d.recordIntentType(intent.Type)
				intent.Method = "llm"
				// Provenance (leaf 01 of classifier-observability): the
				// model that actually served this classification, including
				// any failover rotation. Empty when unknown — honest.
				intent.Model = d.llmClassifier.ResolvedModel()
				return d.applyContextWeighting(intent, memCtx, input), nil
			} else {
				d.logger.Debug("LLM classifier result below threshold",
					"intent", intent.Type,
					"confidence", intent.Confidence,
					"threshold", GetThresholdForIntent(intent.Type),
				)
			}
		} else if err != nil {
			kind := llm.ClassifyClassificationFailure(err)
			d.logger.Warn("LLM classifier failed, trying keyword",
				"error", err,
				"failure_kind", kind,
			)
			// An empty LLM response usually means the local model is
			// degraded, not that the input is ambiguous. The keyword table
			// is verb-dense and misclassifies task-shaped prompts as skill
			// requests (meept-bench smoke run, 2026-08-24), so when the LLM
			// path is down we route to chat — the chat agent has the full
			// tool registry and can do the work itself. Keyword matching
			// stays available only for the explicit high-confidence platform
			// patterns handled in Step 1.
			if kind == llm.ClassificationFailureEmptyResponse {
				d.recordClassificationMethod("llm_empty_fallback_chat")
				d.recordAgent(config.AgentIDChat)
				d.recordIntentType(string(IntentChat))
				return &Intent{
					Type:       string(IntentChat),
					Confidence: 0.6,
					AgentType:  config.AgentIDChat,
					Summary:    extractSummary(input),
					Method:     "llm_empty_fallback_chat",
				}, nil
			}
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
			intent.Method = "keyword"
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
				Type: string(match.IntentType),
				// RequiresPlanning mirrors the method's rule (bughunt
				// 2026-09-10 C1): quickplan matches need the struct
				// field set or the handler's async gate never opens and
				// the created task is orphaned. Other types are
				// unaffected — the method verdict is preserved.
				Confidence:       match.Confidence,
				AgentType:        match.IntentType.DefaultAgent(),
				RequiresPlanning: match.IntentType.RequiresPlanning(),
				Summary:          extractSummary(input),
			}
			d.recordClassificationMethod("semantic")
			intent.Method = "semantic"
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
		heuristic.Method = "heuristic_fallback"
		return d.applyContextWeighting(heuristic, memCtx, input), nil
	}

	// Step 5: Final fallback — quickplan: clarify-if-needed, plan, execute
	d.recordFallback(input, "all_classifiers_failed", 0.0, "orchestrator")
	d.recordClassificationMethod("fallback")
	d.recordAgent("orchestrator")
	d.recordIntentType(string(IntentQuickPlan))
	return &Intent{
		Type:       string(IntentQuickPlan),
		Confidence: 0.3,
		AgentType:  "orchestrator",
		// RequiresPlanning opens the async-dispatch gate (bughunt
		// 2026-09-10 C1): ShouldDispatchAsync reads this struct field for
		// quickplan, so without it the handler routes quickplan fallbacks
		// to a chat loop and orphans the created task. Mirrors the
		// keyword producers' RequiresPlanning: p.planning pattern.
		RequiresPlanning: true,
		Summary:          "Could not determine intent; planning and executing with clarification as needed",
		Method:           "fallback",
	}, nil

}

// validModes is the set of accepted SuggestedMode values.
var validModes = map[string]struct{}{
	"direct":     {},
	"plan":       {},
	"quick_plan": {},
	"spec_plan":  {},
	"spec_pair":  {},
}

// validateMode returns the mode if valid, empty string otherwise.
func validateMode(s string) string {
	if _, ok := validModes[s]; ok {
		return s
	}
	return ""
}

// suggestMode synthesizes the planning mode from intent type, optional
// analyzer suggestion, and input length. Pure function — unit-testable
// without a dispatcher.
//
// Priority:
//  1. IntentCompound → "spec_pair" (forced)
//  2. analysis.SuggestedMode (if valid)
//  3. intentType.SuggestedMode() (rule-based fallback)
//  4. Short-input downgrade: if input < 50 chars, mode is "direct"
//     (unless analysis explicitly overrode to spec_plan)
func suggestMode(intentType IntentType, analysis *TrueIntentAnalysis, input string) string {
	// quickplan never short-input downgrades: the explicit execution
	// phrasing IS the signal.
	if intentType == IntentQuickPlan {
		return "quick_plan" // explicit execution phrasing IS the signal;
		// never short-input downgraded
	}
	if intentType == IntentCompound {
		return "spec_pair"
	}
	analysisMode := ""
	if analysis != nil {
		analysisMode = validateMode(analysis.SuggestedMode)
	}
	if analysisMode != "" {
		// Short-input downgrade does NOT override an explicit spec_plan
		// suggestion from the analyzer.
		if analysisMode == "spec_plan" {
			return "spec_plan"
		}
		// For other analyzer-suggested modes, apply short-input downgrade.
		if len(input) < 50 {
			return "direct"
		}
		return analysisMode
	}
	mode := intentType.SuggestedMode()
	if mode != "spec_plan" && len(input) < 50 {
		return "direct"
	}
	return mode
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
//
// Results are pre-sorted by relevance (descending). The absolute 0.3 gate
// guards against noise injection, but normalized BM25 scores are
// query-dependent: short queries against long memories routinely score the
// TRUE best match at 0.15–0.25, and dropping it means the single most
// relevant memory never reaches the conversation (observed: stored codeword
// scored 0.203 as the top hit and was silently discarded). Policy: keep the
// 0.3 bar for the tail, but always inject the top-ranked result when it
// clears a low sanity floor (score > 0.1 means FTS actually matched).
func (d *Dispatcher) extractMemoryRefs(results []memory.MemoryResult) []string {
	refs := make([]string, 0, len(results))
	for i, r := range results {
		score := r.RelevanceScore
		if score > memoryInjectionThreshold ||
			(i == 0 && score > memoryTopResultFloor) {
			refs = append(refs, r.Memory.ID)
		}
	}
	return refs
}

const (
	// memoryInjectionThreshold is the absolute relevance bar for injecting
	// any memory into a new conversation.
	memoryInjectionThreshold = 0.3
	// memoryTopResultFloor is the reduced bar for the single top-ranked
	// result — the best match for the query is injection-worthy even when
	// normalized BM25 lands low (see extractMemoryRefs).
	memoryTopResultFloor = 0.1
)

// routeToPlan creates a plan via the PlanManager and returns a DispatchResult
// with the created plan. This is used for plan-eligible requests that should
// go through the planning workflow instead of direct task creation.
func (d *Dispatcher) routeToPlan(ctx context.Context, input string, intent *Intent, sessionID string) (*DispatchResult, error) {
	return d.routeToPlanWithOverride(ctx, input, intent, sessionID, false)
}

// routeToPlanWithOverride is routeToPlan with the client-override flag.
// agentOverrideApplied records that a client agent override was applied
// upstream (step 5.3) and must survive plan routing.
func (d *Dispatcher) routeToPlanWithOverride(ctx context.Context, input string, intent *Intent, sessionID string, agentOverrideApplied bool) (*DispatchResult, error) {
	summary := intent.Summary
	if summary == "" {
		summary = truncateString(input, 100)
	}

	p, err := d.planManager.CreatePlan(ctx, summary, input, "", "", sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to create plan: %w", err)
	}

	// Advance the lifecycle past draft (issue #40 item 1): plans created on
	// the daemon dispatch path previously stayed in state='draft' forever
	// because SubmitPlan was only reachable from the TUI/HTTP paths. Submit
	// immediately — when plans.approval.require_approval is false the plan
	// auto-approves (pending_approval -> approved -> synthesized task
	// hierarchy); when true it parks at pending_approval for the operator's
	// approve/reject decision. A submit failure must not fail the dispatch:
	// the plan row exists and the approval paths can still act on it.
	if err := d.planManager.SubmitPlan(ctx, p.ID); err != nil {
		d.logger.Warn("failed to submit plan after creation",
			"plan_id", p.ID,
			"error", err,
		)
	} else if refreshed, err := d.planManager.GetPlan(ctx, p.ID); err == nil {
		p = refreshed
	}

	d.logger.Info("Routed request to plan",
		"plan_id", p.ID,
		"title", p.Title,
		"state", p.State,
		"session", sessionID,
	)

	// Quickplan carry-through (bughunt 2026-09-10 M3): a quickplan intent
	// routed here (plans config always-plan, or threshold) must keep
	// executing, not strand the draft behind a "plan created" text reply.
	// Create the task and leave Response empty so the handler's
	// async_dispatch gate (ShouldDispatchAsync && Task != nil) publishes
	// orchestrator.plan with SuggestedMode quick_plan; RequiresPlanning on
	// the intent makes that gate reachable. Non-quickplan intents keep the
	// legacy text response.
	if IntentType(intent.Type) == IntentQuickPlan {
		created := d.createTask(ctx, input, intent, sessionID, intent.AgentType)
		// Preserve a client agent override (researcher-extract e2e,
		// 2026-09-15): the quickplan path previously hard-coded the
		// planner as the DispatchResult agent, discarding the override —
		// the researcher/analyst assignment was lost and the strategic
		// planner re-picked agents for the synthesized subtasks. When an
		// override named a specific executor, keep it as the dispatch
		// agent and flag the result so downstream override handling
		// (RouteToAgent's shortcut skip) still applies.
		resultAgent := intent.AgentType
		if resultAgent == "" || resultAgent == config.AgentIDChat {
			resultAgent = config.AgentIDPlanner
		}
		return &DispatchResult{
			AgentID:              resultAgent,
			Task:                 created,
			Intent:               intent,
			Plan:                 p,
			SuggestedMode:        string(IntentQuickPlan.SuggestedMode()),
			AgentOverrideApplied: agentOverrideApplied,
		}, nil
	}

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

	// PendingMode seeding (quickplan-mode leaf 02): when the caller's
	// intent type resolves to quickplan, record "quick_plan" so the
	// post-clarification resume re-enters execution in quickplan mode
	// instead of re-classifying from scratch. Empty for all other
	// intents — the ambiguity gate is unchanged for them.
	pendingMode := ""
	if IntentType(analysis.Category) == IntentQuickPlan {
		pendingMode = string(IntentQuickPlan.SuggestedMode())
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
		Type:          string(IntentClarify),
		Confidence:    analysis.Confidence,
		AgentType:     config.AgentIDChat,
		Summary:       extractSummary(input),
		OriginalInput: input, // full input for lossless resume (M4)
		TrueAnalysis:  analysis,
		SuggestedMode: pendingMode,
	}

	// Record for analytics
	d.recordClassificationMethod("intent_analyzer")
	d.recordAgent(config.AgentIDChat)
	d.recordIntentType(string(IntentClarify))

	// Record the pending clarification (quickplan-mode leaf 02): the
	// ambiguity gate must leave the clarify intent as the session's last
	// intent so the user's answer re-enters via ResumeAfterClarification
	// with PendingMode preserved — mirroring the model-directive
	// clarification path in ClassifyAndRoute step 2.
	if d.sessionTracker != nil {
		d.sessionTracker.RecordIntent(sessionID, intent, intent.AgentType)
	}

	d.logger.Info("Requesting clarification",
		"ambiguity", analysis.Ambiguity,
		"category", analysis.Category,
		"questions", len(questions),
	)

	return &DispatchResult{
		AgentID:             config.AgentIDChat,
		Intent:              intent,
		ClarificationReply:  sb.String(),
		ClarificationNeeded: true,
	}, nil
}

// pendingClarification holds the state needed to resume after a clarification.
type pendingClarification struct {
	OriginalInput string              `json:"original_input"`
	Analysis      *TrueIntentAnalysis `json:"analysis"`
	SessionID     string              `json:"session_id"`
	// PendingMode is the SuggestedMode the pre-clarification dispatch had
	// synthesized ("" for legacy/clarify flows). When the pending intent
	// was quickplan this is "quick_plan", and the resumed dispatch must
	// carry it so the clarification answer completes the plan instead of
	// downgrading to plain chat.
	PendingMode string `json:"pending_mode,omitempty"`
}

// maxCombinedClarificationLen caps the combined input to prevent
// unbounded growth when clarification retries accumulate context.
const maxCombinedClarificationLen = 32_000

// ResumeAfterClarification re-classifies a user input that is a response to a
// previous clarification request. It combines the original input with the user's
// response and re-runs intent analysis. If the combined input is still ambiguous,
// it asks follow-up questions. Otherwise, it routes normally.
func (d *Dispatcher) ResumeAfterClarification(ctx context.Context, originalInput, userResponse, sessionID string) (*DispatchResult, error) {
	combinedInput := originalInput + "\n\nUser clarification: " + userResponse

	// Pending mode (quickplan-mode leaf 02): when the clarification was
	// seeded with PendingMode="quick_plan", the resumed dispatch must
	// carry SuggestedMode="quick_plan" — the clarification answer
	// completes the plan; it never downgrades to plain chat. Read BEFORE
	// any clearPendingClarification call below.
	pendingMode := ""
	if pending := d.getPendingClarification(sessionID); pending != nil {
		pendingMode = pending.PendingMode
	}

	// Guard against unbounded growth from repeated clarification retries.
	if len(combinedInput) > maxCombinedClarificationLen {
		d.logger.Warn("Clarification combined input too large, clearing and classifying raw",
			"session", sessionID,
			"combined_len", len(combinedInput),
		)
		d.clearPendingClarification(sessionID)
		// Classify just the user's latest response to break the cycle.
		// (Parts and the one-shot request model do not survive clarification.)
		return d.ClassifyAndRoute(ctx, userResponse, sessionID, nil, "", "")
	}

	d.logger.Info("Resuming after clarification",
		"session", sessionID,
		"combined_len", len(combinedInput),
	)

	// Re-analyze the combined input with the intent analyzer. The session
	// digest is built BEFORE the call so the analyzer can resolve references
	// against recent session activity (leaf 02 of session-aware-intent-gate).
	if d.intentAnalyzer != nil {
		digest := d.buildSessionContextDigest(sessionID)
		analysis, err := d.intentAnalyzer.AnalyzeTrueIntent(ctx, combinedInput, digest)
		if err == nil && analysis != nil {
			// A5 gate (mirrors ClassifyAndRoute 3.5): with session context
			// present, an ambiguous re-analysis of the user's answer should
			// proceed with history-aware classification rather than loop
			// another clarification prompt. Empty digest = context-less
			// session; keep the follow-up prompt there.
			//
			// Emptiness ignores the clarify marker (bughunt 2026-09-10 M2):
			// buildClarificationResult records the clarify intent BEFORE this
			// digest is rebuilt, so plain IsEmpty() is always false here and
			// the follow-up branch could never re-fire. Only REAL context
			// (task fields, non-clarify intents) should suppress the
			// follow-up question.
			if analysis.IsAmbiguous(d.intentAnalyzer.ambiguityThreshold) && digest.IsEmptyIgnoringClarify() {
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

			// Preserve the pre-clarification mode (quickplan-mode leaf
			// 02): a pending quickplan clarification resumes in
			// quick_plan mode so the post-clarification turn executes
			// without a new approval gate. All other pending flows keep
			// the previous behavior (no mode synthesized here).
			if pendingMode == "quick_plan" {
				intent.SuggestedMode = "quick_plan"
			}

			// Check for plan routing.
			if d.planManager != nil && d.planManager.ShouldCreatePlan(intent.Type, 0) {
				return d.routeToPlan(ctx, combinedInput, intent, sessionID)
			}

			// Create task if needed (for trackable work). Same shared gate
			// as the primary route: a task is created only when dispatch
			// actually consumes it, so an ambiguous question answered with a
			// schedule intent does not leave a task row no handler branch
			// reads (the orphan the C-0 wave closed on the primary route).
			var createdTask *task.Task
			if d.shouldCreateTask(intent) && d.taskStore != nil && d.dispatchConsumesTask(intent) {
				createdTask = d.createTask(ctx, combinedInput, intent, sessionID, intent.AgentType)
			}

			result := &DispatchResult{
				Task:          createdTask,
				AgentID:       intent.AgentType,
				Intent:        intent,
				MemoryContext: memCtx.Results,
				OriginalInput: combinedInput,
				SuggestedMode: intent.SuggestedMode,
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
	// original user turn. The request model is likewise one-shot and does not
	// survive a clarification round-trip ("" = alias/default chain).
	d.clearPendingClarification(sessionID)
	result, err := d.ClassifyAndRoute(ctx, combinedInput, sessionID, nil, "", "")
	// Pending-mode preservation (quickplan-mode leaf 02): the analyzer-less
	// fallback must not downgrade a pending quickplan clarification either.
	if err == nil && result != nil && pendingMode == "quick_plan" {
		result.SuggestedMode = "quick_plan"
		if result.Intent != nil {
			result.Intent.SuggestedMode = "quick_plan"
		}
	}
	return result, err
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
	// Reconstruct the original input: prefer the full untruncated input
	// carried on the clarify intent (bughunt 2026-09-10 M4); fall back to
	// the summary for pre-existing in-memory records that predate the
	// OriginalInput field. PendingMode rides on the clarify intent's
	// SuggestedMode: the ambiguity gate seeds it there so the resumed
	// dispatch can restore the pre-clarification mode (quick_plan) after
	// the user answers.
	original := lastIntent.OriginalInput
	if original == "" {
		original = lastIntent.Summary
	}
	return &pendingClarification{
		OriginalInput: original,
		Analysis:      lastIntent.TrueAnalysis,
		SessionID:     sessionID,
		PendingMode:   lastIntent.SuggestedMode,
	}
}

// clearPendingClarification removes the clarification state for a session
// by recording a neutral intent so isPendingClarification returns false.
func (d *Dispatcher) clearPendingClarification(sessionID string) {
	if d.sessionTracker == nil {
		return
	}
	// Record a non-clarify intent to break the clarification detection
	// loop. Without this, isPendingClarification keeps returning true
	// and ResumeAfterClarification recurses infinitely.
	d.sessionTracker.RecordIntent(sessionID, &Intent{
		Type:       string(IntentChat),
		Confidence: 1.0,
		AgentType:  config.AgentIDChat,
		Summary:    "(clarification cleared)",
	}, config.AgentIDChat)
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

// dispatchConsumesTask reports whether the created task will actually be
// consumed downstream, i.e. whether some branch RUNS it. Two do: the
// async-dispatch branch (handler.go: ShouldDispatchAsync(result) &&
// result.Task != nil — the only branch that drives the task to completion) and
// the collaboration route (startCollaborationSession, which reads
// result.Task.ID for the collaborative task and falls back to the conversation
// ID when Task is nil). A task created for a synchronously dispatched intent is
// never run — the row is orphaned the moment it is written (IntentSchedule:
// ShouldCreateTask()=true, ShouldDispatchAsync()=false). Gate creation on
// this, not on shouldCreateTask alone.
//
// Result.Task is also READ, without being consumed, on three dispatcher paths:
// buildContextMessage (called from RouteToAgent at dispatcher.go:2510) names
// the new task in the context message and excludes it from the digest,
// recordInteraction stores its id in the interaction metadata, and
// recordDispatch — the routing record — stores it as task_id. Those reads are
// why the field stays populated for consumed tasks; they do not make an
// UNCONSUMED task meaningful, and they are not a reason to widen this
// predicate. (An earlier revision of this comment named only the handler
// branches and the compound/plan path, which mis-stated the reader set.)
//
// Residual gap against the handler's own ShouldDispatchAsync, documented not
// redesigned (LOW): this predicate delegates to
// IntentType.ShouldDispatchAsync(RequiresPlanning), which does NOT model the
// handler's extra `result.Response != ""` refusal — that early return is what
// keeps SKILL results inline (the inline branch is tested FIRST at
// handler.go:728). So an async-true intent that also carries a non-empty
// Response still writes a task row while the handler answers it inline. The
// row is not run by the async branch; only a non-empty Response distinguishes
// it, and threading DispatchResult.Response through this predicate is a wider
// change than the orphan-task gate needs.
//
// No pair/collaborate special case: both intents are in ShouldDispatchAsync's
// unconditional true case, and IntentPair never reaches task creation anyway
// (IntentPair.ShouldCreateTask()==false and RequiresPlanning()==false), so the
// plain async rule below already answers true for each. An earlier revision
// carried explicit arms for both; they were dead — deleting them leaves this
// predicate's value identical for every intent — and their stated premise
// (that the pair route reads Result.Task) was wrong: the pair route uses only
// AgentID and Intent.Summary.
func (d *Dispatcher) dispatchConsumesTask(intent *Intent) bool {
	return IntentType(intent.Type).ShouldDispatchAsync(intent.RequiresPlanning)
}

// createTask creates a new task for the request. The agentID is the agent
// that will handle this task; it is persisted on the task via
// WithAssignedAgent so downstream consumers know the routing decision at
// dispatch time.
func (d *Dispatcher) createTask(_ context.Context, input string, intent *Intent, sessionID, agentID string) *task.Task {
	// Create task summary
	summary := intent.Summary
	if summary == "" {
		summary = truncateString(input, 100)
	}

	t := task.NewTask(summary, input)
	t.WithAssignedAgent(agentID)
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
		if laneForcesSequentialCompound(IntentType(intent.Type)) {
			m.CompoundType = "sequential"
			return true
		}
	}
	m.CompoundType = "parallel"
	return true
}

// laneForcesSequentialCompound reports whether a lane would force its compound
// request into SEQUENTIAL execution if anything consumed CompoundType. It is
// deliberately keyed on the LANE SET, not on Intent.RequiresPlanning: that flag
// answers a different question ("does this lane dispatch asynchronously?"), it
// is true for `code` (the C-0 async-gate rule), and keying CompoundType off it
// flipped an independent code+* pair from parallel to sequential the moment the
// LLM classifier started deriving the flag from IntentType.RequiresPlanning()
// (bughunt 2026-09-12 wave). The set matches what the keyword producer has
// always declared via its planning column (plan/architect/collaborate) plus the
// campaign's planning lanes (quickplan/compound).
//
// The value this fixes is currently INERT: CompoundType is written to the
// MultiIntent, logged, and copied into the parent task's metadata JSON, but
// nothing branches on it — handler.go reads meta["compound_type"] into a
// PlanRequest field that no subscriber decodes, and strategic.go gates the
// compound plan on IsCompound / Intent==compound only. So neither the wave-1
// "regression" nor this set changes any behaviour today; the set is kept as the
// corrected intent record, not because a consumer reads it.
//
// IntentCompound is a dead member of the set: DetectCompound runs over the
// per-fragment intents produced by the keyword classifier and the LLM
// multi-intent classifier, and classifierLanes — the single source of truth for
// what the LLM may emit — never lists "compound". No producer emits a compound
// fragment, so the case cannot match.
func laneForcesSequentialCompound(t IntentType) bool {
	switch t {
	case IntentPlan, IntentArchitect, IntentCollaborate, IntentQuickPlan, IntentCompound:
		return true
	default:
		return false
	}
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

	// Create a parent task to track the compound request.
	// Compound tasks are always assigned to the orchestrator (matches
	// the AgentID on the returned DispatchResult).
	//
	// The description is the FULL user input, not multi.Summary: the task
	// description is what flows to the strategic planner (PlanRequest.Input)
	// and into every fallback/pair step prompt. extractSummary truncates to
	// ~100 chars, which cut off the second clause of compound requests
	// ("...then tell me the full path" vanished — e2e T1 2026-09-10), so
	// executors never saw the whole request. OriginalInput below already
	// carries the full input on the DispatchResult; the persisted task must
	// match it.
	parentTask := d.createTask(ctx, input, &Intent{
		Type:    string(IntentCompound),
		Summary: multi.Summary,
	}, sessionID, "orchestrator")

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
			Method:  "compound",
		},
		OriginalInput: input,
		Steps:         steps,
	}, nil
}

// classifyMultiIntent runs classification to detect all potential intents.
// Adds complexity heuristics (Issue 0029): short messages without compound
// signal words are skipped to avoid false positive compound detection.
//
// LLM arbitration (e2e T1, 2026-09-10): when exactly one detected intent is
// actionable work (code/debug/git/...) and the rest are conversational
// tag-alongs (chat/report/...), the request is a single action with a
// trailing acknowledgment ask — "create hello.txt, then tell me the path".
// The multi-intent classifier routinely emits chat as a second intent for
// these, which flipped compound on and hi-jacked a straightforward single
// task into a pair session that previously degraded to a contextless chat
// deflection. The collapse fires only for the work-plus-report shape
// (actionable == 1 && chatLike >= 1); genuine multi-work requests
// (code+debug) stay compound, and "then tell me X" report-backs are exactly
// what the chatLike arm absorbs.
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

	// LLM arbitration: multi-intent says compound, single-intent says one
	// actionable intent — trust single-intent for work-plus-report
	// patterns. Only flips compound→single; never single→compound.
	if multi.IsCompound {
		actionable := 0
		chatLike := 0
		var nonChatIntent *Intent
		for _, intent := range multi.Intents {
			if intent.Confidence < compoundIntentConfidenceFloor {
				continue
			}
			// Only genuinely CONVERSATIONAL tag-alongs may be absorbed:
			// chat / platform / recall. Search and analyze are WORK lanes
			// (they route to the analyst — intent.go:136-137), so
			// counting them as chat-like let "search the web for X and
			// write it to notes.md" collapse to the write half and
			// silently drop the search; Report is likewise a second
			// deliverable, not conversation (bughunt 2026-09-12 F42).
			// DetectCompound already excluded only chat/platform
			// (:2000), so admitting work lanes here undid that verdict.
			switch intent.Type {
			case string(IntentChat), string(IntentPlatform), string(IntentRecall):
				chatLike++
			default:
				actionable++
				if nonChatIntent == nil || intent.Confidence > nonChatIntent.Confidence {
					nonChatIntent = intent
				}
			}
		}
		// Exactly one actionable + at least one conversational tag-along
		// ⇒ collapse to the actionable intent. Deliberately NOT applied
		// when actionable >= 2 (a code+debug request is genuinely compound)
		// or when actionable == 0 (not a work request at all).
		if actionable == 1 && chatLike >= 1 && nonChatIntent != nil {
			d.logger.Info("Compound arbitration: work-plus-report request routed as single intent",
				"actionable_intent", nonChatIntent.Type,
				"chat_like_count", chatLike,
				"input_len", len(input),
			)
			multi.IsCompound = false
			multi.CompoundType = ""
		}

		// Report-readback arbitration (2026-09-18 tool-boundary-hardening
		// leaf 04, e2e T1): exactly TWO actionable intents where the
		// lower-confidence one is `report` AND the report clause references
		// the first action's output ("then tell me the full path") is a
		// readback, not a second deliverable — collapse to the single
		// actionable intent. F42's protection stands: a report clause with
		// its own work verb ("write a report about the findings") fails
		// classifyReportTagAlong and stays compound. actionable >= 3 and
		// the chatLike arm above are untouched. Clause split is connector
		// substring matching ONLY (" then " / ", and " / " and then ") — no
		// general NLP parser; without a recognized connector the verdict
		// stays compound.
		if actionable >= 2 && multi.IsCompound {
			// Diagnostic (2026-09-19 live e2e run 4): log the above-floor
			// intent set whenever compound stands with 2+ actionable —
			// whether or not the readback arm fires — so live runs reveal
			// which verdict shape missed. (First placement was inside
			// actionable==2, so runs with 3+ actionable intents — the
			// actual live shape, intents=5 — produced no diagnostic.)
			names := make([]string, 0, len(multi.Intents))
			for _, it := range multi.Intents {
				if it.Confidence >= compoundIntentConfidenceFloor {
					names = append(names, fmt.Sprintf("%s@%.2f", it.Type, it.Confidence))
				}
			}
			d.logger.Info("Compound intent shape (above floor)",
				"intents", strings.Join(names, ","),
				"actionable", actionable,
			)
			primary, secondary := reportReadbackIntents(multi.Intents)
			if primary != nil && secondary != nil {
				actionClause, reportClause, ok := splitReportReadbackClauses(input)
				if ok && classifyReportTagAlong(actionClause, reportClause) {
					d.logger.Info("Compound arbitration: report readback collapsed",
						"actionable_intent", primary.Type,
						"report_clause", truncateString(reportClause, 80),
						"action_clause", truncateString(actionClause, 80),
						"input_len", len(input),
					)
					multi.IsCompound = false
					multi.CompoundType = ""
				}
			}
		}
	}

	return multi
}

// reportReadbackIntents reports whether the intent set above the confidence
// floor is exactly two actionable intents with `report` as the LOWER-confidence
// one, returning the primary (higher-confidence) and the report intent.
func reportReadbackIntents(intents []*Intent) (primary, report *Intent) {
	var secondary []*Intent
	for _, intent := range intents {
		if intent.Confidence < compoundIntentConfidenceFloor {
			continue
		}
		if intent.Type == string(IntentChat) || intent.Type == string(IntentPlatform) || intent.Type == string(IntentRecall) {
			continue
		}
		secondary = append(secondary, intent)
	}
	if len(secondary) != 2 {
		return nil, nil
	}
	a, b := secondary[0], secondary[1]
	if a.Type == string(IntentReport) && a.Confidence < b.Confidence {
		return b, a
	}
	if b.Type == string(IntentReport) && b.Confidence < a.Confidence {
		return a, b
	}
	return nil, nil
}

// splitReportReadbackClauses splits the two-intent input on the compound
// connectors the clause-split rule allows (" then ", " and then ", ", and ")
// and returns (actionClause, reportClause, true). Everything BEFORE the
// connector is the action; everything after is the report readback. Only the
// FIRST connector occurrence splits; no recognized connector ⇒ (,, false).
func splitReportReadbackClauses(input string) (action, report string, ok bool) {
	// ", and " must be tried before " and " shapes so the comma variant
	// wins when both match.
	for _, sep := range []string{" then ", " and then ", ", and "} {
		if idx := strings.Index(input, sep); idx >= 0 {
			return input[:idx], input[idx+len(sep):], true
		}
	}
	return "", "", false
}

// compoundIntentConfidenceFloor is the minimum confidence an intent from the
// multi-intent classifier must carry to participate in compound arbitration
// counting. Mirrors DetectCompound's own strong-intent filter.
const compoundIntentConfidenceFloor = 0.5

// RouteToAgent routes a dispatch result to the appropriate agent.
// If an active agent loop exists for this conversation, it injects
// the message into the queue (steer or follow-up) based on the
// SteeringHeuristicTable. Otherwise, it runs the agent synchronously.
func (d *Dispatcher) RouteToAgent(ctx context.Context, result *DispatchResult, conversationID string) (string, error) {
	if result == nil || result.Intent == nil {
		return "", fmt.Errorf("dispatch result has no intent to route")
	}

	// sessionConversationID is the session-level conversation ID used for
	// session store lookups (project path, session-scoped loop creation).
	// The thread router may replace conversationID with a thread-specific
	// ID below, but session lookups must always use the session-level ID
	// because GetByConversationID only knows session conversation IDs.
	sessionConversationID := conversationID

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

	// Handle platform introspection directly without LLM — but only when
	// no client agent override demanded a specific executor. An override
	// rewrites result.AgentType (dispatcher.go:1039) while the classified
	// intent.Type may still be "platform": the researchers-extract e2e
	// (2026-09-12) showed the 8B classifier labeling "use the json_extract
	// tool ..." as IntentPlatform, and this shortcut then swallowed the
	// turn into the canned capabilities dump before the overridden agent
	// ever ran. An explicit override is a task for THAT agent.
	if result.Intent.Type == string(IntentPlatform) && !result.AgentOverrideApplied {
		return d.handlePlatformIntrospection(ctx, result.Intent.Summary)
	}

	// Check if there's an active agent loop for this conversation
	queue, generation := d.registry.GetActiveQueue(conversationID)
	if queue != nil {
		// BUG FIX: Before steering/following-up through the queue, ensure
		// the queue's agent loop has the correct session-scoped workingDir.
		// The loop was created by the registry with workingDir="" and only
		// gets the project path when resolveAgent runs (which is bypassed
		// when the queue is active).
		//
		// Use sessionConversationID (pre-thread-router) for session lookup
		// because the session store only knows session-level conversation IDs.
		if d.sessionStore != nil && sessionConversationID != "" {
			if sess := d.sessionStore.GetByConversationID(sessionConversationID); sess != nil {
				projectPath, wdSource := d.resolveSessionWorkingDir(sess)
				if projectPath != "" {
					if qLoop := d.registry.GetActiveQueueLoop(conversationID); qLoop != nil {
						qLoop.SetWorkingDir(projectPath)
					}
				} else {
					d.logger.Warn("dispatched turn has no working directory",
						"conversation_id", sessionConversationID,
						"session_id", sess.ID,
						"source", string(wdSource),
					)
				}
			}
		}
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
	contextMsg := d.buildContextMessage(result, conversationID)

	// Resolve the agent loop. Prefer a session-scoped loop (with the
	// session's project_path as working directory) when the manager and
	// session store are wired. Falls back to the singleton registry agent
	// when the session has no project path or the manager is unavailable.
	agent := d.resolveAgent(result.AgentID, sessionConversationID)
	if agent == nil {
		return "", fmt.Errorf("no agent available for %q", result.AgentID)
	}

	// Apply the client-supplied per-request model (chat.request "model")
	// through the loop's ONE-SHOT SetModelOverride seam — the same
	// precedence slot as a parsed user model directive, so it outranks the
	// agent's alias resolution for exactly this turn and auto-clears after
	// it (nothing is persisted to the config). applyRequestModel no-ops on
	// an empty ref or an unresolvable one (a warn is logged and the turn
	// runs the alias/default chain).
	agent.ApplyRequestModel(result.RequestModel)

	// Run the agent. When the dispatcher is carrying multimodal parts
	// (e.g. image attachments), route them through RunOnceWithParts so the
	// provider serializer emits native image blocks. Otherwise use the
	// plain RunOnce path — the two are equivalent for text-only turns.
	var response string
	var err error
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
		// Build the child's spawn context under the ArtifactOnly default:
		// the report-derived brief + memory references ride along as
		// structured artifacts; the parent's message transcript does not.
		// (Leaf 10-isolation: handoffs must not inherit parent tool dumps.)
		spawnCtx := d.buildHandoffSpawnContext(report, conversationID)
		nextResult := &DispatchResult{
			AgentID:       nextAgentID,
			Intent:        result.Intent,
			OriginalInput: result.OriginalInput,
			// Structured handoff brief; never a parent transcript copy.
			Brief:     RenderSpawnContext(spawnCtx),
			MemoryIDs: spawnCtx.MemoryIDs,
			Isolation: spawnCtx.Isolation,
			// Preserve multimodal parts for the next hop so attachments are
			// not silently dropped during report-router handoffs.
			Parts: result.Parts,
		}
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

	// Structural depth gating
	if d.toolRegistry != nil {
		maxDepth := d.toolRegistry.MaxDepth()
		fmt.Fprintf(&sb, "### Depth Gating\n\n")
		if maxDepth > 0 {
			fmt.Fprintf(&sb, "- Maximum depth: %d\n", maxDepth)
			fmt.Fprintf(&sb, "- At max depth, spawn tools are structurally unavailable (not registered)\n")
		} else {
			sb.WriteString("- No depth gating configured\n")
		}
		sb.WriteString("\n")
	}

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

// resolveAgent returns the AgentLoop that should handle a dispatched
// request for the given conversation. When the loop manager and session
// store are wired AND the session has a project_path, it returns a
// session-scoped loop whose working directory matches the session's
// project. Otherwise it falls back to the singleton registry agent.
//
// This mirrors ChatHandler.sessionLoop() but lives on the Dispatcher so
// that the multi-agent dispatch path (RouteToAgent) also benefits from
// per-session project isolation.
func (d *Dispatcher) resolveAgent(agentID, conversationID string) *AgentLoop {
	// Try session-scoped loop first.
	if d.loopManager != nil && d.sessionStore != nil && conversationID != "" {
		sess := d.sessionStore.GetByConversationID(conversationID)
		d.logger.Debug("resolveAgent: session lookup",
			"conversation_id", conversationID,
			"session_found", sess != nil,
			"project_id", func() string {
				if sess != nil {
					return sess.ProjectID
				}
				return ""
			}(),
			"project_path", func() string {
				if sess != nil {
					return sess.ProjectPath
				}
				return ""
			}(),
		)
		if sess != nil {
			// Legacy session fallback: if ProjectPath is empty but
			// ProjectID is set, look up LocalPath from the project
			// manager (available via loop manager) before falling back.
			if sess.ProjectPath == "" && sess.ProjectID != "" && d.loopManager != nil {
				if path := d.loopManager.ResolveProjectPath(context.Background(), sess.ProjectID); path != "" {
					sess.ProjectPath = path
				}
			}
			// The turn's working directory follows the ONE precedence
			// (worktree > project > detection CWD) plus the active-project
			// fallback, so a session with no project still runs in a real
			// directory instead of failing every filesystem tool with
			// tools.ErrNoWorkingDir.
			workingDir, wdSource := d.resolveSessionWorkingDir(sess)
			if workingDir == "" {
				d.logger.Warn("dispatched turn has no working directory",
					"conversation_id", conversationID,
					"session_id", sess.ID,
					"source", string(wdSource),
				)
			}
			if workingDir != "" {
				// Bind the shared fence sandbox to this turn's directory
				// and apply the per-session --nofence override.
				d.configureFence(conversationID, workingDir, sess.NoFence)
				// Use the registry agent as the template so the session loop
				// inherits LLM client, tools, skills, and hooks.
				template, templateErr := d.registry.Get(agentID)
				if templateErr != nil {
					template, templateErr = d.registry.Get(config.AgentIDChat)
				}
				if templateErr == nil && template != nil {
					loop, err := d.loopManager.GetOrCreateWired(conversationID, workingDir, template)
					if err == nil {
						// Wire session identity + project context (mirrors
						// ChatHandler.sessionLoop; ConfigSnapshot excludes these).
						loop.SetProjectID(sess.ProjectID)
						// The directory may come from the detection context or
						// the active project, not only ProjectPath, so set it
						// explicitly: executeToolCalls injects GetWorkingDir()
						// into every tool context.
						loop.SetWorkingDir(workingDir)
						if sess.DetectionContext != nil {
							loop.SetDetectionContext(&DetectionContext{
								CWD:               sess.DetectionContext.CWD,
								DetectedProjectID: sess.DetectionContext.DetectedProjectID,
								CLIArgs:           sess.DetectionContext.CLIArgs,
							})
						}
						return loop
					}
					d.logger.Warn("session-scoped loop creation failed; using registry agent",
						"session", conversationID,
						"project", sess.ProjectPath,
						"error", err,
					)
				}
			}
		}
	}

	// Fallback: try session-scoped loop before resorting to the registry
	// singleton. registry.Get() is deferred until actually needed (either
	// as a template for the session-scoped loop, or as the final return
	// value) to avoid eagerly creating a task-scoped loop via GetForTask
	// that is discarded when the session-scoped loop succeeds.
	if d.sessionStore != nil && conversationID != "" {
		if sess := d.sessionStore.GetByConversationID(conversationID); sess != nil {
			projectPath, wdSource := d.resolveSessionWorkingDir(sess)
			if projectPath == "" {
				d.logger.Warn("dispatched turn has no working directory",
					"conversation_id", conversationID,
					"session_id", sess.ID,
					"source", string(wdSource),
				)
			}
			if projectPath != "" {
				// Try to create a per-session loop to avoid the singleton race.
				// registry.Get is called here (not earlier) so the task-scoped
				// loop is only created when we actually need it as a template.
				if d.loopManager != nil {
					template, templateErr := d.registry.Get(agentID)
					if templateErr != nil {
						template, templateErr = d.registry.Get(config.AgentIDChat)
					}
					if templateErr == nil && template != nil {
						if loop, err := d.loopManager.GetOrCreateWired(conversationID, projectPath, template); err == nil {
							loop.SetProjectID(sess.ProjectID)
							return loop
						}
					}
				}
				// Session-scoped loop unavailable or failed; fall back to
				// mutating the singleton with a warning — this is a known
				// race that only manifests under concurrent multi-session load.
				if agent, agentErr := d.registry.Get(agentID); agentErr == nil {
					d.logger.Warn("resolveAgent: mutating shared singleton workingDir (race risk)",
						"conversation_id", conversationID,
						"project_path", projectPath,
					)
					agent.SetWorkingDir(projectPath)
					return agent
				}
			}
		}
	}

	// Final fallback: singleton from registry (no session/project context).
	agent, err := d.registry.Get(agentID)
	if err != nil {
		d.logger.Warn("Agent not found, falling back to chat", "agent", agentID, "error", err)
		agent, err = d.registry.Get(config.AgentIDChat)
		if err != nil {
			d.logger.Error("fallback agent not found", "error", err)
			return nil
		}
	}
	d.logger.Debug("resolveAgent: using registry fallback",
		"agent_id", agentID,
		"conversation_id", conversationID,
		"loop_session_id", agent.GetSessionID(),
		"loop_working_dir_before", agent.GetWorkingDir(),
	)

	return agent
}

// The primary content is the full original user input (result.OriginalInput);
// a brief summary is prepended only when it differs from the full input.
func (d *Dispatcher) buildContextMessage(result *DispatchResult, conversationID string) string {
	var parts []string

	// Inject cross-thread context so the active thread has continuity with
	// prior topics. Without this, switching threads loses all context from
	// inactive threads.
	if d.threadRouter != nil && conversationID != "" {
		if crossCtx := d.threadRouter.CrossThreadContext(conversationID, ""); crossCtx != "" {
			parts = append(parts, crossCtx)
		}
	}

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

	// Add the structured handoff brief when this dispatch is a handoff hop
	// (leaf 10-isolation). This is report-derived context produced via
	// BuildSpawnContext under artifact_only isolation — never a copy of the
	// parent agent's conversation transcript.
	if result.Brief != "" {
		parts = append(parts, "## Handoff Context\n\n"+result.Brief+"\n\n")
	}

	// Use the full original user input, falling back to the intent summary
	// when OriginalInput is not populated (e.g. programmatic dispatch results).
	content := result.OriginalInput
	if content == "" {
		content = result.Intent.Summary
	}

	// Executing-agent session context (e2e run 5, 2026-09-10 T3): the
	// classifier already sees the session digest via AnalyzeTrueIntent, but
	// the EXECUTING agent's prompt did not — so "did the change get made?"
	// misrouted to git (committer) and the committer answered "I don't have
	// any evidence that a change was made" with no session context. Inject
	// a compact digest block of the session's most recent PRIOR task here,
	// excluding the current turn's own just-created placeholder (result.Task
	// is the task ClassifyAndRoute created for THIS message seconds ago; the
	// digest must describe the earlier work the user is asking about).
	if d.digestContextEnabled() {
		excludeID := ""
		if result.Task != nil {
			excludeID = result.Task.ID
		}
		digest := d.buildSessionContextDigestExcluding(conversationID, excludeID)
		if !digest.IsEmpty() {
			parts = append(parts, BuildSessionContextBlock(digest))
		}
	}

	parts = append(parts, content)

	return strings.Join(parts, "")
}

// digestContextEnabled reports whether the executing-agent digest context
// block is enabled. Default on; set MEEPT_DISABLE_DIGEST_CONTEXT=1 to opt
// out for A/B comparison of reply quality.
func (d *Dispatcher) digestContextEnabled() bool {
	return os.Getenv("MEEPT_DISABLE_DIGEST_CONTEXT") != "1"
}

// BuildSessionContextBlock renders a SessionContextDigest as a compact
// markdown block for an executing agent's prompt — the same session facts
// the intent classifier saw, now visible to the agent that answers. Bounded
// (name 200 / summary 400 chars at digest build time) so it cannot bloat the
// prompt. Returns "" for a nil/empty digest.
//
// Context-aware usage rule (e2e A5, gh #37): the block carries a one-line
// instruction telling the model to ANSWER FROM this context whenever the
// user asks about prior work — not merely to be aware of it. Small local
// models treat passive context as background and reply "ok" to status
// questions even when the answer is stated in the block. The rule is
// generic: it covers status questions, artifact references, follow-ups,
// and corrections — any input that concerns this prior work — not one
// scripted phrasing.
func BuildSessionContextBlock(digest *SessionContextDigest) string {
	if digest.IsEmpty() {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Session context (most recent work in this conversation)\n\n")
	sb.WriteString("If the user's message asks about, refers to, or follows up on this work, answer from the context below — name the specific files, paths, and results it records — instead of asking for information already present here.\n\n")
	if digest.LastTaskName != "" {
		state := digest.LastTaskState
		if state == "" {
			state = "unknown"
		}
		fmt.Fprintf(&sb, "- Prior task: %q — status: %s", digest.LastTaskName, state)
		if digest.LastTaskAgent != "" {
			fmt.Fprintf(&sb, " (agent: %s)", digest.LastTaskAgent)
		}
		sb.WriteString("\n")
	}
	if digest.LastResultSummary != "" {
		fmt.Fprintf(&sb, "- Prior result: %s\n", digest.LastResultSummary)
	}
	if digest.WorkingDirectory != "" {
		fmt.Fprintf(&sb, "- Working directory: %s\n", digest.WorkingDirectory)
	}
	return sb.String()
}

// buildAccumulatedContext creates context from a previous agent's report for the next agent.
//
// Deprecated: superseded by buildHandoffSpawnContext, which routes the same
// report content through BuildSpawnContext so handoffs carry a structured
// brief + artifact refs under artifact_only isolation. Retained for callers
// that still need the bare report-derived string.
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

// buildHandoffSpawnContext constructs the child agent's SpawnContext for a
// report-router handoff under the ArtifactOnly default. The parent's report
// becomes the structured brief; the conversation-level memory record (if any)
// is referenced as an artifact rather than copied as transcript.
//
// The parent message list is deliberately NOT passed to BuildSpawnContext:
// handoff children must not inherit parent tool dumps or chain-of-thought
// (leaf 10-isolation contract C4).
func (d *Dispatcher) buildHandoffSpawnContext(report *AgentReport, conversationID string) SpawnContext {
	brief := d.buildAccumulatedContext(report, "")
	var artifacts []ArtifactRef
	if brief != "" {
		// Reference the accumulated context as a durable artifact so the
		// child can consult the full handoff digest on demand.
		artifacts = append(artifacts, ArtifactRef{
			Path: "handoff://" + conversationID + "/accumulated-context",
		})
	}
	// nil parent: ArtifactOnly keeps Transcript empty by construction.
	return BuildSpawnContext(IsolationArtifactOnly, brief, artifacts, nil, nil)
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
	{[]string{"write code", "implement", "create function", "add feature", KeywordRefactor, "create a file", "create the file", "write a file", "write the file"}, string(IntentCode), config.AgentIDCoder, 0.8, false},
	{[]string{"code review", "review pr", "check code"}, string(IntentReview), config.AgentIDCoder, 0.75, false},

	// Git operations
	{[]string{KeywordCommit, "push", "pull", "merge", "branch", string(IntentGit)}, string(IntentGit), config.AgentIDCommitter, 0.8, false},

	// Scheduling
	{[]string{"remind", string(IntentSchedule), "alarm", "timer", "at ", "tomorrow", "next week"}, string(IntentSchedule), config.AgentIDScheduler, 0.8, false},

	// Planning
	// Note: "architect" moved to the IntentArchitect pattern below so it
	// no longer collides with system-design requests.
	{[]string{string(IntentPlan), KeywordDesign, "how should i", "break down", "decompose"}, string(IntentPlan), config.AgentIDPlanner, 0.8, true},

	// Plan 2: knowledge-work intents. Longer compound phrases are listed
	// first so the best-score-wins classifier prefers them over the
	// generic bare-word variants ("architect", "write").
	{[]string{"write an essay", "write a brief", "write a blog post", "long-form writing", "long form", "write doc", "write article", "draft a", "draft an", "write a doc"}, string(IntentWrite), config.AgentIDWriter, 0.85, false},
	{[]string{"design a system", "design system", "architect a", "architect the", "architect this", "tech stack", "trade-off", "tradeoff", "should we use", "evaluate technology", "compare technologies"}, string(IntentArchitect), config.AgentIDArchitect, 0.85, true},
	{[]string{"stress-test", "stress test", "steelman", "what's wrong with", "what is wrong with", "challenge this", "adversarial review", "challenge my"}, string(IntentSkeptic), config.AgentIDSkeptic, 0.85, false},
	{[]string{"review my memory", "review memory", "memory review", "clean up tags", "mine backlog", "what contradictions", "what have i been thinking", "clean up my tags"}, string(IntentLibrarian), config.AgentIDLibrarian, 0.85, false},

	// Media intents. Longer compound phrases first so they beat generic
	// "create"/"analyze"/"find" patterns on the best-score-wins classifier.
	{[]string{"generate an image", "generate image", "text to image", "txt2img", "image generation", "create an image", "make an image", "draw me", "render an image", "draw an image"}, string(IntentImageGen), config.AgentIDImageGen, 0.9, false},
	{[]string{"generate a video", "generate video", "text to video", "txt2video", "video generation", "create a video", "make a video", "animate this", "animate the"}, string(IntentVideoGen), config.AgentIDVideoGen, 0.9, false},
	{[]string{"identify this image", "identify the image", "what is this image", "describe this image", "what's in this photo", "whats in this photo", "analyze this image", "what is in this picture", "reverse image"}, string(IntentImageID), config.AgentIDImageID, 0.9, false},

	// Collaboration (pair programming, differential analysis)
	{[]string{"collaborate", "pair program", "differential", "a/b test", "compare approaches", "work together", "collaborative"}, string(IntentCollaborate), config.AgentIDAnalyst, 0.8, true},

	// Analysis/Research ("summarize" alone stays here for document summarization;
	// "summarize what" and "summary of work" are captured by report intent above).
	// Pure research/investigation intents route to the dedicated researcher agent;
	// synthesis/explanation intents stay with the analyst.
	{[]string{"research", "investigate", "deep dive", "study"}, string(IntentResearch), config.AgentIDResearcher, 0.7, false},
	{[]string{string(IntentAnalyze), "summarize", KeywordExplain, "what is", "how does"}, string(IntentAnalyze), config.AgentIDAnalyst, 0.7, false},
	{[]string{string(IntentSearch), "find", "look up", "google"}, string(IntentSearch), config.AgentIDAnalyst, 0.7, false},

	// Codebase exploration (read-only search specialist)
	{[]string{"explore", "find in codebase", "search codebase", "where is", "locate", "find file", "find function", "find symbol"}, string(IntentExplore), "explore", 0.75, false},

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
				// Platform lane demotion (I11, routing-repair leaf 04):
				// the platform row carries the bare informational phrase
				// "help me understand", which pulled "help me understand
				// how X works" onto the roster-dump lane. Platform intent
				// is QUESTIONS ABOUT THE PLATFORM; an informational help
				// request about a domain topic is ANALYZE material. The
				// informationalHelpRe match is the shared operation rule,
				// not a sentence exception: any input asking to
				// understand a topic skips ONLY the platform row.
				if p.intentType == string(IntentPlatform) && informationalHelpRe.MatchString(lower) {
					continue
				}
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
// The result is sorted by confidence descending (ties broken by type) so
// callers that take intents[0] get deterministic output; Go map iteration
// order is randomized, and an unordered return made top-intent selection
// nondeterministic whenever two intent types tied on confidence.
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
	sort.Slice(result, func(i, j int) bool {
		if result[i].Confidence != result[j].Confidence {
			return result[i].Confidence > result[j].Confidence
		}
		return result[i].Type < result[j].Type
	})
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

// SetDriftDetector wires the session drift detector (issue #41). Nil
// guard per project invariant (cf. SetMetricsStore): a nil detector —
// including a typed-nil pointer — is ignored so a wiring-order bug
// cannot strip a live detector. Disabled detectors are accepted and
// stay inert at Observe.
//
// Wiring model: the dispatcher registers the detector as the prefilter's
// embedding observer (SetEmbeddingObserver), so every healthy Door-1
// embedding feeds the per-session drift check with zero extra model
// calls. The signal is LOG-ONLY at this call site (issue #41 acceptance
// criterion 3): a drift event logs one Info line and never changes
// routing — the full-chain fall-through decision belongs to a later,
// measured rollout.
func (d *Dispatcher) SetDriftDetector(det *SessionDriftDetector) {
	if det == nil {
		return
	}
	d.sessionDrift = det
	if d.prefilter != nil {
		d.prefilter.SetEmbeddingObserver(func(sessionID string, vec []float64) {
			if sessionID == "" || !det.Enabled() {
				return
			}
			drifted, score := det.Observe(sessionID, vec)
			if drifted {
				// Log-only signal (issue #41): session id and
				// numbers only, never message text.
				d.logger.Info("session drift detected (log-only; not acting)",
					"session", sessionID,
					"score", score,
					"threshold", det.Threshold(),
					"window", det.WindowSize(),
				)
			}
		})
	}
}

// SetBurstDetector wires the tool-failure burst detector (issue #43).
// Nil guard per project invariant (cf. SetMetricsStore). The dispatcher
// feeds it resolved dispatch outcomes in recordDispatch; the signal is
// LOG-ONLY at that call site.
func (d *Dispatcher) SetBurstDetector(det *metrics.BurstDetector) {
	if det != nil {
		d.burstDetector = det
	}
}

// SetInputHasher wires the salted input-hash function used to fill
// dispatch_log.input_hash (classifier-observability S4). The daemon loads
// the per-install salt and injects a closure over metrics.HashInput; the
// dispatcher itself performs no crypto or file I/O. A nil hasher (including
// a typed-nil func) disables hashing: InputHash is persisted as "".
func (d *Dispatcher) SetInputHasher(fn func(message string) string) {
	if fn != nil {
		d.inputHasher = fn
	}
}

// stashPrefilterVerdict is the prefilter's Door-1 verdict observer
// (classifier-outcome-loop leaf 02): it captures the last emitted verdict
// so recordDispatch can persist the kNN margin even when the vote was
// suppressed or abstained. The prefilter block in ClassifyAndRoute runs
// synchronously before recordDispatch on the same goroutine — no
// goroutine hop between Match and the persist site — so the stash always
// holds THIS dispatch's verdict when read; the mutex guards only the
// (test-exercised) public RecordDispatch path.
func (d *Dispatcher) stashPrefilterVerdict(v PrefilterVerdict) {
	d.prefilterMu.Lock()
	d.stashVerdict = v
	d.stashHasVerdict = true
	d.prefilterMu.Unlock()
}

// takeStashedPrefilterVerdict pops the stashed Door-1 verdict: non-nil
// when the embedding prefilter observed (this) dispatch, nil otherwise.
// Always takes — a verdict is consumed exactly once, at the persist site
// (recordDispatch), so a dispatch that never ran the prefilter cannot
// inherit a stale one.
func (d *Dispatcher) takeStashedPrefilterVerdict() *PrefilterVerdict {
	d.prefilterMu.Lock()
	defer d.prefilterMu.Unlock()
	if !d.stashHasVerdict {
		return nil
	}
	v := d.stashVerdict
	d.stashVerdict = PrefilterVerdict{}
	d.stashHasVerdict = false
	return &v
}

// peekStashedPrefilterVerdict reads the stashed Door-1 verdict WITHOUT
// consuming it — used by the ClassifyAndRoute prefilter block for gate
// marking and result transport. The stash is only ever consumed at the
// persist site (recordDispatch), which runs synchronously after the block
// returns.
func (d *Dispatcher) peekStashedPrefilterVerdict() *PrefilterVerdict {
	d.prefilterMu.Lock()
	defer d.prefilterMu.Unlock()
	if !d.stashHasVerdict {
		return nil
	}
	v := d.stashVerdict
	return &v
}

// SetToolRegistry wires a depth-based tool registry for structural gating.
// At maxDepth the agent simply won't have spawn tools in its registry.
func (d *Dispatcher) SetToolRegistry(reg *DepthToolRegistry) {
	if reg != nil {
		d.toolRegistry = reg
	}
}

// ToolRegistry returns the depth-based tool registry, or nil if not configured.
func (d *Dispatcher) ToolRegistry() *DepthToolRegistry {
	return d.toolRegistry
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

	// Extract the classification method from the intent when it carries one
	// (each classify branch sets Intent.Method); fall back to the
	// dispatcher's last-recorded method for paths that don't (e.g. agent
	// overrides reusing a classified intent).
	if result != nil {
		agentID = result.AgentID
		if result.Intent != nil {
			intentType = result.Intent.Type
			confidence = result.Intent.Confidence
			classifierMethod = result.Intent.Method
		}
		if result.Task != nil {
			taskID = result.Task.ID
		}
	}
	// classifierMethod intentionally stays empty for dispatches whose
	// intent carries no Method: there is no honest classification to
	// attribute, and falling back to a previous message's method would
	// contaminate the ByMethod regression stats (a /skill invocation
	// right after a keyword-classified message would count as keyword).
	// Consumers of GetStats treat the empty bucket as "non-classified
	// path" (skill execution, plan clarification, agent overrides).

	errStr := ""
	if dispatchErr != nil {
		errStr = dispatchErr.Error()
		// Privacy scrub (classifier-observability S4): LLM/transport
		// errors can embed URL fragments, model names, or request
		// fragments. Drop anything matching an obvious key/token shape
		// entirely, and cap the remainder at 200 chars.
		if secretShape.MatchString(strings.ToLower(errStr)) {
			errStr = "scrubbed"
		} else {
			errStr = truncateString(errStr, 200)
		}
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
		// Privacy: the raw input summary is used only for the log line --
		// it must NOT reach the DB. inputHash of the full raw input
		// replaces it as the join/dedup key (classifier-observability S4).
		// The hasher is daemon-injected; nil => "" preserves the
		// multi-user-disabled (unwired) path invariant. The hash covers
		// result.OriginalInput (the full raw input, set by the main
		// routing paths) when present, falling back to the passed input
		// summary on paths that don't populate it.
		inputHash := ""
		if d.inputHasher != nil {
			hashInput := inputSummary
			if result != nil && result.OriginalInput != "" {
				hashInput = result.OriginalInput
			}
			inputHash = d.inputHasher(hashInput)
		}
		model := ""
		if result != nil && result.Intent != nil {
			// Honest provenance: empty for deterministic doors and when
			// all LLM candidates failed.
			model = result.Intent.Model
		}
		// Door-1 margin (classifier-outcome-loop leaf 02): populated when
		// the embedding prefilter observed this dispatch — routed,
		// suppressed, OR abstained. nil (SQL NULL) when the dispatch came
		// from any other door, preserving the leaf-01 column contract.
		// The verdict is consumed here exactly once.
		var pfMargin *float64
		if v := d.takeStashedPrefilterVerdict(); v != nil {
			m := v.Margin
			pfMargin = &m
		}
		// Turn number: one past the current session row count, so the
		// first dispatch of a session is turn 1. The COUNT is index-backed
		// and session rows are bounded by 30-day retention.
		turnNo := d.metricsStore.CountDispatchRows(sessionID) + 1
		d.metricsStore.RecordDispatch(metrics.DispatchEntry{
			SessionID:        sessionID,
			InputSummary:     "",
			IntentType:       intentType,
			AgentID:          agentID,
			Confidence:       confidence,
			ClassifierMethod: classifierMethod,
			HandlerCase:      handlerCase,
			TaskID:           taskID,
			HasParts:         hasParts,
			Error:            errStr,
			InputHash:        inputHash,
			Model:            model,
			Margin:           pfMargin,
			TurnNo:           turnNo,
			Outcome:          "pending",
		})

		// Re-route detector (classifier-outcome-loop leaf 03, Signal A):
		// resolve the prior pending row for this session now that the
		// current dispatch's final agent is known. A same-agent follow-up
		// resolves the prior row 'ok'; a different-agent dispatch within
		// reRouteWindow turns marks it 'corrected' (design.md S2 proxy for
		// "user re-asked"). Non-classified current dispatches pass an empty
		// agentID so they can resolve the prior row to 'ok' only -- their
		// agent comparison is meaningless. fire-and-forget: outcome capture
		// must never fail the dispatch path.
		resolveAgentID := agentID
		if classifierMethod == "" {
			resolveAgentID = ""
		}
		resolvedOutcome, resolveErr := d.metricsStore.ResolvePendingOutcome(
			sessionID, turnNo, resolveAgentID, reRouteWindow,
		)
		if resolveErr != nil {
			d.logger.Warn("failed to resolve pending dispatch outcome",
				"session", sessionID, "turn", turnNo, "error", resolveErr)
		}

		// Tool-failure burst detection (issue #43): feed each resolved
		// outcome into the temporal detector. Log-only at this call
		// site — a burst logs one Info line and never triggers a replan
		// or model switch (acceptance criterion 3; the replan decision
		// belongs to a later, measured rollout). The detector itself is
		// nil/disabled-inert, so this is a no-op until wired.
		if resolvedOutcome != "" && d.burstDetector != nil && d.burstDetector.Enabled() {
			if burst, score := d.burstDetector.ObserveOutcome(sessionID, resolvedOutcome); burst {
				d.logger.Info("tool-failure burst signal (log-only; not acting)",
					"session", sessionID,
					"score", score,
					"threshold", d.burstDetector.Threshold(),
					"turn", turnNo,
				)
			}
		}
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

// arithmeticExprRe matches a bounded arithmetic EXPRESSION: digits,
// decimal points, parentheses, whitespace and operator symbols only —
// at least one digit and at least one operator. It is a shape test for
// routing only; the expression is never evaluated, so no arbitrary
// input reaches any evaluator. (AR-1: the previous check read ANY
// +-*-/ anywhere in prose as arithmetic, so "what is the bug in
// src/parser.go?" matched "what is " + "/" and was short-circuited to
// chat before any classifier ran.)
var arithmeticExprRe = regexp.MustCompile(`^[0-9().\s+\-*/xX]+$`)

// isArithmeticExpression reports whether s is a bare arithmetic
// expression ("2+2", "3.5 * 4", "(2 + 3) / 7"). Requires at least one
// digit and one operator so ".", "()" or prose cannot match, and the
// character class leaves no room for identifiers, paths or code.
func isArithmeticExpression(s string) bool {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "?!.= ")
	if s == "" {
		return false
	}
	if !arithmeticExprRe.MatchString(s) {
		return false
	}
	hasDigit, hasOp := false, false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case r == '+' || r == '-' || r == '*' || r == '/' || r == 'x' || r == 'X':
			hasOp = true
		}
	}
	return hasDigit && hasOp
}

func isShortSimpleMessage(input string) bool {
	trimmed := strings.TrimSpace(input)
	if len(trimmed) == 0 {
		return true
	}
	lower := strings.ToLower(trimmed)

	// Pure arithmetic or math expressions: an arithmetic EXPRESSION —
	// operands joined by operators — not operator punctuation anywhere
	// in prose (AR-1, routing-repair leaf 04). A question carrying a
	// source path ("what is the bug in src/parser.go?") is different
	// from "what is 144/12?" and must reach classification.
	if isArithmeticExpression(lower) {
		return true
	}
	for _, prefix := range []string{"what is ", "what's ", "what are "} {
		if strings.HasPrefix(lower, prefix) && isArithmeticExpression(strings.TrimPrefix(lower, prefix)) {
			return true
		}
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
		"plan", "design", "break down", "decompose",
		// Plan 2: knowledge-work intents
		"write an essay", "write a brief", "long-form", "long form", "write doc", "draft a", "draft an",
		"design system", "tech stack", "trade-off", "tradeoff", "should we use", "evaluate technology", "compare technologies",
		"stress-test", "stress test", "steelman", "what's wrong with", "challenge this",
		"review memory", "memory review", "clean up tags", "mine backlog",
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

// hasLeadingImperativeVerb reports whether the input OPENS with an
// imperative execution verb ("create a file…", "write a function…",
// "make it beep…", "fix the parser…"). Used by the platform-vs-action
// arbitration: a platform introspection verdict on an imperative sentence
// is a classifier mistake — introspection asks ("what can you do"),
// imperatives instruct. Only leading position counts, so
// "what can you do to create a file?" still classifies as platform.
// inputMentionsWorkArtifact reports whether the input names a concrete work
// artifact (a file, directory, script, component, test, doc, endpoint...).
// Used by the chat-vs-imperative arbitration: "create a file named hello.txt"
// mentions an artifact; "hey there" and "thanks" do not. Conservative noun
// list — a false false keeps the old chat behavior (safe), a false true would
// route real conversation to executors (avoid).
func inputMentionsWorkArtifact(input string) bool {
	trimmed := strings.ToLower(input)
	for _, noun := range []string{
		"file", "directory", "folder", "script", "function", "component",
		"endpoint", "test", "doc", "document", "readme", "config", "report",
		"summary", "list", "table", "schema", "migration", "api", "service",
	} {
		if strings.Contains(trimmed, noun) {
			return true
		}
	}
	return false
}

func hasLeadingImperativeVerb(input string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(input))
	if trimmed == "" {
		return false
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}
	// Treat leading polite/clarifying lead-ins ("please create…",
	// "hey, create…") as still imperative. Fields are compared with
	// punctuation stripped (AR-4, routing-repair leaf 04): the raw
	// field "hey," (comma attached) missed the bare-word case and the
	// imperative recognition silently died.
	for _, f := range fields {
		switch strings.Trim(f, ",.!?:;\"'") {
		case "please", "hey", "ok", "okay", "now", "first", "then", ",":
			continue
		}
		switch strings.Trim(f, ",.!?:;") {
		case "create", "write", "make", "fix", "add", "build", "implement",
			"delete", "remove", "update", "refactor", "generate", "run",
			"code", "commit", "push", "merge", "rebase", "deploy",
			"install", "configure", "set", "rename", "move", "copy",
			"open", "close", "start", "stop", "restart",
			// Sweep e2e (2026-09-15): "Use the json_extract tool with
			// schema=…" scored intent=platform @0.9 because "use" was not
			// an imperative verb and the roster dump swallowed the turn.
			// Tool-invocation and data-extraction imperatives belong here.
			"use", "extract", "call", "fetch", "search", "analyze":
			return true
		default:
			return false
		}
	}
	return false
}

// nonImperativeHeadWords is the CLOSED class of words that can never OPEN an
// imperative clause: interrogatives, auxiliaries/modals/copulas, subject
// pronouns and determiners, prepositions/conjunctions, and the polite or
// filler lead-ins the callers already skip. Closed classes do not grow, which
// is the point: the alternative — an open list of imperative verbs — is what
// the wave-3 regression review found broken (see hasImperativeHead).
var nonImperativeHeadWords = map[string]bool{
	"what": true, "which": true, "where": true, "when": true, "why": true,
	"who": true, "whom": true, "whose": true, "how": true, "whether": true,
	"is": true, "are": true, "was": true, "were": true, "be": true,
	"been": true, "being": true, "am": true, "do": true, "does": true,
	"did": true, "have": true, "has": true, "had": true, "will": true,
	"would": true, "shall": true, "should": true, "can": true, "could": true,
	"may": true, "might": true, "must": true,
	"i": true, "we": true, "you": true, "he": true, "she": true, "it": true,
	"they": true, "them": true, "the": true, "a": true, "an": true,
	"this": true, "that": true, "these": true, "those": true, "my": true,
	"our": true, "your": true, "his": true, "her": true, "its": true,
	"their": true, "there": true, "here": true,
	"in": true, "on": true, "at": true, "for": true, "to": true, "of": true,
	"with": true, "by": true, "from": true, "if": true, "because": true,
	"so": true, "but": true, "and": true, "or": true, "not": true, "no": true,
	"yes": true, "maybe": true, "perhaps": true, "please": true, "hey": true,
	"ok": true, "okay": true, "now": true, "first": true, "then": true,
}

// headTokenRe is the shape of a plausible imperative head token: a plain
// lowercase word. Numbers, symbols and empty tokens are never imperatives.
var headTokenRe = regexp.MustCompile(`^[a-z]+$`)

// hasImperativeHead reports whether the input OPENS with an imperative clause
// — a bare verb followed by its object or argument ("test the endpoint",
// "check that all tests pass", "implement the endpoint").
//
// STRUCTURAL on purpose. The wave-3 fix keyed the recall narrowing on
// hasLeadingImperativeVerb, whose hand-maintained 22-verb list omits
// check/test/verify/review/look/tell/show/remind/validate/inspect — so "test
// the endpoint. check that the response is this format" was not recognized as
// imperative-headed, the narrowing never ran, and the request stayed swallowed
// as a work-status question (wave-3 regression review of 9af23f86). A closed
// class of NON-imperative openers cannot be defeated by any verb the next
// reviewer forgets to add: everything left over is treated as a verb in head
// position. The caller still requires an explicit object, so a bare noun or
// interjection ("hello there" — one clause, no argument) is not an
// instruction, and the recall question's own lead-ins are handled by
// hasPronounObjectImperativeHead.
//
// Deliberately NOT wired into hasLeadingImperativeVerb's callers: those are
// pinned on the execution-verb list (platform/action arbitration), and this
// predicate is the broader question the recall narrowing actually asks.
func hasImperativeHead(input string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(input))
	if trimmed == "" {
		return false
	}
	fields := strings.Fields(trimmed)
	headIdx := -1
	for i, f := range fields {
		// Punctuation-stripped comparison (AR-4): "hey," and "ok:" are
		// the same lead-ins as their bare forms.
		switch strings.Trim(f, ",.!?:;\"'") {
		case "please", "hey", "ok", "okay", "now", "first", "then", ",":
			continue
		}
		headIdx = i
		break
	}
	if headIdx < 0 {
		return false
	}
	head := strings.Trim(fields[headIdx], ",.!?:;\"'")
	if head == "" || !headTokenRe.MatchString(head) || nonImperativeHeadWords[head] {
		return false
	}
	// An imperative carries an explicit object/argument; a bare head token
	// names no work and is not treated as an instruction.
	return len(fields) > headIdx+1
}

// recallLeadInPronouns are the objects of an imperative lead-in that is
// directing the ASSISTANT ("update me", "run me through it", "build me a
// summary", "make us a report") rather than naming a work artifact.
var recallLeadInPronouns = map[string]bool{
	"me": true, "us": true, "it": true, "them": true, "him": true,
	"her": true, "these": true, "those": true, "everything": true,
	"anything": true, "myself": true, "ourselves": true,
}

// hasPronounObjectImperativeHead reports whether the input's head imperative
// takes a pronoun object ("update me: …", "fix it, …"). Those heads are also
// the natural lead-ins for a recall request, so narrowing to them deletes the
// question ("update me, did the file get created?" → "update me") and lets an
// untrusted verdict through (wave-3 regression review of 9af23f86).
func hasPronounObjectImperativeHead(input string) bool {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(input)))
	for i, f := range fields {
		// Punctuation-stripped comparison (AR-4).
		switch strings.Trim(f, ",.!?:;\"'") {
		case "please", "hey", "ok", "okay", "now", "first", "then", ",":
			continue
		}
		if i+1 >= len(fields) {
			return false
		}
		return recallLeadInPronouns[strings.Trim(fields[i+1], ",.!?:;\"'")]
	}
	return false
}

// recallStatusOpenerRe matches a clause that OPENS with an interrogative
// status predicate or wh-word — the shape of a standalone work-status
// question. A clause that merely CONTAINS "is … this" (an instruction like
// "check that the response is this format") does not open with one.
var recallStatusOpenerRe = regexp.MustCompile(
	`(?i)^(?:and |then |but |ok |okay |so |also |please |hey |now )*(?:did|was|were|is|are|has|have|do|does|what|which|where|who|how)\b`)

// hasInterrogativeRecallClause reports whether any clause AFTER the input's
// first boundary is itself a work-status question. The status phrase then
// legitimately sits outside the leading clause ("fix the test, did the file
// get created?", "set up the report, is the task done?"), so narrowing would
// delete the real question and leave the untrusted verdict standing.
//
// Clause boundaries are the same ones leadingRecallClause uses, internal
// periods included: a version point must not be read as a clause end here
// either ("deploy 1.2, did the change get made?").
func hasInterrogativeRecallClause(input string) bool {
	rest := input
	offset := 0
	for {
		loc := leadingClauseSepRe.FindStringIndex(rest)
		if loc == nil {
			return false
		}
		idx := offset + loc[0]
		if input[idx] == '.' && isInternalPeriod(input, idx) {
			offset = idx + 1
			rest = input[offset:]
			continue
		}
		offset = idx + 1
		rest = strings.TrimSpace(input[offset:])
		if rest == "" {
			return false
		}
		if recallStatusOpenerRe.MatchString(rest) &&
			(isWorkStatusRecall(rest) || isSecondPersonWorkRecall(rest)) {
			return true
		}
	}
}

// narrowsRecallToLeadingClause reports whether a whole-input recall match must
// be re-judged on the leading clause alone. It is the F41 guard's inverse: an
// imperative head is needed for the narrowing at all (otherwise the whole
// input's recall match stands), but two head shapes ARE the recall request and
// must keep the whole-input match (see the call site in classifyIntent).
func narrowsRecallToLeadingClause(input string) bool {
	if !hasImperativeHead(input) {
		return false
	}
	if hasPronounObjectImperativeHead(input) {
		return false
	}
	return !hasInterrogativeRecallClause(input)
}

// isSecondPersonWorkRecall reports whether the input asks what the
// ASSISTANT did ("what files did you make for me?", "what did you
// create?"). Used by the platform-vs-recall arbitration (e2e run 5,
// 2026-09-10): the 8B classifier scored that phrasing as platform @0.9 and
// the platform branch dumped the agent roster. "Did YOU make/create/write
// X" is a question about the assistant's own past actions — recall or
// report material — never a platform-introspection question. Leading-
// position like hasLeadingImperativeVerb so third-person or hypothetical
// work questions ("what files should I make?") keep their classified route.
func isSecondPersonWorkRecall(input string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(input))
	if trimmed == "" {
		return false
	}
	fields := strings.Fields(trimmed)
	for i, f := range fields {
		f = strings.Trim(f, ",.!?:;\"'")
		switch f {
		case "did", "have", "what", "which", "where":
			// Scan a small window after the wh/did word for
			// "you" + past-tense work verb.
			for j := i + 1; j < len(fields) && j <= i+2; j++ {
				nxt := strings.Trim(fields[j], ",.!?:;\"'")
				if nxt != "you" && !(nxt == "u" && j == i+1) {
					if j == i+1 {
						break // "you" must directly follow
					}
					continue
				}
				// Found "you": the next word decides.
				if j+1 < len(fields) {
					verb := strings.Trim(fields[j+1], ",.!?:;\"'")
					switch verb {
					case "make", "made", "create", "created", "write", "wrote",
						"build", "built", "fix", "fixed", "generate", "generated",
						"do", "did", "produce", "update", "change", "modify",
						"put", "save", "saved", "leave", "store", "place":
						return true
					}
				}
				break
			}
		}
	}
	return false
}

// hasTimeSignal reports whether the input contains explicit scheduling
// vocabulary — the minimal credibility bar for a schedule intent verdict.
// e2e run 8 (2026-09-10): the 8B scored "create a file named hello.txt …"
// as schedule @0.8 with zero time references; a schedule classification
// without any of these signals is a classifier mistake.
//
// Evidence class, not substring presence (AR-3, routing-repair leaf 04):
// a locative preposition ("the parse error at src/parser.go") does NOT
// express timing, and a noun naming an ARTIFACT ("a reminder component
// in React") does not request scheduling. So:
//   - " at " requires a digit-clock on at least one side ("at 3pm",
//     "meeting at 5") — the clock IS the time evidence;
//   - bare clock forms (3pm, 7:30 am) match via timeMeridiemRe;
//   - "reminder(s)" requires a scheduling verb within a short window
//     before it (remind/set/schedule/create… for me) — an artifact noun
//     ("Create a reminder component in React") is not evidence;
//   - weekdays require "next"/"this"/"on" anchoring or a clock ("on
//     monday", "next friday") — a bare weekday inside a path or prose
//     token is not timing.
func hasTimeSignal(input string) bool {
	lower := strings.ToLower(input)
	for _, signal := range []string{
		"remind me", "reminder for me", "set a reminder", "schedule ",
		"alarm", "timer", "tomorrow", "today at", "in minutes",
		"in hours", "o'clock", "oclock", "deadline",
		"next week", "next month", "every day", "daily", "weekly",
	} {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	// "am"/"pm" need a digit prefix and word boundary: "name" contains "am".
	if timeMeridiemRe.MatchString(lower) {
		return true
	}
	// " at " is temporal only when anchored to a clock: a digit on at
	// least one side ("at 3pm", "meeting at 5", "3pm at the office").
	// A locative "at src/parser.go" or "error at line 12" is position,
	// not timing (AR-3).
	if timeAtClockRe.MatchString(lower) {
		return true
	}
	// Weekdays anchored by "next"/"this"/"on" (or followed by " at <n>"):
	// "on monday", "next friday", "monday at 5". A bare weekday inside a
	// filename or prose ("satellite", no — word-bounded) is not timing.
	if anchoredWeekdayRe.MatchString(lower) {
		return true
	}
	// "remind"/"reminder" scheduling VERB forms already matched above;
	// the artifact noun "reminder(s)" alone is NOT a time signal.
	return false
}

// timeAtClockRe matches " at " anchored to a clock: a digit or
// meridiem-terminated time on at least one side of the preposition
// ("at 3pm", "meeting at 5", "3pm at the office"). Pure locative uses
// ("at src/parser.go", "at line 12") have no digit-clock head and do
// not match.
var timeAtClockRe = regexp.MustCompile(
	`(?:\b[0-9]{1,2}(:[0-9]{2})?\s*(?:am|pm)?\s*at\b)|(?:\bat\s+[0-9]{1,2}(:[0-9]{2})?\s*(?:am|pm)\b)|(?:\bat\s+[0-9]{1,2}\b)`)

// anchoredWeekdayRe matches weekday evidence anchored to scheduling
// usage: "next <day>", "this <day>", "on <day>", or "<day> at <n>".
var anchoredWeekdayRe = regexp.MustCompile(
	`\b(?:(?:next|this|on)\s+(?:monday|tuesday|wednesday|thursday|friday|saturday|sunday))\b|(?:\b(?:monday|tuesday|wednesday|thursday|friday|saturday|sunday)\s+at\s+[0-9])`)

// interrogativeOpeners is the CLOSED class of wh-words that open a
// direct question. A question is not an instruction; a platform or
// time-signal-free schedule verdict on a wh-question is the classifier
// costume the arbitration discards (AR-3 arm, routing-repair leaf 04:
// "what is the parse error at src/parser.go line 12" scored schedule
// @0.9 — the verdict names no executable lane for an interrogative).
var interrogativeOpeners = map[string]bool{
	"what": true, "which": true, "where": true, "when": true,
	"why": true, "who": true, "whom": true, "whose": true, "how": true,
}

// isInterrogativeNonImperativeQuestion reports whether the input opens
// with a wh-word (polite lead-ins skipped, punctuation-insensitive) —
// i.e. it is a QUESTION, not an imperative. Used by the platform/
// schedule arbitration so an untrusted verdict on a question is
// discarded like the imperative case, letting the chain continue.
// Deliberately position-bound: "tell me how the parser works" keeps its
// verdict because its head is an imperative, not a question.
func isInterrogativeNonImperativeQuestion(input string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(input))
	if trimmed == "" {
		return false
	}
	fields := strings.Fields(trimmed)
	for _, f := range fields {
		// Normalize punctuation before comparison (AR-4 evidence class).
		switch strings.Trim(f, ",.!?:;\"'") {
		case "please", "hey", "ok", "okay", "now", "first", "then":
			continue
		}
		return interrogativeOpeners[strings.Trim(f, ",.!?:;\"'")]
	}
	return false
}

// timeMeridiemRe matches "3am", "7:30 pm", "9 AM" — digit-prefixed
// meridiem with a word boundary.
var timeMeridiemRe = regexp.MustCompile(`\b[0-9]{1,2}(:[0-9]{2})?\s*(am|pm)\b`)

// inputContainsGitVerb reports whether the input carries an explicit git
// action verb (commit, push, pull, merge, branch, rebase, revert, checkout,
// stash, cherry-pick). Used by the platform/recall arbitration (e2e run 10,
// 2026-09-11 YJ7oSn): the 8B scored "did the change get made? where is the
// file?" intent=git @0.9 — a git verdict on a work-status question with no
// git verb is the same credibility failure as the platform/schedule
// costumes, and the committer runs contextless.
// gitVerbRe matches a git action verb as a WHOLE WORD, inflections
// included ("merge" but not "emergency"/"emerged"; "commit" and
// "committed" but not "commitment"). Word boundaries are load-bearing:
// plain substring matching made "did the emergency change get made?"
// contain "merge", so the guard below wrongly kept an untrusted git
// verdict alive and the committer ran contextless (bughunt 2026-09-12
// F40). This file already word-bounds the meridiem the same way
// (timeMeridiemRe).
//
// The stems are written so the -ing forms that DROP the silent e are
// reachable: "merge" → "merging" and "rebase" → "rebasing", not
// "mergeing"/"rebaseing". The F40 word-bounding fix over-narrowed here:
// a bare "merge"/"rebase" stem plus an "ing" suffix matched the
// vowel-keeping form only, so "are there uncommitted changes in the
// tree?" (git noun, no verb match) and "merging"/"rebasing" all read as
// git-verb-free. Also admits the "uncommitted" prefix the commit stem
// needs to reach.
//
// git-status evidence class (routing-repair acceptance, 2026-09-18): an
// explicit "git <subcommand>" pair (status, diff, log, show, blame, tag,
// fetch, remote, config, add, restore, switch, init, clone) is git
// OPERATION evidence equivalent to an action verb — the acceptance rig
// scored verdict=git @0.95 on "run git status --porcelain…" and the
// agreement veto killed the correct verdict because "status" carried no
// action-verb stem. The pair must be adjacent ("git status", not "the
// status of git") to stay noun-tight; standalone "status"/"diff"/"log"
// still match nothing ("what is my status", "log these hours").
var gitSubcommandRe = regexp.MustCompile(
	`\bgit (?:status|diff|log|show|blame|tag|fetch|remote|config|add|restore|switch|init|clone)\b`)

var gitVerbRe = regexp.MustCompile(
	`\b(?:un)?(?:commit|push|pull|merg(?:e|es|ed|ing)|branch(?:es|ed|ing)?|rebas(?:e|es|ed|ing)|revert|checkout|stash|cherry-pick)(?:s|es|ed|d|ing|ted|ting)?\b`)

func inputContainsGitVerb(input string) bool {
	return gitVerbRe.MatchString(strings.ToLower(input)) ||
		gitSubcommandRe.MatchString(strings.ToLower(input))
}

// leadingClauseSepRe matches the first clause boundary: a comma, semicolon,
// period, question mark, exclamation mark, or the conjunction phrases
// " and " / " then " / " but " (case-insensitively). It is matched against the
// ORIGINAL input so FindStringIndex returns a byte offset INTO that input.
// The previous implementation lowercased the input, found the separator index
// in the LOWERED bytes, and sliced the ORIGINAL — which panics whenever
// strings.ToLower changes byte length ("Ⱥ" U+023A lowers to 3-byte "ⱥ" U+2C65:
// input[:120] against a 106-byte string) and returns broken UTF-8 when it
// shortens ("K" U+212A lowers to 1-byte "k": "K\xe2"). Never map lowered-byte
// indices onto the original.
var leadingClauseSepRe = regexp.MustCompile(`(?i),|;|\.|\?|!| and | then | but `)

// isInternalPeriod reports whether the period at byte offset i in input is a
// point INSIDE a token rather than a clause boundary: a version/decimal point
// ("v1.2", "1.2", "2.0.10") or the period closing an abbreviation ("e.g.",
// "i.e.", "etc.", "vs.", a single-letter initial). Treating those as
// boundaries truncated the leading clause mid-token — "run the check on v1.2
// is the file created?" cut to "run the check on v1" (outcome-changing: the
// recall predicate fell outside the judged clause) and "deploy 1.2. did the
// change get made?" cut to "deploy 1" (wave-3 regression review of 9af23f86).
// The period after the version in that last example IS a boundary (flanked by
// a digit and a space), so the clause keeps the whole "deploy 1.2".
func isInternalPeriod(input string, i int) bool {
	// Digit on both sides: a version/decimal point.
	if i > 0 && i+1 < len(input) && isASCIIDigit(input[i-1]) && isASCIIDigit(input[i+1]) {
		return true
	}
	// Walk back over the token ending at i (letters and earlier periods, so
	// the first period of "e.g." sees the "e" of the same token).
	j := i
	for j > 0 {
		c := input[j-1]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '.' {
			j--
			continue
		}
		break
	}
	token := strings.ToLower(strings.Trim(input[j:i], "."))
	if token == "" {
		return false
	}
	if abbreviationTokens[token] {
		return true
	}
	// A single-letter token is an initial or abbreviation stem ("e." in
	// "e.g.", "A. Smith"), never a real clause end.
	return len([]rune(token)) == 1
}

func isASCIIDigit(c byte) bool { return c >= '0' && c <= '9' }

// abbreviationTokens is the closed set of abbreviation stems whose trailing
// period is not a clause boundary.
var abbreviationTokens = map[string]bool{
	"e.g": true, "i.e": true, "etc": true, "eg": true, "ie": true,
	"vs": true, "cf": true, "approx": true, "dept": true, "est": true,
	"fig": true, "figs": true, "no": true, "vol": true, "sec": true,
	"dr": true, "mr": true, "mrs": true, "ms": true, "prof": true,
	"sr": true, "jr": true, "inc": true, "ltd": true, "co": true,
	"al": true, "min": true, "max": true, "p": true, "pp": true,
	"ed": true, "eds": true, "st": true,
}

// quickPlanCueUpgradeApplies reports whether an LLM verdict in the
// quickplan-scatter set (the lanes the 8B's quickplan cases scattered to in
// campaign 20260918 phase 2: debug, review, analyze, plan, code) should be
// upgraded to quickplan because the input carries STRONG adjudicated
// orchestration evidence. The cue check reuses QuickPlanCuePattern narrowed
// to its strongest forms — the same requirement heuristicFallback applies —
// so incidental cue words ("add a function" + one loose hit) cannot trigger
// the upgrade. chat/clarify verdicts are excluded: the ambiguity gate's
// question is a real question about scope, not a lexical costume.
func quickPlanCueUpgradeApplies(verdictType string, input string) bool {
	switch verdictType {
	case string(IntentDebug), string(IntentReview), string(IntentAnalyze),
		string(IntentPlan), string(IntentCode):
	default:
		return false
	}
	lower := strings.ToLower(input)
	if !QuickPlanCuePattern.MatchString(lower) {
		return false
	}
	return strings.Contains(lower, "subagent") ||
		strings.Contains(lower, "implement the plan") ||
		strings.Contains(lower, "implement tasks")
}

// leadingRecallClause returns the input up to its first clause boundary. The
// recall matchers are position-independent — isWorkStatusRecall scans every
// field for a status predicate plus a nearby work noun — so evaluated against
// the WHOLE input they also fire on an imperative work request whose status
// clause sits in the tail ("implement the endpoint and check that the response
// is this format" reads as a work-status question because "is … this" appears
// after the " and "). Judging the LEADING clause only keeps F41's own case
// ("update me: did the file get created?" — no clause boundary, so the whole
// string) and lets a genuine imperative fall through to the imperative
// override.
//
// A period that sits INSIDE a token (version point, abbreviation) is skipped
// and scanning continues for the next boundary (isInternalPeriod).
func leadingRecallClause(input string) string {
	rest := input
	offset := 0
	for {
		loc := leadingClauseSepRe.FindStringIndex(rest)
		if loc == nil {
			return input
		}
		idx := offset + loc[0]
		if input[idx] == '.' && isInternalPeriod(input, idx) {
			offset = idx + 1
			rest = input[offset:]
			continue
		}
		return input[:idx]
	}
}

// isWorkStatusRecall reports whether the input is a yes/no WORK-STATUS
// question ("did the change get made?", "was the file created?", "is it
// done?"). Used by the platform-vs-recall arbitration next to
// isSecondPersonWorkRecall (e2e run 7, 2026-09-11 rkl3Th): "did the change
// get made? where is the file?" scored platform @0.9 → introspection —
// the chat agent answered with the agent-catalog fallback text and the A5
// continuity assertion failed, even though the digest carried the answer.
// Unlike the second-person matcher, no "you" is required: the user speaks
// of the WORK ("the change", "the file"), not the assistant. Verbs accept
// both agent-side (make/create/write) and patient-side (get made/be
// created/be done) forms. Negative markers (not, n't) deliberately fall
// through — "did you not make it?" is a reproach, not a status poll.
func isWorkStatusRecall(input string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(input))
	if trimmed == "" {
		return false
	}
	fields := strings.Fields(trimmed)
	for i, f := range fields {
		f = strings.Trim(f, ",.!?:;\"'")
		switch f {
		case "did", "was", "were", "is", "are", "has", "have":
			// The work noun must follow the predicate within 3 words and
			// NOT sit in the predicate's own position — "is it?" is a bare
			// pronoun echo, not a work-status question.
			for j := i + 1; j < len(fields) && j <= i+3; j++ {
				w := strings.Trim(fields[j], ",.!?:;\"'")
				switch w {
				case "change", "changes", "file", "files", "edit", "edits",
					"task", "tasks", "work", "that", "this", "they",
					"those", "thing", "things", "stuff", "job", "update":
					return true
				}
			}
		}
	}
	return false
}

// heuristicFallback provides targeted keyword-based routing when all other
// classifiers fail (Issue 0036). Rules are ordered by specificity and
// confidence to avoid misrouting code tasks to scheduler/committer.
// reviewRequestRe matches review-OPERATION evidence: a request for a
// verdict on quality — findings only, nothing modified. Anchored to a
// review verb + object window so "review the auth module", "check the
// migration script", and "audit the config" all fire, while
// "re-view the code" style non-words cannot. The correction-clause and
// autonomy-clause alternatives are checked FIRST (classifyReviewIntent
// order) so review-and-correct routes quickplan, not review.
var reviewRequestRe = regexp.MustCompile(
	`(?i)\b(?:review|recheck|re-check|audit|verify|validate|check)\b\s+(?:the |this |my |our |all |each )?[a-z][a-z0-9 _./-]{0,60}`)

// correctionClauseRe matches an autonomous-correction clause: the
// orchestration evidence that upgrades a review request to quickplan
// ("and correct them as you find them", "fix any issues", "apply the
// fixes"). Mirrors QuickPlanCuePattern's adjudicated evidence rule.
var correctionClauseRe = regexp.MustCompile(
	`(?i)\b(?:and |then |)?(?:correct|fix|repair|apply|resolve|address)\b[^.!?]{0,40}\b(?:them|as you|any|all|the (?:issues|findings|problems|bugs))\b|` +
		`\bas you (?:find|go)\b|` +
		`\b(?:fix|correct|address)\s+(?:them|any|all|everything)\b`)

// defectReportRe matches ONE named defect evidence: a defect noun with
// a pointer to its location ("the nil pointer in handler.go", "a crash
// on startup", "this panic"). The debug lane's own contract: one named
// defect, fix follows directly. The bare words "bug"/"error" inside a
// review request ("for bugs") do NOT constitute a named defect.
var defectReportRe = regexp.MustCompile(
	`(?i)\b(?:fix|repair|resolve|debug)\b[^.!?]{0,60}\b(?:the |a |an |this )?(?:nil pointer|segfault|panic|crash|leak|deadlock|race|regression|off-by-one|null pointer|stack overflow|infinite loop)\b|` +
		`\b(?:nil|null) pointer\b|\bsegfault\b|\bpanics?\b|\bdeadlock\b|\bdata race\b`)

// informationalHelpRe matches an informational help request: "help me
// understand/explain/learn X" — the speaker wants to understand X, not
// to introspect the platform. (I11: the platform keyword row's bare
// "help me understand" pulled these onto the roster-dump lane.)
var informationalHelpRe = regexp.MustCompile(
	`(?i)\bhelp (?:me |us )?(?:to )?(?:understand|explain|learn|figure out|make sense of|see why|know why|grasp)\b`)

// docUpdateRe matches a documentation/repository ARTIFACT update: an
// imperative on a named repo document (README, docs, CHANGELOG, …).
// The degraded path previously had NO rule for these, so they fell to
// quickplan fallback — treating a one-file doc edit as undeterminable
// orchestration (I11 taxonomy: an artifact to change is CODE/WRITE).
var docUpdateRe = regexp.MustCompile(
	`(?i)\b(?:update|edit|rewrite|revise|amend|extend|improve|document)\b[^.!?]{0,50}\b(?:the |this |a |an )?(?:readme|changelog|license|contributing|makefile|dockerfile|docs?|documentation|guide|notes)\b`)

func heuristicFallback(input string) *Intent {
	lower := strings.ToLower(strings.TrimSpace(input))

	// QuickPlan cue arbitration (#52, campaign 20260918 phase 2): the
	// adjudicated orchestration evidence (QuickPlanCuePattern — subagents,
	// "implement the plan", "implement tasks N") outranks the generic code
	// keywords below. "implement the plan using subagents" matched
	// "implement the" → code at 0.55 BEFORE any quickplan consideration;
	// the cue check must precede it. Narrowed to the STRONG adjudicated
	// forms (subagents / implement plan-or-tasks), not the full loose
	// pattern, so "add a function" with an incidental cue word still
	// routes code.
	if QuickPlanCuePattern.MatchString(lower) &&
		(strings.Contains(lower, "subagent") ||
			strings.Contains(lower, "implement the plan") ||
			strings.Contains(lower, "implement tasks")) {
		return &Intent{
			Type:             string(IntentQuickPlan),
			Confidence:       0.6,
			AgentType:        "orchestrator",
			RequiresPlanning: true,
			Summary:          extractSummary(input),
		}
	}

	// Knowledge-lane arbitration (I11, routing-repair leaf 04): the
	// degraded path must agree with docs/workflows/intent-routing.md's
	// output-based taxonomy. Ordered by specificity:
	//   1. review + correction clause  → quickplan (autonomous execution)
	//   2. review operation            → review  (verdict only)
	//   3. one named defect            → debug
	// Informational "help me understand" is ANALYZE material and is
	// demoted from the platform keyword row below (keywordPatterns).
	if reviewRequestRe.MatchString(lower) {
		if correctionClauseRe.MatchString(lower) {
			return &Intent{
				Type:             string(IntentQuickPlan),
				Confidence:       0.6,
				AgentType:        "orchestrator",
				RequiresPlanning: true,
				Summary:          extractSummary(input),
			}
		}
		return &Intent{
			Type:       string(IntentReview),
			Confidence: 0.55,
			AgentType:  config.AgentIDCoder,
			Summary:    extractSummary(input),
		}
	}
	if defectReportRe.MatchString(lower) {
		return &Intent{
			Type:       string(IntentDebug),
			Confidence: 0.55,
			AgentType:  config.AgentIDDebugger,
			Summary:    extractSummary(input),
		}
	}
	if docUpdateRe.MatchString(lower) {
		return &Intent{
			Type:       string(IntentWrite),
			Confidence: 0.55,
			AgentType:  config.AgentIDWriter,
			Summary:    extractSummary(input),
		}
	}

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
		"write a file", "write the file",
		"create a file", "create the file",
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
