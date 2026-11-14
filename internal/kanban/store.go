package kanban

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/pkg/id"
	_ "modernc.org/sqlite"
)

// Store provides Kanban board persistence.
type Store struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewStore creates a new SQLite-backed Kanban store.
func NewStore(dbPath string, logger *slog.Logger) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// Using the pattern from internal/queue/store.go
	// WAL + busy_timeout + synchronous(NORMAL) + _txlock=immediate.
	// foreign_keys(1) is REQUIRED: modernc.org/sqlite defaults the pragma to
	// OFF, so without it every FOREIGN KEY clause in baseSchema — including
	// the ON DELETE CASCADE rules the projection layer relies on — is inert.
	// Only the modernc _pragma=name(value) form reaches SQLite here; mattn
	// keys would be silently dropped (audit-sqlite-dsn-pragmas.py).
	dsn := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)&_txlock=immediate", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	store := &Store{
		db:     db,
		logger: logger,
	}

	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return store, nil
}

const baseSchema = `
CREATE TABLE IF NOT EXISTS kanban_boards (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS kanban_columns (
    id TEXT PRIMARY KEY,
    board_id TEXT NOT NULL,
    name TEXT NOT NULL,
    position INTEGER NOT NULL,
    wip INTEGER DEFAULT 0,
    FOREIGN KEY (board_id) REFERENCES kanban_boards(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS kanban_cards (
    id TEXT PRIMARY KEY,
    board_id TEXT NOT NULL,
    column_id TEXT NOT NULL,
    title TEXT NOT NULL,
    ref_kind TEXT,
    ref_id TEXT,
    labels TEXT, -- JSON array
    goal_id TEXT,
    employee_id TEXT,
    plan_id TEXT,
    session_id TEXT,
    trigger_ref TEXT,
    order_val INTEGER NOT NULL,
    FOREIGN KEY (board_id) REFERENCES kanban_boards(id) ON DELETE CASCADE,
    FOREIGN KEY (column_id) REFERENCES kanban_columns(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS kanban_projections (
    id TEXT PRIMARY KEY,
    board_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    from_state TEXT NOT NULL,
    column_id TEXT NOT NULL,
    FOREIGN KEY (board_id) REFERENCES kanban_boards(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS kanban_card_events (
    id TEXT PRIMARY KEY,
    card_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    actor TEXT NOT NULL,
    body TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (card_id) REFERENCES kanban_cards(id) ON DELETE CASCADE
);
`

func (s *Store) migrate() error {
	if _, err := s.db.Exec(baseSchema); err != nil {
		return fmt.Errorf("failed to apply base schema: %w", err)
	}
	return nil
}

func (s *Store) ListBoards(ctx context.Context) ([]Board, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name FROM kanban_boards")
	if err != nil {
		return nil, fmt.Errorf("failed to list boards: %w", err)
	}
	defer rows.Close()

	var boards []Board
	for rows.Next() {
		var b Board
		if err := rows.Scan(&b.ID, &b.Name); err != nil {
			return nil, fmt.Errorf("failed to scan board: %w", err)
		}
		boards = append(boards, b)
	}
	return boards, nil
}

func (s *Store) CreateBoard(ctx context.Context, b Board) error {
	if b.ID == "" {
		b.ID = id.Generate("board-")
	}
	if b.Name == "" {
		return fmt.Errorf("board name is required")
	}

	_, err := s.db.ExecContext(ctx, "INSERT INTO kanban_boards (id, name, updated_at) VALUES (?, ?, ?)",
		b.ID, b.Name, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("failed to create board: %w", err)
	}
	return nil
}

func (s *Store) ListColumns(ctx context.Context, boardID string) ([]Column, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, board_id, name, position, wip FROM kanban_columns WHERE board_id = ?", boardID)
	if err != nil {
		return nil, fmt.Errorf("failed to list columns: %w", err)
	}
	defer rows.Close()

	var columns []Column
	for rows.Next() {
		var c Column
		if err := rows.Scan(&c.ID, &c.BoardID, &c.Name, &c.Position, &c.WIP); err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}
		columns = append(columns, c)
	}
	return columns, nil
}

func (s *Store) CreateColumn(ctx context.Context, c Column) error {
	if c.ID == "" {
		c.ID = id.Generate("col-")
	}
	if c.BoardID == "" {
		return fmt.Errorf("board_id is required")
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO kanban_columns (id, board_id, name, position, wip) VALUES (?, ?, ?, ?, ?)",
		c.ID, c.BoardID, c.Name, c.Position, c.WIP)
	if err != nil {
		return fmt.Errorf("failed to create column: %w", err)
	}
	return nil
}

func (s *Store) ListCards(ctx context.Context, columnID string) ([]Card, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, board_id, column_id, title, ref_kind, ref_id, labels, goal_id, employee_id, plan_id, session_id, trigger_ref, order_val FROM kanban_cards WHERE column_id = ? ORDER BY order_val ASC", columnID)
	if err != nil {
		return nil, fmt.Errorf("failed to list cards: %w", err)
	}
	defer rows.Close()

	var cards []Card
	for rows.Next() {
		var c Card
		var labelsJSON string
		if err := rows.Scan(&c.ID, &c.BoardID, &c.ColumnID, &c.Title, &c.Ref.Kind, &c.Ref.ID, &labelsJSON, &c.GoalID, &c.EmployeeID, &c.PlanID, &c.SessionID, &c.TriggerRef, &c.Order); err != nil {
			return nil, fmt.Errorf("failed to scan card: %w", err)
		}
		if err := unmarshalLabels(labelsJSON, &c.Labels); err != nil {
			return nil, fmt.Errorf("failed to decode card %s labels: %w", c.ID, err)
		}
		cards = append(cards, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate cards: %w", err)
	}
	return cards, nil
}

// unmarshalLabels decodes the labels JSON column. An empty column means
// "no labels" (nil slice), not corrupt data — a nil Labels marshals to the
// string "null", so both forms must round-trip to nil.
func unmarshalLabels(labelsJSON string, labels *[]string) error {
	if labelsJSON == "" || labelsJSON == "null" {
		*labels = nil
		return nil
	}
	return json.Unmarshal([]byte(labelsJSON), labels)
}

func (s *Store) CreateCard(ctx context.Context, c Card) error {
	if c.ID == "" {
		c.ID = id.Generate("card-")
	}
	if c.Title == "" {
		return fmt.Errorf("card title is required")
	}
	labelsJSON, err := json.Marshal(c.Labels)
	if err != nil {
		return fmt.Errorf("failed to encode card labels: %w", err)
	}
	// Provenance columns (goal_id..trigger_ref) ride the INSERT: C1's rule
	// is that the projector fills them from the source work-item row, and
	// it fills them AT CREATE TIME. An INSERT that omits them makes the
	// columns unreachable for every later reader.
	_, err = s.db.ExecContext(ctx, `INSERT INTO kanban_cards (id, board_id, column_id, title, ref_kind, ref_id, labels, goal_id, employee_id, plan_id, session_id, trigger_ref, order_val) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.BoardID, c.ColumnID, c.Title, c.Ref.Kind, c.Ref.ID, string(labelsJSON),
		c.GoalID, c.EmployeeID, c.PlanID, c.SessionID, c.TriggerRef, c.Order)
	if err != nil {
		return fmt.Errorf("failed to create card: %w", err)
	}
	return nil
}

// UpdateCard writes the mutable fields of a card. Column moves go through
// MoveCard (which CAS-guards the source column); the column_id write here
// is for projector-driven re-materialisation, where no user move races.
func (s *Store) UpdateCard(ctx context.Context, c Card) error {
	labelsJSON, err := json.Marshal(c.Labels)
	if err != nil {
		return fmt.Errorf("failed to encode card labels: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE kanban_cards SET board_id=?, column_id=?, title=?, ref_kind=?, ref_id=?, labels=?, goal_id=?, employee_id=?, plan_id=?, session_id=?, trigger_ref=?, order_val=? WHERE id=?`,
		c.BoardID, c.ColumnID, c.Title, c.Ref.Kind, c.Ref.ID, string(labelsJSON),
		c.GoalID, c.EmployeeID, c.PlanID, c.SessionID, c.TriggerRef, c.Order, c.ID)
	if err != nil {
		return fmt.Errorf("failed to update card: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to read rows affected: %w", err)
	}
	if affected == 0 {
		return ErrUnknownCard{ID: c.ID}
	}
	return nil
}

func (s *Store) DeleteCard(ctx context.Context, cardID string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM kanban_cards WHERE id = ?", cardID)
	if err != nil {
		return fmt.Errorf("failed to delete card: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to read rows affected: %w", err)
	}
	if affected == 0 {
		return ErrUnknownCard{ID: cardID}
	}
	return nil
}

// ErrStaleCard means the card is not in the column the caller believed it
// was in (concurrent move) or does not exist. Callers treat it as "retry
// after re-read", never as lease or WIP refusal — those are C10 checks in
// the service layer (03-board-projection/03) that run BEFORE this CAS.
var ErrStaleCard = errors.New("card not in source column or not found")

func (s *Store) MoveCard(ctx context.Context, cardID, fromCol, toCol string) error {
	// CAS: the source-column predicate in the WHERE clause is what makes
	// concurrent moves linearise — the loser sees 0 affected rows instead
	// of silently overwriting the winner (C10 command path).
	res, err := s.db.ExecContext(ctx, "UPDATE kanban_cards SET column_id = ? WHERE id = ? AND column_id = ?", toCol, cardID, fromCol)
	if err != nil {
		return fmt.Errorf("failed to move card: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to read rows affected: %w", err)
	}
	if affected == 0 {
		return ErrStaleCard
	}
	return nil
}

func (s *Store) GetCard(ctx context.Context, cardID string) (Card, error) {
	var c Card
	var labelsJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, board_id, column_id, title, ref_kind, ref_id, labels, goal_id, employee_id, plan_id, session_id, trigger_ref, order_val
		FROM kanban_cards WHERE id = ?`, cardID).Scan(
		&c.ID, &c.BoardID, &c.ColumnID, &c.Title, &c.Ref.Kind, &c.Ref.ID, &labelsJSON, &c.GoalID, &c.EmployeeID, &c.PlanID, &c.SessionID, &c.TriggerRef, &c.Order)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Card{}, ErrUnknownCard{ID: cardID}
		}
		return Card{}, fmt.Errorf("failed to get card: %w", err)
	}
	if err := unmarshalLabels(labelsJSON, &c.Labels); err != nil {
		return Card{}, fmt.Errorf("failed to decode card %s labels: %w", c.ID, err)
	}
	return c, nil
}

// AppendCardEvent appends one row to the kanban_card_events timeline (C3,
// append-only). Card comments and timeline entries share the table; Kind
// distinguishes them. The FK to kanban_cards is enforced (foreign_keys(1)
// in the DSN), so events for deleted cards cannot exist.
type CardEvent struct {
	ID        string
	CardID    string
	Kind      string
	Actor     string
	Body      string
	CreatedAt time.Time
}

func (s *Store) AppendCardEvent(ctx context.Context, ev CardEvent) error {
	if ev.ID == "" {
		ev.ID = id.Generate("evt-")
	}
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO kanban_card_events (id, card_id, kind, actor, body, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		ev.ID, ev.CardID, ev.Kind, ev.Actor, ev.Body, ev.CreatedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("failed to append card event: %w", err)
	}
	return nil
}

// ListCardEvents returns a card's timeline oldest-first (created_at ASC).
func (s *Store) ListCardEvents(ctx context.Context, cardID string) ([]CardEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, card_id, kind, actor, body, created_at FROM kanban_card_events WHERE card_id = ? ORDER BY created_at ASC, id ASC", cardID)
	if err != nil {
		return nil, fmt.Errorf("failed to list card events: %w", err)
	}
	defer rows.Close()

	var events []CardEvent
	for rows.Next() {
		var ev CardEvent
		var createdAt string
		if err := rows.Scan(&ev.ID, &ev.CardID, &ev.Kind, &ev.Actor, &ev.Body, &createdAt); err != nil {
			return nil, fmt.Errorf("failed to scan card event: %w", err)
		}
		ev.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse card event timestamp %q: %w", createdAt, err)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate card events: %w", err)
	}
	return events, nil
}
