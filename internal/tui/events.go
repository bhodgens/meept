package tui

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

// BusEvent represents an event from the message bus.
type BusEvent struct {
	Topic     string    `json:"topic"`
	Type      string    `json:"type"` // event, request, response
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`
	// Seq is the daemon's per-subscription monotonic record sequence
	// (internal/rpc/proxy.go). It is the authoritative identity for dedupe;
	// a zero value means an older daemon or a hand-built event, in which
	// case addToBuffer falls back to a full-field signature.
	Seq     int64 `json:"seq"`
	Payload any   `json:"payload"`
}

// EventStream manages real-time event streaming from the daemon's message bus.
// It uses polling since the RPC layer doesn't support server-push.
type EventStream struct {
	rpc            *RPCClient
	subscriptionID string
	topics         []string
	done           chan struct{}
	mu             sync.Mutex
	running        bool
	// generation is advanced on every Start. An armed tea.Tick is a one-shot
	// timer that Stop cannot retract; stamping the generation at arm time and
	// dropping mismatched ticks in Update prevents a pre-Stop tick from
	// re-arming a second (or third) poll chain after a fast Stop->Start.
	generation uint64
	// inflight is true while a Poll RPC is in progress. A second Poll that
	// lands while one is already running returns nil so two poll chains
	// cannot double-deliver the same page into the buffer.
	inflight     bool
	pollInterval time.Duration
	// lastSeq is the daemon-side monotonic cursor: bus.poll(since_seq)
	// returns records with Seq > lastSeq and the response's next_seq is
	// stored back here. Unlike the former client-side wall-clock lastPoll
	// cursor (strict Timestamp.After comparison), a server-seq cursor can
	// never skip an event that was buffered before the previous poll
	// returned — the load-shaped silent-drop bug this replaces.
	lastSeq int64
	// pendingResubscribe is set when a poll found the subscription lost
	// and subscribe() re-ran successfully; the next successful poll
	// carries it as EventStreamDataMsg.Resubscribed so the app can tell
	// the user the feed reconnected (C6).
	pendingResubscribe bool
	buffer             []BusEvent // Circular buffer for recent events
	bufferSize         int
	bufferIdx          int
}

// EventStreamConfig configures the event stream.
type EventStreamConfig struct {
	Topics       []string
	BufferSize   int
	PollInterval time.Duration
}

// DefaultEventStreamConfig returns default configuration.
func DefaultEventStreamConfig() *EventStreamConfig {
	return &EventStreamConfig{
		// Topic patterns MUST be pairwise non-overlapping. bus.subscribe
		// creates ONE COLLECTOR PER MATCHING PATTERN (internal/rpc/proxy.go:
		// 452-456) and bus.poll returns every buffered record with no dedupe
		// (:553-558), so a topic matched by two patterns is delivered to the
		// TUI TWICE. The viz handlers absorb the duplicate (map-keyed pending
		// buffer, viz/dispatch.go:360) but the activity feed renders
		// RecentEvents(10) verbatim (sidebar.go:676-695), so every duplicated
		// event appears twice on screen.
		//
		// matchWildcard requires EQUAL SEGMENT COUNTS (internal/bus/bus.go:
		// 440), so the three agent forms below are mutually exclusive by
		// construction. They are also the complete coverage: the narrower
		// "agent.event.*" / "agent.progress.*" patterns that once sat here
		// are strict subsets of "agent.*.*" and added nothing but a second
		// delivery per event (bughunt H9).
		//
		// The 3- and 4-segment forms are load-bearing, not redundant:
		// "agent.*" cannot see them, and without them the TUI never learns an
		// agent went active (agentActive stays false, ctrl+s steering dead)
		// and SteeringInjectedMsg never arrives (e2e finding tui-steer-01,
		// 2026-10-01).
		//
		// TestTopicPatternsAreNonOverlapping pins this invariant across every
		// published agent.* topic in the repo.
		Topics: []string{
			"agent.*",
			"agent.*.*",
			"agent.*.*.*",
			"task.*",
			"step.*",
			"queue.*",
			"memory.*",
			"worker.*",
			"chat.worker.*",
			"llm.*",
			"chat.*",
			"conversation.*",
			"plan.*",
			"turn.terminal",
			// bot.result / employee.result are 2-segment typed topics;
			// matchWildcard needs equal segment counts, so no wildcard
			// above sees them. Without exact-topic subscriptions the TUI
			// never learns a bot/employee round finished (issue #160).
			// Non-overlapping with every pattern above.
			"bot.result",
			"employee.result",
			// ACP human-permission relay (acp-human-permission-relay
			// leaf 04, Contract D): acp.permission.requested/.resolved
			// are 3-segment topics — matchWildcard needs equal segment
			// counts, so "acp.*" would NOT match them (the same gap
			// leaf 03 fixed in the WS-proxy list). Non-overlapping with
			// every pattern above.
			"acp.*.*",
		},
		BufferSize:   50,
		PollInterval: 500 * time.Millisecond,
	}
}

// NewEventStream creates a new event stream.
func NewEventStream(rpc *RPCClient, cfg *EventStreamConfig) *EventStream {
	if cfg == nil {
		cfg = DefaultEventStreamConfig()
	}

	return &EventStream{
		rpc:          rpc,
		topics:       cfg.Topics,
		done:         make(chan struct{}),
		pollInterval: cfg.PollInterval,
		buffer:       make([]BusEvent, cfg.BufferSize),
		bufferSize:   cfg.BufferSize,
	}
}

// Start begins polling for events.
//
// Re-arming: done is re-made here, so a Stop -> Start cycle resumes
// normally. The channel is allocated once in NewEventStream and closed by
// Stop, so without the re-make the second Start handed back a stream whose
// done channel was already closed — any future closer panicked.
func (es *EventStream) Start() tea.Cmd {
	es.mu.Lock()
	if es.running {
		es.mu.Unlock()
		return nil
	}
	es.running = true
	es.done = make(chan struct{})
	// Invalidate any tick armed by a previous run. A Stop cannot retract an
	// already-armed one-shot tea.Tick, so a tick from a prior generation that
	// fires after this Start must be dropped by Update rather than re-arming
	// a competing poll chain.
	es.generation++
	gen := es.generation
	es.mu.Unlock()

	// Subscribe to topics synchronously to ensure subscription is ready before polling
	es.subscribe()

	// Return command to start poll loop
	return es.schedulePoll(gen)
}

// subscribe sends subscription request to daemon.
//
// Locking: called synchronously from Start (no poll goroutine running yet)
// and from the Poll error path while inflight is held, but it mutates
// shared state (subscriptionID, lastSeq), so it takes es.mu around the
// assignments. It must NEVER be called with es.mu already held.
func (es *EventStream) subscribe() {
	if !es.rpc.IsConnected() {
		return
	}

	params := map[string]any{
		"topics": es.topics,
	}

	result, err := es.rpc.Call("bus.subscribe", params)
	if err != nil {
		return
	}

	var resp struct {
		SubscriptionID string `json:"subscription_id"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return
	}
	es.mu.Lock()
	es.subscriptionID = resp.SubscriptionID
	// A fresh subscription restarts the daemon-side seq counter at 1.
	// The cursor must reset with it, or every event on the new ring would
	// look "already seen" to the since_seq filter (C6 re-subscribe path).
	es.lastSeq = 0
	es.mu.Unlock()
}

// Stop stops the event stream. It is idempotent: the running guard is taken
// under the same mutex that closes done, so a second Stop — including one
// racing a Stop from another goroutine — returns without closing twice.
func (es *EventStream) Stop() {
	es.mu.Lock()
	if !es.running {
		es.mu.Unlock()
		return
	}

	es.running = false
	close(es.done)

	// Snapshot subscription state before releasing the lock so the RPC
	// call below does not perform I/O while holding the mutex.
	subID := es.subscriptionID
	// Clear it under the lock: a Start whose subscribe() fails (disconnected
	// socket) must not leave Poll() polling a subscription id that no longer
	// exists on the daemon.
	es.subscriptionID = ""
	es.mu.Unlock()

	// Unsubscribe if we have a subscription
	if subID != "" && es.rpc.IsConnected() {
		params := map[string]string{
			"subscription_id": subID,
		}
		_, _ = es.rpc.Call("bus.unsubscribe", params)
	}
}

// EventStreamTickMsg signals time to poll for events.
// generation is the value of EventStream.generation at the moment the tick
// was armed. Update drops ticks from a superseded generation so a tick
// armed before Stop cannot re-arm the poll chain after a fast Stop->Start.
type EventStreamTickMsg struct {
	generation uint64
}

// EventStreamDataMsg carries fetched events.
type EventStreamDataMsg struct {
	Events []BusEvent
	Err    error
	// Dropped is the daemon's in-band loss counter for this
	// subscription (bus.poll "dropped": ring-trim overflow + collector
	// channel drops). C6 (01-safety/03): loss must never be silent, so
	// the app renders a one-per-episode system notice when it is > 0.
	Dropped int64
	// Resubscribed reports that this poll recovered a lost subscription
	// (daemon restart / "subscription not found"): the cursor was reset,
	// so the app can tell the user the feed reconnected.
	Resubscribed bool
}

// schedulePoll schedules the next poll. gen is the generation captured at
// Start (or the last accepted tick); the resulting msg carries it so an
// obsolete armed tick can be identified and dropped.
func (es *EventStream) schedulePoll(gen uint64) tea.Cmd {
	return tea.Tick(es.pollInterval, func(t time.Time) tea.Msg {
		return EventStreamTickMsg{generation: gen}
	})
}

// Poll fetches new events from the daemon.
func (es *EventStream) Poll() tea.Cmd {
	return func() tea.Msg {
		es.mu.Lock()
		if !es.running {
			es.mu.Unlock()
			return nil
		}
		// A second Poll landing while one RPC is in progress is dropped:
		// without this guard two chains (e.g. a stale tick from P1 plus the
		// live one) could each fetch the same page and double-feed the
		// sidebar's recent-events ring.
		if es.inflight {
			es.mu.Unlock()
			return nil
		}
		es.inflight = true
		defer func() {
			es.mu.Lock()
			es.inflight = false
			es.mu.Unlock()
		}()
		subID := es.subscriptionID
		lastSeq := es.lastSeq
		es.mu.Unlock()

		if !es.rpc.IsConnected() || subID == "" {
			// Debug: only log periodically to avoid spam
			return EventStreamDataMsg{Events: nil, Err: nil}
		}

		params := map[string]any{
			"subscription_id": subID,
			"since_seq":       lastSeq,
		}

		result, err := es.rpc.Call("bus.poll", params)
		if err != nil {
			// C6 (01-safety/03): a lost subscription (daemon restart,
			// connection churn) is recoverable ONLY by re-running
			// subscribe(). Before the fix this error was returned and
			// Update ignored it, so the poll chain kept polling a dead
			// subscription id and the feed stayed permanently dead.
			// Re-subscribe right here: we are inside the single in-flight
			// poll goroutine and hold no lock, so subscribe() is safe to
			// call; idempotent by construction (one call per detected
			// loss, and the loss signal only exists while the id is dead).
			if isSubscriptionLostErr(err) {
				es.subscribe()
				es.mu.Lock()
				es.pendingResubscribe = true
				es.mu.Unlock()
			}
			return EventStreamDataMsg{Events: nil, Err: err}
		}

		var resp struct {
			Events  []BusEvent `json:"events"`
			NextSeq int64      `json:"next_seq"`
			Dropped int64      `json:"dropped"`
		}
		if err := json.Unmarshal(result, &resp); err != nil {
			return EventStreamDataMsg{Events: nil, Err: err}
		}

		es.mu.Lock()
		// Advance the cursor to the daemon's next_seq (monotonic server-side
		// sequence): events buffered after this poll's snapshot carry higher
		// seqs and are returned by the next poll — nothing is skipped.
		if resp.NextSeq > es.lastSeq {
			es.lastSeq = resp.NextSeq
		}
		resub := es.pendingResubscribe
		es.pendingResubscribe = false
		es.mu.Unlock()

		for _, e := range resp.Events {
			es.addToBuffer(e)
		}

		return EventStreamDataMsg{Events: resp.Events, Err: nil, Dropped: resp.Dropped, Resubscribed: resub}
	}
}

// isSubscriptionLostErr reports whether a bus.poll error means the
// daemon no longer knows this subscription ("subscription not found:
// sub-..." from internal/rpc/proxy.go handleBusPoll). That is the only
// poll failure a re-subscribe can fix; transport errors wait for the
// next ConnectSuccessMsg-driven Start instead.
func isSubscriptionLostErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "subscription not found")
}

// busEventEqual reports whether two events are the same for dedupe purposes.
// A positive daemon Seq (internal/rpc/proxy.go) is the authoritative identity:
// when either side carries one, the events match only if both share that same
// positive Seq. This keeps two legitimately identical events distinct when
// they arrive with different seq values. When Seq is unavailable (0, e.g. an
// older daemon or a hand-built event), the full field set is the fallback
// identity; Payload is compared with reflect.DeepEqual because it is an
// arbitrary decoded-JSON value that is not necessarily comparable.
func busEventEqual(a, b BusEvent) bool {
	if a.Seq > 0 || b.Seq > 0 {
		return a.Seq > 0 && a.Seq == b.Seq
	}
	return a.Topic == b.Topic &&
		a.Type == b.Type &&
		a.Source == b.Source &&
		a.Timestamp.Equal(b.Timestamp) &&
		reflect.DeepEqual(a.Payload, b.Payload)
}

// isEmptyBusEvent reports whether a circular-buffer slot is unused. Slots are
// preallocated as zero-value BusEvents, so an all-zero entry is empty; real
// events always carry at least a topic or a seq.
func isEmptyBusEvent(e BusEvent) bool {
	return e.Topic == "" && e.Type == "" && e.Source == "" && e.Seq == 0 &&
		e.Timestamp.IsZero() && e.Payload == nil
}

// addToBuffer adds an event to the circular buffer. The same event is never
// added twice: the existing ring is scanned first (O(bufferSize), 50 entries
// by default) and a duplicate returns without advancing bufferIdx.
func (es *EventStream) addToBuffer(event BusEvent) {
	es.mu.Lock()
	defer es.mu.Unlock()

	for _, existing := range es.buffer {
		if isEmptyBusEvent(existing) {
			continue
		}
		if busEventEqual(existing, event) {
			return // duplicate: do not advance bufferIdx
		}
	}

	es.buffer[es.bufferIdx] = event
	es.bufferIdx = (es.bufferIdx + 1) % es.bufferSize
}

// RecentEvents returns the most recent events from the buffer.
func (es *EventStream) RecentEvents(limit int) []BusEvent {
	es.mu.Lock()
	defer es.mu.Unlock()

	if limit <= 0 || limit > es.bufferSize {
		limit = es.bufferSize
	}

	// Collect events in reverse order (most recent first)
	events := make([]BusEvent, 0, limit)
	idx := (es.bufferIdx - 1 + es.bufferSize) % es.bufferSize

	for i := 0; i < limit && i < es.bufferSize; i++ {
		if es.buffer[idx].Topic != "" { // Non-empty event
			events = append(events, es.buffer[idx])
		}
		idx = (idx - 1 + es.bufferSize) % es.bufferSize
	}

	return events
}

// Update handles event stream messages and returns next poll command.
func (es *EventStream) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case EventStreamTickMsg:
		es.mu.Lock()
		running := es.running
		current := es.generation
		es.mu.Unlock()

		if !running {
			return nil
		}
		// A tick armed by a prior generation (before a Stop -> Start cycle)
		// is stale. Dropping it here prevents it from re-arming a second
		// poll chain alongside the current one.
		if msg.generation != current {
			return nil
		}

		// Poll and schedule next tick
		return tea.Batch(es.Poll(), es.schedulePoll(current))

	case EventStreamDataMsg:
		// Events have been added to buffer in Poll()
		// No additional action needed here
		return nil
	}
	return nil
}

// IsRunning returns whether the stream is active.
func (es *EventStream) IsRunning() bool {
	es.mu.Lock()
	defer es.mu.Unlock()
	return es.running
}

// MetricsSnapshot represents a point-in-time metrics snapshot.
type MetricsSnapshot struct {
	Timestamp    time.Time
	QueueDepth   int
	WorkersBusy  int
	WorkersIdle  int
	MemoryOps    int
	AgentsActive int
}

// MetricsCollector gathers metrics for sparklines.
type MetricsCollector struct {
	rpc          *RPCClient
	history      []MetricsSnapshot
	historySize  int
	pollInterval time.Duration
	mu           sync.Mutex
	running      bool
}

// NewMetricsCollector creates a new metrics collector.
func NewMetricsCollector(rpc *RPCClient, historySize int) *MetricsCollector {
	if historySize <= 0 {
		historySize = 60 // Default: 60 data points
	}
	return &MetricsCollector{
		rpc:          rpc,
		history:      make([]MetricsSnapshot, 0, historySize),
		historySize:  historySize,
		pollInterval: 2 * time.Second,
	}
}

// MetricsTickMsg signals time to collect metrics.
type MetricsTickMsg struct{}

// MetricsDataMsg carries collected metrics.
type MetricsDataMsg struct {
	Snapshot MetricsSnapshot
	Err      error
}

// Start begins collecting metrics.
func (mc *MetricsCollector) Start() tea.Cmd {
	mc.mu.Lock()
	mc.running = true
	mc.mu.Unlock()

	return mc.scheduleTick()
}

// Stop stops collecting metrics.
func (mc *MetricsCollector) Stop() {
	mc.mu.Lock()
	mc.running = false
	mc.mu.Unlock()
}

// scheduleTick schedules the next metrics collection.
func (mc *MetricsCollector) scheduleTick() tea.Cmd {
	return tea.Tick(mc.pollInterval, func(t time.Time) tea.Msg {
		return MetricsTickMsg{}
	})
}

// Collect gathers metrics from the daemon.
func (mc *MetricsCollector) Collect() tea.Cmd {
	return func() tea.Msg {
		if !mc.rpc.IsConnected() {
			return MetricsDataMsg{Err: nil}
		}

		snapshot := MetricsSnapshot{
			Timestamp: time.Now(),
		}

		// Get queue stats
		if stats, err := mc.rpc.GetQueueStats(); err == nil {
			if stats.ByState != nil {
				snapshot.QueueDepth = stats.ByState["pending"] + stats.ByState["claimed"]
			}
		}

		// Get worker stats
		if workers, err := mc.rpc.ListPoolWorkers(); err == nil {
			for _, w := range workers.Workers {
				switch w.State {
				case StateProcessing, "claiming":
					snapshot.WorkersBusy++
				case "idle":
					snapshot.WorkersIdle++
				}
			}
		}

		// Get active agents
		if agents, err := mc.rpc.ListWorkers(); err == nil {
			snapshot.AgentsActive = agents.Count
		}

		return MetricsDataMsg{Snapshot: snapshot, Err: nil}
	}
}

// Update handles metrics messages.
func (mc *MetricsCollector) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case MetricsTickMsg:
		mc.mu.Lock()
		running := mc.running
		mc.mu.Unlock()

		if !running {
			return nil
		}

		return tea.Batch(mc.Collect(), mc.scheduleTick())

	case MetricsDataMsg:
		if msg.Err != nil {
			return nil
		}

		mc.mu.Lock()
		mc.history = append(mc.history, msg.Snapshot)
		if len(mc.history) > mc.historySize {
			// Copy remaining elements into a fresh slice to avoid retaining
			// references to old snapshots in the underlying array (memory leak).
			tmp := make([]MetricsSnapshot, len(mc.history)-1, mc.historySize)
			copy(tmp, mc.history[1:])
			mc.history = tmp
		}
		mc.mu.Unlock()
		return nil
	}
	return nil
}

// QueueDepthHistory returns queue depth history.
func (mc *MetricsCollector) QueueDepthHistory() []int {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	result := make([]int, len(mc.history))
	for i, s := range mc.history {
		result[i] = s.QueueDepth
	}
	return result
}

// WorkersBusyHistory returns busy worker count history.
func (mc *MetricsCollector) WorkersBusyHistory() []int {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	result := make([]int, len(mc.history))
	for i, s := range mc.history {
		result[i] = s.WorkersBusy
	}
	return result
}

// AgentsActiveHistory returns active agent count history.
func (mc *MetricsCollector) AgentsActiveHistory() []int {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	result := make([]int, len(mc.history))
	for i, s := range mc.history {
		result[i] = s.AgentsActive
	}
	return result
}

// LatestSnapshot returns the most recent metrics snapshot.
func (mc *MetricsCollector) LatestSnapshot() *MetricsSnapshot {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if len(mc.history) == 0 {
		return nil
	}
	return &mc.history[len(mc.history)-1]
}
