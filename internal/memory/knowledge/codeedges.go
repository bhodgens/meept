package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// This file extends the knowledge-store schema ADDITIVELY (tree 04 leaf L3):
// the code_edges table backing the Go code index's call graph. The table
// lives in the SAME SQLite database as the pages, but call edges are not
// pages (they are not text), so they get their own table and methods here
// rather than flowing through Ingest. store.go's existing schema is not
// touched; CREATE TABLE IF NOT EXISTS is additive-safe.

const (
	createCodeEdgesTableSQL = `
CREATE TABLE IF NOT EXISTS code_edges (
    from_file   TEXT NOT NULL,
    from_symbol TEXT NOT NULL,
    to_symbol   TEXT NOT NULL,
    from_line   INTEGER NOT NULL DEFAULT 0
)`

	createCodeEdgesFromFileIdxSQL = `
CREATE INDEX IF NOT EXISTS idx_code_edges_from_file ON code_edges(from_file)`

	createCodeEdgesToSymbolIdxSQL = `
CREATE INDEX IF NOT EXISTS idx_code_edges_to_symbol ON code_edges(to_symbol)`
)

// CallEdge is one resolved intra-repo call: fromSymbol (in fromFile, at
// fromLine) calls toSymbol. Symbols are package-qualified
// ("pkg.Name", "pkg.Type.Method"); unresolved (external) calls are never
// stored — only intra-repo edges enter this table.
type CallEdge struct {
	FromFile   string
	FromSymbol string
	ToSymbol   string
	FromLine   int
}

// ensureCodeEdgesSchema creates the code_edges table and indexes if absent.
// Called before every write/read instead of at New() so store.go's init
// path stays untouched; CREATE ... IF NOT EXISTS on SQLite is cheap and
// idempotent.
func (s *Store) ensureCodeEdgesSchema(ctx context.Context) error {
	for _, stmt := range []string{
		createCodeEdgesTableSQL,
		createCodeEdgesFromFileIdxSQL,
		createCodeEdgesToSymbolIdxSQL,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create code_edges schema: %w", err)
		}
	}
	return nil
}

// ListSources returns every page source with the given prefix (empty prefix
// = all), sorted. The code indexer uses the "code:" prefix to enumerate
// indexed files; a source with no matching file on disk this pass is stale
// (deleted file) and gets removed.
func (s *Store) ListSources(ctx context.Context, prefix string) ([]string, error) {
	if s == nil {
		return nil, fmt.Errorf("list sources: nil store")
	}
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	var (
		rows *sql.Rows
		err  error
	)
	if prefix == "" {
		rows, err = s.db.QueryContext(ctx, `SELECT source FROM knowledge_pages ORDER BY source`)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT source FROM knowledge_pages WHERE source LIKE ? ESCAPE '\' ORDER BY source`,
			strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(prefix)+"%")
	}
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var src string
		if err := rows.Scan(&src); err != nil {
			continue
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// DeleteBySource removes a knowledge page by its Source value (plus its link
// rows) inside one transaction. Missing page → no-op, nil error. The code
// indexer calls this for files that disappeared from the indexed dirs, so
// deleted *.go files stop appearing in knowledge search and their call edges
// stop naming callers that no longer exist (bughunt 2026-10-14 — IndexGoDirs
// only ingested; nothing ever dropped a vanished file).
func (s *Store) DeleteBySource(ctx context.Context, source string) error {
	if s == nil {
		return fmt.Errorf("delete by source: nil store")
	}
	if err := s.checkReady(); err != nil {
		return err
	}
	if source == "" {
		return fmt.Errorf("delete by source: empty source")
	}
	return s.withTx(ctx, func(tx txExec) error {
		var id string
		err := tx.QueryRowContext(ctx,
			`SELECT id FROM knowledge_pages WHERE source = ? LIMIT 1`, source).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("lookup page by source %q: %w", source, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_pages WHERE id = ?`, id); err != nil {
			return fmt.Errorf("delete page %s: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_links WHERE from_id = ? OR to_id = ?`, id, id); err != nil {
			return fmt.Errorf("delete links for %s: %w", id, err)
		}
		return nil
	})
}

// ReplaceCallEdges atomically replaces all edges originating from fromFile
// with edges. Re-indexing an unchanged file calls this with the same edges
// (idempotent); re-indexing a changed file drops its stale edges first, so
// removed call sites never linger. fromFile is the indexer's canonical
// relative path (slash-separated), matching the page Source field.
func (s *Store) ReplaceCallEdges(ctx context.Context, fromFile string, edges []CallEdge) error {
	if s == nil {
		return fmt.Errorf("replace call edges: nil store")
	}
	if err := s.checkReady(); err != nil {
		return err
	}
	if err := s.ensureCodeEdgesSchema(ctx); err != nil {
		return err
	}
	err := s.withTx(ctx, func(tx txExec) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM code_edges WHERE from_file = ?`, fromFile); err != nil {
			return fmt.Errorf("delete stale call edges for %s: %w", fromFile, err)
		}
		for _, e := range edges {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO code_edges (from_file, from_symbol, to_symbol, from_line) VALUES (?, ?, ?, ?)`,
				e.FromFile, e.FromSymbol, e.ToSymbol, e.FromLine,
			); err != nil {
				return fmt.Errorf("insert call edge %s -> %s: %w", e.FromSymbol, e.ToSymbol, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// CallersEdges returns the full call-edge rows whose to_symbol resolves to
// symbol (same matching rule as Callers). Companion to Callers for consumers
// that need the call-site line, not just the name.
func (s *Store) CallersEdges(ctx context.Context, symbol string) ([]CallEdge, error) {
	if s == nil {
		return nil, fmt.Errorf("callers edges: nil store")
	}
	if symbol == "" {
		return nil, fmt.Errorf("callers edges: empty symbol")
	}
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	if err := s.ensureCodeEdgesSchema(ctx); err != nil {
		return nil, err
	}
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(symbol)
	rows, err := s.db.QueryContext(ctx,
		`SELECT from_file, from_symbol, to_symbol, from_line FROM code_edges
		 WHERE to_symbol = ? OR to_symbol LIKE ? ESCAPE '\'
		 ORDER BY from_symbol, from_file`,
		symbol, "%."+escaped,
	)
	if err != nil {
		return nil, fmt.Errorf("callers edges query: %w", err)
	}
	defer rows.Close()

	var out []CallEdge
	for rows.Next() {
		var e CallEdge
		if err := rows.Scan(&e.FromFile, &e.FromSymbol, &e.ToSymbol, &e.FromLine); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Callers returns the one-hop callers of symbol from the call-edge table.
// symbol may be bare ("Alpha") or package-qualified ("pkg.Alpha"); bare
// names match qualified stored symbols by ".name" suffix (a bare name that
// is a suffix of another symbol — e.g. Read matching Store.Read — is an
// accepted v1 heuristic, documented in the codeindex package). Results are
// distinct and sorted. No callers → empty slice, never an error.
func (s *Store) Callers(ctx context.Context, symbol string) ([]string, error) {
	if s == nil {
		return nil, fmt.Errorf("callers: nil store")
	}
	if symbol == "" {
		return nil, fmt.Errorf("callers: empty symbol")
	}
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	if err := s.ensureCodeEdgesSchema(ctx); err != nil {
		return nil, err
	}
	// Escape LIKE wildcards in the symbol so a name containing _ or % matches
	// only itself (bughunt LOW: do_thing previously also matched symbols whose
	// qualified forms differ at that character). ESCAPE '\' is SQLite's
	// standard form; the v1 suffix heuristic (pkg.Func) is preserved because a
	// dot carries no wildcard meaning in LIKE.
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(symbol)
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT from_symbol FROM code_edges
		 WHERE to_symbol = ? OR to_symbol LIKE ? ESCAPE '\'
		 ORDER BY from_symbol`,
		symbol, "%."+escaped,
	)
	if err != nil {
		return nil, fmt.Errorf("query callers of %s: %w", symbol, err)
	}
	defer rows.Close() //nolint:errcheck // read-only rows cleanup

	var callers []string
	for rows.Next() {
		var from string
		if err := rows.Scan(&from); err != nil {
			return nil, fmt.Errorf("scan caller: %w", err)
		}
		callers = append(callers, from)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate callers of %s: %w", symbol, err)
	}
	return callers, nil
}
