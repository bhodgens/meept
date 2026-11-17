//go:build e2e

// Suite execution-observability: the ROOT cross-branch hermetic e2e gate of
// the issue-#140 bot/employee execution-observability tree (plan
// docs/plans/20261117-bot-employee-execution-observability, leaf 00T). Scratch
// MEEPT_HOME daemons, a live WS collector, and the daemon's real bus/relay —
// the bus and the relay are NEVER mocked. The gate stays RED until branches
// 01-03 land; assertions are deliberately not weakened to make it pass.
//
// Scenarios (manifest execution-observability-01..04) and the contract each
// waits on:
//
//	01 (TestExecutionObservability)        C1/C5/F1/F2/F7: the frozen
//	    ExecutionResultEvent payload reaches a WS subscriber on BOTH
//	    bot.result and employee.result, classified agent_progress, with no
//	    kind key. Compile-RED on agent.ExecutionResultEvent /
//	    agent.TopicBotResult / agent.TopicEmployeeResult (branch 01); runtime
//	    RED on the missing bot.result subscription + typed decoder (branch
//	    01 leaf 02 / branch 04).
//	02 (TestExecutionObservability_ResultEvent)  F3/F4/F9: a real bot bus
//	    trigger round emits bot.result and a real agents.trigger round emits
//	    employee.result; both carry status + stop_reason and no kind key.
//	    RED until the emitter bus seams (branch 02) and the daemon wiring /
//	    bot trigger handler (branch 04 / branch 02 leaf 03) land.
//	03 (TestExecutionObservability_TurnState)  C2/F3/F4: a real round shows
//	    running then idle on agent.turn.state with stop_reason end_turn and
//	    no phase, classified agent_progress.
//	04 (TestExecutionObservability_BotInbox)  C4/F5/F6: send_agent_message
//	    accepts a BOT id (no "unknown recipient" refusal) and the message is
//	    drained and injected into that bot's next round's turn-start prompt.
//
// The frozen wire contract (master C1) is asserted here, not re-invented:
// two SEPARATE topics carrying the SAME agent.ExecutionResultEvent shape —
// agent_id, status, output?, tokens_used, error?, duration_ms, stop_reason?,
// trigger_ref?, session_id?, timestamp — with NO kind discriminator.
package executionobservability

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/agent"
)

const (
	botResultTopic      = "bot.result"
	employeeResultTopic = "employee.result"
	turnStateTopic      = "agent.turn.state"
)

// baseOverlay is the config shared by every scenario in this suite: WS relay
// on (the collector needs it), high-risk tool confirmation off (the chat turn
// that sends a message to a bot must not stall on a confirmation prompt).
func baseOverlay() map[string]any {
	return map[string]any{
		"transport.http.websocket":           true,
		"security.require_confirmation_high": false,
	}
}

func obsStack(t *testing.T) *harness.Stack {
	t.Helper()
	return harness.Start(t, harness.WithConfigOverlay(baseOverlay()))
}

// ---------------------------------------------------------------------------
// WS frame collector (inter-agent-coord harness shape; never mocks the relay)
// ---------------------------------------------------------------------------

// obsFrame is one decoded WS frame. The relay wraps payloads as
// {"type": ..., "data": {...flat payload + source_topic...}}.
type obsFrame struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// wsCollector pumps every frame from one unfiltered broadcast WS connection
// into a slice for post-hoc scanning. A dedicated client is required because
// harness.WSClient.RecvEvent Fatalf's on read errors, which is unsafe from a
// goroutine (ws-filter / inter-agent-coord precedent).
type wsCollector struct {
	mu     sync.Mutex
	frames []obsFrame
	conn   *websocket.Conn
}

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
		t.Fatalf("execution-observability: dial ws %s: %v", wsURL, err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "e2e done") })
	c := &wsCollector{conn: conn}
	_ = c.readWithin(10 * time.Second) // consume the welcome status frame
	go c.pump(window)
	return c
}

func (c *wsCollector) readWithin(d time.Duration) *obsFrame {
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
		return c.readWithin(d)
	}
	var f obsFrame
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

func (c *wsCollector) snapshot() []obsFrame {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]obsFrame, len(c.frames))
	copy(out, c.frames)
	return out
}

// waitFor polls the collected frames until pred matches, returning the FIRST
// match in arrival order. The bounded frame dump on timeout is the RED
// diagnostic surface.
func (c *wsCollector) waitFor(t *testing.T, what string, d time.Duration, pred func(obsFrame) bool) obsFrame {
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
	t.Fatalf("execution-observability: no %s within %s; frames seen:\n%s", what, d, c.dump())
	return obsFrame{}
}

func (c *wsCollector) dump() string {
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
	return sb.String()
}

// indexOf returns the arrival index of f (by wire identity) or -1.
func (c *wsCollector) indexOf(f obsFrame) int {
	for i, fr := range c.snapshot() {
		if fr.Type == f.Type && string(fr.Data) == string(f.Data) {
			return i
		}
	}
	return -1
}

func framePayload(f obsFrame) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(f.Data, &m)
	return m
}

func frameString(f obsFrame, key string) string {
	s, _ := framePayload(f)[key].(string)
	return s
}

func frameSourceTopic(f obsFrame) string { return frameString(f, "source_topic") }

// ---------------------------------------------------------------------------
// RPC + daemon restart helpers (real daemon, real bus)
// ---------------------------------------------------------------------------

// publishViaRPC publishes through the daemon's REAL bus.publish RPC bridge
// and fails when no subscriber received it (the WS relay's subscription must
// be live). This injects at the bus boundary — never mocks the bus/relay.
func publishViaRPC(t *testing.T, s *harness.Stack, topic string, payload map[string]any) {
	t.Helper()
	rpc := harness.DialRPC(t, s.SocketPath)
	result := rpc.CallResult("bus.publish", map[string]any{
		"topic":   topic,
		"payload": payload,
	})
	if delivered, _ := result["delivered"].(float64); delivered < 1 {
		t.Fatalf("execution-observability: bus.publish to %q delivered=%v; the WS relay subscription is not live (result: %v)",
			topic, result["delivered"], result)
	}
}

// publishTrigger publishes to a bot bus-trigger topic through the daemon's
// REAL bus.publish RPC bridge. Unlike WS-relayed topics, a 3-segment trigger
// topic is subscribed by the bot router, not the WS relay; delivered>=1 here
// is the precise signal that the bot trigger handler is live (branch 02 leaf
// 03). RED today: delivered=0 because no router is installed.
func publishTrigger(t *testing.T, s *harness.Stack, topic string, payload map[string]any) {
	t.Helper()
	rpc := harness.DialRPC(t, s.SocketPath)
	result := rpc.CallResult("bus.publish", map[string]any{
		"topic":   topic,
		"payload": payload,
	})
	if delivered, _ := result["delivered"].(float64); delivered < 1 {
		t.Fatalf("execution-observability: bus.publish to bot trigger topic %q delivered=%v; the bot trigger handler / router is not subscribed (branch 02 leaf 03 not landed): %v",
			topic, result["delivered"], result)
	}
}

func stopDaemonPID(t *testing.T, pid int, socketPath string) {
	t.Helper()
	if pid <= 0 {
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = proc.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { _, _ = proc.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = proc.Kill()
		<-done
	}
	_ = os.Remove(socketPath)
}

// bootDaemon starts a fresh scratch daemon against the SAME sandbox home,
// config, state dir and socket path. GoalLoops register only at boot, so an
// employee/bot hired via RPC needs this before agents.trigger / bus triggers
// reach its loop (goal-completion suite precedent).
func bootDaemon(t *testing.T, s *harness.Stack) {
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
	cmd.Dir = s.Work
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = s.Env()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}

	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
			ForceAttemptHTTP2: false,
		},
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		resp, err := client.Get(s.HTTPBaseURL() + "/health") //nolint:noctx // bounded poll in test harness
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatalf("re-booted daemon not healthy within 60s; log tail:\n%s", s.Daemon.LogTail())
		}
		if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
			t.Fatalf("re-booted daemon died during boot; log tail:\n%s", s.Daemon.LogTail())
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// restartDaemon stops the harness-booted daemon and re-boots one against the
// same sandbox. The returned func stops the re-booted daemon; the harness's
// own cleanup remains a safe no-op afterwards.
func restartDaemon(t *testing.T, s *harness.Stack) func() {
	t.Helper()
	oldPID := s.Daemon.Pid()
	if oldPID == 0 {
		t.Fatal("harness daemon pid not recorded; nothing to restart")
	}
	stopDaemonPID(t, oldPID, s.SocketPath)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(s.SocketPath); os.IsNotExist(err) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	bootDaemon(t, s)
	return func() {
		data, err := os.ReadFile(filepath.Join(s.StateDir, "meept.pid"))
		if err != nil {
			return
		}
		var pid int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err == nil {
			stopDaemonPID(t, pid, s.SocketPath)
		}
	}
}

// ---------------------------------------------------------------------------
// Round driving (real agents.create / agents.trigger / bus triggers + FakeLLM)
// ---------------------------------------------------------------------------

// hireEntity creates a tier-1 employee/bot definition through the REAL
// agents.create RPC. When triggerTopic is non-empty the definition carries a
// bus_event trigger whose prompt template expands the published marker, so a
// bus.publish to that topic drives a real bot round through the trigger
// handler (branch 02 leaf 03). A cron-disabled trigger is used otherwise.
func hireEntity(t *testing.T, s *harness.Stack, id, triggerTopic string) {
	t.Helper()
	triggers := []map[string]any{{"type": "cron", "schedule": "0 4 * * *", "enabled": false}}
	if triggerTopic != "" {
		triggers = []map[string]any{{
			"type":            "bus_event",
			"topic":           triggerTopic,
			"prompt_template": "Execute {{.marker}}",
			"enabled":         true,
		}}
	}
	created := harness.DialRPC(t, s.SocketPath).CallResult("agents.create", map[string]any{
		"id":       id,
		"name":     id,
		"prompt":   "You are " + id + ", an e2e execution-observability probe.",
		"enabled":  true,
		"triggers": triggers,
		"constitution": map[string]any{
			"purpose":       id,
			"role":          "e2e execution-observability employee",
			"charter":       "Answer calls briefly and report the round marker.",
			"autonomy_tier": "tier_1_reactive",
			"escalates_to":  []string{"user"},
			"constraints": map[string]any{
				"tools_allowed": []string{},
				"risk_ceiling":  "medium",
			},
			"amendment_policy": map[string]any{
				"self_propose_allowed": false,
				"requires_approval":    true,
				"frozen_fields":        []string{},
			},
			"version":     1,
			"authored_by": "user",
		},
	})
	if got, _ := created["id"].(string); got != id {
		t.Fatalf("agents.create id = %v, want %s (%v)", got, id, created)
	}
}

// scriptRound scripts the three FakeLLM turns of a tier-1 goal round:
// ASSESS (assessment-module system prompt) -> one candidate whose prompt
// carries marker; EXECUTE (RunOnce with that prompt) -> plain text; REFLECT
// ("# execution outcome") -> a healthy verdict. The classifier is pinned to
// chat so no work-intent classification derails the executor turn.
func scriptRound(s *harness.Stack, marker string) {
	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat for execution-observability"}`)
	execPrompt := marker + " execute now"
	s.Fake.ScriptOnce(harness.MessageContains("assessment module"), harness.TextResponse(
		`{"candidates":[{"title":"obs round","description":"observability round","prompt":"`+execPrompt+`"}]}`))
	s.Fake.ScriptN(3, harness.MessageContains(execPrompt), harness.TextResponse("done: "+marker))
	s.Fake.ScriptN(3, harness.MessageContains("# execution outcome"), harness.TextResponse(
		`{"health":"healthy","reasoning":"the execution satisfied the mandate"}`))
	s.Fake.SetPostToolText("round step complete")
	s.Fake.SetChatText("ok")
}

func triggerRound(t *testing.T, s *harness.Stack, id string) {
	t.Helper()
	res := harness.DialRPC(t, s.SocketPath).CallResult("agents.trigger", map[string]any{
		"id":      id,
		"payload": map[string]any{"source": "manual", "round": id},
	})
	if status, _ := res["status"].(string); status == "" {
		t.Fatalf("agents.trigger returned no status for %q: %v", id, res)
	}
}

func toolResultsJoined(f *harness.FakeLLM) string {
	var out []string
	for _, body := range f.Requests() {
		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok || msg["role"] != "tool" {
				continue
			}
			if c, _ := msg["content"].(string); c != "" {
				out = append(out, c)
			}
		}
	}
	return strings.Join(out, "\n")
}

func allRequestTexts(f *harness.FakeLLM) string {
	var sb strings.Builder
	for _, body := range f.Requests() {
		b, err := json.Marshal(body)
		if err == nil {
			sb.Write(b)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Wire-shape assertions (frozen C1 / C2 contract)
// ---------------------------------------------------------------------------

// assertResultFrame pins an agent_progress WS frame whose source_topic is a
// result topic: the frozen ExecutionResultEvent shape decodes, status and
// stop_reason are present, and there is NO kind discriminator.
func assertResultFrame(t *testing.T, f obsFrame, wantTopic, wantAgent string) {
	t.Helper()
	if f.Type != "agent_progress" {
		t.Fatalf("execution-observability: %s frame classified as type %q; the WS relay's typedPayloadDecoders entry for the result topic is missing, so the WSProgress marker never reached the wire (frame: %s)",
			wantTopic, f.Type, string(f.Data))
	}
	if got := frameSourceTopic(f); got != wantTopic {
		t.Fatalf("execution-observability: result frame source_topic = %q, want %q", got, wantTopic)
	}
	m := framePayload(f)
	if _, ok := m["kind"]; ok {
		t.Fatalf("execution-observability: %s payload must NOT contain a %q discriminator (topic names identify the tier): %s", wantTopic, "kind", string(f.Data))
	}
	for _, k := range []string{"agent_id", "status", "tokens_used", "duration_ms", "stop_reason", "timestamp"} {
		if _, ok := m[k]; !ok {
			t.Errorf("execution-observability: %s payload missing key %q: %s", wantTopic, k, string(f.Data))
		}
	}
	var ev agent.ExecutionResultEvent
	if err := json.Unmarshal(f.Data, &ev); err != nil {
		t.Fatalf("execution-observability: %s payload does not decode into agent.ExecutionResultEvent: %v (%s)", wantTopic, err, f.Data)
	}
	if ev.AgentID != wantAgent {
		t.Errorf("execution-observability: %s agent_id = %q, want %q", wantTopic, ev.AgentID, wantAgent)
	}
	if ev.Status != "completed" {
		t.Errorf("execution-observability: %s status = %q, want %q", wantTopic, ev.Status, "completed")
	}
	if ev.StopReason != agent.StopReasonEndTurn {
		t.Errorf("execution-observability: %s stop_reason = %q, want %q", wantTopic, ev.StopReason, agent.StopReasonEndTurn)
	}
}

func isResultTopic(topic string) func(obsFrame) bool {
	return func(f obsFrame) bool { return frameSourceTopic(f) == topic }
}

func isTurnState(topic, state, agentID string) func(obsFrame) bool {
	return func(f obsFrame) bool {
		return frameSourceTopic(f) == topic &&
			frameString(f, "state") == state &&
			frameString(f, "agent_id") == agentID
	}
}

// clonePayload returns a shallow copy so each published topic gets its own
// agent_id without mutating the shared base.
func clonePayload(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// Manifest contract (scenario 01 documents the four coverage claims)
// ---------------------------------------------------------------------------

func assertManifestDeclaresFourScenarios(t *testing.T) {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 3; i++ {
		root = filepath.Dir(root)
	}
	data, err := os.ReadFile(filepath.Join(root, "e2e", "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m struct {
		Suites []struct {
			Name      string   `json:"name"`
			Dir       string   `json:"dir"`
			Scenarios []string `json:"scenarios"`
		} `json:"suites"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	for _, suite := range m.Suites {
		if suite.Name == "execution-observability" {
			if suite.Dir != "e2e/suites/execution-observability" {
				t.Errorf("manifest suite dir = %q, want e2e/suites/execution-observability", suite.Dir)
			}
			if len(suite.Scenarios) != 4 {
				t.Errorf("manifest execution-observability declares %d scenarios, want 4", len(suite.Scenarios))
			}
			return
		}
	}
	t.Fatal("manifest does not declare the execution-observability suite")
}

// ---------------------------------------------------------------------------
// execution-observability-01 (T1, F1/F2/F7, contracts C1/C5): wire + relay
// ---------------------------------------------------------------------------

// TestExecutionObservability (T1, scenario 01) proves the frozen shared
// payload reaches a live WS subscriber on BOTH result topics, classified
// agent_progress, with no kind key. Also pins the two topic names and the
// manifest's four-scenario contract.
//
// RED today: agent.ExecutionResultEvent / TopicBotResult / TopicEmployeeResult
// are undefined (branch 01); once they land, bot.result is not in the WS
// subscription list and neither topic has a typed decoder, so bus.publish's
// delivered count / classification assertions fail until branches 01/02/04
// land.
func TestExecutionObservability(t *testing.T) {
	assertManifestDeclaresFourScenarios(t)

	// Compile-time contract pins: the two frozen typed topics and the shared
	// payload must exist with the exact names from master C1.
	if got := agent.TopicBotResult.Name; got != botResultTopic {
		t.Errorf("agent.TopicBotResult.Name = %q, want %q", got, botResultTopic)
	}
	if got := agent.TopicEmployeeResult.Name; got != employeeResultTopic {
		t.Errorf("agent.TopicEmployeeResult.Name = %q, want %q", got, employeeResultTopic)
	}

	s := obsStack(t)
	collector := collectWS(t, s, 60*time.Second)

	base := map[string]any{
		"agent_id":    "obs-contract",
		"status":      "completed",
		"output":      "contract probe ok",
		"tokens_used": 7,
		"duration_ms": 12,
		"stop_reason": string(agent.StopReasonEndTurn),
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}
	for _, topic := range []string{botResultTopic, employeeResultTopic} {
		p := clonePayload(base)
		p["agent_id"] = "obs-" + strings.ReplaceAll(topic, ".", "-")
		publishViaRPC(t, s, topic, p)
	}

	botFrame := collector.waitFor(t, "an agent_progress frame on bot.result (branch 01 subscription + decoder)", 30*time.Second,
		isResultTopic(botResultTopic))
	empFrame := collector.waitFor(t, "an agent_progress frame on employee.result (branch 01 subscription + decoder)", 30*time.Second,
		isResultTopic(employeeResultTopic))
	assertResultFrame(t, botFrame, botResultTopic, "obs-bot-result")
	assertResultFrame(t, empFrame, employeeResultTopic, "obs-employee-result")

	// The result topics are lifecycle progress, never chat messages (the
	// blank-bubble invariant). No frame from either topic may be relayed as
	// chat_message.
	for _, f := range collector.snapshot() {
		if f.Type == "chat_message" && (frameSourceTopic(f) == botResultTopic || frameSourceTopic(f) == employeeResultTopic) {
			t.Fatalf("execution-observability: a result topic was relayed as chat_message: %s", string(f.Data))
		}
	}
}

// ---------------------------------------------------------------------------
// execution-observability-02 (T2, F3/F4/F9, contracts C1/C2/C3): result events
// ---------------------------------------------------------------------------

// TestExecutionObservability_ResultEvent (T2, scenario 02) drives a REAL bot
// bus-trigger round and a REAL agents.trigger round, then asserts a bot.result
// frame and a separate employee.result frame each carry status + stop_reason
// and no kind key. RED until the emitters (branch 02), the daemon bus wiring
// (branch 04) and the bot trigger handler (branch 02 leaf 03) land.
func TestExecutionObservability_ResultEvent(t *testing.T) {
	s := obsStack(t)
	hireEntity(t, s, "obs-result-bot", "obs.result.trigger")
	hireEntity(t, s, "obs-result-emp", "")
	stop := restartDaemon(t, s)
	defer stop()
	collector := collectWS(t, s, 4*time.Minute)

	// Bot round: a real bus.publish on the bot's trigger topic reaches the
	// daemon's bot router, whose handler runs the round. The prompt template
	// expands {{.marker}}, so the round is individually identifiable.
	scriptRound(s, "OBS-BOT-RESULT")
	publishTrigger(t, s, "obs.result.trigger", map[string]any{"marker": "OBS-BOT-RESULT"})
	botFrame := collector.waitFor(t, "a bot.result frame from a real bot bus-trigger round", 2*time.Minute,
		isResultTopic(botResultTopic))
	assertResultFrame(t, botFrame, botResultTopic, "obs-result-bot")

	// Employee round: agents.trigger drives the tier-1 GoalLoop through
	// Assess -> Execute -> Reflect; Execute publishes employee.result.
	scriptRound(s, "OBS-EMP-RESULT")
	triggerRound(t, s, "obs-result-emp")
	empFrame := collector.waitFor(t, "an employee.result frame from a real agents.trigger round", 2*time.Minute,
		isResultTopic(employeeResultTopic))
	assertResultFrame(t, empFrame, employeeResultTopic, "obs-result-emp")
}

// ---------------------------------------------------------------------------
// execution-observability-03 (T3, F3/F4, contract C2): turn-state edges
// ---------------------------------------------------------------------------

// TestExecutionObservability_TurnState (T3, scenario 03) drives a real round
// and asserts the shared turn-state edges: running at entry then idle with
// stop_reason end_turn, no phase, classified agent_progress — the #139
// vocabulary reused by both tiers (master C2).
func TestExecutionObservability_TurnState(t *testing.T) {
	s := obsStack(t)
	hireEntity(t, s, "obs-turn-emp", "")
	stop := restartDaemon(t, s)
	defer stop()
	collector := collectWS(t, s, 4*time.Minute)

	scriptRound(s, "OBS-TURN-ROUND")
	triggerRound(t, s, "obs-turn-emp")

	running := collector.waitFor(t, "an agent.turn.state running edge for the round", 2*time.Minute,
		isTurnState(turnStateTopic, string(agent.TurnStateRunning), "obs-turn-emp"))
	idle := collector.waitFor(t, "an agent.turn.state idle edge for the round", 2*time.Minute,
		isTurnState(turnStateTopic, string(agent.TurnStateIdle), "obs-turn-emp"))

	var runningEv, idleEv agent.TurnStateEvent
	if err := json.Unmarshal(running.Data, &runningEv); err != nil {
		t.Fatalf("running TurnStateEvent decode: %v (%s)", err, running.Data)
	}
	if err := json.Unmarshal(idle.Data, &idleEv); err != nil {
		t.Fatalf("idle TurnStateEvent decode: %v (%s)", err, idle.Data)
	}
	if runningEv.State != agent.TurnStateRunning {
		t.Errorf("first edge state = %q, want %q", runningEv.State, agent.TurnStateRunning)
	}
	if idleEv.State != agent.TurnStateIdle {
		t.Errorf("settle edge state = %q, want %q", idleEv.State, agent.TurnStateIdle)
	}
	if idleEv.StopReason != agent.StopReasonEndTurn {
		t.Errorf("idle edge stop_reason = %q, want %q (C1: completed -> end_turn)", idleEv.StopReason, agent.StopReasonEndTurn)
	}
	if idleEv.Phase != "" {
		t.Errorf("idle edge must carry no phase (C2), got %q", idleEv.Phase)
	}
	if ri, ii := collector.indexOf(running), collector.indexOf(idle); ri >= ii {
		t.Errorf("running edge (index %d) did not precede idle edge (index %d)", ri, ii)
	}
	if running.Type != "agent_progress" || idle.Type != "agent_progress" {
		t.Errorf("turn-state edges classified as %q/%q, want agent_progress (never chat_message)", running.Type, idle.Type)
	}
}

// ---------------------------------------------------------------------------
// execution-observability-04 (T4, F5/F6, contract C4): bot inbox round-trip
// ---------------------------------------------------------------------------

// grantChatMessaging patches the seeded chat AGENT.md so the driver turn can
// call send_agent_message (inter-agent-coord inbox rig shape).
func grantChatMessaging(st *harness.Stack) error {
	chatSpec := filepath.Join(st.MeeptHome, "agents", "chat", "AGENT.md")
	data, err := os.ReadFile(chatSpec)
	if err != nil {
		return err
	}
	patched := strings.Replace(string(data),
		"additional_tools:\n",
		"additional_tools:\n  - send_agent_message\n  - inbox\n", 1)
	if patched == string(data) {
		return fmt.Errorf("chat AGENT.md has no additional_tools block to patch")
	}
	return os.WriteFile(chatSpec, []byte(patched), 0o644)
}

// TestExecutionObservability_BotInbox (T4, scenario 04) proves C4 end to end:
// send_agent_message accepts a BOT id (no "unknown recipient" refusal), and
// the queued message is drained and injected into that bot's NEXT round's
// turn-start prompt. RED until the roster (branch 03 T2) and drainer
// propagation (branch 03 T3) land.
func TestExecutionObservability_BotInbox(t *testing.T) {
	s := harness.Start(t,
		harness.WithConfigOverlay(baseOverlay()),
		harness.WithPreBootHook(grantChatMessaging),
	)
	hireEntity(t, s, "obs-inbox-bot", "obs.inbox.trigger")
	stop := restartDaemon(t, s)
	defer stop()
	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat for bot inbox"}`)

	s.RegisterProject(t, "obs-inbox")
	sessionID := s.CreateSession(t, "obs-inbox-session", s.ProjectDir)

	const injected = "OBS-INJECT-MARKER drained at next bot round"
	s.Fake.SetPostToolText("message sent; reporting the receipt verbatim.")
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "send_agent_message",
		Arguments: `{"to":"obs-inbox-bot","message":"` + injected + `"}`,
	})
	s.ChatTurn(t, sessionID,
		`Use send_agent_message to send obs-inbox-bot the message "`+injected+`". Report the raw tool result verbatim.`,
		3*time.Minute)

	// The bot id must be an addressable roster target today: an unknown
	// recipient refusal is the exact RED reason called out in the leaf.
	joined := toolResultsJoined(s.Fake)
	lower := strings.ToLower(joined)
	if strings.Contains(lower, "unknown recipient") || strings.Contains(lower, "unknown tool") {
		t.Fatalf("execution-observability: bot id was rejected by send_agent_message (branch 03 roster seam not wired):\n%s", joined)
	}

	// Drive the bot's next round via its real bus trigger. The turn-start
	// drain must inject the queued message into the executor prompt as a
	// "[message from ...]" section — exactly once, not as user discourse.
	scriptRound(s, "OBS-INJECT-EXEC")
	publishTrigger(t, s, "obs.inbox.trigger", map[string]any{"marker": "OBS-INJECT-EXEC"})
	harness.WaitFor(t, 2*time.Minute, "the queued message injected into the bot's next round turn-start prompt", func() bool {
		text := allRequestTexts(s.Fake)
		return strings.Contains(text, injected) && strings.Contains(text, "[message from")
	})
}
