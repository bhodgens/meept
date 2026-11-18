package kanban

import (
	"context"
	"errors"
	"testing"
)

func TestManager_CreateColumn(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, _, _ := seedBoard(t, s)
	mgr, err := NewManager(s)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// The Manager is the leaf's contract surface: it stamps BoardID from
	// the Board argument, so callers never hand-build cross-references.
	err = mgr.CreateColumn(ctx, Board{ID: boardID}, Column{Name: "New Col", Position: 5, WIP: 3})
	if err != nil {
		t.Fatalf("CreateColumn failed: %v", err)
	}

	columns, err := s.ListColumns(ctx, boardID)
	if err != nil {
		t.Fatalf("ListColumns failed: %v", err)
	}

	found := false
	for _, c := range columns {
		if c.Name == "New Col" && c.WIP == 3 && c.Position == 5 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Column not found in list or values mismatch")
	}
}

func TestManager_ReorderColumn(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, colB := seedBoard(t, s)
	// colA is pos 0, colB is pos 1.
	// We'll add a third column colC at pos 2.
	colC := "col-c"
	err := s.CreateColumn(ctx, Column{ID: colC, BoardID: boardID, Name: "col-c", Position: 2})
	if err != nil {
		t.Fatalf("CreateColumn colC failed: %v", err)
	}

	mgr, _ := NewManager(s)

	// Case 1: Move col-a (pos 0) to pos 1
	// Expected order: [col-b (0), col-a (1), col-c (2)]
	err = mgr.ReorderColumn(ctx, Board{ID: boardID}, colA, 1)
	if err != nil {
		t.Fatalf("ReorderColumn failed: %v", err)
	}

	columns, _ := s.ListColumns(ctx, boardID)
	if len(columns) != 3 {
		t.Fatalf("Expected 3 columns, got %d", len(columns))
	}

	posMap := make(map[string]int)
	for _, c := range columns {
		posMap[c.ID] = c.Position
	}

	if posMap[colA] != 1 || posMap[colB] != 0 || posMap[colC] != 2 {
		t.Errorf("Reorder failed. posMap: %+v", posMap)
	}

	// Case 2: Move col-c (pos 2) to pos 0
	// Expected order: [col-c (0), col-b (1), col-a (2)]
	err = mgr.ReorderColumn(ctx, Board{ID: boardID}, colC, 0)
	if err != nil {
		t.Fatalf("ReorderColumn failed: %v", err)
	}

	// Re-fetch: a stale slice from case 1 would compare old positions.
	columns, _ = s.ListColumns(ctx, boardID)
	posMap = make(map[string]int)
	for _, c := range columns {
		posMap[c.ID] = c.Position
	}
	if posMap[colC] != 0 || posMap[colB] != 1 || posMap[colA] != 2 {
		t.Errorf("Reorder failed. posMap: %+v", posMap)
	}

	// Idempotence: re-running case 2 must not drift positions.
	err = mgr.ReorderColumn(ctx, Board{ID: boardID}, colC, 0)
	if err != nil {
		t.Fatalf("ReorderColumn (repeat) failed: %v", err)
	}
	columns, _ = s.ListColumns(ctx, boardID)
	posMap = make(map[string]int)
	for _, c := range columns {
		posMap[c.ID] = c.Position
	}
	if posMap[colC] != 0 || posMap[colB] != 1 || posMap[colA] != 2 {
		t.Errorf("Reorder drifted on repeat. posMap: %+v", posMap)
	}

	// Out-of-range clamp: pos past the end lands on the last slot.
	err = mgr.ReorderColumn(ctx, Board{ID: boardID}, colC, 99)
	if err != nil {
		t.Fatalf("ReorderColumn (clamp) failed: %v", err)
	}
	columns, _ = s.ListColumns(ctx, boardID)
	if columns[len(columns)-1].ID != colC {
		t.Errorf("Expected colC last after clamp, order: %+v", columns)
	}
}

func TestManager_SetWIPLimit_UnknownColumn(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, _, _ := seedBoard(t, s)
	mgr, _ := NewManager(s)

	err := mgr.SetWIPLimit(ctx, Board{ID: boardID}, "no-such-col", 5)
	if err == nil {
		t.Fatal("expected ErrUnknownColumn for missing column, got nil")
	}
	if _, ok := errors.AsType[ErrUnknownColumn](err); !ok {
		t.Errorf("expected wrapped ErrUnknownColumn, got %v", err)
	}

	// Negative limit normalizes to 0 (no limit), not a negative WIP.
	colID := "col-neg"
	if err := s.CreateColumn(ctx, Column{ID: colID, BoardID: boardID, Name: "neg", Position: 3}); err != nil {
		t.Fatalf("CreateColumn: %v", err)
	}
	if err := mgr.SetWIPLimit(ctx, Board{ID: boardID}, colID, -7); err != nil {
		t.Fatalf("SetWIPLimit(-7): %v", err)
	}
	cols, _ := s.ListColumns(ctx, boardID)
	for _, c := range cols {
		if c.ID == colID && c.WIP != 0 {
			t.Errorf("expected WIP 0 after negative limit, got %d", c.WIP)
		}
	}
}

func TestManager_SetWIPLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, _ := seedBoard(t, s)

	mgr, _ := NewManager(s)
	err := mgr.SetWIPLimit(ctx, Board{ID: boardID}, colA, 5)
	if err != nil {
		t.Fatalf("SetWIPLimit failed: %v", err)
	}

	columns, _ := s.ListColumns(ctx, boardID)
	for _, c := range columns {
		if c.ID == colA {
			if c.WIP != 5 {
				t.Errorf("Expected WIP 5, got %d", c.WIP)
			}
			break
		}
	}
}

func TestManager_Labels(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, colA, _ := seedBoard(t, s)

	// Create a card
	cardID := "card-1"
	err := s.CreateCard(ctx, Card{ID: cardID, BoardID: boardID, ColumnID: colA, Title: "Label Test", Order: 0})
	if err != nil {
		t.Fatalf("CreateCard failed: %v", err)
	}

	mgr, _ := NewManager(s)

	// Add labels
	err = mgr.AddLabel(ctx, cardID, "urgent")
	if err != nil {
		t.Fatalf("AddLabel failed: %v", err)
	}
	err = mgr.AddLabel(ctx, cardID, "p1")
	if err != nil {
		t.Fatalf("AddLabel failed: %v", err)
	}

	// Verify
	card, _ := s.GetCard(ctx, cardID)
	if len(card.Labels) != 2 || card.Labels[0] != "urgent" || card.Labels[1] != "p1" {
		t.Errorf("Labels mismatch: %+v", card.Labels)
	}

	// Test Idempotency
	err = mgr.AddLabel(ctx, cardID, "urgent")
	if err != nil {
		t.Errorf("AddLabel (duplicate) failed: %v", err)
	}
	card, _ = s.GetCard(ctx, cardID)
	if len(card.Labels) != 2 {
		t.Errorf("Duplicate label added: %v", card.Labels)
	}

	// Remove label
	err = mgr.RemoveLabel(ctx, cardID, "urgent")
	if err != nil {
		t.Fatalf("RemoveLabel failed: %v", err)
	}

	// Verify
	card, _ = s.GetCard(ctx, cardID)
	if len(card.Labels) != 1 || card.Labels[0] != "p1" {
		t.Errorf("RemoveLabel failed: %+v", card.Labels)
	}

	// Remove absent label
	err = mgr.RemoveLabel(ctx, cardID, "nonexistent")
	if err != nil {
		t.Errorf("RemoveLabel (absent) failed: %v", err)
	}
}

func TestManager_Errors(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	boardID, _, _ := seedBoard(t, s)
	mgr, _ := NewManager(s)

	t.Run("CreateColumn_NoBoard", func(t *testing.T) {
		err := mgr.CreateColumn(ctx, Board{ID: ""}, Column{Name: "No Board"})
		if err == nil {
			t.Error("Expected error for empty board ID")
		}
	})

	t.Run("ReorderColumn_NoCol", func(t *testing.T) {
		err := mgr.ReorderColumn(ctx, Board{ID: boardID}, "non-existent", 0)
		if err == nil {
			t.Error("Expected error for non-existent column")
		}
	})

	t.Run("AddLabel_NoCard", func(t *testing.T) {
		err := mgr.AddLabel(ctx, "non-existent", "label")
		if err == nil {
			t.Error("Expected error for non-existent card")
		}
	})
}
