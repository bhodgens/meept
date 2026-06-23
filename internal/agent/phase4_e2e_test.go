package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/preferences"
	"github.com/caimlas/meept/pkg/models"
)

// TestPhase4_E2E_ParseVerifySaveExecute tests the full instruction pipeline:
// parse NL → verify → save → bus publish instruction.execute → handler
// dispatches → verify execution event published.
func TestPhase4_E2E_ParseVerifySaveExecute(t *testing.T) {
	tmpDir := t.TempDir()
	tier := filepath.Join(tmpDir, "instructions")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Create components
	store := preferences.NewUserInstructionStore([]string{tier})
	_, _ = store.Discovery()

	parser := NewInstructionParser()
	verifier := preferences.NewInstructionVerifier(nil)
	msgBus := bus.New(nil, logger)

	handler := NewInstructionHandler(store, msgBus, parser, verifier, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler.Start(ctx)
	defer handler.Stop()

	// Step 1: Parse NL input
	input := "Every day at 9am run tests in this project"
	parsed, err := parser.Parse(ctx, input)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if parsed.Trigger.Type != "cron" {
		t.Errorf("Parse() trigger type = %q, want 'cron'", parsed.Trigger.Type)
	}
	if parsed.Action.Tool != "shell_execute" {
		t.Errorf("Parse() action tool = %q, want 'shell_execute'", parsed.Action.Tool)
	}

	// Step 2: Verify
	vResult := verifier.Verify(parsed)
	if !vResult.Valid {
		t.Fatalf("Verify() invalid: %v", vResult.Errors)
	}

	// Step 3: Save to store
	instr := &preferences.UserInstruction{
		ID:         "phase4-e2e-1",
		Name:       "phase4-e2e-1",
		Trigger:    parsed.Trigger.Type + ":" + parsed.Trigger.Pattern,
		Action:     parsed.Action.Tool,
		ActionArgs: parsed.Action.Args,
		Enabled:    true,
		Scope:      parsed.Scope,
		Priority:   parsed.Priority,
		CreatedAt:  time.Now(),
	}
	if err := store.Save(instr, tier); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Verify it's in the store
	if store.Get("phase4-e2e-1") == nil {
		t.Fatal("store.Get() returned nil, expected instruction")
	}

	// Step 4: Subscribe to reply + executing topics, then publish instruction.execute
	replyTopic := "test.reply.execute." + instr.ID
	executingSub := msgBus.Subscribe("exec-monitor", "instruction.executing")
	defer msgBus.Unsubscribe(executingSub)

	// Pre-subscribe to the reply topic before publishing
	replySub := msgBus.Subscribe("test-reply", replyTopic)
	defer msgBus.Unsubscribe(replySub)

	busMsg, err := models.NewBusMessage("request", "test", map[string]any{
		"id": instr.ID,
	})
	if err != nil {
		t.Fatalf("NewBusMessage() error: %v", err)
	}
	busMsg.ReplyTo = replyTopic

	msgBus.Publish("instruction.execute", busMsg)

	// Step 5: Wait for handler response on reply topic
	select {
	case reply := <-replySub.Channel:
		var resp InstructionResponse
		if err := json.Unmarshal(reply.Payload, &resp); err != nil {
			t.Fatalf("Failed to unmarshal reply: %v", err)
		}
		if !resp.Success {
			t.Errorf("Execute response success = false, error: %s", resp.Error)
		}
		if resp.Instruction == nil {
			t.Error("Execute response instruction is nil")
		} else if resp.Instruction.ID != instr.ID {
			t.Errorf("Execute response instruction ID = %q, want %q", resp.Instruction.ID, instr.ID)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for instruction.execute reply")
	}

	// Step 6: Verify instruction.executing was published
	select {
	case execMsg := <-executingSub.Channel:
		var execData map[string]any
		if err := json.Unmarshal(execMsg.Payload, &execData); err != nil {
			t.Fatalf("Failed to unmarshal executing payload: %v", err)
		}
		if execData["action"] == nil {
			t.Error("Expected 'action' field in executing payload")
		}

	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for instruction.executing event")
	}
}

// TestPhase4_E2E_PreviewViaBus tests the preview flow via bus messaging:
// publish instruction.preview → handler parses → verify response has
// ParsedInstruction.
func TestPhase4_E2E_PreviewViaBus(t *testing.T) {
	tmpDir := t.TempDir()
	tier := filepath.Join(tmpDir, "instructions")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	store := preferences.NewUserInstructionStore([]string{tier})
	_, _ = store.Discovery()

	parser := NewInstructionParser()
	verifier := preferences.NewInstructionVerifier(nil)
	msgBus := bus.New(nil, logger)

	handler := NewInstructionHandler(store, msgBus, parser, verifier, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler.Start(ctx)
	defer handler.Stop()

	replyTopic := "test.reply.preview"
	busMsg, err := models.NewBusMessage("request", "test", map[string]any{
		"input": "Whenever I write Go files, run gofmt",
	})
	if err != nil {
		t.Fatalf("NewBusMessage() error: %v", err)
	}
	busMsg.ReplyTo = replyTopic

	// Subscribe to reply before publishing
	replySub := msgBus.Subscribe("preview-reply", replyTopic)
	defer msgBus.Unsubscribe(replySub)

	msgBus.Publish("instruction.preview", busMsg)

	select {
	case reply := <-replySub.Channel:
		var resp InstructionResponse
		if err := json.Unmarshal(reply.Payload, &resp); err != nil {
			t.Fatalf("Failed to unmarshal preview reply: %v", err)
		}
		if !resp.Success {
			t.Errorf("Preview success = false, error: %s", resp.Error)
		}
		if resp.ParsedInstruction == nil {
			t.Fatal("Preview response ParsedInstruction is nil")
		}
		// "Whenever I write Go files" should parse as post_hook
		if resp.ParsedInstruction.Trigger.Type == "" {
			t.Error("Expected non-empty trigger type in preview")
		}

	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for instruction.preview reply")
	}
}

// TestPhase4_E2E_AddListDeleteViaBus tests the CRUD lifecycle via the bus:
// add → list → delete → list (empty).
func TestPhase4_E2E_AddListDeleteViaBus(t *testing.T) {
	tmpDir := t.TempDir()
	tier := filepath.Join(tmpDir, "instructions")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	store := preferences.NewUserInstructionStore([]string{tier})
	_, _ = store.Discovery()

	parser := NewInstructionParser()
	verifier := preferences.NewInstructionVerifier(nil)
	msgBus := bus.New(nil, logger)

	handler := NewInstructionHandler(store, msgBus, parser, verifier, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler.Start(ctx)
	defer handler.Stop()

	// Step 1: Add an instruction via bus
	addReply := "test.reply.add1"
	addMsg, _ := models.NewBusMessage("request", "test", map[string]any{
		"input": "Every day at 9am run tests in this project",
		"tier":  tier,
	})
	addMsg.ReplyTo = addReply

	addSub := msgBus.Subscribe("add-sub", addReply)
	msgBus.Publish("instruction.add", addMsg)

	var addedID string
	select {
	case reply := <-addSub.Channel:
		var resp InstructionResponse
		if err := json.Unmarshal(reply.Payload, &resp); err != nil {
			t.Fatalf("Failed to unmarshal add reply: %v", err)
		}
		if !resp.Success {
			t.Fatalf("Add success = false, error: %s", resp.Error)
		}
		if resp.Instruction == nil {
			t.Fatal("Add returned nil instruction")
		}
		addedID = resp.Instruction.ID
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for instruction.add reply")
	}
	msgBus.Unsubscribe(addSub)

	// Step 2: List via bus — verify the instruction is present
	listReply := "test.reply.list1"
	listMsg, _ := models.NewBusMessage("request", "test", map[string]any{})
	listMsg.ReplyTo = listReply

	listSub := msgBus.Subscribe("list-sub", listReply)
	msgBus.Publish("instruction.list", listMsg)

	select {
	case reply := <-listSub.Channel:
		var resp InstructionResponse
		if err := json.Unmarshal(reply.Payload, &resp); err != nil {
			t.Fatalf("Failed to unmarshal list reply: %v", err)
		}
		if !resp.Success {
			t.Error("List success = false")
		}
		// At least the instruction we just added should be discoverable
		// Note: the store's in-memory map should have it from Save()
		active := store.GetActive()
		if len(active) == 0 {
			t.Error("Expected at least one active instruction after add")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for instruction.list reply")
	}
	msgBus.Unsubscribe(listSub)

	// Step 3: Delete via bus
	if addedID == "" {
		t.Fatal("No instruction ID captured from add step")
	}
	delReply := "test.reply.del1"
	delMsg, _ := models.NewBusMessage("request", "test", map[string]any{
		"id": addedID,
	})
	delMsg.ReplyTo = delReply

	delSub := msgBus.Subscribe("del-sub", delReply)
	msgBus.Publish("instruction.delete", delMsg)

	select {
	case reply := <-delSub.Channel:
		var resp InstructionResponse
		if err := json.Unmarshal(reply.Payload, &resp); err != nil {
			t.Fatalf("Failed to unmarshal delete reply: %v", err)
		}
		if !resp.Success {
			t.Errorf("Delete success = false, error: %s", resp.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for instruction.delete reply")
	}
	msgBus.Unsubscribe(delSub)

	// Step 4: Verify the instruction is gone from the store
	if store.Get(addedID) != nil {
		t.Error("Expected instruction to be deleted from store")
	}
}

// TestPhase4_E2E_ExecuteNotFound verifies that executing a non-existent
// instruction returns an error response.
func TestPhase4_E2E_ExecuteNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	tier := filepath.Join(tmpDir, "instructions")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	store := preferences.NewUserInstructionStore([]string{tier})
	_, _ = store.Discovery()

	parser := NewInstructionParser()
	verifier := preferences.NewInstructionVerifier(nil)
	msgBus := bus.New(nil, logger)

	handler := NewInstructionHandler(store, msgBus, parser, verifier, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler.Start(ctx)
	defer handler.Stop()

	replyTopic := "test.reply.notfound"
	busMsg, _ := models.NewBusMessage("request", "test", map[string]any{
		"id": "does-not-exist-xyz",
	})
	busMsg.ReplyTo = replyTopic

	replySub := msgBus.Subscribe("nf-sub", replyTopic)
	defer msgBus.Unsubscribe(replySub)

	msgBus.Publish("instruction.execute", busMsg)

	select {
	case reply := <-replySub.Channel:
		var resp InstructionResponse
		if err := json.Unmarshal(reply.Payload, &resp); err != nil {
			t.Fatalf("Failed to unmarshal reply: %v", err)
		}
		if resp.Success {
			t.Error("Expected success = false for non-existent instruction")
		}
		if resp.Error == "" {
			t.Error("Expected non-empty error message")
		}

	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for execute reply")
	}
}
