package knowledge

import (
	"context"

	"github.com/caimlas/meept/internal/tools/builtin"
)

// adapter adapts *Store to the narrow builtin.KnowledgeStore interface.
// The interface is defined in internal/tools/builtin so the tools package
// never imports internal/memory; this adapter satisfies it structurally
// (same seam style as the memory.VectorSearcher adapter). Wiring code
// (leaf T04-L4) passes NewToolStore(s) wherever a builtin.KnowledgeStore
// is needed. Compile-time proof of method-set compatibility lives in
// adapter_test.go.
type adapter struct {
	s *Store
}

// Search delegates to Store.Search, converting Hit rows.
func (a adapter) Search(ctx context.Context, query string, limit int) ([]builtin.KnowledgeHit, error) {
	hits, err := a.s.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]builtin.KnowledgeHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, builtin.KnowledgeHit{
			PageID:  h.PageID,
			Title:   h.Title,
			Snippet: h.Snippet,
			Score:   h.Score,
		})
	}
	return out, nil
}

// Read delegates to Store.Read, converting the Page (the additive
// UnresolvedLinks field is carried through; IngestedAt is dropped — the
// tool surface never needs it).
func (a adapter) Read(ctx context.Context, id string) (builtin.KnowledgePage, error) {
	p, err := a.s.Read(ctx, id)
	if err != nil {
		return builtin.KnowledgePage{}, err
	}
	return builtin.KnowledgePage{
		ID:              p.ID,
		Title:           p.Title,
		Body:            p.Body,
		Links:           p.Links,
		Source:          p.Source,
		UnresolvedLinks: p.UnresolvedLinks,
	}, nil
}

// Ingest delegates to Store.Ingest.
func (a adapter) Ingest(ctx context.Context, page builtin.KnowledgePage) (string, error) {
	return a.s.Ingest(ctx, Page{
		Title:  page.Title,
		Body:   page.Body,
		Source: page.Source,
	})
}

// NewToolStore returns the store as a builtin.KnowledgeStore.
func NewToolStore(s *Store) builtin.KnowledgeStore {
	return adapter{s: s}
}
