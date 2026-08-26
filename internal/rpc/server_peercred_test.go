package rpc

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// logCapture is a slog.Handler that collects every emitted record so tests can
// assert on debug/warn output without a real logging pipeline.
type logCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *logCapture) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (c *logCapture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, r.Clone())
	return nil
}

func (c *logCapture) WithAttrs(attrs []slog.Attr) slog.Handler { return c }
func (c *logCapture) WithGroup(name string) slog.Handler       { return c }

// findMessage returns the first record whose message matches, or nil.
func (c *logCapture) findMessage(msg string) *slog.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.records {
		if c.records[i].Message == msg {
			return &c.records[i]
		}
	}
	return nil
}

// shortSocketDir returns a compact temporary directory. macOS limits Unix
// socket paths to ~104 bytes and t.TempDir() embeds the test name, which can
// exceed that; /tmp/mrt* keeps the bind address short on every platform.
func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "mrt")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			t.Logf("cleanup %s: %v", dir, rmErr)
		}
	})
	return dir
}

// requirePeerCred skips the test on platforms where the kernel peer-credential
// lookup is unavailable (e.g. windows, plan9), matching the no-op fallback.
func requirePeerCred(t *testing.T) {
	t.Helper()
	sock := filepath.Join(shortSocketDir(t), "probe.sock")

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	defer ln.Close()

	type result struct {
		ok  bool
		uid int
	}
	resCh := make(chan result, 1)
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			resCh <- result{}
			return
		}
		defer conn.Close()
		uid, ok := peerCredential(conn)
		resCh <- result{ok: ok, uid: uid}
	}()

	client, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("probe dial: %v", err)
	}
	client.Close()

	select {
	case res := <-resCh:
		if !res.ok {
			t.Skipf("peer credentials unavailable on %s", runtime.GOOS)
		}
		if res.uid != os.Getuid() {
			t.Fatalf("probe uid = %d, want self uid %d", res.uid, os.Getuid())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for probe accept")
	}
}

// newPeerCredTestServer starts a Server with the given peer-credential policy
// on a fresh temp socket and returns the server and its socket path.
func newPeerCredTestServer(t *testing.T, allowed []int, logs *logCapture) (*Server, string) {
	t.Helper()
	sock := filepath.Join(shortSocketDir(t), "test.sock")
	logger := slog.New(logs)

	srv := New(&Config{
		SocketPath:  sock,
		AllowedUIDs: allowed,
	}, nil, logger)

	ctx := context.Background()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("server start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Stop(stopCtx); err != nil {
			t.Logf("server stop: %v", err)
		}
	})
	return srv, sock
}

// dialPing sends a ping request over sock and returns the connection, the raw
// server response bytes, and any protocol-level error.
func dialPing(t *testing.T, sock string) (net.Conn, json.RawMessage, error) {
	t.Helper()
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	req := map[string]any{"jsonrpc": "2.0", "method": "ping", "id": 1}
	payload, marshalErr := json.Marshal(req)
	if marshalErr != nil {
		t.Fatalf("marshal ping: %v", marshalErr)
	}
	writer := NewFrameWriter(conn)
	if err := writer.WriteFrame(payload); err != nil {
		conn.Close()
		t.Fatalf("write ping: %v", err)
	}

	respPayload, readErr := NewFrameReader(conn).ReadFrame()
	if readErr != nil {
		return conn, nil, readErr
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  any             `json:"error"`
	}
	if unmarshalErr := json.Unmarshal(respPayload, &resp); unmarshalErr != nil {
		t.Fatalf("unmarshal response: %v", unmarshalErr)
	}
	if resp.Error != nil {
		t.Fatalf("ping returned error: %v", resp.Error)
	}
	return conn, resp.Result, nil
}

// TestPeerCredential_SelfSocket verifies the kernel lookup reports our own UID
// for both ends of a local Unix-socket connection, and rejects non-Unix conns.
func TestPeerCredential_SelfSocket(t *testing.T) {
	requirePeerCred(t)

	dir := t.TempDir()
	sock := filepath.Join(dir, "self.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	type result struct {
		conn net.Conn
		err  error
	}
	acceptCh := make(chan result, 1)
	go func() {
		conn, acceptErr := ln.Accept()
		acceptCh <- result{conn: conn, err: acceptErr}
	}()

	client, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	acceptedRes := <-acceptCh
	if acceptedRes.err != nil {
		t.Fatalf("accept: %v", acceptedRes.err)
	}
	defer acceptedRes.conn.Close()

	uid, ok := peerCredential(acceptedRes.conn)
	if !ok {
		t.Fatal("peerCredential failed on accepted Unix conn")
	}
	if uid != os.Getuid() {
		t.Errorf("accepted conn peer uid = %d, want %d", uid, os.Getuid())
	}

	peerUID, ok := peerCredential(client)
	if !ok {
		t.Fatal("peerCredential failed on dialed Unix conn")
	}
	if peerUID != os.Getuid() {
		t.Errorf("dialed conn peer uid = %d, want %d", peerUID, os.Getuid())
	}

	// Non-Unix connections have no peer credentials.
	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()
	if _, ok := peerCredential(p1); ok {
		t.Error("peerCredential reported success on net.Pipe conn")
	}
}

// TestServer_PeerCredLogsUIDAndAllowsConnection covers default log-only mode
// (empty allowlist) plus the explicit own-UID allowlist case: connection works
// and the peer UID was logged.
func TestServer_PeerCredLogsUIDAndAllowsConnection(t *testing.T) {
	requirePeerCred(t)

	logs := &logCapture{}

	for _, tc := range []struct {
		name        string
		allowlist   []int
		expectDebug bool
	}{
		{"empty allowlist (log-only)", nil, true},
		{"own uid in allowlist", []int{os.Getuid()}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, sock := newPeerCredTestServer(t, tc.allowlist, logs)
			if !srv.Running() {
				t.Fatal("server not running after Start")
			}

			conn, result, err := dialPing(t, sock)
			defer func() {
				if conn != nil {
					conn.Close()
				}
			}()
			if err != nil {
				t.Fatalf("ping failed with allowlist=%v: %v", tc.allowlist, err)
			}
			var pong string
			if unmarshalErr := json.Unmarshal(result, &pong); unmarshalErr != nil || pong != "pong" {
				t.Fatalf("expected pong result, got %s (err=%v)", result, unmarshalErr)
			}

			rec := logs.findMessage("rpc: connection accepted")
			if rec == nil {
				t.Fatal("no peer_uid debug log found")
			}
			foundUID := false
			rec.Attrs(func(a slog.Attr) bool {
				if a.Key == "peer_uid" && a.Value.Int64() == int64(os.Getuid()) {
					foundUID = true
				}
				return true
			})
			if !foundUID {
				t.Error("debug log missing correct peer_uid attribute")
			}
		})
	}
}

// dialPingRaw sends a ping request over sock and returns the connection plus
// the protocol-level outcome without asserting success, for tests that expect
// the server to close the connection.
func dialPingRaw(t *testing.T, sock string) (net.Conn, error) {
	t.Helper()
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	req := map[string]any{"jsonrpc": "2.0", "method": "ping", "id": 1}
	payload, marshalErr := json.Marshal(req)
	if marshalErr != nil {
		t.Fatalf("marshal ping: %v", marshalErr)
	}
	writer := NewFrameWriter(conn)
	if err := writer.WriteFrame(payload); err != nil {
		return conn, err
	}
	respPayload, readErr := NewFrameReader(conn).ReadFrame()
	if readErr != nil {
		return conn, readErr
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  any             `json:"error"`
	}
	if unmarshalErr := json.Unmarshal(respPayload, &resp); unmarshalErr != nil {
		t.Fatalf("unmarshal response: %v", unmarshalErr)
	}
	if resp.Error != nil {
		t.Fatalf("ping returned error: %v", resp.Error)
	}
	return conn, nil
}

// TestServer_RejectsForeignUID verifies that a peer whose kernel-verified UID
// is absent from a non-empty allowlist is closed before the RPC handshake:
// its written ping never receives a response, and the rejection is logged at
// warn level with the peer UID and allowlist size.
func TestServer_RejectsForeignUID(t *testing.T) {
	requirePeerCred(t)

	foreignUID := os.Getuid() + 65536
	logs := &logCapture{}

	_, sock := newPeerCredTestServer(t, []int{foreignUID}, logs)

	conn, pingErr := dialPingRaw(t, sock)
	if conn != nil {
		defer conn.Close()
	}
	if pingErr == nil {
		t.Fatal("ping succeeded despite foreign UID allowlist; expected pre-handshake close")
	}
	t.Logf("connection terminated as expected with: %v", pingErr)

	rec := logs.findMessage("rpc: connection rejected by uid allowlist")
	if rec == nil {
		t.Fatal("no warn-level rejection log found")
	}
	var gotUID int64
	gotSize := false
	rec.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "peer_uid":
			gotUID = a.Value.Int64()
		case "allowlist_size":
			gotSize = a.Value.Int64() == 1
		}
		return true
	})
	if gotUID != int64(os.Getuid()) {
		t.Errorf("rejection log peer_uid = %d, want %d", gotUID, os.Getuid())
	}
	if !gotSize {
		t.Error("rejection log missing allowlist_size=1")
	}

	// Server still serves subsequently-allowed peers after a rejection.
	logs2 := &logCapture{}
	_, sock2 := newPeerCredTestServer(t, []int{os.Getuid()}, logs2)
	conn2, result, err := dialPing(t, sock2)
	if conn2 != nil {
		defer conn2.Close()
	}
	if err != nil {
		t.Fatalf("post-rejection ping failed: %v", err)
	}
	var pong string
	if unmarshalErr := json.Unmarshal(result, &pong); unmarshalErr != nil || pong != "pong" {
		t.Fatalf("expected pong after rejection, got %s (err=%v)", result, unmarshalErr)
	}
}
