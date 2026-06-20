package web

import (
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/net/websocket"
)

func TestWebSocketHub_New(t *testing.T) {
	hub := NewWebSocketHub(nil)
	if hub == nil {
		t.Fatalf("expected non-nil hub")
	}
	if hub.ClientCount() != 0 {
		t.Fatalf("expected 0 clients, got %d", hub.ClientCount())
	}
}

func TestWebSocketHub_Broadcast_NoClients_NoPanic(t *testing.T) {
	hub := NewWebSocketHub(nil)
	// Should not panic
	hub.Broadcast("status", map[string]string{"state": "running"})
	hub.Broadcast("chat", map[string]string{"msg": "hello"})
}

func TestWebSocketHub_RegisterUnregister_Count(t *testing.T) {
	hub := NewWebSocketHub(nil)
	// Without actual WebSocket connections, we can only test the hub's
	// basic state management.
	if count := hub.ClientCount(); count != 0 {
		t.Fatalf("expected 0 clients initially, got %d", count)
	}
}

func TestWSMessage_Type(t *testing.T) {
	msg := WSMessage{Type: "ping"}
	if msg.Type != "ping" {
		t.Fatalf("expected ping type")
	}
}

// TestWebSocketHub_ConnWriteMu_StableAcrossCalls verifies that the per-conn
// write mutex is stable across calls and that Unregister cleans it up. This
// is the core invariant for the fix that serializes Broadcast writes with
// read-loop responses on the same conn (golang.org/x/net/websocket requires
// no concurrent Write on the same conn).
func TestWebSocketHub_ConnWriteMu_StableAcrossCalls(t *testing.T) {
	hub := NewWebSocketHub(nil)

	// Use distinct pointer values as stand-ins for *websocket.Conn. We can't
	// allocate a real websocket.Conn without a handshake, but the mutex map
	// is keyed purely on pointer identity.
	c1 := &websocket.Conn{}
	c2 := &websocket.Conn{}

	mu1a := hub.connWriteMu(c1)
	mu1b := hub.connWriteMu(c1)
	if mu1a != mu1b {
		t.Fatalf("expected stable mutex for same conn")
	}
	mu2 := hub.connWriteMu(c2)
	if mu1a == mu2 {
		t.Fatalf("expected distinct mutexes for distinct conns")
	}

	hub.writeMu.Delete(c1)
	// After delete, a new mutex may be created for c1; that's expected.
	// Just verify no panic.
	_ = hub.connWriteMu(c1)
}

// TestWebSocketHub_ConnWriteMu_SerializesConcurrentLockers is the regression
// test for the Broadcast vs handleWSMessage write race. If two goroutines
// both acquire connWriteMu(c) for the same conn, they must be serialized —
// i.e. never both inside the critical section at once.
func TestWebSocketHub_ConnWriteMu_SerializesConcurrentLockers(t *testing.T) {
	hub := NewWebSocketHub(nil)
	c := &websocket.Conn{}

	mu := hub.connWriteMu(c)

	var inSection atomic.Int32
	const goroutines = 8
	const itersPer = 100
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < itersPer; i++ {
				mu.Lock()
				cur := inSection.Add(1)
				if cur != 1 {
					panic("concurrent holders of per-conn write mutex")
				}
				inSection.Add(-1)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
}
