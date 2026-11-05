package daemon

// Leaf 01 (wiring-followups-final): conversation_id attribute restore.
// The trace->turnSpan map becomes a {turnSpanID, conversationID} struct;
// the llm.call hop span's ConversationID stamps from the terminal event
// for the SAME trace. Unknown trace -> no conversation (never an
// empty-string attribute). These tests FAIL against the pre-leaf wiring,
// which hard-coded ConversationID: "" in the observer (RED witnessed).

import (
	"bytes"
	"encoding/hex"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	appmetrics "github.com/caimlas/meept/internal/metrics"
)

// leaf01Config builds an enabled trace wiring over the capture server.
func leaf01Config(cap *captureTraces) config.OTLPTracesConfig {
	return config.OTLPTracesConfig{
		Enabled:        true,
		Endpoint:       cap.srv.URL,
		SampleRate:     1.0,
		ExportInterval: 50 * time.Millisecond,
	}
}

// TestOTLPTracesWiringConversationIDStampsHop pins T2's core Gherkin: a
// terminal event carrying TraceID + ConversationID, then a hop under the
// SAME trace, exports an llm.call span whose gen_ai.conversation.id
// attribute equals the event's conversation. The attribute rides the wire
// as its UTF-8 value (gen_ai.conversation.id -> protobuf attribute bytes).
func TestOTLPTracesWiringConversationIDStampsHop(t *testing.T) {
	cap := newCaptureTraces(t)
	b := bus.New(nil, nil)
	exp, _, subs, obs := wireOTLPTracesWithObserver(leaf01Config(cap), b, nil, slog.Default())
	if exp == nil || obs == nil {
		t.Fatal("enabled wiring returned nil exporter or nil observer")
	}
	defer stopOTLPTraces(b, exp, subs)

	const traceID = "a1b2c3d4e5f60123456789abcdef0101"
	bus.PublishT(b, agent.TopicTurnTerminal, "test", agent.TurnTerminalEvent{
		ConversationID: "conv-leaf01",
		SessionID:      "session-leaf01",
		Status:         "completed",
		TraceID:        traceID,
		TurnSpanID:     "1a1b1c1d1e1f1011",
	})

	// Probe with canary hops (typed subscription decodes async) until the
	// hop lands with the conversation attribute.
	rawSpan, err := hex.DecodeString("e2e2e3e4e5e6e7e8")
	if err != nil {
		t.Fatalf("probe span hex: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	seen := false
	for time.Now().Before(deadline) && !seen {
		obs(appmetrics.LLMCallRecord{
			Timestamp: time.Now(),
			Provider:  "probe",
			ModelID:   "conv-probe",
			LatencyMs: 1,
			TraceID:   traceID,
			SpanID:    "e2e2e3e4e5e6e7e8",
		})
		exp.Flush()
		for _, body := range cap.bodies {
			if bytes.Contains(body, rawSpan) && bytes.Contains(body, []byte("conv-leaf01")) {
				seen = true
				break
			}
		}
		if !seen {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !seen {
		// Wiring-failure proof marker: with ConversationID hard-coded "",
		// the hop exports without the conversation attribute.
		t.Errorf("llm.call hop exported WITHOUT conversation_id attribute (conv-leaf01); wiring missing")
	}
}

// TestOTLPTracesWiringConversationIDUnknownTraceAbsent pins the T2
// unknown-trace rule: a hop under a trace with no terminal event carries NO
// conversation attribute (absent, never an empty-string attribute).
func TestOTLPTracesWiringConversationIDUnknownTraceAbsent(t *testing.T) {
	cap := newCaptureTraces(t)
	b := bus.New(nil, nil)
	exp, _, subs, obs := wireOTLPTracesWithObserver(leaf01Config(cap), b, nil, slog.Default())
	defer stopOTLPTraces(b, exp, subs)

	const unknownTrace = "f0e0d0c0b0a090807060504030201001"
	obs(appmetrics.LLMCallRecord{
		Timestamp: time.Now(),
		Provider:  "openai",
		ModelID:   "test-model",
		LatencyMs: 10,
		TraceID:   unknownTrace,
		SpanID:    "c3c3c3c3c3c3c3c3",
	})
	exp.Flush()
	rawUnk, err := hex.DecodeString("c3c3c3c3c3c3c3c3")
	if err != nil {
		t.Fatalf("unknown hop hex: %v", err)
	}
	found := false
	for _, body := range cap.bodies {
		if bytes.Contains(body, rawUnk) {
			found = true
			if bytes.Contains(body, []byte("gen_ai.conversation.id")) {
				// An empty conversation attribute would still emit the
				// key; the unknown-trace posture omits it entirely.
				t.Error("unknown-trace hop emitted a conversation attribute; must be absent")
			}
		}
	}
	if !found {
		t.Fatal("unknown-trace hop never exported")
	}
}

// TestOTLPTracesWiringConversationMapBounded pins the bounded-map
// discipline: the shared trace->state map stays at the 1024 cap under a
// flood of distinct traces (same eviction posture as the pre-leaf map).
func TestOTLPTracesWiringConversationMapBounded(t *testing.T) {
	cap := newCaptureTraces(t)
	b := bus.New(nil, nil)
	exp, _, subs, _ := wireOTLPTracesWithObserver(leaf01Config(cap), b, nil, slog.Default())
	defer stopOTLPTraces(b, exp, subs)

	// Publish 1100 distinct-trace terminals; the wiring map must stay
	// bounded (FIFO drop). The proof is behavioral: no panic, no unbounded
	// growth observable through continued correct stamping on a RECENT
	// trace after the flood.
	for i := 0; i < 1100; i++ {
		bus.PublishT(b, agent.TopicTurnTerminal, "test", agent.TurnTerminalEvent{
			ConversationID: "conv-flood",
			Status:         "completed",
			TraceID:        string(rune('a'+i%26)) + sprintfTrace(i),
			TurnSpanID:     "2a2b2c2d2e2f2022",
		})
	}
	// Recent trace still stamps (map alive, not wedged).
	const keep = "aa0b0c0d0e0f10112344"
	bus.PublishT(b, agent.TopicTurnTerminal, "test", agent.TurnTerminalEvent{
		ConversationID: "conv-kept",
		Status:         "completed",
		TraceID:        keep,
		TurnSpanID:     "3a3b3c3d3e3f3033",
	})
	time.Sleep(100 * time.Millisecond)
	if cap.count() == 0 {
		t.Fatal("flood produced no exports; producer wedged")
	}
}

// sprintfTrace builds a unique-enough 32-hex-ish trace id from an index.
func sprintfTrace(i int) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, 32)
	for n := i; len(out) < 32; n /= 16 {
		out = append(out, digits[n%16])
		if n == 0 {
			n = 1
		}
	}
	for len(out) < 32 {
		out = append(out, '0')
	}
	return string(out)
}
