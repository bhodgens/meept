# Plan: Parallel Tool Execution Enhancement

**Status:** Proposed
**Created:** 2026-07-10
**Priority:** Medium
**Risk:** Medium (concurrency introduces complexity in error handling and taint tracking)

---

## Summary

Enhance Meept's tool execution system to support intelligent parallel execution while maintaining security guarantees. The system already has basic parallelism in `Executor.ExecuteAll()`, but it can be improved with:

1. **Dependency-aware scheduling** - Independent tools run in parallel, dependent tools run sequentially
2. **Adaptive parallelism limits** - Adjust concurrency based on tool type and system load
3. **Better error aggregation** - Collect and report errors from parallel executions without losing context
4. **Taint propagation in parallel** - Ensure security taint labels are correctly tracked across concurrent executions

---

## Current State Analysis

### Existing Implementation

**Location:** `internal/agent/executor.go:652-695`

```go
func (e *Executor) ExecuteAll(ctx context.Context, toolCalls []llm.ToolCall) []*ExecutionResult {
    // ... single call handling ...

    results := make([]*ExecutionResult, len(toolCalls))
    var wg sync.WaitGroup
    sem := make(chan struct{}, e.parallelism)  // Semaphore for limiting concurrency

    for i, tc := range toolCalls {
        wg.Add(1)
        go func(idx int, toolCall llm.ToolCall) {
            defer wg.Done()
            select {
            case sem <- struct{}{}:
                defer func() { <-sem }()
            case <-ctx.Done():
                results[idx] = &ExecutionResult{...}
                return
            }
            results[idx] = e.Execute(ctx, toolCall)
        }(i, tc)
    }
    wg.Wait()
    return results
}
```

### Strengths
- ✅ Basic parallel execution with semaphore-based concurrency limiting
- ✅ Context cancellation handling per-goroutine
- ✅ Results returned in original order (index-preserving)

### Gaps
- ❌ No dependency detection - tools that depend on each other may run out of order
- ❌ Fixed parallelism limit - not adaptive based on tool type or system state
- ❌ No error aggregation strategy - errors from parallel calls lose contextual relationship
- ❌ Taint propagation not considered for cross-tool contamination in parallel execution

---

## Objectives

| Objective | Success Metric |
|-----------|----------------|
| **O1: Dependency-Aware Scheduling** | Tool calls with data dependencies execute in correct order; independent calls run in parallel |
| **O2: Adaptive Parallelism** | Parallelism limit adjusts based on tool type (I/O-bound vs. CPU-bound) and system load |
| **O3: Error Aggregation** | Errors include cross-reference to related tool calls and cascading failure detection |
| **O4: Taint Propagation** | Security taint labels correctly propagate when parallel tool outputs are combined |

---

## Implementation Phases

### Phase 1: Dependency Detection Infrastructure

**Goal:** Build the foundation for detecting data dependencies between tool calls.

#### 1.1: Define Tool Dependency Graph

**File:** `internal/agent/tool_dependency.go` (new)

```go
// ToolDependencyGraph represents dependencies between tool calls.
type ToolDependencyGraph struct {
    // nodes: toolCallID -> tool call definition
    nodes map[string]llm.ToolCall
    // edges: toolCallID -> list of toolCallIDs that this one depends on
    edges map[string][]string
    // outputSchemas: toolCallID -> JSON schema of expected output
    outputSchemas map[string]*jsonschema.Schema
}

// AddCall adds a tool call to the graph.
func (g *ToolDependencyGraph) AddCall(call llm.ToolCall)

// AddDependency records that 'from' depends on 'to' (to must execute first).
func (g *ToolDependencyGraph) AddDependency(from, to string)

// IndependentGroups returns groups of tool calls that can execute in parallel.
// Each group contains calls that have no dependencies on each other.
func (g *ToolDependencyGraph) IndependentGroups() [][]llm.ToolCall
```

#### 1.2: Implement Dependency Inference

**File:** `internal/agent/tool_dependency.go` (continued)

```go
// DependencyInferrer analyzes tool calls to infer dependencies.
type DependencyInferrer struct {
    toolRegistry ToolRegistry
    logger       *slog.Logger
}

// InferDependencies analyzes a list of tool calls and returns a dependency graph.
// Currently uses heuristic analysis:
// - File writes that follow file reads on the same path are dependent
// - Shell commands referencing outputs of previous commands are dependent
// - Tool calls with argument references to $TOOL_CALL_ID.output are dependent
func (r *DependencyInferrer) InferDependencies(calls []llm.ToolCall) *ToolDependencyGraph
```

**Heuristic Rules:**
1. **File path overlap**: `file_read` on `/path/x` → `file_write` on `/path/x` = dependent
2. **Shell command substitution**: `$(previous_command_output)` = dependent
3. **Explicit argument references**: `{"ref": "$call_123.result"}` = dependent
4. **Same resource writes**: Two `file_write` to same path = ordered (last wins, but must be sequential)

#### 1.3: Write Unit Tests

**File:** `internal/agent/tool_dependency_test.go`

```go
func TestDependencyInferrer_FilePathOverlap(t *testing.T) {
    // Test that file_read → file_write on same path is detected
}

func TestDependencyInferrer_IndependentCalls(t *testing.T) {
    // Test that file_read on /a and file_read on /b are independent
}

func TestDependencyGraph_IndependentGroups(t *testing.T) {
    // Test topological sort produces correct parallel groups
}
```

**Verification:**
- [ ] All unit tests pass
- [ ] `go test ./internal/agent -run TestDependencyInferrer -v` shows 100% coverage on inference logic

---

### Phase 2: Dependency-Aware Executor

**Goal:** Modify the executor to use dependency graphs for scheduling.

#### 2.1: Update ExecuteAll Signature

**File:** `internal/agent/executor.go`

```go
// ExecuteAll runs multiple tool calls with dependency-aware scheduling.
// Independent tool calls are executed in parallel; dependent calls are ordered.
func (e *Executor) ExecuteAll(ctx context.Context, toolCalls []llm.ToolCall) []*ExecutionResult {
    if len(toolCalls) == 0 {
        return nil
    }

    // Build dependency graph
    graph := e.inferDependencies(toolCalls)

    // Get independent groups (topologically sorted)
    groups := graph.IndependentGroups()

    var allResults []*ExecutionResult
    for _, group := range groups {
        // Execute each group in parallel
        groupResults := e.executeParallel(ctx, group)
        allResults = append(allResults, groupResults...)

        // Check for early termination on critical errors
        if e.shouldTerminateEarly(groupResults) {
            break
        }
    }

    return allResults
}

// executeParallel executes a group of independent tool calls in parallel.
func (e *Executor) executeParallel(ctx context.Context, toolCalls []llm.ToolCall) []*ExecutionResult {
    // Existing semaphore-based parallel execution
    // ...
}
```

#### 2.2: Add Early Termination Logic

```go
// shouldTerminateEarly returns true if execution should stop due to critical errors.
func (e *Executor) shouldTerminateEarly(results []*ExecutionResult) bool {
    for _, r := range results {
        if !r.Success && e.isCriticalError(r.Error) {
            return true
        }
    }
    return false
}

// isCriticalError returns true for errors that should halt execution.
func (e *Executor) isCriticalError(err string) bool {
    // Permission denied, authentication failures, etc.
    return strings.Contains(err, "permission denied") ||
           strings.Contains(err, "authentication failed")
}
```

#### 2.3: Integrate with Reasoning Cycle

**File:** `internal/agent/loop.go`

Update the tool execution call in `reasoningCycle()` to use the enhanced `ExecuteAll`:

```go
// OLD (line ~2863):
results := l.executeToolCalls(ctx, response.ToolCalls)

// NEW:
results := l.executor.ExecuteAll(ctx, response.ToolCalls)
```

**Verification:**
- [ ] `go build ./...` succeeds
- [ ] Existing tool execution tests pass
- [ ] Integration test with dependent tool calls shows correct ordering

---

### Phase 3: Adaptive Parallelism

**Goal:** Adjust parallelism limits based on tool type and system state.

#### 3.1: Define Tool Concurrency Profiles

**File:** `internal/agent/executor.go`

```go
// ToolConcurrencyProfile defines how a tool should be scheduled for parallel execution.
type ToolConcurrencyProfile int

const (
    // ProfileIOBound: Network, file I/O - can run many in parallel
    ProfileIOBound ToolConcurrencyProfile = iota
    // ProfileCPUBound: AST parsing, code analysis - limit parallelism
    ProfileCPUBound
    // ProfileStateful: Shell sessions, database connections - run sequentially
    ProfileStateful
    // ProfileExclusive: Operations requiring exclusive access (e.g., git operations)
    ProfileExclusive
)

// toolProfiles maps tool names to their concurrency profiles.
var toolProfiles = map[string]ToolConcurrencyProfile{
    "web_search":  ProfileIOBound,
    "web_fetch":   ProfileIOBound,
    "file_read":   ProfileIOBound,
    "file_write":  ProfileIOBound,
    "ast_parse":   ProfileCPUBound,
    "ast_query":   ProfileCPUBound,
    "shell":       ProfileStateful,
    "git_diff":    ProfileExclusive,
    "git_commit":  ProfileExclusive,
}
```

#### 3.2: Implement Adaptive Parallelism Limiter

```go
// AdaptiveParallelismLimiter dynamically adjusts concurrency limits.
type AdaptiveParallelismLimiter struct {
    mu              sync.RWMutex
    baseParallelism int
    currentLoad     int64  // Atomic counter of active operations
    systemLoadAvg   float64 // Recent system load average

    // Per-profile limits
    ioBoundLimit    int
    cpuBoundLimit   int
    statefulLimit   int
    exclusiveLimit  int
}

// Acquire acquires a slot for the given profile, blocking if necessary.
func (l *AdaptiveParallelismLimiter) Acquire(ctx context.Context, profile ToolConcurrencyProfile) error

// Release releases a slot for the given profile.
func (l *AdaptiveParallelismLimiter) Release(profile ToolConcurrencyProfile)

// AdjustLimits updates limits based on observed performance.
func (l *AdaptiveParallelismLimiter) AdjustLimits(metrics ExecutionMetrics)
```

#### 3.3: Wire Into Executor

**File:** `internal/agent/executor.go`

```go
type Executor struct {
    // ... existing fields ...
    parallelismLimiter *AdaptiveParallelismLimiter
}

func NewExecutor(...) *Executor {
    // ...
    return &Executor{
        // ...
        parallelismLimiter: NewAdaptiveParallelismLimiter(config.Parallelism),
    }
}
```

**Verification:**
- [ ] Parallel execution respects profile-based limits
- [ ] System load monitoring adjusts limits dynamically
- [ ] No deadlocks or starvation with adaptive limiter

---

### Phase 4: Error Aggregation and Reporting

**Goal:** Improve error reporting for parallel tool executions.

#### 4.1: Define Aggregated Error Type

**File:** `internal/agent/executor.go`

```go
// ParallelExecutionError aggregates errors from multiple tool calls.
type ParallelExecutionError struct {
    ToolCallID     string            `json:"tool_call_id"`
    Error          string            `json:"error"`
    IsCascading    bool              `json:"is_cascading"`     // True if caused by earlier failure
    CascadeSource  string            `json:"cascade_source"`   // Tool call ID that caused this failure
    RelatedErrors  []string          `json:"related_errors"`   // IDs of related tool call errors
    Severity       ErrorSeverity     `json:"severity"`         // critical, warning, info
}

type ErrorSeverity string

const (
    ErrorSeverityCritical ErrorSeverity = "critical"
    ErrorSeverityWarning  ErrorSeverity = "warning"
    ErrorSeverityInfo     ErrorSeverity = "info"
)

func (e *ParallelExecutionError) Error() string {
    if e.IsCascading {
        return fmt.Sprintf("tool %s failed (cascading from %s): %s", e.ToolCallID, e.CascadeSource, e.Error)
    }
    return fmt.Sprintf("tool %s failed: %s", e.ToolCallID, e.Error)
}
```

#### 4.2: Implement Cascade Detection

```go
// detectCascadingErrors identifies errors that are likely caused by earlier failures.
func detectCascadingErrors(results []*ExecutionResult, graph *ToolDependencyGraph) []*ExecutionResult {
    failedCalls := make(map[string]bool)

    // First pass: identify direct failures
    for _, r := range results {
        if !r.Success {
            failedCalls[r.ToolCallID] = true
        }
    }

    // Second pass: mark cascading failures
    for _, r := range results {
        if !r.Success {
            // Check if any dependency failed
            deps := graph.GetDependencies(r.ToolCallID)
            for _, dep := range deps {
                if failedCalls[dep] {
                    r.CascadeFrom = dep
                    r.IsCascading = true
                    break
                }
            }
        }
    }

    return results
}
```

**Verification:**
- [ ] Error reports include cascade information
- [ ] Related errors are linked in output

---

### Phase 5: Taint Propagation in Parallel Execution

**Goal:** Ensure security taint labels are correctly tracked when parallel tool outputs are combined.

#### 5.1: Extend Taint Tracking for Parallel Context

**File:** `internal/security/taint/taint.go`

```go
// ParallelTaintTracker tracks taint propagation across parallel tool executions.
type ParallelTaintTracker struct {
    mu          sync.Mutex
    taintStates map[string]TaintLabel  // toolCallID -> taint label
    mergeRules  []MergeRule
}

// MergeRule defines how to combine taints from multiple sources.
type MergeRule struct {
    // Input taint patterns
    InputPatterns []TaintPattern
    // Output taint when inputs match
    OutputTaint TaintLabel
    // Description for auditing
    Description string
}

// RecordTaint records the taint label for a tool call's output.
func (t *ParallelTaintTracker) RecordTaint(toolCallID string, label TaintLabel)

// GetCombinedTaint returns the combined taint for a set of tool call outputs.
func (t *ParallelTaintTracker) GetCombinedTaint(toolCallIDs []string) TaintLabel

// IsTainted returns true if any of the tool call outputs are tainted.
func (t *ParallelTaintTracker) IsTainted(toolCallIDs []string, taintType TaintType) bool
```

#### 5.2: Integrate with Executor

**File:** `internal/agent/executor.go`

Add taint tracking to `ExecuteAll`:

```go
func (e *Executor) ExecuteAll(ctx context.Context, toolCalls []llm.ToolCall) []*ExecutionResult {
    // ... existing execution logic ...

    // Track taint propagation
    taintTracker := NewParallelTaintTracker()
    for i, result := range results {
        if result.Success && result.TaintLabel != "" {
            taintTracker.RecordTaint(result.ToolCallID, result.TaintLabel)
        }
    }

    // Check for taint violations when combining results
    combinedTaint := taintTracker.GetCombinedTaint(getSuccessfulCallIDs(results))
    if combinedTaint.RequiresPolicyCheck() {
        // Log taint combination for audit
        e.logger.Warn("Parallel execution produced combined taint",
            "taint", combinedTaint,
            "tool_calls", getSuccessfulCallIDs(results))
    }

    return results
}
```

**Verification:**
- [ ] Taint labels are correctly propagated in parallel execution
- [ ] Audit logs show taint combinations
- [ ] Taint policy violations are detected and logged

---

## Testing Strategy

### Unit Tests

| Test Case | File | Description |
|-----------|------|-------------|
| `TestDependencyInferrer_FilePathOverlap` | `tool_dependency_test.go` | Detect read→write dependencies |
| `TestDependencyInferrer_Independent` | `tool_dependency_test.go` | Independent calls detected |
| `TestExecuteAll_ParallelIndependent` | `executor_test.go` | Verify parallel execution of independent calls |
| `TestExecuteAll_SequentialDependent` | `executor_test.go` | Verify ordered execution of dependent calls |
| `TestAdaptiveParallelism_ProfileRespect` | `executor_test.go` | Verify profiles adjust parallelism |
| `TestCascadeDetection` | `executor_test.go` | Verify cascading errors are detected |

### Integration Tests

| Test Case | Description |
|-----------|-------------|
| `TestReasoningCycle_ParallelTools` | Full reasoning cycle with parallel tool calls |
| `TestTaintPropagation_Parallel` | End-to-end taint tracking in parallel execution |
| `TestErrorAggregation_MultipleFailures` | Multiple tool failures produce aggregated errors |

### Manual Testing

1. **Parallel file operations**: Verify independent file reads run in parallel
2. **Dependent shell commands**: Verify `ls` → `cat $(ls)` executes in order
3. **Mixed dependency graph**: Complex scenarios with some parallel, some sequential

---

## Rollback Plan

If issues arise:

1. **Immediate**: Set `agent.parallel_tool_execution` config to `false` to revert to sequential execution
2. **Code**: Revert to using `ExecuteSequential` as default in `reasoningCycle()`
3. **Gradual rollout**: Enable per-tool-type via config before full rollout

---

## Configuration Changes

**File:** `config/agent.json5`

```json5
{
  "agent": {
    "parallel_tool_execution": {
      "enabled": true,
      "base_parallelism": 5,
      "adaptive": true,
      "profiles": {
        "io_bound": { "parallelism": 10 },
        "cpu_bound": { "parallelism": 2 },
        "stateful": { "parallelism": 1 },
        "exclusive": { "parallelism": 1 }
      }
    }
  }
}
```

---

## Metrics to Track

| Metric | Description | Alert Threshold |
|--------|-------------|-----------------|
| `agent.tool.parallelism_utilization` | % of available parallelism slots used | < 50% (underutilized) |
| `agent.tool.dependency_ratio` | % of tool calls that are dependent | > 80% (bottleneck) |
| `agent.tool.cascade_error_rate` | % of errors that are cascading | > 20% |
| `tool.execution_time.p99` | 99th percentile tool execution time | > 30s |

---

## Success Criteria

- [ ] **Phase 1**: Dependency graph correctly identifies independent vs. dependent tool calls
- [ ] **Phase 2**: `ExecuteAll` respects dependency ordering
- [ ] **Phase 3**: Adaptive parallelism adjusts based on tool profiles
- [ ] **Phase 4**: Error reports include cascade information
- [ ] **Phase 5**: Taint propagation works correctly in parallel context
- [ ] **Overall**: No regression in tool execution correctness; >30% improvement in wall-clock time for independent tool batches
