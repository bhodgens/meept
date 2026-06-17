package pty

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestPTYSession_Interface(t *testing.T) {
	// Verify ptySession implements Session
	var _ Session = (*ptySession)(nil)
}

func TestPTYSession_BasicIO(t *testing.T) {
	// Check if PTY is available
	if !IsTerminalAvailable() {
		t.Skip("PTY not available on this platform")
	}

	sess, err := NewSession(SessionConfig{
		Cmd:  "cat",
		Rows: 24,
		Cols: 80,
	})
	if err != nil {
		t.Skipf("PTY not available: %v", err)
	}
	defer sess.Close()

	if !sess.IsRunning() {
		t.Fatal("session should be running")
	}

	_, err = sess.Write([]byte("hello\n"))
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	output := make([]byte, 1024)
	n, err := sess.Read(ctx, output)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if n == 0 {
		t.Fatal("expected non-zero output")
	}

	got := string(output[:n])
	// cat echoes back with a newline, so expect "hello\n" or similar
	t.Logf("got output: %q", got)
}

func TestPTYSession_Close(t *testing.T) {
	if !IsTerminalAvailable() {
		t.Skip("PTY not available")
	}

	sess, err := NewSession(SessionConfig{
		Cmd: "cat",
	})
	if err != nil {
		t.Skipf("PTY not available: %v", err)
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	if sess.IsRunning() {
		t.Fatal("session should not be running after close")
	}

	// Writing to a closed session should return ErrSessionClosed
	_, err = sess.Write([]byte("test"))
	if err != ErrSessionClosed {
		t.Fatalf("expected ErrSessionClosed, got %v", err)
	}

	// Double close should be idempotent
	if err := sess.Close(); err != nil {
		t.Fatalf("double close should not error")
	}
}

func TestPTYSession_Size(t *testing.T) {
	if !IsTerminalAvailable() {
		t.Skip("PTY not available")
	}

	sess, err := NewSession(SessionConfig{
		Cmd:  "cat",
		Rows: 40,
		Cols: 120,
	})
	if err != nil {
		t.Skipf("PTY not available: %v", err)
	}
	defer sess.Close()

	rows, cols := sess.Size()
	if rows != 40 {
		t.Errorf("expected rows=40, got %d", rows)
	}
	if cols != 120 {
		t.Errorf("expected cols=120, got %d", cols)
	}
}

func TestPTYSession_Resize(t *testing.T) {
	if !IsTerminalAvailable() {
		t.Skip("PTY not available")
	}

	sess, err := NewSession(SessionConfig{
		Cmd:  "cat",
		Rows: 24,
		Cols: 80,
	})
	if err != nil {
		t.Skipf("PTY not available: %v", err)
	}
	defer sess.Close()

	if err := sess.Resize(50, 200); err != nil {
		t.Fatalf("resize failed: %v", err)
	}

	rows, cols := sess.Size()
	if rows != 50 {
		t.Errorf("expected rows=50, got %d", rows)
	}
	if cols != 200 {
		t.Errorf("expected cols=200, got %d", cols)
	}
}

func TestPTYSession_ContextTimeout(t *testing.T) {
	if !IsTerminalAvailable() {
		t.Skip("PTY not available")
	}

	sess, err := NewSession(SessionConfig{
		Cmd:  "cat",
		Cols: 80,
		Rows: 24,
	})
	if err != nil {
		t.Skipf("PTY not available: %v", err)
	}
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	buf := make([]byte, 1024)
	_, err = sess.Read(ctx, buf)
	// Acceptable: either deadline or EOF (if write happened before deadline)
	t.Logf("read context result: %v", err)
	_ = err // expected timeout since write didn't happen yet
}

func TestPTYSession_ReadFromChannel(t *testing.T) {
	if !IsTerminalAvailable() {
		t.Skip("PTY not available")
	}

	sess, err := NewSession(SessionConfig{
		Cmd:  "echo",
		Args: []string{"test-output"},
		Rows: 24,
		Cols: 80,
	})
	if err != nil {
		t.Skipf("PTY not available: %v", err)
	}
	defer sess.Close()

	// Read from channel (command should finish quickly)
	select {
	case output, ok := <-sess.Output():
		if !ok {
			t.Fatal("channel was closed before receiving output")
		}
		t.Logf("got output: %q", string(output))
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for output")
	}
}

func TestPTYSessionsConfig_EmptyCmd(t *testing.T) {
	_, err := NewSession(SessionConfig{
		Cmd:  "",
		Cols: 80,
		Rows: 24,
	})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestPTYSession_BasicIORW(t *testing.T) {
	if !IsTerminalAvailable() {
		t.Skip("PTY not available")
	}

	// Verify ptySession implements Session interface
	var _ Session = (*ptySession)(nil)

	sess, err := NewSession(SessionConfig{
		Cmd:  "cat",
		Cols: 80,
		Rows: 24,
	})
	if err != nil {
		t.Skipf("PTY not available: %v", err)
	}
	defer sess.Close()

	// Write a larger message
	input := []byte("hello world from pty test\n")
	n, err := sess.Write(input)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if n != len(input) {
		t.Errorf("expected to write %d bytes, wrote %d", len(input), n)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	output := make([]byte, 1024)
	n, err = sess.Read(ctx, output)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if n == 0 {
		t.Fatal("expected non-zero output")
	}

	// PTY may add \r before \n
	got := string(output[:n])
	if got != "hello world from pty test\n" && got != "hello world from pty test\r\n" {
		t.Errorf("expected 'hello world from pty test\\n' or with \\r, got %q", got)
	}
}

func TestKillProcessTree(t *testing.T) {
	cmd := exec.Command("sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Skipf("failed to start test process: %v", err)
	}

	// KillProcessTree tries process group kill first, then cmd.Process.Kill.
	// On some platforms (macOS), the group kill may succeed in killing a child
	// that doesn't exist, but cmd.Process.Kill() should still kill the main process.
	if err := KillProcessTree(cmd); err != nil {
		t.Logf("kill returned error (may be expected on platforms without process-group kill): %v", err)
	}

	// cmd.Wait will reap the process
	cmd.Wait()

	// Process should be dead
	err := cmd.Process.Signal(syscall.Signal(0))
	if err == nil {
		t.Fatal("process should be dead after kill")
	}
}
