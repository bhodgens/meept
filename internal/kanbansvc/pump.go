package kanbansvc

// Board-event pump — the daemon→sidecar live feed that replaces IdleSource
// (kanban-board follow-up gap #4 / the GUI live feed).
//
// # Why this shape
//
// The daemon already exposes the bus over RPC: bus.subscribe (creates a
// buffered, connection-scoped subscription; answers
// {"subscription_id": ...}), bus.poll ({"subscription_id", "since_seq"} →
// {"events": [...], "next_seq": N, "dropped": N}), and bus.unsubscribe
// (internal/rpc/proxy.go). The pump rides the SAME RPCCaller client the
// gateway's REST→RPC path uses — one connection, no new seam, no new
// imports (the sidecar stays sqlite-free and config-blind).
//
// The seven kanban topics are subscribed CONCRETE, not wildcarded: the
// bus matcher (internal/bus.MatchWildcard) binds "*" to exactly one
// segment, so "kanban.*" would never match the 4-segment
// kanban.event.<kind>.state topics. The list is card.moved,
// board.changed, and the five event.*.state topics (C2/C8 surface).
//
// Frames are delivered to gateway relay subscribers in the daemon's own
// WS envelope — {"type":"board_event","data":{...payload, "topic":...}} —
// because the Flutter board client (kanban_models.dart KanbanBoardEvent)
// decodes that shape and the gateway is contractually forward-only: it
// must NOT re-derive classification, it mirrors what the daemon would
// have sent.
//
// Lifecycle: Subscribe(ctx) starts the pump goroutine (subscribe → poll
// loop) lazily per relay; the pump tolerates daemon disconnects with
// bounded retry and re-subscription (the server drops subscriptions when
// the connection dies). Close() ends the pump: every subscriber channel
// closes and the goroutine exits.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// kanbanPumpTopics are the seven concrete board topics (C2 table + the
// two board-side topics). Keep in sync with internal/kanban's topic
// declarations — the pump subscribes by name over RPC and cannot import
// that package (import ban).
var kanbanPumpTopics = []string{
	"kanban.card.moved",
	"kanban.board.changed",
	"kanban.event.task.state",
	"kanban.event.job.state",
	"kanban.event.plan.state",
	"kanban.event.step.state",
	"kanban.event.goal.state",
}

// PumpOptions tunes the poll loop. Zero values select the defaults.
type PumpOptions struct {
	// PollInterval is the bus.poll cadence (default 500ms).
	PollInterval time.Duration
	// RetryBackoff bounds the reconnect wait after a failed RPC
	// (default 2s; the pump retries with ONE attempt per backoff tick —
	// the wait itself is split across polls so Close stays prompt).
	RetryBackoff time.Duration
	// Logger receives pump lifecycle lines (never payloads' secrets —
	// board events carry no credentials; DEBUG level only).
	Logger *slog.Logger
}

const (
	pumpDefaultPollInterval  = 500 * time.Millisecond
	pumpDefaultRetryBackoff  = 2 * time.Second
	pumpSubscriberChanBuffer = 64
)

// BusPump is the RealEventSource: it bridges the daemon's bus.subscribe /
// bus.poll RPC pair into the gateway's EventSource fan-out. Construct
// with NewBusPump; one pump serves every relay connection.
type BusPump struct {
	rpc  RPCCaller
	opts PumpOptions

	mu      sync.Mutex
	subs    map[chan json.RawMessage]struct{}
	started bool
	closed  bool
	cancel  context.CancelFunc // stops the pump goroutine
	done    chan struct{}      // closed when the pump goroutine exits

	subID  string
	cursor int64
}

// pumpRecord mirrors one busEventRecord from the daemon's bus.poll reply.
type pumpRecord struct {
	Topic     string          `json:"topic"`
	Type      string          `json:"type"`
	Source    string          `json:"source"`
	Timestamp string          `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
	Seq       int64           `json:"seq"`
}

type pumpPollReply struct {
	Events  []pumpRecord `json:"events"`
	NextSeq int64        `json:"next_seq"`
	Dropped int64        `json:"dropped"`
}

// NewBusPump builds the board-event pump over the gateway's existing
// RPCCaller seam. rpc must not be nil (the gateway requires one anyway).
func NewBusPump(rpc RPCCaller, opts PumpOptions) (*BusPump, error) {
	if rpc == nil {
		return nil, fmt.Errorf("kanbansvc: pump requires an rpc caller")
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = pumpDefaultPollInterval
	}
	if opts.RetryBackoff <= 0 {
		opts.RetryBackoff = pumpDefaultRetryBackoff
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &BusPump{
		rpc:  rpc,
		opts: opts,
		subs: make(map[chan json.RawMessage]struct{}),
	}, nil
}

// Subscribe implements EventSource. The pump starts on the first call
// (goroutine launched once) and the returned channel yields board_event
// frames until ctx ends or the pump closes. A closed pump yields an
// already-closed channel (an idle relay), never an error.
func (p *BusPump) Subscribe(ctx context.Context) (<-chan json.RawMessage, error) {
	out := make(chan json.RawMessage, pumpSubscriberChanBuffer)

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		close(out)
		return out, nil
	}
	p.subs[out] = struct{}{}
	var pctx context.Context
	start := !p.started
	if start {
		p.started = true
		var cancel context.CancelFunc
		pctx, cancel = context.WithCancel(context.Background())
		p.cancel = cancel
		p.done = make(chan struct{})
	}
	p.mu.Unlock()

	if start {
		go p.run(pctx, p.done)
	}

	// Per-subscriber detach: ctx cancellation closes ONLY this channel.
	go func() {
		<-ctx.Done()
		p.detach(out)
	}()
	return out, nil
}

// detach removes one subscriber and closes its channel. A send into a
// detached channel is dropped by the fan-out (it checks membership under
// the lock), so this never blocks the pump.
func (p *BusPump) detach(ch chan json.RawMessage) {
	p.mu.Lock()
	_, ok := p.subs[ch]
	if ok {
		delete(p.subs, ch)
	}
	p.mu.Unlock()
	if ok {
		close(ch)
	}
}

// Close stops the pump goroutine, waits for it to exit, best-effort
// unsubscribes the daemon-side subscription (a dead socket is not an
// error at teardown time), and closes every subscriber channel.
// Idempotent and safe to call concurrently.
func (p *BusPump) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	cancel := p.cancel
	done := p.done
	subID := p.subID
	p.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done // drain-then-close: the goroutine has stopped polling
	}
	if subID != "" {
		// Best-effort: the daemon also cleans up when the connection
		// drops, so a failure here is not the pump's error to report.
		if _, err := p.rpc.Call("bus.unsubscribe", map[string]any{"subscription_id": subID}); err != nil {
			p.opts.Logger.Debug("kanban pump: unsubscribe on close failed", "error", err)
		}
	}

	p.mu.Lock()
	channels := make([]chan json.RawMessage, 0, len(p.subs))
	for ch := range p.subs {
		channels = append(channels, ch)
	}
	p.subs = make(map[chan json.RawMessage]struct{})
	p.mu.Unlock()
	for _, ch := range channels {
		close(ch)
	}
	return nil
}

// run is the pump goroutine: subscribe (with bounded retry), then the
// poll loop. It exits on ctx cancellation (Close) or when ctx ends; a
// daemon disconnect NEVER exits the goroutine — it re-enters the retry
// path, so no relay is orphaned by a transient socket failure.
func (p *BusPump) run(ctx context.Context, done chan struct{}) {
	defer close(done)

	ticker := time.NewTicker(p.opts.PollInterval)
	defer ticker.Stop()
	scheduled := false

	for {
		if ctx.Err() != nil {
			return
		}
		if !scheduled {
			if !p.ensureSubscribed(ctx) {
				return // ctx ended during retry sleep
			}
			scheduled = true
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if !p.pollOnce(ctx) {
			// Subscription lost (daemon restart / socket drop): the
			// next loop iteration re-subscribes. failed counts the
			// reconnect.
			scheduled = false
		}
	}
}

// ensureSubscribed retries bus.subscribe until it succeeds or ctx ends,
// sleeping in backoff-sized slices so Close stays prompt.
func (p *BusPump) ensureSubscribed(ctx context.Context) bool {
	for {
		reply, err := p.rpc.Call("bus.subscribe", map[string]any{"topics": kanbanPumpTopics})
		if err == nil {
			var got struct {
				SubscriptionID string `json:"subscription_id"`
			}
			if json.Unmarshal(reply, &got) == nil && got.SubscriptionID != "" {
				p.mu.Lock()
				p.subID = got.SubscriptionID
				p.cursor = 0
				p.mu.Unlock()
				p.opts.Logger.Debug("kanban pump: bus subscribed", "subscription_id", got.SubscriptionID)
				return true
			}
			err = fmt.Errorf("malformed bus.subscribe reply")
		}
		p.opts.Logger.Debug("kanban pump: subscribe failed, retrying", "error", err)
		if !sleepCtx(ctx, p.opts.RetryBackoff) {
			return false
		}
	}
}

// pollOnce delivers one bus.poll page. Returns false when the daemon-side
// subscription is gone (caller re-subscribes), true otherwise (a plain
// RPC error keeps the subscription and just skips the tick).
func (p *BusPump) pollOnce(ctx context.Context) bool {
	p.mu.Lock()
	subID, cursor := p.subID, p.cursor
	p.mu.Unlock()
	if subID == "" {
		return false
	}

	reply, err := p.rpc.Call("bus.poll", map[string]any{
		"subscription_id": subID,
		"since_seq":       cursor,
	})
	if err != nil {
		// "subscription not found" is the daemon telling us the
		// connection-scoped subscription died (server dropped the RPC
		// connection or restarted); everything else is transient.
		if isSubGone(err) {
			return false
		}
		p.opts.Logger.Debug("kanban pump: poll failed", "error", err)
		return true
	}

	var got pumpPollReply
	if jsonErr := json.Unmarshal(reply, &got); jsonErr != nil {
		p.opts.Logger.Debug("kanban pump: malformed poll reply", "error", jsonErr)
		return true
	}

	frames := make([]json.RawMessage, 0, len(got.Events))
	for _, ev := range got.Events {
		frames = append(frames, boardFrame(ev))
	}

	p.mu.Lock()
	// Monotonic seq cursor: advance to next_seq ONLY after the fan-out
	// snapshot below has the events queued; events the daemon trimmed
	// (dropped > 0) are honestly lost — the GUI reconciles via the board
	// REST path, same posture as the daemon's own WS relay under load.
	if got.NextSeq > p.cursor {
		p.cursor = got.NextSeq
	}
	targets := make([]chan json.RawMessage, 0, len(p.subs))
	for ch := range p.subs {
		targets = append(targets, ch)
	}
	p.mu.Unlock()

	for _, ch := range targets {
		for _, frame := range frames {
			p.mu.Lock()
			// Re-check membership under the same lock Close/detach use
			// when they close a channel: a send either lands before the
			// close or is skipped — it can NEVER hit a closed channel.
			if _, live := p.subs[ch]; live && !p.closed {
				select {
				case ch <- frame:
				default:
					// A stalled relay (browser gone) drops board frames
					// for THAT subscriber only; the next board REST
					// reload resyncs. Never block the pump on one client.
				}
			}
			p.mu.Unlock()
		}
	}
	return true
}

// boardFrame wraps one bus record in the daemon's board_event envelope —
// the same {"type":"board_event","data":{...}} frame the WS hub sends
// (internal/comm/http/server.go), so the Flutter KanbanBoardEvent
// decoders work unchanged. The source topic rides as data.source_topic,
// mirroring transformBusEventToWS.
func boardFrame(ev pumpRecord) json.RawMessage {
	var payload map[string]any
	if len(ev.Payload) > 0 {
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			payload = map[string]any{"raw": ev.Payload}
		}
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if ev.Topic != "" {
		payload["source_topic"] = ev.Topic
	}
	if ev.Timestamp != "" {
		if _, ok := payload["timestamp"]; !ok {
			payload["timestamp"] = ev.Timestamp
		}
	}
	payload["type"] = "board_event"
	frame, err := json.Marshal(map[string]any{"type": "board_event", "data": payload})
	if err != nil {
		// json.Marshal of a map decoded from valid JSON cannot fail in
		// practice; if it does, deliver the bare payload rather than
		// dropping the delta silently.
		return ev.Payload
	}
	return frame
}

// isSubGone classifies the daemon's "subscription not found: X" poll
// error (internal/rpc/proxy.go handleBusPoll) as a re-subscribe signal.
// Case-insensitive Contains: transports re-wrap the message, the token
// stays intact.
func isSubGone(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "subscription not found")
}

// sleepCtx waits d in slices bounded by the poll interval so a Close
// during a backoff returns within ~one poll tick. Returns false when ctx
// ended first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	for remaining := d; remaining > 0; {
		step := min(remaining, pumpDefaultPollInterval)
		timer := time.NewTimer(step)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
		remaining -= step
	}
	return true
}
