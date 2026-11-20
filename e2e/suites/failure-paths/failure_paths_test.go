//go:build e2e

// Package failurepaths is the 01-safety TEST-GATE T10 e2e suite for the
// failure terminalization contracts (W6/W7/W26/W27; master matrix F2/F3,
// scenarios failure-paths-01..02; C1/C6).
//
// Boots-leg tests through the real hermetic daemon:
//   - a task failure injected on the live bus must relay ONE honest
//     failed turn.terminal under the ORIGINATING turn id (never a fresh
//     id, never silent);
//   - GET /api/v1/plans must answer honestly (200 with a plans list)
//     when the evolver sink is disabled - the typed-nil fallback store
//     must not crash the request (F-NEW-1).
//
// RED today: the injected-failure relay arm depends on the 01-safety/05
// terminalization wiring; the plans arm pins the typed-nil guard through
// the REAL HTTP surface (the unit gates pin the store seam).

package failurepaths

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// terminalEvent mirrors the C1 fields this suite asserts on.
type terminalEvent struct {
	TurnID      string `json:"turn_id"`
	TaskID      string `json:"task_id"`
	HandlerCase string `json:"handler_case"`
	Status      string `json:"status"`
	Reply       string `json:"reply"`
	Error       string `json:"error"`
}

// collectTerminals drains every buffered turn.terminal event so far.
func collectTerminals(t *testing.T, client *harness.RPCClient, subID string) []terminalEvent {
	t.Helper()
	raw := client.CallResult("bus.poll", map[string]any{"subscription_id": subID})
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal poll result: %v", err)
	}
	var page struct {
		Events []struct {
			Topic   string          `json:"topic"`
			Payload json.RawMessage `json:"payload"`
		} `json:"events"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatalf("decode poll page: %v", err)
	}
	var out []terminalEvent
	for _, ev := range page.Events {
		if ev.Topic != "turn.terminal" {
			continue
		}
		var te terminalEvent
		if json.Unmarshal(ev.Payload, &te) == nil {
			out = append(out, te)
		}
	}
	return out
}

// TestInjectedTaskFailureRelaysOriginatingTurn pins the failure relay
// funnel end to end: a dispatched turn whose task fails (failure injected
// on the live task.failed topic with the producer's payload shape) must
// deliver exactly ONE failed turn.terminal under the ack's turn id with
// an actionable error - never a completed terminal, never silence, never
// a fresh turn id the client cannot correlate.
func TestInjectedTaskFailureRelaysOriginatingTurn(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "failure-project")
	sessionID := s.CreateSession(t, "failure-relay", s.ProjectDir)

	client := harness.DialRPC(t, s.SocketPath)
	sub := client.CallResult("bus.subscribe", map[string]any{
		"topics": []string{"turn.terminal"},
	})
	subID, _ := sub["subscription_id"].(string)
	if subID == "" {
		t.Fatalf("bus.subscribe returned no subscription_id: %v", sub)
	}

	// Stall the executor so the turn stays tracked while the failure is
	// injected (a completed turn would detach the task->turn edge).
	s.Fake.Script(harness.IsExecutorRequest(), harness.HTTPStatusResponse(500, `{"error":"e2e failure-lane stall"}`))

	ack := s.SubmitChatHTTP(t, sessionID, "create a file named doomed.txt containing failure probe")
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("ack missing turn_id: %+v", ack)
	}

	// Wait for the dispatched task row, then inject the producer-shaped
	// task.failed payload (internal/agent/ralph_loop.go publish site).
	var taskID string
	harness.WaitFor(t, 60*time.Second, "dispatched task row", func() bool {
		rows := harness.Tasks(t, s.TasksDBPath())
		if len(rows) == 0 {
			return false
		}
		taskID = rows[len(rows)-1].ID
		return true
	})
	const failReason = "e2e injected failure: step evidence gate rejected the write"
	client.CallResult("bus.publish", map[string]any{
		"topic": "task.failed",
		"payload": map[string]any{
			"task_id":         taskID,
			"name":            "failure probe task",
			"failed_jobs":     1,
			"completed_jobs":  0,
			"total_jobs":      1,
			"linked_sessions": []string{sessionID},
			"error":           failReason,
		},
	})

	deadline := time.Now().Add(30 * time.Second)
	var failed []terminalEvent
	for time.Now().Before(deadline) {
		for _, te := range collectTerminals(t, client, subID) {
			if te.Status == "failed" && (te.TurnID == turnID || te.TaskID == taskID) {
				failed = append(failed, te)
			}
			if te.TurnID == turnID && te.Status == "completed" {
				t.Fatalf("CURRENT DEFECT: failed task %s relayed a COMPLETED terminal for turn %s (fabricated success on failure)", taskID, turnID)
			}
		}
		if len(failed) > 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if len(failed) == 0 {
		t.Fatalf("CURRENT DEFECT: task.failed for %s produced NO failed turn.terminal within 30s; every failure must reach the user under the originating turn %s (never silent)", taskID, turnID)
	}
	for _, te := range failed {
		if te.TurnID != turnID {
			t.Errorf("failure relay used turn id %q, want the ORIGINATING turn id %q (fresh ids make the failure invisible to the awaiting client)", te.TurnID, turnID)
		}
		if !strings.Contains(te.Error, failReason) && !strings.Contains(te.Reply, failReason) {
			t.Errorf("failure terminal carries neither error nor reply quoting the actionable reason %q: %+v", failReason, te)
		}
	}
	if len(failed) > 1 {
		t.Errorf("failure relayed %d times for one task failure; exactly ONE terminal per failure (no re-relay storm): %+v", len(failed), failed)
	}
}

// TestPlansListHTTPNeverPanics pins F-NEW-1 through the REAL HTTP
// surface: GET /api/v1/plans must answer 200 with a plans list on a
// default scratch daemon (evolver sink disabled). RED when the typed-nil
// fallback store crashes the request (500 / connection reset); GREEN
// after the reflect-safe nil guard lands. The store-seam mechanics are
// pinned by the T12 unit gates; this arm proves the user-facing route.
func TestPlansListHTTPNeverPanics(t *testing.T) {
	s := harness.Start(t)
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
		},
	}
	resp, err := client.Get(s.HTTPBaseURL() + "/api/v1/plans") //nolint:noctx // bounded gate call
	if err != nil {
		t.Fatalf("CURRENT DEFECT (F-NEW-1): GET /api/v1/plans crashed the request: %v", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		t.Fatalf("read plans body: %v", readErr)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CURRENT DEFECT (F-NEW-1): GET /api/v1/plans status = %d, want 200 (typed-nil sink store must fall back to the shared store, never panic): %s", resp.StatusCode, string(body))
	}
	var out struct {
		Plans json.RawMessage `json:"plans"`
		Count int             `json:"count"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("plans body does not decode: %v (%s)", err, string(body))
	}
	if len(out.Plans) == 0 {
		t.Errorf("plans response carries no plans key: %s", string(body))
	}
}
