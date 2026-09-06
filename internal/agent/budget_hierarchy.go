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
//
// phase-frontier-parallel leaf 03 (Task-3 survey fix): phase selection was
// previously a single slot (currentPhase string + turnBudget pointer), so
// under parallel phases usage from phase B recorded against whichever phase
// was selected LAST. Selection is now map-based: selectedPhases tracks every
// concurrently-running phase and phaseTurnBudgets carries one turn budget per
// selected phase. With exactly one phase selected at a time (serial mode),
// behavior is identical to the legacy single-slot implementation.
type BudgetHierarchy struct {
	mu           sync.RWMutex
	taskBudget   *BudgetAllocation
	phaseBudgets map[string]*BudgetAllocation
	turnBudget   *BudgetAllocation // legacy-compatible handle on the most recently selected phase's turn budget
	logger       *slog.Logger

	// selectedPhases holds every currently-selected phase (leaf 03): the
	// serial path keeps exactly one entry; the parallel frontier path can
	// hold several. RecordUsage routes via its phaseID parameter.
	selectedPhases map[string]bool

	// phaseTurnBudgets carries one turn budget per selected phase (leaf
	// 03), preserving turn-budget continuity while multiple phases run.
	phaseTurnBudgets map[string]*BudgetAllocation

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
		phaseBudgets:         make(map[string]*BudgetAllocation),
		selectedPhases:       make(map[string]bool),
		phaseTurnBudgets:     make(map[string]*BudgetAllocation),
		logger:               slog.Default(),
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

// SelectPhaseBudget selects a phase and creates its per-phase turn budget
// (leaf 03, map-based selection). The turn budget is set to approximately
// 1/10 of the phase's available budget.
//
// Idempotent per phase: re-selecting an already-selected phase is a no-op,
// preserving turn-budget continuity while that phase keeps running (a fresh
// budget would reset its usage and double-add a child). Under parallel
// phases several selections coexist; in serial mode exactly one phase is
// selected at a time, matching the legacy single-slot behavior.
func (h *BudgetHierarchy) SelectPhaseBudget(phaseID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	phase, ok := h.phaseBudgets[phaseID]
	if !ok {
		return fmt.Errorf("phase %s not found", phaseID)
	}
	if h.selectedPhases[phaseID] {
		return nil // already selected: keep its turn budget untouched
	}

	// Create turn budget under phase (estimate ~10 turns per phase)
	turnBudget := NewBudgetAllocation("turn", BudgetLevelTurn, phase.Available()/10).
		WithWarningThreshold(h.turnWarningThreshold)
	phase.AddChild(turnBudget)
	h.phaseTurnBudgets[phaseID] = turnBudget
	h.selectedPhases[phaseID] = true
	// Legacy-compatible handle: the most recently selected phase's turn
	// budget (equal to the sole entry in serial mode).
	h.turnBudget = turnBudget

	return nil
}

// GetTurnBudget returns the sum of available tokens across all selected
// phases' turn budgets (leaf 03). In serial mode exactly one turn budget is
// selected, so this equals the legacy single-slot value; under parallel
// phases the caller sees the combined remaining turn capacity for the task.
// Returns 0 if no turn budget is selected.
func (h *BudgetHierarchy) GetTurnBudget() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	total := 0
	for _, tb := range h.phaseTurnBudgets {
		total += tb.Available()
	}
	return total
}

// TurnBudgetFor returns the available tokens of one phase's turn budget, or
// 0 when the phase is not selected (leaf 03, per-phase accessor).
func (h *BudgetHierarchy) TurnBudgetFor(phaseID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	tb, ok := h.phaseTurnBudgets[phaseID]
	if !ok {
		return 0
	}
	return tb.Available()
}

// RecordUsage records token usage at all three levels of the hierarchy:
// turn, phase, and task. Each level has its own totalBudget pool, so the
// same tokens are independently tracked at each level — this is the correct
// semantic for hierarchical budgets (a turn consumes from its turn
// allocation AND its parent phase AND the task).
//
// leaf 03: phaseID routes the usage to the right phase's pools while phases
// run in parallel — passing a phaseID that is not selected still records the
// usage against that phase and the task root. The phaseID parameter exists
// because the previous single currentPhase slot misattributed usage from
// concurrently-running phases to whichever phase was selected last.
//
// If the phase allocation fails because the phase is exhausted, and the phase
// has borrowing enabled, auto-borrow from a sibling and retry.
func (h *BudgetHierarchy) RecordUsage(tokens int, phaseID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// 1. Record at turn level for the usage's own phase.
	if tb, ok := h.phaseTurnBudgets[phaseID]; ok {
		tb.Allocate(tokens)
		if tb.IsExhausted() {
			if h.logger != nil {
				h.logger.Warn("turn budget exhausted",
					"level", tb.level.String(),
					"used", tb.Used(),
					"total", tb.totalBudget)
			}
		}
	}

	// 2. Record at phase level.
	if phase, ok := h.phaseBudgets[phaseID]; ok {
		if !phase.Allocate(tokens) && phase.allowBorrowing {
			// Phase exhausted and borrowing enabled — auto-borrow and retry.
			if h.borrowForPhaseLocked(phaseID, tokens) {
				if h.logger != nil {
					h.logger.Info("auto-borrow succeeded for phase",
						"phase", phaseID,
						"tokens", tokens)
				}
				phase.Allocate(tokens)
			}
		}
	}

	// 3. Record at task level.
	if h.taskBudget != nil {
		h.taskBudget.Allocate(tokens)
	}
}

// AdvancePhase transitions the most recently engaged phase's budget
// allocation to a new phase, preserving the legacy serial semantics: carry
// over the from phase's unused budget, then select the new phase. It
// delegates to AdvancePhaseFrom with the resolved from phase (the
// turn-budget handle when set, else the single selected phase). For explicit
// per-phase transitions under parallel dispatch, call AdvancePhaseFrom
// directly.
func (h *BudgetHierarchy) AdvancePhase(newPhaseID string) error {
	h.mu.Lock()
	fromPhaseID := ""
	for id := range h.selectedPhases {
		if h.turnBudget != nil && h.phaseTurnBudgets[id] == h.turnBudget {
			fromPhaseID = id
			break
		}
	}
	if fromPhaseID == "" && len(h.selectedPhases) == 1 {
		// The most recent handle is gone (already advanced); fall back to
		// the single selected phase.
		for id := range h.selectedPhases {
			fromPhaseID = id
		}
	}
	h.mu.Unlock()
	return h.AdvancePhaseFrom(fromPhaseID, newPhaseID)
}

// AdvancePhaseFrom transitions ONE phase's budget allocation from fromPhaseID
// to newPhaseID (leaf 03): if carryover is enabled on the from phase, its
// unused budget is carried over to the new phase before selecting it.
// Carryover is per-phase on the phase's OWN transition, not a global swap —
// other concurrently-selected phases keep running untouched. An empty
// fromPhaseID selects the new phase without carryover (fresh start). Returns
// an error if either phase does not exist.
func (h *BudgetHierarchy) AdvancePhaseFrom(fromPhaseID, newPhaseID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Validate the destination up front so carryover never fires for a
	// doomed transition.
	if _, ok := h.phaseBudgets[newPhaseID]; !ok {
		return fmt.Errorf("destination phase %s not found", newPhaseID)
	}
	if fromPhaseID != "" {
		if _, ok := h.phaseBudgets[fromPhaseID]; !ok {
			return fmt.Errorf("source phase %s not found", fromPhaseID)
		}
	}

	// Carry over unused budget from the from phase if applicable.
	if fromPhaseID != "" && fromPhaseID != newPhaseID {
		fromPhase := h.phaseBudgets[fromPhaseID]
		if fromPhase.allowCarryover {
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
						"from", fromPhaseID,
						"to", newPhaseID,
						"amount", unused)
				}
			}
		}
	}

	// Select the new phase (inline to avoid re-locking since we hold h.mu).
	phase := h.phaseBudgets[newPhaseID]
	// Deselect the from phase and drop its turn budget; the new phase gets
	// a fresh one. Other selected phases (parallel mode) are untouched.
	turnBudget := NewBudgetAllocation("turn", BudgetLevelTurn, phase.Available()/10).
		WithWarningThreshold(h.turnWarningThreshold)
	phase.AddChild(turnBudget)
	h.phaseTurnBudgets[newPhaseID] = turnBudget
	h.selectedPhases[newPhaseID] = true
	if fromPhaseID != "" && fromPhaseID != newPhaseID {
		delete(h.selectedPhases, fromPhaseID)
		delete(h.phaseTurnBudgets, fromPhaseID)
	}
	// Legacy-compatible handle: the most recently selected phase's turn
	// budget (equal to the sole entry in serial mode).
	h.turnBudget = turnBudget

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

	// leaf 03: Turn aggregates across selected phases' turn budgets so the
	// status reflects every concurrently-running phase, not just the
	// latest. Serial mode has exactly one entry, matching legacy behavior;
	// nothing selected yields the zero summary (Turn omitted downstream).
	sum := BudgetAllocation{id: "turn", level: BudgetLevelTurn}
	for _, tb := range h.phaseTurnBudgets {
		tb.mu.RLock()
		sum.totalBudget += tb.totalBudget
		sum.usedBudget += tb.usedBudget
		tb.mu.RUnlock()
	}
	status.Turn = h.budgetToStatus(&sum)

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
