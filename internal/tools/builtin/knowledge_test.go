package builtin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// fakeKnowledgeStore is an in-memory KnowledgeStore double for tool tests.
type fakeKnowledgeStore struct {
	pages    map[string]KnowledgePage
	searches []struct {
		query string
		limit int
	}
	ingested []KnowledgePage
	readID   string
	searchFn func(query string, limit int) ([]KnowledgeHit, error)
}

func newFakeKnowledgeStore() *fakeKnowledgeStore {
	return &fakeKnowledgeStore{pages: map[string]KnowledgePage{}}
}

func (f *fakeKnowledgeStore) Search(_ context.Context, query string, limit int) ([]KnowledgeHit, error) {
	f.searches = append(f.searches, struct {
		query string
		limit int
	}{query, limit})
	if f.searchFn != nil {
		return f.searchFn(query, limit)
	}
	return nil, nil
}

func (f *fakeKnowledgeStore) Read(_ context.Context, id string) (KnowledgePage, error) {
	f.readID = id
	p, ok := f.pages[id]
	if !ok {
		return KnowledgePage{}, errors.New("knowledge page not found")
	}
	return p, nil
}

func (f *fakeKnowledgeStore) Ingest(_ context.Context, page KnowledgePage) (string, error) {
	f.ingested = append(f.ingested, page)
	id := "pg-" + page.Title
	f.pages[id] = page
	return id, nil
}

// Compile-time: the fake must satisfy the tool-facing interface.
var _ KnowledgeStore = (*fakeKnowledgeStore)(nil)

func TestKnowledgeSearchTool_Metadata(t *testing.T) {
	tool := NewKnowledgeSearchTool(nil)
	if tool.Name() != "knowledge_search" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "knowledge_search")
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
	params := tool.Parameters()
	if params.Type != schemaTypeObject {
		t.Errorf("Parameters Type = %q, want %q", params.Type, schemaTypeObject)
	}
	for _, want := range []string{"query", "limit"} {
		if _, ok := params.Properties[want]; !ok {
			t.Errorf("Parameters missing %q property", want)
		}
	}
	if len(params.Required) != 1 || params.Required[0] != "query" {
		t.Errorf("Required = %v, want [query]", params.Required)
	}
	if !tool.IsReadOnly(nil) {
		t.Error("knowledge_search must be read-only")
	}
	if !tool.IsConcurrencySafe(nil) {
		t.Error("knowledge_search must be concurrency-safe")
	}
}

func TestKnowledgeSearchTool_Execute(t *testing.T) {
	store := newFakeKnowledgeStore()
	store.searchFn = func(query string, limit int) ([]KnowledgeHit, error) {
		return []KnowledgeHit{
			{PageID: "pg1", Title: "Docs", Snippet: "a doc", Score: 0.5},
		}, nil
	}
	tool := NewKnowledgeSearchTool(store)

	// Missing required query.
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("expected error for missing query")
	}

	// Defaults: limit clamps to [1,20], default 5.
	res, err := tool.Execute(context.Background(), map[string]any{"query": "docs"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(store.searches) != 1 || store.searches[0].limit != 5 {
		t.Errorf("default limit = %+v, want 5", store.searches)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("result type %T, want map[string]any", res)
	}
	hits, ok := m["hits"].([]map[string]any)
	if !ok || len(hits) != 1 {
		t.Fatalf("hits = %#v, want one hit", m["hits"])
	}
	if hits[0]["page_id"] != "pg1" {
		t.Errorf("page_id = %v, want pg1", hits[0]["page_id"])
	}

	// Limit cap at 20.
	_, _ = tool.Execute(context.Background(), map[string]any{"query": "docs", "limit": float64(500)})
	if store.searches[len(store.searches)-1].limit != 20 {
		t.Errorf("limit cap = %d, want 20", store.searches[len(store.searches)-1].limit)
	}

	// Store error propagates.
	store.searchFn = func(string, int) ([]KnowledgeHit, error) {
		return nil, errors.New("fts exploded")
	}
	if _, err := tool.Execute(context.Background(), map[string]any{"query": "x"}); err == nil || !strings.Contains(err.Error(), "fts exploded") {
		t.Errorf("store error not wrapped through: %v", err)
	}
}

func TestKnowledgeReadTool_Execute(t *testing.T) {
	store := newFakeKnowledgeStore()
	store.pages["pg1"] = KnowledgePage{
		ID: "pg1", Title: "Docs", Body: "body text",
		Links: []string{"pg2"}, Source: "user", UnresolvedLinks: []string{"Missing Page"},
	}
	tool := NewKnowledgeReadTool(store)

	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("expected error for missing page_id")
	}
	if _, err := tool.Execute(context.Background(), map[string]any{"page_id": "nope"}); err == nil {
		t.Error("expected error for unknown page_id")
	}

	res, err := tool.Execute(context.Background(), map[string]any{"page_id": "pg1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if store.readID != "pg1" {
		t.Errorf("Read id = %q, want pg1", store.readID)
	}
	m := res.(map[string]any)
	if m["title"] != "Docs" || m["body"] != "body text" {
		t.Errorf("page fields wrong: %#v", m)
	}
	links, _ := m["links"].([]string)
	if len(links) != 1 || links[0] != "pg2" {
		t.Errorf("links = %v, want [pg2]", links)
	}
	unresolved, _ := m["unresolved_links"].([]string)
	if len(unresolved) != 1 || unresolved[0] != "Missing Page" {
		t.Errorf("unresolved_links = %v, want [Missing Page]", unresolved)
	}
	if !tool.IsReadOnly(nil) {
		t.Error("knowledge_read must be read-only")
	}
}

func TestKnowledgeIngestTool_Execute(t *testing.T) {
	store := newFakeKnowledgeStore()
	tool := NewKnowledgeIngestTool(store)

	cases := []struct {
		name string
		args map[string]any
	}{
		{"missing title", map[string]any{"body": "b"}},
		{"missing body", map[string]any{"title": "t"}},
		{"empty title", map[string]any{"title": "", "body": "b"}},
	}
	for _, tc := range cases {
		if _, err := tool.Execute(context.Background(), tc.args); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}

	res, err := tool.Execute(context.Background(), map[string]any{
		"title": "Design Notes", "body": "Some notes", "source": "user",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m := res.(map[string]any)
	if m["success"] != true {
		t.Errorf("success = %v, want true", m["success"])
	}
	if m["page_id"] != "pg-Design Notes" {
		t.Errorf("page_id = %v", m["page_id"])
	}
	if len(store.ingested) != 1 || store.ingested[0].Source != "user" {
		t.Errorf("ingested page wrong: %#v", store.ingested)
	}
	if tool.IsReadOnly(nil) {
		t.Error("knowledge_ingest must not be read-only")
	}
}

// TestKnowledgeToolsSatisfyToolInterface pins the schema shape contract:
// every knowledge tool returns an object schema with required args declared.
func TestKnowledgeToolsSatisfyToolInterface(t *testing.T) {
	store := newFakeKnowledgeStore()
	tools := []struct {
		tool interface {
			Name() string
			Parameters() llm.FunctionParameters
		}
		required string
	}{
		{NewKnowledgeSearchTool(store), "query"},
		{NewKnowledgeReadTool(store), "page_id"},
		{NewKnowledgeIngestTool(store), "title"},
	}
	for _, tc := range tools {
		params := tc.tool.Parameters()
		if params.Type != schemaTypeObject {
			t.Errorf("%s: Type = %q, want object", tc.tool.Name(), params.Type)
		}
		found := false
		for _, r := range params.Required {
			if r == tc.required {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: required %q not declared", tc.tool.Name(), tc.required)
		}
	}
}
