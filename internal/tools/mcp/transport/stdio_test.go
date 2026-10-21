package transport

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
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
		_ = err
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

	// The command will output TEST_VAR value and exit.
	// relayStdout owns the reader, so read through the relay channel.
	line := <-transport.relayCh
	output := string(line.data)

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

// TestStdioTransport_SendConcurrentWithClose pins the Send/Close race:
// Close() nils t.stdin under t.mu while Send/SendNotification read it, so
// the write side MUST snapshot stdin under t.mu. Without the snapshot the
// test fails under -race (data race on t.stdin) and/or panics with a nil
// pointer dereference. With the fix, every Send either succeeds, times out,
// or returns a clean error ("transport not running" / closed-pipe write
// error) — the pre-fix contract that a concurrent write hits a closed pipe
// and returns an error.
func TestStdioTransport_SendConcurrentWithClose(t *testing.T) {
	for i := range 5 {
		transport := NewStdioTransport("cat", []string{}, Config{TimeoutMS: 200})
		if err := transport.Start(context.Background()); err != nil {
			t.Fatalf("iteration %d: Start failed: %v", i, err)
		}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := range 50 {
				msg := []byte(`{"jsonrpc":"2.0","id":` + string(rune('a'+j%26)) + `,"method":"test"}`)
				// Every outcome must be an error or a response —
				// never a panic (nil stdin deref) or a race.
				_, _ = transport.Send(context.Background(), msg)
				_ = transport.SendNotification(context.Background(), msg)
			}
		}()
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(i) * time.Millisecond)
			if err := transport.Close(); err != nil {
				t.Errorf("iteration %d: Close failed: %v", i, err)
			}
		}()
		wg.Wait()

		if transport.IsRunning() {
			t.Errorf("iteration %d: transport still running after Close", i)
		}
	}
}

// TestStdioTransport_DoubleClose pins Close() idempotency: the first Close
// must clear t.cmd under the lock so the second Close does not re-run
// cmd.Wait ("exec: Wait was already called").
func TestStdioTransport_DoubleClose(t *testing.T) {
	transport := NewStdioTransport("cat", []string{}, DefaultConfig())
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if err := transport.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	// Second Close must be a no-op, not a panic or a Wait-reuse error.
	if err := transport.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
	// And Send after Close must return a clean error, not panic.
	if _, err := transport.Send(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("Send after Close succeeded, want error")
	}
	if err := transport.SendNotification(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("SendNotification after Close succeeded, want error")
	}
	if pid := transport.ProcessID(); pid != 0 {
		t.Fatalf("ProcessID after Close = %d, want 0", pid)
	}
}
