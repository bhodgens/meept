package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

// txExec is the minimal execution surface shared by *sqlx.Tx and *sqlx.DB so
// helper functions run inside or outside a transaction transparently.
type txExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// withTx runs fn inside a SQLite transaction, committing on nil error and
// rolling back otherwise. Rollback after a failed commit logs and is ignored
// (the commit error is authoritative).
func (s *Store) withTx(ctx context.Context, fn func(tx txExec) error) error {
	opts := &sql.TxOptions{}
	pgTx, err := s.db.BeginTxx(ctx, opts)
	if err != nil {
		return fmt.Errorf("begin knowledge tx: %w", err)
	}
	// *sqlx.Tx satisfies txExec via embedded *sql.Tx plus its own methods.
	var tx txExec = pgTx
	if err := fn(tx); err != nil {
		if rbErr := pgTx.Rollback(); rbErr != nil {
			s.logger.Debug("knowledge tx rollback failed", "error", rbErr)
		}
		return err
	}
	if err := pgTx.Commit(); err != nil {
		return fmt.Errorf("commit knowledge tx: %w", err)
	}
	return nil
}

var _ = slog.Default // keep slog linked for store.go logger usage symmetry
