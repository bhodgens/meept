package agent

// Bughunt 2026-11-10 B7 pin: the park store DSN must carry
// `_txlock=immediate`. load() guarantees a transactional read-prune-commit
// (BeginTx -> SELECT -> DELETE expired -> Commit); with a DEFERRED
// transaction, concurrent Loads hit the WAL BUSY_SNAPSHOT deadlock shape
// that busy_timeout cannot retry. An immediate txlock takes the write lock
// at BEGIN, serializing the critical section.
//
// The pin hammers Load from a goroutine herd over one store and asserts no
// "database is locked" / SQLITE_BUSY error escapes.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// TestParkStoreConcurrentLoadsNoBusy hammers Load with a goroutine herd
// mixed with a concurrent writer. Before the _txlock=immediate fix this
// produced intermittent "database is locked" failures under contention.
func TestParkStoreConcurrentLoadsNoBusy(t *testing.T) {
	store := newTestParkStore(t)
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	// Seed rows so every Load has real SELECT + prune-DELETE work to do:
	// half expired (prune half of the transaction), half live.
	resumePast := time.Now().Add(-time.Minute)
	resumeFuture := time.Now().Add(time.Hour)
	for i := range 8 {
		key := fmt.Sprintf("k-expired-%d", i)
		rec := ParkedTurnRecord{
			ConversationID: "conv-" + key,
			SessionID:      "sess-" + key,
			AgentID:        "agent-b7",
			Class:          llm.FailureQuota,
			ResumeAt:       resumePast,
			TurnPayload:    []byte(`{"message":"x"}`),
		}
		if err := store.Save(ctx, ParkKindChat, key, rec); err != nil {
			t.Fatalf("Save expired %s: %v", key, err)
		}
	}
	for i := range 8 {
		key := fmt.Sprintf("k-live-%d", i)
		rec := ParkedTurnRecord{
			ConversationID: "conv-" + key,
			SessionID:      "sess-" + key,
			AgentID:        "agent-b7",
			Class:          llm.FailureThrottle,
			ResumeAt:       resumeFuture,
			TurnPayload:    []byte(`{"message":"x"}`),
		}
		if err := store.Save(ctx, ParkKindChat, key, rec); err != nil {
			t.Fatalf("Save live %s: %v", key, err)
		}
	}

	const loaders = 16
	const iterations = 25
	var wg sync.WaitGroup
	errCh := make(chan error, loaders)
	for i := range loaders {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range iterations {
				recs, _, err := store.Load(ctx, ParkKindChat, time.Now())
				if err != nil {
					errCh <- fmt.Errorf("loader %d iter %d: %w", id, j, err)
					return
				}
				_ = recs
			}
		}(i)
	}
	// A concurrent writer (Save) rides along to make the read-prune-commit
	// transaction contend for real.
	wg.Go(func() {
		for j := range iterations {
			key := fmt.Sprintf("k-writer-%d", j)
			rec := ParkedTurnRecord{
				ConversationID: "conv-" + key,
				SessionID:      "sess-" + key,
				AgentID:        "agent-b7",
				Class:          llm.FailureQuota,
				ResumeAt:       resumeFuture,
				TurnPayload:    []byte(`{"message":"w"}`),
			}
			if err := store.Save(ctx, ParkKindChat, key, rec); err != nil {
				errCh <- fmt.Errorf("writer iter %d: %w", j, err)
				return
			}
		}
	})
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if strings.Contains(err.Error(), "database is locked") ||
			strings.Contains(err.Error(), "SQLITE_BUSY") ||
			strings.Contains(err.Error(), "BUSY_SNAPSHOT") {
			t.Fatalf("lock contention escaped busy_timeout (the B7 deferred-tx deadlock): %v", err)
		}
		t.Fatalf("concurrent park store access failed: %v", err)
	}
}
