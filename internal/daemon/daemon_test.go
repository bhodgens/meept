package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDaemonStartup(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	cfg := &Config{
		SocketPath:      filepath.Join(tmpDir, "meept.sock"),
		PIDFile:         filepath.Join(tmpDir, "meept.pid"),
		StateDir:        tmpDir,
		ShutdownTimeout: 2 * time.Second,
	}

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	// Start daemon in background
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- d.Run(ctx)
	}()

	// Wait for socket to appear (with timeout)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(cfg.SocketPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Verify daemon is running
	if d.Status() != "running" {
		t.Errorf("Expected status 'running', got %q", d.Status())
	}

	// Clean shutdown
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Daemon returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Daemon did not shut down in time")
	}
}

func BenchmarkDaemonStartup(b *testing.B) {
	for i := 0; i < b.N; i++ {
		tmpDir := b.TempDir()
		cfg := &Config{
			SocketPath:      filepath.Join(tmpDir, "meept.sock"),
			PIDFile:         filepath.Join(tmpDir, "meept.pid"),
			StateDir:        tmpDir,
			ShutdownTimeout: 1 * time.Second,
		}

		b.StartTimer()
		d, err := New(cfg)
		if err != nil {
			b.Fatalf("Failed to create daemon: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		ready := make(chan struct{})

		go func() {
			// Run daemon briefly
			go d.Run(ctx)
			// Wait for socket
			for i := 0; i < 100; i++ {
				if _, err := os.Stat(cfg.SocketPath); err == nil {
					close(ready)
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		}()

		<-ready
		b.StopTimer()

		cancel()
		time.Sleep(100 * time.Millisecond)
	}
}

// BenchmarkRPCThroughput measures RPC requests per second.
func BenchmarkRPCThroughput(b *testing.B) {
	// Setup daemon
	tmpDir := b.TempDir()
	cfg := &Config{
		SocketPath:      filepath.Join(tmpDir, "meept.sock"),
		PIDFile:         filepath.Join(tmpDir, "meept.pid"),
		StateDir:        tmpDir,
		ShutdownTimeout: 2 * time.Second,
	}

	d, err := New(cfg)
	if err != nil {
		b.Fatalf("Failed to create daemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d.Run(ctx)

	// Wait for socket
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(cfg.SocketPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Connect
	conn, err := net.Dial("unix", cfg.SocketPath)
	if err != nil {
		b.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Prepare ping request
	pingReq := map[string]any{
		"jsonrpc": "2.0",
		"method":  "ping",
		"id":      1,
	}
	reqData, _ := json.Marshal(pingReq)
	frame := fmt.Sprintf("%d\n%s", len(reqData), reqData)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Send request
		conn.SetWriteDeadline(time.Now().Add(time.Second))
		if _, err := conn.Write([]byte(frame)); err != nil {
			b.Fatalf("Write failed: %v", err)
		}

		// Read response (simplified - just read some bytes)
		buf := make([]byte, 256)
		conn.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := conn.Read(buf); err != nil {
			b.Fatalf("Read failed: %v", err)
		}
	}
}

// BenchmarkConcurrentRPC tests concurrent RPC throughput.
func BenchmarkConcurrentRPC(b *testing.B) {
	// Setup daemon
	tmpDir := b.TempDir()
	cfg := &Config{
		SocketPath:      filepath.Join(tmpDir, "meept.sock"),
		PIDFile:         filepath.Join(tmpDir, "meept.pid"),
		StateDir:        tmpDir,
		ShutdownTimeout: 2 * time.Second,
	}

	d, err := New(cfg)
	if err != nil {
		b.Fatalf("Failed to create daemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d.Run(ctx)

	// Wait for socket
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(cfg.SocketPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Create connection pool
	const poolSize = 10
	conns := make([]net.Conn, poolSize)
	for i := 0; i < poolSize; i++ {
		conn, err := net.Dial("unix", cfg.SocketPath)
		if err != nil {
			b.Fatalf("Failed to connect: %v", err)
		}
		conns[i] = conn
	}
	defer func() {
		for _, conn := range conns {
			conn.Close()
		}
	}()

	b.ResetTimer()

	var wg sync.WaitGroup
	var ops atomic.Int64

	for i := 0; i < poolSize; i++ {
		wg.Add(1)
		go func(conn net.Conn) {
			defer wg.Done()

			pingReq := map[string]any{
				"jsonrpc": "2.0",
				"method":  "ping",
				"id":      1,
			}
			reqData, _ := json.Marshal(pingReq)
			frame := fmt.Sprintf("%d\n%s", len(reqData), reqData)
			buf := make([]byte, 256)

			for ops.Load() < int64(b.N) {
				conn.SetWriteDeadline(time.Now().Add(time.Second))
				if _, err := conn.Write([]byte(frame)); err != nil {
					return
				}

				conn.SetReadDeadline(time.Now().Add(time.Second))
				if _, err := conn.Read(buf); err != nil {
					return
				}

				ops.Add(1)
			}
		}(conns[i])
	}

	wg.Wait()
}

// TestRPCLoadTest runs a load test with 1000 concurrent requests.
func TestRPCLoadTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	// Setup daemon
	tmpDir := t.TempDir()
	cfg := &Config{
		SocketPath:      filepath.Join(tmpDir, "meept.sock"),
		PIDFile:         filepath.Join(tmpDir, "meept.pid"),
		StateDir:        tmpDir,
		ShutdownTimeout: 5 * time.Second,
	}

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d.Run(ctx)

	// Wait for socket
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(cfg.SocketPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Run 1000 concurrent requests
	const numRequests = 1000
	const concurrency = 50

	var wg sync.WaitGroup
	var successCount atomic.Int64
	var failCount atomic.Int64

	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			conn, err := net.Dial("unix", cfg.SocketPath)
			if err != nil {
				failCount.Add(int64(numRequests / concurrency))
				return
			}
			defer conn.Close()

			pingReq := map[string]any{
				"jsonrpc": "2.0",
				"method":  "ping",
				"id":      1,
			}
			reqData, _ := json.Marshal(pingReq)
			frame := fmt.Sprintf("%d\n%s", len(reqData), reqData)
			buf := make([]byte, 256)

			for j := 0; j < numRequests/concurrency; j++ {
				conn.SetWriteDeadline(time.Now().Add(time.Second))
				if _, err := conn.Write([]byte(frame)); err != nil {
					failCount.Add(1)
					continue
				}

				conn.SetReadDeadline(time.Now().Add(time.Second))
				if _, err := conn.Read(buf); err != nil {
					failCount.Add(1)
					continue
				}

				successCount.Add(1)
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	success := successCount.Load()
	fail := failCount.Load()
	rps := float64(success) / duration.Seconds()

	t.Logf("Load Test Results:")
	t.Logf("  Total requests: %d", numRequests)
	t.Logf("  Successful: %d", success)
	t.Logf("  Failed: %d", fail)
	t.Logf("  Duration: %v", duration)
	t.Logf("  Throughput: %.0f req/sec", rps)

	// Target: 2000 req/sec minimum
	const targetRPS = 2000
	if rps < targetRPS {
		t.Errorf("Throughput %.0f req/sec below target %d req/sec", rps, targetRPS)
	} else {
		t.Logf("  PASS: Exceeds %d req/sec target", targetRPS)
	}

	if fail > 0 {
		t.Errorf("Had %d failed requests", fail)
	}
}
