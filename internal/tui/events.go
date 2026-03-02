package tui

import (
	"encoding/json"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// BusEvent represents an event from the message bus.
type BusEvent struct {
	Topic     string    `json:"topic"`
	Type      string    `json:"type"` // event, request, response
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`
	Payload   any       `json:"payload"`
}

// EventStream manages real-time event streaming from the daemon's message bus.
// It uses polling since the RPC layer doesn't support server-push.
type EventStream struct {
	rpc          *RPCClient
	subscriptionID string
	topics       []string
	events       chan BusEvent
	done         chan struct{}
	mu           sync.Mutex
	running      bool
	pollInterval time.Duration
	lastPoll     time.Time
	buffer       []BusEvent // Circular buffer for recent events
	bufferSize   int
	bufferIdx    int
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
		Topics: []string{
			"agent.*",
			"task.*",
			"queue.*",
			"memory.*",
			"worker.*",
			"llm.*",
			"conversation.*",
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
		events:       make(chan BusEvent, cfg.BufferSize),
		done:         make(chan struct{}),
		pollInterval: cfg.PollInterval,
		buffer:       make([]BusEvent, cfg.BufferSize),
		bufferSize:   cfg.BufferSize,
	}
}

// Start begins polling for events.
func (es *EventStream) Start() tea.Cmd {
	es.mu.Lock()
	if es.running {
		es.mu.Unlock()
		return nil
	}
	es.running = true
	es.mu.Unlock()

	// Subscribe to topics synchronously to ensure subscription is ready before polling
	es.subscribe()

	// Return command to start poll loop
	return es.schedulePoll()
}

// subscribe sends subscription request to daemon.
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
	es.subscriptionID = resp.SubscriptionID
}

// Stop stops the event stream.
func (es *EventStream) Stop() {
	es.mu.Lock()
	defer es.mu.Unlock()

	if !es.running {
		return
	}

	es.running = false
	close(es.done)

	// Unsubscribe if we have a subscription
	if es.subscriptionID != "" && es.rpc.IsConnected() {
		params := map[string]string{
			"subscription_id": es.subscriptionID,
		}
		_, _ = es.rpc.Call("bus.unsubscribe", params)
	}
}

// EventStreamTickMsg signals time to poll for events.
type EventStreamTickMsg struct{}

// EventStreamDataMsg carries fetched events.
type EventStreamDataMsg struct {
	Events []BusEvent
	Err    error
}

// schedulePoll schedules the next poll.
func (es *EventStream) schedulePoll() tea.Cmd {
	return tea.Tick(es.pollInterval, func(t time.Time) tea.Msg {
		return EventStreamTickMsg{}
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
		subID := es.subscriptionID
		es.mu.Unlock()

		if !es.rpc.IsConnected() || subID == "" {
			// Debug: only log periodically to avoid spam
			return EventStreamDataMsg{Events: nil, Err: nil}
		}

		params := map[string]any{
			"subscription_id": subID,
			"since":           es.lastPoll.Format(time.RFC3339Nano),
		}

		result, err := es.rpc.Call("bus.poll", params)
		if err != nil {
			return EventStreamDataMsg{Events: nil, Err: err}
		}

		var resp struct {
			Events []BusEvent `json:"events"`
		}
		if err := json.Unmarshal(result, &resp); err != nil {
			return EventStreamDataMsg{Events: nil, Err: err}
		}

		es.lastPoll = time.Now()

		for _, e := range resp.Events {
			es.addToBuffer(e)
		}

		return EventStreamDataMsg{Events: resp.Events, Err: nil}
	}
}

// addToBuffer adds an event to the circular buffer.
func (es *EventStream) addToBuffer(event BusEvent) {
	es.mu.Lock()
	defer es.mu.Unlock()

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
	switch msg.(type) {
	case EventStreamTickMsg:
		es.mu.Lock()
		running := es.running
		es.mu.Unlock()

		if !running {
			return nil
		}

		// Poll and schedule next tick
		return tea.Batch(es.Poll(), es.schedulePoll())

	case EventStreamDataMsg:
		// Events have been added to buffer in Poll()
		// No additional action needed here
		return nil
	}
	return nil
}

// Events returns the event channel (for external consumers).
func (es *EventStream) Events() <-chan BusEvent {
	return es.events
}

// IsRunning returns whether the stream is active.
func (es *EventStream) IsRunning() bool {
	es.mu.Lock()
	defer es.mu.Unlock()
	return es.running
}

// MetricsSnapshot represents a point-in-time metrics snapshot.
type MetricsSnapshot struct {
	Timestamp     time.Time
	QueueDepth    int
	WorkersBusy   int
	WorkersIdle   int
	MemoryOps     int
	AgentsActive  int
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
				case "processing", "claiming":
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
			mc.history = mc.history[1:]
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
