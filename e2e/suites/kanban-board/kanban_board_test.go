//go:build e2e

// Suite kanban-board (kanban-board plan leaf 04/02, sidecar gateway).
//
// Coverage:
//
//	kanban-board-01  the REAL cmd/meept-kanban binary (go build, exec)
//	                 against the harness daemon's unix socket: index
//	                 serves 200; every API route answers 401 without a
//	                 token (empty grant store is the correct unpaired
//	                 state until leaf 04/03); unknown api path 404; wrong
//	                 verb 405 (verb check precedes auth, pinned); SIGTERM
//	                 drains and exits 0.
//	kanban-board-02  C7 bind guard as a process boundary: a non-loopback
//	                 -bind without -allow-lan refuses to start (exit != 0,
//	                 reason on stderr) and nothing ever listens there.
//
// Hermetic: 127.0.0.1 + unix sockets only; no LLM call is made (the
// suite never authenticates, so no kanban.* RPC reaches the daemon —
// those handlers land in leaf 05/01).
package kanbanboard

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// repoRoot walks up from the test package dir (e2e/suites/kanban-board)
// to the module root, the same pattern voice-lifecycle's buildFakeChild
// uses.
func repoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(cwd)))
	if _, err := os.Stat(filepath.Join(root, "cmd", "meept-kanban", "main.go")); err != nil {
		t.Fatalf("module root %s: %v", root, err)
	}
	return root
}

func buildSidecar(t *testing.T, dir string) string {
	t.Helper()
	out := filepath.Join(dir, "meept-kanban")
	cmd := exec.Command("go", "build", "-o", out, "./cmd/meept-kanban")
	cmd.Dir = repoRoot(t)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build meept-kanban: %v", err)
	}
	return out
}

// freePort reserves a loopback port the same way the daemon harness does:
// bind :0, read the port, release it.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

// startSidecar launches the binary pinned to the stack's daemon socket and
// waits for the listener to accept connections.
func startSidecar(t *testing.T, bin, socket string, port int) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(bin,
		"-bind", fmt.Sprintf("127.0.0.1:%d", port),
		"-daemon-socket", socket,
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sidecar: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/")
		if err == nil {
			resp.Body.Close()
			return cmd
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("sidecar never listened on port %d", port)
	return nil
}

// wsHandshakeCode performs a raw WebSocket upgrade handshake and returns
// the HTTP status line the server answered. x/net/websocket writes its
// handshake rejections as a raw status line (it hijacks the connection),
// which net/http's client cannot parse — so this reads the socket.
func wsHandshakeCode(t *testing.T, rawURL string, headers map[string]string) int {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %s: %v", rawURL, err)
	}
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatalf("dial %s: %v", rawURL, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	var b strings.Builder
	fmt.Fprintf(&b, "GET %s HTTP/1.1\r\nHost: %s\r\n", u.Path, u.Host)
	fmt.Fprintf(&b, "Upgrade: websocket\r\nConnection: Upgrade\r\n")
	fmt.Fprintf(&b, "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n")
	fmt.Fprintf(&b, "Sec-WebSocket-Version: 13\r\n")
	for k, v := range headers {
		fmt.Fprintf(&b, "%s: %s\r\n", k, v)
	}
	b.WriteString("\r\n")
	if _, err := conn.Write([]byte(b.String())); err != nil {
		t.Fatalf("write handshake: %v", err)
	}

	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read status line: %v", err)
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		t.Fatalf("bad status line %q", line)
	}
	code, err := strconv.Atoi(fields[1])
	if err != nil {
		t.Fatalf("status %q: %v", fields[1], err)
	}
	return code
}

func get(t *testing.T, target string) (int, string) {
	t.Helper()
	resp, err := http.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		t.Fatalf("read body %s: %v", target, err)
	}
	return resp.StatusCode, string(body)
}

func TestSidecarServesUnpairedGateway(t *testing.T) {
	stack := harness.Start(t)
	bin := buildSidecar(t, t.TempDir())
	port := freePort(t)
	cmd := startSidecar(t, bin, stack.SocketPath, port)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	// 1. Index serves (placeholder or bundle — both are 200).
	if code, _ := get(t, base+"/"); code != http.StatusOK {
		t.Errorf("GET / = %d, want 200", code)
	}

	// 2. Every REST API route answers 401 with no token: the empty
	//    grant store is correct until pairing (04/03) lands. (The WS
	//    relay refuses in its handshake too, but x/net/websocket
	//    answers every handshake rejection with 403 — the 401/403
	//    distinction is a REST-surface contract.)
	for _, path := range []string{
		"/api/v1/board/list",
		"/api/v1/card/get",
	} {
		code, body := get(t, base+path)
		if code != http.StatusUnauthorized {
			t.Errorf("GET %s unauthenticated = %d, want 401 (body %q)", path, code, body)
		}
	}

	// 2b. The WS relay refuses an unauthenticated upgrade: the handshake
	//     fails, so no 101 ever comes back.
	wsCode := wsHandshakeCode(t, base+"/api/v1/events", nil)
	if wsCode == http.StatusSwitchingProtocols {
		t.Errorf("WS handshake without token = 101, want refusal")
	}

	// 3. Unknown api path 404s even with the auth shape present.
	if code, _ := get(t, base+"/api/v1/nope"); code != http.StatusNotFound {
		t.Errorf("GET unknown api = %d, want 404", code)
	}

	// 4. Verb check precedes auth: wrong method 405s without a token.
	resp, err := http.Post(base+"/api/v1/board/list", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST board/list: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/v1/board/list = %d, want 405 (verb check precedes grant check)", resp.StatusCode)
	}

	// 5. Graceful shutdown: SIGTERM drains the listener and exits 0.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("sidecar exit after SIGTERM = %v, want clean exit", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("sidecar did not exit within 15s of SIGTERM")
	}
}

func TestBindGuardRefusesLANWithoutFlag(t *testing.T) {
	bin := buildSidecar(t, t.TempDir())
	port := freePort(t)
	socket := filepath.Join(t.TempDir(), "unused.sock")

	cmd := exec.Command(bin, "-bind", fmt.Sprintf("0.0.0.0:%d", port), "-daemon-socket", socket)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatalf("sidecar started on 0.0.0.0 with no -allow-lan: C7 guard is inert")
	}
	if !strings.Contains(stderr.String(), "non-loopback") {
		t.Errorf("stderr = %q, want the C7 refusal naming -allow-lan", stderr.String())
	}

	// Nothing may be listening there afterwards.
	conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if dialErr == nil {
		conn.Close()
		t.Errorf("port %d accepts connections after the refused start", port)
	}
	_ = os.Remove(socket)
}
