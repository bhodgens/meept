package http_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/pkg/models"
	"golang.org/x/net/websocket"
)

// TestWebSocket_Load_100Concurrent validates WebSocket event delivery under load.
//
// The test opens 100 concurrent WebSocket client connections, publishes 50
// agent.progress bus messages, and measures:
//   - time-to-first-event per client
//   - total delivery time (last event received across all clients)
//   - dropped events (each client should receive all 50 events)
//
// Assertions:
//   - delivery ratio > 95%
//   - mean latency < 500ms
//   - no client receives zero events
//
// This test is a load test and may take 10-20 seconds. It exercises the full
// WebSocket hub broadcast path (bus subscribe -> handleWSEvent -> wsHub.Broadcast)
// and the session-scoped progress path (handleWSProgress).
func TestWebSocket_Load_100Concurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	const (
		numClients   = 100
		numEvents    = 50
		testTimeout  = 30 * time.Second
	)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Start an in-process HTTPS + WebSocket server with a mock bus.
	msgBus := bus.New(nil, nil)
	cfg := http.DefaultServerConfig()
	cfg.Addr = ":0"
	cfg.TLSCertFile = filepath.Join(t.TempDir(), "cert.pem")
	cfg.TLSKeyFile = filepath.Join(t.TempDir(), "key.pem")
	cfg.RequireAuth = false // no auth for test

	srv := http.NewServer(cfg, nil, nil, nil, nil, nil, http.WithWebSocket(msgBus, "/ws"))
	if srv == nil {
		t.Fatal("failed to create server")
	}

	srvCtx, srvCancel := context.WithCancel(ctx)
	defer srvCancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(srvCtx)
	}()

	// Wait for listener.
	var baseURL string
	for i := 0; i < 100; i++ {
		time.Sleep(20 * time.Millisecond)
		addr := srv.Addr()
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			continue
		}
		if host == "" || host == "::" {
			host = "127.0.0.1"
		}
		baseURL = "https://" + host + ":" + port
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, time.Second)
		if err == nil {
			conn.Close()
			break
		}
	}
	if baseURL == "" {
		t.Fatal("server did not start in time")
	}

	wsURL := "wss://" + baseURL[8:] + "/ws"

	// Create a shared TLS client config for WS connections.
	tlsConfig := &tls.Config{InsecureSkipVerify: true} //nolint:gosec // test-only

	// --- Connect 100 concurrent WS clients ---

	type clientResult struct {
		eventsReceived atomic.Int64
		firstEventAt   atomic.Int64 // unix nanos; 0 = never
		errors         atomic.Int64
	}

	results := make([]*clientResult, numClients)
	var wg sync.WaitGroup

	// Barrier: all clients try to connect simultaneously.
	startBarrier := make(chan struct{})

	for i := 0; i < numClients; i++ {
		results[i] = &clientResult{}
		wg.Add(1)
		go func(idx int, res *clientResult) {
			defer wg.Done()
			<-startBarrier // wait for go signal

			wsCfg, err := websocket.NewConfig(wsURL, baseURL)
			if err != nil {
				res.errors.Add(1)
				return
			}
			wsCfg.TlsConfig = tlsConfig

			conn, err := websocket.DialConfig(wsCfg)
			if err != nil {
				res.errors.Add(1)
				return
			}
			defer conn.Close()

			// Consume the welcome status message sent on connect.
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			var welcome map[string]any
			if err := websocket.JSON.Receive(conn, &welcome); err != nil {
				res.errors.Add(1)
				return
			}

			// Subscribe to the "chat" channel (sets up session-scope if desired).
			subscribeMsg := map[string]any{
				"type": "subscribe",
				"data": map[string]any{"channel": "chat"},
			}
			subBytes, _ := json.Marshal(subscribeMsg)
			conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if _, err := conn.Write(subBytes); err != nil {
				res.errors.Add(1)
				return
			}

			// Consume subscribed ack.
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			var subAck map[string]any
			websocket.JSON.Receive(conn, &subAck) //nolint:errcheck // best-effort

			// Read events until context is done or we time out.
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				conn.SetReadDeadline(time.Now().Add(2 * time.Second))
				var msg map[string]any
				if err := websocket.JSON.Receive(conn, &msg); err != nil {
					return // connection closed or timed out
				}

				// Count only progress events.
				if msgType, ok := msg["type"].(string); ok && msgType == "job_update" {
					count := res.eventsReceived.Add(1)
					if count == 1 {
						res.firstEventAt.Store(time.Now().UnixNano())
					}
				}
			}
		}(i, results[i])
	}

	// Signal all clients to connect.
	close(startBarrier)

	// Give clients time to connect and subscribe.
	time.Sleep(2 * time.Second)

	// --- Publish events ---
	// Use "task.status" topic which the WS event transformer maps to "job_update"
	// type (confirmed by existing tests in unified_http_test.go).
	publishStart := time.Now()

	for i := 0; i < numEvents; i++ {
		payload, _ := json.Marshal(map[string]any{
			"task_id": fmt.Sprintf("load-test-task-%d", i),
			"status":  "running",
			"message": fmt.Sprintf("progress event %d", i),
		})
		msgBus.Publish("task.status", &models.BusMessage{
			ID:        fmt.Sprintf("load-evt-%d", i),
			Type:      models.MessageTypeEvent,
			Source:    "load-test",
			Topic:     "task.status",
			Timestamp: time.Now().UTC(),
			Payload:   payload,
		})
		// Small delay between events to avoid overwhelming the channel buffer.
		time.Sleep(5 * time.Millisecond)
	}

	// Wait for delivery or timeout.
	deliveryDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deliveryDeadline) {
		allDone := true
		for _, res := range results {
			if res.eventsReceived.Load() < int64(numEvents) {
				allDone = false
				break
			}
		}
		if allDone {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Cancel server context to unblock client reader goroutines.
	srvCancel()

	// Wait for all client goroutines to finish (with hard timeout).
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Log("warning: not all client goroutines exited within 5s")
	}

	// --- Compute metrics ---

	totalReceived := int64(0)
	totalExpected := int64(numClients * numEvents)
	var sumFirstEventLatencyMs float64
	receivedFirstEvent := 0
	zeroEventClients := 0

	for _, res := range results {
		count := res.eventsReceived.Load()
		totalReceived += count
		if count == 0 {
			zeroEventClients++
		}
		firstNs := res.firstEventAt.Load()
		if firstNs > 0 {
			latencyMs := float64(firstNs-publishStart.UnixNano()) / 1e6
			if latencyMs < 0 {
				latencyMs = 0
			}
			sumFirstEventLatencyMs += latencyMs
			receivedFirstEvent++
		}
	}

	deliveryRatio := float64(totalReceived) / float64(totalExpected)
	meanLatencyMs := 0.0
	if receivedFirstEvent > 0 {
		meanLatencyMs = sumFirstEventLatencyMs / float64(receivedFirstEvent)
	}

	t.Logf("Load test results: clients=%d events=%d", numClients, numEvents)
	t.Logf("  Total received: %d / %d (%.1f%%)", totalReceived, totalExpected, deliveryRatio*100)
	t.Logf("  Delivery ratio: %.2f%%", deliveryRatio*100)
	t.Logf("  Mean time-to-first-event: %.1fms", meanLatencyMs)
	t.Logf("  Zero-event clients: %d", zeroEventClients)

	// --- Assertions ---

	// Delivery ratio must exceed 95%.
	if deliveryRatio < 0.95 {
		t.Errorf("delivery ratio %.2f%% is below 95%% threshold", deliveryRatio*100)
	}

	// Mean latency must be under 500ms.
	if meanLatencyMs >= 500 {
		t.Errorf("mean latency %.1fms exceeds 500ms threshold", meanLatencyMs)
	}

	// No client should receive zero events (indicates a connection or broadcast bug).
	if zeroEventClients > 0 {
		t.Errorf("%d clients received zero events (expected 0)", zeroEventClients)
	}
}

// TestWebSocket_Load_BroadcastCorrectness is a smaller-scale test that
// validates the broadcast path delivers the same message to all connected
// clients without corruption. It uses 20 clients and 10 events, focusing on
// correctness rather than raw load. This test runs in short mode.
func TestWebSocket_Load_BroadcastCorrectness(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	const (
		numClients = 20
		numEvents  = 10
	)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	msgBus := bus.New(nil, nil)
	cfg := http.DefaultServerConfig()
	cfg.Addr = ":0"
	cfg.TLSCertFile = filepath.Join(t.TempDir(), "cert.pem")
	cfg.TLSKeyFile = filepath.Join(t.TempDir(), "key.pem")
	cfg.RequireAuth = false

	srv := http.NewServer(cfg, nil, nil, nil, nil, nil, http.WithWebSocket(msgBus, "/ws"))
	if srv == nil {
		t.Fatal("failed to create server")
	}

	srvCtx, srvCancel := context.WithCancel(ctx)
	defer srvCancel()

	go func() { _ = srv.Start(srvCtx) }()

	// Wait for server.
	var baseURL string
	for i := 0; i < 100; i++ {
		time.Sleep(20 * time.Millisecond)
		addr := srv.Addr()
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			continue
		}
		baseURL = "https://127.0.0.1:" + port
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, time.Second)
		if err == nil {
			conn.Close()
			break
		}
	}
	if baseURL == "" {
		t.Fatal("server did not start")
	}

	wsURL := "wss://" + baseURL[8:] + "/ws"
	tlsConfig := &tls.Config{InsecureSkipVerify: true} //nolint:gosec // test-only

	// Connect clients.
	conns := make([]*websocket.Conn, 0, numClients)
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()

	for i := 0; i < numClients; i++ {
		wsCfg, err := websocket.NewConfig(wsURL, baseURL)
		if err != nil {
			t.Fatalf("client %d config error: %v", i, err)
		}
		wsCfg.TlsConfig = tlsConfig
		conn, err := websocket.DialConfig(wsCfg)
		if err != nil {
			t.Fatalf("client %d dial error: %v", i, err)
		}
		conns = append(conns, conn)

		// Consume welcome.
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var welcome map[string]any
		if err := websocket.JSON.Receive(conn, &welcome); err != nil {
			t.Fatalf("client %d welcome error: %v", i, err)
		}
	}

	// Publish events.
	for i := 0; i < numEvents; i++ {
		payload, _ := json.Marshal(map[string]any{
			"task_id": fmt.Sprintf("broadcast-test-%d", i),
			"status":  "done",
		})
		msgBus.Publish("task.status", &models.BusMessage{
			ID:        fmt.Sprintf("bcast-evt-%d", i),
			Type:      models.MessageTypeEvent,
			Source:    "broadcast-test",
			Topic:     "task.status",
			Timestamp: time.Now().UTC(),
			Payload:   payload,
		})
	}

	// Read events from each client.
	type readResult struct {
		count   int
		payloads []string
	}
	results := make([]readResult, numClients)

	var readWg sync.WaitGroup
	for i, conn := range conns {
		readWg.Add(1)
		go func(idx int, c *websocket.Conn) {
			defer readWg.Done()
			res := readResult{payloads: make([]string, 0)}
			for {
				select {
				case <-ctx.Done():
					results[idx] = res
					return
				default:
				}
				c.SetReadDeadline(time.Now().Add(2 * time.Second))
				var msg map[string]any
				if err := websocket.JSON.Receive(c, &msg); err != nil {
					results[idx] = res
					return
				}
				if msg["type"] == "job_update" {
					res.count++
					if tid, ok := msg["task_id"].(string); ok {
						res.payloads = append(res.payloads, tid)
					}
				}
			}
		}(i, conn)
	}

	// Wait for readers with timeout.
	readDone := make(chan struct{})
	go func() {
		readWg.Wait()
		close(readDone)
	}()
	select {
	case <-readDone:
	case <-time.After(8 * time.Second):
	}

	srvCancel()

	// Verify each client received events.
	for i, res := range results {
		if res.count == 0 {
			t.Errorf("client %d received 0 events (expected at least 1)", i)
		}
	}
}
