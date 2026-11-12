package voice

// client_events_test.go - pins for the daemon->client barge-in CONTROL
// lane (bughunt 2026-11-11 defect 1: the ~750 ms ghost tail): the
// ClientEventServer accept/Emit/Close posture, the drop-never-block
// contract with no client attached, and the LocalLeg plumbing that
// surfaces <ccid>.c.ev on the open result and tears it down at End.

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// evDialOK dials path with a bounded retry (the listener file appears
// at bind() a beat before listen() finishes; the same ECONNREFUSED
// window the leg fixture's fake-child dial pins).
func evDialOK(t *testing.T, path string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.Dial("unix", path)
		if err == nil {
			t.Cleanup(func() { _ = conn.Close() })
			return conn
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial ev lane %s: %v", path, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// awaitEvClient blocks until srv's accept goroutine has stored a client
// conn. A successful net.Dial only proves the KERNEL queued the
// connection - the daemon's Emit DROPS until the accept stores it (the
// documented no-client posture), so emitting right after dialing would
// race the accept. In-package polling of ml.current() is the honest
// sync point.
func awaitEvClient(t *testing.T, srv *ClientEventServer) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if srv.ml.current() != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("ev accept never stored the client conn")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestClientEventServerEmitAndAccept(t *testing.T) {
	// shortDir: the sun_path cap binds, so a plain t.TempDir() path
	// (/var/folders/... ~ 60 B base) must be refused, not listened.
	tmp := shortDir(t)
	path := filepath.Join(tmp, "x.c.ev")
	srv, err := NewClientEventServer(path, nil)
	if err != nil {
		t.Fatalf("NewClientEventServer: %v", err)
	}
	t.Cleanup(srv.Close)

	if srv.Path() != path {
		t.Fatalf("Path() = %q, want %q", srv.Path(), path)
	}

	// (a) Emit with NO client attached must be a silent no-op, never a
	// panic or block: the drop-never-block posture mirrored from the
	// monitor tap.
	srv.Emit(ClientEvent{Event: ClientEventClear})

	// (b) A late client still gets later events (a dropped early event
	// does not disarm the lane). Wait for the accept to STORE the conn
	// before emitting (awaitEvClient) - dial success alone would race
	// it.
	conn := evDialOK(t, path)
	awaitEvClient(t, srv)
	br := bufio.NewReader(conn)

	srv.Emit(ClientEvent{Event: ClientEventClear})
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read clear line: %v", err)
	}
	if line != "{\"event\":\"clear\"}\n" {
		t.Fatalf("clear line = %q, want byte-exact {\"event\":\"clear\"}\\n", line)
	}

	// (c) Unknown future kinds are plain JSON lines - the protocol is
	// additive by contract; the server just emits what it is told.
	srv.Emit(ClientEvent{Event: "future-kind"})
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, err = br.ReadString('\n')
	if err != nil || line != "{\"event\":\"future-kind\"}\n" {
		t.Fatalf("second line = %q err %v, want future-kind JSON line", line, err)
	}

	// (d) A nil receiver Emit is a no-op (a degraded call's routeFrame
	// path reaches ClientEventsFor -> nil).
	var nilSrv *ClientEventServer
	nilSrv.Emit(ClientEvent{Event: ClientEventClear})
	nilSrv.Close()
	if nilSrv.Path() != "" {
		t.Fatal("nil Path() must be \"\"")
	}
}

func TestClientEventServerPathTooLongRefused(t *testing.T) {
	long := filepath.Join(t.TempDir(), string(make([]byte, 200)), "x.c.ev")
	if _, err := NewClientEventServer(long, nil); err == nil {
		t.Fatal("over-cap path must be refused (sun_path discipline, monitor-tap precedent)")
	}
}

// TestLocalLegEvLane pins the leg plumbing end-to-end inside the
// package: the open result carries EvPath, the socket file exists, the
// ccid registry makes ClientEventsFor reach the server (what
// VoiceWiring.routeFrame calls after a latch), and End tears the lane
// down (unregister + socket file removed).
func TestLocalLegEvLane(t *testing.T) {
	f := newLegFixture(t)

	call, err := f.leg.Open(context.Background(), "emp-ev", "evbox")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if call.EvPath == "" {
		t.Fatalf("open result EvPath empty (degrade logged? check the warn): %+v", call)
	}
	wantEv := filepath.Join(SocketDir(), call.CallID+".c.ev")
	if call.EvPath != wantEv {
		t.Fatalf("EvPath = %q, want %q", call.EvPath, wantEv)
	}
	if _, err := os.Stat(call.EvPath); err != nil {
		t.Fatalf("ev socket file missing: %v", err)
	}

	// The registry route the frame router uses.
	srv := ClientEventsFor(call.CallID)
	if srv == nil {
		t.Fatal("ClientEventsFor(registry ccid) = nil, want the live server")
	}

	// A client dial + clear emit: exactly one JSON line (what the CLI's
	// clientClearReader consumes). The dial's ACCEPT must land before
	// Emit or the event drops (no-client posture) - awaitEvClient.
	conn := evDialOK(t, call.EvPath)
	awaitEvClient(t, srv)
	srv.Emit(ClientEvent{Event: ClientEventClear})
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || line != "{\"event\":\"clear\"}\n" {
		t.Fatalf("ev line = %q err %v", line, err)
	}

	// status/Live surfaces the same path.
	var found bool
	for _, c := range f.leg.Live() {
		if c.CallID == call.CallID {
			found = true
			if c.EvPath != wantEv {
				t.Fatalf("Live() EvPath = %q, want %q", c.EvPath, wantEv)
			}
		}
	}
	if !found {
		t.Fatal("Live() missing the call")
	}

	// End tears the lane down: registry entry gone + socket file removed
	// (newMonitorListener.remove semantics).
	if err := f.leg.End(call.CallID, "ev lane test"); err != nil {
		t.Fatalf("End: %v", err)
	}
	if ClientEventsFor(call.CallID) != nil {
		t.Fatal("ev server still registered after End")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(call.EvPath); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ev socket file %s survived End", call.EvPath)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestClientEventJSONShape byte-pins the wire line the CLI's scanner
// decodes: key "event" exactly (the C1 byte-exact-key discipline applies
// to this lane too).
func TestClientEventJSONShape(t *testing.T) {
	b, err := json.Marshal(ClientEvent{Event: ClientEventClear})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"event":"clear"}` {
		t.Fatalf("marshal = %s", b)
	}
}
