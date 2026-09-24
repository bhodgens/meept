//go:build e2e

// Package toolsmemory drives the memory tool family through REAL agent turns
// and asserts observable end state: verbatim retrieval in the tool-result
// envelopes the model sees on the follow-up request.
//
// Turn-splitting note: memory_store is a TerminatingTool (its confirmation
// ends the loop with the tool result as the reply), so store-then-search is
// asserted across TWO turns — turn 1 stores, turn 2 searches and the
// follow-up completion request carries the search result envelope.
//
// Coverage map (manifest scenarios):
//
//	tools-memory-01  memory_store -> memory_search verbatim   — TestMemoryStoreThenSearchRetrievesVerbatim
//	tools-memory-02  memory_vote delta                        — deferred (t.Skip; not granted to any roster agent)
//	tools-memory-03  retain/recall curation pair              — deferred (t.Skip; not granted/registered)
//	tools-memory-04  remember queues a proposal               — deferred (t.Skip; not granted to any roster agent)
package toolsmemory

import (
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// toolResultTexts collects the content of every role=tool message the daemon
// has ever sent back to the fake LLM.
func toolResultTexts(f *harness.FakeLLM) []string {
	var out []string
	for _, body := range f.Requests() {
		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if role, _ := msg["role"].(string); role == "tool" {
				if c, _ := msg["content"].(string); c != "" {
					out = append(out, c)
				}
			}
		}
	}
	return out
}

func toolResultsJoined(f *harness.FakeLLM) string {
	return strings.Join(toolResultTexts(f), "\n")
}

// tools-memory-01: memory_store in turn 1, memory_search in turn 2 — the
// search result must carry the stored content verbatim.
func TestMemoryStoreThenSearchRetrievesVerbatim(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "mem-basic", s.ProjectDir)

	const codeword = "xylophone-delta-7741"

	// Turn 1: store (terminating tool; the store itself is the observable of
	// this turn, verified by turn 2's retrieval).
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name: "memory_store",
		Arguments: `{"content":"the release codeword is ` + codeword + `","type":"task",` +
			`"category":"general"}`,
	})
	s.ChatTurn(t, sessionID,
		"Create a short report for the release checklist: store in memory that the release codeword is "+codeword,
		120*time.Second)

	// Turn 2: search (non-terminating; the follow-up request carries the
	// search result envelope).
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "memory_search",
		Arguments: `{"query":"codeword","limit":10,"min_relevance":0}`,
	})
	s.ChatTurn(t, sessionID,
		"Create a summary file of the release codeword: search memory for the codeword first",
		120*time.Second)

	res := toolResultsJoined(s.Fake)
	if !strings.Contains(res, `"count":1`) || !strings.Contains(res, `"results"`) {
		t.Fatalf("memory_search result missing the results payload:\n%s", res)
	}
	if !strings.Contains(res, codeword) {
		t.Fatalf("memory_search result missing the stored codeword %q; results:\n%s", codeword, res)
	}
}

// Baseline-granted get-context leg (extends memory-01): memory_get_context
// must surface the stored memory for a topic query.
func TestMemoryGetContextSurfacesStoredMemory(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "mem-ctx", s.ProjectDir)

	const marker = "kumquat-arcade-3307"

	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "memory_store",
		Arguments: `{"content":"the workshop password is ` + marker + `","type":"task","category":"general"}`,
	})
	s.ChatTurn(t, sessionID,
		"Create a note for the workshop checklist: store in memory that the workshop password is "+marker,
		120*time.Second)

	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "memory_get_context",
		Arguments: `{"query":"workshop password","max_items":10}`,
	})
	s.ChatTurn(t, sessionID,
		"Create a summary file about access: get context for the workshop password topic first",
		120*time.Second)

	res := toolResultsJoined(s.Fake)
	if !strings.Contains(res, `"context"`) {
		t.Fatalf("memory_get_context result missing the context payload:\n%s", res)
	}
	if !strings.Contains(res, marker) {
		t.Fatalf("memory_get_context result missing the stored marker %q; results:\n%s", marker, res)
	}
}

// tools-memory-02: memory_vote applies a delta. DEFERRED: memory_vote has no
// grant in any roster agent and no executor ToolActionMap entry, so a real
// turn denies it before the vote store is touched.
func TestMemoryVoteAppliesDelta(t *testing.T) {
	t.Skip("deferred: memory_vote is not granted to any roster agent (config/agents) " +
		"and lacks an executor ToolActionMap entry; a real turn denies the call with " +
		"`Unknown action: memory_vote` before RecordVote runs. Requires a roster grant " +
		"+ action mapping to cover e2e.")
}

// tools-memory-03: retain/recall curation pair persists. DEFERRED: the wired
// tools memory_retain/memory_recall carry no roster grant, and the un-wired
// retain/recall tools (memory_curation.go) are not registered by the daemon.
func TestMemoryRetainRecallPairPersists(t *testing.T) {
	t.Skip("deferred: memory_retain/memory_recall carry no roster grant, and the " +
		"memory_curation.go retain/recall tools are not registered by the daemon " +
		"(components.go registers only MemoryRetainTool/MemoryRecallTool). " +
		"Requires a roster grant + registration to cover e2e.")
}

// tools-memory-04: remember queues a proposal without applying it. DEFERRED:
// the remember tool has no roster grant, so the per-agent filtered registry
// answers a scripted call with `unknown tool: remember`.
func TestRememberQueuesProposalOnly(t *testing.T) {
	t.Skip("deferred: remember is not granted to any roster agent; the filtered " +
		"registry turns the call into `unknown tool: remember`. The queue file " +
		"(.meept/improvements.md, daemon-cwd relative) is otherwise assertable — " +
		"requires a roster grant to cover e2e.")
}
