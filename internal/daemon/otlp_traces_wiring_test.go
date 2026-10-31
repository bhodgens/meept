package daemon

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/pkg/models"
)

// captureTraces counts every request hitting the test receiver and records
// the raw bodies so tests can assert presence/absence of network traffic.
type captureTraces struct {
	mu     sync.Mutex
	bodies [][]byte
	srv    *httptest.Server
}

func newCaptureTraces(t *testing.T) *captureTraces {
	c := &captureTraces{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.bodies = append(c.bodies, body)
		c.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	// Any other path is a test bug: count it as traffic too.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.bodies = append(c.bodies, body)
		c.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	c.srv = httptest.NewServer(mux)
	t.Cleanup(c.srv.Close)
	return c
}

func (c *captureTraces) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.bodies)
}

// waitTraceBodies polls until the exporter flushes at least n bodies or the
// deadline passes (the exporter flush loop / Stop-driven flush is async).
func waitTraceBodies(t *testing.T, c *captureTraces, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c.count() >= n {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("receiver saw %d export bodies, want >= %d", c.count(), n)
}

// T2 Given/When/Then: Given enabled config pointing at a capture server;
// When a turn terminal event (and a step event) flow through the bus;
// Then the server saw chat.turn with session_id attribute and task.step.
func TestOTLPTracesWiringEnabledExportsTurnAndStepSpans(t *testing.T) {
	cap := newCaptureTraces(t)
	b := bus.New(nil, nil)

	cfg := config.OTLPTracesConfig{
		Enabled:        true,
		Endpoint:       cap.srv.URL,
		SampleRate:     1.0,
		ExportInterval: 50 * time.Millisecond,
	}
	exp, _, subs := wireOTLPTraces(cfg, b, slog.Default())
	if exp == nil {
		t.Fatal("enabled wiring returned nil exporter")
	}
	defer stopOTLPTraces(b, exp, subs)

	// When: a turn terminal event flows through the typed topic (the
	// bus-driven producer path — no synchronous agent-loop hook).
	bus.PublishT(b, agent.TopicTurnTerminal, "test", agent.TurnTerminalEvent{
		ConversationID: "conv-1",
		SessionID:      "session-abc",
		TurnID:         "turn-1",
		AgentID:        "agent-1",
		HandlerCase:    "direct",
		Status:         "completed",
		Model:          "test-model",
		DurationMS:     1500,
	})

	waitTraceBodies(t, cap, 1)
	if got := cap.count(); got != 1 {
		t.Fatalf("receiver bodies = %d, want 1", got)
	}
	// The protobuf body must mention the span name and correlation fields.
	for _, want := range []string{"chat.turn", "session-abc", "agent-1", "test-model"} {
		if !bytes.Contains(cap.bodies[0], []byte(want)) {
			t.Errorf("export body missing %q", want)
		}
	}

	// And: a step event flows through the raw topic.
	b.Publish("task.step_completed", stepMsg(t, `{"task_id":"task-9","step_id":"step-9","agent_id":"agent-2","state":"completed","duration":"2s"}`))
	waitTraceBodies(t, cap, 2)
	if !bytes.Contains(cap.bodies[1], []byte("task.step")) || !bytes.Contains(cap.bodies[1], []byte("step-9")) {
		t.Error("step span missing name/step_id on the wire")
	}
}

// T2: disabled (the default) constructs NOTHING and produces ZERO network
// activity — the hard opt-in requirement.
func TestOTLPTracesWiringDisabledIsInert(t *testing.T) {
	cap := newCaptureTraces(t)
	b := bus.New(nil, nil)

	cfg := config.OTLPTracesConfig{} // Enabled=false — the shipped default
	exp, producer, subs := wireOTLPTraces(cfg, b, slog.Default())
	if exp != nil {
		t.Fatal("disabled wiring constructed an exporter")
	}
	if producer != nil {
		t.Fatal("disabled wiring constructed a producer")
	}
	if subs != nil {
		t.Fatal("disabled wiring armed bus subscriptions")
	}

	// Turn terminal events flow (as they always do) — nothing may ship.
	for i := 0; i < 5; i++ {
		bus.PublishT(b, agent.TopicTurnTerminal, "test", agent.TurnTerminalEvent{
			ConversationID: "conv-1",
			Status:         "completed",
		})
	}
	b.Publish("task.step_completed", stepMsg(t, `{"task_id":"task-1","step_id":"step-1","state":"completed"}`))

	// Give any hypothetical flush loop time to misbehave.
	time.Sleep(150 * time.Millisecond)
	if got := cap.count(); got != 0 {
		t.Fatalf("disabled boot exported %d bodies; zero network activity required", got)
	}
}

// T2: export failures never fail the turn — a dead receiver is absorbed by
// the producer path (the bus handler does not panic, the producer keeps
// recording, and the sink's bounded logging/circuit takes over).
func TestOTLPTracesExportFailureNeverBlocksTurn(t *testing.T) {
	// Receiver that accepts connections then 502s everything.
	var hits int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		http.Error(w, "down", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	b := bus.New(nil, nil)
	cfg := config.OTLPTracesConfig{
		Enabled:        true,
		Endpoint:       srv.URL,
		ExportInterval: 20 * time.Millisecond,
	}
	exp, _, subs := wireOTLPTraces(cfg, b, slog.Default())
	defer stopOTLPTraces(b, exp, subs)

	// A burst of terminal events while the receiver is down: none of these
	// may panic, block, or fail the (simulated) turn.
	for i := 0; i < 20; i++ {
		bus.PublishT(b, agent.TopicTurnTerminal, "test", agent.TurnTerminalEvent{
			ConversationID: "conv-x",
			Status:         "completed",
		})
	}
	// Wait past several flush windows; the circuit opens and drops.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		h := hits
		mu.Unlock()
		if h >= 3 { // threshold reached, circuit about to open
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// stepMsg wraps a raw payload in a BusMessage the way tactical.publishEvent
// does.
func stepMsg(t *testing.T, payload string) *models.BusMessage {
	t.Helper()
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "test", map[string]any{})
	if err != nil {
		t.Fatalf("bus message: %v", err)
	}
	msg.Payload = []byte(payload)
	return msg
}

// Silence the unused-import risk if metrics becomes unneeded in future
// edits of this file.
var _ = metrics.DefaultTraceServiceName
