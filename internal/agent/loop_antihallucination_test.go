package agent

import (
	"testing"
)

// E2E run 7 (2026-09-11, rkl3Th) T1 regression: the quantized LFM2.5-8B
// produced a polished JSON report claiming "Created file hello.txt" with
// fabricated file_exists evidence — with ZERO tool executions in the turn
// (classification_method=heuristic_fallback run; no "Executing tool" lines
// in daemon.log). The step completed with the hallucination as its result.
// The anti-hallucination contract must detect that narration shape so the
// reasoningCycle can nudge for real tool use instead of shipping the fake
// success downstream.

func TestUnbackedSideEffectClaims_Run7Report(t *testing.T) {
	l := &AgentLoop{}
	// The hallucinated report shape (abbreviated): accomplished + evidence
	// arrays both narrate a completed write.
	report := `{
  "status": "completed",
  "accomplished": ["Created file hello.txt containing 'hello'"],
  "evidence": [
    {"type": "file_exists", "path": "hello.txt"}
  ]
}`
	if !l.unbackedSideEffectClaims(report) {
		t.Fatal("run-7 hallucinated report not detected")
	}
}

func TestUnbackedSideEffectClaims_PlainNarration(t *testing.T) {
	l := &AgentLoop{}
	cases := []string{
		"Created file hello.txt containing hello",
		"I created the file /tmp/x.txt",
		"I've updated the config",
		"Wrote the output to out.txt",
		"- deleted the stale cache file",
		"* saved results to disk",
	}
	for _, c := range cases {
		if !l.unbackedSideEffectClaims(c) {
			t.Errorf("claim not detected: %q", c)
		}
	}
}

func TestUnbackedSideEffectClaims_NonClaimsPassThrough(t *testing.T) {
	l := &AgentLoop{}
	cases := []string{
		"I will create the file now",             // plan, not claim
		"Should I write it?",                     // question
		"the file created by the previous step",  // past participle
		"hello",                                  // greeting
		"",                                       // empty
		"Here is what I found in the directory:", // read report
	}
	for _, c := range cases {
		if l.unbackedSideEffectClaims(c) {
			t.Errorf("non-claim matched as claim: %q", c)
		}
	}
}

func TestTurnExecutedFileTools_Ledger(t *testing.T) {
	l := &AgentLoop{}
	if l.turnExecutedFileTools() {
		t.Fatal("empty ledger must report no file tools")
	}
	l.mu.Lock()
	l.turnToolCalls = map[string]int{"file_write": 1}
	l.mu.Unlock()
	if !l.turnExecutedFileTools() {
		t.Fatal("file_write in ledger must report file tools")
	}
	l.mu.Lock()
	l.turnToolCalls = map[string]int{"web_search": 2}
	l.mu.Unlock()
	if l.turnExecutedFileTools() {
		t.Fatal("non-file tools must NOT satisfy the file-tool check")
	}
}

func TestResetTurnGuards_ClearsToolLedger(t *testing.T) {
	l := &AgentLoop{}
	l.mu.Lock()
	l.turnToolCalls = map[string]int{"file_write": 3}
	l.mu.Unlock()
	l.resetTurnGuards()
	l.mu.RLock()
	got := l.turnToolCalls
	l.mu.RUnlock()
	if len(got) != 0 {
		t.Fatalf("ledger survived turn reset: map=%v", got)
	}
}
