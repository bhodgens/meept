package tui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/sharedclient"
	"github.com/caimlas/meept/internal/tui/models"
)

// headlessProgram is the handle a test drives the running App through.
//
// Concurrency contract: the program's event-loop goroutine owns ALL model
// mutation. Tests drive it exclusively via send (key presses, window
// messages, domain messages) — never by calling App.Update or a sub-model
// Update directly from the test goroutine (that races the loop and
// intermittently loses state, e.g. cleared table rows). bubbletea v2's
// Send blocks on an unbuffered channel until the single-threaded loop
// receives the message, so sequential sends are ordered; a trailing
// settle() is a full barrier: when it returns, every earlier message —
// including bubbletea's own startup resize and any async cmd result that
// beat it to the channel — has been processed.
type headlessProgram struct {
	app     *App
	program *tea.Program
	done    chan struct{} // closed when p.Run returns
	out     *bytes.Buffer
	stub    *rpcStub
}

// send delivers one message to the App's event loop; blocks until the
// loop has RECEIVED it.
func (h *headlessProgram) send(msg tea.Msg) {
	h.program.Send(msg)
}

// settle drains to a known point: it sends one more WindowSizeMsg so the
// final processed state is the fixed size regardless of where bubbletea's
// own startup resize or any pending async cmd result landed. Call it
// after a scripted sequence and before any View() snapshot.
func (h *headlessProgram) settle(w, hgt int) {
	h.send(tea.WindowSizeMsg{Width: w, Height: hgt})
}

// finish stops the program and waits for p.Run to return, handing back
// the App for race-free reads: once the loop goroutine is gone, View()
// and field reads on the model are plain single-goroutine accesses.
// This is the assertion entry point for every steady-state check —
// reading View() while the loop runs would race it.
//
// After quit, the fixed window size is re-applied directly on the model:
// bubbletea's own startup resize (WindowSizeMsg{0,0}, sent via a goroutine
// during Run) can land AFTER every scripted message and clobber the size —
// Run returning means that message was already processed, so re-applying
// here is final. Single-goroutine Update, no race.
func (h *headlessProgram) finish(w, hgt int) *App {
	h.quit()
	h.app.Update(tea.WindowSizeMsg{Width: w, Height: hgt})
	return h.app
}

// quit stops the program and waits for p.Run to return. Registered as a
// t.Cleanup by newHeadlessApp; safe to call again (quit is once-guarded
// by the closed done channel and tea.Quit idempotence).
func (h *headlessProgram) quit() {
	h.program.Quit()
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		// p.Run should return promptly after Quit; do not hang the suite.
	}
}

// rpcStub is a minimal in-process JSON-RPC server over a unix socket that
// speaks the daemon's length-prefixed wire protocol (decimal length line
// + JSON payload). Every method is answered with the mapped result —
// unknown methods get {"ok":true}. This gives the App's init-time RPC
// cmds deterministic instant success, which is what makes goldens of
// loaded views race-free (a dead socket makes every init fetch resolve
// asynchronously with an error that lands AFTER any ordering barrier the
// event loop can offer).
type rpcStub struct {
	listener net.Listener
	mu       sync.Mutex
	results  map[string]any // method -> result value (marshaled per call)

	socketPath string
	t          *testing.T
}

// newRPCStub starts a stub server on a fresh socket under t.TempDir().
func newRPCStub(t *testing.T) *rpcStub {
	t.Helper()
	dir, err := os.MkdirTemp("", "meept-tui-stub-*")
	if err != nil {
		t.Fatalf("rpc stub: temp dir: %v", err)
	}
	// Keep the socket path short (sun_path limits).
	path := filepath.Join(dir, "s.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("rpc stub: listen: %v", err)
	}
	s := &rpcStub{listener: l, results: map[string]any{}, socketPath: path, t: t}
	go s.acceptLoop()
	t.Cleanup(func() {
		l.Close()
		os.RemoveAll(dir)
	})
	return s
}

// setResult maps one RPC method to a canned result value. The value is
// marshaled per call; handlers return it as the JSON-RPC "result".
func (s *rpcStub) setResult(method string, result any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[method] = result
}

func (s *rpcStub) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return // listener closed by cleanup
		}
		go s.serve(conn)
	}
}

func (s *rpcStub) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	for {
		var length int
		if _, err := fmt.Fscanf(r, "%d\n", &length); err != nil {
			return
		}
		buf := make([]byte, length)
		if _, err := readFull(r, buf); err != nil {
			return
		}
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(buf, &req); err != nil {
			return
		}
		s.mu.Lock()
		result := s.results[req.Method]
		s.mu.Unlock()
		if result == nil {
			result = map[string]any{"ok": true}
		}
		respData, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
		if err != nil {
			return
		}
		fmt.Fprintf(conn, "%d\n", len(respData))
		conn.Write(respData)
	}
}

func readFull(r *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// newHeadlessApp constructs the App against the stub RPC server, starts a
// real headless program loop over it, connects, and applies the fixed
// WindowSizeMsg. Every golden and flow test builds on this.
func newHeadlessApp(t *testing.T, w, h int) *headlessProgram {
	t.Helper()

	stub := newRPCStub(t)
	// Minimal canned results for the App's init-time fetches: an empty
	// session list + empty task/queue lists keep the loaded views
	// deterministic while the test seeds its own data via Send.
	stub.setResult("session.list", map[string]any{"sessions": []any{}})
	stub.setResult("task.list_extended", map[string]any{"tasks": []any{}})
	stub.setResult("queue.stats", map[string]any{"by_state": map[string]int{}})
	stub.setResult("queue.jobs", map[string]any{"jobs": []any{}})
	stub.setResult("plan.list_by_session", map[string]any{"plans": []any{}})

	hp := newHeadlessAppSocket(t, stub.socketPath, w, h)
	hp.stub = stub
	// The program's own Init fires connectDaemon; on success it emits
	// ConnectSuccessMsg (initCurrentView + sidebar + loadSession — all
	// served instantly by the stub). Do NOT send ConnectSuccessMsg here:
	// App.Init already dispatched one, and a second concurrent loadSession
	// races the first on sessionMgr (pre-existing product race the tests
	// must not amplify). Just wait for the socket connect: sub-model
	// fetches guard on IsConnected, and a fetch that runs before connect
	// completes would fail and poison the view with an error that lands
	// after every ordering barrier. IsConnected is an atomic bool — safe
	// to poll while the loop runs.
	deadline := time.Now().Add(5 * time.Second)
	for !hp.app.rpc.IsConnected() && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	hp.settle(w, h)
	return hp
}

// newHeadlessAppSocket is the raw constructor for both variants above.
func newHeadlessAppSocket(t *testing.T, socketPath string, w, h int) *headlessProgram {
	t.Helper()
	if socketPath == "" {
		socketPath = "/tmp/meept-test-nonexistent.sock"
	}

	app := createTestApp()
	rpc := NewRPCClient(socketPath)
	app.rpc = rpc
	// createTestApp leaves sessionMgr nil (NewApp sets it); loadSession
	// needs it as soon as any fetch succeeds.
	app.sessionMgr = sharedclient.NewSessionManager(rpc, app.clientConfig.Session.DefaultName)
	app.chat = models.NewChatModel(rpc, DefaultStyles().UserMessage, DefaultStyles().AssistantMessage, DefaultStyles().SystemMessage, "once")
	app.sessions = models.NewSessionsModel(rpc)
	app.tasks = models.NewTasksModel(rpc)
	app.queue = models.NewQueueModel(rpc)
	app.memory = models.NewMemoryModel(rpc)
	app.plans = models.NewPlansModel(rpc)

	var out bytes.Buffer
	var in bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	p := tea.NewProgram(app,
		tea.WithContext(ctx),
		tea.WithInput(&in),
		tea.WithOutput(&out),
	)

	hp := &headlessProgram{app: app, program: p, out: &out, done: make(chan struct{})}
	go func() {
		_, _ = p.Run()
		close(hp.done)
	}()
	t.Cleanup(hp.quit)

	hp.send(tea.WindowSizeMsg{Width: w, Height: h})
	hp.settle(w, h)
	return hp
}

// keyPress builds the KeyPressMsg for a single rune (helper so scripted
// sequences read as data, not struct literals).
func keyPress(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}
