# Plan: Hierarchical Budget Management

**Status:** Proposed
**Created:** 2026-07-10
**Priority:** Medium
**Risk:** Low (additive hierarchy, existing budget tracker continues to work)

---

## Summary

Enhance Meept's token budget management from a flat per-conversation model to a hierarchical multi-level budget system. This enables:

1. **Task-level budgets** - Overall budget for a complete task
2. **Phase-level budgets** - Budget allocation per plan phase
3. **Turn-level budgets** - Per-iteration token limits
4. **Emergency reserves** - Tokens reserved for cleanup/wrap-up

Current implementation (`TurnBudgetTracker` in `conversation.go`) tracks total budget and per-turn allocation, but lacks hierarchical structure for complex multi-phase tasks.

---

## Current State Analysis

### Existing Budget Tracker

**Location:** `internal/agent/conversation.go:32-120`

```go
type TurnBudgetTracker struct {
    mu              sync.Mutex
    totalBudget     int  // Total tokens allocated for the session
    usedBudget      int  // Tokens used so far
    tokensPerTurn   int  // Expected tokens per turn (for estimation)
    maxTurns        int  // Maximum turns before wrap-up
    currentTurn     int  // Current turn number
    warningZone     bool // Set when budget is nearly depleted
    wrapUpRequested bool // Set when wrap-up is requested
}

func (t *TurnBudgetTracker) RecordUsage(tokensUsed int) {
    t.usedBudget += tokensUsed
    t.currentTurn++
    // Check warning zone (80% depleted)
    // Check max turns reached
}
```

### Strengths
- ✅ Simple and easy to understand
- ✅ Tracks total budget and per-turn usage
- ✅ Warning zone alerts before depletion
- ✅ Max turns enforcement

### Gaps
- ❌ No task/phase isolation - one task can exhaust budget for all
- ❌ No budget carryover - unused budget from phases is lost
- ❌ No emergency reserve - no tokens reserved for cleanup operations
- ❌ No budget borrowing - can't reallocate from completed phases
- ❌ No visibility into budget distribution across task hierarchy

---

## Objectives

| Objective | Success Metric |
|-----------|----------------|
| **O1: Hierarchical Budget Structure** | Task → Phase → Turn budget levels implemented |
| **O2: Budget Allocation** | Parent budgets can allocate to children |
| **O3: Budget Borrowing** | Unused budget can be reallocated |
| **O4: Emergency Reserve** | Reserved tokens for cleanup operations |
| **O5: Observability** | Budget status visible at all levels |

---

## Implementation Phases

### Phase 1: Define Hierarchical Budget Types

**Goal:** Create the core budget hierarchy types.

#### 1.1: Define Budget Level Enum

**File:** `internal/agent/budget_hierarchy.go` (new)

```go
package agent

import (
    "fmt"
    "sync"
    "time"
)

// BudgetLevel represents the level in the budget hierarchy.
type BudgetLevel uint8

const (
    // BudgetLevelTask: Top-level budget for entire task
    BudgetLevelTask BudgetLevel = iota
    // BudgetLevelPhase: Budget for a specific plan phase
    BudgetLevelPhase
    // BudgetLevelTurn: Budget for a single iteration/turn
    BudgetLevelTurn
)

// String returns a human-readable name for the budget level.
func (l BudgetLevel) String() string {
    switch l {
    case BudgetLevelTask:
        return "task"
    case BudgetLevelPhase:
        return "phase"
    case BudgetLevelTurn:
        return "turn"
    default:
        return "unknown"
    }
}
```

#### 1.2: Define Budget Allocation

```go
// BudgetAllocation represents a token allocation at a specific level.
type BudgetAllocation struct {
    mu sync.RWMutex

    // Metadata
    id        string
    level     BudgetLevel
    name      string            // Human-readable name (e.g., "Phase 1: Research")
    parentID  string            // Parent allocation ID (empty for task-level)
    createdAt time.Time

    // Budget tracking
    totalBudget    int  // Total tokens allocated
    usedBudget     int  // Tokens used so far
    reservedBudget int  // Tokens reserved (emergency reserve, not to be used normally)

    // Configuration
    warningThreshold   float64 // Ratio at which to warn (default 0.8 = 80%)
    allowBorrowing     bool    // Can borrow from sibling allocations
    allow Carryover    bool    // Can unused budget carry over to next allocation

    // Hierarchical relationships
    children []*BudgetAllocation // Sub-allocations (for task/phase levels)

    // Callbacks
    onLowBudget  func(allocation *BudgetAllocation)
    onExhausted  func(allocation *BudgetAllocation)
}

// NewBudgetAllocation creates a new budget allocation.
func NewBudgetAllocation(id string, level BudgetLevel, totalBudget int) *BudgetAllocation {
    return &BudgetAllocation{
        id:               id,
        level:            level,
        totalBudget:      totalBudget,
        warningThreshold: 0.8,
        allowBorrowing:   false,
        allowCarryover:   false,
        createdAt:        time.Now(),
    }
}

// WithReserved sets the reserved budget (emergency reserve).
func (a *BudgetAllocation) WithReserved(reserved int) *BudgetAllocation {
    a.reservedBudget = reserved
    return a
}

// WithBorrowing enables/disables borrowing from siblings.
func (a *BudgetAllocation) WithBorrowing(enabled bool) *BudgetAllocation {
    a.allowBorrowing = enabled
    return a
}

// WithCarryover enables/disables carryover of unused budget.
func (a *BudgetAllocation) WithCarryover(enabled bool) *BudgetAllocation {
    a.allowCarryover = enabled
    return a
}

// WithWarningThreshold sets the warning threshold ratio.
func (a *BudgetAllocation) WithWarningThreshold(ratio float64) *BudgetAllocation {
    a.warningThreshold = ratio
    return a
}

// Available returns the available budget (total - used - reserved).
func (a *BudgetAllocation) Available() int {
    a.mu.RLock()
    defer a.mu.RUnlock()
    return a.totalBudget - a.usedBudget - a.reservedBudget
}

// AvailableWithReserved returns available budget including reserved (for emergency use).
func (a *BudgetAllocation) AvailableWithReserved() int {
    a.mu.RLock()
    defer a.mu.RUnlock()
    return a.totalBudget - a.usedBudget
}

// Used returns the used budget.
func (a *BudgetAllocation) Used() int {
    a.mu.RLock()
    defer a.mu.RUnlock()
    return a.usedBudget
}

// UsageRatio returns the fraction of budget used (0.0 to 1.0).
func (a *BudgetAllocation) UsageRatio() float64 {
    a.mu.RLock()
    defer a.mu.RUnlock()
    if a.totalBudget == 0 {
        return 0
    }
    return float64(a.usedBudget) / float64(a.totalBudget)
}

// IsExhausted returns true if budget is fully used.
func (a *BudgetAllocation) IsExhausted() bool {
    return a.Available() <= 0
}

// IsWarningZone returns true if budget is in warning zone.
func (a *BudgetAllocation) IsWarningZone() bool {
    return a.UsageRatio() >= a.warningThreshold
}
```

#### 1.3: Budget Operations

```go
// Allocate consumes budget from the allocation.
// Returns true if successful, false if insufficient budget.
func (a *BudgetAllocation) Allocate(tokens int) bool {
    a.mu.Lock()
    defer a.mu.Unlock()

    available := a.totalBudget - a.usedBudget - a.reservedBudget
    if tokens > available {
        return false
    }

    a.usedBudget += tokens
    return true
}

// AllocateFromReserved consumes from reserved budget (emergency use).
// Returns true if successful, false if insufficient reserved budget.
func (a *BudgetAllocation) AllocateFromReserved(tokens int) bool {
    a.mu.Lock()
    defer a.mu.Unlock()

    available := a.reservedBudget - (a.usedBudget - (a.totalBudget - a.reservedBudget))
    if tokens > available {
        return false
    }

    a.usedBudget += tokens
    return true
}

// Release returns budget to the allocation (e.g., on error recovery).
func (a *BudgetAllocation) Release(tokens int) {
    a.mu.Lock()
    defer a.mu.Unlock()

    if tokens > a.usedBudget {
        tokens = a.usedBudget
    }
    a.usedBudget -= tokens
}

// AddChild adds a child allocation (for hierarchical structure).
func (a *BudgetAllocation) AddChild(child *BudgetAllocation) {
    a.mu.Lock()
    defer a.mu.Unlock()
    child.parentID = a.id
    a.children = append(a.children, child)
}

// GetChild returns a child allocation by ID.
func (a *BudgetAllocation) GetChild(id string) *BudgetAllocation {
    a.mu.RLock()
    defer a.mu.RUnlock()
    for _, child := range a.children {
        if child.id == id {
            return child
        }
    }
    return nil
}

// GetChildren returns all child allocations.
func (a *BudgetAllocation) GetChildren() []*BudgetAllocation {
    a.mu.RLock()
    defer a.mu.RUnlock()
    result := make([]*BudgetAllocation, len(a.children))
    copy(result, a.children)
    return result
}
```

#### 1.4: Budget Hierarchy Manager

```go
// BudgetHierarchy manages the budget hierarchy for a task.
type BudgetHierarchy struct {
    mu           sync.RWMutex
    taskBudget   *BudgetAllocation       // Root task budget
    phaseBudgets map[string]*BudgetAllocation // Phase budgets by ID
    turnBudget   *BudgetAllocation       // Current turn budget
    logger       AgentLogger
}

// NewBudgetHierarchy creates a new budget hierarchy.
func NewBudgetHierarchy(taskBudget int, phases []string, phaseBudgets []int) *BudgetHierarchy {
    h := &BudgetHierarchy{
        phaseBudgets: make(map[string]*BudgetAllocation),
    }

    // Create task-level budget
    h.taskBudget = NewBudgetAllocation("task", BudgetLevelTask, taskBudget).
        WithReserved(taskBudget / 10). // 10% emergency reserve
        WithWarningThreshold(0.7).     // Warn at 70%
        WithBorrowing(true)

    // Create phase budgets
    for i, phaseID := range phases {
        budget := phaseBudgets[i]
        if budget <= 0 {
            // Distribute remaining budget evenly
            budget = (taskBudget - taskBudget/10) / len(phases)
        }

        phaseAlloc := NewBudgetAllocation(phaseID, BudgetLevelPhase, budget).
            WithCarryover(true).
            WithWarningThreshold(0.8)

        h.phaseBudgets[phaseID] = phaseAlloc
        h.taskBudget.AddChild(phaseAlloc)
    }

    return h
}

// SelectPhaseBudget sets the active phase budget.
func (h *BudgetHierarchy) SelectPhaseBudget(phaseID string) error {
    h.mu.Lock()
    defer h.mu.Unlock()

    phase, ok := h.phaseBudgets[phaseID]
    if !ok {
        return fmt.Errorf("phase %s not found", phaseID)
    }

    // Create turn budget under phase
    turnBudget := phase.Available() / 10 // 10 turns per phase estimate
    h.turnBudget = NewBudgetAllocation("turn", BudgetLevelTurn, turnBudget)
    phase.AddChild(h.turnBudget)

    return nil
}

// GetTurnBudget returns the current turn budget.
func (h *BudgetHierarchy) GetTurnBudget() int {
    h.mu.RLock()
    defer h.mu.RUnlock()
    if h.turnBudget == nil {
        return 0
    }
    return h.turnBudget.Available()
}

// RecordUsage records token usage at the turn level.
func (h *BudgetHierarchy) RecordUsage(tokens int) {
    h.mu.Lock()
    defer h.mu.Unlock()

    if h.turnBudget != nil {
        h.turnBudget.Allocate(tokens)

        // Check if turn budget exhausted
        if h.turnBudget.IsExhausted() {
            h.logger.Warn("Turn budget exhausted")
        }
    }

    // Also record at phase level (if turn is child of phase)
    // ...
}

// GetStatus returns a summary of budget status at all levels.
func (h *BudgetHierarchy) GetStatus() BudgetStatus {
    h.mu.RLock()
    defer h.mu.RUnlock()

    status := BudgetStatus{
        Task:   h.budgetToStatus(h.taskBudget),
        Phases: make(map[string]BudgetSummary),
    }

    for id, phase := range h.phaseBudgets {
        status.Phases[id] = h.budgetToStatus(phase)
    }

    if h.turnBudget != nil {
        status.Turn = h.budgetToStatus(h.turnBudget)
    }

    return status
}

type BudgetStatus struct {
    Task   BudgetSummary            `json:"task"`
    Phases map[string]BudgetSummary `json:"phases"`
    Turn   BudgetSummary            `json:"turn,omitempty"`
}

type BudgetSummary struct {
    Total        int     `json:"total"`
    Used         int     `json:"used"`
    Available    int     `json:"available"`
    UsageRatio   float64 `json:"usage_ratio"`
    IsExhausted  bool    `json:"is_exhausted"`
    IsWarning    bool    `json:"is_warning"`
}

func (h *BudgetHierarchy) budgetToStatus(b *BudgetAllocation) BudgetSummary {
    return BudgetSummary{
        Total:       b.totalBudget,
        Used:        b.usedBudget,
        Available:   b.Available(),
        UsageRatio:  b.UsageRatio(),
        IsExhausted: b.IsExhausted(),
        IsWarning:   b.IsWarningZone(),
    }
}
```

#### 1.5: Unit Tests

**File:** `internal/agent/budget_hierarchy_test.go`

```go
func TestBudgetAllocation_Available(t *testing.T) {
    a := NewBudgetAllocation("test", BudgetLevelTurn, 1000)
    if a.Available() != 1000 {
        t.Errorf("expected 1000 available, got %d", a.Available())
    }

    a.Allocate(300)
    if a.Available() != 700 {
        t.Errorf("expected 700 available, got %d", a.Available())
    }
}

func TestBudgetAllocation_Reserved(t *testing.T) {
    a := NewBudgetAllocation("test", BudgetLevelPhase, 1000).WithReserved(200)
    if a.Available() != 800 {
        t.Errorf("expected 800 available (excluding reserved), got %d", a.Available())
    }
    if a.AvailableWithReserved() != 1000 {
        t.Errorf("expected 1000 available (with reserved), got %d", a.AvailableWithReserved())
    }
}

func TestBudgetHierarchy_Creation(t *testing.T) {
    h := NewBudgetHierarchy(10000, []string{"phase1", "phase2"}, []int{5000, 5000})

    if h.taskBudget == nil {
        t.Fatal("task budget should be created")
    }
    if len(h.phaseBudgets) != 2 {
        t.Errorf("expected 2 phase budgets, got %d", len(h.phaseBudgets))
    }
}

func TestBudgetHierarchy_SelectPhase(t *testing.T) {
    h := NewBudgetHierarchy(10000, []string{"phase1"}, []int{5000})
    err := h.SelectPhaseBudget("phase1")
    if err != nil {
        t.Fatalf("SelectPhaseBudget failed: %v", err)
    }

    turnBudget := h.GetTurnBudget()
    if turnBudget <= 0 {
        t.Error("turn budget should be positive")
    }
}
```

**Verification:**
- [ ] Budget allocation arithmetic is correct
- [ ] Reserved budget is handled properly
- [ ] Hierarchy creation works
- [ ] Phase selection creates turn budget

---

### Phase 2: Integrate with AgentLoop

**Goal:** Replace TurnBudgetTracker with BudgetHierarchy.

#### 2.1: Update AgentLoop Struct

**File:** `internal/agent/loop.go`

```go
type AgentLoop struct {
    // ... existing fields ...

    // OLD:
    // budgetTracker *TurnBudgetTracker

    // NEW:
    budgetHierarchy *BudgetHierarchy
}
```

#### 2.2: Update NewAgentLoop

```go
// In NewAgentLoop, initialize budget hierarchy:
loop.budgetHierarchy = NewBudgetHierarchy(
    config.TotalBudget,      // e.g., 200000 tokens
    config.PhaseIDs,         // e.g., ["research", "plan", "implement", "test"]
    config.PhaseBudgets,     // e.g., [40000, 40000, 80000, 40000]
)
```

#### 2.3: Update reasoningCycle

```go
func (l *AgentLoop) reasoningCycle(ctx context.Context, conv *Conversation, conversationID string) (string, error) {
    // Select phase budget at start of each phase
    if currentPhase := l.getCurrentPhaseID(); currentPhase != "" {
        if err := l.budgetHierarchy.SelectPhaseBudget(currentPhase); err != nil {
            l.logger.Warn("Failed to select phase budget", "error", err)
        }
    }

    for iteration := 1; iteration <= l.config.MaxIterations; iteration++ {
        // Get turn budget from hierarchy
        turnBudget := l.budgetHierarchy.GetTurnBudget()
        if turnBudget <= 0 {
            return "Turn budget exhausted. Please request continuation with additional budget.", ErrBudgetExhausted
        }

        // Check if approaching phase budget limit
        if l.budgetHierarchy.GetPhaseUsageRatio(currentPhase) > 0.9 {
            l.logger.Warn("Phase budget nearly exhausted",
                "phase", currentPhase,
                "ratio", l.budgetHierarchy.GetPhaseUsageRatio(currentPhase))
        }

        // ... existing iteration logic ...

        // Record token usage
        if response != nil {
            l.budgetHierarchy.RecordUsage(response.Usage.TotalTokens)
        }
    }

    return response.Content, nil
}
```

#### 2.4: Maintain Backward Compatibility

```go
// Adapter to keep existing code working during migration
type TurnBudgetTracker struct {
    hierarchy *BudgetHierarchy
}

func (t *TurnBudgetTracker) RecordUsage(tokens int) {
    t.hierarchy.RecordUsage(tokens)
}

func (t *TurnBudgetTracker) IsWrapUpRequested() bool {
    return t.hierarchy.GetStatus().Task.IsExhausted
}

// ... other methods ...
```

**Verification:**
- [ ] Agent loop initializes hierarchy correctly
- [ ] Phase budget selection works
- [ ] Turn budget limiting works

---

### Phase 3: Budget Borrowing and Carryover

**Goal:** Enable intelligent budget reallocation.

#### 3.1: Implement Borrowing Logic

```go
// BorrowFromSibling attempts to borrow budget from a sibling allocation.
func (a *BudgetAllocation) BorrowFromSibling(tokens int) bool {
    if !a.allowBorrowing {
        return false
    }

    // Find parent
    // For simplicity, assume this is called from BudgetHierarchy
    // which knows the sibling relationships

    // Find sibling with surplus
    // Borrow from the one with most surplus

    return true // Placeholder
}

// CarryoverUnused moves unused budget to the next phase.
func (h *BudgetHierarchy) CarryoverUnused(fromPhaseID, toPhaseID string) error {
    fromPhase, ok := h.phaseBudgets[fromPhaseID]
    if !ok {
        return fmt.Errorf("source phase %s not found", fromPhaseID)
    }
    if !fromPhase.allowCarryover {
        return fmt.Errorf("phase %s does not allow carryover", fromPhaseID)
    }

    toPhase, ok := h.phaseBudgets[toPhaseID]
    if !ok {
        return fmt.Errorf("destination phase %s not found", toPhaseID)
    }

    unused := fromPhase.Available()
    if unused <= 0 {
        return nil // Nothing to carry over
    }

    // Transfer budget
    toPhase.mu.Lock()
    toPhase.totalBudget += unused
    toPhase.mu.Unlock()

    // Reset source phase used tracking
    fromPhase.mu.Lock()
    fromPhase.usedBudget = fromPhase.totalBudget // Mark as "used" for accounting
    fromPhase.mu.Unlock()

    h.logger.Info("Budget carryover",
        "from", fromPhaseID,
        "to", toPhaseID,
        "amount", unused)

    return nil
}
```

**Verification:**
- [ ] Borrowing works between siblings
- [ ] Carryover transfers unused budget

---

### Phase 4: Budget Visibility and API

**Goal:** Expose budget status to users.

#### 4.1: Add Budget Status Endpoint

**File:** `internal/comm/http/server.go`

```go
// GET /api/v1/sessions/{session_id}/budget
func (s *Server) handleGetBudget(w http.ResponseWriter, r *http.Request) {
    sessionID := mux.Vars(r)["session_id"]

    agent := s.registry.GetAgentForSession(sessionID)
    if agent == nil {
        http.Error(w, "session not found", http.StatusNotFound)
        return
    }

    status := agent.GetBudgetStatus()

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(status)
}
```

#### 4.2: Add Budget Status Method to AgentLoop

```go
// GetBudgetStatus returns the current budget status at all levels.
func (l *AgentLoop) GetBudgetStatus() BudgetStatus {
    if l.budgetHierarchy == nil {
        return BudgetStatus{}
    }
    return l.budgetHierarchy.GetStatus()
}
```

**Verification:**
- [ ] API returns correct budget JSON
- [ ] Frontend can display budget status

---

## Testing Strategy

### Unit Tests

| Test Case | File | Description |
|-----------|------|-------------|
| `TestBudgetAllocation_Available` | `budget_hierarchy_test.go` | Available calculation |
| `TestBudgetAllocation_Reserved` | `budget_hierarchy_test.go` | Reserved budget handling |
| `TestBudgetHierarchy_Creation` | `budget_hierarchy_test.go` | Hierarchy initialization |
| `TestBudgetHierarchy_Carryover` | `budget_hierarchy_test.go` | Budget carryover |

### Integration Tests

| Test Case | Description |
|-----------|-------------|
| `TestAgentLoop_BudgetHierarchy` | Full agent loop with hierarchical budgets |
| `TestBudgetExhaustion_Phase` | Phase budget exhaustion handling |

---

## Configuration Changes

**File:** `config/agent.json5`

```json5
{
  "agent": {
    "budget": {
      "total": 200000,
      "reserved_ratio": 0.1,
      "phases": {
        "enabled": true,
        "auto_allocate": true,  // Auto-distribute if not specified
        "carryover": true,
        "borrowing": true
      },
      "warning_thresholds": {
        "task": 0.7,
        "phase": 0.8,
        "turn": 0.9
      }
    }
  }
}
```

---

## Success Criteria

- [ ] **Phase 1**: Budget hierarchy types defined and tested
- [ ] **Phase 2**: AgentLoop integrated with hierarchy
- [ ] **Phase 3**: Borrowing and carryover working
- [ ] **Phase 4**: Budget status API exposed
- [ ] **Overall**: Better budget management for multi-phase tasks

---

## Comparison: Before vs. After

| Aspect | Before (TurnBudgetTracker) | After (BudgetHierarchy) |
|--------|---------------------------|------------------------|
| **Levels** | Single (turn) | Three (task/phase/turn) |
| **Isolation** | None | Phase-level isolation |
| **Reserve** | None | 10% emergency reserve |
| **Borrowing** | N/A | Between siblings |
| **Carryover** | N/A | Unused → next phase |
| **Visibility** | Total/used only | Per-level breakdown |
