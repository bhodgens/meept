package knowledge

import (
	"context"
	"fmt"
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
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT from_symbol FROM code_edges
		 WHERE to_symbol = ? OR to_symbol LIKE ?
		 ORDER BY from_symbol`,
		symbol, "%."+symbol,
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
