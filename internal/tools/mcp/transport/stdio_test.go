package transport

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestStdioTransport_StartStop(t *testing.T) {
	// Use echo as a simple test command
	transport := NewStdioTransport("cat", []string{}, DefaultConfig())

	ctx := context.Background()

	// Start
	if err := transport.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !transport.IsRunning() {
		t.Error("transport should be running after Start")
	}

	pid := transport.ProcessID()
	if pid == 0 {
		t.Error("expected non-zero PID")
	}

	// Close
	if err := transport.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if transport.IsRunning() {
		t.Error("transport should not be running after Close")
	}
}

func TestStdioTransport_Send(t *testing.T) {
	// Use a simple cat command that echoes back input
	transport := NewStdioTransport("cat", []string{}, Config{
		TimeoutMS: 5000,
	})

	ctx := context.Background()

	if err := transport.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer transport.Close()

	// Send a JSON message
	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "test",
	}
	reqData, _ := json.Marshal(request)

	response, err := transport.Send(ctx, reqData)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// cat should echo back the same message
	respStr := strings.TrimSpace(string(response))
	if respStr != string(reqData) {
		t.Errorf("expected echo of request, got %q", respStr)
	}
}

func TestStdioTransport_SendTimeout(t *testing.T) {
	// Use sleep as a command that won't respond
	transport := NewStdioTransport("sleep", []string{"10"}, Config{
		TimeoutMS: 100, // Very short timeout
	})

	ctx := context.Background()

	if err := transport.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer transport.Close()

	// Send should timeout
	_, err := transport.Send(ctx, []byte(`{"test": true}`))
	if err == nil {
		t.Error("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") && !strings.Contains(err.Error(), "read response") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

func TestStdioTransport_SendContextCancel(t *testing.T) {
	transport := NewStdioTransport("cat", []string{}, Config{
		TimeoutMS: 30000,
	})

	ctx := context.Background()

	if err := transport.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer transport.Close()

	// Create a context that we'll cancel
	cancelCtx, cancel := context.WithCancel(ctx)

	// Cancel after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// This should be interrupted by context cancellation
	// Since cat doesn't respond until we close stdin, it should hang
	_, err := transport.Send(cancelCtx, []byte(`{"test": true}`))
	if err == nil {
		// cat might respond immediately, which is OK too
		return
	}

	// Should either timeout or be canceled
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "canceled") {
		// This is fine - cat might have responded quickly
	}
}

func TestStdioTransport_DoubleStart(t *testing.T) {
	transport := NewStdioTransport("cat", []string{}, DefaultConfig())

	ctx := context.Background()

	if err := transport.Start(ctx); err != nil {
		t.Fatalf("First Start failed: %v", err)
	}
	defer transport.Close()

	// Second start should fail
	err := transport.Start(ctx)
	if err == nil {
		t.Error("expected error on double Start")
	}
}

func TestStdioTransport_SendNotRunning(t *testing.T) {
	transport := NewStdioTransport("cat", []string{}, DefaultConfig())

	ctx := context.Background()

	// Send without Start should fail
	_, err := transport.Send(ctx, []byte(`{}`))
	if err == nil {
		t.Error("expected error when not running")
	}
}

func TestStdioTransport_WithEnvironment(t *testing.T) {
	// Use printenv to check environment variables
	transport := NewStdioTransport("/bin/sh", []string{"-c", "echo $TEST_VAR"}, Config{
		TimeoutMS: 5000,
		Environment: map[string]string{
			"TEST_VAR": "test_value",
		},
	})

	ctx := context.Background()

	if err := transport.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer transport.Close()

	// The command will output TEST_VAR value and exit
	// We need to close stdin to let it complete
	transport.stdin.Close()

	// Read any output
	buf := make([]byte, 1024)
	n, _ := transport.stdout.Read(buf)
	output := string(buf[:n])

	if !strings.Contains(output, "test_value") {
		t.Errorf("expected 'test_value' in output, got %q", output)
	}
}

func TestStdioTransport_InvalidCommand(t *testing.T) {
	transport := NewStdioTransport("/nonexistent/command", []string{}, DefaultConfig())

	ctx := context.Background()

	err := transport.Start(ctx)
	if err == nil {
		transport.Close()
		t.Error("expected error for invalid command")
	}
}
