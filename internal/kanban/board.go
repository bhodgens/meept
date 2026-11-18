package kanban

import (
	"context"
	"fmt"
	"slices"
)

// Manager provides Kanban board management operations.
// It is renamed from the leaf's "Board" to avoid collision with the Board struct.
type Manager struct {
	store *Store
}

// NewManager creates a new Kanban Manager.
func NewManager(s *Store) (*Manager, error) {
	if s == nil {
		return nil, fmt.Errorf("store is required")
	}
	return &Manager{store: s}, nil
}

// CreateColumn adds a column to the board.
func (m *Manager) CreateColumn(ctx context.Context, b Board, c Column) error {
	if b.ID == "" {
		return fmt.Errorf("board id is required")
	}
	c.BoardID = b.ID
	return m.store.CreateColumn(ctx, c)
}

// ReorderColumn moves one column to pos and shifts the others so positions
// stay a dense 0..N-1. The out-of-range clamp (pos<0 -> 0, pos>=len -> last)
// makes re-runs idempotent: re-applying the same reorder never drifts.
func (m *Manager) ReorderColumn(ctx context.Context, b Board, colID string, pos int) error {
	if b.ID == "" {
		return fmt.Errorf("board id is required")
	}

	columns, err := m.store.ListColumns(ctx, b.ID)
	if err != nil {
		return fmt.Errorf("failed to list columns: %w", err)
	}

	// ListColumns orders by position, so the slice order IS the current
	// column order. Splice into it and rewrite every position densely.
	targetIdx := -1
	for i, col := range columns {
		if col.ID == colID {
			targetIdx = i
			break
		}
	}
	if targetIdx == -1 {
		return ErrUnknownColumn{ID: colID}
	}

	order := make([]string, 0, len(columns))
	for _, col := range columns {
		order = append(order, col.ID)
	}

	// Remove the target, then splice it back at the clamped index.
	order = append(order[:targetIdx], order[targetIdx+1:]...)
	if pos < 0 {
		pos = 0
	}
	if pos > len(order) {
		pos = len(order)
	}
	order = append(order[:pos], append([]string{colID}, order[pos:]...)...)

	positions := make(map[string]int, len(order))
	for i, id := range order {
		positions[id] = i
	}
	if err := m.store.SetColumnPositions(ctx, b.ID, positions); err != nil {
		return fmt.Errorf("failed to apply column positions: %w", err)
	}
	return nil
}

// SetWIPLimit sets the WIP limit for a column. limit < 0 normalizes to 0
// (0 = no limit). An unknown column is an error, not a silent no-op.
func (m *Manager) SetWIPLimit(ctx context.Context, b Board, colID string, limit int) error {
	if b.ID == "" {
		return fmt.Errorf("board id is required")
	}
	if limit < 0 {
		limit = 0
	}
	if err := m.store.UpdateColumnWIP(ctx, b.ID, colID, limit); err != nil {
		return fmt.Errorf("failed to set WIP limit: %w", err)
	}
	return nil
}

// AddLabel adds a label to a card.
func (m *Manager) AddLabel(ctx context.Context, cardID string, label string) error {
	if cardID == "" || label == "" {
		return fmt.Errorf("cardID and label are required")
	}

	card, err := m.store.GetCard(ctx, cardID)
	if err != nil {
		return fmt.Errorf("failed to get card: %w", err)
	}

	if slices.Contains(card.Labels, label) {
		return nil // Idempotent
	}

	card.Labels = append(card.Labels, label)
	return m.store.UpdateCard(ctx, card)
}

// RemoveLabel removes a label from a card.
func (m *Manager) RemoveLabel(ctx context.Context, cardID string, label string) error {
	if cardID == "" || label == "" {
		return fmt.Errorf("cardID and label are required")
	}

	card, err := m.store.GetCard(ctx, cardID)
	if err != nil {
		return fmt.Errorf("failed to get card: %w", err)
	}

	for i, l := range card.Labels {
		if l == label {
			card.Labels = append(card.Labels[:i], card.Labels[i+1:]...)
			return m.store.UpdateCard(ctx, card)
		}
	}

	return nil // Idempotent
}
