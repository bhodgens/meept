//go:build e2e

// Suite memory-turn-capture (plan docs/plans/20261013-turn-terminal-capture,
// Task 3): the turn.terminal memory-capture subscriber end to end against a
// REAL scratch daemon — L0 JSONL lines, the chained memory.l1_extract job
// storing a status=auto claim, and the failed-turn control.
//
//	01 completed DIRECT chat turn -> 2 L0 lines + the subscriber's ambient
//	   auto claim + the L1 job's auto claim
//	02 completed DISPATCHED turn (multiagent task lane) -> same assertions —
//	   the regression proof for the hook-registry gap
//	   (.github/ISSUES/task-loops-no-hook-registry-l0-ambient.md: the retired
//	   session-end hook never saw task-scoped loops)
//	03 FAILED turn (provider 500) -> zero new L0 lines for the session
//
// Config gates (both required; both default false):
//
//	memory.l0_enabled = true
//	memory.epistemic.ambient_extraction.enabled = true
//
// The fake LLM serves every memory-path prompt deterministically:
// the ambient-extraction / L1-extraction prompt (they share the "You are an
// epistemic extractor" template) returns a bare JSON candidate array whose
// text carries the per-suite marker; the L2 scene-segmentation prompt returns
// a whole-session segment so the chained L2 job completes with zero
// dead-letters.
package memoryturncapture

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/memory"
	_ "modernc.org/sqlite" // pure-Go sqlite driver (module dep of the daemon)
)

// terminalEvent mirrors the frozen turn.terminal payload fields this suite
// filters on (subset of agent.TurnTerminalEvent).
type terminalEvent struct {
	ConversationID string `json:"conversation_id"`
	SessionID      string `json:"session_id"`
	TurnID         string `json:"turn_id"`
	TaskID         string `json:"task_id"`
	HandlerCase    string `json:"handler_case"`
	Status         string `json:"status"`
	Reply          string `json:"reply"`
	Error          string `json:"error"`
}

// ---------------------------------------------------------------------------
// turn.terminal collector (single persistent RPC connection — subscriptions
// die with the connection that created them).
// ---------------------------------------------------------------------------

type rpcSession struct {
	conn   net.Conn
	reader *bufio.Reader
}

func openRPCSession(t *testing.T, s *harness.Stack) *rpcSession {
	t.Helper()
	conn, err := net.DialTimeout("unix", s.SocketPath, 5*time.Second)
	if err != nil {
		t.Fatalf("dial unix %s: %v", s.SocketPath, err)
	}
	return &rpcSession{conn: conn, reader: bufio.NewReader(conn)}
}

func (c *rpcSession) Close() { c.conn.Close() }

// call performs one framed JSON-RPC request (wire format:
// internal/rpc/protocol.go "<length>\n<payload>" both ways).
func (c *rpcSession) call(method string, params any) (json.RawMessage, error) {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      strconv.FormatInt(time.Now().UnixNano(), 10),
		"method":  method,
	}
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
		return nil, fmt.Errorf("invalid frame length %q: %w", lengthLine, err)
	}
	respData := make([]byte, length)
	if _, err := io.ReadFull(c.reader, respData); err != nil {
		return nil, fmt.Errorf("read frame payload: %w", err)
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("decode rpc response: %w", err)
	}
	if resp.Error != nil {
		return nil, errors.New(resp.Error.Message)
	}
	return resp.Result, nil
}

// terminalCollector polls a turn.terminal subscription and records events.
type terminalCollector struct {
	sess    *rpcSession
	subID   string
	events  []terminalEvent
	stop    chan struct{}
	stopped chan struct{}
}

// newCollector subscribes BEFORE any submit (the CLI ordering contract) and
// starts the poll loop on ONE persistent connection.
func newCollector(t *testing.T, s *harness.Stack) *terminalCollector {
	t.Helper()
	sess := openRPCSession(t, s)
	raw, err := sess.call("bus.subscribe", map[string]any{
		"topics": []string{"turn.terminal"},
	})
	if err != nil {
		t.Fatalf("bus.subscribe: %v", err)
	}
	var sub struct {
		SubscriptionID string `json:"subscription_id"`
	}
	if err := json.Unmarshal(raw, &sub); err != nil || sub.SubscriptionID == "" {
		t.Fatalf("bus.subscribe ack unparseable: %s (%v)", raw, err)
	}
	c := &terminalCollector{
		sess:    sess,
		subID:   sub.SubscriptionID,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go c.loop()
	t.Cleanup(func() {
		close(c.stop)
		<-c.stopped
		_, _ = sess.call("bus.unsubscribe",
			map[string]string{"subscription_id": c.subID})
		sess.Close()
	})
	return c
}

func (c *terminalCollector) loop() {
	defer close(c.stopped)
	for {
		select {
		case <-c.stop:
			return
		case <-time.After(250 * time.Millisecond):
		}
		raw, err := c.sess.call("bus.poll", map[string]string{
			"subscription_id": c.subID,
		})
		if err != nil {
			continue
		}
		var resp struct {
			Events []struct {
				Topic   string          `json:"topic"`
				Payload json.RawMessage `json:"payload"`
			} `json:"events"`
		}
		if json.Unmarshal(raw, &resp) != nil {
			continue
		}
		for _, ev := range resp.Events {
			if ev.Topic != "turn.terminal" {
				continue
			}
			var payload terminalEvent
			if json.Unmarshal(ev.Payload, &payload) == nil {
				c.events = append(c.events, payload)
			}
		}
	}
}

// waitFinal returns the turn's final non-parked terminal event and requires
// status=completed (fail on timeout or any failed/timeout terminal).
func (c *terminalCollector) waitFinal(t *testing.T, s *harness.Stack, turnID string, timeout time.Duration) terminalEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, ev := range c.events {
			if ev.TurnID == turnID && ev.Status != "parked" {
				if ev.Status != "completed" {
					t.Fatalf("turn %s terminal status = %q (error %q, reply %q)",
						turnID, ev.Status, ev.Error, ev.Reply)
				}
				return ev
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("no non-parked turn.terminal for %s within %s; collected: %+v\ndaemon log tail:\n%s",
		turnID, timeout, c.events, s.Daemon.LogTail())
	return terminalEvent{}
}

// ---------------------------------------------------------------------------
// per-test stack bootstrap + shared assertions.
// ---------------------------------------------------------------------------

// extractionCandidate is the JSON array served for every "epistemic
// extractor" prompt (the ambient subscriber path AND the L1 distill job —
// they share the extraction template, so one scripted reply serves both;
// the l0_progress marker suppresses a second distill of the same window).
func extractionCandidate(marker string) string {
	return `[{"type":"claim","text":"user commits to the ` + marker + ` decision","source":"conversation","confidence":0.9,"premises":[],"category":"technical"}]`
}

// newStack boots the hermetic stack with BOTH memory-capture config gates on
// and every memory-path LLM prompt scripted deterministically. The roster is
// seeded by the harness (config/agents), so dispatched task turns get real
// specialist lanes.
func newStack(t *testing.T, extractionMarker string) *harness.Stack {
	t.Helper()
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"memory.episodic.enabled":                     true,
		"memory.l0_enabled":                           true,
		"memory.epistemic.ambient_extraction.enabled": true,
	}))
	s.RegisterProject(t, "e2e-project")

	// Script the memory-path completions. Scripts are evaluated in
	// registration order and the first matching predicate wins; the
	// segmentation predicate is registered first so an extraction prompt
	// that happens to mention segmenting still resolves to the extractor
	// reply (the extractor template never contains "conversation
	// segmenter", so the two are disjoint in practice).
	s.Fake.Script(
		harness.MessageContains("conversation segmenter"),
		harness.TextResponse(`[]`), // L2: zero scenes is a valid outcome
	)
	s.Fake.Script(
		harness.MessageContains("epistemic extractor"),
		harness.TextResponse(extractionCandidate(extractionMarker)),
	)
	return s
}

// episodicDBPath returns the daemon's real episodic claim DB path.
func episodicDBPath(s *harness.Stack) string {
	return filepath.Join(s.StateDir, "memory", "episodic", "episodic_memories.db")
}

// ---------------------------------------------------------------------------
// l0 file reading (plain JSONL scan — the frozen §2.1 schema).
// ---------------------------------------------------------------------------

type l0Line struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	Meta      map[string]any `json:"meta"`
}

// readL0LinesForSession reads the day file and returns the session's lines.
// A missing file is an empty slice (nothing recorded yet).
func readL0LinesForSession(t *testing.T, l0Dir, sessionID string) []l0Line {
	t.Helper()
	path := filepath.Join(l0Dir, memory.LocalDayKey(time.Now())+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	var out []l0Line
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec l0Line
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue // torn line: same tolerance the reader uses
		}
		if rec.SessionID == sessionID {
			out = append(out, rec)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return out
}

// countLinesByRole tallies l0 lines by role.
func countLinesByRole(lines []l0Line) map[string]int {
	out := map[string]int{}
	for _, l := range lines {
		out[l.Role]++
	}
	return out
}

// l0DirFor returns the daemon's L0 directory for the sandbox memory dir.
func l0DirFor(s *harness.Stack) string {
	return filepath.Join(s.StateDir, "memory", "l0")
}

// sessionMessageCount opens the daemon's real sessions.db read-only and
// returns the persisted message count for the session (the store side of
// the capture contract: persistExchange writes user + assistant there).
func sessionMessageCount(s *harness.Stack, sessionID string) (int, error) {
	db, err := sql.Open("sqlite",
		s.StateDir+"/sessions.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var count int
	err = db.QueryRow(
		`SELECT COUNT(*) FROM session_messages WHERE session_id = ?`, sessionID).Scan(&count)
	return count, err
}

// waitL0Exchange polls until the session's L0 lines contain at least one
// user line carrying marker and one assistant line, then returns them.
func waitL0Exchange(t *testing.T, l0Dir, sessionID, marker string, timeout time.Duration) []l0Line {
	t.Helper()
	var lines []l0Line
	harness.WaitFor(t, timeout, "the session's L0 exchange lines", func() bool {
		lines = readL0LinesForSession(t, l0Dir, sessionID)
		counts := countLinesByRole(lines)
		return counts["user"] >= 1 && counts["assistant"] >= 1
	})
	counts := countLinesByRole(lines)
	if counts["user"] != 1 || counts["assistant"] != 1 {
		t.Fatalf("l0 lines = %+v, want exactly 1 user + 1 assistant for the turn", counts)
	}
	if lines[0].Role != "user" || !strings.Contains(lines[0].Content, marker) {
		t.Fatalf("first l0 line = %+v, want the user message carrying %q", lines[0], marker)
	}
	if lines[1].Role != "assistant" || lines[1].Content == "" {
		t.Fatalf("second l0 line = %+v, want the non-empty assistant reply", lines[1])
	}
	return lines
}

// ---------------------------------------------------------------------------
// memory-turn-capture-01 (M): completed DIRECT chat turn -> L0 + claims
// ---------------------------------------------------------------------------

// TestTurnCapture01DirectChatTurnCapturesL0AndClaim pins the happy path: a
// chat turn pinned to the chat lane (direct mode — the reply comes from the
// chat loop, never a dispatched task) records the user+assistant L0 lines via
// the turn.terminal subscriber, and BOTH the subscriber's ambient extraction
// and the chained L1 distill job land status=auto claims.
func TestTurnCapture01DirectChatTurnCapturesL0AndClaim(t *testing.T) {
	const marker = "mtc01direct"
	s := newStack(t, marker)
	sessionID := s.CreateSession(t, "mtc01", s.ProjectDir)

	const chatReply = "mtc01 direct reply: noted the constraint."
	// Pin the classifier to the chat lane (direct mode: no task row; the
	// reply is the chat loop's own text).
	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat"}`)
	s.Fake.SetChatText(chatReply)
	s.Fake.SetPostToolText(chatReply)

	coll := newCollector(t, s)
	ack := s.SubmitChatHTTP(t, sessionID,
		"mtc01 direct message about the "+marker+" constraint")
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("ack missing turn_id: %+v", ack)
	}
	ev := coll.waitFinal(t, s, turnID, 120*time.Second)
	if ev.Reply == "" {
		t.Fatalf("completed terminal carries empty reply: %+v", ev)
	}

	// 1. L0: the subscriber appended the exchange (user then assistant).
	waitL0Exchange(t, l0DirFor(s), sessionID, marker, 30*time.Second)

	// 2. The subscriber's ambient extraction wrote its auto claim (the
	// turn-terminal seam's own output, independent of the queue).
	waitAutoClaim(t, episodicDBPath(s), marker, 60*time.Second)

	// 3. The chained L1 job ran over the same window and stored its own
	// auto claim (the scripted extractor reply serves both prompts, so the
	// two claims share the marker text): after the marker-advanced L1
	// there are TWO status=auto claims for the marker — one from the
	// subscriber's ambient extraction, one from the queue-side
	// memory.l1_extract DistillWorker run (the queue.db job's result
	// carries claims_stored >= 1).
	waitAutoClaimCount(t, episodicDBPath(s), marker, 2, 90*time.Second)
}

// ---------------------------------------------------------------------------
// memory-turn-capture-02 (M): DISPATCHED turn -> L0 + claims (regression proof)
// ---------------------------------------------------------------------------

// TestTurnCapture02DispatchedTurnCapturesL0AndClaim drives the case the
// turn-terminal subscriber was built for: multiagent.enabled=true (the
// harness default), the input classifies to the task lane (imperative +
// artifact noun -> code), the turn routes through the dispatcher into a task
// loop — a loop shape the retired session-end l0_record hook never received —
// and the turn.terminal subscriber is the ONLY seam that sees the completion.
//
// RESOLVED RELAY GAP (originally a tripwire): the task_completed_relay event
// (internal/agent/handler.go publishTaskTerminalRelay) carries
// SessionID == ConversationID == the session's CONVERSATION id (conv-…),
// while the message store keys messages by the PRIMARY session id. The
// subscriber resolves this via store.GetByConversationID when the direct id
// has no messages (internal/daemon/memory_capture.go capture()). This test
// asserts the positive outcome: the task's reply reaches L0 and the
// subscriber's ambient extraction lands a status=auto claim.
func TestTurnCapture02DispatchedTurnCapturesL0AndClaim(t *testing.T) {
	const marker = "mtc02dispatch"
	s := newStack(t, marker)
	sessionID := s.CreateSession(t, "mtc02", s.ProjectDir)

	// Script the dispatched task: planner emits a single step, the executor
	// writes the artifact, and the final reply carries the artifact path.
	artifact := filepath.Join(s.ProjectDir, "mtc02.txt")
	s.Fake.SetPlannerResponse(`{"steps":[{"description":"` + marker + `: create the file mtc02.txt containing mtc02","tool_hint":"file_write","depends_on":[]}]}`)
	s.Fake.ScriptN(1,
		harness.And(harness.IsExecutorRequest(),
			harness.Not(harness.IsPlannerRequest()),
			harness.MessageContains(marker)),
		harness.ToolCallResponse(harness.ToolCall{
			Name:      "file_write",
			Arguments: `{"path":"` + artifact + `","content":"mtc02","direct":true}`,
		}))
	s.Fake.SetPostToolText("Created mtc02.txt at " + artifact + " with the requested content.")

	// The imperative + artifact-noun message classifies code via the
	// harness heuristic -> dispatched async task lane (no classifier pin).
	coll := newCollector(t, s)
	ack := s.SubmitChatHTTP(t, sessionID,
		"Create a file named mtc02.txt containing mtc02. "+marker)
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("ack missing turn_id: %+v", ack)
	}
	ev := coll.waitFinal(t, s, turnID, 180*time.Second)
	if ev.TaskID == "" {
		t.Fatalf("dispatched turn terminal missing task_id: %+v", ev)
	}
	// The task really completed through a genuinely dispatched loop.
	row := harness.WaitTaskCompleted(t, s.TasksDBPath(), ev.TaskID, 120*time.Second)
	if row.CompletedJobs < 1 {
		t.Fatalf("task completed_jobs = %d, want >= 1: %+v", row.CompletedJobs, row)
	}

	// Give any (wrong or right) async capture a grace window.
	time.Sleep(3 * time.Second)

	// The session message store DID receive the exchange (the direct RPC
	// turn persisted user + the parked-ack text under the primary session
	// id) — the store side of the contract holds.
	count, err := sessionMessageCount(s, sessionID)
	if err != nil || count < 2 {
		t.Fatalf("session store messages = %d (err %v), want >= 2 (user + assistant)", count, err)
	}

	// POSITIVE: the subscriber resolved the relay's conversation-id key to
	// the primary session (GetByConversationID fallback) and captured the
	// exchange. The user side carries the original request (the marker);
	// the assistant side is the task-completion RELAY reply
	// ("task_completed_relay" handler case) — the subscriber dedupes the
	// two terminal events (same ConversationID) and records one exchange.
	waitL0Exchange(t, l0DirFor(s), sessionID, marker, 60*time.Second)

	// POSITIVE: the subscriber's ambient extraction landed a status=auto
	// claim for the marker (dispatched turns are captured like direct ones).
	waitAutoClaim(t, episodicDBPath(s), marker, 60*time.Second)
}

// ---------------------------------------------------------------------------
// memory-turn-capture-03 (S): FAILED turn -> zero L0 lines
// ---------------------------------------------------------------------------

// TestTurnCapture03FailedTurnRecordsNothing pins the status gate: a turn whose
// model call fails (raw HTTP 500 from the fake LLM — the only scripted
// provider shape) terminates failed, and the subscriber records NOTHING for
// the session (failed turns' replies are error text, not dialogue).
func TestTurnCapture03FailedTurnRecordsNothing(t *testing.T) {
	const marker = "mtc03failed"
	// short_retries=1 + base_throttle=1s: the direct loop's bounded 5xx
	// retry budget is one attempt with a 1s backoff step, so the turn
	// fails fast instead of burning 3 x exponential (30s base) steps.
	s := harness.Start(t,
		harness.WithConfigOverlay(map[string]any{
			"memory.episodic.enabled":                     true,
			"memory.l0_enabled":                           true,
			"memory.epistemic.ambient_extraction.enabled": true,
			"llm.failure_policy.short_retries":            1,
			"llm.failure_policy.base_throttle":            1000000000,
			// Turn off the classifier chain's alias rotation so a 500 on
			// the analyzer surfaces immediately (one chatWithFailover
			// attempt, no per-call retries), and disable dispatcher-level
			// LLM failure retries.
			"orchestrator.classifier_fail_fast": true,
		}))
	s.RegisterProject(t, "e2e-project")
	// Same deterministic memory-path scripts as newStack (the failing turn
	// must not reach them, but a leaked capture would).
	s.Fake.Script(
		harness.MessageContains("conversation segmenter"),
		harness.TextResponse(`[]`),
	)
	s.Fake.Script(
		harness.MessageContains("epistemic extractor"),
		harness.TextResponse(extractionCandidate(marker)),
	)
	sessionID := s.CreateSession(t, "mtc03", s.ProjectDir)

	const chatReply = "unused: the provider errors before any reply exists"
	// Chat lane so the failure happens in the direct loop.
	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat"}`)
	s.Fake.SetChatText(chatReply)
	s.Fake.SetPostToolText(chatReply)
	// The turn's OWN completion fails: requests that mention the marker
	// AND are not the dispatcher's intent-analysis prompt (the analyzer
	// sees the raw input too and must stay healthy, or routing itself
	// degrades into the task lane). After the client's bounded 5xx budget
	// the turn fails. The marker makes any leaked L0 line attributable.
	s.Fake.Script(
		harness.And(
			harness.MessageContains(marker),
			harness.Not(harness.SystemPromptContains("intent analysis assistant")),
		),
		harness.HTTPStatusResponse(500, `{"error":"provider unavailable"}`),
	)

	coll := newCollector(t, s)
	ack := s.SubmitChatHTTP(t, sessionID, "mtc03 failing turn "+marker)
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("ack missing turn_id: %+v", ack)
	}

	// Wait for the FAILED terminal.
	deadline := time.Now().Add(120 * time.Second)
	sawFailed := false
	for time.Now().Before(deadline) {
		for _, ev := range coll.events {
			if ev.TurnID == turnID && ev.Status != "parked" {
				if ev.Status != "failed" {
					t.Fatalf("turn terminal status = %q, want failed: %+v", ev.Status, ev)
				}
				sawFailed = true
			}
		}
		if sawFailed {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !sawFailed {
		t.Fatalf("no failed turn.terminal for %s within 2m; collected: %+v\ndaemon log tail:\n%s",
			turnID, coll.events, s.Daemon.LogTail())
	}

	// Grace window for any (wrong) async capture, then assert zero L0.
	time.Sleep(3 * time.Second)
	lines := readL0LinesForSession(t, l0DirFor(s), sessionID)
	if len(lines) != 0 {
		t.Fatalf("failed turn recorded %d L0 lines, want 0: %+v", len(lines), lines)
	}
	// And zero claims carrying the turn marker (ambient must not have run).
	for _, c := range queryClaims(t, episodicDBPath(s), marker) {
		if c.Status == string(memory.ClaimStatusAuto) {
			t.Fatalf("failed turn produced an auto claim: %+v", c)
		}
	}
}
