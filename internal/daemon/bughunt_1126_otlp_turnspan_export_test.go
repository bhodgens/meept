package daemon

import (
	"bytes"
	"encoding/hex"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
)

// Bughunt 2026-11-25 A1-6: the chat.turn span never adopted the minted
// TurnSpanID. The producer reads TurnSpanSpec.TurnSpanID as the span's own
// OTLP span id (internal/metrics/otlp_trace_producer.go), and every llm.call
// hop of the turn names that same id as parent_span_id. The wiring omitted
// the field, so the exporter random-filled chat.turn's span id
// (internal/metrics/otlp_trace_exporter.go zero-value fill) and the hop's
// parent id matched NO exported span: the receiver rendered an orphaned tree
// per hop instead of one trace tree per turn.
//
// The pre-existing TestOTLPTracesWiringLLMCallHopParentsOnTurnSpan could not
// catch this: it publishes hop spans too, and it asserts only that the TURN
// SPAN BYTES appear somewhere in an export body. The hop's
// parent_span_id field carries those same bytes, so that assertion passes on
// hop traffic alone while chat.turn itself exports a random id. This test
// publishes a terminal event ONLY (no observer, no hops), so the sole source
// of the turn-span bytes in any body can be the chat.turn span's own span_id.
func TestOTLPTracesWiringTurnSpanExportsMintedSpanID(t *testing.T) {
	cap := newCaptureTraces(t)
	b := bus.New(nil, nil)
	cfg := config.OTLPTracesConfig{
		Enabled:        true,
		Endpoint:       cap.srv.URL,
		SampleRate:     1.0,
		ExportInterval: 50 * time.Millisecond,
	}
	// nil observer: no llm.call hop spans exist, so no hop's parent_span_id
	// can supply the turn-span bytes. The bytes must come from chat.turn.
	exp, _, subs, _ := wireOTLPTracesWithObserver(cfg, b, nil, slog.Default())
	if exp == nil {
		t.Fatal("enabled wiring returned nil exporter")
	}
	defer stopOTLPTraces(b, exp, subs)

	const (
		turnTraceID = "b1b2c3d4e5f60718293a4b5c6d7e8f90"
		turnSpanID  = "0a0b0c0d0e0f1011"
	)

	bus.PublishT(b, agent.TopicTurnTerminal, "test", agent.TurnTerminalEvent{
		ConversationID: "conv-turnspan-export",
		SessionID:      "session-turnspan",
		TurnID:         "turn-export-1",
		HandlerCase:    "direct",
		Status:         "completed",
		TraceID:        turnTraceID,
		TurnSpanID:     turnSpanID,
	})
	waitTraceBodies(t, cap, 1)

	rawTrace, err := hex.DecodeString(turnTraceID)
	if err != nil {
		t.Fatalf("turn trace hex: %v", err)
	}
	rawSpan, err := hex.DecodeString(turnSpanID)
	if err != nil {
		t.Fatalf("turn span hex: %v", err)
	}

	// The minted trace id identifies the chat.turn span's body.
	turnBody := -1
	for i, body := range cap.bodies {
		if bytes.Contains(body, rawTrace) {
			turnBody = i
			break
		}
	}
	if turnBody < 0 {
		t.Fatalf("no export body carries the minted chat.turn trace id %s", turnTraceID)
	}
	if !bytes.Contains(cap.bodies[turnBody], rawSpan) {
		// Wiring-failure marker (A1-6): the chat.turn span exported a
		// RANDOM-FILLED span id, so no llm.call hop can parent on the
		// turn. TurnSpanID is missing from the wiring's TurnSpanSpec.
		t.Errorf("chat.turn span does NOT carry the minted turn span id %s "+
			"(exporter random-filled it; hop spans are orphaned): %v", turnSpanID, cap.bodies[turnBody])
	}
}
