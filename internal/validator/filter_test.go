package validator

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

// --- test filter stubs ------------------------------------------------------

// stubFilter invokes a scripted verdict per invocation. The script is a
// queue: each Process call pops the next verdict; the last verdict repeats
// once the queue is empty.
type stubFilter struct {
	name    string
	applies bool
	script  []func(output string) FilterResult
	calls   *[]string // invocation log: "<name>:<output>"
}

func (f *stubFilter) Name() string { return f.name }

func (f *stubFilter) Applies(_ *task.TaskStep) bool { return f.applies }

func (f *stubFilter) Process(_ context.Context, _ *task.TaskStep, output string) FilterResult {
	if f.calls != nil {
		*f.calls = append(*f.calls, f.name+":"+output)
	}
	if len(f.script) == 0 {
		return FilterResult{Outcome: FilterPass, Filter: f.name}
	}
	// Pop from the front; the final scripted verdict repeats forever.
	fn := f.script[0]
	if len(f.script) > 1 {
		f.script = f.script[1:]
	}
	return fn(output)
}

func passFilter(name string, calls *[]string) *stubFilter {
	return &stubFilter{name: name, applies: true, calls: calls}
}

func skipFilter(name string, calls *[]string) *stubFilter {
	return &stubFilter{name: name, applies: false, calls: calls}
}

// scripted builds a filter that returns the given results in order,
// repeating the last one forever.
func scripted(name string, calls *[]string, results ...FilterResult) *stubFilter {
	cps := make([]func(string) FilterResult, 0, len(results))
	for _, r := range results {
		res := r
		cps = append(cps, func(string) FilterResult { return res })
	}
	return &stubFilter{name: name, applies: true, calls: calls, script: cps}
}

// --- Task 1: FilterResult invariants ----------------------------------------

func TestFilterResultInvariants(t *testing.T) {
	r := FilterResult{Outcome: FilterRewrite, Filter: "json_format", Output: "{}"}
	if r.Output == "" || r.Reason != "" {
		t.Fatal("rewrite must set Output only")
	}
	if !r.Valid() {
		t.Fatal("rewrite result with Output set must be Valid")
	}

	f := FilterResult{Outcome: FilterFail, Filter: "language_en", Reason: "lang=de confidence=0.97"}
	if f.Reason == "" || f.Output != "" {
		t.Fatal("fail must set Reason only")
	}
	if !f.Valid() {
		t.Fatal("fail result with Reason set must be Valid")
	}

	p := FilterResult{Outcome: FilterPass, Filter: "lint_go"}
	if p.Output != "" || p.Reason != "" {
		t.Fatal("pass must set neither")
	}
	if !p.Valid() {
		t.Fatal("pass result with neither set must be Valid")
	}

	// Invalid variants must be rejected by Valid.
	invalids := []FilterResult{
		{Outcome: FilterRewrite, Filter: "x"},                    // rewrite without Output
		{Outcome: FilterRewrite, Output: "y"},                    // rewrite without Filter
		{Outcome: FilterFail, Filter: "x"},                       // fail without Reason
		{Outcome: FilterFail, Reason: "why", Output: "leak"},     // fail with Output
		{Outcome: FilterPass, Filter: "x", Reason: "unexpected"}, // pass with Reason
		{Outcome: FilterPass, Filter: "x", Output: "unexpected"}, // pass with Output
		{Outcome: FilterOutcome(99), Filter: "x"},                // unknown outcome
	}
	for i, inv := range invalids {
		if inv.Valid() {
			t.Errorf("invalid case %d must not be Valid: %+v", i, inv)
		}
	}
}

// --- Task 2: sweep order, fail-fast, Applies skipping ------------------------

func TestFilterChainSweep(t *testing.T) {
	t.Run("declaration_order", func(t *testing.T) {
		var calls []string
		chain := NewFilterChain([]OutputFilter{
			passFilter("a", &calls),
			passFilter("b", &calls),
			passFilter("c", &calls),
		}, 0)
		res := chain.Run(context.Background(), &task.TaskStep{}, "out")
		if res.Rejected != nil {
			t.Fatalf("unexpected rejection: %+v", res.Rejected)
		}
		want := []string{"a:out", "b:out", "c:out"}
		if strings.Join(calls, ",") != strings.Join(want, ",") {
			t.Fatalf("invocation order = %v, want %v", calls, want)
		}
		if res.Passes != 1 {
			t.Errorf("Passes = %d, want 1", res.Passes)
		}
		if res.Output != "out" {
			t.Errorf("Output = %q, want %q", res.Output, "out")
		}
	})

	t.Run("fail_fast_stops_later_filters", func(t *testing.T) {
		var calls []string
		chain := NewFilterChain([]OutputFilter{
			passFilter("a", &calls),
			scripted("b", &calls, FilterResult{Outcome: FilterFail, Filter: "b", Reason: "bad"}),
			passFilter("c", &calls),
		}, 0)
		res := chain.Run(context.Background(), &task.TaskStep{}, "out")
		if res.Rejected == nil {
			t.Fatal("expected rejection")
		}
		if res.Rejected.Filter != "b" || res.Rejected.Reason != "bad" {
			t.Errorf("rejected = %+v, want filter=b reason=bad", res.Rejected)
		}
		if !res.Rejected.Valid() {
			t.Error("rejected FilterResult must be Valid")
		}
		for _, c := range calls {
			if strings.HasPrefix(c, "c:") {
				t.Errorf("filter c ran after fail: %v", calls)
			}
		}
		if len(calls) != 2 {
			t.Errorf("invocations = %v, want exactly a and b", calls)
		}
		if res.Passes != 1 {
			t.Errorf("Passes = %d, want 1", res.Passes)
		}
	})

	t.Run("applies_false_skipped_entirely", func(t *testing.T) {
		var calls []string
		skipped := skipFilter("skipped", &calls)
		chain := NewFilterChain([]OutputFilter{
			passFilter("a", &calls),
			skipped,
			passFilter("c", &calls),
		}, 0)
		res := chain.Run(context.Background(), &task.TaskStep{}, "out")
		if res.Rejected != nil {
			t.Fatalf("unexpected rejection: %+v", res.Rejected)
		}
		if len(calls) != 2 {
			t.Errorf("skipped filter ran: %v", calls)
		}
		for _, action := range res.Actions {
			if strings.Contains(action, "filter=skipped") {
				t.Errorf("skipped filter appears in Actions: %v", res.Actions)
			}
		}
	})
}

// --- Task 3: rewrite re-sweep, convergence cap, default MaxPasses ------------

func TestFilterChainRewrite(t *testing.T) {
	t.Run("rewrite_restarts_from_first_filter", func(t *testing.T) {
		var calls []string
		a := passFilter("a", &calls)
		// b rewrites once, then passes so the second sweep converges.
		b := scripted("b", &calls,
			FilterResult{Outcome: FilterRewrite, Filter: "b", Output: "fixed"},
			FilterResult{Outcome: FilterPass, Filter: "b"},
		)
		chain := NewFilterChain([]OutputFilter{a, b}, 5)
		res := chain.Run(context.Background(), &task.TaskStep{}, "raw")
		if res.Rejected != nil {
			t.Fatalf("unexpected rejection: %+v", res.Rejected)
		}
		if res.Output != "fixed" {
			t.Errorf("Output = %q, want %q", res.Output, "fixed")
		}
		// a runs first with "raw", then rewrite restarts the sweep and a
		// runs again with "fixed".
		want := []string{"a:raw", "b:raw", "a:fixed", "b:fixed"}
		if strings.Join(calls, ",") != strings.Join(want, ",") {
			t.Fatalf("invocations = %v, want %v", calls, want)
		}
		if res.Passes != 2 {
			t.Errorf("Passes = %d, want 2", res.Passes)
		}
	})

	t.Run("converges_on_second_sweep", func(t *testing.T) {
		var calls []string
		// a rewrites once, then passes so the second sweep converges.
		a := scripted("a", &calls,
			FilterResult{Outcome: FilterRewrite, Filter: "a", Output: "clean"},
			FilterResult{Outcome: FilterPass, Filter: "a"},
		)
		b := passFilter("b", &calls)
		chain := NewFilterChain([]OutputFilter{a, b}, 3)
		res := chain.Run(context.Background(), &task.TaskStep{}, "dirty")
		if res.Rejected != nil {
			t.Fatalf("unexpected rejection: %+v", res.Rejected)
		}
		if res.Output != "clean" {
			t.Errorf("Output = %q, want %q", res.Output, "clean")
		}
		if res.Passes != 2 {
			t.Errorf("Passes = %d, want 2 (rewrite sweep + converging sweep)", res.Passes)
		}
	})

	t.Run("ping_pong_hits_non_convergence", func(t *testing.T) {
		var calls []string
		// A appends "x", B strips it: the output changes on every sweep
		// forever.
		appender := &stubFilter{
			name:    "appender",
			applies: true,
			calls:   &calls,
			script: []func(string) FilterResult{func(o string) FilterResult {
				return FilterResult{Outcome: FilterRewrite, Filter: "appender", Output: o + "x"}
			}},
		}
		stripper := &stubFilter{
			name:    "stripper",
			applies: true,
			calls:   &calls,
			script: []func(string) FilterResult{func(o string) FilterResult {
				return FilterResult{Outcome: FilterRewrite, Filter: "stripper", Output: strings.TrimSuffix(o, "x")}
			}},
		}
		const maxPasses = 4
		chain := NewFilterChain([]OutputFilter{appender, stripper}, maxPasses)
		res := chain.Run(context.Background(), &task.TaskStep{}, "start")
		if res.Rejected == nil {
			t.Fatal("expected non-convergence rejection")
		}
		want := fmt.Sprintf("filter chain did not converge after %d passes", maxPasses)
		if res.Rejected.Reason != want {
			t.Errorf("Reason = %q, want %q", res.Rejected.Reason, want)
		}
		if res.Rejected.Outcome != FilterFail {
			t.Errorf("Outcome = %v, want FilterFail", res.Rejected.Outcome)
		}
		if res.Passes != maxPasses {
			t.Errorf("Passes = %d, want %d", res.Passes, maxPasses)
		}
		// Bounded invocations: exactly one applied filter action per sweep
		// is impossible here (both apply), but the total must be capped:
		// maxPasses sweeps x 2 filters = 8 invocations, never more.
		if len(calls) > maxPasses*2 {
			t.Errorf("invocations = %d, want <= %d (no infinite loop)", len(calls), maxPasses*2)
		}
	})

	t.Run("max_passes_default", func(t *testing.T) {
		for _, in := range []int{0, -1, -100} {
			chain := NewFilterChain(nil, in)
			if chain.config.MaxPasses != defaultFilterMaxPasses {
				t.Errorf("NewFilterChain(_, %d).MaxPasses = %d, want %d", in, chain.config.MaxPasses, defaultFilterMaxPasses)
			}
		}
	})

	t.Run("no_step_mutation", func(t *testing.T) {
		step := &task.TaskStep{ID: "s1", Result: "original"}
		mutator := scripted("mutator",
			nil,
			FilterResult{Outcome: FilterRewrite, Filter: "mutator", Output: "rewritten"},
		)
		chain := NewFilterChain([]OutputFilter{mutator}, 0)
		res := chain.Run(context.Background(), step, "original")
		if res.Output != "rewritten" {
			t.Errorf("Output = %q, want %q", res.Output, "rewritten")
		}
		if step.Result != "original" {
			t.Errorf("step.Result mutated: %q", step.Result)
		}
		if step.ID != "s1" {
			t.Errorf("step.ID mutated: %q", step.ID)
		}
	})
}

// --- Task 4: Actions log lines ------------------------------------------------

func TestFilterChainActions(t *testing.T) {
	var calls []string
	chain := NewFilterChain([]OutputFilter{
		scripted("first", &calls,
			FilterResult{Outcome: FilterRewrite, Filter: "first", Output: "v2"},
			FilterResult{Outcome: FilterPass, Filter: "first"},
		),
		skipFilter("never", &calls),
		scripted("second", &calls,
			FilterResult{Outcome: FilterFail, Filter: "second", Reason: "boom"},
		),
		// third must never run; include it to prove fail-fast ordering.
		passFilter("third", &calls),
	}, 3)
	res := chain.Run(context.Background(), &task.TaskStep{}, "v1")
	if res.Rejected == nil {
		t.Fatal("expected rejection")
	}
	want := []string{
		"pass=1 filter=first action=rewrite",
		"pass=2 filter=first action=pass",
		"pass=2 filter=second action=fail",
	}
	if strings.Join(res.Actions, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Actions = %v\nwant %v", res.Actions, want)
	}
	for _, line := range res.Actions {
		if !strings.HasPrefix(line, "pass=") || !strings.Contains(line, " filter=") || !strings.Contains(line, " action=") {
			t.Errorf("malformed Actions line: %q", line)
		}
	}
}

func TestFilterChainActionsPassOnly(t *testing.T) {
	var calls []string
	chain := NewFilterChain([]OutputFilter{passFilter("a", &calls), passFilter("b", &calls)}, 0)
	res := chain.Run(context.Background(), &task.TaskStep{}, "v1")
	want := []string{
		"pass=1 filter=a action=pass",
		"pass=1 filter=b action=pass",
	}
	if strings.Join(res.Actions, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Actions = %v\nwant %v", res.Actions, want)
	}
}
