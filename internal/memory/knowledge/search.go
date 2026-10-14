package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/caimlas/meept/pkg/sqlite"
	_ "modernc.org/sqlite" //nolint:revive // blank import for side effects
)

// Page is the frozen cross-tree contract (SHARED-CONVENTIONS §2.5).
type Page struct {
	ID         string
	Title      string
	Body       string
	Links      []string // page IDs
	Source     string
	IngestedAt time.Time

	// UnresolvedLinks holds wikilink titles whose target page does not exist
	// (yet). Stored and reported, never an error; not part of the frozen
	// struct shape above — additive field, ignored by contract consumers.
	UnresolvedLinks []string `json:"unresolved_links,omitempty"`
}

// Hit is one search result.
type Hit struct {
	PageID  string  `json:"page_id"`
	Title   string  `json:"title"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score"`
}

// maxPageResults caps any single search regardless of the requested limit
// (the tool layer defaults to 5 and caps at 20; this guards direct callers).
const maxPageResults = 100

// Search runs an FTS5 BM25 query over page titles and bodies, returning up to
// limit hits ranked by bm25 (best first). Each hit carries a snippet() window
// around the first match and a normalized relevance score in [0,1).
// Scores and hit counts are zero (not an error) when FTS5 is unavailable.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > maxPageResults {
		limit = maxPageResults
	}

	match := sqlite.SanitizeQuery(query)
	if match == "" {
		return nil, nil
	}

	rows, err := s.db.QueryxContext(ctx, fmt.Sprintf(`
SELECT p.id, p.title,
       snippet(knowledge_fts, 1, '', '', '…', 12) AS snip,
       %s AS rank
FROM knowledge_fts
JOIN knowledge_pages p ON p.rowid = knowledge_fts.rowid
WHERE knowledge_fts MATCH ?
ORDER BY rank
LIMIT ?`, sqlite.BM25("knowledge_fts")), match, limit)
	if err != nil {
		return nil, fmt.Errorf("knowledge search: %w", err)
	}
	defer rows.Close()

	var hits []Hit
	for rows.Next() {
		var h Hit
		var rawRank float64
		if err := rows.Scan(&h.PageID, &h.Title, &h.Snippet, &rawRank); err != nil {
			return nil, fmt.Errorf("scan knowledge hit: %w", err)
		}
		h.Score = sqlite.NormalizeRank(rawRank)
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate knowledge hits: %w", err)
	}
	return hits, nil
}

// Read returns a page by ID with its link graph resolved: every stored link
// whose target page exists becomes a page ID in Links; wikilinks stored
// unresolved are re-resolved at read time (a page ingested after its referrer
// heals the link). Links pointing at missing pages keep their title in
// UnresolvedLinks and are never an error.
func (s *Store) Read(ctx context.Context, id string) (Page, error) {
	if err := s.checkReady(); err != nil {
		return Page{}, err
	}

	var p Page
	var ingestedAt string
	err := s.db.QueryRowxContext(ctx, `
SELECT id, title, body, source, ingested_at FROM knowledge_pages WHERE id = ?`, id).
		Scan(&p.ID, &p.Title, &p.Body, &p.Source, &ingestedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Page{}, ErrNotFound
	}
	if err != nil {
		return Page{}, fmt.Errorf("read knowledge page: %w", err)
	}
	p.IngestedAt, err = time.Parse(time.RFC3339Nano, ingestedAt)
	if err != nil {
		return Page{}, fmt.Errorf("parse ingested_at %q: %w", ingestedAt, err)
	}

	// Resolve link rows at read time: to_id present and target page exists →
	// Links; empty to_id (or dangling to_id) → re-resolve by title, else keep
	// the title in UnresolvedLinks.
	rows, err := s.db.QueryxContext(ctx, `
SELECT l.to_id, l.unresolved_title
FROM knowledge_links l
WHERE l.from_id = ?`, id)
	if err != nil {
		return Page{}, fmt.Errorf("read knowledge links: %w", err)
	}
	type linkRow struct {
		ToID            string `db:"to_id"`
		UnresolvedTitle string `db:"unresolved_title"`
	}
	// Iterated explicitly with rows.Close (bughunt LOW): rows.StructScan does
	// NOT close rows on a scan error, and with the store's single pooled
	// connection that leak would wedge every later query on this handle.
	var linkRows []linkRow
	for rows.Next() {
		var lr linkRow
		if err := rows.StructScan(&lr); err != nil {
			_ = rows.Close()
			return Page{}, fmt.Errorf("scan knowledge links: %w", err)
		}
		linkRows = append(linkRows, lr)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return Page{}, fmt.Errorf("iterate knowledge links: %w", err)
	}
	_ = rows.Close()

	seen := map[string]bool{}
	for _, lr := range linkRows {
		target := lr.ToID
		if target != "" {
			// knowledge: URI link: resolved only when the target page exists
			// (at ingest or healed later); a phantom ID is dropped silently.
			if err := s.pageExists(ctx, target); err != nil {
				if errors.Is(err, errNoPage) {
					continue
				}
				return Page{}, fmt.Errorf("check link target %q: %w", target, err)
			}
		} else {
			// Unresolved wikilink: try to heal by title now.
			healed, err := s.resolveWikilink(ctx, lr.UnresolvedTitle)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return Page{}, fmt.Errorf("heal wikilink %q: %w", lr.UnresolvedTitle, err)
			}
			target = healed
			if target == "" {
				if !seen[lr.UnresolvedTitle] {
					seen[lr.UnresolvedTitle] = true
					p.UnresolvedLinks = append(p.UnresolvedLinks, lr.UnresolvedTitle)
				}
				continue
			}
		}
		if !seen[target] {
			seen[target] = true
			p.Links = append(p.Links, target)
		}
	}
	return p, nil
}

// ensure json import is used if the tool layer needs marshaling helpers here
// later; currently the Hit struct carries json tags only.
var _ = json.Marshal
