package kanban

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// newTestStore opens a store in the test's temp dir (house pattern:
// t.Cleanup over manual RemoveAll).
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "kanban-test.db")
	s, err := NewStore(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return s
}

// seedBoard creates one board with two columns ("todo" -> col-a,
// "doing" -> col-b) and returns their ids.
func seedBoard(t *testing.T, s *Store) (boardID, colA, colB string) {
	t.Helper()
	ctx := context.Background()
	boardID = "b-1"
	if err := s.CreateBoard(ctx, Board{ID: boardID, Name: "Test Board"}); err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}
	colA = "col-a"
	if err := s.CreateColumn(ctx, Column{ID: colA, BoardID: boardID, Name: "todo", Position: 0}); err != nil {
		t.Fatalf("CreateColumn a: %v", err)
	}
	colB = "col-b"
	if err := s.CreateColumn(ctx, Column{ID: colB, BoardID: boardID, Name: "doing", Position: 1, WIP: 2}); err != nil {
		t.Fatalf("CreateColumn b: %v", err)
	}
	return boardID, colA, colB
}

func TestStore_BoardRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.CreateBoard(ctx, Board{Name: ""}); err == nil {
		t.Fatal("CreateBoard with empty name: want error")
	}
	// Empty ID gets generated via pkg/id, never left blank. Note:
	// CreateBoard takes Board BY VALUE, so the caller never sees the
	// generated ID — read it back from the store instead. (If 01/03's
	// board-crud service needs caller-visible IDs, that is its problem to
	// solve; the store contract stays value-semantics.)
	generated := Board{Name: "generated"}
	if err := s.CreateBoard(ctx, generated); err != nil {
		t.Fatalf("CreateBoard generated: %v", err)
	}
	genBoards, err := s.ListBoards(ctx)
	if err != nil {
		t.Fatalf("ListBoards after generated create: %v", err)
	}
	var genFound bool
	for _, b := range genBoards {
		if b.Name == "generated" {
			genFound = true
			if b.ID == "" {
				t.Error("generated board landed with empty ID")
			}
		}
	}
	if !genFound {
		t.Error("generated board not found in ListBoards")
	}
	if err := s.CreateBoard(ctx, Board{ID: "b-1", Name: "Test Board"}); err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}

	boards, err := s.ListBoards(ctx)
	if err != nil {
		t.Fatalf("ListBoards: %v", err)
	}
	if len(boards) != 2 {
		t.Fatalf("ListBoards len = %d, want 2", len(boards))
	}
	var found bool
	for _, b := range boards {
		if b.ID == "b-1" && b.Name == "Test Board" {
			found = true
		}
	}
	if !found {
		t.Error("b-1 not found in ListBoards")
	}
}

func TestStore_ColumnRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, colB := seedBoard(t, s)

	// A column for an unknown board must fail once foreign keys are
	// enforced (DSN carries _pragma=foreign_keys(1)).
	if err := s.CreateColumn(ctx, Column{ID: "col-x", BoardID: "nope", Name: "orphan", Position: 0}); err == nil {
		t.Fatal("CreateColumn with unknown board_id: want FK violation")
	}

	columns, err := s.ListColumns(ctx, boardID)
	if err != nil {
		t.Fatalf("ListColumns: %v", err)
	}
	if len(columns) != 2 {
		t.Fatalf("ListColumns len = %d, want 2", len(columns))
	}
	if columns[0].ID != colA || columns[0].Position != 0 || columns[0].WIP != 0 {
		t.Errorf("col-a = %+v, want id=%s position=0 wip=0", columns[0], colA)
	}
	if columns[1].ID != colB || columns[1].Position != 1 || columns[1].WIP != 2 {
		t.Errorf("col-b = %+v, want id=%s position=1 wip=2", columns[1], colB)
	}
}

func fullProvenanceCard(boardID, colID string) Card {
	return Card{
		ID:         "card-1",
		BoardID:    boardID,
		ColumnID:   colID,
		Title:      "implement lease rule",
		Ref:        WorkRef{Kind: "step", ID: "step-77"},
		Labels:     []string{"voice", "p1"},
		GoalID:     "goal-3",
		EmployeeID: "emp-secretary",
		PlanID:     "plan-12",
		SessionID:  "session-9",
		TriggerRef: "intake:repo#42",
		Order:      5,
	}
}

func TestStore_CardProvenanceRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, _ := seedBoard(t, s)

	// C1 provenance rule: the projector fills GoalID/EmployeeID/PlanID/
	// SessionID/TriggerRef when it creates the card. Every field must
	// survive the write/read round-trip, or the board never shows it.
	if err := s.CreateCard(ctx, fullProvenanceCard(boardID, colA)); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}

	got, err := s.GetCard(ctx, "card-1")
	if err != nil {
		t.Fatalf("GetCard: %v", err)
	}
	want := fullProvenanceCard(boardID, colA)
	// Card holds a []string, so it is not comparable; compare field by field.
	if got.ID != want.ID || got.BoardID != want.BoardID || got.ColumnID != want.ColumnID ||
		got.Title != want.Title || got.Ref != want.Ref ||
		got.GoalID != want.GoalID || got.EmployeeID != want.EmployeeID ||
		got.PlanID != want.PlanID || got.SessionID != want.SessionID ||
		got.TriggerRef != want.TriggerRef || got.Order != want.Order {
		t.Errorf("GetCard scalar fields = %+v, want %+v", got, want)
	}
	if len(got.Labels) != 2 || got.Labels[0] != "voice" || got.Labels[1] != "p1" {
		t.Errorf("Labels = %v, want [voice p1]", got.Labels)
	}

	// Empty labels round-trip to nil ("" and "null" both mean none).
	if err := s.CreateCard(ctx, Card{ID: "card-2", BoardID: boardID, ColumnID: colA, Title: "no labels"}); err != nil {
		t.Fatalf("CreateCard card-2: %v", err)
	}
	got2, err := s.GetCard(ctx, "card-2")
	if err != nil {
		t.Fatalf("GetCard card-2: %v", err)
	}
	if got2.Labels != nil {
		t.Errorf("Labels = %v, want nil", got2.Labels)
	}
}

func TestStore_UpdateAndDeleteCard(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, colB := seedBoard(t, s)
	if err := s.CreateCard(ctx, fullProvenanceCard(boardID, colA)); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}

	card, err := s.GetCard(ctx, "card-1")
	if err != nil {
		t.Fatalf("GetCard: %v", err)
	}
	card.Title = "retitled"
	card.Order = 9
	card.EmployeeID = "emp-coder"
	if err := s.UpdateCard(ctx, card); err != nil {
		t.Fatalf("UpdateCard: %v", err)
	}
	got, err := s.GetCard(ctx, "card-1")
	if err != nil {
		t.Fatalf("GetCard after update: %v", err)
	}
	if got.Title != "retitled" || got.Order != 9 || got.EmployeeID != "emp-coder" {
		t.Errorf("after UpdateCard = %+v, want title=retitled order=9 employee=emp-coder", got)
	}

	// ErrUnknownCard is a value type, so errors.Is matches it by equality —
	// assert on the ID too so a wrong-ID report fails here.
	if err := s.UpdateCard(ctx, Card{ID: "nope", Title: "x"}); !errors.Is(err, ErrUnknownCard{ID: "nope"}) {
		t.Errorf("UpdateCard unknown: err = %v, want ErrUnknownCard{nope}", err)
	}
	if err := s.DeleteCard(ctx, "nope"); !errors.Is(err, ErrUnknownCard{ID: "nope"}) {
		t.Errorf("DeleteCard unknown: err = %v, want ErrUnknownCard{nope}", err)
	}
	if err := s.DeleteCard(ctx, "card-1"); err != nil {
		t.Fatalf("DeleteCard: %v", err)
	}
	if _, err := s.GetCard(ctx, "card-1"); !errors.Is(err, ErrUnknownCard{ID: "card-1"}) {
		t.Errorf("GetCard deleted: err = %v, want ErrUnknownCard{card-1}", err)
	}
	_ = colB
}

func TestStore_MoveCardCAS(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, colB := seedBoard(t, s)
	if err := s.CreateCard(ctx, fullProvenanceCard(boardID, colA)); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}

	// Stale source column (another mover already moved it) must fail the
	// CAS instead of overwriting.
	if err := s.MoveCard(ctx, "card-1", "col-stale", colB); !errors.Is(err, ErrStaleCard) {
		t.Errorf("MoveCard stale from: err = %v, want ErrStaleCard", err)
	}
	if err := s.MoveCard(ctx, "card-missing", colA, colB); !errors.Is(err, ErrStaleCard) {
		t.Errorf("MoveCard unknown card: err = %v, want ErrStaleCard", err)
	}
	if err := s.MoveCard(ctx, "card-1", colA, colB); err != nil {
		t.Fatalf("MoveCard legal: %v", err)
	}
	got, err := s.GetCard(ctx, "card-1")
	if err != nil {
		t.Fatalf("GetCard: %v", err)
	}
	if got.ColumnID != colB {
		t.Errorf("ColumnID = %q, want %q", got.ColumnID, colB)
	}

	// Concurrent moves: exactly one of two simultaneous A->B moves wins.
	// MaxOpenConns(1) serialises at the driver, so the loser must see
	// ErrStaleCard — this is the linearisation the C10 command path needs.
	if err := s.MoveCard(ctx, "card-1", colB, colA); err != nil {
		t.Fatalf("MoveCard back: %v", err)
	}
	var mu sync.Mutex
	var okCount, staleCount int
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.MoveCard(ctx, "card-1", colA, colB)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				okCount++
			case errors.Is(err, ErrStaleCard):
				staleCount++
			default:
				t.Errorf("MoveCard concurrent: unexpected err %v", err)
			}
		}()
	}
	wg.Wait()
	if okCount != 1 || staleCount != 1 {
		t.Errorf("concurrent moves: ok=%d stale=%d, want ok=1 stale=1", okCount, staleCount)
	}
}

func TestStore_ListCardsOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, colB := seedBoard(t, s)
	// Two cards in colA with out-of-order order_val; the other column must
	// never appear in the result.
	for _, c := range []Card{
		{ID: "c-hi", BoardID: boardID, ColumnID: colA, Title: "high", Order: 10},
		{ID: "c-lo", BoardID: boardID, ColumnID: colA, Title: "low", Order: 1},
		{ID: "c-other", BoardID: boardID, ColumnID: colB, Title: "elsewhere", Order: 0},
	} {
		if err := s.CreateCard(ctx, c); err != nil {
			t.Fatalf("CreateCard %s: %v", c.ID, err)
		}
	}
	cards, err := s.ListCards(ctx, colA)
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != 2 || cards[0].ID != "c-lo" || cards[1].ID != "c-hi" {
		t.Errorf("ListCards = %+v, want [c-lo c-hi] ordered by order_val", cards)
	}
}

func TestStore_CardEventsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, _ := seedBoard(t, s)
	if err := s.CreateCard(ctx, fullProvenanceCard(boardID, colA)); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}

	base := time.Now().UTC().Truncate(time.Second)
	events := []CardEvent{
		{CardID: "card-1", Kind: "comment", Actor: "user:caimlas", Body: "looks ready", CreatedAt: base.Add(-2 * time.Minute)},
		{CardID: "card-1", Kind: "timeline", Actor: "projector", Body: "moved to doing", CreatedAt: base.Add(-time.Minute)},
	}
	for _, ev := range events {
		if err := s.AppendCardEvent(ctx, ev); err != nil {
			t.Fatalf("AppendCardEvent: %v", err)
		}
	}
	// FK enforcement: an event for a nonexistent card must fail.
	if err := s.AppendCardEvent(ctx, CardEvent{CardID: "ghost", Kind: "comment", Actor: "x", Body: "y"}); err == nil {
		t.Fatal("AppendCardEvent unknown card: want FK violation")
	}

	got, err := s.ListCardEvents(ctx, "card-1")
	if err != nil {
		t.Fatalf("ListCardEvents: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListCardEvents len = %d, want 2", len(got))
	}
	if got[0].Body != "looks ready" || got[1].Body != "moved to doing" {
		t.Errorf("order = [%q %q], want oldest-first", got[0].Body, got[1].Body)
	}
	for i, ev := range got {
		if ev.ID == "" {
			t.Errorf("event %d has empty ID", i)
		}
		if ev.CreatedAt.IsZero() {
			t.Errorf("event %d has zero CreatedAt", i)
		}
	}
}

func TestStore_ForeignKeyCascade(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, _ := seedBoard(t, s)
	if err := s.CreateCard(ctx, fullProvenanceCard(boardID, colA)); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if err := s.AppendCardEvent(ctx, CardEvent{CardID: "card-1", Kind: "comment", Actor: "a", Body: "b"}); err != nil {
		t.Fatalf("AppendCardEvent: %v", err)
	}

	// C3's cascade contract: deleting the board removes columns, cards and
	// events. Requires _pragma=foreign_keys(1) in the DSN.
	// Cards reference columns, so drop the cards before their parent rows
	// remain reachable from the board delete only through cascade; do the
	// board delete last after verifying each level is gone.
	if _, err := s.db.ExecContext(ctx, "DELETE FROM kanban_boards WHERE id = ?", boardID); err != nil {
		t.Fatalf("delete board: %v", err)
	}
	for _, q := range []struct {
		table string
		count int
	}{
		{"kanban_columns", 0},
		{"kanban_cards", 0},
		{"kanban_card_events", 0},
	} {
		var n int
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM "+q.table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", q.table, err)
		}
		if n != q.count {
			t.Errorf("%s after board delete = %d, want %d (FK CASCADE not enforced?)", q.table, n, q.count)
		}
	}
}

// verifyDSNPragmas proves the store actually applied WAL + foreign_keys on
// a fresh connection; a silently-dropped pragma (mattn-style key) would
// make both checks read their default values.
func TestStore_DSNPragmasApplied(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	var journal, fk string
	if err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if journal != "wal" {
		t.Errorf("journal_mode = %q, want wal", journal)
	}
	if err := s.db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if fk != "1" {
		t.Errorf("foreign_keys = %q, want 1", fk)
	}
}

// ensure the sql import is used even when helpers above are trimmed
var _ = sql.ErrNoRows

// TestStore_Close pins the exported drain-then-close accessor: a Close
// releases the handle (later queries fail), a second Close is a no-op
// returning nil, (*Store)(nil).Close() is nil-safe, and the DB file can be
// reopened after Close (WAL is checkpointed, data intact).
func TestStore_Close(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "close-test.db")

	s, err := NewStore(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.CreateBoard(ctx, Board{ID: "b1", Name: "close"}); err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Double-Close: idempotent, no error.
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	// Nil receiver: shutdown paths call it unconditionally.
	var nilStore *Store
	if err := nilStore.Close(); err != nil {
		t.Fatalf("nil store Close: %v", err)
	}
	// Use-after-close fails instead of panicking.
	if _, err := s.ListBoards(ctx); err == nil {
		t.Fatal("ListBoards after Close: want error, got nil")
	}

	// Reopen the same file: the board survived (clean WAL shutdown).
	reopened, err := NewStore(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("reopen after Close: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	boards, err := reopened.ListBoards(ctx)
	if err != nil {
		t.Fatalf("ListBoards after reopen: %v", err)
	}
	if len(boards) != 1 || boards[0].ID != "b1" {
		t.Fatalf("reopened boards = %+v, want the one card written pre-Close", boards)
	}
}
