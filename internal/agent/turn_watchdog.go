package agent

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// TurnWatchdog is the async-turn liveness reaper (async-turn-migration
// leaf 06). Every pass it asks the TurnRegistry for turns whose last
// progress is older than the stale threshold and, for each, emits a
// turn.terminal event with status=failed and handler_case=turn_reaped,
// then removes the turn from tracking. Mark-and-continue ONLY: the reaper
// never re-dispatches work and never cancels in-flight goroutines — if
// late work eventually completes, the normal turn.terminal still fires
// and clients treat a post-reaped completion as valid.
//
// The emit func is the injection seam: production passes
// ChatHandler.EmitTurnTerminal (wrapped by the daemon composition, which
// also logs the Warn "turn reaped" line per leaf-06 task 4); tests pass a
// capturing func. This keeps package agent free of any
// watchdog→ChatHandler construction dependency.
//
// Staleness is judged on the registry's injectable clock (TurnRegistry.now),
// so tests advance time without sleeping. The pass cadence is a real
// time.Ticker, exercised with tiny intervals in tests.
type TurnWatchdog struct {
	reg    *TurnRegistry
	emit   func(TurnTerminalEvent)
	logger *slog.Logger

	mu      sync.Mutex
	started bool
	stopped bool
	stopCh  chan struct{}
	done    chan struct{}
}

// NewTurnWatchdog builds a watchdog over reg. reg or emit nil disables it
// (Start no-ops). Nil logger falls back to slog.Default().
func NewTurnWatchdog(reg *TurnRegistry, emit func(TurnTerminalEvent), logger *slog.Logger) *TurnWatchdog {
	if logger == nil {
		logger = slog.Default()
	}
	return &TurnWatchdog{reg: reg, emit: emit, logger: logger}
}

// Start spawns the reap loop. Safe to call once; subsequent calls are
// no-ops. Also a no-op when the watchdog was built without a registry or
// emit func (disabled).
func (w *TurnWatchdog) Start(interval, staleAfter time.Duration) {
	if w == nil || w.reg == nil || w.emit == nil {
		return
	}
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return
	}
	w.started = true
	w.stopCh = make(chan struct{})
	w.done = make(chan struct{})
	w.mu.Unlock()
	go w.loop(interval, staleAfter)
}

// Stop halts the loop and waits for its exit (no goroutine leak: the wait
// is on the loop's own done channel). Idempotent and safe when Start was
// never called.
func (w *TurnWatchdog) Stop() {
	if w == nil {
		return
	}
	w.mu.Lock()
	done := w.done
	if w.started && !w.stopped {
		w.stopped = true
		close(w.stopCh)
	}
	w.mu.Unlock()
	if done != nil {
		<-done
	}
}

// RunOnce executes exactly one reap pass and returns the number of turns
// reaped. Exported test seam over the unexported reapOnce (the leaf
// contract offers "RunOnce() exported test seam OR Start with tiny
// interval" — this is the former; Start-with-tiny-interval is covered by
// the lifecycle test too).
func (w *TurnWatchdog) RunOnce(staleAfter time.Duration) int {
	return w.reapOnce(staleAfter)
}

// loop is the background pass loop. One pass runs immediately on start so
// a turn that went stale before Start is reaped on the first tick-free
// pass; subsequent passes ride the ticker.
func (w *TurnWatchdog) loop(interval, staleAfter time.Duration) {
	defer w.loopDone()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	w.reapOnce(staleAfter)
	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.reapOnce(staleAfter)
		}
	}
}

// loopDone closes the done channel exactly once (Stop is the only closer
// of stopCh, and it closes it at most once, so this runs exactly once per
// started loop).
func (w *TurnWatchdog) loopDone() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done != nil {
		close(w.done)
	}
}

// reapOnce is one pass: for every stale record, emit the reaped
// turn.terminal event (panic-isolated per record), log the Warn line, and
// Complete the turn so the next pass cannot double-reap it. Returns the
// reaped count.
func (w *TurnWatchdog) reapOnce(staleAfter time.Duration) int {
	if w == nil || w.reg == nil || w.emit == nil {
		return 0
	}
	stales := w.reg.Stale(staleAfter)
	reaped := 0
	for _, rec := range stales {
		w.safeEmit(TurnTerminalEvent{
			ConversationID: rec.ConversationID,
			TurnID:         rec.TurnID,
			TaskID:         rec.TaskID,
			Status:         "failed",
			Error:          fmt.Sprintf("turn reaped: no progress for %s", staleAfter),
			HandlerCase:    "turn_reaped",
			Reply:          "this turn stopped responding and was marked failed",
		})
		// Observability (leaf-06 task 4): the emit wrapper's Warn line —
		// countable in the daemon log alongside the terminal event itself.
		// The metrics.db schema is deliberately NOT widened.
		w.logger.Warn("turn reaped",
			"turn_id", rec.TurnID,
			"conversation_id", rec.ConversationID,
			"stale_for", staleAfter.String(),
		)
		// Complete AFTER emit, per the leaf contract. Complete is
		// idempotent; after removal Stale() can never return this turn
		// again (no double-reap).
		w.reg.Complete(rec.TurnID)
		reaped++
	}
	return reaped
}

// safeEmit isolates a panicking emit func from the loop: the panic is
// recovered per record, logged, and the pass continues (the turn is still
// Completed by reapOnce — a broken emitter must not wedge the registry).
func (w *TurnWatchdog) safeEmit(ev TurnTerminalEvent) {
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("turn watchdog: emit panicked",
				"panic", r,
				"turn_id", ev.TurnID,
			)
		}
	}()
	w.emit(ev)
}
