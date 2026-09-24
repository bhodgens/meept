//go:build e2e

// Package rpcping covers the RPC transport surface over the Unix socket:
// ping round-trip, method-not-found, status/uptime monotonicity, invalid
// params mapping, and the RPC->bus publish bridge.
package rpcping

import (
	"strings"
	"testing"
	"time"
)

// rpc-ping-01: ping round-trip on the Unix socket returns a valid JSON-RPC
// envelope (jsonrpc "2.0", matching id, result "pong").
func TestPingRoundTrip(t *testing.T) {
	s := start(t)
	resp := rpcCall(t, s.SocketPath, "ping", nil)
	if resp["jsonrpc"] != "2.0" {
		t.Fatalf("jsonrpc = %v, want 2.0", resp["jsonrpc"])
	}
	if resp["result"] != "pong" {
		t.Fatalf("result = %v, want pong (full envelope: %v)", resp["result"], resp)
	}
	if e, ok := resp["error"]; ok && e != nil {
		t.Fatalf("ping returned an error: %v", e)
	}
}

// rpc-ping-02: unknown method returns JSON-RPC -32601 method-not-found and
// never hangs.
func TestUnknownMethodReturnsMethodNotFound(t *testing.T) {
	s := start(t)
	done := make(chan map[string]any, 1)
	go func() { done <- rpcCall(t, s.SocketPath, "definitely.not.a.method", map[string]any{}) }()
	select {
	case resp := <-done:
		errObj, ok := resp["error"].(map[string]any)
		if !ok {
			t.Fatalf("expected error object, got: %v", resp)
		}
		code, _ := errObj["code"].(float64)
		if int(code) != -32601 {
			t.Fatalf("error code = %v, want -32601", errObj["code"])
		}
		msg, _ := errObj["message"].(string)
		if !strings.Contains(msg, "method not found") {
			t.Fatalf("error message = %q, want it to mention method-not-found", msg)
		}
		// The daemon still answers a follow-up ping (no wedge).
		resp2 := rpcCall(t, s.SocketPath, "ping", nil)
		if resp2["result"] != "pong" {
			t.Fatalf("daemon wedged after method-not-found: %v", resp2)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("unknown method hung for 10s; -32601 must be immediate")
	}
}

// rpc-ping-03: status reports version + uptime_seconds, and uptime strictly
// increases between two calls.
func TestStatusReportsVersionAndMonotonicUptime(t *testing.T) {
	s := start(t)

	first := rpcResult(t, s.SocketPath, "status", nil)
	version, _ := first["version"].(string)
	if version == "" {
		t.Fatalf("status missing version: %v", first)
	}
	uptime1, ok := first["uptime_seconds"].(float64)
	if !ok {
		t.Fatalf("uptime_seconds missing/non-numeric: %v", first["uptime_seconds"])
	}

	time.Sleep(1100 * time.Millisecond)

	second := rpcResult(t, s.SocketPath, "status", nil)
	uptime2, ok := second["uptime_seconds"].(float64)
	if !ok {
		t.Fatalf("second uptime_seconds missing: %v", second)
	}
	if uptime2 <= uptime1 {
		t.Fatalf("uptime did not increase: %f then %f", uptime1, uptime2)
	}
	if status, _ := first["status"].(string); status != "running" {
		t.Fatalf("status = %v, want running", first["status"])
	}
}

// rpc-ping-04: invalid params produce ErrCodeInvalidParams (-32602), not the
// generic internal error (-32603).
func TestInvalidParamsMapToInvalidParamsCode(t *testing.T) {
	s := start(t)

	// memory.vector.search is a DIRECT handler that passes the service
	// error through untouched: MemoryService.VectorSearch wraps
	// services.ErrInvalidInput for an empty query, and errcls maps that
	// registered sentinel to -32602 ErrCodeInvalidParams (not -32603).
	resp := rpcCall(t, s.SocketPath, "memory.vector.search", map[string]any{
		"query": "",
	})
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got: %v", resp)
	}
	code, _ := errObj["code"].(float64)
	if int(code) != -32602 {
		t.Fatalf("error code = %v (%s), want -32602 ErrCodeInvalidParams", errObj["code"], errObj["message"])
	}
}

// rpc-ping-05: bus.publish RPC delivers to a bus subscriber (the RPC->bus
// bridge). Subscribe, publish, and poll over ONE persistent connection —
// the daemon cancels bus subscriptions when the client disconnects (Bug C8),
// so per-call connections would kill the subscription immediately.
func TestBusPublishDeliversToSubscriber(t *testing.T) {
	s := start(t)
	c := dialRPC(t, s.SocketPath)

	sub := c.callResult("bus.subscribe", map[string]any{
		"topics": []string{"e2e.ping.topic"},
	})
	subID, _ := sub["subscription_id"].(string)
	if subID == "" {
		t.Fatalf("bus.subscribe returned no subscription_id: %v", sub)
	}
	defer c.call("bus.unsubscribe", map[string]any{"subscription_id": subID})

	// Give the collector goroutine a beat to attach, then publish.
	time.Sleep(300 * time.Millisecond)
	pub := c.callResult("bus.publish", map[string]any{
		"topic":   "e2e.ping.topic",
		"payload": map[string]any{"hello": "world"},
	})
	if delivered, _ := pub["delivered"].(float64); delivered < 1 {
		t.Fatalf("bus.publish delivered=%v; the subscription was not live", pub["delivered"])
	}

	// The event must appear on the subscription.
	var events []any
	pollDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(pollDeadline) {
		poll := c.callResult("bus.poll", map[string]any{
			"subscription_id": subID,
		})
		events, _ = poll["events"].([]any)
		if len(events) > 0 {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if len(events) == 0 {
		t.Fatal("bus.poll returned no events after a subscribed bus.publish")
	}
	raw := rawJSON(t, events)
	if !strings.Contains(raw, "e2e.ping.topic") {
		t.Fatalf("expected the published topic on the subscription, got: %s", raw)
	}
	if !strings.Contains(raw, "hello") {
		t.Fatalf("expected the published payload on the subscription, got: %s", raw)
	}
}
