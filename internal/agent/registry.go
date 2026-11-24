package agent

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"maps"
	"sort"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/agents"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/groups"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory/memvid"
	"github.com/caimlas/meept/internal/shadow"
	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/security"
)

// QueueEntry wraps a MessageQueue with generation tracking.
type QueueEntry struct {
	Queue      *MessageQueue
	Loop       *AgentLoop // the loop that owns this queue (may be nil in tests)
	Generation uint64
	// PrimaryID is the conversation id this entry was registered under as
	// its PRIMARY key (RegisterActiveQueue's conversationID argument).
	// Alias keys point at the same entry but are never an entry's primary
	// key; the shadow guard in RegisterActiveQueue uses this to tell a
	// live primary registration apart from a replaceable alias mapping.
	PrimaryID string
}

// AgentRegistry manages agent specifications and instantiated agent loops.
//
//nolint:revive // stutter with package name is intentional for API clarity
type AgentRegistry struct {
	mu sync.RWMutex

	// Agent specifications
	specs map[string]*AgentSpec

	// loops is keyed by agentID → taskID → loop. The taskID "_default" is
	// used for non-task-scoped callers via the backward-compat Get() shim.
	// Nested-map structure allows per-task isolation of AgentLoop state
	// (conversation history, PromptBuilder, FilteredToolRegistry) without
	// cross-task bleed.
	loops map[string]map[string]*AgentLoop

	// Capabilities map for fast routing
	capabilitiesMap *CapabilitiesMap

	// Global rules injected into all agent prompts
	globalRules string

	// Shared dependencies
	// AGENT-21 DEFERRED: AgentRegistry depends on a concrete *llm.Client.
	// After the provider-manager refactor (llm.ProviderManager) the registry
	// should accept an interface (llm.Chatter) or hold both the concrete
	// client and the provider-manager; for now we keep *llm.Client as the
	// backing field but callers should be aware this is a coupling that may
	// need decoupling in a future refactor.
	memvid          *memvid.Client
	taskStore       *task.Store
	llm             *llm.Client
	resolver        *llm.Resolver
	bus             *bus.MessageBus
	security        *security.PermissionChecker
	tools           ToolRegistry
	shadowMgr       *shadow.Manager
	capabilityIndex *skills.CapabilityIndex
	skillLoader     *skills.LazySkillLoader
	logger          *slog.Logger

	// Agent validation components (shared across all agent loops)
	watchdog              *Watchdog
	hallucinationDetector *HallucinationDetector
	artifactManager       *ArtifactManager

	// TT-SR stream rule enforcement (shared across all agent loops)
	ttsrManager *TTSRManager

	// components resolves prompt component IDs to their content. May be nil
	// when BundledPromptsPath was not set; in that case the AGENT.md body
	// alone becomes the Purpose.
	components *agents.ComponentRegistry

	// Queue tracking for conversation-scoped queue management
	activeQueues   map[string]*QueueEntry
	activeQueuesMu sync.RWMutex
	nextGen        uint64

	// Queues config from config file (overrides code defaults)
	queueConfig config.AgentQueuesConfig

	// guardsCfg holds [agent.guards] settings from the config file
	// (leaf 07); applied to every loop created by this registry.
	guardsCfg config.AgentGuardsConfig

	// schemaModeCfg holds the [agent.tools] tool-schema settings (leaf 04).
	// It is applied to the shared parent tool registry so every task-scoped
	// loop observes the same stubbed definitions through its filtered view
	// (FilteredToolRegistry delegates GetDefinitions to the parent). Both
	// fields are guarded by mu.
	schemaModeCfg config.AgentToolsConfig
	schemaModeSet bool

	// Learning + evolution wiring (WikiSkill raw layer, arXiv:2608.27454):
	// shared across all registry-created loops so specialist agents record
	// traces, skill outcomes, and learned patterns identically to the
	// primary daemon loop. All three are optional (nil = not wired).
	traceWriter      TraceWriter
	usageTracker     lifecycleUsageTracker
	learningPipeline LearningPipeline

	// db is the SQLite connection for queue persistence (may be nil).
	db *sql.DB

	// quotaTracker is the shared quota episode tracker. Nil = quota
	// tracking disabled for loops built by this registry.
	quotaTracker *QuotaEpisodeTracker

	// sharedConvStore is a single ConversationStore shared across all agent
	// loops so that cross-agent handoffs preserve conversation history.
	sharedConvStore *ConversationStore

	// globalRefusalModel mirrors RegistryConfig.GlobalRefusalModel: the
	// global models.json5 refusal_model slot stamped onto every
	// registry-built loop (bughunt F5).
	globalRefusalModel string

	// piiRestore mirrors RegistryConfig.PiiRestore: the shared PII
	// unmasking restore lane stamped onto every registry-built loop
	// (bughunt C19). Without it, registry-built loops (delegate children
	// via RunAgent, specialists) shipped provider-bound context unmasked
	// and never restored [PII:*] tokens in their replies — only the
	// daemon template lane had the hook via GetOrCreateWired. Nil =
	// feature off (byte-identical legacy behavior). Guarded by mu.
	piiRestore *PiiRestoreHook

	// plannerChatter mirrors RegistryConfig.PlannerChatter: the dedicated
	// models.json5 planner_model client used ONLY when createLoop builds
	// the planner agent's loop (issue #53 direction 4). Nil = the planner
	// loop uses the shared chain like every other loop.
	plannerChatter llm.Chatter

	// acpExecutor is the shared external acp turn producer (dim 3A)
	// stamped onto every loop this registry builds. Nil ([acp] disabled
	// or no daemon wiring) leaves executor: acp turns failing loudly
	// with ErrACPExecutorNotWired — never the local LLM loop.
	acpExecutor *ACPExecutor

	// sharedChatter mirrors RegistryConfig.SharedChatter: the daemon-wide
	// ProviderManager for every registry-built loop (nil = single-client
	// chain, byte-identical to the pre-SharedChatter behavior).
	sharedChatter llm.Chatter

	// hookRegistry mirrors RegistryConfig.HookRegistry: the daemon-wide
	// lifecycle hook registry every registry-built loop inherits (nil =
	// no hooks wired; the security hardening and decision gate are then
	// inert on dispatched turns — production wiring always sets it).
	hookRegistry *HookRegistry

	// compactionCfg mirrors RegistryConfig.Compaction: the user-facing
	// [compaction] block carried into every registry-built loop BEFORE
	// construction, so its ContextFirewall bakes the decision-compaction
	// lane (resolver + decision_enabled).
	compactionCfg config.CompactionConfig

	// groupsResolver mirrors RegistryConfig.GroupsResolver (agent-groups
	// leaf 04): the group policy resolver consulted at loop creation to
	// resolve each spec's record (carried on the loop + executor) and in
	// filterTools for the dispatch intersection. Nil = no groups config
	// (byte-identical legacy behavior).
	groupsResolver *groups.Resolver

	// decisionLogLevel mirrors RegistryConfig.DecisionLogLevel: the C2
	// decision-audit level applied to every loop this registry builds.
	decisionLogLevel string

	// codemodeEnabled mirrors RegistryConfig.CodemodeEnabled
	// (token-efficiency branch 02 leaf 02): when true, filterTools unions
	// the codemode tool into every scoped agent's allow-list so the
	// filtered registry exposes it. Guarded by mu; written only at
	// construction (NewAgentRegistry) — it is immutable after that.
	codemodeEnabled bool

	// dynamicExposureEnabled mirrors RegistryConfig.DynamicExposureEnabled
	// (bughunt agent-core F-2): when true, filterTools unions the
	// tool_search/tool_view meta pair into every scoped agent's allow-list
	// so the dynamic exposure engine's recovery path (§1 layer 3 always-
	// active discoverer+expander) actually exists in the filtered registry
	// the executor consults. Guarded by mu; written only at construction.
	dynamicExposureEnabled bool

	// seedChildConversation is the DSH-1 fork-seed adapter captured at
	// construction: DelegateManager.SetForkSeedWriter binds it so a
	// full-context fork child's seed is stamped onto its conversation
	// before the first turn. Method value on the receiver; immutable.
	seedChildConversation func(agentID, conversationID string, seed []llm.ChatMessage)

	// Lazy-discovery fallback state (leaf 03-agent-recall-wiring):
	// bundledAgentsPath is the path captured at construction so a
	// post-boot discovery rescan uses the same tiers;
	// discoveryRescanned guards the one-shot rescan on a spec miss.
	// Both guarded by mu.
	bundledAgentsPath  string
	allowLateDiscovery bool
	discoveryRescanned bool
}

// RegistryConfig holds configuration for creating an AgentRegistry.
type RegistryConfig struct {
	MemvidClient    *memvid.Client
	TaskStore       *task.Store
	LLMClient       *llm.Client
	Resolver        *llm.Resolver
	MessageBus      *bus.MessageBus
	SecurityChecker *security.PermissionChecker
	ToolRegistry    ToolRegistry
	ShadowManager   *shadow.Manager
	Logger          *slog.Logger

	// Agent validation components (shared across all agent loops)
	Watchdog              *Watchdog
	HallucinationDetector *HallucinationDetector
	ArtifactManager       *ArtifactManager

	// TT-SR stream rule enforcement (shared across all agent loops)
	TTSRManager *TTSRManager

	// QuotaEpisodeTracker is the shared quota episode tracker (singleton).
	// When set, registry-built agent loops inherit it so quota episodes and
	// escalation events fire identically for specialist agents. Nil = quota
	// tracking disabled for registry loops.
	QuotaTracker *QuotaEpisodeTracker

	// BundledAgentsPath is the path to bundled AGENT.md files (e.g., "config/agents").
	BundledAgentsPath string
	// AllowLateDiscovery opts in to the one-shot post-boot discovery
	// rescan (EnsureLateDiscovered / GetSpec-miss fallback, leaf
	// 03-agent-recall-wiring): definitions seeded into the discovery tiers
	// after daemon boot join the registry and lane index at first
	// dispatch. Wired ONLY by the daemon — test registries must never
	// merge definitions from the host's real discovery tiers mid-test.
	AllowLateDiscovery bool

	// BundledPromptsPath is the path to bundled prompt component markdown
	// files (e.g., "config/prompts"). When set, a ComponentRegistry scans
	// the standard 3-tier hierarchy plus this bundled tier and assembles
	// each agent's Purpose from the components declared in its AGENT.md
	// frontmatter. When unset, prompt component assembly is disabled and
	// the AGENT.md body alone becomes the Purpose (backward compatible).
	BundledPromptsPath string

	// GlobalRules is the global rules content to inject into all agents.
	// If empty, the registry will auto-discover rules using RulesDiscovery.
	GlobalRules string

	// Queues holds steering and follow-up message queue settings from config.
	// When non-zero, these values override the code defaults.
	Queues config.AgentQueuesConfig

	// Guards holds [agent.guards] loop-safety settings (leaf 07). Zero
	// values normalize to ship-on defaults at loop construction.
	Guards config.AgentGuardsConfig

	// DB is an optional SQLite connection used for queue persistence.
	// When set, follow-up messages are persisted to the queued_followups table.
	DB *sql.DB

	// ConversationStoreSize is the LRU capacity of the shared ConversationStore.
	// When <= 0, DefaultConversationStoreSize is used.
	ConversationStoreSize int

	// GlobalRefusalModel is the global models.json5 refusal_model slot
	// (bughunt F5). When set, every registry-built agent loop inherits it as
	// the fallback of last resort in refusalFallbackRef (spec.RefusalModel
	// still outranks it). Empty = refusal fallback feature off unless the
	// per-agent spec sets its own RefusalModel.
	GlobalRefusalModel string

	// PlannerChatter is the dedicated chatter (built at daemon wiring time
	// from the models.json5 planner_model slot; issue #53 direction 4).
	// When set, the registry builds the planner agent's loop on this
	// chatter instead of the shared LLMClient and blanks the loop's
	// modelRef so the planner alias resolution cannot retarget the
	// dedicated client. Nil = every loop uses the shared chain (pre-slot
	// behavior, byte-identical).
	PlannerChatter llm.Chatter

	// SharedChatter is the daemon-wide chatter for registry-built loops —
	// the ProviderManager when multi-provider wiring is on, nil otherwise.
	// When set, EVERY loop (not just the planner) is built on it via
	// WithLLMChatter, LLMClient remaining the fallback for callers that
	// never wire a manager. Without this, dispatched chat/specialist loops
	// ran on the single default client: WithResolvedModel named a provider
	// the client cannot reach (requestModelOverride lane 2 rejects a
	// cross-provider selection), so alias rotation and the session-affinity
	// reporting seam never engaged for dispatched turns — observed live by
	// the session-affinity e2e suites (rotation: every attempt redialed the
	// same dead endpoint; teardown: the endpoint snapshot stayed empty).
	// Model-switcher semantics are unaffected: a ProviderManager has no
	// single config to retarget, and chatWithFailoverRaw carries the
	// resolution request-scoped via WithResolvedModel.
	SharedChatter llm.Chatter

	// HookRegistry is the daemon-wide lifecycle hook registry (security
	// before-tool-call HardError deny, decision gate, transform hooks).
	// Registry-built loops MUST inherit it: without it every
	// dispatcher-routed chat/specialist turn executes tools with
	// l.hookRegistry == nil, silently bypassing the security hardening and
	// the decision gate that the daemon template loop enforces. Nil = no
	// hooks (tests, standalone use).
	HookRegistry *HookRegistry

	// Compaction carries the user-facing [compaction] config block into
	// registry-built loops. The loop's ContextFirewall bakes its
	// decision-compaction lane (DecisionEnabled + resolver-derived
	// DecisionClient) AT CONSTRUCTION, so the config must arrive as a
	// LoopOption — the post-construction SetCompactionConfig setter is too
	// late for the firewall. Zero value keeps every lane off (pre-wiring
	// behavior, byte-identical).
	Compaction config.CompactionConfig

	// GroupsResolver is the daemon's group policy resolver (agent-groups
	// leaf 04). When set, every registry-built loop resolves its spec's
	// group record at creation (carried on the loop + executor for the
	// call-time gate) and filterTools intersects the spec allowlist with
	// it (dispatch gate; D8 deny-all via the explicit marker). Nil = no
	// groups config: byte-identical legacy behavior (D7).
	GroupsResolver *groups.Resolver

	// PiiRestore is the shared PII-unmasking restore lane (bughunt C19).
	// When set, every registry-built loop inherits it via SetPiiRestoreHook
	// so specialist/delegate-child turns mask provider-bound context and
	// restore [PII:*] tokens exactly like the daemon template lane. Nil =
	// feature off (byte-identical legacy behavior).
	PiiRestore *PiiRestoreHook

	// DecisionLogLevel is security.groups.decision_log_level (agent-groups
	// leaf 04 daemon wiring): applied to every registry-built loop's
	// executor so the C2 decision-audit lines honor the knob on
	// dispatched turns too. "" = deny-warn default.
	DecisionLogLevel string

	// CodemodeEnabled is [agent.tools].codemode_enabled (token-efficiency
	// branch 02 leaf 02): when true, every registry-built loop's filtered
	// tool registry is granted the codemode tool (filterTools unions it
	// into the scoped agent's allow-list). Default false: the daemon
	// behaves byte-identically to pre-codemode and the tool stays absent
	// from filtered registries.
	CodemodeEnabled bool

	// DynamicExposureEnabled is [agent.tools].dynamic_tool_exposure: when
	// true, filterTools unions the tool_search/tool_view meta pair into
	// every SCOPED agent's allow-list (bughunt agent-core F-2). Design
	// §1 layer 3 makes the pair always-active in dynamic modes, and the
	// row-4 hard execution gate refuses any non-active tool — so an agent
	// whose grant lacks the meta pair would have no recovery path (it
	// could never activate an in-ceiling non-core tool). Most AGENT.md
	// specs grant neither meta tool (only coder grants tool_view), so
	// without this union the gate starves scoped agents. Default false
	// keeps filtered registries byte-identical to the pre-union posture.
	DynamicExposureEnabled bool
}

// NewAgentRegistry creates a new agent registry.
func NewAgentRegistry(cfg RegistryConfig) *AgentRegistry {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	r := &AgentRegistry{
		specs:                  make(map[string]*AgentSpec),
		loops:                  make(map[string]map[string]*AgentLoop),
		activeQueues:           make(map[string]*QueueEntry),
		memvid:                 cfg.MemvidClient,
		taskStore:              cfg.TaskStore,
		llm:                    cfg.LLMClient,
		sharedChatter:          cfg.SharedChatter,
		resolver:               cfg.Resolver,
		bus:                    cfg.MessageBus,
		security:               cfg.SecurityChecker,
		tools:                  cfg.ToolRegistry,
		shadowMgr:              cfg.ShadowManager,
		logger:                 cfg.Logger,
		watchdog:               cfg.Watchdog,
		hallucinationDetector:  cfg.HallucinationDetector,
		artifactManager:        cfg.ArtifactManager,
		ttsrManager:            cfg.TTSRManager,
		quotaTracker:           cfg.QuotaTracker,
		queueConfig:            cfg.Queues,
		guardsCfg:              cfg.Guards,
		db:                     cfg.DB,
		sharedConvStore:        NewConversationStore(cfg.ConversationStoreSize),
		globalRefusalModel:     cfg.GlobalRefusalModel,
		plannerChatter:         cfg.PlannerChatter,
		hookRegistry:           cfg.HookRegistry,
		compactionCfg:          cfg.Compaction,
		groupsResolver:         cfg.GroupsResolver,
		decisionLogLevel:       cfg.DecisionLogLevel,
		codemodeEnabled:        cfg.CodemodeEnabled,
		dynamicExposureEnabled: cfg.DynamicExposureEnabled,
		piiRestore:             cfg.PiiRestore,
	}

	// DSH-1 fork-seed: expose the conversation-seed adapter so a wired
	// DelegateManager (SetForkSeedWriter(r.SeedChildConversation)) can
	// materialize a full-context fork child's seed onto its conversation
	// BEFORE the first turn. The adapter goes through SessionConversation
	// (cache auto-vivify — the child's loop may never have run a turn), a
	// fresh *Conversation, so no eviction hazard exists; the pre-stamped
	// messages become the child's inherited history. No I/O under any lock
	// (RestoreFromMessages is an in-memory copy).
	r.seedChildConversation = r.SeedChildConversation

	// Load global rules
	if cfg.GlobalRules != "" {
		r.globalRules = cfg.GlobalRules
	} else {
		r.globalRules = r.discoverGlobalRules()
	}

	// Build a ComponentRegistry if a bundled prompts path is configured. The
	// ComponentRegistry is used by definitionToSpec/mergeSpec to assemble
	// each agent's Purpose from its declared prompt_components.
	if cfg.BundledPromptsPath != "" {
		r.components = agents.NewDefaultComponentRegistry(cfg.BundledPromptsPath, r.logger)
	}

	// Discover and load AGENT.md definitions (the canonical source of truth).
	r.loadAgentDefinitions(cfg.BundledAgentsPath)
	r.bundledAgentsPath = cfg.BundledAgentsPath
	r.allowLateDiscovery = cfg.AllowLateDiscovery

	return r
}

// ensureAgentDiscovered is the lazy-discovery fallback for registry lookups
// (leaf 03-agent-recall-wiring, chief-of-staff gate): agent definitions
// seeded into the discovery tiers AFTER daemon boot (the e2e harness seeds
// per-test agents into MEEPT_HOME/agents post-Start) would never route —
// a chat lane pinned to a post-boot agent resolved the static chat fallback
// and the gate's per-agent scope asymmetry was unobservable. On a spec MISS,
// run ONE discovery rescan over the same tiers and merge whatever is new.
// Idempotent: loaded definitions merge into existing specs without clobber
// (mergeAgentDefinition semantics), the lane index is republished, and the
// rescan happens at most... per miss, but merges are cheap and only run
// while specs are actually missing. The bundled path is the one captured at
// construction.
func (r *AgentRegistry) ensureAgentDiscovered(id string) {
	r.ensureDiscoveredRescan()
}

// EnsureLateDiscovered runs the one-shot post-boot discovery rescan if it
// has not run yet (leaf 03-agent-recall-wiring). Called at dispatch entry
// so definitions seeded after daemon boot (the e2e harness seeds per-test
// agents into the user tier post-Start) are in the lane routing index
// BEFORE classification — a chat lane pinned to a post-boot agent would
// otherwise resolve the static chat fallback and the per-agent scope
// asymmetry would be unobservable.
func (r *AgentRegistry) EnsureLateDiscovered() {
	r.ensureDiscoveredRescan()
}

// ensureDiscoveredRescan is the guarded one-shot rescan. No-op once run
// (and at construction the full scan already happened, so the flag only
// needs to gate post-boot additions).
func (r *AgentRegistry) ensureDiscoveredRescan() {
	r.mu.RLock()
	rescanned := r.discoveryRescanned
	allowed := r.allowLateDiscovery
	r.mu.RUnlock()
	if rescanned || !allowed {
		return
	}

	r.mu.Lock()
	if r.discoveryRescanned {
		r.mu.Unlock()
		return
	}
	r.discoveryRescanned = true
	path := r.bundledAgentsPath
	r.mu.Unlock()

	r.logger.Debug("registry late discovery rescan")
	r.loadAgentDefinitions(path)
}

// RegisterSpec registers an agent specification.
func (r *AgentRegistry) RegisterSpec(spec *AgentSpec) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if spec.ID == "" {
		return fmt.Errorf("agent spec ID is required")
	}

	r.specs[spec.ID] = spec
	r.logger.Debug("Registered agent spec", "id", spec.ID, "name", spec.Name, "role", spec.Role)
	return nil
}

// UnregisterSpec removes an agent specification and its loop if any.
func (r *AgentRegistry) UnregisterSpec(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.specs, id)
	delete(r.loops, id)
	r.logger.Debug("Unregistered agent spec", "id", id)
}

// agentIDAlias maps a routing-only synthetic agent id to a real agent spec.
// The dispatcher/strategist stamp "orchestrator" on compound/quickplan step
// jobs, but no config/agents/orchestrator/AGENT.md spec exists — so
// GetForTask used to fail ("agent spec not found: orchestrator") and the
// step job fell back to the daemon's tool-less template loop. Route it to
// the canonical executor instead (mirrors config.ToolHintAgent's
// "orchestrator" -> coder mapping). Keep this table tiny and directional:
// it is a compatibility shim for ids that are roles/routing targets, not
// real personas.
var agentIDAlias = map[string]string{
	"orchestrator": config.AgentIDCoder,
}

// canonicalAgentID resolves an alias to a real spec id (identity when no
// alias applies).
func canonicalAgentID(id string) string {
	if real, ok := agentIDAlias[id]; ok {
		return real
	}
	return id
}

// GetSpec returns an agent specification by ID. On a miss, a one-shot
// discovery rescan runs first (ensureAgentDiscovered) so definitions seeded
// after daemon boot (e2e harness per-test agents) are routable.
func (r *AgentRegistry) GetSpec(id string) (*AgentSpec, bool) {
	id = canonicalAgentID(id)
	r.mu.RLock()
	spec, ok := r.specs[id]
	r.mu.RUnlock()
	if ok {
		return spec, ok
	}
	r.ensureAgentDiscovered(id)

	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok = r.specs[id]
	return spec, ok
}

// ListSpecs returns all registered agent specifications.
func (r *AgentRegistry) ListSpecs() []*AgentSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	specs := make([]*AgentSpec, 0, len(r.specs))
	for _, spec := range r.specs {
		specs = append(specs, spec)
	}
	return specs
}

// Get returns the AgentLoop for the given agent, keyed by the synthetic
// "_default" task ID. Preserved for non-task callers (CLI one-shots, manual
// RPCs, dispatcher). New callers should use GetForTask to get per-task
// isolation.
func (r *AgentRegistry) Get(id string) (*AgentLoop, error) {
	if r == nil {
		return nil, fmt.Errorf("agent registry not wired (nil registry)")
	}
	return r.GetForTask(id, "_default")
}

// GetForTask returns the AgentLoop for (agentID, taskID). Lazy-creates:
//  1. The task bucket for this agentID on first access
//  2. The loop on first access for this (agentID, taskID)
//
// Empty taskID defaults to "_default" (matches the Get shim's namespace).
// createLoop is pure struct construction (no I/O — no disk reads, no network
// calls), so holding the write lock through lazy-create is safe per CLAUDE.md
// mutex-scope rule.
func (r *AgentRegistry) GetForTask(agentID, taskID string) (*AgentLoop, error) {
	// Nil-receiver guard (01-safety/07): the strategic planner can run
	// with no registry wired (degraded/unit wiring); a nil *AgentRegistry
	// must surface an actionable error, never a nil-pointer panic at the
	// mutex lock below.
	if r == nil {
		return nil, fmt.Errorf("agent registry not wired (nil registry) for agent %q", agentID)
	}
	// Routing-only ids (e.g. "orchestrator") have no AGENT.md spec; resolve
	// them to a real executor persona so step jobs don't fall back to the
	// tool-less template loop. Loop keys use the canonical id so aliased and
	// direct lookups of the same persona share one task-scoped loop.
	agentID = canonicalAgentID(agentID)
	if taskID == "" {
		taskID = "_default"
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	taskLoops, ok := r.loops[agentID]
	if !ok {
		taskLoops = make(map[string]*AgentLoop)
		r.loops[agentID] = taskLoops
	}

	if loop, ok := taskLoops[taskID]; ok {
		return loop, nil
	}

	spec, ok := r.specs[agentID]
	if !ok {
		return nil, fmt.Errorf("agent spec not found: %s", agentID)
	}

	loop := r.createLoop(spec)
	taskLoops[taskID] = loop
	r.logger.Info("Created task-scoped agent loop", "agent_id", agentID, "task_id", taskID, "name", spec.Name)
	return loop, nil
}

// ReleaseTaskLoops removes all loops for the given taskID across all agents.
// Called by the orchestrator on task completion to free memory and reset
// per-task conversation state. Empty taskID is a no-op (never releases the
// "_default" bucket via this path, preserving backward-compat Get callers).
func (r *AgentRegistry) ReleaseTaskLoops(taskID string) {
	if taskID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for agentID, taskLoops := range r.loops {
		delete(taskLoops, taskID)
		if len(taskLoops) == 0 {
			delete(r.loops, agentID)
		}
	}
}

// GetDispatcher returns the dispatcher agent loop.
func (r *AgentRegistry) GetDispatcher() (*AgentLoop, error) {
	return r.Get(config.AgentIDDispatcher)
}

// modelRefForAgent picks the model reference for an agent's loop.
//
// Precedence: spec.Model (a direct model ref or alias in the AGENT.md
// model: field) > spec.ModelAlias (an explicit models.json5 alias named
// independently of the agent id) > the agent's OWN id when models.json5
// defines an alias under it (compatibility default; the aliases coder,
// planner, analyst, ... exist for exactly that) > ” (the loop keeps the
// client it was built with, which is the resolver's default model).
// Without the same-id fallback the loop resolved no alias at all and ran
// every turn on the config's default model - observed 2026-09-13: the
// coder agent always ran on agnes/agnes-2.5-flash even with
// local/lfm-8b-q4 first in the coder alias, so the local driver was never
// exercised through the daemon.
//
// spec.ModelAlias is returned WITHOUT a HasAlias pre-check: the
// resolver's own alias resolution will error visibly if the alias is
// missing, which is the intended failure mode. A Debug log fires when
// the resolver does not know the named alias so a typo is diagnosable.
//
// An empty return means "no alias": the loop keeps the client it was built
// with, which is the resolver's default model.
func modelRefForAgent(spec *AgentSpec, resolver *llm.Resolver) string {
	if spec == nil {
		return ""
	}
	if spec.Model != "" {
		return spec.Model
	}
	if spec.ModelAlias != "" {
		if resolver != nil && !resolver.HasAlias(spec.ModelAlias) {
			slog.Debug("modelRefForAgent: model_alias names an alias the resolver does not have; resolution will error visibly",
				"agent", spec.ID,
				"model_alias", spec.ModelAlias,
			)
		}
		return spec.ModelAlias
	}
	if resolver != nil && spec.ID != "" && resolver.HasAlias(spec.ID) {
		return spec.ID
	}
	return ""
}

// modelSwitcher is the part of a chat client that alias resolution needs: it
// can report and retarget the model it will call. *llm.Client implements it;
// naming it as an interface means a client reached through the Chatter seam
// (WithLLMChatter accepts any llm.Chatter) can still be retargeted, instead of
// the resolution being silently dropped because the concrete *llm.Client could
// not be recovered from the wrapper.
type modelSwitcher interface {
	SwitchModel(config *llm.ModelConfig) error
	Config() *llm.ModelConfig
}

// modelSwitcherFor returns a retargetable client for the loop: the concrete
// client when it is known, else the chatter if it implements the switcher.
func modelSwitcherFor(client *llm.Client, chatter llm.Chatter) (modelSwitcher, bool) {
	if client != nil {
		return client, true
	}
	if sw, ok := chatter.(modelSwitcher); ok {
		return sw, true
	}
	return nil, false
}

// plannerChatterLive reports whether the planner chatter is usable: the
// interface must be non-nil AND must not carry a typed-nil *llm.Client
// (RegistryConfig is a public struct; the daemon wiring guards this, but
// the check here is defense in depth against the documented typed-nil
// hazard json_extract also guards against).
func plannerChatterLive(c llm.Chatter) bool {
	if c == nil {
		return false
	}
	if cc, ok := c.(*llm.Client); ok && cc == nil {
		return false
	}
	return true
}

func (r *AgentRegistry) createLoop(spec *AgentSpec) *AgentLoop {
	agentCfg := AgentConfig{
		MaxIterations:         spec.Constraints.MaxIterations,
		Timeout:               spec.Constraints.Timeout,
		Purpose:               spec.Purpose,
		MaxConversationTokens: spec.Constraints.MaxConversationTokens,
		GlobalRules:           r.globalRules,
		Guards:                GuardConfig{}.Normalized(),
	}
	// Zero MaxIterations floors at the discovery default (the same rule the
	// definition→spec path applies): a bare RegisterSpec (tests, programmatic
	// registration) carries zero constraints, and WithAgentConfig would
	// otherwise REPLACE the loop's DefaultMaxIterations=25 with 0 — the
	// reasoning cycle then exhausts before its first LLM call
	// ("maximum iterations reached" with zero model calls).
	if agentCfg.MaxIterations == 0 {
		agentCfg.MaxIterations = DefaultMaxIterations
	}
	if r.guardsCfg != (config.AgentGuardsConfig{}) {
		g := r.guardsCfg
		config.NormalizeAgentGuardsDefaults(&g)
		agentCfg.Guards = GuardConfig{
			NoProgressWarnAt:        g.NoProgressWarnAt,
			NoProgressVetoAt:        g.NoProgressVetoAt,
			GracefulAfterVetoes:     g.GracefulAfterVetoes,
			DuplicateSearchRollback: g.DuplicateSearchRollback,
			RollbackWindow:          g.RollbackWindow,
			ReasoningTokenCap:       g.ReasoningTokenCap,
			ReasoningStreakTurns:    g.ReasoningStreakTurns,
			RepeatErrorLimit:        g.RepeatErrorLimit,
		}
	}

	opts := []LoopOption{
		WithAgentConfig(agentCfg),
		WithAgentID(spec.ID),
		WithLoopLogger(r.logger.With("agent", spec.ID)),
		// Progress is default-on platform-wide (AgentConfig
		// ProgressEnabled, schema.go) - the ACP bridge (publishACPUpdate)
		// and the coordination surfaces publish through this lane.
		// Registry-built specialist loops previously ran with it off,
		// silently dropping every ACP session/update from the bus.
		WithProgressEnabled(true),
	}

	// Hook registry inheritance (security-before-tool HardError deny,
	// decision gate, transform hooks): the daemon template loop enforces
	// these through its registry; registry-built loops that ran with
	// l.hookRegistry == nil silently bypassed all of them on every
	// dispatcher-routed turn. Nil-guarded: nil keeps no-hook behavior.
	if r.hookRegistry != nil {
		opts = append(opts, WithHookRegistry(r.hookRegistry))
	}

	// Compaction config BEFORE construction: the loop's ContextFirewall
	// bakes the decision-compaction lane (DecisionEnabled + the
	// resolver-derived DecisionClient) at NewAgentLoop time, so the
	// config must arrive as an option, not via the post-construction
	// setter (too late for the firewall's summarizer selection).
	if r.compactionCfg != (config.CompactionConfig{}) {
		opts = append(opts, WithCompactionConfig(r.compactionCfg))
	}

	if r.llm != nil {
		// Dedicated planner_model slot (issue #53 direction 4): when the
		// daemon resolved the models.json5 planner_model entry, the
		// planner's loop is built on that dedicated client and its
		// modelRef is blanked — otherwise the alias-resolution block in
		// reasoningCycle would SwitchModel the dedicated client onto the
		// planner alias chain and silently discard the slot. Empty slot
		// (nil plannerChatter) leaves every loop on the shared client,
		// byte-identical to the pre-slot behavior.
		if plannerChatterLive(r.plannerChatter) {
			if spec.ID == config.AgentIDPlanner {
				opts = append(opts, WithLLMChatter(r.plannerChatter))
			} else if plannerChatterLive(r.sharedChatter) {
				// Multi-provider wiring: the manager honors WithResolvedModel
				// for the loop's alias and owns rotation/reporting. The
				// single client cannot serve another provider's model
				// (requestModelOverride lane 2), so it must not take the
				// loop when the manager is wired.
				opts = append(opts, WithLLMChatter(r.sharedChatter))
			} else {
				opts = append(opts, WithLLMClient(r.llm))
			}
		} else if plannerChatterLive(r.sharedChatter) {
			opts = append(opts, WithLLMChatter(r.sharedChatter))
		} else {
			opts = append(opts, WithLLMClient(r.llm))
		}
	} else if plannerChatterLive(r.sharedChatter) {
		// r.llm nil but a manager is wired: still prefer the manager.
		opts = append(opts, WithLLMChatter(r.sharedChatter))
	}

	if r.resolver != nil {
		opts = append(opts, WithResolver(r.resolver))
	}

	// Pass the model from the spec (can be alias or direct ref). An agent with
	// no explicit model: still resolves ITS OWN alias when models.json5 defines
	// one under the agent's id - the aliases (coder, planner, analyst, ...)
	// exist for exactly this, and without the fallback the loop resolved no
	// alias at all and every turn ran on the config's default model. Observed
	// 2026-09-13: the coder agent always ran on agnes/agnes-2.5-flash even with
	// local/lfm-8b-q4 first in the coder alias, so the local driver was never
	// exercised through the daemon.
	modelRef := modelRefForAgent(spec, r.resolver)
	if modelRef == "" {
		// Visible fall-through: nothing binds this agent to a model or
		// alias, so the loop keeps the shared default client. Logged once
		// per loop creation (not hot) so the silent global-default path
		// is diagnosable from the daemon log.
		r.logger.Info("no model or alias binding; using the global default model",
			"agent", spec.ID,
		)
	} else {
		// Dedicated planner_model slot (issue #53 direction 4): the slot
		// outranks the planner alias — blank the modelRef so the alias
		// resolution cannot SwitchModel the dedicated client. Any other
		// agent's modelRef passes through untouched.
		if plannerChatterLive(r.plannerChatter) && spec.ID == config.AgentIDPlanner {
			opts = append(opts, WithModelRef(""))
		} else {
			opts = append(opts, WithModelRef(modelRef))
		}
	}

	if r.bus != nil {
		opts = append(opts, WithMessageBus(r.bus))
	}

	if r.security != nil {
		opts = append(opts, WithSecurityChecker(r.security))
	}

	// Loop-carried group resolution (agent-groups leaf 04 T1): resolve the
	// spec's group record ONCE at loop creation and carry it on the loop
	// (the executor's call-time gate consults it; filterTools intersects
	// with the same record). Managed=false records are stamped too — the
	// call-time gate reads Managed and skips, keeping the legacy posture
	// explicit instead of inferred from a nil field.
	if r.groupsResolver != nil {
		res := r.groupsResolver.ResolveForAgent(spec.ID, spec.Groups, spec.GrantedToolNames())
		opts = append(opts, WithGroupResolution(&res))
	}

	if r.memvid != nil {
		opts = append(opts, WithMemvidClient(r.memvid))
	}

	if r.taskStore != nil {
		opts = append(opts, WithTaskStore(r.taskStore))
	}

	if r.tools != nil {
		// Filter tools based on spec
		filtered := r.filterTools(spec)
		// Task-loop schema wiring (leaf 04): push the resolved schema mode
		// into the loop's registry. FilteredToolRegistry does not implement
		// SetSchemaMode; the assertion fires against the shared parent
		// *tools.Registry so the new loop inherits the stubbed definitions
		// through its filtered view. Placeholder registries are guarded by
		// the assertion (no panic).
		r.applySchemaModeLocked()
		opts = append(opts, WithToolRegistry(filtered))
	}

	if r.shadowMgr != nil {
		opts = append(opts, WithShadowManager(r.shadowMgr))
	}

	if r.capabilityIndex != nil {
		opts = append(opts, WithCapabilityIndex(r.capabilityIndex))
	}

	if r.skillLoader != nil {
		opts = append(opts, WithSkillLoader(r.skillLoader))
	}

	// Learning + evolution wiring (WikiSkill raw layer): registry-created
	// loops inherit the trace writer, usage tracker, and learning pipeline
	// so specialist agents (chat/coder/...) record traces, skill outcomes,
	// and patterns identically to the primary daemon loop. Without this,
	// dispatcher-routed turns never reach the knowledge stores.
	if r.traceWriter != nil {
		opts = append(opts, WithTraceWriter(r.traceWriter))
	}
	if r.usageTracker != nil {
		opts = append(opts, WithUsageTracker(r.usageTracker))
	}
	if r.learningPipeline != nil {
		opts = append(opts, WithLearningPipeline(r.learningPipeline))
	}

	// Wire shared validation components into each agent loop
	if r.watchdog != nil {
		opts = append(opts, WithWatchdog(r.watchdog))
	}
	if r.hallucinationDetector != nil {
		opts = append(opts, WithHallucinationDetector(r.hallucinationDetector))
	}
	if r.artifactManager != nil {
		opts = append(opts, WithArtifactManager(r.artifactManager))
	}
	if r.ttsrManager != nil {
		opts = append(opts, WithTTSRManager(r.ttsrManager))
	}

	// Share the quota episode tracker so specialist agents fire quota
	// escalation events and transitions identically to the daemon loop.
	if r.quotaTracker != nil {
		opts = append(opts, WithQuotaTracker(r.quotaTracker))
	}

	// Global refusal_model slot (bughunt F5): registry-built loops inherit
	// the global models.json5 refusal_model so their refusalFallbackRef
	// precedence matches the daemon template's (spec.RefusalModel wins when
	// set, global slot fills an empty spec field, feature off when both
	// empty). Without this, specialist loops silently ran with the fallback
	// feature off.
	if r.globalRefusalModel != "" {
		opts = append(opts, WithGlobalRefusalModel(r.globalRefusalModel))
	}

	// PII restore lane (bughunt C19): registry-built loops inherit the
	// shared *PiiRestoreHook so specialist/delegate-child turns mask
	// provider-bound context (MaskOutboundScoped) and restore [PII:*]
	// tokens in replies (RestoreOutput at the ScanOutput choke point)
	// exactly like the daemon template lane. Nil = feature off.
	if r.piiRestore != nil {
		opts = append(opts, func(l *AgentLoop) {
			l.SetPiiRestoreHook(r.piiRestore)
		})
	}

	// Create a message queue for steering/follow-up support (unless disabled).
	if r.queueConfig.Enabled {
		queueCfg := DefaultQueueConfig()
		if r.queueConfig.MaxSteering > 0 {
			queueCfg.MaxSteering = r.queueConfig.MaxSteering
		}
		if r.queueConfig.MaxFollowUp > 0 {
			queueCfg.MaxFollowUp = r.queueConfig.MaxFollowUp
		}
		if r.queueConfig.SteeringDrain != "" {
			queueCfg.SteeringDrain = ParseDrainMode(r.queueConfig.SteeringDrain)
		}
		if r.queueConfig.FollowUpDrain != "" {
			queueCfg.FollowUpDrain = ParseDrainMode(r.queueConfig.FollowUpDrain)
		}
		queueCfg.PersistFollowUp = r.queueConfig.PersistFollowUp
		if r.queueConfig.FlushDelayMs > 0 {
			queueCfg.FlushDelayMs = r.queueConfig.FlushDelayMs
		}
		queueOpts := []MessageQueueOption{
			WithQueueConfig(queueCfg),
		}
		if r.bus != nil {
			queueOpts = append(queueOpts, WithQueueBus(r.bus))
		}
		if spec.ID != "" {
			queueOpts = append(queueOpts, WithQueueAgentID(spec.ID))
		}
		queueOpts = append(queueOpts, WithQueueLogger(r.logger.With("agent", spec.ID, "component", "queue")))
		opts = append(opts, WithMessageQueue(NewMessageQueue(queueOpts...)))
	}

	// Wire the registry so the loop can register/unregister its queue
	opts = append(opts, WithAgentRegistry(r))

	// External acp turn producer (dim 3A): executor: acp loops route
	// through it; nil leaves the branch failing loudly. The loop reads
	// the registry's own field under r.mu below (createLoop callers
	// already hold r.mu, so snapshot before appending the option).
	if r.acpExecutor != nil {
		opts = append(opts, WithACPExecutor(r.acpExecutor))
	}

	// Share a single ConversationStore across all agent loops so that
	// cross-agent handoffs (dispatcher -> coder -> debugger, etc.) share
	// the same conversation history keyed by conversationID.
	opts = append(opts, WithSharedConversationStore(r.sharedConvStore))

	// Agent loops created by registry use agentID as sessionID
	// workingDir is set later via SetWorkingDir when session binds to project
	loop := NewAgentLoop(spec.ID, "", opts...)
	// dynamic-tools leaf 05: stamp the [agent.tools] schema + exposure config
	// onto the registry-built loop so the warn-once capability-gate in
	// ApplyRequestModel can resolve the active tier. Without this, registry
	// loops have zero-valued schemaModeCfg and the gate never fires. The
	// registry's field is read under r.mu (createLoop callers hold it);
	// loop.mu is uncontended on a fresh loop — no deadlock.
	if r.schemaModeSet {
		loop.SetSchemaModeConfig(r.schemaModeCfg)
	}
	// Decision-log level (agent-groups leaf 04): every registry-built
	// loop's executor logs the C2 permission decision lines at the
	// configured knob ("" = deny-warn default).
	if r != nil && r.decisionLogLevel != "" {
		loop.SetExecutorDecisionLogLevel(r.decisionLogLevel)
	}
	return loop
}

// filterTools returns a filtered tool registry based on agent spec.
// Each agent only gets its baseline tools plus its additional tools,
// reducing the number of tool definitions sent per LLM call.
// When CanDelegate is false, the delegate_task baseline tool is stripped.
//
// The allow-list is normalized to registered tool names (a grant of
// shell_execute resolves to the registered `shell` tool) and capped when the
// spec sets tool_scope_limit (tool-list scoping; 0 = no cap, existing
// behavior). See AgentSpec.GrantedToolNames / ScopedToolNames.
//
// Group intersection (agent-groups leaf 04 T2): when the registry carries a
// groups resolver and the agent resolves MANAGED, the spec allowlist is
// further intersected with the record's AllowedTools minus its DeniedTools
// (binding never widens the spec, D4). A managed record with an EMPTY
// effective allowlist means DENY-ALL — the explicit emptyAllowAll marker
// registry keeps FilteredToolRegistry's legacy empty=all semantics from
// firing (D8). Ungrouped/unmanaged agents are byte-identical (D7).
func (r *AgentRegistry) filterTools(spec *AgentSpec) ToolRegistry {
	if r.tools == nil {
		return nil
	}

	// Presence-filtered cap (bughunt config-tools F-3): a baseline name
	// whose registration is conditional (knowledge_* under
	// memory.knowledge.enabled) must not consume a cap slot when the
	// feature is off — the dead name would silently displace a real tail
	// tool from the grant. Registry presence is checked at boot-stable
	// filter time; nil registry keeps the legacy cap shape.
	baselinePresent := func(name string) bool {
		return r.tools.Get(name) != nil
	}
	allowedTools := spec.ScopedToolNamesPresent(spec.ToolScopeLimit, baselinePresent)
	if !spec.CanDelegate {
		// Strip the FULL delegation tool family (DSH-7): delegate_task alone
		// left the C1 control tools (subagent_send_message /
		// subagent_interrupt / subagent_list_children) reachable for a
		// can_delegate:false agent whenever additional_tools granted one —
		// a can't-spawn agent could still steer/interrupt/enumerate another
		// session's children through the control surface.
		for _, name := range delegationToolNames() {
			allowedTools = removeString(allowedTools, name)
		}
	}
	// Codemode gate (token-efficiency branch 02 leaf 02): when [agent.tools]
	// codemode_enabled is on, union the codemode tool into a SCOPED agent's
	// allow-list so its filtered registry exposes it. Only scoped agents
	// (non-empty allow-list) need the explicit union: the empty-scope early
	// return below hands out the unfiltered registry, which already carries
	// codemode when it is registered. Gate-off leaves allowedTools
	// byte-identical to the pre-codemode path. r.codemodeEnabled is read
	// under r.mu (filterTools runs inside createLoop under the write lock).
	if r.codemodeEnabled && len(allowedTools) > 0 {
		allowedTools = appendUnique(allowedTools, "codemode")
	}
	// Meta-pair union (bughunt agent-core F-2, design §1 layer 3): the
	// dynamic exposure engine's row-4 hard gate refuses every non-active
	// tool, so tool_search + tool_view must be reachable for ANY agent
	// with an armed plan — the executor looks tools up in the FILTERED
	// registry, and most AGENT.md specs grant neither meta tool. Same
	// scoped-agents-only shape as the codemode gate above; flag-off
	// leaves allowedTools byte-identical.
	if r.dynamicExposureEnabled && len(allowedTools) > 0 {
		allowedTools = appendUnique(allowedTools, "tool_search")
		allowedTools = appendUnique(allowedTools, "tool_view")
	}
	if len(allowedTools) == 0 {
		return r.tools
	}

	// Group intersection (T2): resolve BEFORE the legacy empty check so a
	// managed deny-all cannot fall through to the unfiltered registry.
	if res := r.resolveForSpec(spec); res != nil && res.Managed {
		denied := make(map[string]struct{}, len(res.DeniedTools))
		for _, d := range res.DeniedTools {
			denied[d] = struct{}{}
		}
		allowed := make(map[string]struct{}, len(res.AllowedTools))
		for _, a := range res.AllowedTools {
			allowed[a] = struct{}{}
		}
		intersected := make([]string, 0, len(allowedTools))
		for _, t := range allowedTools {
			if _, ok := denied[t]; ok {
				continue
			}
			if _, ok := allowed[t]; ok {
				intersected = append(intersected, t)
			}
		}
		if len(intersected) == 0 {
			// D8 deny-all: explicit marker, never the legacy empty=all.
			return NewFilteredToolRegistry(r.tools, nil, WithDenyAll())
		}
		return NewFilteredToolRegistry(r.tools, intersected)
	}

	return NewFilteredToolRegistry(r.tools, allowedTools)
}

// resolveForSpec resolves the spec's group record through the registry's
// resolver (nil resolver or empty spec id => nil). The record is the SAME
// one stamped onto the loop (createLoop), so the dispatch gate and the
// call-time gate can never disagree.
func (r *AgentRegistry) resolveForSpec(spec *AgentSpec) *groups.Resolved {
	if r.groupsResolver == nil || spec == nil {
		return nil
	}
	res := r.groupsResolver.ResolveForAgent(spec.ID, spec.Groups, spec.GrantedToolNames())
	return &res
}

// GetByRole returns all agent loops with the given role.
// Holds the read lock during all lookups to avoid TOCTOU races where a
// concurrent UnregisterSpec could remove a loop between the spec match
// and the Get call.
func (r *AgentRegistry) GetByRole(role AgentRole) []*AgentLoop {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var loops []*AgentLoop
	for _, spec := range r.specs {
		if spec.Role != role {
			continue
		}
		if taskLoops, ok := r.loops[spec.ID]; ok {
			// Collect all task-scoped loops for this spec, in stable key order.
			for _, loop := range taskLoops {
				loops = append(loops, loop)
			}
		}
	}
	return loops
}

// GetExecutors returns all executor agent loops.
func (r *AgentRegistry) GetExecutors() []*AgentLoop {
	return r.GetByRole(RoleExecutor)
}

// findReviewerByDomain returns the ID of the first reviewer-role agent whose
// reviews_domain matches the given domain, or "" if none match. Used by
// ReviewPolicy.SelectReviewer for dynamic reviewer routing.
func (r *AgentRegistry) findReviewerByDomain(domain string) string {
	if domain == "" {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	// First pass: exact match on reviews_domain.
	for _, spec := range r.specs {
		if spec.Role == RoleReviewer && spec.ReviewsDomain == domain && spec.Enabled {
			return spec.ID
		}
	}
	return ""
}

// RunAgent sends a single message to an agent and returns its response.
//
// Context isolation (leaf 10): the message is treated as a structured brief
// — built by the caller through BuildSpawnContext — and starts a FRESH
// conversation keyed by conversationID. The child never inherits a parent
// conversation's transcript; cross-agent context flows via explicit
// SpawnContext briefs (report-router handoffs, step AccumulatedContext), not
// shared message lists.
func (r *AgentRegistry) RunAgent(ctx context.Context, agentID, message, conversationID string) (string, error) {
	loop, err := r.Get(agentID)
	if err != nil {
		return "", err
	}

	return loop.RunOnce(ctx, message, conversationID)
}

// RunAgentWithModelRef sends a single message to an agent with a per-run model
// override (delegation route enforcement, C10): the resolved child route is
// armed through the loop's ONE-SHOT ApplyRequestModel seam (the same precedence
// slot as a client-supplied per-request model) so the delegated child's FIRST
// LLM call runs the RESOLVED route — the allowlist-gated route the manager
// recorded — instead of silently falling through to the child spec's own model
// pin. Empty modelRef behaves exactly like RunAgent (no override armed). The
// override is one-shot: it auto-clears after the first reasoning cycle and
// never persists onto the agent's other conversations.
//
// R3-2 (turn-scoped override): the override is armed on the SHARED per-agent
// loop, so a child run that parks or errors BEFORE the reasoning cycle
// consumes it used to leave the override stuck — the next unrelated turn on
// that loop silently ran the child's route. The override is cleared as soon
// as this run returns (defer ClearModelOverride-style): if the turn consumed
// it, the one-shot clear already ran and this is a no-op; if it never did,
// nothing leaks to the next turn. The armed window (arm → RunOnce) is the
// same slot the dispatcher's ApplyRequestModel uses for user directives; a
// full per-turn context-carried mechanism does not exist on the RunOnce API
// (the consumption site reads loop state in reasoningCycle), so the
// defer-clear shape is the turn-scoping mechanism chosen here.
func (r *AgentRegistry) RunAgentWithModelRef(ctx context.Context, agentID, message, conversationID, modelRef string) (string, error) {
	loop, err := r.Get(agentID)
	if err != nil {
		return "", err
	}
	// Turn-scoped (R3-2): no transient override survives past THIS run —
	// armed here by ApplyRequestModel below, or left stuck by an earlier
	// child run that parked/errored before consumption (which would
	// otherwise route the NEXT delegation turn on the shared loop down the
	// dead child's route). A persistent override (shadow hot-swap) is NOT
	// demoted: the guard matches SetModelOverride's own anti-demotion rule.
	defer func() {
		if !loop.IsModelOverridePersistent() {
			loop.ClearModelOverride()
		}
	}()
	if modelRef != "" {
		loop.ApplyRequestModel(modelRef)
	}
	return loop.RunOnce(ctx, message, conversationID)
}

// Close shuts down all agent loops and releases resources.
func (r *AgentRegistry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Close all active conversation-scoped queues.
	r.activeQueuesMu.Lock()
	for convID, entry := range r.activeQueues {
		if entry != nil && entry.Queue != nil {
			entry.Queue.Close() //nolint:mutexio // registry teardown; no concurrent callers expected
		}
		delete(r.activeQueues, convID)
	}
	r.activeQueuesMu.Unlock()

	// Clear all loops (AgentLoop has no explicit Stop/Close method,
	// but dropping references allows GC to reclaim resources).
	r.loops = make(map[string]map[string]*AgentLoop)

	// Close the database connection used for queue persistence.
	var firstErr error
	if r.db != nil {
		if err := r.db.Close(); err != nil && firstErr == nil { //nolint:mutexio // registry teardown; no concurrent callers expected
			firstErr = err
		}
	}

	r.logger.Info("Agent registry closed")
	return firstErr
}

// MemvidClient returns the shared memvid client.
func (r *AgentRegistry) MemvidClient() *memvid.Client {
	return r.memvid
}

// Stats returns registry statistics.
func (r *AgentRegistry) Stats() map[string]int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return map[string]int{
		"specs":        len(r.specs),
		"active_loops": r.loopCount(),
	}
}

// loopCount returns the total number of loops across all agents and tasks.
// Caller must hold r.mu.
func (r *AgentRegistry) loopCount() int {
	n := 0
	for _, taskLoops := range r.loops {
		n += len(taskLoops)
	}
	return n
}

// CapabilitiesMap returns the capabilities map (may be nil).
func (r *AgentRegistry) CapabilitiesMap() *CapabilitiesMap {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.capabilitiesMap
}

// SetACPExecutor wires the external acp turn producer (dim 3A) onto this
// registry: every loop createLoop builds afterwards inherits it. Nil
// receiver and nil executor are no-ops (repo setter rule) — [acp]
// disabled simply never calls this, and executor: acp turns then fail
// loudly with ErrACPExecutorNotWired.
func (r *AgentRegistry) SetACPExecutor(e *ACPExecutor) {
	if r == nil || e == nil {
		return
	}
	r.mu.Lock()
	r.acpExecutor = e
	r.mu.Unlock()
}

// ACPExecutor returns the wired external acp turn producer (nil = none).
func (r *AgentRegistry) ACPExecutor() *ACPExecutor {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.acpExecutor
}

// SetTraceWriter wires the turn-trace persistence hook into every loop this
// registry creates (WikiSkill raw layer). Nil is a no-op.
func (r *AgentRegistry) SetTraceWriter(tw TraceWriter) {
	if tw == nil {
		return
	}
	r.mu.Lock()
	r.traceWriter = tw
	r.mu.Unlock()
}

// SetUsageTracker wires the skill usage tracker into every loop this
// registry creates (closed-loop skill evolution). Nil is a no-op.
func (r *AgentRegistry) SetUsageTracker(ut lifecycleUsageTracker) {
	if ut == nil {
		return
	}
	r.mu.Lock()
	r.usageTracker = ut
	r.mu.Unlock()
}

// SetPiiRestoreHook wires the shared PII-unmasking restore lane into every
// loop this registry creates (bughunt C19): registry-built loops (delegate
// children via RunAgent, specialists) then mask provider-bound context and
// restore [PII:*] tokens exactly like the daemon template lane. Nil is a
// no-op. Loops already created keep their wiring (the lane is stateless per
// call, scoped by the sessionID argument); loops created afterwards inherit
// the hook at construction.
func (r *AgentRegistry) SetPiiRestoreHook(hook *PiiRestoreHook) {
	if hook == nil {
		return
	}
	r.mu.Lock()
	r.piiRestore = hook
	r.mu.Unlock()
}

// SetSchemaModeConfig supplies the [agent.tools] tool-schema settings
// (chat-dispatch-ux leaf 04) for task-scoped loops created by this registry.
// The effective mode is pushed into the shared parent tool registry, so the
// setting covers every loop already served by the registry as well as every
// loop createLoop builds afterwards: FilteredToolRegistry delegates
// GetDefinitions to its parent, so parent-level stubbing is exactly what
// task-scoped loops observe. Resolution semantics mirror the primary loop's
// applySchemaModeLocked (loop.go): "" / anything-but-"full" resolves to
// indexed (default-on), "full" restores legacy full-schema payloads;
// an empty AlwaysFull falls back to config.DefaultAlwaysFullTools(). Safe
// for placeholder registries that do not implement SetSchemaMode (guarded
// by an interface assertion — no panic).
func (r *AgentRegistry) SetSchemaModeConfig(cfg config.AgentToolsConfig) {
	r.mu.Lock()
	r.schemaModeCfg = cfg
	r.schemaModeSet = true
	r.applySchemaModeLocked()
	r.mu.Unlock()
}

// applySchemaModeLocked pushes the effective schema mode into the shared
// parent tool registry. r.mu must be held by the caller. No-op when the
// registry does not expose SetSchemaMode (e.g. placeholder registries).
func (r *AgentRegistry) applySchemaModeLocked() {
	if r.tools == nil {
		return
	}
	sm, ok := r.tools.(interface {
		SetSchemaMode(tools.SchemaMode, []string)
	})
	if !ok {
		return
	}

	mode := tools.SchemaModeIndexed
	alwaysFull := config.DefaultAlwaysFullTools()
	if r.schemaModeSet {
		if r.schemaModeCfg.SchemaMode == "full" {
			mode = tools.SchemaModeFull
		}
		if len(r.schemaModeCfg.AlwaysFull) > 0 {
			alwaysFull = r.schemaModeCfg.AlwaysFull
		}
	}
	sm.SetSchemaMode(mode, alwaysFull)
}

// SetLearningPipeline wires the learning pipeline (Judge/Distill) into every
// loop this registry creates. Nil is a no-op.
func (r *AgentRegistry) SetLearningPipeline(lp LearningPipeline) {
	if lp == nil {
		return
	}
	r.mu.Lock()
	r.learningPipeline = lp
	r.mu.Unlock()
}

// SetCapabilitiesMap sets the capabilities map for fast routing.
func (r *AgentRegistry) SetCapabilitiesMap(capMap *CapabilitiesMap) {
	if capMap == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.capabilitiesMap = capMap
	r.logger.Debug("Capabilities map set", "agents", capMap.Count())
}

// discoverGlobalRules loads global rules from the discovery hierarchy.
func (r *AgentRegistry) discoverGlobalRules() string {
	discovery := agents.NewRulesDiscovery(r.logger)
	content, path := discovery.DiscoverWithPath()
	if path != "" {
		r.logger.Info("Loaded global rules", "path", path)
	} else {
		r.logger.Debug("Using embedded default rules")
	}
	return content
}

// loadAgentDefinitions discovers AGENT.md files and loads them into the
// registry. AGENT.md is the canonical source of truth for agent definitions;
// there are no programmatic DefaultSpecs anymore. Definitions with
// enabled: false in their frontmatter are filtered out at load time.
func (r *AgentRegistry) loadAgentDefinitions(bundledPath string) {
	// Build discovery options
	opts := []agents.DiscoveryOption{
		agents.WithDiscoveryLogger(r.logger),
	}
	if bundledPath != "" {
		opts = append(opts, agents.WithBundledPath(bundledPath))
	}

	// Create discovery and discover AGENT.md files
	discovery := agents.NewDiscovery(opts...)
	definitions, err := discovery.Discover()
	if err != nil {
		r.logger.Warn("Failed to discover agent definitions", "error", err)
		return
	}

	if len(definitions) == 0 {
		r.logger.Debug("No AGENT.md definitions discovered")
		return
	}

	loaded := 0
	disabled := 0
	for _, def := range definitions {
		// Filter disabled agents at load time. Agents with enabled absent
		// or nil in frontmatter default to enabled.
		if !def.IsEnabled() {
			disabled++
			r.logger.Info("Skipping disabled agent", "id", def.ID)
			// Also remove any prior spec with the same ID (in case the agent
			// was previously enabled and is now toggled off).
			r.mu.Lock()
			delete(r.specs, def.ID)
			delete(r.loops, def.ID)
			r.mu.Unlock()
			continue
		}
		r.mergeAgentDefinition(def)
		loaded++
	}

	r.logger.Info("Loaded agent definitions from AGENT.md files",
		"count", loaded, "disabled", disabled,
	)

	// Publish the frontmatter-derived lane routing index so agentForIntent
	// resolves lanes from AGENT.md `intents:` first (no Go change to add a
	// routable specialist). Falls back to the static tables for undeclared
	// lanes; see publishLaneIndex and internal/agent/llm_classifier.go.
	r.publishLaneIndex()
}

// publishLaneIndex rebuilds the frontmatter-derived lane routing index from
// the registry's loaded specs and installs it for agentForIntent. A lane is
// attributed to the first spec (in sorted ID order) that declares it, so the
// result is deterministic. Called after AGENT.md definitions load; with no
// declared intents the index is cleared and agentForIntent uses its static
// fallbacks. Safe to call concurrently: it snapshots under the read lock and
// installs the finished map outside it (no I/O while holding the lock).
func (r *AgentRegistry) publishLaneIndex() {
	r.mu.RLock()
	ids := make([]string, 0, len(r.specs))
	for id := range r.specs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	idx := make(map[string]string)
	for _, id := range ids {
		spec := r.specs[id]
		for _, lane := range spec.Intents {
			lane = strings.TrimSpace(lane)
			if lane == "" {
				continue
			}
			if _, exists := idx[lane]; !exists {
				idx[lane] = spec.ID
			}
		}
	}
	r.mu.RUnlock()
	PublishLaneAgentIndex(idx)
}

// mergeAgentDefinition merges an AGENT.md definition into the registry.
// If a spec with the same ID exists, non-empty fields from the definition override it.
// If no spec exists, a new one is created from the definition.
func (r *AgentRegistry) mergeAgentDefinition(def *agents.AgentDefinition) {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, hasExisting := r.specs[def.ID]

	if hasExisting {
		// Merge: AGENT.md fields override non-empty existing fields
		r.specs[def.ID] = r.mergeSpec(existing, def)
		r.logger.Debug("Merged AGENT.md definition",
			"id", def.ID,
			"source", "merged",
		)
	} else {
		// New spec from AGENT.md only
		r.specs[def.ID] = r.definitionToSpec(def)
		r.logger.Debug("Added AGENT.md definition",
			"id", def.ID,
			"source", "agent.md",
		)
	}

	// Invalidate any existing loop so it gets recreated with new config
	delete(r.loops, def.ID)
}

// mergeSpec merges an AGENT.md definition into an existing spec.
func (r *AgentRegistry) mergeSpec(base *AgentSpec, def *agents.AgentDefinition) *AgentSpec {
	merged := &AgentSpec{
		ID: base.ID,
	}

	// Name: prefer AGENT.md if set
	if def.Name != "" {
		merged.Name = def.Name
	} else {
		merged.Name = base.Name
	}

	// Role: prefer AGENT.md if set
	if def.Role != "" {
		merged.Role = AgentRole(def.Role)
	} else {
		merged.Role = base.Role
	}

	// Description: prefer AGENT.md if set
	if def.Description != "" {
		merged.Description = def.Description
	} else {
		merged.Description = base.Description
	}

	// Enabled: prefer AGENT.md explicit setting; default true.
	if def.Enabled != nil {
		merged.Enabled = *def.Enabled
	} else {
		merged.Enabled = true
	}

	// CanDelegate: AGENT.md boolean wins (default false in metadata).
	merged.CanDelegate = def.CanDelegate || base.CanDelegate

	// ReviewsDomain: prefer AGENT.md if set
	if def.ReviewsDomain != "" {
		merged.ReviewsDomain = def.ReviewsDomain
	} else {
		merged.ReviewsDomain = base.ReviewsDomain
	}

	// Intents: MERGE (union) so a lane declared in either the loaded AGENT.md
	// or a prior spec is retained; the lane routing index reads these.
	merged.Intents = mergeStringSlices(base.Intents, def.Intents)

	// SystemPromptSections: carry from base (AGENT.md body replaces Purpose, not sections)
	if len(base.SystemPromptSections) > 0 {
		merged.SystemPromptSections = make([]string, len(base.SystemPromptSections))
		copy(merged.SystemPromptSections, base.SystemPromptSections)
	}

	// Purpose: assemble from prompt_components (wrapping the body), or fall
	// back to the AGENT.md body alone. The body is preferred over the base
	// Purpose when present; when neither body nor components are set we keep
	// the base Purpose to avoid wiping an existing prompt.
	body := def.Body
	if body == "" {
		body = base.Purpose
	}
	merged.Purpose = r.assemblePurpose(def.PromptComponents, body)

	// Model: prefer AGENT.md if set
	if def.Model != "" {
		merged.Model = def.Model
	} else {
		merged.Model = base.Model
	}

	// ModelAlias: prefer AGENT.md if set (scalar; no union merge)
	if def.ModelAlias != "" {
		merged.ModelAlias = def.ModelAlias
	} else {
		merged.ModelAlias = base.ModelAlias
	}

	// Groups: SCALAR REPLACEMENT, not union (model_alias precedent, NOT the
	// additional_tools/additional-tools additive lane): a non-empty override
	// list replaces the base list wholesale, since group membership is a
	// declarative set the overriding AGENT.md owns; an omitted (empty) list
	// preserves the base.
	if len(def.Groups) > 0 {
		merged.Groups = append([]string(nil), def.Groups...)
	} else {
		merged.Groups = base.Groups
	}

	// EnhancerModel: prefer AGENT.md if set
	if def.EnhancerModel != "" {
		merged.EnhancerModel = def.EnhancerModel
	} else {
		merged.EnhancerModel = base.EnhancerModel
	}

	// EscalationModel: prefer AGENT.md if set (empty = escalation disabled)
	if def.EscalationModel != "" {
		merged.EscalationModel = def.EscalationModel
	} else {
		merged.EscalationModel = base.EscalationModel
	}

	// RefusalModel: prefer AGENT.md if set (empty = inherit the global
	// models.json5 refusal_model slot; both empty = fallback off)
	if def.RefusalModel != "" {
		merged.RefusalModel = def.RefusalModel
	} else {
		merged.RefusalModel = base.RefusalModel
	}

	// MemoryMaxScope: prefer AGENT.md if set (scalar replacement, C3);
	// omission keeps the base.
	if def.MemoryMaxScope != "" {
		merged.MemoryMaxScope = def.MemoryMaxScope
	} else {
		merged.MemoryMaxScope = base.MemoryMaxScope
	}

	// DelegateContext: prefer AGENT.md if set (scalar replacement, DSH-1);
	// omission keeps the base. The fork gate fails safe to "task" on an
	// unknown value, so no parse-time validation here either.
	if def.DelegateContext != "" {
		merged.DelegateContext = def.DelegateContext
	} else {
		merged.DelegateContext = base.DelegateContext
	}

	// Verification: prefer AGENT.md when the re-loaded frontmatter carries a
	// verification block; omission keeps the current config (D16 gap fix —
	// previously only definitionToSpec wired this, so re-loads silently
	// dropped verification changes).
	if def.Verification != nil {
		merged.Verification = verificationFromMetadata(def.Verification)
	} else {
		merged.Verification = base.Verification
	}

	// Roster quality gate: prefer AGENT.md when the re-loaded frontmatter
	// carries a non-empty gate command; omission keeps the current gate
	// (D16 gap fix). An empty gate block counts as omitted — mirroring
	// definitionToSpec, a gate cannot be cleared via re-load. The preserve
	// path shallow-copies to avoid aliasing the base spec's config.
	if def.Gate != nil && def.Gate.Command != "" {
		gm := *def.Gate
		gm.NormalizeGateDefaults()
		merged.Gate = &RosterGateConfig{
			Command:           gm.Command,
			TimeoutSeconds:    gm.TimeoutSeconds,
			SkipWhenUnchanged: gm.SkipWhenUnchanged,
		}
	} else if base.Gate != nil {
		g := *base.Gate
		merged.Gate = &g
	}

	// Executor: a non-empty AGENT.md value wins; omission keeps the base.
	if def.Executor != "" {
		merged.Executor = def.Executor
	} else {
		merged.Executor = base.Executor
	}

	// ACP: a non-nil AGENT.md block REPLACES the base block wholesale.
	// Per-field ACP merging is deliberately avoided: it invites
	// half-overrides (e.g. a new agent id keeping the old persona's
	// command_args). Omission (nil) preserves the base binding.
	if def.ACP != nil {
		merged.ACP = &ACPSpec{
			Agent:             def.ACP.Agent,
			Persona:           def.ACP.Persona,
			CommandArgs:       append([]string(nil), def.ACP.CommandArgs...),
			PermissionMode:    def.ACP.PermissionMode,
			DecisionThreshold: def.ACP.DecisionThreshold,
		}
	} else if base.ACP != nil {
		a := *base.ACP
		a.CommandArgs = append([]string(nil), base.ACP.CommandArgs...)
		merged.ACP = &a
	}

	// Tools: MERGE (union)
	merged.AdditionalTools = mergeStringSlices(base.AdditionalTools, def.AdditionalTools)

	// Skills: MERGE (union)
	merged.AvailableSkills = mergeStringSlices(base.AvailableSkills, def.AvailableSkills)

	// SkillTriggers: MERGE
	merged.SkillTriggers = mergeStringMaps(base.SkillTriggers, def.SkillTriggers)

	// Constraints: prefer AGENT.md if non-zero
	merged.Constraints = base.Constraints
	if def.MaxIterations > 0 {
		merged.Constraints.MaxIterations = def.MaxIterations
	}
	if def.TimeoutSeconds > 0 {
		merged.Constraints.Timeout = def.Timeout()
	}
	if def.MaxTokensPerTurn > 0 {
		merged.Constraints.MaxTokensPerTurn = def.MaxTokensPerTurn
	}
	if def.MaxConversationTokens > 0 {
		merged.Constraints.MaxConversationTokens = def.MaxConversationTokens
	}
	if def.MaxMemoryRefs > 0 {
		merged.Constraints.MaxMemoryRefs = def.MaxMemoryRefs
	}
	if def.Temperature != nil {
		v := *def.Temperature
		merged.Constraints.Temperature = &v
	} else if base.Constraints.Temperature != nil {
		v := *base.Constraints.Temperature
		merged.Constraints.Temperature = &v
	}
	if def.TopP != nil {
		v := *def.TopP
		merged.Constraints.TopP = &v
	} else if base.Constraints.TopP != nil {
		v := *base.Constraints.TopP
		merged.Constraints.TopP = &v
	}

	return merged
}

// definitionToSpec converts an AGENT.md definition to an AgentSpec.
func (r *AgentRegistry) definitionToSpec(def *agents.AgentDefinition) *AgentSpec {
	defaults := agents.DefaultMetadata()

	spec := &AgentSpec{
		ID:              def.ID,
		Name:            def.Name,
		Description:     def.Description,
		Enabled:         def.IsEnabled(),
		CanDelegate:     def.CanDelegate,
		ReviewsDomain:   def.ReviewsDomain,
		Intents:         append([]string(nil), def.Intents...),
		Groups:          append([]string(nil), def.Groups...),
		Purpose:         r.assemblePurpose(def.PromptComponents, def.Body),
		Model:           def.Model,
		ModelAlias:      def.ModelAlias,
		EnhancerModel:   def.EnhancerModel,
		EscalationModel: def.EscalationModel,
		RefusalModel:    def.RefusalModel,
		MemoryMaxScope:  def.MemoryMaxScope,
		DelegateContext: def.DelegateContext,
		AdditionalTools: append([]string(nil), def.AdditionalTools...),
		AvailableSkills: append([]string(nil), def.AvailableSkills...),
		SkillTriggers:   copyStringMap(def.SkillTriggers),
	}

	// Apply defaults
	if spec.Name == "" {
		spec.Name = def.ID
	}

	// Role
	if def.Role != "" {
		spec.Role = AgentRole(def.Role)
	} else {
		spec.Role = AgentRole(defaults.Role)
	}

	// Constraints
	spec.Constraints.MaxIterations = def.MaxIterations
	if spec.Constraints.MaxIterations == 0 {
		spec.Constraints.MaxIterations = defaults.MaxIterations
	}

	spec.Constraints.Timeout = def.Timeout()

	spec.Constraints.MaxTokensPerTurn = def.MaxTokensPerTurn
	if spec.Constraints.MaxTokensPerTurn == 0 {
		spec.Constraints.MaxTokensPerTurn = defaults.MaxTokensPerTurn
	}

	spec.Constraints.MaxConversationTokens = def.MaxConversationTokens
	spec.Constraints.MaxMemoryRefs = def.MaxMemoryRefs
	if spec.Constraints.MaxMemoryRefs == 0 {
		spec.Constraints.MaxMemoryRefs = defaults.MaxMemoryRefs
	}

	if def.Temperature != nil {
		v := *def.Temperature
		spec.Constraints.Temperature = &v
	}
	if def.TopP != nil {
		v := *def.TopP
		spec.Constraints.TopP = &v
	}

	// Executor: "" (default local loop), "local", or "acp" (validated at
	// parse time in internal/agents).
	spec.Executor = def.Executor

	// ACP binding: only allocated when the frontmatter carries an acp:
	// block; nil-safe for local-loop agents.
	if def.ACP != nil {
		spec.ACP = &ACPSpec{
			Agent:             def.ACP.Agent,
			Persona:           def.ACP.Persona,
			CommandArgs:       append([]string(nil), def.ACP.CommandArgs...),
			PermissionMode:    def.ACP.PermissionMode,
			DecisionThreshold: def.ACP.DecisionThreshold,
		}
	}

	// Verification
	spec.Verification = verificationFromMetadata(def.Verification)

	// Roster quality gate (leaf 04-coder-gates). Nil metadata = no gate.
	if def.Gate != nil && def.Gate.Command != "" {
		gm := *def.Gate
		gm.NormalizeGateDefaults()
		spec.Gate = &RosterGateConfig{
			Command:           gm.Command,
			TimeoutSeconds:    gm.TimeoutSeconds,
			SkipWhenUnchanged: gm.SkipWhenUnchanged,
		}
	}

	return spec
}

// assemblePurpose builds the final Purpose string from prompt components and
// the AGENT.md body. Layout:
//
//  1. Constitution (from the "base.constitution" component if declared, else
//     the DefaultConstitution fallback is used ONLY when no components at
//     all are declared).
//  2. Restrictions (same rule with "base.restrictions" / DefaultRestrictions).
//  3. All other declared components, in the order listed, each rendered as a
//     titled section.
//  4. The AGENT.md body, injected as the "Purpose & Task Principles" section.
//
// When components is empty or the ComponentRegistry is nil, the body alone
// becomes the Purpose (backward compatible with body-only AGENT.md files).
func (r *AgentRegistry) assemblePurpose(components []string, body string) string {
	if r == nil || r.components == nil || len(components) == 0 {
		return body
	}
	sections := r.components.Resolve(components)
	if len(sections) == 0 {
		return body
	}

	var b strings.Builder
	for _, sec := range sections {
		if sec.Content == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("# ")
		b.WriteString(sec.Title)
		b.WriteString("\n\n")
		b.WriteString(sec.Content)
	}
	if body != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("# Purpose & Task Principles\n\n")
		b.WriteString(body)
	}
	return b.String()
}

// Helper functions for merging

func mergeStringSlices(base, overlay []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(base)+len(overlay))

	for _, s := range base {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}

	for _, s := range overlay {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}

	return result
}

func copyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	c := make(map[string]string, len(m))
	maps.Copy(c, m)
	return c
}

func mergeStringMaps(base, overlay map[string]string) map[string]string {
	if base == nil && overlay == nil {
		return nil
	}

	result := make(map[string]string)
	maps.Copy(result, base)
	maps.Copy(result, overlay)
	return result
}

// SetCapabilityIndex sets the capability index for all agents.
// Invalidates existing loops so they get recreated with the new index.
func (r *AgentRegistry) SetCapabilityIndex(ci *skills.CapabilityIndex) {
	if ci == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.capabilityIndex = ci
	// Invalidate all loops so they get recreated with skill discovery
	r.loops = make(map[string]map[string]*AgentLoop)
	r.logger.Debug("Capability index set, agent loops invalidated")
}

// SetSkillLoader sets the lazy skill loader for all agents.
// Invalidates existing loops so they get recreated with the new loader.
func (r *AgentRegistry) SetSkillLoader(loader *skills.LazySkillLoader) {
	if loader == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skillLoader = loader
	// Invalidate all loops so they get recreated with skill loading
	r.loops = make(map[string]map[string]*AgentLoop)
	r.logger.Debug("Skill loader set, agent loops invalidated")
}

// SetTTSRManager sets the TT-SR stream rule manager for all agents.
// Invalidates existing loops so they get recreated with the new manager.
func (r *AgentRegistry) SetTTSRManager(mgr *TTSRManager) {
	if mgr == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ttsrManager = mgr
	// Invalidate all loops so they get recreated with TT-SR enforcement
	r.loops = make(map[string]map[string]*AgentLoop)
	r.logger.Debug("TT-SR manager set, agent loops invalidated")
}

// GlobalRules returns the global rules content.
func (r *AgentRegistry) GlobalRules() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.globalRules
}

// SetGlobalRules sets the global rules content.
func (r *AgentRegistry) SetGlobalRules(rules string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.globalRules = rules

	// Invalidate all loops so they get recreated with new rules
	r.loops = make(map[string]map[string]*AgentLoop)
	r.logger.Info("Global rules updated, agent loops invalidated")
}

// SetAgentRegistry attaches the shared AgentRegistry to an ALREADY-CONSTRUCTED
// loop and mints the steering/follow-up queue the loop lacks (steering-fix,
// 07-bench residual): the daemon singleton template (components.go) is built
// BEFORE NewAgentRegistry, so it can never receive the registry through
// WithAgentRegistry at construction. Without this late wiring, every
// direct-mode chat turn clones the unwired template (ChatHandler.sessionLoop
// -> GetOrCreateWired), RunOnceWithParts' registration guard
// (l.agentRegistry != nil && l.queue != nil) fails, and chat.steer /
// chat.followup fail with ErrQueueNotFound for the whole daemon lifetime --
// exactly the production-rig p5 defect (4/4 acceptance runs).
//
// Queue policy mirrors the registry's createLoop construction
// (queueConfig.Enabled): DefaultQueueConfig-based steering/follow-up queue,
// bus-attached when the registry carries one, agent-id stamped from the
// loop. The queue is minted ONLY when the loop has none -- a loop that
// already carries a queue keeps it (tests, registry-built loops).
//
// Not safe for concurrent wiring with in-flight turns: call once during
// daemon boot, before any turn runs. The l.mu lock protects the field
// writes against concurrent ConfigSnapshot readers.
func (l *AgentLoop) SetAgentRegistry(r *AgentRegistry) {
	if r == nil {
		return
	}
	l.mu.Lock()
	l.agentRegistry = r
	if l.queue == nil && r.queueConfig.Enabled {
		queueCfg := DefaultQueueConfig()
		if r.queueConfig.MaxSteering > 0 {
			queueCfg.MaxSteering = r.queueConfig.MaxSteering
		}
		if r.queueConfig.MaxFollowUp > 0 {
			queueCfg.MaxFollowUp = r.queueConfig.MaxFollowUp
		}
		if r.queueConfig.SteeringDrain != "" {
			queueCfg.SteeringDrain = ParseDrainMode(r.queueConfig.SteeringDrain)
		}
		if r.queueConfig.FollowUpDrain != "" {
			queueCfg.FollowUpDrain = ParseDrainMode(r.queueConfig.FollowUpDrain)
		}
		queueCfg.PersistFollowUp = r.queueConfig.PersistFollowUp
		if r.queueConfig.FlushDelayMs > 0 {
			queueCfg.FlushDelayMs = r.queueConfig.FlushDelayMs
		}
		queueOpts := []MessageQueueOption{
			WithQueueConfig(queueCfg),
		}
		if r.bus != nil {
			queueOpts = append(queueOpts, WithQueueBus(r.bus))
		}
		if l.agentID != "" {
			queueOpts = append(queueOpts, WithQueueAgentID(l.agentID))
		}
		queueOpts = append(queueOpts, WithQueueLogger(l.logger))
		l.queue = NewMessageQueue(queueOpts...)
	}
	l.mu.Unlock()
}

// SteeringQueue returns the loop's steering/follow-up queue (nil when the
// loop has none). Read-only seam for wiring tests and surfaces that must
// observe the queue the loop will register per turn.
func (l *AgentLoop) SteeringQueue() *MessageQueue {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.queue
}

// SteeringRegistry returns the loop's attached agent registry (nil when
// unwired -- the pre-fix daemon template shape). Read-only seam.
func (l *AgentLoop) SteeringRegistry() *AgentRegistry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.agentRegistry
}

// SetChatter replaces the loop's LLM chatter. Test/wiring seam: it must run
// before any turn (concurrent swap mid-turn is not supported -- the executor
// captures the chatter at call time but reasoning state assumes a stable
// client). The daemon never calls this on a live loop; it exists so wiring
// tests can inject a spy into a MANAGER CLONE (GetOrCreateWired returns
// ready-built loops that cannot be re-optioned).
func (l *AgentLoop) SetChatter(c llm.Chatter) {
	l.mu.Lock()
	l.llm = c
	l.mu.Unlock()
}

// RegisterActiveQueue associates a queue with a running conversation.
// The loop parameter is the AgentLoop that owns this queue; it may be nil
// in tests. Returns the generation number for this registration.
//
// Identity canonicalization (20261121-interactive-usability leaf 01):
// aliasIDs are optional extra keys (e.g. the task's linked session-level
// conversation id) mapped to the SAME QueueEntry so steer/followup/
// queue_status addressed by any of the session's identity shapes hit the
// exact key. Aliases share the primary entry's generation and are
// unregistered together with it; empty or duplicate ids are skipped.
func (r *AgentRegistry) RegisterActiveQueue(conversationID string, q *MessageQueue, loop *AgentLoop, aliasIDs ...string) uint64 {
	r.activeQueuesMu.Lock()
	defer r.activeQueuesMu.Unlock()

	r.nextGen++
	entry := &QueueEntry{
		Queue:      q,
		Loop:       loop,
		Generation: r.nextGen,
		PrimaryID:  conversationID,
	}

	r.activeQueues[conversationID] = entry
	// Identity-canonicalization alias (20261121-interactive-usability leaf
	// 01, fix 1): task-dispatched turns execute under an EPHEMERAL
	// step-task-... conversation id, which no session-store lookup can
	// derive, so steer/followup/queue_status addressed to the session's
	// identity 404 (the queue-not-found class). Callers that know the
	// task->session linkage register the SAME QueueEntry under the
	// session-level conversation id as an exact-key alias: resolution step
	// 1 hits it directly, no prefix guessing, and two concurrent tasks can
	// never collide because each alias carries its own live entry.
	// Generation, lifecycle, and the boot-recovery inbox stay owned by the
	// PRIMARY key; aliases are unregistered with it (UnregisterActiveQueue).
	for _, aliasID := range aliasIDs {
		if aliasID == "" || aliasID == conversationID {
			continue
		}
		// Shadow guard (bughunt, 2026-11-23): an alias key may collide
		// with a LIVE registration's PRIMARY key (normal shape: a
		// step-job turn aliases the session conversation id while an
		// interactive chat turn is registered under that same id as its
		// primary). Overwriting that entry silently orphaned it —
		// UnregisterActiveQueue then found the wrong entry under the
		// key, deleted every key pointing at it, and closed a live
		// queue mid-turn (steer/followup 404 + injection into a closed
		// queue), or the real owner's unregister leaked its queue. A
		// primary registration can never be shadowed by an alias: skip
		// when the entry currently under aliasID owns that key as its
		// primary. Alias-vs-alias replacement (a newer turn re-arming
		// the same session alias) is unaffected — the pointer-compare
		// sweep in UnregisterActiveQueue already keeps the older
		// registration's unregister from deleting the newer alias.
		if existing, ok := r.activeQueues[aliasID]; ok && existing != nil &&
			existing != entry && existing.PrimaryID == aliasID {
			r.logger.Debug("skipping active-queue alias: key is another registration's primary",
				"alias_id", aliasID,
				"primary_id", conversationID,
				"existing_primary", existing.PrimaryID,
			)
			continue
		}
		r.activeQueues[aliasID] = entry
	}
	// Boot-recovery inbox wiring (02-routing/03 T2): a live conversation
	// queue is the injection target for persisted follow-ups. Recovery
	// runs again on reconnect/turn start and delivers any rows a previous
	// boot kept because no inbox was listening.
	SetBootRecoveryInbox(conversationID, q)
	if db := r.DB(); db != nil && q != nil {
		RecoverPendingFollowUps(db, r.bus, r.logger)
	}
	return r.nextGen
}

// UnregisterActiveQueue removes the queue when the loop exits.
// Also closes the queue to reject any in-flight injection attempts.
// Any alias keys registered alongside the primary (RegisterActiveQueue's
// aliasIDs) carry the same QueueEntry, so the entry-pointer sweep removes
// them in the same pass — one lifecycle, no orphaned alias keys.
func (r *AgentRegistry) UnregisterActiveQueue(conversationID string) {
	// Snapshot under lock, then close outside to avoid holding the map
	// mutex across queue shutdown I/O (CLAUDE.md mutex scope rule).
	r.activeQueuesMu.Lock()
	entry, exists := r.activeQueues[conversationID]
	if exists {
		delete(r.activeQueues, conversationID)
		// Remove alias keys pointing at the SAME entry (identity
		// canonicalization aliases). A subsequent registration may have
		// replaced the alias with a newer generation — compare the
		// pointer before deleting so only THIS registration's aliases go.
		for key, e := range r.activeQueues {
			if e == entry {
				delete(r.activeQueues, key)
			}
		}
	}
	r.activeQueuesMu.Unlock()

	// Drop the boot-recovery inbox registration: the queue is closing.
	SetBootRecoveryInbox(conversationID, nil)

	if !exists {
		return
	}

	entry.Queue.Close()
}

// GetActiveQueue returns the queue for a running conversation, or nil if not
// found. Also returns the generation number for version checking.
//
// C3 (01-safety/02-queue-lifecycle): the lookup follows the same resolution
// order as ResolveActiveQueue -- exact key -> thread-unwrapped base ->
// session-owned thread keys -- so a session-level id finds the queue RunOnce
// registered under the thread router's thread-scoped id. An ambiguous
// multi-thread match resolves to nil here (the error-carrying twin is
// ResolveActiveQueue); never a silent pick.
func (r *AgentRegistry) GetActiveQueue(conversationID string) (queue *MessageQueue, generation uint64) {
	r.activeQueuesMu.RLock()
	defer r.activeQueuesMu.RUnlock()

	entry, err := r.resolveActiveQueueLocked(conversationID)
	if err != nil {
		return nil, 0
	}

	return entry.Queue, entry.Generation
}

// TaskStore returns the shared task store wired via
// RegistryConfig.TaskStore, or nil when the registry runs without one.
// Read-only surfaces (chat.queue_status task_states, leaf 05-navigation/03
// T2) use this instead of reaching for an unexported field.
func (r *AgentRegistry) TaskStore() *task.Store {
	return r.taskStore
}

// GetActiveQueueLoop returns the AgentLoop that owns the active queue for a
// conversation, or nil if no active queue exists or the entry has no loop.
// Uses the same C3 thread-remap resolution as GetActiveQueue.
func (r *AgentRegistry) GetActiveQueueLoop(conversationID string) *AgentLoop {
	r.activeQueuesMu.RLock()
	defer r.activeQueuesMu.RUnlock()

	entry, err := r.resolveActiveQueueLocked(conversationID)
	if err != nil {
		return nil
	}
	return entry.Loop
}

// ActiveLoopCount returns the number of currently-active queues owned by
// loops built for the given agent ID. It is the live per-agent episode count
// surfaced by the unified agents.roster C1 active_count field (plan tree
// tui-gui-consistency-fixes, leaf 01-backend/02).
//
// The queue entries are snapshotted under activeQueuesMu and that lock is
// RELEASED before calling GetAgentID (which takes the loop's own mutex).
// Holding activeQueuesMu while dereferencing a loop would risk a lock-order
// inversion with the chat path — the registry never nests those two locks.
// Entries with a nil loop (tests) or an empty agent ID carry no identity and
// are not counted. Nil receiver and empty agentID are no-ops.
func (r *AgentRegistry) ActiveLoopCount(agentID string) int {
	if r == nil || agentID == "" {
		return 0
	}

	r.activeQueuesMu.RLock()
	loops := make([]*AgentLoop, 0, len(r.activeQueues))
	for _, entry := range r.activeQueues {
		if entry != nil {
			loops = append(loops, entry.Loop)
		}
	}
	r.activeQueuesMu.RUnlock()

	n := 0
	for _, loop := range loops {
		if loop != nil && loop.GetAgentID() == agentID {
			n++
		}
	}
	return n
}

// GetQueueWithVersion performs a version-check after lookup.
// Returns ErrQueueClosed or ErrGenerationMismatch if the queue is stale.
func (r *AgentRegistry) GetQueueWithVersion(conversationID string, expectedGen uint64) (*MessageQueue, error) {
	r.activeQueuesMu.RLock()
	defer r.activeQueuesMu.RUnlock()

	entry, exists := r.activeQueues[conversationID]
	if !exists {
		return nil, ErrQueueNotFound
	}

	if entry.Generation != expectedGen {
		return nil, ErrGenerationMismatch
	}

	if entry.Queue.IsClosed() {
		return nil, ErrQueueClosed
	}

	return entry.Queue, nil
}

// DB returns the SQLite connection used for queue persistence, or nil.
func (r *AgentRegistry) DB() *sql.DB {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.db
}

// GetModelConfig returns the model configuration for the given agent, or the
// resolver's default model when the agent has no explicit Model ref. Returns
// an error if the agent ID is unknown or if the resolver has no default and
// the agent has no Model. Exposed so the tactical orchestrator can size task
// steps against the executor's ContextLimit without reaching into registry
// internals.
func (r *AgentRegistry) GetModelConfig(agentID string) (*llm.ModelConfig, error) {
	r.mu.RLock()
	spec, ok := r.specs[agentID]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("agent %q not found", agentID)
	}
	if spec.Model == "" {
		// No explicit model on the spec — fall back to resolver default.
		if r.resolver == nil {
			return nil, fmt.Errorf("agent %q has no model and registry has no resolver", agentID)
		}
		if cfg := r.resolver.DefaultModel(); cfg != nil {
			return cfg, nil
		}
		return nil, fmt.Errorf("agent %q has no model and resolver has no default", agentID)
	}
	if r.resolver == nil {
		return nil, fmt.Errorf("agent %q has model %q but registry has no resolver", agentID, spec.Model)
	}
	cfg := r.resolver.ResolveRef(spec.Model)
	if cfg == nil {
		return nil, fmt.Errorf("agent %q model %q could not be resolved (provider disabled or model missing)", agentID, spec.Model)
	}
	return cfg, nil
}
