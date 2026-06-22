package agent

import (
	"context"
	"log/slog"
	"sort"
	"sync"

	"github.com/caimlas/meept/internal/llm"
)

// HookPriority defines ordering for hook execution.
// Lower values run first.
type HookPriority int

const (
	HookPriorityCritical HookPriority = 0   // security, must run first
	HookPriorityHigh     HookPriority = 10  // audit, cost gating
	HookPriorityNormal   HookPriority = 50  // default
	HookPriorityLow      HookPriority = 90  // logging, metrics
	HookPriorityMonitor  HookPriority = 100 // monitoring, shadow training
)

// BlockResult is returned by BeforeToolCallHook.
type BlockResult struct {
	Block  bool
	Reason string // human-readable reason if blocked
}

// OverrideResult is returned by AfterToolCallHook.
type OverrideResult struct {
	Override bool
	Result   *ExecutionResult // replacement result (if Override=true)
	Reason   string
}

// TurnState provides read-only access to the current turn state.
type TurnState struct {
	ConversationID string
	Iteration      int
	Messages       []llm.ChatMessage
	ModelRef       string
	TotalTokens    int
	LastResponse   string
}

// TurnModification requests changes to the next turn.
type TurnModification struct {
	Modified      bool
	ExtraMessages []llm.ChatMessage // prepended before next LLM call
	ModelOverride string            // if non-empty, switch model for next call
	SkipTools     bool              // if true, don't send tool definitions next call
	Reason        string
}

// StopDecision is returned by ShouldStopAfterTurnHook.
type StopDecision struct {
	Stop   bool
	Reason string // used in the "wrap up" message
}

// ContextTransform is returned by TransformContextHook.
type ContextTransform struct {
	Messages []llm.ChatMessage    // replacement messages
	ToolDefs []llm.ToolDefinition // replacement tool definitions (nil = keep original)
	Modified bool
	Reason   string
}

// BeforeToolCallHook is called before a tool is executed.
// Return BlockResult with Block=true to prevent execution.
type BeforeToolCallHook interface {
	BeforeToolCall(ctx context.Context, toolCall llm.ToolCall) BlockResult
}

// AfterToolCallHook is called after a tool executes.
// Return OverrideResult to replace the tool's output.
type AfterToolCallHook interface {
	AfterToolCall(ctx context.Context, toolCall llm.ToolCall, result *ExecutionResult) OverrideResult
}

// PrepareNextTurnHook is called between turns.
// It can swap the context (messages), model, or inference parameters.
type PrepareNextTurnHook interface {
	PrepareNextTurn(ctx context.Context, state TurnState) TurnModification
}

// ShouldStopAfterTurnHook is called after each turn.
// Return true to force the loop to exit.
type ShouldStopAfterTurnHook interface {
	ShouldStopAfterTurn(ctx context.Context, state TurnState) StopDecision
}

// TransformContextHook is called before each LLM call.
// It can modify the messages sent to the LLM.
type TransformContextHook interface {
	TransformContext(ctx context.Context, messages []llm.ChatMessage, toolDefs []llm.ToolDefinition) ContextTransform
}

// HookRegistration holds a registered hook with its priority.
type HookRegistration struct {
	Name     string
	Priority HookPriority
	Hook     any // concrete hook interface
}

// HookRegistry manages all agent hooks.
// Hooks are executed in priority order (lowest first) with short-circuit
// semantics for blocking hooks.
type HookRegistry struct {
	mu                sync.RWMutex
	beforeToolCalls   []HookRegistration
	afterToolCalls    []HookRegistration
	prepareNextTurns  []HookRegistration
	shouldStopAfter   []HookRegistration
	transformContexts []HookRegistration
	logger            *slog.Logger
}

// NewHookRegistry creates a new HookRegistry.
func NewHookRegistry(logger *slog.Logger) *HookRegistry {
	if logger == nil {
		logger = slog.Default()
	}
	return &HookRegistry{
		logger: logger.With("component", "hook-registry"),
	}
}

// RegisterBeforeToolCall registers a BeforeToolCallHook.
func (r *HookRegistry) RegisterBeforeToolCall(name string, priority HookPriority, hook BeforeToolCallHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.beforeToolCalls = append(r.beforeToolCalls, HookRegistration{
		Name: name, Priority: priority, Hook: hook,
	})
	sort.Slice(r.beforeToolCalls, func(i, j int) bool {
		return r.beforeToolCalls[i].Priority < r.beforeToolCalls[j].Priority
	})
}

// RegisterAfterToolCall registers an AfterToolCallHook.
func (r *HookRegistry) RegisterAfterToolCall(name string, priority HookPriority, hook AfterToolCallHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.afterToolCalls = append(r.afterToolCalls, HookRegistration{
		Name: name, Priority: priority, Hook: hook,
	})
	sort.Slice(r.afterToolCalls, func(i, j int) bool {
		return r.afterToolCalls[i].Priority < r.afterToolCalls[j].Priority
	})
}

// RegisterPrepareNextTurn registers a PrepareNextTurnHook.
func (r *HookRegistry) RegisterPrepareNextTurn(name string, priority HookPriority, hook PrepareNextTurnHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prepareNextTurns = append(r.prepareNextTurns, HookRegistration{
		Name: name, Priority: priority, Hook: hook,
	})
	sort.Slice(r.prepareNextTurns, func(i, j int) bool {
		return r.prepareNextTurns[i].Priority < r.prepareNextTurns[j].Priority
	})
}

// RegisterShouldStopAfterTurn registers a ShouldStopAfterTurnHook.
func (r *HookRegistry) RegisterShouldStopAfterTurn(name string, priority HookPriority, hook ShouldStopAfterTurnHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.shouldStopAfter = append(r.shouldStopAfter, HookRegistration{
		Name: name, Priority: priority, Hook: hook,
	})
	sort.Slice(r.shouldStopAfter, func(i, j int) bool {
		return r.shouldStopAfter[i].Priority < r.shouldStopAfter[j].Priority
	})
}

// RegisterTransformContext registers a TransformContextHook.
func (r *HookRegistry) RegisterTransformContext(name string, priority HookPriority, hook TransformContextHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.transformContexts = append(r.transformContexts, HookRegistration{
		Name: name, Priority: priority, Hook: hook,
	})
	sort.Slice(r.transformContexts, func(i, j int) bool {
		return r.transformContexts[i].Priority < r.transformContexts[j].Priority
	})
}

// Unregister removes all hooks with the given name.
func (r *HookRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.beforeToolCalls = filterHooks(r.beforeToolCalls, name)
	r.afterToolCalls = filterHooks(r.afterToolCalls, name)
	r.prepareNextTurns = filterHooks(r.prepareNextTurns, name)
	r.shouldStopAfter = filterHooks(r.shouldStopAfter, name)
	r.transformContexts = filterHooks(r.transformContexts, name)
}

// RunBeforeToolCalls runs all BeforeToolCall hooks in priority order.
// Returns the first BlockResult with Block=true (short-circuit).
func (r *HookRegistry) RunBeforeToolCalls(ctx context.Context, toolCall llm.ToolCall) BlockResult {
	r.mu.RLock()
	hooks := make([]HookRegistration, len(r.beforeToolCalls))
	copy(hooks, r.beforeToolCalls)
	r.mu.RUnlock()

	for _, reg := range hooks {
		hook, ok := reg.Hook.(BeforeToolCallHook)
		if !ok {
			r.logger.Warn("hook does not implement BeforeToolCallHook",
				"name", reg.Name,
			)
			continue
		}
		result := hook.BeforeToolCall(ctx, toolCall)
		if result.Block {
			r.logger.Info("tool call blocked by hook",
				"hook", reg.Name,
				"tool", toolCall.Function.Name,
				"reason", result.Reason,
			)
			return result
		}
	}
	return BlockResult{}
}

// RunAfterToolCalls runs all AfterToolCall hooks in priority order.
// Returns the first OverrideResult with Override=true (short-circuit).
func (r *HookRegistry) RunAfterToolCalls(ctx context.Context, toolCall llm.ToolCall, result *ExecutionResult) OverrideResult {
	r.mu.RLock()
	hooks := make([]HookRegistration, len(r.afterToolCalls))
	copy(hooks, r.afterToolCalls)
	r.mu.RUnlock()

	for _, reg := range hooks {
		hook, ok := reg.Hook.(AfterToolCallHook)
		if !ok {
			r.logger.Warn("hook does not implement AfterToolCallHook",
				"name", reg.Name,
			)
			continue
		}
		ovr := hook.AfterToolCall(ctx, toolCall, result)
		if ovr.Override {
			r.logger.Info("tool result overridden by hook",
				"hook", reg.Name,
				"tool", toolCall.Function.Name,
				"reason", ovr.Reason,
			)
			return ovr
		}
	}
	return OverrideResult{}
}

// RunPrepareNextTurn runs all PrepareNextTurn hooks in priority order.
// Returns the first TurnModification with Modified=true (short-circuit).
func (r *HookRegistry) RunPrepareNextTurn(ctx context.Context, state TurnState) TurnModification {
	r.mu.RLock()
	hooks := make([]HookRegistration, len(r.prepareNextTurns))
	copy(hooks, r.prepareNextTurns)
	r.mu.RUnlock()

	for _, reg := range hooks {
		hook, ok := reg.Hook.(PrepareNextTurnHook)
		if !ok {
			r.logger.Warn("hook does not implement PrepareNextTurnHook",
				"name", reg.Name,
			)
			continue
		}
		mod := hook.PrepareNextTurn(ctx, state)
		if mod.Modified {
			return mod
		}
	}
	return TurnModification{}
}

// RunShouldStopAfterTurn runs all ShouldStopAfterTurn hooks in priority order.
// Returns the first StopDecision with Stop=true (short-circuit).
func (r *HookRegistry) RunShouldStopAfterTurn(ctx context.Context, state TurnState) StopDecision {
	r.mu.RLock()
	hooks := make([]HookRegistration, len(r.shouldStopAfter))
	copy(hooks, r.shouldStopAfter)
	r.mu.RUnlock()

	for _, reg := range hooks {
		hook, ok := reg.Hook.(ShouldStopAfterTurnHook)
		if !ok {
			r.logger.Warn("hook does not implement ShouldStopAfterTurnHook",
				"name", reg.Name,
			)
			continue
		}
		decision := hook.ShouldStopAfterTurn(ctx, state)
		if decision.Stop {
			r.logger.Info("loop stop requested by hook",
				"hook", reg.Name,
				"reason", decision.Reason,
			)
			return decision
		}
	}
	return StopDecision{}
}

// RunTransformContext runs all TransformContext hooks in priority order.
// Returns the first ContextTransform with Modified=true (short-circuit).
func (r *HookRegistry) RunTransformContext(ctx context.Context, messages []llm.ChatMessage, toolDefs []llm.ToolDefinition) ContextTransform {
	r.mu.RLock()
	hooks := make([]HookRegistration, len(r.transformContexts))
	copy(hooks, r.transformContexts)
	r.mu.RUnlock()

	for _, reg := range hooks {
		hook, ok := reg.Hook.(TransformContextHook)
		if !ok {
			r.logger.Warn("hook does not implement TransformContextHook",
				"name", reg.Name,
			)
			continue
		}
		transform := hook.TransformContext(ctx, messages, toolDefs)
		if transform.Modified {
			return transform
		}
	}
	return ContextTransform{}
}

// filterHooks returns a new slice excluding hooks with the given name.
func filterHooks(hooks []HookRegistration, name string) []HookRegistration {
	filtered := make([]HookRegistration, 0, len(hooks))
	for _, h := range hooks {
		if h.Name != name {
			filtered = append(filtered, h)
		}
	}
	return filtered
}
