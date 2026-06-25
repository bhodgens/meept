package agent

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/agents"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory/memvid"
	"github.com/caimlas/meept/internal/shadow"
	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/security"
)

// QueueEntry wraps a MessageQueue with generation tracking.
type QueueEntry struct {
	Queue      *MessageQueue
	Generation uint64
}

// AgentRegistry manages agent specifications and instantiated agent loops.
//
//nolint:revive // stutter with package name is intentional for API clarity
type AgentRegistry struct {
	mu sync.RWMutex

	// Agent specifications
	specs map[string]*AgentSpec

	// Instantiated agent loops (lazy creation)
	loops map[string]*AgentLoop

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

	// db is the SQLite connection for queue persistence (may be nil).
	db *sql.DB

	// sharedConvStore is a single ConversationStore shared across all agent
	// loops so that cross-agent handoffs preserve conversation history.
	sharedConvStore *ConversationStore
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

	// BundledAgentsPath is the path to bundled AGENT.md files (e.g., "config/agents").
	BundledAgentsPath string

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

	// DB is an optional SQLite connection used for queue persistence.
	// When set, follow-up messages are persisted to the queued_followups table.
	DB *sql.DB
}

// NewAgentRegistry creates a new agent registry.
func NewAgentRegistry(cfg RegistryConfig) *AgentRegistry {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	r := &AgentRegistry{
		specs:                 make(map[string]*AgentSpec),
		loops:                 make(map[string]*AgentLoop),
		activeQueues:          make(map[string]*QueueEntry),
		memvid:                cfg.MemvidClient,
		taskStore:             cfg.TaskStore,
		llm:                   cfg.LLMClient,
		resolver:              cfg.Resolver,
		bus:                   cfg.MessageBus,
		security:              cfg.SecurityChecker,
		tools:                 cfg.ToolRegistry,
		shadowMgr:             cfg.ShadowManager,
		logger:                cfg.Logger,
		watchdog:              cfg.Watchdog,
		hallucinationDetector: cfg.HallucinationDetector,
		artifactManager:       cfg.ArtifactManager,
		ttsrManager:           cfg.TTSRManager,
		queueConfig:           cfg.Queues,
		db:                    cfg.DB,
		sharedConvStore:       NewConversationStore(100),
	}

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

	return r
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

// GetSpec returns an agent specification by ID.
func (r *AgentRegistry) GetSpec(id string) (*AgentSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	spec, ok := r.specs[id]
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

// Get returns an agent loop for the given spec ID, creating it if needed.
func (r *AgentRegistry) Get(id string) (*AgentLoop, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Return existing loop if available
	if loop, ok := r.loops[id]; ok {
		return loop, nil
	}

	// Get spec
	spec, ok := r.specs[id]
	if !ok {
		return nil, fmt.Errorf("agent spec not found: %s", id)
	}

	// Create new loop
	loop := r.createLoop(spec)

	r.loops[id] = loop
	r.logger.Info("Created agent loop", "id", id, "name", spec.Name)
	return loop, nil
}

// GetDispatcher returns the dispatcher agent loop.
func (r *AgentRegistry) GetDispatcher() (*AgentLoop, error) {
	return r.Get(config.AgentIDDispatcher)
}

// createLoop creates a new agent loop from a spec.
func (r *AgentRegistry) createLoop(spec *AgentSpec) *AgentLoop {
	agentCfg := AgentConfig{
		MaxIterations:         spec.Constraints.MaxIterations,
		Timeout:               spec.Constraints.Timeout,
		Purpose:               spec.Purpose,
		MaxConversationTokens: spec.Constraints.MaxConversationTokens,
		GlobalRules:           r.globalRules,
	}

	opts := []LoopOption{
		WithAgentConfig(agentCfg),
		WithAgentID(spec.ID),
		WithLoopLogger(r.logger.With("agent", spec.ID)),
	}

	if r.llm != nil {
		opts = append(opts, WithLLMClient(r.llm))
	}

	if r.resolver != nil {
		opts = append(opts, WithResolver(r.resolver))
	}

	// Pass the model from the spec (can be alias or direct ref)
	if spec.Model != "" {
		opts = append(opts, WithModelRef(spec.Model))
	}

	if r.bus != nil {
		opts = append(opts, WithMessageBus(r.bus))
	}

	if r.security != nil {
		opts = append(opts, WithSecurityChecker(r.security))
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

	// Share a single ConversationStore across all agent loops so that
	// cross-agent handoffs (dispatcher -> coder -> debugger, etc.) share
	// the same conversation history keyed by conversationID.
	opts = append(opts, WithSharedConversationStore(r.sharedConvStore))

	return NewAgentLoop(opts...)
}

// filterTools returns a filtered tool registry based on agent spec.
// Each agent only gets its baseline tools plus its additional tools,
// reducing the number of tool definitions sent per LLM call.
// When CanDelegate is false, the delegate_task baseline tool is stripped.
func (r *AgentRegistry) filterTools(spec *AgentSpec) ToolRegistry {
	if r.tools == nil {
		return nil
	}

	allowedTools := spec.AllTools()
	if !spec.CanDelegate {
		allowedTools = removeString(allowedTools, "delegate_task")
	}
	if len(allowedTools) == 0 {
		return r.tools
	}

	return NewFilteredToolRegistry(r.tools, allowedTools)
}

// GetByRole returns all agent loops with the given role.
func (r *AgentRegistry) GetByRole(role AgentRole) ([]*AgentLoop, error) {
	r.mu.RLock()
	specs := make([]*AgentSpec, 0)
	for _, spec := range r.specs {
		if spec.Role == role {
			specs = append(specs, spec)
		}
	}
	r.mu.RUnlock()

	loops := make([]*AgentLoop, 0, len(specs))
	for _, spec := range specs {
		loop, err := r.Get(spec.ID)
		if err != nil {
			return nil, err
		}
		loops = append(loops, loop)
	}
	return loops, nil
}

// GetExecutors returns all executor agent loops.
func (r *AgentRegistry) GetExecutors() ([]*AgentLoop, error) {
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

// RunAgent runs a specific agent with a message and context.
func (r *AgentRegistry) RunAgent(ctx context.Context, agentID, message, conversationID string) (string, error) {
	loop, err := r.Get(agentID)
	if err != nil {
		return "", err
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
	r.loops = make(map[string]*AgentLoop)

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
		"active_loops": len(r.loops),
	}
}

// CapabilitiesMap returns the capabilities map (may be nil).
func (r *AgentRegistry) CapabilitiesMap() *CapabilitiesMap {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.capabilitiesMap
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
		Purpose:         r.assemblePurpose(def.PromptComponents, def.Body),
		Model:           def.Model,
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
	r.loops = make(map[string]*AgentLoop)
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
	r.loops = make(map[string]*AgentLoop)
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
	r.loops = make(map[string]*AgentLoop)
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
	r.loops = make(map[string]*AgentLoop)
	r.logger.Info("Global rules updated, agent loops invalidated")
}

// RegisterActiveQueue associates a queue with a running conversation.
// Returns the generation number for this registration.
func (r *AgentRegistry) RegisterActiveQueue(conversationID string, q *MessageQueue) uint64 {
	r.activeQueuesMu.Lock()
	defer r.activeQueuesMu.Unlock()

	r.nextGen++
	entry := &QueueEntry{
		Queue:      q,
		Generation: r.nextGen,
	}

	r.activeQueues[conversationID] = entry
	return r.nextGen
}

// UnregisterActiveQueue removes the queue when the loop exits.
// Also closes the queue to reject any in-flight injection attempts.
func (r *AgentRegistry) UnregisterActiveQueue(conversationID string) {
	// Snapshot under lock, then close outside to avoid holding the map
	// mutex across queue shutdown I/O (CLAUDE.md mutex scope rule).
	r.activeQueuesMu.Lock()
	entry, exists := r.activeQueues[conversationID]
	if exists {
		delete(r.activeQueues, conversationID)
	}
	r.activeQueuesMu.Unlock()

	if !exists {
		return
	}

	entry.Queue.Close()
}

// GetActiveQueue returns the queue for a running conversation, or nil if not found.
// Also returns the generation number for version checking.
func (r *AgentRegistry) GetActiveQueue(conversationID string) (queue *MessageQueue, generation uint64) {
	r.activeQueuesMu.RLock()
	defer r.activeQueuesMu.RUnlock()

	entry, exists := r.activeQueues[conversationID]
	if !exists {
		return nil, 0
	}

	return entry.Queue, entry.Generation
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
	return r.resolver.ResolveRef(spec.Model), nil
}
