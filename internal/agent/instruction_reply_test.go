package agent

import (
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/preferences"
	"github.com/caimlas/meept/pkg/models"
)

// TestInstructionSendResponseEchoesRequestID pins the bus reply contract:
// publish on req.ReplyTo with ReplyTo=req.ID so a proxy (or any correlating
// subscriber) can match the response. The previous helper minted a fresh ID
// and left ReplyTo empty — guaranteed timeout if ever proxied.
func TestInstructionSendResponseEchoesRequestID(t *testing.T) {
	t.Parallel()

	msgBus := bus.New(nil, slog.Default())
	defer msgBus.Close()

	h := NewInstructionHandler(
		preferences.NewUserInstructionStore(nil),
		msgBus,
		NewInstructionParser(),
		preferences.NewInstructionVerifier(nil),
		slog.Default(),
	)

	sub := msgBus.Subscribe("test-reply", "instr-reply-topic")
	defer msgBus.Unsubscribe(sub)

	req := &models.BusMessage{
		ID:      "req-abc",
		ReplyTo: "instr-reply-topic",
	}
	h.sendResponse(req, InstructionResponse{Success: true})

	select {
	case got := <-sub.Channel:
		if got.ReplyTo != "req-abc" {
			t.Errorf("ReplyTo = %q, want req-abc", got.ReplyTo)
		}
		if got.Topic != "instr-reply-topic" {
			t.Errorf("Topic = %q, want instr-reply-topic", got.Topic)
		}
		if got.Type != models.MessageTypeResponse {
			t.Errorf("Type = %q, want %q", got.Type, models.MessageTypeResponse)
		}
		var resp InstructionResponse
		if err := json.Unmarshal(got.Payload, &resp); err != nil {
			t.Fatalf("payload: %v", err)
		}
		if !resp.Success {
			t.Errorf("Success = false")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for reply")
	}
}

func TestInstructionSendResponseNilBusNoPanic(t *testing.T) {
	t.Parallel()
	h := &InstructionHandler{}
	h.sendResponse(&models.BusMessage{ID: "x", ReplyTo: "y"}, InstructionResponse{Success: true})
}
