package builtin

import (
	"context"
	"fmt"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// KnowledgeStore is the narrow interface the knowledge_* tools depend on.
// Defined HERE (internal/tools/builtin) so the tools package never imports
// internal/memory — the concrete store satisfies it structurally via the
// adapter in internal/memory/knowledge/adapter.go (same seam style as the
// memory.VectorSearcher adapter).
type KnowledgeStore interface {
	// Search runs a full-text query over stored pages, returning up to
	// limit hits ranked by relevance.
	Search(ctx context.Context, query string, limit int) ([]KnowledgeHit, error)
	// Read returns a page by ID.
	Read(ctx context.Context, id string) (KnowledgePage, error)
	// Ingest stores (upsert) a page and returns its deterministic ID.
	Ingest(ctx context.Context, page KnowledgePage) (string, error)
}

// KnowledgeHit mirrors knowledge.Hit without dragging its package into the
// tools import graph.
type KnowledgeHit struct {
	PageID  string
	Title   string
	Snippet string
	Score   float64
}

// KnowledgePage mirrors knowledge.Page for the same reason. Links are page
// IDs; UnresolvedLinks carries wikilink titles whose target page does not
// exist (additive, never an error).
type KnowledgePage struct {
	ID              string
	Title           string
	Body            string
	Links           []string
	Source          string
	UnresolvedLinks []string
}

// KnowledgeSearchTool is the FTS retrieval tool over the knowledge-asset
// store ([memory.knowledge], opt-in). Wraps KnowledgeStore.Search.
type KnowledgeSearchTool struct {
	tools.ToolDefaults
	store KnowledgeStore
}

// NewKnowledgeSearchTool constructs the tool bound to the given store.
func NewKnowledgeSearchTool(store KnowledgeStore) *KnowledgeSearchTool {
	return &KnowledgeSearchTool{store: store}
}

func (t *KnowledgeSearchTool) Name() string     { return "knowledge_search" }
func (t *KnowledgeSearchTool) Category() string { return "memory" }

func (t *KnowledgeSearchTool) Description() string {
	return "Full-text search over the knowledge-asset store (ingested documents " +
		"and notes). Returns page IDs, titles, and snippets ranked by relevance. " +
		"Pair with knowledge_read to fetch a full page."
}

func (t *KnowledgeSearchTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"query": {
				Type:        schemaTypeString,
				Description: "The search query (full-text).",
			},
			"limit": {
				Type:        schemaTypeInteger,
				Description: "Max hits to return (default 5, cap 20). Optional.",
			},
		},
		Required: []string{"query"},
	}
}

func (t *KnowledgeSearchTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.store == nil {
		return nil, fmt.Errorf("knowledge store not configured")
	}
	query := asStringArg(args["query"])
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	limit := 5
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}
	if limit > 20 {
		limit = 20
	}
	hits, err := t.store.Search(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("knowledge search: %w", err)
	}
	results := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		results = append(results, map[string]any{
			"page_id": h.PageID,
			"title":   h.Title,
			"snippet": h.Snippet,
			"score":   h.Score,
		})
	}
	return map[string]any{"success": true, "hits": results}, nil
}

func (t *KnowledgeSearchTool) IsReadOnly(map[string]any) bool        { return true }
func (t *KnowledgeSearchTool) IsConcurrencySafe(map[string]any) bool { return true }

// KnowledgeReadTool returns a full knowledge page by ID, including its
// resolved link graph. Wraps KnowledgeStore.Read.
type KnowledgeReadTool struct {
	tools.ToolDefaults
	store KnowledgeStore
}

// NewKnowledgeReadTool constructs the tool bound to the given store.
func NewKnowledgeReadTool(store KnowledgeStore) *KnowledgeReadTool {
	return &KnowledgeReadTool{store: store}
}

func (t *KnowledgeReadTool) Name() string     { return "knowledge_read" }
func (t *KnowledgeReadTool) Category() string { return "memory" }

func (t *KnowledgeReadTool) Description() string {
	return "Read a full knowledge page by ID. Returns the title, body, source, " +
		"and link graph (resolved page-ID links plus any wikilinks whose target " +
		"page does not exist yet). Use knowledge_search to find page IDs."
}

func (t *KnowledgeReadTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"page_id": {
				Type:        schemaTypeString,
				Description: "The knowledge page ID (from knowledge_search).",
			},
		},
		Required: []string{"page_id"},
	}
}

func (t *KnowledgeReadTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.store == nil {
		return nil, fmt.Errorf("knowledge store not configured")
	}
	id := asStringArg(args["page_id"])
	if id == "" {
		return nil, fmt.Errorf("page_id is required")
	}
	page, err := t.store.Read(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("knowledge read: %w", err)
	}
	return map[string]any{
		"success":          true,
		"page_id":          page.ID,
		"title":            page.Title,
		"body":             page.Body,
		"links":            page.Links,
		"source":           page.Source,
		"unresolved_links": page.UnresolvedLinks,
	}, nil
}

func (t *KnowledgeReadTool) IsReadOnly(map[string]any) bool        { return true }
func (t *KnowledgeReadTool) IsConcurrencySafe(map[string]any) bool { return true }

// KnowledgeIngestTool stores (upserts) a page into the knowledge store.
// Identity is the hash of title+source, so re-ingesting the same pair
// replaces the body. Wraps KnowledgeStore.Ingest.
type KnowledgeIngestTool struct {
	tools.ToolDefaults
	store KnowledgeStore
}

// NewKnowledgeIngestTool constructs the tool bound to the given store.
func NewKnowledgeIngestTool(store KnowledgeStore) *KnowledgeIngestTool {
	return &KnowledgeIngestTool{store: store}
}

func (t *KnowledgeIngestTool) Name() string     { return "knowledge_ingest" }
func (t *KnowledgeIngestTool) Category() string { return "memory" }

func (t *KnowledgeIngestTool) Description() string {
	return "Store a document into the knowledge-asset store for later full-text " +
		"retrieval. The title+source pair identifies the page: re-ingesting the " +
		"same pair replaces the body. Markdown links and [[wikilinks]] become " +
		"part of the page link graph."
}

func (t *KnowledgeIngestTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"title": {
				Type:        schemaTypeString,
				Description: "The page title (part of its identity).",
			},
			"body": {
				Type:        schemaTypeString,
				Description: "The page content (markdown).",
			},
			"source": {
				Type:        schemaTypeString,
				Description: "Provenance label (part of the identity, e.g. a URL or 'user'). Optional; empty = direct ingest.",
			},
		},
		Required: []string{"title", "body"},
	}
}

func (t *KnowledgeIngestTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.store == nil {
		return nil, fmt.Errorf("knowledge store not configured")
	}
	title := asStringArg(args["title"])
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	body := asStringArg(args["body"])
	if body == "" {
		return nil, fmt.Errorf("body is required")
	}
	page := KnowledgePage{
		Title:  title,
		Body:   body,
		Source: asStringArg(args["source"]),
	}
	id, err := t.store.Ingest(ctx, page)
	if err != nil {
		return nil, fmt.Errorf("knowledge ingest: %w", err)
	}
	return map[string]any{
		"success": true,
		"page_id": id,
	}, nil
}

// Ensure the knowledge tools satisfy the Tool interface.
var (
	_ tools.Tool = (*KnowledgeSearchTool)(nil)
	_ tools.Tool = (*KnowledgeReadTool)(nil)
	_ tools.Tool = (*KnowledgeIngestTool)(nil)
)
