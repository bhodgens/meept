package agent

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"sync"

	"github.com/caimlas/meept/internal/agents"
	"github.com/caimlas/meept/internal/bus"
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

	// Queue tracking for conversation-scoped queue management
	activeQueues map[string]*QueueEntry
	activeQueuesMu sync.RWMutex
	nextGen      uint64
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

	// BundledAgentsPath is the path to bundled AGENT.md files (e.g., "config/agents").
	BundledAgentsPath string

	// GlobalRules is the global rules content to inject into all agents.
	// If empty, the registry will auto-discover rules using RulesDiscovery.
	GlobalRules string
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
	}

	// Load global rules
	if cfg.GlobalRules != "" {
		r.globalRules = cfg.GlobalRules
	} else {
		r.globalRules = r.discoverGlobalRules()
	}

	// Register default specs
	for _, spec := range DefaultSpecs() {
		if err := r.RegisterSpec(spec); err != nil {
			r.logger.Warn("failed to register default agent spec", "id", spec.ID, "error", err)
		}
	}

	// Discover and merge AGENT.md definitions
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
	return r.Get("dispatcher")
}

// createLoop creates a new agent loop from a spec.
func (r *AgentRegistry) createLoop(spec *AgentSpec) *AgentLoop {
	config := AgentConfig{
		MaxIterations:         spec.Constraints.MaxIterations,
		Timeout:               spec.Constraints.Timeout,
		Purpose:               spec.Purpose,
		MaxConversationTokens: spec.Constraints.MaxConversationTokens,
		GlobalRules:           r.globalRules,
	}

	opts := []LoopOption{
		WithAgentConfig(config),
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

	// Create a message queue for steering/follow-up support
	queueOpts := []MessageQueueOption{
		WithQueueConfig(DefaultQueueConfig()),
	}
	if r.bus != nil {
		queueOpts = append(queueOpts, WithQueueBus(r.bus))
	}
	if spec.ID != "" {
		queueOpts = append(queueOpts, WithQueueAgentID(spec.ID))
	}
	queueOpts = append(queueOpts, WithQueueLogger(r.logger.With("agent", spec.ID, "component", "queue")))
	opts = append(opts, WithMessageQueue(NewMessageQueue(queueOpts...)))

	// Wire the registry so the loop can register/unregister its queue
	opts = append(opts, WithAgentRegistry(r))

	return NewAgentLoop(opts...)
}

// filterTools returns a filtered tool registry based on agent spec.
// Each agent only gets its baseline tools plus its additional tools,
// reducing the number of tool definitions sent per LLM call.
func (r *AgentRegistry) filterTools(spec *AgentSpec) ToolRegistry {
	if r.tools == nil {
		return nil
	}

	allowedTools := spec.AllTools()
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

// RunAgent runs a specific agent with a message and context.
func (r *AgentRegistry) RunAgent(ctx context.Context, agentID, message, conversationID string) (string, error) {
	loop, err := r.Get(agentID)
	if err != nil {
		return "", err
	}

	return loop.RunOnce(ctx, message, conversationID)
}

// Close shuts down all agent loops.
func (r *AgentRegistry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Clear all loops
	r.loops = make(map[string]*AgentLoop)
	r.logger.Info("Agent registry closed")
	return nil
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

// loadAgentDefinitions discovers AGENT.md files and merges with programmatic specs.
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

	// Merge each discovered definition into existing specs
	for _, def := range definitions {
		r.mergeAgentDefinition(def)
	}

	r.logger.Info("Loaded agent definitions from AGENT.md files",
		"count", len(definitions),
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

	// SystemPromptSections: carry from base (AGENT.md body replaces Purpose, not sections)
	if len(base.SystemPromptSections) > 0 {
		merged.SystemPromptSections = make([]string, len(base.SystemPromptSections))
		copy(merged.SystemPromptSections, base.SystemPromptSections)
	}

	// Purpose: prefer AGENT.md body if non-empty
	if def.Body != "" {
		merged.Purpose = def.Body
	} else {
		merged.Purpose = base.Purpose
	}

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
		Purpose:         def.Body,
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
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skillLoader = loader
	// Invalidate all loops so they get recreated with skill loading
	r.loops = make(map[string]*AgentLoop)
	r.logger.Debug("Skill loader set, agent loops invalidated")
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
	r.activeQueuesMu.Lock()
	defer r.activeQueuesMu.Unlock()

	entry, exists := r.activeQueues[conversationID]
	if !exists {
		return
	}

	entry.Queue.Close()
	delete(r.activeQueues, conversationID)
}

// GetActiveQueue returns the queue for a running conversation, or nil if not found.
// Also returns the generation number for version checking.
func (r *AgentRegistry) GetActiveQueue(conversationID string) (*MessageQueue, uint64) {
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
