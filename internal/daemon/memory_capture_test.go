package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/l0"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/session"
)

// fakeEnqueuer records enqueued L1 payloads.
type fakeEnqueuer struct {
	mu       sync.Mutex
	jobs     []*queue.Job
	err      error
	failNext bool
}

func (f *fakeEnqueuer) Enqueue(_ context.Context, job *queue.Job) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext {
		f.failNext = false
		return errors.New("enqueue unavailable")
	}
	f.jobs = append(f.jobs, job)
	return f.err
}

func (f *fakeEnqueuer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.jobs)
}

// failingRecorder wraps a real l0.Recorder but fails every Append, letting the
// test prove that a recorder error never suppresses the enqueue (the retired
// hook's independent-error posture).
type failingRecorder struct {
	inner *l0.Recorder
	calls int
}

func (f *failingRecorder) Append(sessionID, role, content string, meta map[string]any) error {
	f.calls++
	return errors.New("disk full")
}

func (f *failingRecorder) DatePath(date string) string { return f.inner.DatePath(date) }

// fakeClassifier implements memory.AmbientClassifierLLM and can be armed to
// panic, proving the subscriber recovers and keeps processing.
type fakeClassifier struct {
	mu     sync.Mutex
	calls  int
	panics bool
}

func (c *fakeClassifier) ExtractCandidates(_ context.Context, _ string) ([]byte, error) {
	c.mu.Lock()
	c.calls++
	panicHere := c.panics
	c.mu.Unlock()
	if panicHere {
		panic("classifier exploded")
	}
	return []byte(`[]`), nil
}

// fakeExtractor adapts a classifier into the AmbientExtractor shape via the
// real memory.NewAmbientExtractor, and records Extract calls through the
// classifier's call count.

func newCaptureHarness(t *testing.T) (*bus.MessageBus, *session.MemoryStore, *l0.Recorder, *fakeEnqueuer, *fakeClassifier, string, string) {
	t.Helper()
	b := bus.New(nil, slog.New(slog.DiscardHandler))
	store := session.NewMemoryStore(slog.New(slog.DiscardHandler))
	sess, err := store.Create("t")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	dataDir := t.TempDir()
	recorder := l0.NewRecorder(dataDir, nil)
	enq := &fakeEnqueuer{}
	classifier := &fakeClassifier{}
	return b, store, recorder, enq, classifier, dataDir, sess.ID
}

// newCaptureHarnessWithSession creates the store with a caller-known session
// id (the MemoryStore generates its own ids on Create, so tests that must
// correlate the L0 session id drive SaveMessages through a created session's
// real ID).
func seedUserMessage(t *testing.T, store *session.MemoryStore, sessionID, content string) {
	t.Helper()
	if err := store.SaveMessages(sessionID, []session.Message{{Role: "user", Content: content}}); err != nil {
		t.Fatalf("seed user message: %v", err)
	}
}

func publishTurnTerminal(t *testing.T, b *bus.MessageBus, ev agent.TurnTerminalEvent) {
	t.Helper()
	bus.PublishT(b, agent.TopicTurnTerminal, "test", ev)
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, desc string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", desc)
}

func captureLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// --- Test cases -------------------------------------------------------

// Completed turn: 2 L0 lines (user then assistant) with the capture session
// ID, and one L1 enqueue with marker semantics (since_msg_id = "" on the
// day's first turn).
func TestMemoryCaptureCompletedTurnRecordsExchange(t *testing.T) {
	b, store, recorder, enq, _, dataDir, sessionID := newCaptureHarness(t)
	sub := WireMemoryCapture(b, store, recorder, enq, nil, captureLogger())
	if sub == nil {
		t.Fatal("WireMemoryCapture returned nil subscriber")
	}
	defer b.Unsubscribe(sub)

	seedUserMessage(t, store, sessionID, "what is a wumpus")

	publishTurnTerminal(t, b, agent.TurnTerminalEvent{
		ConversationID: sessionID,
		SessionID:      sessionID,
		TurnID:         "turn-1",
		Status:         "completed",
		Reply:          "a wumpus is a hypothetical creature",
	})

	date := time.Now().Format("2006-01-02")
	waitFor(t, 2*time.Second, func() bool {
		recs, err := l0.ReadSession(filepath.Join(dataDir, "l0"), date, sessionID)
		return err == nil && len(recs) == 2
	}, "2 L0 records")

	recs, err := l0.ReadSession(filepath.Join(dataDir, "l0"), date, sessionID)
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	if recs[0].Role != "user" || recs[0].Content != "what is a wumpus" {
		t.Errorf("first record = %s/%q, want user/what is a wumpus", recs[0].Role, recs[0].Content)
	}
	if recs[1].Role != "assistant" || recs[1].Content != "a wumpus is a hypothetical creature" {
		t.Errorf("second record = %s/%q, want assistant/reply", recs[1].Role, recs[1].Content)
	}

	waitFor(t, 2*time.Second, func() bool { return enq.count() == 1 }, "1 enqueue")
	if enq.count() != 1 {
		t.Fatalf("enqueues = %d, want 1", enq.count())
	}
	// since_msg_id = "" on the day's first turn (marker-free start).
	var payload memory.L1ExtractPayload
	job := enq.jobs[0]
	if string(job.Type) != "memory.l1_extract" {
		t.Errorf("job type = %s, want memory.l1_extract", job.Type)
	}
	if err := unmarshalJobPayload(job, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.SessionID != sessionID {
		t.Errorf("payload session = %q, want %q", payload.SessionID, sessionID)
	}
	if payload.SinceMsgID != "" {
		t.Errorf("since_msg_id = %q, want empty (first turn of day)", payload.SinceMsgID)
	}
}

// Failed/timeout/parked turns are never captured.
func TestMemoryCaptureSkipsNonCompletedTurns(t *testing.T) {
	for _, status := range []string{"failed", "timeout", "parked"} {
		t.Run(status, func(t *testing.T) {
			b, store, recorder, enq, _, dataDir, sessionID := newCaptureHarness(t)
			sub := WireMemoryCapture(b, store, recorder, enq, nil, captureLogger())
			defer b.Unsubscribe(sub)

			seedUserMessage(t, store, sessionID, "hello")
			publishTurnTerminal(t, b, agent.TurnTerminalEvent{
				ConversationID: sessionID,
				SessionID:      sessionID,
				Status:         status,
				Reply:          "error text",
			})
			time.Sleep(150 * time.Millisecond)

			date := time.Now().Format("2006-01-02")
			if recs, err := l0.ReadSession(filepath.Join(dataDir, "l0"), date, sessionID); err == nil && len(recs) != 0 {
				t.Errorf("%s turn recorded %d L0 lines, want 0", status, len(recs))
			}
			if enq.count() != 0 {
				t.Errorf("%s turn enqueued %d jobs, want 0", status, enq.count())
			}
		})
	}
}

// Empty ConversationID: nothing recorded, no panic.
func TestMemoryCaptureSkipsEmptyConversationID(t *testing.T) {
	b, store, recorder, enq, _, dataDir, sessionID := newCaptureHarness(t)
	sub := WireMemoryCapture(b, store, recorder, enq, nil, captureLogger())
	defer b.Unsubscribe(sub)

	seedUserMessage(t, store, sessionID, "hello")
	publishTurnTerminal(t, b, agent.TurnTerminalEvent{
		SessionID: sessionID,
		Status:    "completed",
		Reply:     "reply",
	})
	time.Sleep(150 * time.Millisecond)

	date := time.Now().Format("2006-01-02")
	if recs, err := l0.ReadSession(filepath.Join(dataDir, "l0"), date, sessionID); err == nil && len(recs) != 0 {
		t.Errorf("recorded %d L0 lines for empty conversation id, want 0", len(recs))
	}
	if enq.count() != 0 {
		t.Errorf("enqueued %d jobs for empty conversation id, want 0", enq.count())
	}
}

// User message not found in the store: skip the whole turn (no lines, no
// enqueue).
func TestMemoryCaptureSkipsWhenUserMessageMissing(t *testing.T) {
	b, store, recorder, enq, _, dataDir, sessionID := newCaptureHarness(t)
	sub := WireMemoryCapture(b, store, recorder, enq, nil, captureLogger())
	defer b.Unsubscribe(sub)

	publishTurnTerminal(t, b, agent.TurnTerminalEvent{
		ConversationID: sessionID,
		SessionID:      sessionID,
		Status:         "completed",
		Reply:          "reply without a prompt",
	})
	time.Sleep(150 * time.Millisecond)

	date := time.Now().Format("2006-01-02")
	if recs, err := l0.ReadSession(filepath.Join(dataDir, "l0"), date, sessionID); err == nil && len(recs) != 0 {
		t.Errorf("recorded %d L0 lines without a user message, want 0", len(recs))
	}
	if enq.count() != 0 {
		t.Errorf("enqueued %d jobs without a user message, want 0", enq.count())
	}
}

// Duplicate (ConversationID, Reply): the second event is dropped.
func TestMemoryCaptureDeduplicatesSameConversationAndReply(t *testing.T) {
	b, store, recorder, enq, _, dataDir, sessionID := newCaptureHarness(t)
	sub := WireMemoryCapture(b, store, recorder, enq, nil, captureLogger())
	defer b.Unsubscribe(sub)

	seedUserMessage(t, store, sessionID, "hello")
	ev := agent.TurnTerminalEvent{
		ConversationID: sessionID,
		SessionID:      sessionID,
		Status:         "completed",
		Reply:          "same reply",
	}
	publishTurnTerminal(t, b, ev)
	publishTurnTerminal(t, b, ev)

	date := time.Now().Format("2006-01-02")
	waitFor(t, 2*time.Second, func() bool {
		recs, err := l0.ReadSession(filepath.Join(dataDir, "l0"), date, sessionID)
		return err == nil && len(recs) == 2
	}, "2 L0 records (exactly one capture of the duplicate pair)")
	time.Sleep(100 * time.Millisecond)
	if recs, err := l0.ReadSession(filepath.Join(dataDir, "l0"), date, sessionID); err != nil || len(recs) != 2 {
		t.Fatalf("L0 records = %d (err %v), want 2 — duplicate was captured twice", len(recs), err)
	}
	if enq.count() != 1 {
		t.Errorf("enqueues = %d, want 1 — duplicate enqueued again", enq.count())
	}
}

// The extractor panicking must not kill the subscriber: the next event still
// processes (L0 lines still land).
func TestMemoryCaptureSurvivesExtractorPanic(t *testing.T) {
	b, store, recorder, enq, classifier, dataDir, sessionID := newCaptureHarness(t)
	extractor := memory.NewAmbientExtractor(memory.AmbientExtractorConfig{
		Manager:    nil, // WriteCandidates would fail anyway; Extract is what panics
		Classifier: classifier,
		Logger:     captureLogger(),
	})
	sub := WireMemoryCapture(b, store, recorder, enq, extractor, captureLogger())
	defer b.Unsubscribe(sub)

	seedUserMessage(t, store, sessionID, "hello")
	classifier.mu.Lock()
	classifier.panics = true
	classifier.mu.Unlock()
	publishTurnTerminal(t, b, agent.TurnTerminalEvent{
		ConversationID: sessionID,
		SessionID:      sessionID,
		Status:         "completed",
		Reply:          "first reply",
	})

	// First event: extraction panics. The recover must fire and L0 must
	// still have been appended BEFORE ambient ran (recording precedes
	// extraction in the subscriber's step order).
	date := time.Now().Format("2006-01-02")
	waitFor(t, 2*time.Second, func() bool {
		recs, err := l0.ReadSession(filepath.Join(dataDir, "l0"), date, sessionID)
		return err == nil && len(recs) == 2
	}, "2 L0 records despite extractor panic")

	// Second event must still process (classifier un-panicked so the turn
	// completes through ambient without a second panic).
	classifier.mu.Lock()
	classifier.panics = false
	classifier.mu.Unlock()
	publishTurnTerminal(t, b, agent.TurnTerminalEvent{
		ConversationID: sessionID,
		SessionID:      sessionID,
		Status:         "completed",
		Reply:          "second reply",
	})
	waitFor(t, 2*time.Second, func() bool {
		recs, err := l0.ReadSession(filepath.Join(dataDir, "l0"), date, sessionID)
		return err == nil && len(recs) == 4
	}, "4 L0 records after recovery")
}

// A recorder Append error is logged but the enqueue still happens (the
// retired hook's independent-error posture, l0_wiring.go:206-215).
func TestMemoryCaptureRecorderErrorStillEnqueues(t *testing.T) {
	b, store, _, enq, _, _, sessionID := newCaptureHarness(t)
	inner := l0.NewRecorder(t.TempDir(), nil)
	bad := &failingRecorder{inner: inner}
	mc := &memoryCapture{
		store:    store,
		recorder: bad,
		enqueuer: enq,
		logger:   captureLogger(),
		seen:     newCaptureLRU(memoryCaptureLRUSize),
	}
	sub := bus.SubscribeT(b, "memory-capture-test", agent.TopicTurnTerminal, mc.handleEvent)
	defer b.Unsubscribe(sub)

	seedUserMessage(t, store, sessionID, "hello")
	publishTurnTerminal(t, b, agent.TurnTerminalEvent{
		ConversationID: sessionID,
		SessionID:      sessionID,
		Status:         "completed",
		Reply:          "reply",
	})

	waitFor(t, 2*time.Second, func() bool { return enq.count() == 1 }, "enqueue despite recorder failure")
	if bad.calls != 2 {
		t.Errorf("recorder Append calls = %d, want 2 (user + assistant)", bad.calls)
	}
}

// A second turn on the same day enqueues with since_msg_id = the previous
// record's ID (marker semantics preserved from the retired hook).
func TestMemoryCaptureSecondTurnSinceMsgID(t *testing.T) {
	b, store, recorder, enq, _, dataDir, sessionID := newCaptureHarness(t)
	sub := WireMemoryCapture(b, store, recorder, enq, nil, captureLogger())
	defer b.Unsubscribe(sub)

	date := time.Now().Format("2006-01-02")
	l0Dir := filepath.Join(dataDir, "l0")

	seedUserMessage(t, store, sessionID, "first question")
	publishTurnTerminal(t, b, agent.TurnTerminalEvent{
		ConversationID: sessionID,
		SessionID:      sessionID,
		Status:         "completed",
		Reply:          "first answer",
	})
	waitFor(t, 2*time.Second, func() bool { return enq.count() == 1 }, "first enqueue")

	seedUserMessage(t, store, sessionID, "second question")
	publishTurnTerminal(t, b, agent.TurnTerminalEvent{
		ConversationID: sessionID,
		SessionID:      sessionID,
		Status:         "completed",
		Reply:          "second answer",
	})
	waitFor(t, 2*time.Second, func() bool { return enq.count() == 2 }, "second enqueue")

	var first, second memory.L1ExtractPayload
	if err := unmarshalJobPayload(enq.jobs[0], &first); err != nil {
		t.Fatalf("unmarshal first: %v", err)
	}
	if err := unmarshalJobPayload(enq.jobs[1], &second); err != nil {
		t.Fatalf("unmarshal second: %v", err)
	}
	if first.SinceMsgID != "" {
		t.Errorf("first since_msg_id = %q, want empty", first.SinceMsgID)
	}
	recs, err := l0.ReadSession(l0Dir, date, sessionID)
	if err != nil || len(recs) != 4 {
		t.Fatalf("L0 records = %d (err %v), want 4", len(recs), err)
	}
	// The second job's window starts at the record BEFORE this turn's
	// appended pair (retired-hook semantics: boundary = last record before
	// the just-appended lines = recs[len-3] with the pair as the tail).
	want := recs[len(recs)-3].ID
	if second.SinceMsgID != want {
		t.Errorf("second since_msg_id = %q, want %q", second.SinceMsgID, want)
	}
}

// Ambient extraction runs over the exchange when an extractor is present.
func TestMemoryCaptureRunsAmbientExtraction(t *testing.T) {
	b, store, recorder, enq, classifier, _, sessionID := newCaptureHarness(t)
	extractor := memory.NewAmbientExtractor(memory.AmbientExtractorConfig{
		Classifier: classifier,
		Logger:     captureLogger(),
	})
	sub := WireMemoryCapture(b, store, recorder, enq, extractor, captureLogger())
	defer b.Unsubscribe(sub)

	seedUserMessage(t, store, sessionID, "I commit to shipping the parser by friday")
	publishTurnTerminal(t, b, agent.TurnTerminalEvent{
		ConversationID: sessionID,
		SessionID:      sessionID,
		Status:         "completed",
		Reply:          "noted",
	})

	waitFor(t, 2*time.Second, func() bool {
		classifier.mu.Lock()
		defer classifier.mu.Unlock()
		return classifier.calls > 0
	}, "ambient classifier call")
}

// Session resolution: empty ev.SessionID falls back to ev.ConversationID as
// the capture session (plan contract step 2).
func TestMemoryCaptureSessionFallbackToConversationID(t *testing.T) {
	b, store, recorder, enq, _, dataDir, sessionID := newCaptureHarness(t)
	sub := WireMemoryCapture(b, store, recorder, enq, nil, captureLogger())
	defer b.Unsubscribe(sub)

	seedUserMessage(t, store, sessionID, "hello")
	publishTurnTerminal(t, b, agent.TurnTerminalEvent{
		ConversationID: sessionID,
		SessionID:      "", // empty → fall back to ConversationID
		Status:         "completed",
		Reply:          "reply",
	})

	date := time.Now().Format("2006-01-02")
	waitFor(t, 2*time.Second, func() bool {
		recs, err := l0.ReadSession(filepath.Join(dataDir, "l0"), date, sessionID)
		return err == nil && len(recs) == 2
	}, "2 L0 records via conversation-id fallback")
}

// RELAYED task turns carry SessionID == ConversationID == the session's
// CONVERSATION id (conv-…) — publishTaskTerminalRelay overwrites sessionID
// with the linked id, while the message store keys messages by the PRIMARY
// session id. The subscriber must resolve the real session via
// GetByConversationID or the task's reply is never captured (live-observed
// defect, fixed by the fallback).
func TestMemoryCaptureRelayConversationIDResolvesPrimarySession(t *testing.T) {
	b, store, recorder, enq, _, dataDir, sessionID := newCaptureHarness(t)
	sub := WireMemoryCapture(b, store, recorder, enq, nil, captureLogger())
	defer b.Unsubscribe(sub)

	// Point the created session's ConversationID at a conv- id, then seed
	// the exchange under the PRIMARY session id (persistExchange shape).
	sess := store.Get(sessionID)
	if sess == nil {
		t.Fatalf("session %s missing", sessionID)
	}
	convID := "conv-relaytest1"
	sess.ConversationID = convID
	seedUserMessage(t, store, sessionID, "dispatched task prompt")

	publishTurnTerminal(t, b, agent.TurnTerminalEvent{
		ConversationID: convID, // relay shape: both fields carry the conv id
		SessionID:      convID,
		Status:         "completed",
		Reply:          "task reply text",
	})

	date := time.Now().Format("2006-01-02")
	waitFor(t, 2*time.Second, func() bool {
		recs, err := l0.ReadSession(filepath.Join(dataDir, "l0"), date, sessionID)
		if err != nil || len(recs) != 2 {
			return false
		}
		return recs[0].Content == "dispatched task prompt" && recs[1].Content == "task reply text"
	}, "2 L0 records under the PRIMARY session id via GetByConversationID fallback")
}

// The last (not the first) user message in the store is the exchange's user
// side.
func TestMemoryCaptureUsesLastUserMessage(t *testing.T) {
	b, store, recorder, enq, _, dataDir, sessionID := newCaptureHarness(t)
	sub := WireMemoryCapture(b, store, recorder, enq, nil, captureLogger())
	defer b.Unsubscribe(sub)

	seedUserMessage(t, store, sessionID, "first user message")
	seedUserMessage(t, store, sessionID, "assistant filler")
	seedUserMessage(t, store, sessionID, "the real last user message")

	publishTurnTerminal(t, b, agent.TurnTerminalEvent{
		ConversationID: sessionID,
		SessionID:      sessionID,
		Status:         "completed",
		Reply:          "final reply",
	})

	date := time.Now().Format("2006-01-02")
	waitFor(t, 2*time.Second, func() bool {
		recs, err := l0.ReadSession(filepath.Join(dataDir, "l0"), date, sessionID)
		return err == nil && len(recs) == 2
	}, "2 L0 records")
	recs, _ := l0.ReadSession(filepath.Join(dataDir, "l0"), date, sessionID)
	if recs[0].Content != "the real last user message" {
		t.Errorf("captured user content = %q, want the LAST user message", recs[0].Content)
	}
}

// unmarshalJobPayload decodes a queue job's payload JSON into dst.
func unmarshalJobPayload(job *queue.Job, dst any) error {
	return json.Unmarshal(job.Payload, dst)
}
