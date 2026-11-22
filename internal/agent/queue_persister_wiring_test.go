package agent

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/mattn/go-sqlite3"
)

// ---------------------------------------------------------------------------
// Queue persistence write-side wiring pin (steering/followup-queues plan).
//
// The daemon boot path reads queued_followups (daemon.go
// RecoverPendingFollowUps); this pin proves the loop path is the producer:
// RunOnceWithParts attaches a per-conversation QueuePersister via
// SetPersister when the registry carries the shared DB, and a follow-up
// injected into that queue round-trips through the REAL wiring into SQLite.
// ---------------------------------------------------------------------------

// newQueueWiringLoop builds a loop wired exactly like the daemon path: an
// agent registry carrying the shared queue DB (daemon passes
// getQueueDB(components)) and a message queue (registry.createLoop always
// attaches one when queues are enabled).
func newQueueWiringLoop(t *testing.T, db *sql.DB, chatter llm.Chatter) (*AgentLoop, *AgentRegistry) {
	t.Helper()
	// A :memory: SQLite database is per-connection; pin the pool to one
	// connection so the schema the persister initializes is visible to
	// every later query in this test.
	db.SetMaxOpenConns(1)
	registry := NewPlaceholderToolRegistry()
	secChecker := security.NewPermissionChecker(security.Config{})
	reg := NewAgentRegistry(RegistryConfig{
		Logger: slogDiscardLogger(),
		Queues: config.AgentQueuesConfig{Enabled: true, PersistFollowUp: true},
		DB:     db,
	})
	loop := NewAgentLoop("queue-wire-agent", "/tmp",
		WithLLMChatter(chatter),
		WithToolRegistry(registry),
		WithSecurityChecker(secChecker),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
		WithAgentRegistry(reg),
		// Mirror registry.createLoop: registry-built loops carry a message
		// queue (that is what the persister attaches to).
		WithMessageQueue(NewMessageQueue(WithQueueConfig(DefaultQueueConfig()))),
		WithAgentConfig(AgentConfig{MaxIterations: 5}),
	)
	loop.executor = NewExecutor(registry, secChecker)
	return loop, reg
}

// blockingChatter holds the FIRST model call open until released. The queue
// registration + persister attach happen in RunOnceWithParts BEFORE the first
// model call, so while blocked the mid-turn wiring is fully armed — a
// deterministic interception point with no executor involvement.
type blockingChatter struct {
	release   chan struct{}
	callCount int
}

func (m *blockingChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	m.callCount++
	select {
	case <-m.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &llm.Response{Content: "done", Usage: llm.TokenUsage{TotalTokens: 1}}, nil
}

func (m *blockingChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *blockingChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "mock-chatter"}
}

func TestLoop_QueuePersisterAttachedThroughRealWiring(t *testing.T) {
	db := newTestDB(t)
	chatter := newMockChatter(&llm.Response{Content: "done", Usage: llm.TokenUsage{TotalTokens: 1}})
	loop, _ := newQueueWiringLoop(t, db, chatter)
	defer loop.wg.Wait()

	const convID = "conv-queue-wire"
	_, err := loop.RunOnce(context.Background(), "hello", convID)
	require.NoError(t, err)

	// NewQueuePersister initializes the schema at construction, so the table
	// existing after the turn proves the persister was constructed and
	// attached through the real wiring.
	var name string
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='queued_followups'`).Scan(&name)
	require.NoError(t, err, "queued_followups table must exist after a wired turn")
	assert.Equal(t, "queued_followups", name)
}

func TestLoop_QueuePersisterRoundTripThroughRealWiring(t *testing.T) {
	db := newTestDB(t)
	chatter := &blockingChatter{release: make(chan struct{})}
	loop, _ := newQueueWiringLoop(t, db, chatter)
	defer loop.wg.Wait()

	const convID = "conv-queue-roundtrip"
	// Intercept the queue mid-turn: while the first model call is blocked,
	// the queue is registered AND the persister attached. A follow-up
	// injected then is persisted synchronously by the queue (FollowUp ->
	// PersistSync on its own goroutine) through the real attach.
	turnDone := make(chan error, 1)
	go func() {
		_, err := loop.RunOnce(context.Background(), "hello", convID)
		turnDone <- err
	}()

	// Wait for the queue to appear in the registry (registered inside
	// RunOnceWithParts).
	var q *MessageQueue
	require.Eventually(t, func() bool {
		q, _ = loop.agentRegistry.GetActiveQueue(convID)
		return q != nil
	}, 5*time.Second, 5*time.Millisecond, "queue must be registered for the running conversation")

	// The attach block runs BEFORE RegisterActiveQueue, so the persister is
	// already attached at registration time; poll defensively under the
	// queue's lock anyway.
	q.mu.Lock()
	persister := q.persister
	q.mu.Unlock()
	require.Eventually(t, func() bool {
		q.mu.Lock()
		defer q.mu.Unlock()
		persister = q.persister
		return persister != nil
	}, 5*time.Second, 5*time.Millisecond, "QueuePersister must be attached to the live queue")

	require.NoError(t, q.FollowUp(context.Background(), "remember this", "test"))

	// Wait for the async PersistSync to land, then release the turn.
	require.Eventually(t, func() bool {
		var n int
		err := db.QueryRow(`SELECT COUNT(*) FROM queued_followups WHERE conversation_id = ?`, convID).Scan(&n)
		return err == nil && n == 1
	}, 5*time.Second, 10*time.Millisecond, "the follow-up must be persisted through the attached QueuePersister")
	close(chatter.release)

	// Round-trip through the raw table the daemon boot recovery reads.
	var content, queueType string
	err := db.QueryRow(`SELECT content, queue_type FROM queued_followups WHERE conversation_id = ?`, convID).
		Scan(&content, &queueType)
	require.NoError(t, err)
	assert.Equal(t, "remember this", content)
	assert.Equal(t, string(QueueTypeFollowUp), queueType)

	// And through the boot-side reader itself: RecoverPendingFollowUps
	// loads + clears the conversation's rows (replicated in-package via
	// loadAndClearForTest, the load half of which is the same production
	// function RecoverPendingFollowUps calls).
	recovered, err := loadAndClearForTest(db, convID)
	require.NoError(t, err)
	require.Len(t, recovered, 1)
	assert.Equal(t, "remember this", recovered[0].Content)
	assert.Equal(t, QueueTypeFollowUp, recovered[0].QueueType)

	require.NoError(t, <-turnDone)
}

// loadAndClearForTest replicates the pre-C3 boot-recovery shape (load +
// delete) so this pin keeps covering the persister round-trip through the
// exact table + reader the daemon boot path uses. Production boot recovery
// deliberately no longer deletes-before-injection (C3: content must survive
// when no inbox listens); the load half here is the real loadFollowups.
func loadAndClearForTest(db *sql.DB, convID string) ([]QueuedMessage, error) {
	msgs, err := loadFollowups(db, convID)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`DELETE FROM queued_followups WHERE conversation_id = ?`, convID); err != nil {
		return nil, err
	}
	return msgs, nil
}
