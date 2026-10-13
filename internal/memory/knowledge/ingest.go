package knowledge

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// knowledgeURIPrefix marks an explicit inter-page link in markdown bodies:
// [text](knowledge:<page_id>).
const knowledgeURIPrefix = "knowledge:"

// markdownLinkRe matches [text](target) markdown links.
var markdownLinkRe = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)\)`)

// wikilinkRe matches [[Page Title]] wikilinks.
var wikilinkRe = regexp.MustCompile(`\[\[([^\[\]]+)\]\]`)

// contentHash is the page identity: sha256 of title+source, first 16 hex
// chars. Same title+source always maps to the same ID (upsert semantics).
func contentHash(title, source string) string {
	sum := sha256.Sum256([]byte(title + "\x00" + source))
	return hex.EncodeToString(sum[:])[:16]
}

// PageID returns the deterministic page ID for a title+source pair. Exported
// so tool wiring (leaf L2) can predict IDs without ingesting.
func PageID(title, source string) string {
	return contentHash(title, source)
}

// extractLinks pulls the link targets out of a markdown body. Two forms:
//   - markdown URIs: [text](knowledge:<page_id>) → to_id set
//   - wikilinks:    [[Page Title]]              → unresolved until a page
//     with that title exists (resolved at read time; never an error)
func extractLinks(body string) (toIDs []string, unresolvedTitles []string) {
	seen := map[string]bool{}

	for _, m := range markdownLinkRe.FindAllStringSubmatch(body, -1) {
		target := m[1]
		if !strings.HasPrefix(target, knowledgeURIPrefix) {
			continue
		}
		id := strings.TrimPrefix(target, knowledgeURIPrefix)
		id = strings.TrimSpace(id)
		if id != "" && !seen["id:"+id] {
			seen["id:"+id] = true
			toIDs = append(toIDs, id)
		}
	}

	for _, m := range wikilinkRe.FindAllStringSubmatch(body, -1) {
		title := strings.TrimSpace(m[1])
		if title == "" || seen["title:"+title] {
			continue
		}
		seen["title:"+title] = true
		unresolvedTitles = append(unresolvedTitles, title)
	}

	return toIDs, unresolvedTitles
}

// resolveWikilink looks up a page ID by exact title. Empty string means
// unresolvable (kept unresolved, not an error).
func (s *Store) resolveWikilink(ctx context.Context, title string) (string, error) {
	var id string
	err := s.db.QueryRowxContext(ctx,
		`SELECT id FROM knowledge_pages WHERE title = ? LIMIT 1`, title).Scan(&id)
	if err != nil {
		return "", err // includes sql.ErrNoRows → caller treats as unresolved
	}
	return id, nil
}

// Ingest stores a page with upsert semantics. The page ID is the content hash
// of title+source; re-ingesting the same title+source replaces body, links,
// and ingested_at. After the write, max_pages eviction removes the oldest
// ingested pages (the just-written page is never evicted).
func (s *Store) Ingest(ctx context.Context, page Page) (string, error) {
	if err := s.checkReady(); err != nil {
		return "", err
	}
	if strings.TrimSpace(page.Title) == "" {
		return "", fmt.Errorf("knowledge page title must not be empty")
	}

	id := contentHash(page.Title, page.Source)
	ingestedAt := page.IngestedAt
	if ingestedAt.IsZero() {
		ingestedAt = time.Now().UTC()
	}

	err := s.withTx(ctx, func(tx txExec) error {
		// Upsert on (title, source) natural key; ID is derived from it, so
		// ON CONFLICT(id) and ON CONFLICT(title, source) coincide.
		if _, err := tx.ExecContext(ctx, `
INSERT INTO knowledge_pages (id, title, body, source, ingested_at, content_hash)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    title       = excluded.title,
    body        = excluded.body,
    source      = excluded.source,
    ingested_at = excluded.ingested_at,
    content_hash = excluded.content_hash
`, id, page.Title, page.Body, page.Source, ingestedAt.UTC().Format(time.RFC3339Nano), id); err != nil {
			return fmt.Errorf("upsert knowledge page: %w", err)
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_links WHERE from_id = ?`, id); err != nil {
			return fmt.Errorf("clear old links: %w", err)
		}
		return s.insertLinkRows(ctx, tx, id, page.Body)
	})
	if err != nil {
		return "", err
	}

	s.evictOldest(ctx, id)
	return id, nil
}

// insertLinkRows extracts links from body and inserts their rows inside the
// caller's transaction. Wikilinks resolve to page IDs when a page with that
// exact title exists at ingest time; otherwise stored unresolved (never an
// error — dangling links are tolerated).
func (s *Store) insertLinkRows(ctx context.Context, tx txExec, fromID, body string) error {
	toIDs, titles := extractLinks(body)

	for _, toID := range toIDs {
		// A knowledge: URI targeting a page that does not exist (yet) is
		// stored unresolved by ID so it can heal at read time — dangling
		// links are never an error.
		existsErr := s.pageExistsTx(ctx, tx, toID)
		if existsErr != nil && !errors.Is(existsErr, errNoPage) {
			return fmt.Errorf("check link target %q: %w", toID, existsErr)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO knowledge_links (from_id, to_id, unresolved_title) VALUES (?, ?, '')`,
			fromID, toID); err != nil {
			return fmt.Errorf("insert knowledge link: %w", err)
		}
	}
	for _, title := range titles {
		resolved, err := s.resolveWikilinkTx(ctx, tx, title)
		if err != nil {
			return err
		}
		if resolved != "" {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO knowledge_links (from_id, to_id, unresolved_title) VALUES (?, ?, '')`,
				fromID, resolved); err != nil {
				return fmt.Errorf("insert knowledge link: %w", err)
			}
		} else {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO knowledge_links (from_id, to_id, unresolved_title) VALUES (?, '', ?)`,
				fromID, title); err != nil {
				return fmt.Errorf("insert unresolved knowledge link: %w", err)
			}
		}
	}
	return nil
}

// errNoPage marks "link target page does not exist (yet)" from pageExistsTx.
var errNoPage = errors.New("knowledge link target page not found")

// pageExists is pageExistsTx against the store's connection (read paths).
func (s *Store) pageExists(ctx context.Context, id string) error {
	return s.pageExistsTx(ctx, s.db, id)
}

// pageExistsTx reports errNoPage when no page with the given ID exists.
func (s *Store) pageExistsTx(ctx context.Context, tx txExec, id string) error {
	var one int
	err := tx.QueryRowContext(ctx,
		`SELECT 1 FROM knowledge_pages WHERE id = ? LIMIT 1`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return errNoPage
	}
	return err
}

// resolveWikilinkTx is resolveWikilink inside a transaction. No matching
// title → ("", nil): unresolved is a normal state, not an error.
func (s *Store) resolveWikilinkTx(ctx context.Context, tx txExec, title string) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM knowledge_pages WHERE title = ? LIMIT 1`, title).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("resolve wikilink %q: %w", title, err)
	}
	return id, nil
}

// evictOldest enforces max_pages by deleting the oldest-ingested pages beyond
// the cap (the just-written page is exempt for this round). Failures are
// logged, never returned: eviction is enforcement, not a correctness
// requirement of the ingest itself.
func (s *Store) evictOldest(ctx context.Context, keepID string) {
	if s.maxPages <= 0 {
		return
	}
	// Survivors are the newest maxPages rows (the just-written page is
	// exempt for this round); everything beyond the cap is a victim
	// (oldest-evicted-first policy).
	rows, err := s.db.QueryContext(ctx, `
SELECT id, title FROM knowledge_pages
ORDER BY ingested_at DESC, id DESC
LIMIT -1 OFFSET ?`,
		s.maxPages)
	if err != nil {
		s.logger.Warn("knowledge eviction query failed", "error", err)
		return
	}
	type victim struct {
		id    string
		title string
	}
	var victims []victim
	for rows.Next() {
		var v victim
		if err := rows.Scan(&v.id, &v.title); err != nil {
			break
		}
		victims = append(victims, v)
	}
	rows.Close() //nolint:sqlclosecheck // closed before deletes so deletes don't run under an open cursor
	if rowsErr := rows.Err(); rowsErr != nil {
		s.logger.Warn("knowledge eviction scan failed", "error", rowsErr)
		return
	}
	if len(victims) == 0 {
		return
	}

	for _, v := range victims {
		err := s.withTx(ctx, func(tx txExec) error {
			if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_pages WHERE id = ?`, v.id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_links WHERE from_id = ? OR to_id = ?`, v.id, v.id); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			s.logger.Warn("knowledge eviction delete failed", "page_id", v.id, "error", err)
			continue
		}
		s.logger.Info("knowledge page evicted (max_pages)", "page_id", v.id, "title", v.title)
	}
}
