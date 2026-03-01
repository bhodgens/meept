package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory/memvid"
	"github.com/caimlas/meept/internal/shadow"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/security"
)

// AgentRegistry manages agent specifications and instantiated agent loops.
type AgentRegistry struct {
	mu sync.RWMutex

	// Agent specifications
	specs map[string]*AgentSpec

	// Instantiated agent loops (lazy creation)
	loops map[string]*AgentLoop

	// Shared dependencies
	memvid    *memvid.Client
	taskStore *task.Store
	llm       *llm.Client
	bus       *bus.MessageBus
	security  *security.PermissionChecker
	tools     ToolRegistry
	shadowMgr *shadow.Manager
	logger    *slog.Logger
}

// RegistryConfig holds configuration for creating an AgentRegistry.
type RegistryConfig struct {
	MemvidClient    *memvid.Client
	TaskStore       *task.Store
	LLMClient       *llm.Client
	MessageBus      *bus.MessageBus
	SecurityChecker *security.PermissionChecker
	ToolRegistry    ToolRegistry
	ShadowManager   *shadow.Manager
	Logger          *slog.Logger
}

// NewAgentRegistry creates a new agent registry.
func NewAgentRegistry(cfg RegistryConfig) *AgentRegistry {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	r := &AgentRegistry{
		specs:     make(map[string]*AgentSpec),
		loops:     make(map[string]*AgentLoop),
		memvid:    cfg.MemvidClient,
		taskStore: cfg.TaskStore,
		llm:       cfg.LLMClient,
		bus:       cfg.MessageBus,
		security:  cfg.SecurityChecker,
		tools:     cfg.ToolRegistry,
		shadowMgr: cfg.ShadowManager,
		logger:    cfg.Logger,
	}

	// Register default specs
	for _, spec := range DefaultSpecs() {
		r.RegisterSpec(spec)
	}

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
	loop, err := r.createLoop(spec)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent loop: %w", err)
	}

	r.loops[id] = loop
	r.logger.Info("Created agent loop", "id", id, "name", spec.Name)
	return loop, nil
}

// GetDispatcher returns the dispatcher agent loop.
func (r *AgentRegistry) GetDispatcher() (*AgentLoop, error) {
	return r.Get("dispatcher")
}

// createLoop creates a new agent loop from a spec.
func (r *AgentRegistry) createLoop(spec *AgentSpec) (*AgentLoop, error) {
	config := AgentConfig{
		MaxIterations:         spec.Constraints.MaxIterations,
		Timeout:               spec.Constraints.Timeout,
		Purpose:               spec.Purpose,
		MaxConversationTokens: spec.Constraints.MaxConversationTokens,
	}

	opts := []LoopOption{
		WithAgentConfig(config),
		WithAgentID(spec.ID),
		WithLoopLogger(r.logger.With("agent", spec.ID)),
	}

	if r.llm != nil {
		opts = append(opts, WithLLMClient(r.llm))
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

	return NewAgentLoop(opts...), nil
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
