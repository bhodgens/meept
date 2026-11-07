package llm

import (
	"sync"
	"testing"

	appmetrics "github.com/caimlas/meept/internal/metrics"
)

// TestLLMCallObserver_SetReadRace pins bughunt 2026-11-07 infra-fixes F-3:
// the observer field is written by ProviderManager's fan-out (pm.mu) while
// every completed call's recordUsageStore goroutine reads it. The field is
// atomic.Pointer, so concurrent Set/Load must be race-clean under -race.
func TestLLMCallObserver_SetReadRace(t *testing.T) {
	c := &Client{}
	a := &AnthropicClient{}
	x := &CodexClient{}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(6)
		go func() { defer wg.Done(); c.SetLLMCallObserver(func(appmetrics.LLMCallRecord) {}) }()
		go func() { defer wg.Done(); a.SetLLMCallObserver(func(appmetrics.LLMCallRecord) {}) }()
		go func() { defer wg.Done(); x.SetLLMCallObserver(func(appmetrics.LLMCallRecord) {}) }()
		go func() { defer wg.Done(); _ = c.llmCallObserverFn() }()
		go func() { defer wg.Done(); _ = a.llmCallObserverFn() }()
		go func() { defer wg.Done(); _ = x.llmCallObserverFn(); _ = i }()
	}
	wg.Wait()
	if c.llmCallObserverFn() == nil || a.llmCallObserverFn() == nil || x.llmCallObserverFn() == nil {
		t.Fatal("observer lost under concurrent attach")
	}
}
