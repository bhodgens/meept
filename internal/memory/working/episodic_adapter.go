package working

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/memory"
)

// turnCategory is the episodic category stamped on flushed working turns.
const turnCategory = "conversation"

// EpisodicSinkAdapter adapts the existing FTS5 episodic store
// (*memory.EpisodicMemory) to the EpisodicSink interface so evicted
// working-tier turns land in recall storage (MemGPT recall-storage shape,
// findings-memory-tiers.md P1/P4). It reuses the episodic store's existing
// Store path (no new schema, no second writer).
type EpisodicSinkAdapter struct {
	store *memory.EpisodicMemory
}

// compile-time proof the adapter satisfies EpisodicSink.
var _ EpisodicSink = (*EpisodicSinkAdapter)(nil)

// NewEpisodicSink adapts an initialized *memory.EpisodicMemory as an
// EpisodicSink for Store.Flush. The store must be Initialize()d by the
// caller before Flush runs.
func NewEpisodicSink(store *memory.EpisodicMemory) EpisodicSink {
	return &EpisodicSinkAdapter{store: store}
}

// AppendTurns stores each turn in the episodic store via the existing
// Add path (EpisodicMemory.Store, MemoryTypeEpisodic territory: category
// "conversation"). Turn identity rides in metadata (turn_id, session_id,
// role) so the lossless copy stays traceable; the session_id/turn_id
// duality follows the root AGENTS.md rule (distinct fields, both kept).
func (a *EpisodicSinkAdapter) AppendTurns(ctx context.Context, turns []Turn) error {
	if a == nil || a.store == nil {
		return fmt.Errorf("working: episodic sink adapter has no store")
	}
	var errs []string
	for _, t := range turns {
		meta := map[string]any{
			"turn_id":    t.ID,
			"session_id": t.SessionID,
			"role":       t.Role,
			"turn_at":    t.At.UTC().Format(time.RFC3339Nano),
		}
		if _, err := a.store.Store(ctx, t.Content, turnCategory, meta); err != nil {
			errs = append(errs, fmt.Sprintf("turn %s: %v", t.ID, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("working: episodic sink stored %d/%d turns, errors: %s",
			len(turns)-len(errs), len(turns), strings.Join(errs, "; "))
	}
	return nil
}
