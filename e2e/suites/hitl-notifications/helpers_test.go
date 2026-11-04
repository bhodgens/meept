//go:build e2e

// Shared helpers for the hitl-notifications suite: the persistent
// framed-protocol JSON-RPC session (bus subscriptions and per-connection
// state live on the RPC connection), a raw websocket probe for the web
// fallback substrate, the stale-pidfile cleanup a SIGKILLed restart needs,
// and the read-only sqlite probe for the parks.db gate rows.
package hitlnotifications

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// Persistent JSON-RPC session (async-turn precedent: connection-scoped).
// ---------------------------------------------------------------------------

type rpcSession struct {
	conn   net.Conn
	reader *bufio.Reader
}

func dialRPC(socketPath string) (*rpcSession, error) {
	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		return nil, err
	}
	return &rpcSession{conn: conn, reader: bufio.NewReader(conn)}, nil
}

func dialRPCS(t *testing.T, socketPath string) *rpcSession {
	t.Helper()
	s, err := dialRPC(socketPath)
	if err != nil {
		t.Fatalf("dial unix %s: %v", socketPath, err)
	}
	return s
}

func (c *rpcSession) Close() {
	if c != nil && c.conn != nil {
		_ = c.conn.Close()
	}
}

// call performs one JSON-RPC request. Wire format: "<length>\n<payload>".
func (c *rpcSession) call(method string, params any) (json.RawMessage, error) {
	if c == nil || c.conn == nil {
		return nil, fmt.Errorf("rpc session closed")
	}
	req := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		req["params"] = params
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if err := c.conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(c.conn, "%d\n", len(payload)); err != nil {
		return nil, fmt.Errorf("write frame length: %w", err)
	}
	if _, err := c.conn.Write(payload); err != nil {
		return nil, fmt.Errorf("write frame payload: %w", err)
	}
	lengthLine, err := c.reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read frame length: %w", err)
	}
	length, err := strconv.Atoi(strings.TrimSpace(lengthLine))
	if err != nil {
		return nil, fmt.Errorf("invalid frame length: %w", err)
	}
	respData := make([]byte, length)
	if _, err := io.ReadFull(c.reader, respData); err != nil {
		return nil, fmt.Errorf("read frame payload: %w", err)
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", method, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("%s: [%d] %s", method, resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}

// ---------------------------------------------------------------------------
// Restart primitive (state-restart precedent: same flags/env as harness
// boot, over the SAME sandbox home).
// ---------------------------------------------------------------------------

// removeStalePidfile deletes the sandbox pidfile left behind by a
// SIGKILLed daemon: the new boot's stale-pid check probes liveness with
// signal 0, which succeeds on a zombie (dead but unreaped), so the boot
// would otherwise refuse with "daemon already running". Also settles past
// the queue's claim-reset second boundary.
func removeStalePidfile(t *testing.T, s *harness.Stack) {
	t.Helper()
	time.Sleep(2 * time.Second) // claim-reset settle (ResetStaleClaimsAtStartup cutoff)
	data, err := os.ReadFile(filepath.Join(s.StateDir, "meept.pid"))
	if err != nil {
		return
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err != nil || pid <= 0 {
		return
	}
	if !daemonAlive(pid) {
		_ = os.Remove(filepath.Join(s.StateDir, "meept.pid"))
	}
}

// daemonAlive reports whether the pid runs as a live (non-zombie) process.
func daemonAlive(pid int) bool {
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return false
	}
	stat := strings.TrimSpace(string(out))
	return stat != "" && !strings.HasPrefix(stat, "Z")
}

// bootRestart re-boots a fresh scratch daemon against the SAME sandbox
// home, waiting for /health. The returned func stops the re-booted daemon
// via the pidfile; the harness cleanup stays a safe no-op afterwards.
func bootRestart(t *testing.T, s *harness.Stack) func() {
	t.Helper()

	bin := harness.DaemonPath(t)
	logPath := filepath.Join(s.Work, "daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open daemon log: %v", err)
	}
	defer logFile.Close()

	cmd := exec.Command(bin,
		"-c", filepath.Join(s.MeeptHome, "meept.json5"),
		"-d", s.StateDir,
		"-s", s.SocketPath,
	)
	cmd.Dir = s.Work // NOT the project dir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = s.Env()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start re-booted daemon: %v", err)
	}

	client := insecureHTTP()
	healthURL := s.HTTPBaseURL() + "/health"
	deadline := time.Now().Add(90 * time.Second)
	for {
		resp, err := client.Get(healthURL) //nolint:noctx // bounded poll in test
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body) //nolint:errcheck // bounded poll
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatalf("re-booted daemon not healthy within 90s; log tail:\n%s", s.Daemon.LogTail())
		}
		if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
			t.Fatalf("re-booted daemon died during boot; log tail:\n%s", s.Daemon.LogTail())
		}
		time.Sleep(250 * time.Millisecond)
	}

	return func() {
		data, err := os.ReadFile(filepath.Join(s.StateDir, "meept.pid"))
		if err != nil {
			return
		}
		var pid int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err == nil && pid > 0 {
			proc, perr := os.FindProcess(pid)
			if perr == nil {
				_ = proc.Signal(syscall.SIGTERM)
				done := make(chan struct{})
				go func() { _, _ = proc.Wait(); close(done) }()
				select {
				case <-done:
				case <-time.After(10 * time.Second):
					_ = proc.Kill()
					<-done
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// parks.db gate-row probe (hitl-gate-02).
// ---------------------------------------------------------------------------

// countGateRows counts parked_turns persistence rows whose key carries the
// plan id (ParkKindGate rows persist under the gateParkKey
// "<gate_kind>|<gate_id>" - see internal/agent/gate_park.go).
func countGateRows(t *testing.T, parksDB, planID string) int {
	t.Helper()
	if _, err := os.Stat(parksDB); os.IsNotExist(err) {
		return 0
	}
	db, err := sql.Open("sqlite", parksDB+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		t.Fatalf("open parks.db: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT COUNT(*) FROM parked_turns WHERE park_key LIKE '%` + planID + `%' OR payload LIKE '%` + planID + `%'`)
	if err != nil {
		// Schema drift fallback: probe both candidate column layouts.
		rows2, err2 := db.Query(`SELECT COUNT(*) FROM parked_turns`)
		if err2 != nil {
			return 0
		}
		defer rows2.Close()
		return scanCount(rows2)
	}
	defer rows.Close()
	return scanCount(rows)
}

func scanCount(rows *sql.Rows) int {
	var n int
	if rows.Next() {
		_ = rows.Scan(&n) //nolint:errcheck // count probe
	}
	return n
}

// webBaseHost resolves the web server's host:port for the stack: the
// scenario's config overlay wrote {"web":{"host":...,"port":N}}; scrape
// it back from the sandbox meept.json5 (plain JSON).
func webBaseHost(t *testing.T, s *harness.Stack) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.MeeptHome, "meept.json5"))
	if err != nil {
		t.Fatalf("read sandbox config: %v", err)
	}
	var cfg struct {
		Web struct {
			Port int    `json:"port"`
			Host string `json:"host"`
		} `json:"web"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil || cfg.Web.Port == 0 {
		t.Fatalf("sandbox config carries no web port: %v (%s)", err, data)
	}
	host := cfg.Web.Host
	if host == "" {
		host = "127.0.0.1"
	}
	return host + ":" + strconv.Itoa(cfg.Web.Port)
}
