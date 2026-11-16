//go:build e2e

// Suite inter-agent-coord: the ROOT e2e test gate of the inter-agent
// coordination tree (docs/plans/20261115-inter-agent-coordination, leaf
// 00T). Hermetic scratch-MEEPT_HOME daemons; asserts over the live WS
// relay, bus topics, ACP subprocess surface, and the send_agent_message /
// inbox tools — never mocks.
//
// Scenarios (manifest inter-agent-coord-01..05) and the contract symbol
// each waits on:
//
//	T1 (turn_state_test.go)  delegate child turn state observed over WS —
//	    waits on agent.TopicTurnState publications (leaf 02-01 emitter)
//	    AND the WS relay's typedPayloadDecoders entry for agent.turn.state
//	    (leaf 04-01) which turns the C1 TurnStateEvent.WSClass marker into
//	    a type:"agent_progress" frame. Decode uses agent.TurnStateEvent
//	    (landed, leaf 01-01).
//	T2 (turn_state_test.go)  delegate result carries stop_reason —
//	    decodes the agent.delegate.result WS frame into
//	    agent.DelegateResult and reads .StopReason (leaf 02-01). COMPILE
//	    RED until that field lands: the whole suite fails to build.
//	T3 (acp_progress_test.go) ACP session/update plan reaches WS — the
//	    stub's plan payload is marshaled from acp.AgentProgressUpdate
//	    (leaf 02-02, C5): COMPILE RED until the type lands; then RUNTIME
//	    RED until onNotice's plan branch forwards it to agent.progress
//	    stage=acp.
//	T4 (inbox_test.go)       structured inbox round-trip — decode uses
//	    employee.AgentInboxBody + the send_agent_message "structured" arg
//	    (leaf 03-01; present UNCOMMITTED in-tree at gate-writing time).
//	T5 (inbox_test.go)       structured taint scan block/allow — RUNTIME
//	    RED until leaf 03-02's structured branch names the offending
//	    field ("file_paths") in the block reason; clean structured
//	    handoffs must converge unchanged.
//
// Deny-pattern fixture: the T5 deny rides the REAL exfiltration_directive
// class of internal/security/agent_boundary.go (credential-bearing URL
// parameter "api_key=" + the known exfiltration endpoint webhook.site) —
// never an invented string.
package interagentcoord

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/agent"
)

// startDelegateStack boots a scratch daemon whose chat agent can drive the
// delegation tools and whose WS relay is live. Same grant seam as the
// delegate-seam suite (can_delegate:true + delegate controls in
// additional_tools, classifier pinned to chat); the only delta is
// transport.http.websocket:true so the suite can subscribe to the relay.
func startDelegateStack(t *testing.T) *harness.Stack {
	t.Helper()
	s := harness.Start(t,
		harness.WithConfigOverlay(map[string]any{
			"transport.http.websocket": true,
		}),
		harness.WithPreBootHook(func(st *harness.Stack) error {
			chatSpec := filepath.Join(st.MeeptHome, "agents", "chat", "AGENT.md")
			data, err := os.ReadFile(chatSpec)
			if err != nil {
				return err
			}
			patched := strings.Replace(string(data),
				"additional_tools:\n",
				"additional_tools:\n  - subagent_send_message\n  - subagent_interrupt\n  - subagent_list_children\n  - workflow\n", 1)
			patched = strings.Replace(patched, "can_delegate: false", "can_delegate: true", 1)
			if patched == string(data) {
				return errors.New("chat AGENT.md has no additional_tools block / can_delegate:false to patch")
			}
			return os.WriteFile(chatSpec, []byte(patched), 0o644)
		}),
	)
	// Pin the intent classifier to chat (delegate-seam rationale: an
	// unscripted intent ANALYZER fails its parse harmlessly and falls
	// through to this classification, keeping the turn on the chat loop).
	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat for inter-agent-coord"}`)
	return s
}

// scriptChild returns a text answer for a delegated coder child whose
// spawn brief carries goalMarker (delegate-seam pattern: the child settles
// with `reply` and never consumes the parent's scripted tool-call queue).
func scriptChild(s *harness.Stack, goalMarker, reply string) {
	s.Fake.Script(harness.And(
		harness.IsExecutorRequest(),
		harness.SystemPromptContains("Code Specialist"),
		harness.LastUserMessageContains(goalMarker),
	), harness.TextResponse(reply))
}

func delegateCall(agentID, message string) harness.ToolCall {
	return harness.ToolCall{Name: "delegate_task", Arguments: `{"agent_id":"` + agentID + `","message":"` + message + `"}`}
}

// ---------------------------------------------------------------------------
// WS frame collector
// ---------------------------------------------------------------------------

// coordFrame is one decoded WS frame. The relay wraps payloads as
// {"type": ..., "data": {...flat payload + source_topic...}}.
type coordFrame struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// wsCollector is one background WS connection pumping every frame into a
// slice for post-hoc scanning. A dedicated client (ws-filter precedent):
// harness.WSClient.RecvEvent Fatalf's on read errors, which is unsafe
// from a goroutine.
type wsCollector struct {
	mu     sync.Mutex
	frames []coordFrame
	conn   *websocket.Conn
}

// collectWS dials the daemon's /ws endpoint (no subscribe frame: an
// unfiltered connection stays in broadcast mode, ws-filter-04) and pumps
// frames for the given window.
func collectWS(t *testing.T, s *harness.Stack, window time.Duration) *wsCollector {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	wsURL := "wss" + strings.TrimPrefix(s.HTTPBaseURL(), "https") + "/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
			},
		},
	})
	if err != nil {
		t.Fatalf("inter-agent-coord: dial ws %s: %v", wsURL, err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "e2e done") })
	c := &wsCollector{conn: conn}
	_ = c.readWithin(10 * time.Second) // consume the welcome status frame
	go c.pump(window)
	return c
}

func (c *wsCollector) readWithin(d time.Duration) *coordFrame {
	if d <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	kind, data, err := c.conn.Read(ctx)
	if err != nil {
		return nil
	}
	if kind != websocket.MessageText {
		return c.readWithin(d) // ignore binary frames
	}
	var f coordFrame
	if err := json.Unmarshal(data, &f); err != nil {
		return nil
	}
	return &f
}

func (c *wsCollector) pump(window time.Duration) {
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		f := c.readWithin(time.Until(deadline))
		if f == nil {
			return
		}
		c.mu.Lock()
		c.frames = append(c.frames, *f)
		c.mu.Unlock()
	}
}

func (c *wsCollector) snapshot() []coordFrame {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]coordFrame, len(c.frames))
	copy(out, c.frames)
	return out
}

// waitFor polls the collected frames until pred matches, returning the
// FIRST matching frame in arrival order. Fails with a frame dump (bounded)
// naming what was waited on — the RED diagnostics surface.
func (c *wsCollector) waitFor(t *testing.T, what string, d time.Duration, pred func(coordFrame) bool) coordFrame {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		for _, ev := range c.snapshot() {
			if pred(ev) {
				return ev
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	var sb strings.Builder
	frames := c.snapshot()
	const maxDump = 40
	for i, ev := range frames {
		if i >= maxDump {
			sb.WriteString("... (")
			sb.WriteString(strconv.Itoa(len(frames) - maxDump))
			sb.WriteString(" more)\n")
			break
		}
		sb.WriteString(ev.Type + " " + string(ev.Data) + "\n")
	}
	t.Fatalf("inter-agent-coord: no %s within %s; frames seen:\n%s", what, d, sb.String())
	return coordFrame{}
}

func (c *wsCollector) indexOf(f coordFrame) int {
	for i, fr := range c.snapshot() {
		if bytes.Equal(fr.Data, f.Data) && fr.Type == f.Type {
			return i
		}
	}
	return -1
}

// framePayload decodes a frame's inner data map (nil-safe).
func framePayload(f coordFrame) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(f.Data, &m)
	return m
}

func frameString(f coordFrame, key string) string {
	m := framePayload(f)
	s, _ := m[key].(string)
	return s
}

func frameSourceTopic(f coordFrame) string {
	return frameString(f, "source_topic")
}

// ---------------------------------------------------------------------------
// inter-agent-coord-01: delegate child turn state observed end-to-end
// ---------------------------------------------------------------------------

// TestT1_DelegateTurnStateObservedOverWS (T1, F1, contracts C1/C6): a
// delegate child's turn state reaches a WS subscriber — TurnStateEvent
// state=running at spawn, then state=idle at settle, both on
// agent.turn.state, classified as agent_progress via the typed WSClass
// marker (leaf 04-01's typedPayloadDecoders entry).
//
// RED today: no publisher (leaf 02-01) -> waitFor times out with the
// frame dump; once 02-01 lands but before 04-01, the frames arrive in the
// generic "event" bucket and the classification assertion below names the
// gap.
func TestT1_DelegateTurnStateObservedOverWS(t *testing.T) {
	s := startDelegateStack(t)
	s.RegisterProject(t, "coord-turnstate")
	sessionID := s.CreateSession(t, "turnstate-test", s.ProjectDir)

	// One short-lived coder child, settled by its own script.
	scriptChild(s, "COORD-STATE-OK", "COORD-STATE-OK")
	collector := collectWS(t, s, 4*time.Minute)

	s.Fake.EnqueueToolCalls(delegateCall("coder", "Reply with exactly: COORD-STATE-OK"))
	s.Fake.SetPostToolText(`delegate_task returned a running child handle; reporting the raw result verbatim.`)
	reply := s.ChatTurn(t, sessionID,
		`Use the delegate_task tool to spawn one child with goal "Reply with exactly: COORD-STATE-OK" and report the raw tool result verbatim.`,
		4*time.Minute)
	lower := strings.ToLower(reply)
	if strings.Contains(lower, "unknown tool") || strings.Contains(lower, "tool not found") {
		t.Fatalf("delegate_task not registered: %s", reply)
	}

	isTurnState := func(state string) func(coordFrame) bool {
		return func(ev coordFrame) bool {
			return frameSourceTopic(ev) == "agent.turn.state" &&
				frameString(ev, "state") == state &&
				frameString(ev, "child_id") != ""
		}
	}
	running := collector.waitFor(t, "agent.turn.state running frame for the delegate child (leaf 02-01 publisher)", 2*time.Minute, isTurnState("running"))
	idle := collector.waitFor(t, "agent.turn.state idle frame for the delegate child (leaf 02-01 publisher)", 2*time.Minute, isTurnState("idle"))

	var runningEv, idleEv struct {
		agent.TurnStateEvent
		SourceTopic string `json:"source_topic"`
	}
	if err := json.Unmarshal(running.Data, &runningEv); err != nil {
		t.Fatalf("agent.turn.state running payload decode: %v (%s)", err, running.Data)
	}
	if err := json.Unmarshal(idle.Data, &idleEv); err != nil {
		t.Fatalf("agent.turn.state idle payload decode: %v (%s)", err, idle.Data)
	}

	if runningEv.SourceTopic != "agent.turn.state" || idleEv.SourceTopic != "agent.turn.state" {
		t.Fatalf("turn-state frames rode source_topic %q / %q, want agent.turn.state", runningEv.SourceTopic, idleEv.SourceTopic)
	}
	if runningEv.ChildID == "" || runningEv.ChildID != idleEv.ChildID {
		t.Fatalf("running/idle TurnStateEvents do not share the delegate child handle: running child_id=%q idle child_id=%q", runningEv.ChildID, idleEv.ChildID)
	}
	if runningEv.State != agent.TurnStateRunning {
		t.Fatalf("first turn-state frame state = %q, want %q", runningEv.State, agent.TurnStateRunning)
	}
	if idleEv.State != agent.TurnStateIdle {
		t.Fatalf("settle turn-state frame state = %q, want %q", idleEv.State, agent.TurnStateIdle)
	}
	if idleEv.StopReason != agent.StopReasonEndTurn {
		t.Fatalf("idle TurnStateEvent stop_reason = %q, want %q (C1: completed child settles idle+end_turn)", idleEv.StopReason, agent.StopReasonEndTurn)
	}
	if runningEv.AgentID == "" {
		t.Fatalf("TurnStateEvent carries no agent_id")
	}
	if ri, ii := collector.indexOf(running), collector.indexOf(idle); ri >= ii {
		t.Fatalf("running frame (index %d) did not precede the idle frame (index %d)", ri, ii)
	}

	// WS classification (F9, leaf 04-01): the C1 WSClass marker must make
	// the relay emit type "agent_progress", never the generic event bucket.
	if running.Type != "agent_progress" {
		t.Fatalf("agent.turn.state frame classified as type %q; the WS relay's typedPayloadDecoders entry for agent.turn.state (leaf 04-01) is missing, so the C1 WSClass marker never reached the wire", running.Type)
	}
	if idle.Type != "agent_progress" {
		t.Fatalf("agent.turn.state idle frame classified as type %q, want agent_progress", idle.Type)
	}
}

// ---------------------------------------------------------------------------
// inter-agent-coord-02: delegate result carries stop_reason
// ---------------------------------------------------------------------------

// TestT2_DelegateResultCarriesStopReason (T2, F2, contract C2): the
// agent.delegate.result settlement frame decodes into agent.DelegateResult
// with StopReason=end_turn for a completed child.
//
// COMPILE RED until leaf 02-01 adds the StopReason field (the suite does
// not build); after it lands, this pins the resultFromRecord mapping on
// the live wire: completed -> end_turn, present on the published payload.
func TestT2_DelegateResultCarriesStopReason(t *testing.T) {
	s := startDelegateStack(t)
	s.RegisterProject(t, "coord-stopreason")
	sessionID := s.CreateSession(t, "stopreason-test", s.ProjectDir)

	scriptChild(s, "COORD-STOP-OK", "COORD-STOP-OK")
	collector := collectWS(t, s, 4*time.Minute)

	s.Fake.EnqueueToolCalls(delegateCall("coder", "Reply with exactly: COORD-STOP-OK"))
	s.Fake.SetPostToolText(`delegate_task returned a running child handle; reporting the raw result verbatim.`)
	reply := s.ChatTurn(t, sessionID,
		`Use the delegate_task tool to spawn one child with goal "Reply with exactly: COORD-STOP-OK" and report the raw tool result verbatim.`,
		4*time.Minute)
	lower := strings.ToLower(reply)
	if strings.Contains(lower, "unknown tool") || strings.Contains(lower, "tool not found") {
		t.Fatalf("delegate_task not registered: %s", reply)
	}

	result := collector.waitFor(t, "agent.delegate.result settlement frame", 2*time.Minute,
		func(ev coordFrame) bool { return frameSourceTopic(ev) == "agent.delegate.result" })

	var res agent.DelegateResult
	if err := json.Unmarshal(result.Data, &res); err != nil {
		t.Fatalf("agent.delegate.result payload decode: %v (%s)", err, result.Data)
	}
	if res.ChildID == "" {
		t.Fatalf("settlement frame carries no child_id: %s", result.Data)
	}
	if res.Status != "completed" {
		t.Fatalf("settlement status = %q, want completed (child scripted to finish cleanly)", res.Status)
	}
	if res.StopReason != agent.StopReasonEndTurn {
		t.Fatalf("DelegateResult.StopReason = %q, want %q (C2 mapping: completed -> end_turn)", res.StopReason, agent.StopReasonEndTurn)
	}
}
