//go:build e2e

// helpers_test.go: suite-local primitives for the kanban e2e suite
// (plan leaf 06/02). RPC helpers mirror the rpc-ping suite's local copies
// (the harness package is sibling-shared and deliberately not extended);
// the queue.db reader mirrors e2e/suites/task-queue/queue_helpers_test.go.
package kanban

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	_ "modernc.org/sqlite" // pure-Go sqlite driver (module dep of the daemon)
)

// rpcCall sends one JSON-RPC request over the sandbox Unix socket and
// decodes the full response envelope. Transport errors fail the test;
// application-level errors are returned for the caller to assert on.
func rpcCall(t *testing.T, socketPath, method string, params any) map[string]any {
	t.Helper()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial %s: %v", socketPath, err)
	}
	defer conn.Close()

	req := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		req["params"] = params
	}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if _, err := fmt.Fprintf(conn, "%d\n", len(payload)); err != nil {
		t.Fatalf("write length: %v", err)
	}
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	reader := bufio.NewReader(conn)
	lengthLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read length line for %s: %v", method, err)
	}
	var length int
	if _, err := fmt.Sscanf(strings.TrimSpace(lengthLine), "%d", &length); err != nil || length <= 0 {
		t.Fatalf("%s: bad length line %q", method, lengthLine)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(reader, buf); err != nil {
		t.Fatalf("%s: read payload: %v", method, err)
	}
	var resp map[string]any
	if err := json.Unmarshal(buf, &resp); err != nil {
		t.Fatalf("%s: decode response %q: %v", method, buf, err)
	}
	return resp
}

// rpcResult asserts the call succeeded (no error object) and returns the
// result object.
func rpcResult(t *testing.T, s *harness.Stack, method string, params any) map[string]any {
	t.Helper()
	resp := rpcCall(t, s.SocketPath, method, params)
	if e, ok := resp["error"]; ok && e != nil {
		t.Fatalf("%s: RPC error: %v", method, e)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("%s: result is not an object: %v", method, resp["result"])
	}
	return result
}

// rpcErr asserts the call FAILED and returns the error message text — the
// frozen C1 refusal strings ride the error path, never a success envelope
// (internal/rpc/kanban.go:43-53), so negative cases must read the envelope.
func rpcErr(t *testing.T, s *harness.Stack, method string, params any) string {
	t.Helper()
	resp := rpcCall(t, s.SocketPath, method, params)
	e, ok := resp["error"].(map[string]any)
	if !ok || e == nil {
		t.Fatalf("%s: expected RPC error, got result: %v", method, resp["result"])
	}
	msg, _ := e["message"].(string)
	if msg == "" {
		t.Fatalf("%s: error envelope without message: %v", method, e)
	}
	return msg
}

// str fetches a string field from a decoded RPC result map.
func str(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// startEnabled boots the harness daemon with [kanban].enabled plus any
// extra config keys (e.g. "kanban.wip_mode").
func startEnabled(t *testing.T, extra map[string]any) *harness.Stack {
	t.Helper()
	keys := map[string]any{"kanban.enabled": true}
	for k, v := range extra {
		keys[k] = v
	}
	return harness.Start(t, harness.WithConfigOverlay(keys))
}

// defaultBoard resolves the daemon-seeded "meept board" and its todo/doing/
// done column ids over the live RPC surface (case (g)'s seeding proof is
// asserted separately; here it is fixture plumbing).
func defaultBoard(t *testing.T, s *harness.Stack) (string, map[string]string) {
	t.Helper()
	out := rpcResult(t, s, "kanban.board.list", nil)
	boards, _ := out["boards"].([]any)
	var boardID string
	for _, b := range boards {
		bm, _ := b.(map[string]any)
		if bm != nil && str(bm, "name") == "meept board" {
			boardID = str(bm, "id")
			break
		}
	}
	if boardID == "" {
		t.Fatalf("seeded default board %q not found in %v", "meept board", out)
	}
	cols := rpcResult(t, s, "kanban.column.list", map[string]any{"board_id": boardID})
	byName := map[string]string{}
	list, _ := cols["columns"].([]any)
	for _, c := range list {
		cm, _ := c.(map[string]any)
		if cm != nil {
			byName[str(cm, "name")] = str(cm, "id")
		}
	}
	for _, name := range []string{"todo", "doing", "done"} {
		if byName[name] == "" {
			t.Fatalf("seeded column %q missing on board %s (%v)", name, boardID, cols)
		}
	}
	return boardID, byName
}

// createManualCard creates a NULL-ref (manual) card and returns its id.
func createManualCard(t *testing.T, s *harness.Stack, boardID, columnID, title string) string {
	t.Helper()
	out := rpcResult(t, s, "kanban.card.create", map[string]any{
		"board_id": boardID, "column_id": columnID, "title": title,
	})
	card, _ := out["card"].(map[string]any)
	if card == nil {
		t.Fatalf("card.create returned no card: %v", out)
	}
	return str(card, "id")
}

// cardsInColumn returns the column's cards as decoded DTOs.
func cardsInColumn(t *testing.T, s *harness.Stack, columnID string) []map[string]any {
	t.Helper()
	out := rpcResult(t, s, "kanban.card.list", map[string]any{"column_id": columnID})
	raw, _ := out["cards"].([]any)
	cards := make([]map[string]any, 0, len(raw))
	for _, c := range raw {
		if cm, ok := c.(map[string]any); ok {
			cards = append(cards, cm)
		}
	}
	return cards
}

// cardColumn reads a card's current column_id via kanban.card.get.
func cardColumn(t *testing.T, s *harness.Stack, cardID string) string {
	t.Helper()
	out := rpcResult(t, s, "kanban.card.get", map[string]any{"card_id": cardID})
	card, _ := out["card"].(map[string]any)
	if card == nil {
		t.Fatalf("card.get(%s) returned no card: %v", cardID, out)
	}
	return str(card, "column_id")
}

// timeline returns the card's timeline rows (kind list, oldest-first).
func timeline(t *testing.T, s *harness.Stack, cardID string) []map[string]any {
	t.Helper()
	out := rpcResult(t, s, "kanban.card.timeline", map[string]any{"card_id": cardID})
	raw, _ := out["events"].([]any)
	events := make([]map[string]any, 0, len(raw))
	for _, e := range raw {
		if em, ok := e.(map[string]any); ok {
			events = append(events, em)
		}
	}
	return events
}

// ---------------------------------------------------------------------------
// queue plumbing (real lease machinery, C10)
// ---------------------------------------------------------------------------

// enqueueIsolatedJob enqueues a one_off job carrying a capability no pool
// worker holds, so ONLY this suite's queue.claim (with the matching cap)
// can claim it — the daemon's own workers skip it (CanBeClaimedBy).
func enqueueIsolatedJob(t *testing.T, s *harness.Stack, cap string) string {
	t.Helper()
	out := rpcResult(t, s, "queue.enqueue", map[string]any{
		"type":          "one_off",
		"prompt":        "e2e kanban lease probe",
		"required_caps": []string{cap},
	})
	jobID := str(out, "id")
	if jobID == "" {
		t.Fatalf("queue.enqueue returned no job id: %v", out)
	}
	return jobID
}

// claimJob claims exactly one job with the given capability; returns the
// claimed job id.
func claimJob(t *testing.T, s *harness.Stack, workerID, cap string) string {
	t.Helper()
	out := rpcResult(t, s, "queue.claim", map[string]any{
		"worker_id":    workerID,
		"capabilities": []string{cap},
	})
	return str(out, "id")
}

// queueJobState reads the daemon's queue.db read-only (the task-queue
// suite's convention: assertions read persisted state directly, never the
// RPC surface the mutation itself reports through).
func queueJobState(t *testing.T, s *harness.Stack, jobID string) (state, claimedBy string) {
	t.Helper()
	dbPath := filepath.Join(s.StateDir, "queue.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		t.Fatalf("open queue.db: %v", err)
	}
	defer db.Close()
	err = db.QueryRow(`SELECT state, COALESCE(claimed_by,'') FROM jobs WHERE id = ?`, jobID).
		Scan(&state, &claimedBy)
	if err == sql.ErrNoRows {
		return "", ""
	}
	if err != nil {
		t.Fatalf("query queue.db job %s: %v", jobID, err)
	}
	return state, claimedBy
}

// waitSecondBoundary sleeps until the wall-clock second flips. The card
// timeline orders by created_at (RFC3339, SECOND precision) with a random
// id tiebreak, so any ordering assertion that spans two timeline writes
// must guarantee they land in different seconds.
func waitSecondBoundary(t *testing.T) {
	t.Helper()
	first := time.Now().UTC().Unix()
	for i := 0; i < 120 && time.Now().UTC().Unix() == first; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if time.Now().UTC().Unix() == first {
		t.Fatalf("wall-clock second never advanced (clock stuck?)")
	}
}
