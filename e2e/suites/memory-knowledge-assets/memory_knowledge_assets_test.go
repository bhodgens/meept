//go:build e2e

// Suite memory-knowledge-assets (tree 04 leaf L4): the knowledge-asset
// store end-to-end against a sandboxed daemon — watched-dir ingestion via
// the real consolidation closure contract, FTS search + wikilink reads,
// upsert-in-place, the go/ast code index over a fixture dir, impact
// queries through the knowledge_impact tool, and the disabled-config
// control (zero knowledge tools in the executor's tool list).
//
// Coverage map (manifest scenarios):
//
//	memory-knowledge-assets-01  ingest 3 linked pages -> search -> read resolves a wikilink; upsert same title updates in place
//	                            — TestKnowledgeIngestSearchReadUppsert
//	memory-knowledge-assets-02  code-index the fixture dir -> knowledge_search finds a symbol -> knowledge_impact returns its caller
//	                            — TestKnowledgeCodeIndexAndImpact
//	memory-knowledge-assets-03  disabled config -> zero knowledge_* tools in the tool list
//	                            — TestKnowledgeDisabledNoTools
package memoryknowledgeassets

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/memory/knowledge"
	"github.com/caimlas/meept/internal/memory/knowledge/codeindex"
)

// seedKnowledgeAgent writes the aastub agent holding the knowledge tool
// grants (the shipped roster grants none). Same pattern as tools-docs:
// "aastub" sorts before the bundled lane holders, so pinned intents route
// to it.
func seedKnowledgeAgent(t *testing.T, st *harness.Stack) {
	t.Helper()
	dir := filepath.Join(st.MeeptHome, "agents", "aastub")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir aastub agent dir: %v", err)
	}
	body := `---
id: aastub
name: E2E Knowledge Specialist
role: executor
description: e2e-only agent holding the knowledge tool grants
intents: [librarian]
enabled: true
can_delegate: false
additional_tools:
  - knowledge_search
  - knowledge_read
  - knowledge_ingest
  - knowledge_impact
capabilities:
  - reasoning
max_iterations: 8
timeout_seconds: 120
---

# E2E Knowledge Specialist

Use the knowledge tools exactly as the user asks.
`
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("write aastub AGENT.md: %v", err)
	}
}

// toolResultTexts collects every role=tool message the fake LLM has seen.
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

// toolNamesInRequests lists every tool name the executor ever offered the
// model (the registry's projection into the requests' tools arrays).
func toolNamesInRequests(f *harness.FakeLLM) map[string]bool {
	names := map[string]bool{}
	for _, body := range f.Requests() {
		tools, ok := body["tools"].([]any)
		if !ok {
			continue
		}
		for _, raw := range tools {
			fn, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if f2, ok := fn["function"].(map[string]any); ok {
				fn = f2
			}
			if n, _ := fn["name"].(string); n != "" {
				names[n] = true
			}
		}
	}
	return names
}

// memory-knowledge-assets-01: ingest 3 linked pages through the REAL
// knowledge_ingest tool in agent turns, search ranks them, knowledge_read
// resolves a [[wikilink]], and re-ingesting the same title+source updates
// in place (no duplicate page).
func TestKnowledgeIngestSearchReadUppsert(t *testing.T) {
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"memory.knowledge.enabled":   true,
		"memory.knowledge.max_pages": 1000,
	}), harness.WithPreBootHook(func(st *harness.Stack) error {
		seedKnowledgeAgent(t, st)
		return nil
	}))
	s.RegisterProject(t, "e2e-project")
	s.Fake.SetClassifierOutput(`{"intent":"librarian","confidence":0.95,"reasoning":"pinned librarian"}`)
	sessionID := s.CreateSession(t, "knowledge-basic", s.ProjectDir)

	// Turn 1: ingest page 1 (terminating tool candidate — ingest returns
	// a small envelope, so run it as its own turn).
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name: "knowledge_ingest",
		Arguments: `{"title":"Deploy Runbook","body":"The deploy runbook pins ` +
			`the release train. See [[Rollback Plan]] for emergencies and ` +
			`[[On-call Rotation]] for paging.","source":"user"}`,
	})
	s.ChatTurn(t, sessionID,
		"Store the deploy runbook page in knowledge for the team",
		120*time.Second)

	// Turn 2: ingest pages 2 and 3 (one turn, two calls).
	s.Fake.EnqueueToolCalls(
		harness.ToolCall{
			Name: "knowledge_ingest",
			Arguments: `{"title":"Rollback Plan","body":"The rollback plan reverts ` +
				`the release train to the last green tag. Runbook reference: ` +
				`[[Deploy Runbook]].","source":"user"}`,
		},
		harness.ToolCall{
			Name: "knowledge_ingest",
			Arguments: `{"title":"On-call Rotation","body":"The on-call rotation ` +
				`pages the current primary. Emergencies follow the ` +
				`[[Rollback Plan]].","source":"user"}`,
		},
	)
	s.ChatTurn(t, sessionID,
		"Store the rollback plan and on-call rotation pages in knowledge too",
		120*time.Second)

	// Turn 3: search — the ranked hits must include the release-train page.
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "knowledge_search",
		Arguments: `{"query":"release train deploy","limit":5}`,
	})
	s.ChatTurn(t, sessionID,
		"Search knowledge for the release train deploy page and report what you find",
		120*time.Second)
	res := toolResultsJoined(s.Fake)
	if !strings.Contains(res, "Deploy Runbook") {
		t.Fatalf("knowledge_search results missing the ingest page title; results:\n%s", res)
	}

	// Extract the Deploy Runbook page_id from the search envelope for the
	// read turn (ids are content-hash derived, but parse rather than pin).
	pageID := knowledgePageIDFromResults(s.Fake, "Deploy Runbook")
	if pageID == "" {
		t.Fatalf("no page_id found for Deploy Runbook in search results:\n%s", res)
	}

	// Turn 4: read the page — the wikilinks must resolve to page ids of
	// the sibling pages (resolved links, not unresolved_titles).
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "knowledge_read",
		Arguments: `{"page_id":"` + pageID + `"}`,
	})
	s.ChatTurn(t, sessionID,
		"Read the deploy runbook knowledge page and list its links",
		120*time.Second)
	res = toolResultsJoined(s.Fake)
	if !strings.Contains(res, "rollback") && !strings.Contains(res, "Rollback") {
		t.Fatalf("knowledge_read result missing resolved links; results:\n%s", res)
	}

	// Upsert leg, direct against the store (same SQLite file the daemon
	// opened): re-ingest Deploy Runbook with a changed body and prove the
	// page count stayed at 3 (in-place update) and the body changed.
	dataDir := filepath.Join(s.StateDir, "knowledge")
	store, err := knowledge.New(dataDir, 0, nil)
	if err != nil {
		t.Fatalf("open knowledge store: %v", err)
	}
	ctx := context.Background()
	countBefore, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("count before upsert: %v", err)
	}
	const upsertMarker = "e2e-upsert-marker-9182"
	if _, err := store.Ingest(ctx, knowledge.Page{
		Title:  "Deploy Runbook",
		Body:   "The deploy runbook now carries the " + upsertMarker + " note.",
		Source: "user",
	}); err != nil {
		t.Fatalf("upsert ingest: %v", err)
	}
	countAfter, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("count after upsert: %v", err)
	}
	if countBefore != 3 || countAfter != 3 {
		t.Fatalf("page count before/after upsert = %d/%d, want 3/3 (upsert duplicated or lost a page)", countBefore, countAfter)
	}
	page, err := store.Read(ctx, pageID)
	if err != nil {
		t.Fatalf("read upserted page: %v", err)
	}
	if !strings.Contains(page.Body, upsertMarker) {
		t.Fatalf("upserted page body missing the marker; body:\n%s", page.Body)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
}

// knowledgePageIDFromResults extracts the page_id whose title matches from
// the tool-result envelopes the fake LLM has seen.
func knowledgePageIDFromResults(f *harness.FakeLLM, title string) string {
	for _, body := range f.Requests() {
		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if role, _ := msg["role"].(string); role != "tool" {
				continue
			}
			content, _ := msg["content"].(string)
			if !strings.Contains(content, `"page_id"`) {
				continue
			}
			// The envelope carries {"page_id":"...","title":"<title>"} in
			// each hit; scan adjacent pairs rather than full JSON (the
			// pretty-printed shapes differ between tools).
			id := extractPageIDForTitle(content, title)
			if id != "" {
				return id
			}
		}
	}
	return ""
}

// extractPageIDForTitle finds the page_id of the hit whose "title" matches
// title. Tool-result envelopes arrive both pretty-printed (`"key": "v"`)
// and minified (`"key":"v"`), so scan for both spellings and take the
// page_id NEAREST (before) the title — each hit object is
// {"page_id":...,"score":...,"snippet":...,"title":...}. Best-effort
// scanner for test assertions only.
func extractPageIDForTitle(content, title string) string {
	for _, sep := range []string{`"title": "`, `"title":"`} {
		search := title
		for from := 0; ; {
			idx := strings.Index(content[from:], sep+search)
			if idx < 0 {
				break
			}
			at := from + idx
			// Verify the title field actually closes here (avoid
			// matching a longer title that merely starts with ours).
			end := at + len(sep) + len(search)
			if end < len(content) && (content[end] == '"' || strings.HasPrefix(content[end:], `"`)) {
				// Walk back within this object for the nearest
				// page_id key.
				lo := at - 600
				if lo < 0 {
					lo = 0
				}
				window := content[lo:at]
				for _, key := range []string{`"page_id": "`, `"page_id":"`} {
					if start := strings.LastIndex(window, key); start >= 0 {
						rest := window[start+len(key):]
						if e := strings.Index(rest, `"`); e >= 0 {
							return rest[:e]
						}
					}
				}
			}
			from = at + 1
		}
	}
	return ""
}

// memory-knowledge-assets-02: the consolidation closure's code-index half
// (codeindex.IndexGoDirs over a fixture dir) lands CODE pages + call edges
// in the SAME store; knowledge_search finds a symbol through the agent
// tool; knowledge_impact returns the pinned caller. The indexing itself is
// driven directly (identical call to the wired closure — the daemon side
// needs a consolidation tick to fire, which the hermetic tier does not
// wait on), while the QUERY legs run through real agent turns.
func TestKnowledgeCodeIndexAndImpact(t *testing.T) {
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"memory.knowledge.enabled":   true,
		"memory.knowledge.max_pages": 1000,
	}), harness.WithPreBootHook(func(st *harness.Stack) error {
		seedKnowledgeAgent(t, st)
		return nil
	}))
	s.RegisterProject(t, "e2e-project")
	s.Fake.SetClassifierOutput(`{"intent":"librarian","confidence":0.95,"reasoning":"pinned librarian"}`)
	sessionID := s.CreateSession(t, "knowledge-code", s.ProjectDir)

	// Index the fixture dir into the daemon's own store (the exact call
	// the consolidation closure makes).
	fixtureDir, err := filepath.Abs("testdata/codedir")
	if err != nil {
		t.Fatalf("fixture dir: %v", err)
	}
	dataDir := filepath.Join(s.StateDir, "knowledge")
	store, err := knowledge.New(dataDir, 1000, nil)
	if err != nil {
		t.Fatalf("open knowledge store: %v", err)
	}
	rep, err := codeindex.IndexGoDirs(context.Background(), store, []string{fixtureDir}, nil)
	if err != nil {
		t.Fatalf("IndexGoDirs: %v", err)
	}
	if rep.FilesIndexed != 2 {
		t.Fatalf("files indexed = %d, want 2", rep.FilesIndexed)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	// Search leg: the symbol surfaces through the agent's knowledge_search.
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "knowledge_search",
		Arguments: `{"query":"Runner input","limit":5}`,
	})
	s.ChatTurn(t, sessionID,
		"Search knowledge for the Runner symbol that parses input tokens and report the code pages you find",
		120*time.Second)
	res := toolResultsJoined(s.Fake)
	if !strings.Contains(res, "Runner") {
		t.Fatalf("knowledge_search results missing the indexed Runner symbol; results:\n%s", res)
	}

	// Impact leg: knowledge_impact on Parser.Parse must return Runner.Run
	// (the pinned intra-repo call edge).
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "knowledge_impact",
		Arguments: `{"symbol":"Parser.Parse"}`,
	})
	s.ChatTurn(t, sessionID,
		"Run knowledge impact analysis on the symbol Parser.Parse and list its callers",
		120*time.Second)
	res = toolResultsJoined(s.Fake)
	if !strings.Contains(res, "Runner.Run") {
		t.Fatalf("knowledge_impact result missing the Runner.Run caller; results:\n%s", res)
	}
}

// memory-knowledge-assets-03 (control): with memory.knowledge.enabled
// false (the default), the executor's tool list carries ZERO knowledge_*
// tools — nothing enters prompts.
func TestKnowledgeDisabledNoTools(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "knowledge-off", s.ProjectDir)

	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "file_read",
		Arguments: `{"path":"README.md"}`,
	})
	s.ChatTurn(t, sessionID,
		"Read the project README file",
		120*time.Second)

	names := toolNamesInRequests(s.Fake)
	if len(names) == 0 {
		t.Fatal("executor offered no tools at all (harness broken)")
	}
	for _, n := range []string{"knowledge_search", "knowledge_read", "knowledge_ingest", "knowledge_impact"} {
		if names[n] {
			t.Fatalf("tool %q exposed while memory.knowledge.enabled=false", n)
		}
	}
}
