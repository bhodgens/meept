package cluster

// L7 of the 2026-10-05 bughunt: internal/cluster's peer-send semaphore was
// never written. `sem := make(chan struct{}, 32)` was created per SendEvent
// and only ever READ (`defer func() { <-sem }()` in sendToPeer), so every
// peer-send goroutine blocked FOREVER on the deferred receive — one
// permanently parked goroutine per peer per event, and a semaphore that
// bounded nothing while its comment claimed the opposite.
//
// These pins assert the real contract rather than the shape of the fix:
//   - in-flight peer sends never exceed maxConcurrentPeerSends, even when the
//     fan-out targets more peers than there are slots;
//   - no send is stranded by a leaked slot, across events either.
//
// Address shape: every peer dials the SAME gossip address
// (WireGuardPort+1 on the member's ClusterIP), so one loopback listener
// receives N distinct connections and can observe the real concurrency.

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/pkg/models"
)

// peerSendListener counts concurrent connections and holds them without
// answering, so every send stays in flight for the measurement window.
type peerSendListener struct {
	ln       net.Listener
	port     int
	inFlight atomic.Int32
	peak     atomic.Int32
	accepted atomic.Int32
	acked    atomic.Int32

	mu    sync.Mutex
	conns []net.Conn
}

// newPeerSendListener starts a loopback listener that ACCEPTS but never ACKs,
// keeping each send in flight until the connection is torn down.
func newPeerSendListener(t *testing.T) *peerSendListener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	l := &peerSendListener{ln: ln, port: ln.Addr().(*net.TCPAddr).Port}
	t.Cleanup(func() {
		_ = ln.Close()
		// Snapshot under the lock, close OUTSIDE it (mutexio: no I/O under a
		// mutex — net.Conn.Close is I/O).
		l.mu.Lock()
		held := append([]net.Conn(nil), l.conns...)
		l.mu.Unlock()
		for _, c := range held {
			_ = c.Close()
		}
	})
	go l.acceptLoop()
	return l
}

func (l *peerSendListener) acceptLoop() {
	for {
		conn, err := l.ln.Accept()
		if err != nil {
			return
		}
		cur := l.inFlight.Add(1)
		l.accepted.Add(1)
		for {
			peak := l.peak.Load()
			if cur <= peak || l.peak.CompareAndSwap(peak, cur) {
				break
			}
		}
		l.mu.Lock()
		l.conns = append(l.conns, conn)
		l.mu.Unlock()
		// Drain the request line so the transport's Write completes, but never
		// ACK: the connection stays open, keeping the send in flight.
		go func(c net.Conn) {
			defer func() {
				l.inFlight.Add(-1)
				_ = c.Close()
			}()
			r := bufio.NewReader(c)
			for {
				_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
				line, rerr := r.ReadString('\n')
				if rerr != nil {
					return
				}
				if line != "" {
					l.acked.Add(1)
				}
			}
		}(conn)
	}
}

// peerSendMembers builds n active members that all resolve to l's gossip port
// (ClusterIP + WireGuardPort+1), which is the only address shape the
// transport's peerGossipAddr can dial.
func peerSendMembers(n int, l *peerSendListener) map[string]*Member {
	members := make(map[string]*Member, n)
	for i := range n {
		nodeID := fmt.Sprintf("peer-%03d", i)
		members[nodeID] = &Member{
			NodeID:    nodeID,
			ClusterIP: "127.0.0.1",
			Status:    "active",
			Endpoint:  fmt.Sprintf("127.0.0.1:%d", l.port),
		}
	}
	return members
}

func peerSendConfig(port int) *Config {
	return &Config{
		// The transport dials WireGuardPort+1, so back the config off by one.
		Network: NetworkConfig{WireGuardPort: port - 1},
		Gossip:  GossipConfig{EventRetention: time.Hour},
	}
}

// TestGossipTransport_SendSemaphoreBoundsConcurrentPeerSends is the L7 pin.
// The fan-out targets maxConcurrentPeerSends+8 peers (more than the semaphore
// has slots), the listener holds every connection open, and the observed peak
// of simultaneously in-flight sends must never exceed the bound.
func TestGossipTransport_SendSemaphoreBoundsConcurrentPeerSends(t *testing.T) {
	const extraPeers = 8
	totalPeers := maxConcurrentPeerSends + extraPeers

	l := newPeerSendListener(t)
	members := peerSendMembers(totalPeers, l)
	transport := NewGossipTransport(peerSendConfig(l.port), "self", nil,
		&mockMembersProvider{members: members}, slog.Default())

	event := &models.ClusterEvent{
		EventID:   models.GenerateEventID(),
		NodeID:    "self",
		EventType: models.EventNodeHeartbeat,
	}

	// SendEvent must RETURN promptly even though the fan-out exceeds the
	// semaphore's capacity: the surplus sends wait for a slot, and slots free
	// as peers complete. A blocking acquire with no release would deadlock
	// this call instead of bounding it.
	done := make(chan struct{})
	go func() {
		transport.SendEvent(event)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("SendEvent blocked: the semaphore is acquired but never released")
	}

	// Every peer must eventually be reached: the surplus sends are delayed by
	// the bound, not stranded by it.
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) && int(l.accepted.Load()) < totalPeers {
		time.Sleep(20 * time.Millisecond)
	}

	peak := int(l.peak.Load())
	if peak == 0 {
		t.Fatalf("no peer sends observed (accepted=%d) — the pin must exercise the send path",
			l.accepted.Load())
	}
	if peak > maxConcurrentPeerSends {
		t.Errorf("peak concurrent peer sends = %d, want <= %d (semaphore must bound the fan-out)",
			peak, maxConcurrentPeerSends)
	}
	if got := int(l.accepted.Load()); got != totalPeers {
		t.Errorf("peers reached = %d, want %d (no send may be stranded by the semaphore)",
			got, totalPeers)
	}
}

// TestGossipTransport_SendSemaphoreReleasesAfterCompletion pins the other half:
// the slot is released when a send finishes, so a LATER SendEvent is not
// starved by slots leaked by earlier ones. A leaked-slot shape blocks here.
func TestGossipTransport_SendSemaphoreReleasesAfterCompletion(t *testing.T) {
	// One gossip listener that ACKs immediately, so every send completes and
	// releases its slot.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port
	served := make(chan struct{}, 16)
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				r := bufio.NewReader(c)
				if _, rerr := r.ReadString('\n'); rerr != nil {
					return
				}
				_, _ = c.Write([]byte("ACK\n"))
				served <- struct{}{}
			}(conn)
		}
	}()

	members := peerSendMembers(1, &peerSendListener{port: port})
	transport := NewGossipTransport(peerSendConfig(port), "self", nil,
		&mockMembersProvider{members: members}, slog.Default())

	for i := range 4 {
		event := &models.ClusterEvent{
			EventID:   models.GenerateEventID(),
			NodeID:    "self",
			EventType: models.EventNodeHeartbeat,
		}
		transport.SendEvent(event)
		select {
		case <-served:
		case <-time.After(15 * time.Second):
			t.Fatalf("send %d never reached the peer (semaphore slot leaked across events)", i)
		}
	}
}
