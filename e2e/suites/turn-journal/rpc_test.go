//go:build e2e

// Shared connection helpers for the turn-journal suite: the persistent
// framed-protocol JSON-RPC session (bus subscriptions are tied to the RPC
// connection that created them) and the scratch-cert TLS config.
package turnjournal

import (
	"bufio"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// insecureTLS returns the scratch-cert TLS config (same posture as the
// harness's own clients: the daemon uses a self-signed cert in sandboxes).
func insecureTLS() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // self-signed scratch cert
}

// rpcSession is one persistent framed-protocol JSON-RPC connection
// (async-turn suite precedent: bus subscriptions live on the connection).
type rpcSession struct {
	conn   net.Conn
	reader *bufio.Reader
}

// dialRPC dials the daemon socket; caller must Close it.
func dialRPC(socketPath string) (*rpcSession, error) {
	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		return nil, err
	}
	return &rpcSession{conn: conn, reader: bufio.NewReader(conn)}, nil
}

// dialRPCF is the testing.TB-flavored dial used by test bodies.
func dialRPCF(t tb, socketPath string) *rpcSession {
	s, err := dialRPC(socketPath)
	if err != nil {
		t.Fatalf("dial unix %s: %v", socketPath, err)
	}
	return s
}

// Close terminates the session.
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

// tb is the minimal testing surface the helpers need (keeps this file
// free of a hard testing import for the plain-dial path).
type tb interface {
	Helper()
	Fatalf(format string, args ...any)
}

// dialRPCS is the dialRPCF for *testing.T call sites in the suite.
func dialRPCS(t tb, socketPath string) *rpcSession { return dialRPCF(t, socketPath) }

// drainCompletionEvents polls bus.poll on sub for the drain window and
// counts turn.terminal successes for turnID and queue.job.completed events.
func drainCompletionEvents(s *rpcSession, subID, turnID string, window time.Duration) (int, int) {
	deadline := time.Now().Add(window)
	terminal, queued := 0, 0
	for time.Now().Before(deadline) {
		raw, err := s.call("bus.poll", map[string]string{"subscription_id": subID})
		if err == nil {
			var resp struct {
				Events []struct {
					Topic   string          `json:"topic"`
					Payload json.RawMessage `json:"payload"`
				} `json:"events"`
			}
			if json.Unmarshal(raw, &resp) == nil {
				for _, ev := range resp.Events {
					switch ev.Topic {
					case "turn.terminal":
						var p struct {
							TurnID string `json:"turn_id"`
							Status string `json:"status"`
						}
						if json.Unmarshal(ev.Payload, &p) == nil && p.TurnID == turnID && p.Status != "parked" && p.Status != "failed" {
							terminal++
						}
					case "queue.job.completed":
						var p struct {
							TaskID string `json:"task_id"`
						}
						if json.Unmarshal(ev.Payload, &p) == nil && (p.TaskID == "" || p.TaskID == turnID) {
							queued++
						}
					}
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return terminal, queued
}

// queueJobRow mirrors one row of the persistent queue's jobs table
// (internal/queue/store.go baseSchema - the columns the dedup scenario
// asserts on).
type queueJobRow struct {
	ID     string
	TaskID string
	State  string
}

// readQueueJobs reads the persistent queue's job rows (read-only SQL over
// $MEEPT_HOME/queue.db - the daemon's own job store).
func readQueueJobs(t tb, dbPath string) []queueJobRow {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		t.Fatalf("open queue.db: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id, COALESCE(task_id,''), COALESCE(state,'') FROM jobs`)
	if err != nil {
		t.Fatalf("query jobs: %v", err)
	}
	defer rows.Close()
	var out []queueJobRow
	for rows.Next() {
		var r queueJobRow
		if err := rows.Scan(&r.ID, &r.TaskID, &r.State); err != nil {
			t.Fatalf("scan job row: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate jobs: %v", err)
	}
	return out
}

// drainDuplicateCompletions polls bus.poll on sub and returns the job IDs
// from wanted that delivered MORE than one queue.job.completed during the
// window (the per-job duplicate-completion probe). A single delivery is
// the contract; zero deliveries for a completed job is equally fine (the
// restart edge produced nothing at all).
func drainDuplicateCompletions(s *rpcSession, subID string, wanted []string, window time.Duration) []string {
	wantSet := map[string]bool{}
	for _, id := range wanted {
		wantSet[id] = true
	}
	counts := map[string]int{}
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		raw, err := s.call("bus.poll", map[string]string{"subscription_id": subID})
		if err == nil {
			var resp struct {
				Events []struct {
					Topic   string          `json:"topic"`
					Payload json.RawMessage `json:"payload"`
				} `json:"events"`
			}
			if json.Unmarshal(raw, &resp) == nil {
				for _, ev := range resp.Events {
					if ev.Topic != "queue.job.completed" {
						continue
					}
					var p struct {
						JobID string `json:"job_id"`
					}
					if json.Unmarshal(ev.Payload, &p) == nil && wantSet[p.JobID] {
						counts[p.JobID]++
					}
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	var dupes []string
	for id, n := range counts {
		if n > 1 {
			dupes = append(dupes, fmt.Sprintf("%s x%d", id, n))
		}
	}
	return dupes
}
