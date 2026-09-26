package validator

import (
	"context"
	"fmt"

	"github.com/caimlas/meept/internal/task"
)

// FilterOutcome is the tri-state outcome of an output filter: the result
// continues unchanged, is rewritten, or is rejected.
type FilterOutcome int

const (
	// FilterPass indicates the result continues unchanged.
	FilterPass FilterOutcome = iota
	// FilterRewrite indicates Output replaces the result.
	FilterRewrite
	// FilterFail indicates the result is rejected; Reason feeds rework.
	FilterFail
	// FilterAdvisory indicates a problem was detected and logged but the
	// result continues unchanged — the verdict is advisory (linters over
	// mixed prose/code step output, runs 33-36: a narration sentence
	// inside a ```js fence is a labeling mistake, not a turn-fatal
	// syntax error). Reason carries the diagnostic.
	FilterAdvisory
)

// String returns the machine-readable name of the outcome, as used in
// ChainResult Actions lines.
func (o FilterOutcome) String() string {
	switch o {
	case FilterPass:
		return "pass"
	case FilterRewrite:
		return "rewrite"
	case FilterFail:
		return "fail"
	case FilterAdvisory:
		return "advisory"
	default:
		return fmt.Sprintf("unknown_filter_outcome(%d)", int(o))
	}
}

// FilterResult is the outcome of a single output-filter invocation.
// The invariants are enforced by Valid: Filter is ALWAYS set, Output is
// set iff the outcome is FilterRewrite, and Reason is set iff the outcome
// is FilterFail.
type FilterResult struct {
	// Outcome is the tri-state verdict of the filter.
	Outcome FilterOutcome
	// Filter is the stage name, e.g. "json_format" - ALWAYS set.
	Filter string
	// Output is the rewritten result; set iff Outcome == FilterRewrite.
	Output string
	// Reason is machine-readable failure context; set iff Outcome == FilterFail.
	Reason string
}

// Valid reports whether the result satisfies the tri-state invariants:
// Filter is always set, a rewrite carries only Output, and a failure
// carries only Reason.
func (r FilterResult) Valid() bool {
	if r.Filter == "" {
		return false
	}
	switch r.Outcome {
	case FilterPass:
		return r.Output == "" && r.Reason == ""
	case FilterRewrite:
		return r.Output != "" && r.Reason == ""
	case FilterFail:
		return r.Reason != "" && r.Output == ""
	case FilterAdvisory:
		return r.Reason != "" && r.Output == ""
	default:
		return false
	}
}

// OutputFilter is the interface for a milter-style post-step content stage.
// Unlike Validator (which validates tool-execution evidence), an OutputFilter
// validates or repairs step RESULT CONTENT.
//
// Implementations MUST NOT mutate the step: they receive the step for
// context (Applies) and return strings (Process). Process must be
// idempotent for rewrite stages: Process(p(x)) == Process(x).
type OutputFilter interface {
	// Name returns the stage name used in Actions log lines.
	Name() string
	// Applies reports whether the filter applies to this step. Filters
	// that return false are skipped entirely: they do not run, do not
	// count as a pass, and do not appear in Actions.
	Applies(step *task.TaskStep) bool
	// Process runs the filter over the current output and returns the verdict.
	Process(ctx context.Context, step *task.TaskStep, output string) FilterResult
}

// FilterChainConfig configures a FilterChain.
type FilterChainConfig struct {
	// Filters is the ordered filter list; the order IS the contract.
	Filters []OutputFilter
	// MaxPasses bounds the rewrite sweeps before forced fail; default 2.
	MaxPasses int
}

// ChainResult is the outcome of a FilterChain run.
type ChainResult struct {
	// Output is the final output after rewrites.
	Output string
	// Rejected is non-nil iff the chain failed (a filter rejection or
	// non-convergence).
	Rejected *FilterResult
	// Passes is the number of sweeps executed.
	Passes int
	// Actions holds one line per applied filter per sweep, in execution
	// order: "pass=<n> filter=<name> action=pass|rewrite|fail".
	Actions []string
}

// defaultFilterMaxPasses is the rewrite-sweep cap applied when a chain is
// constructed with MaxPasses <= 0.
const defaultFilterMaxPasses = 2

// FilterChain executes an ordered set of OutputFilters over step output.
//
// The chain itself holds no per-run mutable state: Run operates only on
// locals, so a single chain is safe for concurrent use.
//
//nolint:revive // stutter with package name is intentional for API clarity
type FilterChain struct {
	config FilterChainConfig
}

// NewFilterChain creates a FilterChain from the ordered filters. A
// maxPasses value <= 0 defaults to 2. The filter slice is copied, so later
// mutation of the caller's slice does not affect the chain.
func NewFilterChain(filters []OutputFilter, maxPasses int) *FilterChain {
	if maxPasses <= 0 {
		maxPasses = defaultFilterMaxPasses
	}
	owned := make([]OutputFilter, len(filters))
	copy(owned, filters)
	return &FilterChain{
		config: FilterChainConfig{Filters: owned, MaxPasses: maxPasses},
	}
}

// Run executes the chain over output for the given step.
//
// Frozen semantics (parent Contracts 1-2):
//  1. One pass = one sweep over all filters in declared order.
//  2. FilterFail terminates the chain immediately; later filters do not run.
//  3. FilterRewrite updates the output; the sweep re-runs from the FIRST
//     filter with the new output.
//  4. If Passes reaches MaxPasses and the output still changes, the chain
//     returns FilterFail with reason "filter chain did not converge after
//     N passes".
//  5. Filters where Applies is false are skipped, do not count as a pass,
//     and do not appear in Actions.
//  6. Every applied filter appends exactly one Actions line per sweep.
//  7. The chain never mutates the step.
func (fc *FilterChain) Run(ctx context.Context, step *task.TaskStep, output string) ChainResult {
	actions := make([]string, 0, len(fc.config.Filters))
	passes := 0

	for passes < fc.config.MaxPasses {
		passes++
		changed := false

		for _, filter := range fc.config.Filters {
			if !filter.Applies(step) {
				continue
			}
			result := filter.Process(ctx, step, output)
			actions = append(actions, fmt.Sprintf("pass=%d filter=%s action=%s",
				passes, filter.Name(), result.Outcome))

			switch result.Outcome {
			case FilterFail:
				rejected := result
				return ChainResult{Output: output, Rejected: &rejected, Passes: passes, Actions: actions}
			case FilterRewrite:
				output = result.Output
				changed = true
			case FilterPass:
				// Output continues unchanged.
			case FilterAdvisory:
				// Diagnostic logged via Actions; output continues.
			}
			if changed {
				break // restart the sweep from the first filter
			}
		}

		if !changed {
			return ChainResult{Output: output, Rejected: nil, Passes: passes, Actions: actions}
		}
	}

	// Non-convergence: the output still changed on the final sweep.
	rejected := FilterResult{
		Outcome: FilterFail,
		Filter:  "filter_chain",
		Reason:  fmt.Sprintf("filter chain did not converge after %d passes", fc.config.MaxPasses),
	}
	return ChainResult{Output: output, Rejected: &rejected, Passes: passes, Actions: actions}
}
