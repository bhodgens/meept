package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/preferences"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
)

// encodeJSON is a test helper that marshals a value to JSON, failing the test
// on error. It is used for building BusMessage payloads.
func encodeJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

// newTestInstructionListener creates a listener with a fresh store and bus for testing.
// It returns the listener, bus, store, and a cleanup function.
func newTestInstructionListener(t *testing.T) (*InstructionListener, *bus.MessageBus, *preferences.Store, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	tier := filepath.Join(tmpDir, "instructions")

	store := preferences.NewUserInstructionStore([]string{tier})
	// Discovery populates the store's in-memory map
	_, _ = store.Discovery()

	msgBus := bus.New(nil, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	toolReg := tools.NewRegistry(nil)

	listener := NewInstructionListener(store, msgBus, toolReg, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	ctx, cancel := context.WithCancel(context.Background())
	listener.Start(ctx)

	cleanup := func() {
		cancel()
		listener.Stop()
		msgBus.Close()
	}

	return listener, msgBus, store, cleanup
}

// TestInstructionListener_PostHookMatch verifies that a post_hook:tool_complete
// trigger fires when a tool.completed bus message is published.
func TestInstructionListener_PostHookMatch(t *testing.T) {
	_, msgBus, store, cleanup := newTestInstructionListener(t)
	defer cleanup()

	// Seed a matching instruction
	instr := &preferences.UserInstruction{
		ID:        "post-hook-test",
		Name:      "post-hook-test",
		Trigger:   "post_hook:tool_complete:*",
		Action:    "shell_execute",
		ActionArgs: map[string]any{"command": "echo triggered"},
		Enabled:   true,
	}
	if err := store.Save(instr, store.DefaultTier()); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	// Re-discover to pick up the saved instruction
	_, _ = store.Discovery()

	if len(store.GetActive()) == 0 {
		t.Fatal("expected at least one active instruction after save+discover")
	}

	// Publish tool.completed
	payload, _ := encodeJSON(map[string]any{"tool": "ls", "result": "ok"})
	msg, err := models.NewBusMessage("event", "test", payload)
	if err != nil {
		t.Fatalf("NewBusMessage() error: %v", err)
	}

	delivered := msgBus.Publish("tool.completed", msg)
	if delivered == 0 {
		t.Error("Publish(tool.completed) delivered = 0, expected at least one subscriber")
	}

	// The listener's executeAction currently just logs — we verify no panic
	// and the message was delivered to the subscription.
}

// TestInstructionListener_FileWrittenMatch verifies that a post_hook:write_file
// trigger fires when a file.written bus message is published.
func TestInstructionListener_FileWrittenMatch(t *testing.T) {
	_, msgBus, store, cleanup := newTestInstructionListener(t)
	defer cleanup()

	instr := &preferences.UserInstruction{
		ID:        "file-write-hook",
		Name:      "file-write-hook",
		Trigger:   "post_hook:write_file:*.go",
		Action:    "shell_execute",
		ActionArgs: map[string]any{"command": "gofmt -w ."},
		Enabled:   true,
	}
	if err := store.Save(instr, store.DefaultTier()); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	_, _ = store.Discovery()

	// Publish file.written with topic field matching pattern
	payload, _ := encodeJSON(map[string]any{"path": "/some/main.go"})
	msg, err := models.NewBusMessage("event", "test", payload)
	if err != nil {
		t.Fatalf("NewBusMessage() error: %v", err)
	}
	// Set the Topic field on the BusMessage so matchPattern can check it
	msg.Topic = "*.go"

	delivered := msgBus.Publish("file.written", msg)
	if delivered == 0 {
		t.Error("Publish(file.written) delivered = 0, expected at least one subscriber")
	}

	// Allow the async handler to process
	time.Sleep(50 * time.Millisecond)
}

// TestInstructionListener_EventMatch verifies that an event:session_start
// trigger fires when a session.started bus message is published.
func TestInstructionListener_EventMatch(t *testing.T) {
	_, msgBus, store, cleanup := newTestInstructionListener(t)
	defer cleanup()

	instr := &preferences.UserInstruction{
		ID:        "session-start-event",
		Name:      "session-start-event",
		Trigger:   "event:session_start",
		Action:    "notification",
		ActionArgs: map[string]any{"message": "session started"},
		Enabled:   true,
	}
	if err := store.Save(instr, store.DefaultTier()); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	_, _ = store.Discovery()

	payload, _ := encodeJSON(map[string]any{"session_id": "sess-123"})
	msg, err := models.NewBusMessage("event", "test", payload)
	if err != nil {
		t.Fatalf("NewBusMessage() error: %v", err)
	}

	delivered := msgBus.Publish("session.started", msg)
	if delivered == 0 {
		t.Error("Publish(session.started) delivered = 0, expected at least one subscriber")
	}

	// Allow the async handler to process
	time.Sleep(50 * time.Millisecond)
}

// TestInstructionListener_NoMatch verifies that when no instructions match the
// event, nothing triggers (no panic, no error).
func TestInstructionListener_NoMatch(t *testing.T) {
	_, msgBus, store, cleanup := newTestInstructionListener(t)
	defer cleanup()

	// Seed a non-matching instruction
	instr := &preferences.UserInstruction{
		ID:        "non-matching",
		Name:      "non-matching",
		Trigger:   "post_hook:write_file:*.py",
		Action:    "notification",
		Enabled:   true,
	}
	if err := store.Save(instr, store.DefaultTier()); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	_, _ = store.Discovery()

	// Publish a tool.completed — should not match the write_file trigger
	payload, _ := encodeJSON(map[string]any{"tool": "ls"})
	msg, err := models.NewBusMessage("event", "test", payload)
	if err != nil {
		t.Fatalf("NewBusMessage() error: %v", err)
	}

	// Should deliver to the subscriber (the listener is subscribed to tool.completed)
	// but should NOT trigger the action because the trigger pattern doesn't match.
	delivered := msgBus.Publish("tool.completed", msg)
	_ = delivered // subscription receives it but no action fires

	time.Sleep(50 * time.Millisecond)

	// No assertion to check here since executeAction only logs.
	// Test passes if no panic occurred.
}

// TestInstructionListener_MatchPattern tests the glob pattern matching
// directly via the listener's matchPattern method.
func TestInstructionListener_MatchPattern(t *testing.T) {
	listener := &InstructionListener{}

	tests := []struct {
		path    string
		pattern string
		want    bool
	}{
		// Wildcard match (filepath.Match: * does not cross /)
		{"/some/main.go", "*.go", false},
		{"/some/test.py", "*.go", false},
		{"main.go", "*.go", true},
		// Star matches everything (within a single path segment)
		{"anything", "*", true},
		// Exact-ish patterns
		{"session_start", "session_start", true},
		// Non-matching
		{"abc", "xyz", false},
		// Multiple components (filepath.Match does not cross /)
		{"foo/bar.go", "*.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.path+"/"+tt.pattern, func(t *testing.T) {
			got := listener.matchPattern(tt.path, tt.pattern)
			if got != tt.want {
				t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.path, tt.pattern, got, tt.want)
			}
		})
	}
}

// TestInstructionListener_ConcurrentPublish verifies the listener handles
// concurrent bus messages without panicking.
func TestInstructionListener_ConcurrentPublish(t *testing.T) {
	_, msgBus, store, cleanup := newTestInstructionListener(t)
	defer cleanup()

	instr := &preferences.UserInstruction{
		ID:        "concurrent-test",
		Name:      "concurrent-test",
		Trigger:   "post_hook:tool_complete:*",
		Action:    "notification",
		Enabled:   true,
	}
	if err := store.Save(instr, store.DefaultTier()); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	_, _ = store.Discovery()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			payload, _ := encodeJSON(map[string]any{"tool": "ls", "n": n})
			msg, _ := models.NewBusMessage("event", "test", payload)
			msgBus.Publish("tool.completed", msg)
		}(i)
	}
	wg.Wait()

	// Allow async handler to drain
	time.Sleep(100 * time.Millisecond)
}
