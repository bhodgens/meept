package validator

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

// pureFilter is a concurrency-safe rewrite filter with no per-run state:
// it converts the output to uppercase on the first sweep and passes on any
// later sweep (idempotent). Safe to share across concurrent chains.
type pureFilter struct {
	name string
}

func (f *pureFilter) Name() string { return f.name }

func (f *pureFilter) Applies(_ *task.TaskStep) bool { return true }

func (f *pureFilter) Process(_ context.Context, _ *task.TaskStep, output string) FilterResult {
	if strings.ContainsRune(output, 'z') {
		// Already rewritten: idempotent pass.
		return FilterResult{Outcome: FilterPass, Filter: f.name}
	}
	rewritten := fmt.Sprintf("%s-z", output)
	return FilterResult{Outcome: FilterRewrite, Filter: f.name, Output: rewritten}
}

// TestFilterChainRace runs the same shared chain and shared pure filters
// from 8 goroutines concurrently and asserts every result is identical:
// chain state never crosses calls.
func TestFilterChainRace(t *testing.T) {
	shared := []OutputFilter{&pureFilter{name: "pure1"}, &pureFilter{name: "pure2"}}
	const goroutines = 8
	const maxPasses = 3

	results := make([]ChainResult, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			chain := NewFilterChain(shared, maxPasses)
			results[idx] = chain.Run(context.Background(), &task.TaskStep{}, "payload")
		}(i)
	}
	wg.Wait()

	first := fmt.Sprintf("%+v|%v", results[0], results[0].Actions)
	for i := 1; i < goroutines; i++ {
		got := fmt.Sprintf("%+v|%v", results[i], results[i].Actions)
		if got != first {
			t.Fatalf("goroutine %d result differs:\n%s\n%s", i, first, got)
		}
	}

	want := []string{
		"pass=1 filter=pure1 action=rewrite",
		"pass=2 filter=pure1 action=pass",
		"pass=2 filter=pure2 action=pass",
	}
	r := results[0]
	if r.Rejected != nil {
		t.Fatalf("unexpected rejection: %+v", r.Rejected)
	}
	if r.Output != "payload-z" {
		t.Errorf("Output = %q, want %q", r.Output, "payload-z")
	}
	if strings.Join(r.Actions, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Actions = %v\nwant %v", r.Actions, want)
	}
	if r.Passes != 2 {
		t.Errorf("Passes = %d, want 2", r.Passes)
	}
}
