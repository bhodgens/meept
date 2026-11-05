package daemon

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	appmetrics "github.com/caimlas/meept/internal/metrics"
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
	exp, _, subs := wireOTLPTraces(cfg, b, nil, slog.Default())
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
	exp, producer, subs := wireOTLPTraces(cfg, b, nil, slog.Default())
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
	exp, _, subs := wireOTLPTraces(cfg, b, nil, slog.Default())
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

// Silence the unused-import risk if the metrics package becomes unneeded in
// future edits of this file.
var _ = appmetrics.DefaultTraceServiceName

// ---------------------------------------------------------------------------
// Leaf 03 (llm-hop-tracing): observer bridge + tool-execution-end wiring.
// ---------------------------------------------------------------------------

// T2(a): disabled config attaches NO observer and logs exactly one disabled
// Warn line.
func TestOTLPTracesWiringDisabledNoObserver(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	b := bus.New(nil, nil)
	cfg := config.OTLPTracesConfig{} // disabled - the shipped default
	exp, producer, subs, obs := wireOTLPTracesWithObserver(cfg, b, nil, logger)
	if exp != nil || producer != nil || subs != nil || obs != nil {
		t.Fatal("disabled wiring constructed pipeline pieces or attached an observer")
	}
	warns := 0
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "OTLP trace export disabled") {
			warns++
		}
	}
	if warns != 1 {
		t.Errorf("disabled Warn lines = %d, want exactly 1", warns)
	}
}

// T2(b): enabled config attaches an observer; a produced LLMCallRecord
// routed through it results in a queued llm.call span with the record's
// decoded values.
func TestOTLPTracesWiringObserverProducesLLMCallSpan(t *testing.T) {
	cap := newCaptureTraces(t)
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	b := bus.New(nil, nil)
	cfg := config.OTLPTracesConfig{
		Enabled:        true,
		Endpoint:       cap.srv.URL,
		SampleRate:     1.0,
		ExportInterval: 50 * time.Millisecond,
	}
	exp, _, subs, obs := wireOTLPTracesWithObserver(cfg, b, nil, logger)
	if exp == nil || obs == nil {
		t.Fatal("enabled wiring returned nil exporter or nil observer")
	}
	defer stopOTLPTraces(b, exp, subs)
	infos := 0
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "OTLP trace export enabled") {
			infos++
		}
	}
	if infos != 1 {
		t.Errorf("enabled Info lines = %d, want exactly 1", infos)
	}

	// When: one completed LLM call flows through the observer (the
	// llm-call-record -> span bridge; no synchronous llm-path hook).
	obs(appmetrics.LLMCallRecord{
		Timestamp:  time.Now(),
		Provider:   "openai",
		ModelID:    "test-model",
		AgentID:    "agent-1",
		SessionID:  "session-abc",
		TokensSent: 52, TokensRecv: 47,
		LatencyMs: 1200,
		TraceID:   "a1b2c3d4e5f60123456789abcdef0123",
		SpanID:    "b1b2b3b4b5b6b7b8",
	})
	waitTraceBodies(t, cap, 1)
	if !bytes.Contains(cap.bodies[0], []byte("llm.call")) ||
		!bytes.Contains(cap.bodies[0], []byte("test-model")) {
		t.Error("llm.call span missing name/model on the wire")
	}
}

// T2(c): a bus message on agent.event.tool_execution_end (AgentEvent JSON
// envelope carrying ToolExecutionEndData) yields one tool.call span with
// the decoded values; a malformed envelope neither panics nor emits.
func TestOTLPTracesWiringToolExecutionEndSpan(t *testing.T) {
	cap := newCaptureTraces(t)
	b := bus.New(nil, nil)
	cfg := config.OTLPTracesConfig{
		Enabled:        true,
		Endpoint:       cap.srv.URL,
		SampleRate:     1.0,
		ExportInterval: 50 * time.Millisecond,
	}
	exp, _, subs, _ := wireOTLPTracesWithObserver(cfg, b, nil, slog.Default())
	defer stopOTLPTraces(b, exp, subs)

	// Build the AgentEvent envelope the emitter bridge publishes (Payload =
	// serialized agent.AgentEvent JSON; Data holds ToolExecutionEndData).
	env := map[string]any{
		"type":            "tool_execution_end",
		"agent_id":        "agent-1",
		"conversation_id": "conv-1",
		"data": map[string]any{
			"tool_call_id":    "call-9",
			"tool_name":       "read_file",
			"success":         true,
			"trace_id":        "a1b2c3d4e5f60123456789abcdef0123",
			"span_id":         "d1d2d3d4d5d6d7d8",
			"arguments_keys":  "path,token",
			"arguments_bytes": 38,
		},
	}
	payload, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("envelope marshal: %v", err)
	}
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "test", map[string]any{})
	if err != nil {
		t.Fatalf("bus message: %v", err)
	}
	msg.Payload = payload
	b.Publish("agent.event.tool_execution_end", msg)
	waitTraceBodies(t, cap, 1)
	if !bytes.Contains(cap.bodies[0], []byte("tool.call")) ||
		!bytes.Contains(cap.bodies[0], []byte("read_file")) ||
		!bytes.Contains(cap.bodies[0], []byte("call-9")) {
		t.Error("tool.call span missing name/tool identity on the wire")
	}

	// Malformed envelope: no panic, no span.
	for _, bad := range []string{`{not json`, `{"type":"other"}`, `[]`} {
		m2, _ := models.NewBusMessage(models.MessageTypeEvent, "test", map[string]any{})
		m2.Payload = []byte(bad)
		b.Publish("agent.event.tool_execution_end", m2)
	}
	time.Sleep(150 * time.Millisecond)
	if got := cap.count(); got > 2 {
		t.Errorf("malformed envelopes produced spans (bodies = %d)", got)
	}
}

// T2(d): an enabled wiring's turn event carrying trace_id produces a
// chat.turn span with that trace id on the wire.
func TestOTLPTracesWiringTurnSpanCarriesTraceID(t *testing.T) {
	cap := newCaptureTraces(t)
	b := bus.New(nil, nil)
	cfg := config.OTLPTracesConfig{
		Enabled:        true,
		Endpoint:       cap.srv.URL,
		SampleRate:     1.0,
		ExportInterval: 50 * time.Millisecond,
	}
	exp, _, subs, _ := wireOTLPTracesWithObserver(cfg, b, nil, slog.Default())
	defer stopOTLPTraces(b, exp, subs)

	bus.PublishT(b, agent.TopicTurnTerminal, "test", agent.TurnTerminalEvent{
		ConversationID: "conv-trace",
		Status:         "completed",
		TraceID:        "a1b2c3d4e5f60123456789abcdef0123",
	})
	waitTraceBodies(t, cap, 1)
	// The trace id rides the OTLP protobuf as its 16 raw bytes.
	rawTrace, err := hex.DecodeString("a1b2c3d4e5f60123456789abcdef0123")
	if err != nil {
		t.Fatalf("trace hex: %v", err)
	}
	if !bytes.Contains(cap.bodies[0], rawTrace) {
		t.Error("chat.turn span missing the minted trace id on the wire")
	}
}

// ---------------------------------------------------------------------------
// Leaf 06 (wiring-gap-remediation): TurnSpanID parent linkage.
// ---------------------------------------------------------------------------

// T3 wiring-failure proof (leaf 06 / R6 "hop spans export with empty
// ParentSpanID"):
//
//	Given: an enabled trace pipeline and a terminal event carrying the
//	       minted trace+turn-span pair;
//	When:  an LLM-call hop for the SAME trace flows through the observer
//	       bridge;
//	Then:  the exported llm.call span's ParentSpanID wire bytes equal the
//	       turn span id.
//
// Wiring-failure property: deleting the TurnSpanID field on
// agent.TurnTerminalEvent (or the trace->turnSpan propagation in the turn
// subscription) leaves the parent empty -- the probe hop never lands with
// the turn-span parent bytes and the test FAILS at the marked assertion
// (witnessed live: with the pair propagation removed, this test reported
// "llm.call hop exported WITHOUT the turn-span parent").
//
// The FIRST hop is observed BEFORE the terminal (real export order --
// children-before-parent); its parent resolution happens at observe time,
// so it carries no assertion. A trailing hop AFTER the terminal must
// still parent on the same turn span: the receiver keys a trace->turnspan
// MAP looked up at hop time, not a last-seen-terminal heuristic -- and
// keeps late-terminal traces rather than resetting on the terminal.
//
// Export sequencing is driven by explicit exp.Flush() calls (the started
// exporter's periodic ticker is irrelevant here). Body composition is
// timing-dependent, so every assertion scans bodies for the span whose
// 8-byte span id is unique to it.
func TestOTLPTracesWiringLLMCallHopParentsOnTurnSpan(t *testing.T) {
	cap := newCaptureTraces(t)
	b := bus.New(nil, nil)
	cfg := config.OTLPTracesConfig{
		Enabled:        true,
		Endpoint:       cap.srv.URL,
		SampleRate:     1.0,
		ExportInterval: 50 * time.Millisecond,
	}
	exp, _, subs, obs := wireOTLPTracesWithObserver(cfg, b, nil, slog.Default())
	if exp == nil || obs == nil {
		t.Fatal("enabled wiring returned nil exporter or nil observer")
	}
	defer stopOTLPTraces(b, exp, subs)

	const (
		traceID   = "a1b2c3d4e5f60123456789abcdef0123"
		turnSpan  = "0a0b0c0d0e0f1011"
		llmSpanID = "b1b2b3b4b5b6b7b8"
	)
	rawTurnSpan, err := hex.DecodeString(turnSpan)
	if err != nil {
		t.Fatalf("turn span hex: %v", err)
	}

	// When FIRST: the mid-turn hop arrives before its terminal. Drained
	// immediately; no assertion (its parent resolution raced the pair by
	// construction).
	obs(appmetrics.LLMCallRecord{
		Timestamp: time.Now(),
		Provider:  "openai",
		ModelID:   "test-model",
		AgentID:   "agent-1",
		SessionID: "session-abc",
		LatencyMs: 300,
		TraceID:   traceID,
		SpanID:    llmSpanID,
	})
	exp.Flush()

	// And THEN: the terminal for the same trace carries the minted pair.
	bus.PublishT(b, agent.TopicTurnTerminal, "test", agent.TurnTerminalEvent{
		ConversationID: "conv-turnspan",
		SessionID:      "session-abc",
		TurnID:         "turn-ts-1",
		HandlerCase:    "direct",
		Status:         "completed",
		TraceID:        traceID,
		TurnSpanID:     turnSpan,
	})
	// The typed subscription decodes on its own goroutine; wait for the
	// pair to be recorded by probing with a canary hop -- the same lookup
	// production uses -- until it lands parented.
	rawProbe, err := hex.DecodeString("e1e2e3e4e5e6e7e8")
	if err != nil {
		t.Fatalf("probe span hex: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	pairSeen := false
	for time.Now().Before(deadline) && !pairSeen {
		obs(appmetrics.LLMCallRecord{
			Timestamp: time.Now(),
			Provider:  "probe",
			ModelID:   "pair-probe",
			LatencyMs: 1,
			TraceID:   traceID,
			SpanID:    "e1e2e3e4e5e6e7e8",
		})
		exp.Flush()
		for _, body := range cap.bodies {
			if bytes.Contains(body, rawProbe) && bytes.Contains(body, rawTurnSpan) {
				pairSeen = true
				break
			}
		}
		if !pairSeen {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !pairSeen {
		// Wiring-failure proof marker: the propagation is gone (field
		// deleted / map write removed) -- every hop exports unparented.
		t.Errorf("llm.call hop exported WITHOUT the turn-span parent (%s): parent linkage wiring removed", turnSpan)
	}

	// A trailing hop AFTER the terminal must still parent on the same
	// turn span (the map keeps late-terminal traces; no reset on the
	// terminal).
	obs(appmetrics.LLMCallRecord{
		Timestamp: time.Now(),
		Provider:  "openai",
		ModelID:   "test-model",
		AgentID:   "agent-1",
		SessionID: "session-abc",
		LatencyMs: 100,
		TraceID:   traceID,
		SpanID:    "d1d2d3d4d5d6d7d8",
	})
	exp.Flush()
	rawTrailing, err := hex.DecodeString("d1d2d3d4d5d6d7d8")
	if err != nil {
		t.Fatalf("trailing hop span hex: %v", err)
	}
	for _, body := range cap.bodies {
		if bytes.Contains(body, rawTrailing) && !bytes.Contains(body, rawTurnSpan) {
			t.Error("post-terminal hop exported WITHOUT the turn-span parent (map reset on terminal?)")
			break
		}
	}

	// A hop under an UNKNOWN trace exports unparented (no cross-trace
	// leakage): its body must not carry the turn-span bytes.
	obs(appmetrics.LLMCallRecord{
		Timestamp: time.Now(),
		Provider:  "openai",
		ModelID:   "test-model",
		SessionID: "session-other",
		LatencyMs: 50,
		TraceID:   "f0e0d0c0b0a090807060504030201000",
		SpanID:    "c1c2c3c4c5c6c7c8",
	})
	exp.Flush()
	rawUnk, err := hex.DecodeString("c1c2c3c4c5c6c7c8")
	if err != nil {
		t.Fatalf("unknown hop span hex: %v", err)
	}
	unkFound := false
	for _, body := range cap.bodies {
		if bytes.Contains(body, rawUnk) {
			unkFound = true
			if bytes.Contains(body, rawTurnSpan) {
				t.Error("unknown-trace hop inherited the turn-span parent (cross-trace leakage)")
			}
		}
	}
	if !unkFound {
		t.Fatal("unknown-trace hop never exported")
	}
}
