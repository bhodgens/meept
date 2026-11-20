//go:build e2e

// Package dispatchtrace is the 02-routing TEST-GATE T7 e2e suite for the
// C5 dispatch evidence surface (W9/W22; audit-routing R3; master matrix
// F7, scenarios dispatch-trace-01..03).
//
// Every scenario boots the full hermetic stack, submits a real turn
// (frozen usability wording), and queries session.dispatch_trace through
// the live RPC socket. Assertions are END STATES: rows returned under
// BOTH identity keys, each carrying turn_id joined to the submitted
// turn and a non-empty input_summary quoting the routed input.
//
// RED today: the primary-session-id query returns count=0 (the handler
// is session_id-only while rows record under the conversation id), and
// rows carry no turn_id / empty input_summary.

package dispatchtrace

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// frozenEditPrompt loads the p1 T2 edit prompt VERBATIM from the shared
// fixture (single source of truth with the unit gates).
func frozenEditPrompt(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "internal", "agent", "testdata", "usability_prompts.json"))
	if err != nil {
		t.Fatalf("read frozen usability prompts: %v", err)
	}
	var f struct {
		P1Naive []string `json:"p1_naive"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parse frozen prompts: %v", err)
	}
	if len(f.P1Naive) < 2 {
		t.Fatalf("fixture drift: p1 has %d turns", len(f.P1Naive))
	}
	return f.P1Naive[1]
}

// traceResult is the session.dispatch_trace response shape.
type traceResult struct {
	Count   int `json:"count"`
	Entries []struct {
		SessionID    string `json:"session_id"`
		TurnID       string `json:"turn_id"`
		InputSummary string `json:"input_summary"`
		IntentType   string `json:"intent_type"`
		AgentID      string `json:"agent_id"`
		HandlerCase  string `json:"handler_case"`
	} `json:"entries"`
}

// queryTrace calls session.dispatch_trace with one identity key. C5
// accepts session_id OR conversation_id; the gate sends BOTH spellings
// under session_id (the handler's only param today) so the dual-key
// resolution is exercised through the live surface.
func queryTrace(t *testing.T, s *harness.Stack, key string) traceResult {
	t.Helper()
	client := harness.DialRPC(t, s.SocketPath)
	raw := client.CallResult("session.dispatch_trace", map[string]any{"session_id": key})
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal trace result: %v", err)
	}
	var out traceResult
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode trace result: %v", err)
	}
	return out
}

// submitFrozenTurn submits the frozen edit prompt and returns the ack
// (turn_id + conversation_id) after the terminal lands.
func submitFrozenTurn(t *testing.T, s *harness.Stack, sessionID string) (turnID, convID string) {
	t.Helper()
	prompt := frozenEditPrompt(t)

	// Script the executor so the turn completes with real work (the
	// trace must record a completed dispatch, not a parked one).
	artifact := filepath.Join(s.ProjectDir, "hello.txt")
	s.Fake.ScriptN(1,
		harness.And(harness.IsExecutorRequest(), harness.Not(harness.IsPlannerRequest()),
			harness.MessageContains("hello")),
		harness.ToolCallResponse(harness.ToolCall{
			Name:      "file_write",
			Arguments: `{"path":"` + artifact + `","content":"hello meept","direct":true}`,
		}))
	s.Fake.SetPostToolText("Updated hello.txt.")

	ack := s.SubmitChatHTTP(t, sessionID, prompt)
	turnID, _ = ack["turn_id"].(string)
	convID, _ = ack["conversation_id"].(string)
	if turnID == "" {
		t.Fatalf("ack missing turn_id: %+v", ack)
	}
	return turnID, convID
}

// waitTraceRows polls until the trace query returns at least one row.
func waitTraceRows(t *testing.T, s *harness.Stack, key string, timeout time.Duration) traceResult {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last traceResult
	for time.Now().Before(deadline) {
		last = queryTrace(t, s, key)
		if len(last.Entries) > 0 {
			return last
		}
		time.Sleep(500 * time.Millisecond)
	}
	return last
}

// TestDispatchTrace01DualKeyByPrimarySessionID pins C5's core contract:
// after a real turn, querying by the session's PRIMARY id returns the
// dispatch row. RED today: count=0 (session_id-only lookup, rows keyed
// by conversation id).
func TestDispatchTrace01DualKeyByPrimarySessionID(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "trace-project")
	sessionID := s.CreateSession(t, "trace-dualkey", s.ProjectDir)

	turnID, convID := submitFrozenTurn(t, s, sessionID)

	// Wait for the row under the conversation key first (the write path
	// that works today), then assert the PRIMARY key resolves the same.
	convRows := waitTraceRows(t, s, convID, 30*time.Second)
	if len(convRows.Entries) == 0 {
		t.Fatalf("no dispatch row recorded under conversation id %q for turn %s", convID, turnID)
	}

	primary := queryTrace(t, s, sessionID)
	if len(primary.Entries) == 0 {
		t.Fatalf("session.dispatch_trace by primary session id %q returned 0 rows while the conversation key returns %d: C5 requires dual-key resolution (the GUI/TUI hold the primary id)",
			sessionID, len(convRows.Entries))
	}
}

// TestDispatchTrace02RowsJoinTurnWithInput pins the C5 row shape: the
// row for the submitted turn carries turn_id == the ack's turn id and a
// non-empty input_summary quoting the frozen input.
func TestDispatchTrace02RowsJoinTurnWithInput(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "trace-project")
	sessionID := s.CreateSession(t, "trace-turnjoin", s.ProjectDir)

	turnID, convID := submitFrozenTurn(t, s, sessionID)
	rows := waitTraceRows(t, s, convID, 30*time.Second)
	if len(rows.Entries) == 0 {
		t.Fatalf("no dispatch rows for turn %s", turnID)
	}

	var matched bool
	for _, e := range rows.Entries {
		if e.TurnID == turnID {
			matched = true
			if e.InputSummary == "" {
				t.Error("trace row input_summary is empty: C5 requires populated input text so the why-this-route popover shows what was routed")
			} else if !strings.Contains(e.InputSummary, "hello") {
				t.Errorf("input_summary %q does not quote the routed input (frozen edit prompt)", e.InputSummary)
			}
		}
	}
	if !matched {
		t.Errorf("no trace row carries turn_id %q: C5 requires the turn join key on every row (rows: %+v)", turnID, rows.Entries)
	}
}

// TestDispatchTrace03HTTPRoute pins the C5 HTTP twin: GET
// /api/v1/sessions/{id}/dispatch-trace answers 200 with the entries for
// either identity key; an unknown id yields an empty list, never a 500.
func TestDispatchTrace03HTTPRoute(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "trace-project")
	sessionID := s.CreateSession(t, "trace-http", s.ProjectDir)

	turnID, convID := submitFrozenTurn(t, s, sessionID)
	waitTraceRows(t, s, convID, 30*time.Second)
	if turnID == "" {
		t.Fatal("submit produced no turn id")
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
		},
	}
	for _, id := range []string{sessionID, convID} {
		resp, err := client.Get(s.HTTPBaseURL() + "/api/v1/sessions/" + id + "/dispatch-trace") //nolint:noctx // bounded gate call
		if err != nil {
			t.Fatalf("GET dispatch-trace for %s: %v", id, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			t.Fatalf("read dispatch-trace body: %v", readErr)
		}
		if resp.StatusCode != 200 {
			t.Errorf("GET /api/v1/sessions/%s/dispatch-trace status = %d, want 200 (C5 HTTP twin; route absent today): %s",
				id, resp.StatusCode, string(body))
			continue
		}
		var out traceResult
		if err := json.Unmarshal(body, &out); err != nil {
			t.Errorf("dispatch-trace HTTP body does not decode: %v (%s)", err, string(body))
			continue
		}
		if len(out.Entries) == 0 {
			t.Errorf("HTTP dispatch-trace for %s returned 0 entries; the RPC conversation key has rows", id)
		}
	}

	// Unknown id: empty list, not a 500.
	resp, err := client.Get(s.HTTPBaseURL() + "/api/v1/sessions/session-nope/dispatch-trace") //nolint:noctx // bounded gate call
	if err != nil {
		t.Fatalf("GET unknown-id dispatch-trace: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode == 500 {
		t.Error("unknown id returned 500: C5 requires an empty list (the popover renders 'no evidence')")
	}
}
