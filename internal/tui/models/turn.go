package models

// Async turn lifecycle for the chat view (async-turn-migration leaf 04).
//
// The TUI no longer blocks on the synchronous "chat" RPC. A send now:
//  1. fires chat.submit via the RPCClient interface (returns the ack
//     immediately — never agent work),
//  2. registers the turn in the chat model's turnRouter keyed by turn_id,
//  3. spawns awaitTurnCmd — a tea.Cmd goroutine that never blocks Update —
//     which waits for the terminal result and delivers it as a tea.Msg.
//
// Terminal results and progress reach this process through the TUI
// EventStream poll loop (internal/tui/events.go): the App's event dispatch
// calls ChatModel.DeliverTurnTerminal / DeliverTurnProgress, which resolve
// the waiting awaitTurnCmd goroutine. No sleeps are involved: liveness is
// enforced with injectable timers (timerSource), so tests never wait on the
// wall clock.

import (
	"context"
	"fmt"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Turn submit statuses — the ack's accepted flag is the honest signal that
// the daemon took the turn; accepted=false means nothing is running.
const (
	turnStatusCompleted = "completed"
	turnStatusFailed    = "failed"
	turnStatusTimeout   = "timeout"
	turnStatusParked    = "parked"
	turnStatusStalled   = "stalled"
)

// TurnSubmitAck is the parsed "chat.submit" RPC ack. It carries ONLY the
// submission receipt — agent work has not happened yet and the final reply
// arrives later on the turn.terminal bus event.
type TurnSubmitAck struct {
	TurnID         string `json:"turn_id"`
	ConversationID string `json:"conversation_id"`
	SessionID      string `json:"session_id"`
	Accepted       bool   `json:"accepted"`
	Note           string `json:"note"`
}

// turnSubmittedMsg signals that chat.submit was accepted and the turn is
// now tracked in the chat view (pending line visible).
type turnSubmittedMsg struct {
	TurnID         string
	ConversationID string
}

// turnProgressMsg carries a live progress text for a submitted turn.
type turnProgressMsg struct {
	ConversationID string
	Text           string
}

// turnTerminalMsg carries the terminal state of a submitted turn. Status is
// completed | failed | timeout | parked (daemon statuses) or stalled (the
// local liveness verdict — the daemon may still complete; the reply is not
// lost, see renderTurnPending).
type turnTerminalMsg struct {
	TurnID         string
	ConversationID string
	Reply          string
	Status         string
	Error          string
	DurationMS     int64
	// TaskID is the C1 identity field for the (turn_id, task_id) dedupe
	// key (04-clutter/02, gate T6): the task the turn dispatched to, when
	// the daemon's turn.terminal payload carries one. Empty on stalled
	// verdicts and on relays that predate the field -- the dedupe ledger
	// keys on turn_id alone for those (defensive shape, pinned in
	// chat_dedupe_test.go).
	TaskID string
	// IntentType / HandlerCase / ClassifiedBy are the remaining C1
	// correlation fields (master C1, 05-navigation T1): what the
	// dispatcher classified the turn as, which handler branch ran, and
	// who classified it. The daemon's turn.terminal payload carries all
	// four (internal/agent/handler.go TurnTerminalEvent); the TUI keep-
	// list must not drop them -- the task chip, route popover and audit
	// surfacing read them off the rendered message.
	IntentType   string
	HandlerCase  string
	ClassifiedBy string
}

// stalledTurnText is the honest, lowercase stalled-turn wording (UI text
// convention: all lowercase).
const stalledTurnText = "no progress for %ds — task may still be running"

// timerSource is the injectable clock used for liveness. The default
// source uses real timers; tests install short timers and a channel-based
// waiter so no test ever sleeps.
type timerSource interface {
	// After arms a one-shot timer. It must respect ctx cancellation so a
	// terminal event racing the timer resolves immediately.
	After(ctx context.Context, d time.Duration) <-chan struct{}
}

// realTimerSource arms real timers (production).
type realTimerSource struct{}

// After implements timerSource with time.AfterFunc.
func (realTimerSource) After(ctx context.Context, d time.Duration) <-chan struct{} {
	ch := make(chan struct{}, 1)
	t := time.AfterFunc(d, func() {
		select {
		case ch <- struct{}{}:
		default:
		}
	})
	go func() {
		<-ctx.Done()
		t.Stop()
	}()
	return ch
}

// defaultTimerSource is the production timer source.
var defaultTimerSource timerSource = realTimerSource{}

// pendingTurn is one chat.submit turn being awaited by the chat view.
type pendingTurn struct {
	turnID         string
	conversationID string
	// sessionID is the daemon session the turn was submitted under (the
	// ack echoes it). F21: terminal delivery/persistence keys on THIS id,
	// never the currently selected session.
	sessionID    string
	startedAt    time.Time
	lastActivity time.Time
	// activityGen increments on every liveness refresh (progress or
	// parked delivery). The await timer re-arms only when the generation
	// changed since it armed — a wall-clock freshness re-check alone
	// would re-arm forever and never emit the stalled verdict.
	activityGen uint64
	// progressText is the latest progress line for this turn.
	progressText string

	// waiters are the awaitTurnCmd goroutine channels (single waiter in
	// practice; a slice keeps delivery fan-out safe).
	mu      sync.Mutex
	waiters []chan turnTerminalMsg
	done    bool
	result  turnTerminalMsg
}

// isTerminalTurnStatus reports whether a turn status RESOLVES the
// awaiter. completed, failed and timeout end the turn; parked (quota wait)
// and the local stalled verdict are non-terminal — the real result still
// arrives later, so the waiter must stay armed (week bughunt F6).
func isTerminalTurnStatus(status string) bool {
	switch status {
	case turnStatusCompleted, turnStatusFailed, turnStatusTimeout:
		return true
	}
	return false
}

// notify resolves every waiter with the terminal result exactly once.
// Non-terminal statuses (parked, stalled) do NOT latch done and do NOT
// resolve waiters: they only record the notice text (and, for parked, the
// daemon liveness) so the re-armed/still-armed await resolves with the
// real terminal result when it arrives.
func (p *pendingTurn) notify(res turnTerminalMsg) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !isTerminalTurnStatus(res.Status) {
		if res.Error != "" {
			p.progressText = res.Error
		}
		if res.Status == turnStatusParked {
			// The daemon is alive (quota wait): this counts as activity
			// for the liveness window.
			p.lastActivity = time.Now()
			p.activityGen++
		}
		return
	}
	if p.done {
		return
	}
	p.done = true
	p.result = res
	for _, w := range p.waiters {
		// Buffered channels: never block the event-dispatch goroutine.
		w <- res
		close(w)
	}
	p.waiters = nil
}

// dropWaiter removes a waiter channel without resolving it. The stall
// re-arm path abandons its waiter (the verdict is returned as the command
// message, not through the channel), so the stale channel must be removed
// or later terminal notifications would fan out to dead receivers.
func (p *pendingTurn) dropWaiter(ch chan turnTerminalMsg) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, w := range p.waiters {
		if w == ch {
			p.waiters = append(p.waiters[:i], p.waiters[i+1:]...)
			return
		}
	}
}

// livenessRemaining reports how much of the liveness window remains based
// on the turn's last recorded activity, together with the activity
// generation (callers re-arm only when the generation changed — see
// pendingTurn.activityGen). live=false means the window has fully
// elapsed since lastActivity (the stalled verdict is then honest).
func (p *pendingTurn) livenessRemaining(liveness time.Duration) (time.Duration, uint64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	gen := p.activityGen
	elapsed := time.Since(p.lastActivity)
	if elapsed >= liveness {
		return 0, gen, false
	}
	return liveness - elapsed, gen, true
}

// bumpActivity refreshes liveness (lastActivity=now) and increments the
// activity generation. Called by progress and parked delivery.
func (p *pendingTurn) bumpActivity() {
	p.mu.Lock()
	p.lastActivity = time.Now()
	p.activityGen++
	p.mu.Unlock()
}

// addWaiter registers a waiter channel unless the turn already resolved.
func (p *pendingTurn) addWaiter() (chan turnTerminalMsg, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done {
		return nil, false
	}
	ch := make(chan turnTerminalMsg, 1)
	p.waiters = append(p.waiters, ch)
	return ch, true
}

// turnRouter tracks submitted turns keyed by turn id. It is safe for
// concurrent use: awaitTurnCmd goroutines, the Update loop, and the
// EventStream dispatch all touch it.
type turnRouter struct {
	mu    sync.Mutex
	turns map[string]*pendingTurn
	// timers is the injectable liveness timer source.
	timers timerSource
}

// newTurnRouter creates a router with the given timer source (nil = real).
func newTurnRouter(timers timerSource) *turnRouter {
	if timers == nil {
		timers = defaultTimerSource
	}
	return &turnRouter{turns: make(map[string]*pendingTurn), timers: timers}
}

// register adds a submitted turn. Returns false if the turn id is already
// tracked (idempotent-retry duplicate acks must not double-register).
// sessionID records the daemon session the turn belongs to (F21); it may
// be empty when the caller only knows the conversation.
func (r *turnRouter) register(turnID, conversationID, sessionID string) (*pendingTurn, bool) {
	if turnID == "" {
		return nil, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.turns[turnID]; exists {
		return nil, false
	}
	now := time.Now()
	pt := &pendingTurn{
		turnID:         turnID,
		conversationID: conversationID,
		sessionID:      sessionID,
		startedAt:      now,
		lastActivity:   now,
	}
	r.turns[turnID] = pt
	return pt, true
}

// get returns the tracked turn by id, if any.
func (r *turnRouter) get(turnID string) (*pendingTurn, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pt, ok := r.turns[turnID]
	return pt, ok
}

// remove forgets a turn (after its terminal resolution was rendered).
func (r *turnRouter) remove(turnID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.turns, turnID)
}

// hasPendingFor reports whether any unresolved submitted turn belongs to
// conversationID. This drives the double-render dedupe: while true, the
// chat view ignores task.completed chat messages for that conversation —
// the same completion also arrives as turn.terminal, and rendering
// both would put two result bubbles in the transcript.
func (r *turnRouter) hasPendingFor(conversationID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, pt := range r.turns {
		if pt.conversationID == conversationID {
			return true
		}
	}
	return false
}

// awaitTurnCmd waits for the terminal result of a submitted turn without
// ever blocking the UI thread: it runs as a tea.Cmd goroutine and delivers
// exactly one turnTerminalMsg. Terminal results arrive through the
// EventStream dispatch (ChatModel.DeliverTurnTerminal); if no terminal
// event — and no progress event — arrives within the liveness window, the
// command emits the honest stalled verdict (Status "stalled") so the view
// can show that the task may still complete. Liveness timers come from the
// injectable timerSource (turnRouter.timers); liveness <= 0 disables the
// stalled check entirely (0=disabled in the chat config contract).
//
// Liveness is keyed on the turn's lastActivity (refreshed by progress and
// parked events): the window is the REMAINING time since the last
// activity, so a turn that just received progress gets a fresh window.
// The stalled verdict never resolves the waiter (pendingTurn.notify
// ignores non-terminal statuses), so this goroutine must abandon its
// waiter channel via dropWaiter before returning the verdict — the later
// re-arm (handleTurnTerminal) registers a fresh one.
func awaitTurnCmd(r *turnRouter, pt *pendingTurn, liveness time.Duration) tea.Cmd {
	return func() tea.Msg {
		waitCh, ok := pt.addWaiter()
		if !ok {
			// Already resolved (terminal raced the await registration).
			pt.mu.Lock()
			res := pt.result
			pt.mu.Unlock()
			return res
		}

		// The liveness window counts from the turn's last activity, not
		// from await arming: progress/parked events keep the turn alive.
		// When the timer fires while a liveness refresh landed mid-window
		// (generation advanced since arming), the timer re-arms for the
		// remaining time instead of emitting a dishonest stalled verdict.
		window := liveness
		armedGen := uint64(0)
		if r != nil {
			if remaining, gen, live := pt.livenessRemaining(liveness); live {
				window = remaining
				armedGen = gen
			}
		}
		var timer <-chan struct{}
		if window > 0 && r != nil {
			timer = r.timers.After(context.Background(), window)
		}

		for {
			select {
			case res := <-waitCh:
				return res
			case <-timer:
				if r != nil {
					if _, gen, live := pt.livenessRemaining(liveness); live && gen != armedGen {
						// Progress arrived mid-window: reset the timer
						// for the remaining time of the fresh window.
						remaining, gen2, _ := pt.livenessRemaining(liveness)
						armedGen = gen2
						timer = r.timers.After(context.Background(), remaining)
						continue
					}
				}
				// Abandon the waiter WITHOUT resolving the turn: the real
				// terminal still lands later through a fresh await (F6).
				pt.dropWaiter(waitCh)
				res := turnTerminalMsg{
					TurnID:         pt.turnID,
					ConversationID: pt.conversationID,
					Status:         turnStatusStalled,
					Error:          fmt.Sprintf(stalledTurnText, int(liveness.Seconds())),
				}
				// Records the notice text only — never latches done.
				pt.notify(res)
				return res
			}
		}
	}
}
