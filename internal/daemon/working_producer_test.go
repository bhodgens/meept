package daemon

import (
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/memory/working"
	"github.com/caimlas/meept/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newWorkingProducerHarness builds the producer fixture: a memory
// session store, a working ring, and the bus. (memory_capture_test.go
// harness shape; kept separate so the two subscribers' fixtures stay
// independently evolvable.)
func newWorkingProducerHarness(t *testing.T) (*bus.MessageBus, *session.MemoryStore, *working.Store, string) {
	t.Helper()
	b := bus.New(nil, captureLogger())
	store := session.NewMemoryStore(captureLogger())
	sess, err := store.Create("t")
	require.NoError(t, err)
	work, err := working.New(working.Config{MaxTokens: 4000, MaxItems: 20}, llmHeuristicCounter{})
	require.NoError(t, err)
	return b, store, work, sess.ID
}

// llmHeuristicCounter adapts the heuristic tokenizer to the ring's
// TokenCounter.
type llmHeuristicCounter struct{}

func (llmHeuristicCounter) CountTokens(s string) int { return len(s) / 4 }

// TestWorkingProducerTurnGrowsRing (leaf T5 Given/When/Then): a
// completed turn appends one ring turn - LiveTokens grows above zero.
// The wiring-failure-proof target: delete the WireWorkingProducer call
// site in components.go (the producer that never existed pre-leaf) and
// this behavior has no author - a ring built by WireContextAssembler
// stays at LiveTokens == 0 forever.
func TestWorkingProducerTurnGrowsRing(t *testing.T) {
	b, store, work, sessionID := newWorkingProducerHarness(t)
	require.NoError(t, store.SaveMessages(sessionID, []session.Message{
		{Role: "user", Content: "what is the deploy checklist?"},
	}))
	sub := WireWorkingProducer(b, store, work, captureLogger())
	require.NotNil(t, sub, "a wired store must produce a subscriber")
	defer b.Unsubscribe(sub)

	assert.Equal(t, 0, work.LiveTokens(), "ring starts empty (pre-leaf defect state)")
	publishTurnTerminal(t, b, agent.TurnTerminalEvent{
		ConversationID: sessionID,
		SessionID:      sessionID,
		TurnID:         "turn-1",
		Status:         "completed",
		Reply:          "here is the checklist: build, test, ship",
	})
	waitFor(t, 2*time.Second, func() bool { return work.LiveTokens() > 0 },
		"working ring to grow after a completed turn")
	assert.Equal(t, 1, work.QueuedTurns()+len(work.Snapshot()))
}

// TestWorkingProducerNilStoreNoSubscriber pins the structural contract:
// no ring store -> no subscriber (the ring is absent exactly as before
// the leaf). This is the disabled posture.
func TestWorkingProducerNilStoreNoSubscriber(t *testing.T) {
	b, store, _, _ := newWorkingProducerHarness(t)
	assert.Nil(t, WireWorkingProducer(b, store, nil, captureLogger()))
	assert.Nil(t, WireWorkingProducer(nil, store, nil, captureLogger()))
}

// TestWorkingProducerSkipsNonCompleted pins the status gate: failed /
// parked turns never enter the ring.
func TestWorkingProducerSkipsNonCompleted(t *testing.T) {
	b, store, work, sessionID := newWorkingProducerHarness(t)
	require.NoError(t, store.SaveMessages(sessionID, []session.Message{
		{Role: "user", Content: "prompt"},
	}))
	sub := WireWorkingProducer(b, store, work, captureLogger())
	require.NotNil(t, sub)
	defer b.Unsubscribe(sub)

	publishTurnTerminal(t, b, agent.TurnTerminalEvent{
		ConversationID: sessionID,
		SessionID:      sessionID,
		TurnID:         "turn-failed",
		Status:         "failed",
		Error:          "boom",
		Reply:          "",
	})
	// No growth; the event handler returns before the goroutine. Give
	// the pump a beat to prove nothing landed asynchronously.
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, work.LiveTokens())
}

// TestWorkingProducerDedup pins the relay+direct double-terminal rule:
// the same (conversation, reply) terminal appends ONCE.
func TestWorkingProducerDedup(t *testing.T) {
	b, store, work, sessionID := newWorkingProducerHarness(t)
	require.NoError(t, store.SaveMessages(sessionID, []session.Message{
		{Role: "user", Content: "prompt"},
	}))
	sub := WireWorkingProducer(b, store, work, captureLogger())
	require.NotNil(t, sub)
	defer b.Unsubscribe(sub)

	ev := agent.TurnTerminalEvent{
		ConversationID: sessionID,
		SessionID:      sessionID,
		TurnID:         "turn-dup",
		Status:         "completed",
		Reply:          "same reply twice",
	}
	publishTurnTerminal(t, b, ev)
	waitFor(t, 2*time.Second, func() bool { return len(work.Snapshot()) == 1 },
		"first terminal to land")
	publishTurnTerminal(t, b, ev)
	time.Sleep(50 * time.Millisecond)
	assert.Len(t, work.Snapshot(), 1, "duplicate terminal must not double-append")
}
