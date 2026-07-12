package taint

import "sync"

// ParallelTaintTracker tracks taint propagation across parallel tool executions.
// It is used after a parallel batch completes to compute the combined taint
// label from all participating tool calls, enabling observability and policy
// checks on aggregated outputs.
type ParallelTaintTracker struct {
	mu          sync.Mutex
	taintStates map[string]TaintLabel // toolCallID -> taint label
}

// NewParallelTaintTracker creates a new ParallelTaintTracker.
func NewParallelTaintTracker() *ParallelTaintTracker {
	return &ParallelTaintTracker{
		taintStates: make(map[string]TaintLabel),
	}
}

// RecordTaint records the taint label for a tool call ID. If the same ID is
// recorded multiple times, the most restrictive label wins.
func (t *ParallelTaintTracker) RecordTaint(toolCallID string, label TaintLabel) {
	t.mu.Lock()
	defer t.mu.Unlock()
	existing, ok := t.taintStates[toolCallID]
	if !ok {
		t.taintStates[toolCallID] = label
		return
	}
	// Keep the most restrictive label.
	if taintSeverityRank(label) > taintSeverityRank(existing) {
		t.taintStates[toolCallID] = label
	}
}

// GetCombinedTaint returns the most restrictive taint label among the listed
// tool call IDs. If none of the IDs have a recorded taint, returns TaintNone.
// Severity ordering (most restrictive first):
//
//	TaintSecret > TaintUntrusted > TaintShell > TaintExternal > TaintUserInput > TaintNone
func (t *ParallelTaintTracker) GetCombinedTaint(toolCallIDs []string) TaintLabel {
	t.mu.Lock()
	defer t.mu.Unlock()

	var maxLabel TaintLabel
	for _, id := range toolCallIDs {
		if label, ok := t.taintStates[id]; ok {
			if taintSeverityRank(label) > taintSeverityRank(maxLabel) {
				maxLabel = label
			}
		}
	}
	return maxLabel
}

// IsTainted returns true if any of the listed tool call IDs has any
// non-TaintNone label recorded.
func (t *ParallelTaintTracker) IsTainted(toolCallIDs []string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, id := range toolCallIDs {
		if label, ok := t.taintStates[id]; ok {
			if label != TaintNone && label != "" {
				return true
			}
		}
	}
	return false
}

// taintSeverityRank returns the severity rank of a taint label for
// "most restrictive" comparison. Higher rank = more restrictive.
//
// Ordering: TaintSecret > TaintUntrusted > TaintShell > TaintExternal > TaintUserInput > TaintNone
func taintSeverityRank(label TaintLabel) int {
	switch label {
	case TaintSecret:
		return 5
	case TaintUntrusted:
		return 4
	case TaintShell:
		return 3
	case TaintExternal:
		return 2
	case TaintUserInput:
		return 1
	default:
		return 0
	}
}
