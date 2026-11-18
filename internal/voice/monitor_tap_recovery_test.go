package voice

// monitor_tap_recovery_test.go - pins that ONE backpressured/late tap
// write does not silence the operator's monitor for the rest of the link
// (live defect 2026-11-09 link-8d03fdac3afb5c0e: the monitor player's
// first write missed its deadline while the oto context was still
// opening; the tap self-disarmed permanently and the operator heard
// nothing despite both agents talking).

import (
	"net"
	"testing"
	"time"
)

func TestMonitorTapRecoversAfterTransientWriteFailure(t *testing.T) {
	// A pipe with NO reader: writes fail once the buffer fills (the
	// deadline path). monitorTap talks to the stream side.
	client, srv := net.Pipe()
	defer func() { _ = client.Close() }()

	ln := &monitorListener{}
	ln.conn = srv // package-internal: mirrors the accept goroutine's set-once store
	tap := &monitorTap{ln: ln}

	// Phase 1: nobody reads -> a write must fail (net.Pipe blocks until
	// the deadline; a pipe has no deadline set by us here, so drive it
	// through the tap's own deadline path with a short write window).
	done := make(chan error, 1)
	go func() {
		_ = srv.SetWriteDeadline(time.Now().Add(10 * time.Millisecond))
		_, err := tap.Write([]byte("stalled batch"))
		done <- err
	}()
	if err := <-done; err == nil {
		t.Fatal("expected the stalled write to fail")
	}
	if tap.dead.Load() {
		t.Fatal("one failed write must NOT disarm the tap")
	}

	// Phase 2: start draining -> the next write succeeds and the
	// consecutive-failure counter resets.
	go func() {
		buf := make([]byte, 64)
		for {
			if _, err := client.Read(buf); err != nil {
				return
			}
		}
	}()
	if _, err := tap.Write([]byte("recovered batch")); err != nil {
		t.Fatalf("write after reader attached: %v (want recovery)", err)
	}
	if got := tap.fails.Load(); got != 0 {
		t.Fatalf("fails counter = %d after a success, want 0", got)
	}

	// Phase 3: permanently dead conn -> threshold consecutive failures
	// disarm (logged-once posture at the pump).
	_ = client.Close()
	for range monitorFailThreshold {
		_, _ = tap.Write([]byte("x"))
	}
	if !tap.dead.Load() {
		t.Fatal("a persistently dead monitor conn must disarm after the threshold")
	}
	// Post-dearm writes are cheap no-ops returning success.
	if n, err := tap.Write([]byte("post-dead")); err != nil || n != len("post-dead") {
		t.Fatalf("post-deadarm Write = (%d, %v), want silent success", n, err)
	}
}
