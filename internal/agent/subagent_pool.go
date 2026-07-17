package agent

import (
	"context"
	"fmt"
	"sync"
)

// -----------------------------------------------------------------------
// SubagentExecution tracks metadata about a spawned subagent.
// -----------------------------------------------------------------------

// SubagentExecution records execution metadata for a subagent invocation.
type SubagentExecution struct {
	// AgentID is the unique ID of this subagent.
	AgentID string `json:"agent_id"`
	// ParentID is the agent ID that spawned this subagent, or empty for root.
	ParentID string `json:"parent_id,omitempty"`
	// Depth is the spawn depth (0 = root).
	Depth int `json:"depth"`
	// TurnsUsed is the number of tool+LLM turns consumed.
	TurnsUsed int `json:"turns_used"`
	// ToolCallsMade is the total number of tool calls issued.
	ToolCallsMade int `json:"tool_calls_made"`
}

// -----------------------------------------------------------------------
// Agent context and invocation
// -----------------------------------------------------------------------

// guardedInvoke enforces structural depth limits, acquires the per-depth
// semaphore, and creates a fresh context for a subagent invocation. It
// returns a cancel function that must be called when done.
//
// Modeled after HALO's guarded_invoke (subagent_tool_factory.py).
func (a *RLMAnalyzer) guardedInvoke(ctx context.Context, depth int, prompt string, parentID string) (*analyzerRun, error) {
	// 1. Structural depth gate.
	if depth > a.config.MaximumDepth {
		return nil, fmt.Errorf("exceeded maximum depth %d (current: %d)",
			a.config.MaximumDepth, depth)
	}
	if depth >= a.config.MaximumDepth && depth > 0 {
		// At max depth (for non-root), we must use _final, not spawn.
		// The caller should check canSpawnSubagentAt(depth) instead.
	}

	// 2. Semaphore acquire (blocks until a slot is available).
	if err := a.semaphore.Acquire(depth); err != nil {
		return nil, fmt.Errorf("semaphore acquire at depth %d: %w", depth, err)
	}

	// 3. Create a cancellable context.
	childCtx, cancel := context.WithCancel(ctx)

	// 4. Turn counter starts at 0.
	tc := newTurnCounter(a.config.MaximumTurns)

	run := &analyzerRun{
		ctx:       childCtx,
		cancel:    cancel,
		depth:     depth,
		parentID:  parentID,
		semaphore:   a.semaphore,
		turnCounter: tc,
		toolRegistry:  newToolRegistry(),
		logger:      a.logger,
		done:        make(chan struct{}),
	}

	return run, nil
}

// -----------------------------------------------------------------------
// analyzerRun holds per-agent execution state.
// -----------------------------------------------------------------------

type analyzerRun struct {
	ctx      context.Context
	cancel   context.CancelFunc
	depth    int
	parentID string

	semaphore   *PerDepthSemaphore // for Release on completion
	turnCounter *turnCounter
	toolRegistry  *toolRegistry
	logger        interface{ Info(string, ...any); Warn(string, ...any); Error(string, ...any) }
	agentID       string

	done chan struct{}

	// Accumulated output from this agent run.
	//lint:ignore U1000 // reserved for future subagent output tracking
	mu        sync.Mutex
	//lint:ignore U1000 // reserved for future subagent output tracking
	output    string
	//lint:ignore U1000 // reserved for future subagent output tracking
	turnCount int
	//lint:ignore U1000 // reserved for future subagent output tracking
	toolCalls int
	//lint:ignore U1000 // reserved for future subagent output tracking
	finished  bool
}

// releaseSemaphore returns the depth-N slot to the per-depth semaphore pool.
// Safe to call multiple times (double-release is a no-op on the channel).
func (r *analyzerRun) releaseSemaphore() {
	if r.semaphore != nil {
		r.semaphore.Release(r.depth)
	}
}

//lint:ignore U1000 // reserved for future subagent output tracking
func (r *analyzerRun) recordTurn() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turnCount++
}

//lint:ignore U1000 // reserved for future subagent output tracking
func (r *analyzerRun) recordToolCall() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.toolCalls++
}

//lint:ignore U1000 // reserved for future subagent output tracking
func (r *analyzerRun) appendOutput(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.output += s
}

//lint:ignore U1000 // reserved for future subagent output tracking
func (r *analyzerRun) finishedResult() SubagentExecution {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finished = true
	return SubagentExecution{
		AgentID:       "", // filled by caller
		ParentID:      r.parentID,
		Depth:         r.depth,
		TurnsUsed:     r.turnCount,
		ToolCallsMade: r.toolCalls,
	}
}

// -----------------------------------------------------------------------
// turnCounter provides self-pacing nudges similar to HALO's turn counter.
// -----------------------------------------------------------------------

type turnCounter struct {
	current   int
	limit     int
	mu        sync.Mutex
}

func newTurnCounter(limit int) *turnCounter {
	return &turnCounter{limit: limit}
}

func (tc *turnCounter) Increment() (int, bool) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.current++
	return tc.current, tc.current >= tc.limit
}

func (tc *turnCounter) Remaining() int {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	remaining := tc.limit - tc.current
	if remaining < 0 {
		remaining = 0
	}
	return remaining
}

// Nudge returns structured self-pacing information for the RLM analyzer.
// Implementation is in turn_counter.go (same receiver type).

// -----------------------------------------------------------------------
// toolRegistry is a minimal tool set for the analyzer.
// -----------------------------------------------------------------------

type toolRegistry struct {
	mu     sync.Mutex
	tools  []string
}

func newToolRegistry() *toolRegistry {
	return &toolRegistry{tools: make([]string, 0)}
}

func (tr *toolRegistry) Register(name string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	for _, existing := range tr.tools {
		if existing == name {
			return // already registered
		}
	}
	tr.tools = append(tr.tools, name)
}

func (tr *toolRegistry) List() []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	result := make([]string, len(tr.tools))
	copy(result, tr.tools)
	return result
}
