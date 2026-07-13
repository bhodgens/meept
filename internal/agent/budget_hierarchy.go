package agent

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// BudgetLevel represents the level in the budget hierarchy.
type BudgetLevel uint8

const (
	// BudgetLevelTask is the top-level budget for an entire task.
	BudgetLevelTask BudgetLevel = iota
	// BudgetLevelPhase is the budget for a specific plan phase.
	BudgetLevelPhase
	// BudgetLevelTurn is the budget for a single iteration/turn.
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

// BudgetAllocation represents a token allocation at a specific level
// in the hierarchical budget system.
type BudgetAllocation struct {
	mu sync.RWMutex

	// Metadata
	id        string
	level     BudgetLevel
	name      string // Human-readable name (set via WithName)
	parentID  string // Parent allocation ID (empty for task-level)
	createdAt time.Time

	// Budget tracking
	totalBudget    int // Total tokens allocated
	usedBudget     int // Tokens used so far
	reservedBudget int // Tokens reserved (emergency reserve, not to be used normally)

	// Configuration
	warningThreshold float64 // Ratio at which to warn (default 0.8 = 80%)
	allowBorrowing   bool    // Can borrow from sibling allocations
	allowCarryover   bool    // Can unused budget carry over to next allocation

	// Hierarchical relationships
	children []*BudgetAllocation // Sub-allocations (for task/phase levels)

	// Callbacks (invoked from Allocate when thresholds are crossed).
	onLowBudget func(*BudgetAllocation)
	onExhausted func(*BudgetAllocation)
}

// NewBudgetAllocation creates a new budget allocation with sensible defaults.
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

// WithBorrowing enables or disables borrowing from siblings.
func (a *BudgetAllocation) WithBorrowing(enabled bool) *BudgetAllocation {
	a.allowBorrowing = enabled
	return a
}

// WithCarryover enables or disables carryover of unused budget.
func (a *BudgetAllocation) WithCarryover(enabled bool) *BudgetAllocation {
	a.allowCarryover = enabled
	return a
}

// WithWarningThreshold sets the warning threshold ratio.
func (a *BudgetAllocation) WithWarningThreshold(ratio float64) *BudgetAllocation {
	a.warningThreshold = ratio
	return a
}

// WithName sets a human-readable name for the allocation.
func (a *BudgetAllocation) WithName(name string) *BudgetAllocation {
	a.name = name
	return a
}

// OnLowBudget registers a callback fired when the allocation enters the
// warning zone (usage >= warningThreshold) during a successful Allocate. The
// callback is invoked outside the allocation mutex to avoid re-entrancy.
func (a *BudgetAllocation) OnLowBudget(cb func(*BudgetAllocation)) {
	a.mu.Lock()
	a.onLowBudget = cb
	a.mu.Unlock()
}

// OnExhausted registers a callback fired when the allocation becomes
// exhausted (Available() <= 0) during a successful Allocate. Invoked outside
// the allocation mutex.
func (a *BudgetAllocation) OnExhausted(cb func(*BudgetAllocation)) {
	a.mu.Lock()
	a.onExhausted = cb
	a.mu.Unlock()
}

// Available returns the available budget (total - used - reserved).
func (a *BudgetAllocation) Available() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.totalBudget - a.usedBudget - a.reservedBudget
}

// AvailableWithReserved returns available budget including reserved tokens
// (for emergency use).
func (a *BudgetAllocation) AvailableWithReserved() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.totalBudget - a.usedBudget
}

// Used returns the amount of budget used.
func (a *BudgetAllocation) Used() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.usedBudget
}

// UsageRatio returns the fraction of budget used (0.0 to 1.0+).
// Returns 0 if totalBudget is 0.
func (a *BudgetAllocation) UsageRatio() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.totalBudget == 0 {
		return 0
	}
	return float64(a.usedBudget) / float64(a.totalBudget)
}

// IsExhausted returns true if no budget is available (excluding reserved).
func (a *BudgetAllocation) IsExhausted() bool {
	return a.Available() <= 0
}

// IsWarningZone returns true if usage has reached the warning threshold.
func (a *BudgetAllocation) IsWarningZone() bool {
	a.mu.RLock()
	threshold := a.warningThreshold
	a.mu.RUnlock()
	return a.UsageRatio() >= threshold
}

// Allocate consumes budget from the allocation.
// Returns true if successful, false if insufficient budget (excluding reserved).
// On success, fires onLowBudget when usage crosses the warning threshold and
// onExhausted when usage exhausts the available pool. Callbacks run outside
// the allocation mutex.
func (a *BudgetAllocation) Allocate(tokens int) bool {
	a.mu.Lock()
	available := a.totalBudget - a.usedBudget - a.reservedBudget
	if tokens > available {
		a.mu.Unlock()
		return false
	}
	wasWarning := a.usageRatioLocked() >= a.warningThreshold
	wasExhausted := available == 0
	a.usedBudget += tokens
	nowWarning := a.usageRatioLocked() >= a.warningThreshold
	nowExhausted := (a.totalBudget - a.usedBudget - a.reservedBudget) <= 0
	lowCB, exCB := a.onLowBudget, a.onExhausted
	a.mu.Unlock()

	// Fire callbacks outside the mutex.
	if !wasWarning && nowWarning && lowCB != nil {
		lowCB(a)
	}
	if !wasExhausted && nowExhausted && exCB != nil {
		exCB(a)
	}
	return true
}

// usageRatioLocked is a lock-held helper for computing usage ratio.
func (a *BudgetAllocation) usageRatioLocked() float64 {
	if a.totalBudget == 0 {
		return 0
	}
	return float64(a.usedBudget) / float64(a.totalBudget)
}

// AllocateFromReserved consumes from the reserved budget (emergency use).
//
// Deviation from plan: The plan's arithmetic
// (`reservedBudget - (usedBudget - (totalBudget - reservedBudget))`) produces
// nonsensical results when the normal allocation pool hasn't been fully
// consumed. Instead, this implementation treats the reserved pool as part of
// the total: it succeeds only if the resulting usedBudget does not exceed
// totalBudget. This means: after all normal budget is consumed, up to
// reservedBudget tokens of emergency allocation are accepted.
//
// Returns true if successful, false if tokens <= 0 or if allocating would
// exceed totalBudget.
func (a *BudgetAllocation) AllocateFromReserved(tokens int) bool {
	if tokens <= 0 {
		return false
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.usedBudget+tokens > a.totalBudget {
		return false
	}

	a.usedBudget += tokens
	return true
}

// Release returns budget to the allocation (e.g., on error recovery).
// Clamps to usedBudget so it cannot go negative.
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

// GetChild returns a child allocation by ID, or nil if not found.
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

// GetChildren returns a defensive copy of all child allocations.
func (a *BudgetAllocation) GetChildren() []*BudgetAllocation {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make([]*BudgetAllocation, len(a.children))
	copy(result, a.children)
	return result
}

// BudgetSummary is an exported, JSON-serializable snapshot of a budget
// allocation's current state.
type BudgetSummary struct {
	Total       int     `json:"total"`
	Used        int     `json:"used"`
	Available   int     `json:"available"`
	UsageRatio  float64 `json:"usage_ratio"`
	IsExhausted bool    `json:"is_exhausted"`
	IsWarning   bool    `json:"is_warning"`
}

// BudgetStatus is an exported, JSON-serializable overview of the entire
// budget hierarchy.
type BudgetStatus struct {
	Task   BudgetSummary            `json:"task"`
	Phases map[string]BudgetSummary `json:"phases"`
	Turn   BudgetSummary            `json:"turn,omitempty"`
}

// BudgetHierarchy manages the budget hierarchy for a task, providing
// task-level, phase-level, and turn-level budget tracking.
type BudgetHierarchy struct {
	mu           sync.RWMutex
	taskBudget   *BudgetAllocation
	phaseBudgets map[string]*BudgetAllocation
	turnBudget   *BudgetAllocation
	currentPhase string // ID of the currently-selected phase
	logger       *slog.Logger

	// turnWarningThreshold is applied to turn budgets created by
	// SelectPhaseBudget and AdvancePhase. Defaults to 0.9 when zero.
	turnWarningThreshold float64
}

// BudgetHierarchyOptions configures optional BudgetHierarchy behavior.
// All fields have sensible defaults when zero-valued.
type BudgetHierarchyOptions struct {
	ReservedRatio     float64 // 0 = use default 0.1
	TaskWarningRatio  float64 // 0 = use default 0.7
	PhaseWarningRatio float64 // 0 = use default 0.8
	TurnWarningRatio  float64 // 0 = use default 0.9
	CarryoverEnabled  bool
	BorrowingEnabled  bool
}

// Default option values when BudgetHierarchyOptions fields are zero.
const (
	defaultReservedRatio     = 0.1
	defaultTaskWarningRatio  = 0.7
	defaultPhaseWarningRatio = 0.8
	defaultTurnWarningRatio  = 0.9
)

// effectiveReservedRatio returns the option value or the default when zero.
func (o BudgetHierarchyOptions) effectiveReservedRatio() float64 {
	if o.ReservedRatio == 0 {
		return defaultReservedRatio
	}
	return o.ReservedRatio
}

// effectiveTaskWarningRatio returns the option value or the default when zero.
func (o BudgetHierarchyOptions) effectiveTaskWarningRatio() float64 {
	if o.TaskWarningRatio == 0 {
		return defaultTaskWarningRatio
	}
	return o.TaskWarningRatio
}

// effectivePhaseWarningRatio returns the option value or the default when zero.
func (o BudgetHierarchyOptions) effectivePhaseWarningRatio() float64 {
	if o.PhaseWarningRatio == 0 {
		return defaultPhaseWarningRatio
	}
	return o.PhaseWarningRatio
}

// effectiveTurnWarningRatio returns the option value or the default when zero.
func (o BudgetHierarchyOptions) effectiveTurnWarningRatio() float64 {
	if o.TurnWarningRatio == 0 {
		return defaultTurnWarningRatio
	}
	return o.TurnWarningRatio
}

// NewBudgetHierarchy creates a new budget hierarchy.
//
// The task budget receives a 10% emergency reserve, a 0.7 warning threshold,
// and borrowing enabled. Each phase budget gets carryover enabled and a 0.8
// warning threshold. Phases with a budget <= 0 are auto-distributed an equal
// share of the non-reserved task budget.
func NewBudgetHierarchy(taskBudget int, phases []string, phaseBudgets []int) *BudgetHierarchy {
	return newBudgetHierarchyWithOpts(taskBudget, phases, phaseBudgets, BudgetHierarchyOptions{
		ReservedRatio:     defaultReservedRatio,
		TaskWarningRatio:  defaultTaskWarningRatio,
		PhaseWarningRatio: defaultPhaseWarningRatio,
		TurnWarningRatio:  defaultTurnWarningRatio,
		CarryoverEnabled:  true,
		BorrowingEnabled:  true,
	})
}

// NewBudgetHierarchyWithConfig is like NewBudgetHierarchy but accepts
// configuration overrides for reserve ratio, warning thresholds, and
// carryover/borrowing behavior. Zero-valued fields fall back to defaults.
func NewBudgetHierarchyWithConfig(taskBudget int, phases []string, phaseBudgets []int, opts BudgetHierarchyOptions) *BudgetHierarchy {
	return newBudgetHierarchyWithOpts(taskBudget, phases, phaseBudgets, opts)
}

// newBudgetHierarchyWithOpts is the shared implementation for both
// NewBudgetHierarchy and NewBudgetHierarchyWithConfig.
func newBudgetHierarchyWithOpts(taskBudget int, phases []string, phaseBudgets []int, opts BudgetHierarchyOptions) *BudgetHierarchy {
	h := &BudgetHierarchy{
		phaseBudgets:        make(map[string]*BudgetAllocation),
		logger:              slog.Default(),
		turnWarningThreshold: opts.effectiveTurnWarningRatio(),
	}

	reservedRatio := opts.effectiveReservedRatio()
	taskWarn := opts.effectiveTaskWarningRatio()
	phaseWarn := opts.effectivePhaseWarningRatio()

	reserved := int(float64(taskBudget) * reservedRatio)

	// Create task-level budget
	h.taskBudget = NewBudgetAllocation("task", BudgetLevelTask, taskBudget).
		WithReserved(reserved).
		WithWarningThreshold(taskWarn).
		WithBorrowing(opts.BorrowingEnabled)

	// Create phase budgets
	// Compute auto-distribution correctly: sum explicitly-allocated phases,
	// then split the remaining non-reserved pool among auto-allocated phases.
	nonReservedPool := taskBudget - reserved
	explicitSum := 0
	autoCount := 0
	for i := range phases {
		budget := 0
		if i < len(phaseBudgets) {
			budget = phaseBudgets[i]
		}
		if budget > 0 {
			explicitSum += budget
		} else {
			autoCount++
		}
	}
	remaining := nonReservedPool - explicitSum
	if remaining < 0 {
		if h.logger != nil {
			h.logger.Warn("phase budgets exceed task budget",
				"task_budget", taskBudget,
				"explicit_sum", explicitSum,
				"reserved", reserved)
		}
		remaining = 0
	}

	for i, phaseID := range phases {
		budget := 0
		if i < len(phaseBudgets) {
			budget = phaseBudgets[i]
		}
		if budget <= 0 {
			if autoCount > 0 {
				budget = remaining / autoCount
			}
		}

		phaseAlloc := NewBudgetAllocation(phaseID, BudgetLevelPhase, budget).
			WithCarryover(opts.CarryoverEnabled).
			WithWarningThreshold(phaseWarn)

		h.phaseBudgets[phaseID] = phaseAlloc
		h.taskBudget.AddChild(phaseAlloc)
	}

	return h
}

// SelectPhaseBudget sets the active phase budget and creates a turn budget
// under it. The turn budget is set to approximately 1/10 of the phase's
// available budget.
func (h *BudgetHierarchy) SelectPhaseBudget(phaseID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	phase, ok := h.phaseBudgets[phaseID]
	if !ok {
		return fmt.Errorf("phase %s not found", phaseID)
	}

	// Create turn budget under phase (estimate ~10 turns per phase)
	turnBudget := phase.Available() / 10
	h.turnBudget = NewBudgetAllocation("turn", BudgetLevelTurn, turnBudget).
		WithWarningThreshold(h.turnWarningThreshold)
	phase.AddChild(h.turnBudget)
	h.currentPhase = phaseID

	return nil
}

// GetTurnBudget returns the current turn budget's available tokens.
// Returns 0 if no turn budget is selected.
func (h *BudgetHierarchy) GetTurnBudget() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.turnBudget == nil {
		return 0
	}
	return h.turnBudget.Available()
}

// RecordUsage records token usage at all three levels of the hierarchy:
// turn, the currently-selected phase, and the task root. Each level has its
// own totalBudget pool, so the same tokens are independently tracked at each
// level — this is the correct semantic for hierarchical budgets (a turn
// consumes from its turn allocation AND its parent phase AND the task).
//
// If the phase allocation fails because the phase is exhausted, and the phase
// has borrowing enabled, auto-borrow from a sibling and retry.
func (h *BudgetHierarchy) RecordUsage(tokens int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// 1. Record at turn level.
	if h.turnBudget != nil {
		h.turnBudget.Allocate(tokens)

		if h.turnBudget.IsExhausted() {
			if h.logger != nil {
				h.logger.Warn("turn budget exhausted",
					"level", h.turnBudget.level.String(),
					"used", h.turnBudget.Used(),
					"total", h.turnBudget.totalBudget)
			}
		}
	}

	// 2. Record at phase level.
	if h.currentPhase != "" {
		phase, ok := h.phaseBudgets[h.currentPhase]
		if ok {
			if !phase.Allocate(tokens) && phase.allowBorrowing {
				// Phase exhausted and borrowing enabled — auto-borrow and retry.
				if h.borrowForPhaseLocked(h.currentPhase, tokens) {
					if h.logger != nil {
						h.logger.Info("auto-borrow succeeded for phase",
							"phase", h.currentPhase,
							"tokens", tokens)
					}
					phase.Allocate(tokens)
				}
			}
		}
	}

	// 3. Record at task level.
	if h.taskBudget != nil {
		h.taskBudget.Allocate(tokens)
	}
}

// AdvancePhase transitions from the current phase to a new phase. If
// carryover is enabled on the current phase, unused budget is carried over to
// the new phase before selecting it. Returns an error if the new phase does
// not exist or if carryover fails.
func (h *BudgetHierarchy) AdvancePhase(newPhaseID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Carry over unused budget from current phase if applicable.
	if h.currentPhase != "" && h.currentPhase != newPhaseID {
		fromPhase, ok := h.phaseBudgets[h.currentPhase]
		if ok && fromPhase.allowCarryover {
			if _, ok := h.phaseBudgets[newPhaseID]; !ok {
				return fmt.Errorf("destination phase %s not found", newPhaseID)
			}
			unused := fromPhase.Available()
			if unused > 0 {
				toPhase := h.phaseBudgets[newPhaseID]
				toPhase.mu.Lock()
				toPhase.totalBudget += unused
				toPhase.mu.Unlock()

				fromPhase.mu.Lock()
				fromPhase.usedBudget = fromPhase.totalBudget
				fromPhase.mu.Unlock()

				if h.logger != nil {
					h.logger.Info("budget carryover on phase advance",
						"from", h.currentPhase,
						"to", newPhaseID,
						"amount", unused)
				}
			}
		}
	}

	// Select the new phase (inline to avoid re-locking since we hold h.mu).
	phase, ok := h.phaseBudgets[newPhaseID]
	if !ok {
		return fmt.Errorf("phase %s not found", newPhaseID)
	}
	turnBudget := phase.Available() / 10
	h.turnBudget = NewBudgetAllocation("turn", BudgetLevelTurn, turnBudget).
		WithWarningThreshold(h.turnWarningThreshold)
	phase.AddChild(h.turnBudget)
	h.currentPhase = newPhaseID

	return nil
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

// CarryoverUnused moves unused budget from one phase to another. The source
// phase must have allowCarryover enabled. After carryover, the source phase's
// usedBudget is set equal to its totalBudget so it appears fully consumed for
// accounting purposes.
func (h *BudgetHierarchy) CarryoverUnused(fromPhaseID, toPhaseID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

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

	// Transfer budget to destination phase
	toPhase.mu.Lock()
	toPhase.totalBudget += unused
	toPhase.mu.Unlock()

	// Mark source phase as fully consumed
	fromPhase.mu.Lock()
	fromPhase.usedBudget = fromPhase.totalBudget
	fromPhase.mu.Unlock()

	if h.logger != nil {
		h.logger.Info("budget carryover",
			"from", fromPhaseID,
			"to", toPhaseID,
			"amount", unused)
	}

	return nil
}

// BorrowForPhase attempts to borrow tokens from a sibling phase that has
// borrowing enabled and sufficient surplus. The sibling with the most surplus
// is preferred. Returns true if the borrow succeeded, false otherwise.
func (h *BudgetHierarchy) BorrowForPhase(phaseID string, tokens int) bool {
	if tokens <= 0 {
		return false
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	return h.borrowForPhaseLocked(phaseID, tokens)
}

// borrowForPhaseLocked is the lock-held implementation of BorrowForPhase.
// Caller MUST hold h.mu.
func (h *BudgetHierarchy) borrowForPhaseLocked(phaseID string, tokens int) bool {
	if tokens <= 0 {
		return false
	}

	phase, ok := h.phaseBudgets[phaseID]
	if !ok {
		return false
	}

	// Find the sibling with the most surplus that allows borrowing.
	var bestSibling *BudgetAllocation
	bestSurplus := 0
	for id, sibling := range h.phaseBudgets {
		if id == phaseID {
			continue
		}
		if !sibling.allowBorrowing {
			continue
		}
		surplus := sibling.Available()
		if surplus >= tokens && surplus > bestSurplus {
			bestSibling = sibling
			bestSurplus = surplus
		}
	}

	if bestSibling == nil {
		return false
	}

	// Deduct from sibling
	bestSibling.mu.Lock()
	bestSibling.usedBudget += tokens
	bestSibling.mu.Unlock()

	// Add to requesting phase
	phase.mu.Lock()
	phase.totalBudget += tokens
	phase.mu.Unlock()

	if h.logger != nil {
		h.logger.Info("budget borrow",
			"phase", phaseID,
			"amount", tokens)
	}

	return true
}

// budgetSnapshot captures a consistent point-in-time view of a BudgetAllocation.
type budgetSnapshot struct {
	totalBudget      int
	usedBudget       int
	reservedBudget   int
	warningThreshold float64
}

// snapshot returns a consistent snapshot of the allocation's budget fields
// under a read lock. Callers can use this to avoid reading individual fields
// without synchronization.
func (a *BudgetAllocation) snapshot() budgetSnapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return budgetSnapshot{
		totalBudget:      a.totalBudget,
		usedBudget:       a.usedBudget,
		reservedBudget:   a.reservedBudget,
		warningThreshold: a.warningThreshold,
	}
}

// budgetToStatus converts a BudgetAllocation to an exported BudgetSummary.
func (h *BudgetHierarchy) budgetToStatus(b *BudgetAllocation) BudgetSummary {
	if b == nil {
		return BudgetSummary{}
	}
	s := b.snapshot()
	available := s.totalBudget - s.usedBudget - s.reservedBudget
	var usageRatio float64
	if s.totalBudget > 0 {
		usageRatio = float64(s.usedBudget) / float64(s.totalBudget)
	}
	return BudgetSummary{
		Total:       s.totalBudget,
		Used:        s.usedBudget,
		Available:   available,
		UsageRatio:  usageRatio,
		IsExhausted: available <= 0,
		IsWarning:   usageRatio >= s.warningThreshold,
	}
}
