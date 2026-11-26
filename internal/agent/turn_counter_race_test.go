package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
	"github.com/stretchr/testify/require"
)

// TestTurnCounterRace_A23 pins the bughunt 2026-11-25 A2-3 fix: the loop's
// turnCounter is read under l.mu by triggerTitleRefresh, under stateMu by
// GetStateSnapshot, and was incremented UNGUARDED by publishTurnEndEvent —
// a three-way mutex-asymmetry race. The counter now rides sync/atomic, so
// concurrent publishes + snapshot/status reads must be -race clean and
// every increment must land exactly once.
//
// Failing revert: restore `turnCounter int` + bare `l.turnCounter++` — run
// with -race and the detector fires (or the exact-count assert flakes).
func TestTurnCounterRace_A23(t *testing.T) {
	mb := bus.New(nil, slogDiscardLogger())
	t.Cleanup(mb.Close)

	loop := NewAgentLoop("loop-a23", t.TempDir(),
		WithMessageBus(mb),
		WithLoopLogger(slogDiscardLogger()),
		WithEventEmitter(NewEventEmitter("a23", mb, slogDiscardLogger())),
	)
	require.NotNil(t, loop)

	var wg sync.WaitGroup
	const rounds = 40
	for i := 0; i < rounds; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			loop.publishTurnEndEvent(context.Background(), "conv-a23", 1, false, 0, 10, 0, "end_turn")
		}()
		go func() {
			defer wg.Done()
			_ = loop.GetStateSnapshot()
		}()
		go func() {
			defer wg.Done()
			_ = loop.turnStatus()
		}()
	}
	wg.Wait()

	require.Equal(t, int64(rounds), loop.turnCounter.Load(),
		"every increment must land exactly once (lost-update detector)")
}

// TestTitleRefreshJoinOnWg_A24 pins the A2-4 fix: the periodic title-refresh
// goroutine (which contains an LLM call) must ride l.wg so the loop's join
// points wait it out — the old bare `go l.triggerTitleRefresh` let Stop()
// return while the call was in flight. The refresh request parks inside a
// stalled test server; wg.Wait() must block until the server is released,
// then return.
//
// Failing revert: restore `go l.triggerTitleRefresh(...)` — the refresh
// escapes wg, so the first Wait below returns IMMEDIATELY (before release)
// and the pin fails at its "returned while still in flight" assert.
func TestTitleRefreshJoinOnWg_A24(t *testing.T) {
	mb := bus.New(nil, slogDiscardLogger())
	t.Cleanup(mb.Close)

	release := make(chan struct{})
	var closeOnce sync.Once
	releaseNow := func() { closeOnce.Do(func() { close(release) }) }
	requested := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case requested <- struct{}{}:
		default:
		}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"name\":\"a24\",\"description\":\"one two three four\"}"}}]}`))
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(releaseNow) // LIFO: release runs BEFORE srv.Close, never strands the handler

	loop := NewAgentLoop("loop-a24", t.TempDir(),
		WithMessageBus(mb),
		WithLoopLogger(slogDiscardLogger()),
		WithSessionID("sess-a24"),
		WithEventEmitter(NewEventEmitter("a24", mb, slogDiscardLogger())),
	)
	require.NotNil(t, loop)
	loop.SetSessionRefresher(session.NewSessionRefresher(
		llm.NewClient(&llm.ModelConfig{BaseURL: srv.URL, ModelID: "a24-pin"}),
		slogDiscardLogger()))
	loop.turnCounter.Store(4) // next publish crosses the %5 boundary

	loop.publishTurnEndEvent(context.Background(), "conv-a24", 1, false, 0, 10, 0, "end_turn")

	// The refresh goroutine reached the LLM and parked on the stalled
	// server.
	select {
	case <-requested:
	case <-time.After(3 * time.Second):
		t.Fatal("title refresh never fired the LLM request (arming path broken)")
	}

	waitDone := make(chan struct{})
	go func() { loop.wg.Wait(); close(waitDone) }()
	select {
	case <-waitDone:
		t.Fatal("l.wg.Wait returned while the tracked refresh call was still in flight (A2-4)")
	case <-time.After(300 * time.Millisecond):
	}

	releaseNow()
	select {
	case <-waitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("l.wg.Wait never joined the title-refresh goroutine (A2-4)")
	}
}
