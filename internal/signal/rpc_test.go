package signal

// Gate tests for contract C3 (master.md, quoted verbatim): the
// signal-cli json-rpc client over a unix socket. Written BEFORE
// implementation (00T-gate-tests.md, T3). RED until leaf 10 lands
// rpc.go.
//
// Pinned signatures:
//
//	func Dial(socketPath string) (*Client, error)
//	func (c *Client) StartCall(recipient string, audioMode CallAudioMode) (callID string, err error)
//	func (c *Client) AcceptCall(callID string) error
//	func (c *Client) HangupCall(callID string) error
//	func (c *Client) ListCalls() ([]CallInfo, error)
//	func (c *Client) SubscribeCallEvents(subscriptionID string) error
//	func (c *Client) Send(recipient, message string, attachments []string) (ts int64, err error)
//	func (c *Client) Typing(recipient string, stop bool) error
//
// Client behavior: JSON-RPC 2.0, request-id mutex (concurrent requests
// serialize on the conn), notification demux goroutine routing
// "callEvent" by callID and "envelope" DataMessages.
// States (per PR #1932): RINGING_INCOMING | RINGING_OUTGOING | CONNECTING
// | CONNECTED | RECONNECTING | ENDED (reason carries busy/timeout/declined).

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// stubRPCServer is a minimal in-test signal-cli-shaped json-rpc server:
// it answers requests (echoing the method with a deterministic result)
// and pushes scripted notifications on demand.
type stubRPCServer struct {
	t   *testing.T
	ln  net.Listener
	mu  sync.Mutex
	got []stubRequest
	// conns tracks every accepted client conn (notification push target);
	// respond lets a test override the result body per method.
	conns   []net.Conn
	respond func(method string, id int) any
}

type stubRequest struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type stubResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
}

type stubNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func startStubRPC(t *testing.T) *stubRPCServer {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "signal-cli.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	s := &stubRPCServer{t: t, ln: ln}
	t.Cleanup(func() { _ = ln.Close() })
	go s.serve()
	return s
}

func (s *stubRPCServer) socketPath() string { return s.ln.Addr().String() }

func (s *stubRPCServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *stubRPCServer) handle(conn net.Conn) {
	s.mu.Lock()
	s.conns = append(s.conns, conn)
	s.mu.Unlock()
	defer func() { _ = conn.Close() }() //nolint:errcheck // test stub teardown
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var req stubRequest
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			continue
		}
		s.mu.Lock()
		s.got = append(s.got, req)
		respond := s.respond
		s.mu.Unlock()
		result := respond(req.Method, req.ID)
		body, err := json.Marshal(result)
		if err != nil {
			continue
		}
		resp, err := json.Marshal(stubResponse{JSONRPC: "2.0", ID: req.ID, Result: body})
		if err != nil {
			continue
		}
		_, _ = conn.Write(append(resp, '\n')) //nolint:errcheck // test stub
	}
}

func (s *stubRPCServer) requests() []stubRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]stubRequest, len(s.got))
	copy(out, s.got)
	return out
}

// firstConnLocked returns the stub's most recent live client conn
// (caller holds s.mu). The single client conn is what the client's
// notification demux reads; notifications MUST ride it.
func (s *stubRPCServer) firstConnLocked() net.Conn {
	s.t.Helper()
	if s.conns == nil {
		s.t.Fatalf("stub has no live client conn (client never dialed)")
	}
	return s.conns[len(s.conns)-1]
}

func (s *stubRPCServer) count(method string) int {
	n := 0
	for _, r := range s.requests() {
		if r.Method == method {
			n++
		}
	}
	return n
}

// notify writes one scripted notification to the (single) client conn.
func (s *stubRPCServer) notify(conn net.Conn, method string, params any) {
	s.t.Helper()
	body, err := json.Marshal(params)
	if err != nil {
		s.t.Fatalf("marshal notification: %v", err)
	}
	n, err := json.Marshal(stubNotification{JSONRPC: "2.0", Method: method, Params: body})
	if err != nil {
		s.t.Fatalf("marshal notification envelope: %v", err)
	}
	if _, err := conn.Write(append(n, '\n')); err != nil {
		s.t.Fatalf("write notification: %v", err)
	}
}

// TestClientDialAndStartCall pins the leaf's Given/When/Then: Dial over a
// unix socket to a stub signal-cli; StartCall issues the json-rpc
// `startCall` method with the recipient + audio mode and surfaces the
// callID from the result.
func TestClientDialAndStartCall(t *testing.T) {
	srv := startStubRPC(t)
	srv.mu.Lock()
	srv.respond = func(method string, id int) any {
		if method == "startCall" {
			return map[string]any{"callId": "call-abc"}
		}
		return map[string]any{}
	}
	srv.mu.Unlock()

	c, err := Dial(srv.socketPath())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() }) //nolint:errcheck // test teardown

	callID, err := c.StartCall("+15550000011", CallAudioModeSocket)
	if err != nil {
		t.Fatalf("StartCall: %v", err)
	}
	if callID != "call-abc" {
		t.Errorf("StartCall callID = %q, want %q", callID, "call-abc")
	}
	if srv.count("startCall") != 1 {
		t.Errorf("stub saw %d startCall requests, want 1", srv.count("startCall"))
	}
}

// TestClientConcurrentRequestsSerialize pins the request-id mutex:
// concurrent Call/ListCalls requests must never interleave json-rpc
// frames — every response id must match its request (a torn frame or a
// mismatched id fails the test).
func TestClientConcurrentRequestsSerialize(t *testing.T) {
	srv := startStubRPC(t)
	srv.mu.Lock()
	srv.respond = func(method string, id int) any {
		// Echo the id inside the result so the client-visible reply is
		// uniquely attributable even if frames were interleaved.
		return map[string]any{"echo": id, "callIds": []string{}}
	}
	srv.mu.Unlock()

	c, err := Dial(srv.socketPath())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() }) //nolint:errcheck // test teardown

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.ListCalls(); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent ListCalls: %v (request-id mutex failed to serialize)", err)
	}
	if srv.count("listCalls") != n {
		t.Errorf("stub saw %d listCalls, want %d", srv.count("listCalls"), n)
	}
}

// TestClientCallEventDemux pins the leaf's canonical scenario: with a
// subscribed call-event stream, a stub emitting callEvent CONNECTED then
// ENDED surfaces BOTH events, in order, for the right callID.
func TestClientCallEventDemux(t *testing.T) {
	srv := startStubRPC(t)
	srv.mu.Lock()
	srv.respond = func(method string, id int) any {
		return map[string]any{}
	}
	srv.mu.Unlock()

	c, err := Dial(srv.socketPath())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() }) //nolint:errcheck // test teardown

	if err := c.SubscribeCallEvents("sub-1"); err != nil {
		t.Fatalf("SubscribeCallEvents: %v", err)
	}

	// Demux side: events for our callID surface on the call-event stream.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events := c.CallEvents(ctx, "call-x")

	// Grab the stub's client conn by dialing the recording helper: the
	// notifications must ride the SAME conn the client uses (json-rpc
	// demux over one unix socket). The stub shares conns with requests,
	// so we drive notifications through the server's live conn set.
	srv.pushCallEvent("call-x", "CONNECTED")
	srv.pushCallEvent("call-x", "ENDED")
	srv.pushCallEvent("call-other", "CONNECTED") // demux must NOT surface this one

	var got []CallEventState
	deadline := time.After(4 * time.Second)
	for len(got) < 2 {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("call-event stream closed early; got %v", got)
			}
			if ev.CallID != "call-x" {
				t.Errorf("demux routed event for callID %q, want call-x only", ev.CallID)
			}
			got = append(got, ev.State)
		case <-deadline:
			t.Fatalf("timed out collecting call events; got %v", got)
		case <-ctx.Done():
			t.Fatalf("ctx done collecting call events; got %v", got)
		}
	}
	if len(got) != 2 || got[0] != CallEventStateConnected || got[1] != CallEventStateEnded {
		t.Errorf("event states = %v, want [CONNECTED ENDED] in order", got)
	}
}

// pushCallEvent finds the stub's first live client conn and writes one
// callEvent notification (the signal-cli notification shape: params
// envelope carries account, callId, state).
func (s *stubRPCServer) pushCallEvent(callID, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	conn := s.firstConnLocked()
	params := map[string]any{
		"account": "+15559999999",
		"envelope": map[string]any{
			"sourceUuid": "11111111-2222-3333-4444-555555555555",
			"callEvent": map[string]any{
				"callId": callID,
				"state":  state,
			},
		},
	}
	body, err := json.Marshal(params)
	if err != nil {
		return
	}
	n, err := json.Marshal(stubNotification{JSONRPC: "2.0", Method: "callEvent", Params: body})
	if err != nil {
		return
	}
	_, _ = conn.Write(append(n, '\n')) //nolint:errcheck // test stub
}
