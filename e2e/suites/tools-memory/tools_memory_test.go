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
	"os"
	"path/filepath"
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

// tools-memory-02: memory_vote applies a delta. STILL BLOCKED on an
// internal-seam: memory_vote IS registered by the daemon, but it has no
// internal/agent ToolActionMap entry, so Executor.checkPermission falls back
// to the tool NAME as the permission action and pkg/security.BuiltinRules
// denies it ("Unknown action: memory_vote") before RecordVote runs. Adding
// the grant via the roster seeding below is not sufficient — the deny happens
// before the tool executes.
func TestMemoryVoteAppliesDelta(t *testing.T) {
	t.Skip("blocked: memory_vote lacks an internal/agent ToolActionMap entry, so " +
		"Executor.checkPermission falls back to the tool name as the action and " +
		"pkg/security.BuiltinRules denies it (`Unknown action: memory_vote`) before " +
		"the tool runs. Roster grants alone cannot cover it — needs the action mapping.")
}

// seedCurationAgent writes a user-tier agent ("aastub", alphabetically first
// holder of the librarian lane) with the curation + remember grants, so the
// filtered per-agent registry exposes them to real turns.
func seedCurationAgent(t *testing.T, st *harness.Stack) {
	t.Helper()
	dir := filepath.Join(st.MeeptHome, "agents", "aastub")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir aastub agent dir: %v", err)
	}
	body := `---
id: aastub
name: E2E Curator
role: executor
description: e2e-only agent holding the curation tool grants
intents: [librarian]
enabled: true
can_delegate: false
additional_tools:
  - memory_retain
  - memory_recall
  - remember
  - memory_search
  - memory_store
capabilities:
  - reasoning
max_iterations: 8
timeout_seconds: 120
---

# E2E Curator

Follow the user's request briefly.
`
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write aastub AGENT.md: %v", err)
	}
}

// tools-memory-03: the registered curation pair memory_retain/memory_recall
// persists across turns — retain queues a hindsight fact, recall surfaces it
// verbatim in a later turn's tool envelope.
func TestMemoryRetainRecallPairPersists(t *testing.T) {
	s := harness.Start(t, harness.WithPreBootHook(func(st *harness.Stack) error {
		seedCurationAgent(t, st)
		return nil
	}))
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "mem-curation", s.ProjectDir)
	s.Fake.SetClassifierOutput(`{"intent":"librarian","confidence":0.95,"reasoning":"pinned librarian"}`)

	const fact = "the hindsight bank marker is quokka-lantern-4419"

	// Turn 1: retain the fact.
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "memory_retain",
		Arguments: `{"content":"` + fact + `","domain":"e2e","importance":"high"}`,
	})
	s.ChatTurn(t, sessionID,
		"Curate the knowledge bank: retain that "+fact+" for later recall",
		120*time.Second)

	res := toolResultsJoined(s.Fake)
	if !strings.Contains(res, `"success":true`) || !strings.Contains(res, "hindsight bank") {
		t.Fatalf("memory_retain result missing the success envelope:\n%s", res)
	}

	// Turn 2: recall it.
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "memory_recall",
		Arguments: `{"query":"hindsight bank marker","limit":10}`,
	})
	s.ChatTurn(t, sessionID,
		"Curate the knowledge bank: recall the hindsight bank marker fact",
		120*time.Second)

	res = toolResultsJoined(s.Fake)
	if !strings.Contains(res, fact) {
		t.Fatalf("memory_recall result missing the retained fact %q; results:\n%s", fact, res)
	}
}

// tools-memory-04: remember queues a proposal without applying a change —
// the queue file (.meept/improvements.md, daemon-cwd relative) is the
// observable end state.
func TestRememberQueuesProposalOnly(t *testing.T) {
	s := harness.Start(t, harness.WithPreBootHook(func(st *harness.Stack) error {
		seedCurationAgent(t, st)
		return nil
	}))
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "mem-remember", s.ProjectDir)
	s.Fake.SetClassifierOutput(`{"intent":"librarian","confidence":0.95,"reasoning":"pinned librarian"}`)

	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name: "remember",
		Arguments: `{"target":"config/agents/coder/AGENT.md","change":"add a section on caching planner prompts",` +
			`"justification":"cuts turn latency","proposal_type":"agent_prompt"}`,
	})
	s.ChatTurn(t, sessionID,
		"Remember an improvement for the coder agent prompt and queue it",
		120*time.Second)

	// The remember tool binds ".meept/improvements.md" relative to the
	// daemon's CWD — the sandbox work root.
	queue := filepath.Join(s.Work, ".meept", "improvements.md")
	data, err := os.ReadFile(queue)
	if err != nil {
		t.Fatalf("improvements queue not created at %s: %v\ntool results:\n%s\ndaemon log tail:\n%s",
			queue, err, toolResultsJoined(s.Fake), s.Daemon.LogTail())
	}
	if !strings.Contains(string(data), "add a section on caching planner prompts") {
		t.Fatalf("queue file missing the remembered improvement:\n%s", data)
	}
}
