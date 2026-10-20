//go:build e2e

// Suite acp-wire: the ACP (Agent Client Protocol) wire surface — the
// disabled-by-default posture (no subprocess on call) and the full wire
// against a REAL stub agent subprocess: catalog load, handshake, send,
// response, plus the not-found/disabled error paths.
package acpwire

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// stubAgentBin builds the fake ACP agent from internal/acp testdata —
// the same subprocess the package's own tests use, exercised here from
// OUTSIDE the package over its exported surface.
func stubAgentBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "fakeagent")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/fakeagent")
	cmd.Dir = filepath.Join(repoRoot(), "internal", "acp")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fakeagent: %v\n%s", err, out)
	}
	return bin
}

// repoRoot resolves the module root relative to this file's location.
func repoRoot() string {
	_, thisFile, _, _ := runtime.Caller(0) //nolint:dogsled // identity probe
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}

// TestACPWire_DisabledByDefault covers acp-wire-01: with [acp] enabled
// unset (the default), GetOrCreate returns the disabled sentinel and —
// the core safety property — NO subprocess is spawned even when a
// command is configured in the catalog.
func TestACPWire_DisabledByDefault(t *testing.T) {
	// Build a stub agent we can PROVE never ran: its argv points at a
	// sentinel file that would be created on spawn.
	sentinel := filepath.Join(t.TempDir(), "spawned.marker")
	stub := filepath.Join(t.TempDir(), "noop-agent.sh")
	script := "#!/bin/sh\ntouch " + sentinel + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	catalog := &config.ACPAgentsConfig{Agents: []config.ACPAgentEntry{{
		ID:      "e2e-agent",
		Command: []string{stub},
		Enabled: true,
	}}}
	// ACPConfig zero value: Enabled=false — the default posture.
	mgr := acp.NewManager(config.ACPConfig{}, catalog)

	if mgr.Enabled() {
		t.Fatal("ACP must be disabled by default")
	}
	// The catalog snapshot still lists entries (surface introspection),
	// but GetOrCreate refuses before any lookup happens.

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := mgr.GetOrCreate(ctx, "e2e-agent", t.TempDir())
	if sess != nil {
		_ = sess.Close()
		t.Fatal("disabled manager returned a live session")
	}
	if !errors.Is(err, acp.ErrDisabled) {
		t.Fatalf("GetOrCreate error = %v, want ErrDisabled", err)
	}
	// The subprocess proof: the sentinel file was never created.
	if _, statErr := os.Stat(sentinel); !os.IsNotExist(statErr) {
		t.Fatalf("a subprocess WAS spawned while ACP is disabled (sentinel: %v)", statErr)
	}
	if live := mgr.LiveSessions(); len(live) != 0 {
		t.Fatalf("disabled manager reports live sessions: %+v", live)
	}
	// Stop/StopAll are no-ops while disabled (no panic, no error surface).
	if err := mgr.Stop("e2e-agent"); err != nil {
		t.Fatalf("disabled Stop errored: %v", err)
	}
	mgr.StopAll()
}

// TestACPWire_CatalogStubAgentHandshakeSendErrors covers acp-wire-02:
// with ACP enabled, the catalog loads, the stub agent handshakes to
// ready, send round-trips the response, and the not-found/disabled agent
// errors are distinct.
func TestACPWire_CatalogStubAgentHandshakeSendErrors(t *testing.T) {
	bin := stubAgentBin(t)
	catalog := &config.ACPAgentsConfig{Agents: []config.ACPAgentEntry{
		{ID: "echo-agent", Command: []string{bin, "-mode", "echo"}, Enabled: true},
		{ID: "off-agent", Command: []string{bin, "-mode", "echo"}, Enabled: false},
	}}
	mgr := acp.NewManager(config.ACPConfig{
		Enabled:        true,
		DialTimeout:    30,
		CallTimeout:    20,
		MaxAgents:      8,
		PermissionMode: "permissive",
	}, catalog)

	if !mgr.Enabled() {
		t.Fatal("manager must report enabled")
	}
	agents := mgr.Agents()
	if len(agents) != 2 {
		t.Fatalf("catalog snapshot = %d agents, want 2", len(agents))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	workdir := t.TempDir()

	// Distinct error paths first: unknown id vs disabled entry.
	if _, err := mgr.GetOrCreate(ctx, "no-such-agent", workdir); !errors.Is(err, acp.ErrAgentNotFound) {
		t.Fatalf("unknown agent = %v, want ErrAgentNotFound", err)
	}
	if _, err := mgr.GetOrCreate(ctx, "off-agent", workdir); !errors.Is(err, acp.ErrAgentDisabled) {
		t.Fatalf("disabled agent = %v, want ErrAgentDisabled", err)
	}
	if live := mgr.LiveSessions(); len(live) != 0 {
		t.Fatalf("failed lookups must not leave sessions: %+v", live)
	}

	// The happy wire: handshake → ready → send → response.
	sess, err := mgr.GetOrCreate(ctx, "echo-agent", workdir)
	if err != nil {
		t.Fatalf("GetOrCreate echo-agent: %v", err)
	}
	if sess.State() != acp.StateReady {
		t.Fatalf("post-handshake state = %v, want ready", sess.State())
	}
	sendCtx, sendCancel := context.WithTimeout(ctx, 20*time.Second)
	defer sendCancel()
	reply, err := sess.Send(sendCtx, "hello over the acp wire")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if reply != "hello over the acp wire" {
		t.Fatalf("echo reply = %q", reply)
	}

	// GetOrCreate returns the SAME live session (no second subprocess).
	again, err := mgr.GetOrCreate(ctx, "echo-agent", workdir)
	if err != nil {
		t.Fatalf("second GetOrCreate: %v", err)
	}
	if again != sess {
		t.Fatal("GetOrCreate minted a second session for a live agent")
	}
	if live := mgr.LiveSessions(); len(live) != 1 || live["echo-agent"] != acp.StateReady {
		t.Fatalf("LiveSessions = %+v, want echo-agent ready", live)
	}

	// Stop closes the session and drops it from the registry.
	if err := mgr.Stop("echo-agent"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if live := mgr.LiveSessions(); len(live) != 0 {
		t.Fatalf("sessions after Stop: %+v", live)
	}
	// StopAll idempotent.
	mgr.StopAll()
}

// stubDecisioner is the injected decision layer for the decision-mode
// scenario: scripted answer + confidence, recording the questions asked.
// It satisfies acp.Decisioner from OUTSIDE the package — the same shape
// *llm.DecisionClient satisfies (contract B: no adapter needed).
type stubDecisioner struct {
	available  bool
	answer     string
	confidence float64

	mu    sync.Mutex
	calls int
}

func (d *stubDecisioner) Available() bool { return d.available }

func (d *stubDecisioner) Ask(_ context.Context, _ llm.DecisionQuestion) (*llm.DecisionResult, error) {
	d.mu.Lock()
	d.calls++
	d.mu.Unlock()
	return &llm.DecisionResult{Answer: d.answer, Confidence: d.confidence}, nil
}

func (d *stubDecisioner) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// collectEvents drains a session's event stream until the given text shows
// up on a permission_decision event (or the deadline passes), returning
// everything it saw.
func collectEvents(t *testing.T, sess *acp.Session, want string, timeout time.Duration) []acp.SessionEvent {
	t.Helper()
	deadline := time.After(timeout)
	var seen []acp.SessionEvent
	for {
		select {
		case ev := <-sess.Events():
			seen = append(seen, ev)
			if ev.Kind == "permission_decision" && ev.Text == want {
				return seen
			}
		case <-deadline:
			t.Fatalf("no permission_decision %q event within %v (saw %+v)", want, timeout, seen)
			return seen
		}
	}
}

// TestACPWire_DecisionModeAllowDeny extends acp-wire-02's surface: the fake
// agent sends session/requestPermission during a prompt; the manager is in
// permission_mode "decision" with an injected stub Decisioner. An allow
// answer must select allow-once on the wire (the agent's prompt completes
// and echoes); a deny answer must cancel the request (the fake agent
// still completes its turn with a cancelled outcome — the contract is the
// reply frame, so we assert the permission_decision event + Send result).
func TestACPWire_DecisionModeAllowDeny(t *testing.T) {
	bin := stubAgentBin(t)

	run := func(t *testing.T, answer string, wantEvent string) {
		t.Helper()
		stub := &stubDecisioner{available: true, answer: answer, confidence: 1.0}
		catalog := &config.ACPAgentsConfig{Agents: []config.ACPAgentEntry{
			{ID: "perm-agent", Command: []string{bin, "-mode", "permission"}, Enabled: true},
		}}
		mgr := acp.NewManager(config.ACPConfig{
			Enabled:           true,
			DialTimeout:       30,
			CallTimeout:       20,
			MaxAgents:         4,
			PermissionMode:    "decision",
			DecisionThreshold: 0.5,
		}, catalog)
		mgr.SetDecisionClient(stub)
		t.Cleanup(mgr.StopAll)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		sess, err := mgr.GetOrCreate(ctx, "perm-agent", t.TempDir())
		if err != nil {
			t.Fatalf("GetOrCreate: %v", err)
		}

		// The fake agent fires requestPermission mid-prompt; the lane's
		// decision must be observable both on the event stream and via a
		// completed wire round trip.
		sendCtx, sendCancel := context.WithTimeout(ctx, 20*time.Second)
		defer sendCancel()
		type sendRes struct {
			text string
			err  error
		}
		ch := make(chan sendRes, 1)
		go func() {
			text, err := sess.Send(sendCtx, "decide")
			ch <- sendRes{text, err}
		}()
		seen := collectEvents(t, sess, wantEvent, 15*time.Second)
		res := <-ch
		if res.err != nil {
			t.Fatalf("Send: %v", res.err)
		}
		if stub.count() != 1 {
			t.Fatalf("decisioner asked %d times, want 1", stub.count())
		}
		// The permission_request event must still be on the stream (stays,
		// now carrying the decision text on this lane).
		var sawReq bool
		for _, ev := range seen {
			if ev.Kind == "permission_request" {
				sawReq = true
			}
		}
		if !sawReq {
			t.Fatalf("permission_request event missing: %+v", seen)
		}
	}

	t.Run("allow", func(t *testing.T) { run(t, "allow", "allow") })
	t.Run("deny", func(t *testing.T) { run(t, "deny", "deny") })
}

// relayNotifier is the exported PermissionNotifier seam for the human
// relay scenario: records publishes, always reports a listener (the test
// resolves directly via the registry).
type relayNotifier struct {
	mu        sync.Mutex
	requested int
	resolved  int
}

func (n *relayNotifier) NotifyPermissionRequested(_ acp.PendingPermission) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.requested++
	return true
}

func (n *relayNotifier) NotifyPermissionResolved(_ acp.PendingPermission, _, _ string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.resolved++
}

func (n *relayNotifier) counts() (requested, resolved int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.requested, n.resolved
}

// waitForPermissionEvent drains events until the given kind arrives
// (deadline fails the test) and returns the event text.
func waitForPermissionEvent(t *testing.T, sess *acp.Session, kind string, timeout time.Duration) string {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-sess.Events():
			if ev.Kind == kind {
				return ev.Text
			}
		case <-deadline:
			t.Fatalf("no %q event within %v", kind, timeout)
			return ""
		}
	}
}

// TestACPWire_HumanModeAllowDenyTimeout extends the decision-mode scenario
// shape across the subprocess path for permission_mode "human"
// (acp-human-permission-relay leaf 02): three prompts over one session —
// relay-answered allow proceeds, relay-answered deny cancels (the fake
// agent still completes its turn — the contract is the reply frame), and
// an unanswered request times out fail-closed after relay_timeout.
func TestACPWire_HumanModeAllowDenyTimeout(t *testing.T) {
	bin := stubAgentBin(t)
	rl := acp.NewRelay()
	notif := &relayNotifier{}
	catalog := &config.ACPAgentsConfig{Agents: []config.ACPAgentEntry{
		{ID: "perm-agent", Command: []string{bin, "-mode", "permission"}, Enabled: true},
	}}
	mgr := acp.NewManager(config.ACPConfig{
		Enabled:        true,
		DialTimeout:    30,
		CallTimeout:    20,
		MaxAgents:      4,
		PermissionMode: "human",
		RelayTimeout:   2, // seconds — the unanswered third prompt fails closed
	}, catalog)
	mgr.SetPermissionRelay(rl, notif)
	t.Cleanup(mgr.StopAll)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	sess, err := mgr.GetOrCreate(ctx, "perm-agent", t.TempDir())
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	// runPrompt sends one turn and waits for the relay round trip. answer
	// is the verb relayed from the harness ("" = never answer -> timeout).
	// Returns the permission_resolved event text.
	runPrompt := func(t *testing.T, text, answer string) string {
		t.Helper()
		sendCtx, sendCancel := context.WithTimeout(ctx, 30*time.Second)
		defer sendCancel()
		ch := make(chan error, 1)
		go func() {
			_, err := sess.Send(sendCtx, text)
			ch <- err
		}()
		// The lane surfaces the pending id BEFORE the reply frame lands,
		// so the harness answerer never races the agent's block.
		id := waitForPermissionEvent(t, sess, "permission_requested", 15*time.Second)
		if id == "" {
			t.Fatal("permission_requested event must carry the pending id")
		}
		if answer != "" {
			if err := rl.Resolve(id, answer); err != nil {
				t.Fatalf("Resolve(%q, %q): %v", id, answer, err)
			}
		}
		resolvedText := waitForPermissionEvent(t, sess, "permission_resolved", 20*time.Second)
		if err := <-ch; err != nil {
			t.Fatalf("Send(%q) after %q resolution: %v", text, resolvedText, err)
		}
		return resolvedText
	}

	t.Run("allow", func(t *testing.T) {
		if got := runPrompt(t, "grant", "allow"); got != "allow" {
			t.Fatalf("permission_resolved text = %q, want allow", got)
		}
	})
	t.Run("deny", func(t *testing.T) {
		if got := runPrompt(t, "block", "deny"); got != "deny" {
			t.Fatalf("permission_resolved text = %q, want deny", got)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		if got := runPrompt(t, "ignore", ""); got != "timeout" {
			t.Fatalf("permission_resolved text = %q, want timeout", got)
		}
	})

	if got := len(mgr.PendingPermissions()); got != 0 {
		t.Fatalf("PendingPermissions after round trips = %d, want 0", got)
	}
	nReq, nRes := notif.counts()
	if nReq != 3 || nRes != 3 {
		t.Fatalf("notifier calls = %d/%d, want 3/3", nReq, nRes)
	}
}

// TestACPWire_DecisionModeFailClosed pins the fail-closed posture end to
// end: decision mode
// WITHOUT a configured Decisioner (nil seam — the daemon never wired one)
// must fail CLOSED: the request is cancelled ("no_decisioner") yet the
// wire round trip still completes (a cancelled outcome is a valid reply).
func TestACPWire_DecisionModeFailClosed(t *testing.T) {
	bin := stubAgentBin(t)
	catalog := &config.ACPAgentsConfig{Agents: []config.ACPAgentEntry{
		{ID: "perm-agent", Command: []string{bin, "-mode", "permission"}, Enabled: true},
	}}
	mgr := acp.NewManager(config.ACPConfig{
		Enabled:           true,
		DialTimeout:       30,
		CallTimeout:       20,
		MaxAgents:         4,
		PermissionMode:    "decision",
		DecisionThreshold: 0.5,
	}, catalog)
	// No SetDecisionClient call — the fail-closed posture.
	t.Cleanup(mgr.StopAll)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	sess, err := mgr.GetOrCreate(ctx, "perm-agent", t.TempDir())
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	sendCtx, sendCancel := context.WithTimeout(ctx, 20*time.Second)
	defer sendCancel()
	ch := make(chan error, 1)
	go func() {
		_, err := sess.Send(sendCtx, "decide")
		ch <- err
	}()
	collectEvents(t, sess, "no_decisioner", 15*time.Second)
	if err := <-ch; err != nil {
		t.Fatalf("Send after cancelled permission: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Full-stack human relay surface (acp-human-permission-relay leaf 03,
// Contract C): a REAL scratch daemon booted with [acp] permission_mode
// "human" + the fakeagent stub, a live /ws client, and the HTTP resolve
// route. This is the mandated proof that bus → WS relay delivery works —
// hermetic RPC tests do NOT prove the WS frame (master.md's classic wiring
// defect note).
// ---------------------------------------------------------------------------

// acpStack boots the harness daemon with the human relay armed: ACP
// enabled, permission_mode human, a 5 s relay_timeout (so the
// unanswered leg fails closed fast), websocket on, and the fakeagent
// catalog seeded under the sandbox MEEPT_HOME (~/.meept/acp_agents.json5
// default path, redirected by MEEPT_HOME).
func acpStack(t *testing.T, bin string) *harness.Stack {
	t.Helper()
	return harness.Start(t,
		harness.WithConfigOverlay(map[string]any{
			"transport.http.websocket": true,
			// acp_agent classifies HIGH (external subprocess side
			// effects); with the default require_confirmation_high the
			// executor's confirmation gate denies the tool call before
			// the ACP lane ever runs. The scratch daemon turns the gate
			// off — THIS test's subject is the ACP human relay, which is
			// itself the confirmation layer.
			"security.require_confirmation_high": false,
			"acp": map[string]any{
				"enabled":         true,
				"permission_mode": "human",
				"relay_timeout":   5,
				"dial_timeout":    30,
				"call_timeout":    20,
				"max_agents":      2,
			},
		}),
		harness.WithPreBootHook(func(s *harness.Stack) error {
			catalog := `{"agents":[{"id":"perm-agent","command":["` + bin +
				`","-mode","permission"],"enabled":true}]}`
			if err := os.WriteFile(filepath.Join(s.MeeptHome, "acp_agents.json5"), []byte(catalog), 0o600); err != nil {
				return err
			}
			// Grant acp_agent to the sandbox chat roster (the harness
			// seeds config/agents into MEEPT_HOME BEFORE this hook runs,
			// so we patch the copy): turns pinned to intent=chat route to
			// the chat loop, whose scoped registry only serves granted
			// tools. Never touch the repo file.
			chatSpec := filepath.Join(s.MeeptHome, "agents", "chat", "AGENT.md")
			data, err := os.ReadFile(chatSpec)
			if err != nil {
				return err
			}
			patched := strings.Replace(string(data),
				"additional_tools:\n", "additional_tools:\n  - acp_agent\n", 1)
			if patched == string(data) {
				return errors.New("chat AGENT.md has no additional_tools block to extend")
			}
			return os.WriteFile(chatSpec, []byte(patched), 0o644)
		}),
	)
}

// httpInsecure mirrors the harness's own scratch-cert client.
func httpInsecure() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
		},
	}
}

// nextACPFrame drains WS frames on the one long-lived connection until one
// carries the wanted source_topic (acp.permission.requested /
// acp.permission.resolved), failing the test with a daemon log excerpt past
// the budget. Other agent_progress traffic during the chat turn is skipped,
// not needed later: the relay's ordering (requested → wait → resolved) plus
// the test's sequential legs mean a skipped frame is never a wanted frame.
func nextACPFrame(t *testing.T, s *harness.Stack, ws *harness.WSClient, wantSourceTopic string, budget time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		ev := ws.RecvEvent(time.Until(deadline))
		if ev == nil {
			break // recv deadline: give up below
		}
		var data map[string]any
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			continue
		}
		if got, _ := data["source_topic"].(string); got == wantSourceTopic {
			// Frame type pin (Contract C): the relay must classify the
			// acp.permission.* topics as agent_progress — never
			// chat_message (AGENTS.md blank-bubble invariant).
			if ev.Type != "agent_progress" {
				t.Fatalf("%s frame type = %q, want agent_progress", wantSourceTopic, ev.Type)
			}
			return data
		}
	}
	t.Fatalf("no %s WS frame within %s; daemon log tail:\n%s", wantSourceTopic, budget, s.Daemon.LogTail())
	return nil
}

func TestACPWire_FullStackHumanRelayWSAndHTTPResolve(t *testing.T) {
	bin := stubAgentBin(t)
	s := acpStack(t, bin)

	// Wiring proof #0: the single lowercase armed Info line landed (read
	// the FULL log file — LogTail is a 4 KB window and boot lines get
	// pushed out by later chatter).
	logBytes, logErr := os.ReadFile(s.Daemon.LogPath())
	if logErr != nil {
		t.Fatalf("read daemon log: %v", logErr)
	}
	if !strings.Contains(string(logBytes), "acp human permission relay armed") {
		t.Fatalf("relay not armed at boot; daemon log:\n%s", logBytes)
	}

	ws := harness.DialWS(t, s, "")

	// Pin the intent classifier to chat: un-pinned, imperative-ish
	// "launch/ask ..." inputs classify as code, route through the planner,
	// and reach the CODER loop (which lacks the acp_agent grant — the turn
	// then dies with "Unknown tool requested"). The chat route runs the
	// chat loop, whose sandbox roster we extended with acp_agent above.
	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat for acp relay surface"}`)

	s.RegisterProject(t, "acp-relay-project")
	sessionID := s.CreateSession(t, "acp-relay", s.ProjectDir)
	client := httpInsecure()

	// Turn 1: acp_agent launch (spawns the fakeagent subprocess inside the
	// daemon).
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "acp_agent",
		Arguments: `{"agent":"perm-agent","verb":"launch"}`,
	})
	s.ChatTurn(t, sessionID, "launch the perm-agent external coding agent", 120*time.Second)

	// runSendTurn fires one acp_agent send turn in a goroutine (the tool
	// blocks on the human relay) and returns its (out, err) channel.
	runSendTurn := func(text string) <-chan string {
		done := make(chan string, 1)
		go func() {
			s.Fake.EnqueueToolCalls(harness.ToolCall{
				Name:      "acp_agent",
				Arguments: `{"agent":"perm-agent","verb":"send","message":"` + text + `"}`,
			})
			out, errOut := s.RunCLI(t, 150*time.Second, true,
				"chat", "--session", sessionID, "ask perm-agent to do: "+text)
			done <- out + "||" + errOut
		}()
		return done
	}

	// resolve posts the human verdict through the HTTP surface.
	resolve := func(id, decision string) map[string]any {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"id": id, "decision": decision})
		resp, err := client.Post(s.HTTPBaseURL()+"/api/v1/acp/permissions/resolve",
			"application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST resolve: %v", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST resolve status %d: %s", resp.StatusCode, raw)
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("POST resolve decode %s: %v", raw, err)
		}
		return out
	}

	// listPending fetches GET /api/v1/acp/permissions.
	listPending := func() []any {
		t.Helper()
		resp, err := client.Get(s.HTTPBaseURL() + "/api/v1/acp/permissions")
		if err != nil {
			t.Fatalf("GET permissions: %v", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET permissions status %d: %s", resp.StatusCode, raw)
		}
		var out struct {
			Enabled bool  `json:"enabled"`
			Pending []any `json:"pending"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("GET permissions decode: %v (%s)", err, raw)
		}
		if !out.Enabled {
			t.Fatalf("GET permissions reports enabled=false while a relay request is pending: %s", raw)
		}
		return out.Pending
	}

	// --- LEG 1: allow. WS requested frame -> pending shows via HTTP ->
	// POST resolve allow -> WS resolved frame (decision allow, reason
	// answered).
	turnDone := runSendTurn("grant")
	reqFrame := nextACPFrame(t, s, ws, "acp.permission.requested", 90*time.Second)
	pendID, _ := reqFrame["id"].(string)
	if pendID == "" {
		t.Fatalf("requested frame missing id: %v", reqFrame)
	}
	if got, _ := reqFrame["agent_id"].(string); got != "perm-agent" {
		t.Errorf("requested frame agent_id = %q, want perm-agent", got)
	}
	if _, ok := reqFrame["created_at"]; !ok {
		t.Errorf("requested frame missing created_at: %v", reqFrame)
	}

	pending := listPending()
	if len(pending) != 1 {
		t.Fatalf("HTTP pending snapshot = %v, want exactly 1 row while the relay waits", pending)
	}

	echo := resolve(pendID, "allow")
	if echo["decision"] != "allow" || echo["reason"] != "answered" {
		t.Errorf("resolve echo = %v, want decision allow reason answered", echo)
	}

	resFrame := nextACPFrame(t, s, ws, "acp.permission.resolved", 30*time.Second)
	if resFrame["id"] != pendID || resFrame["decision"] != "allow" {
		t.Fatalf("resolved frame = %v, want id %s decision allow", resFrame, pendID)
	}
	if out := <-turnDone; strings.Contains(out, "||failed") {
		t.Logf("allow-turn cli output (non-fatal): %s", out)
	}
	// The turn completed: the pending registry drained.
	if pending := listPending(); len(pending) != 0 {
		// enabled=false now legitimately short-circuits? No — relay stays
		// armed; the list must drain to empty after resolution.
		t.Fatalf("pending after allow resolve = %v, want empty", pending)
	}

	// --- LEG 2: deny over the RPC surface (unix socket), asserted via the
	// WS resolved frame carrying deny.
	turnDone = runSendTurn("block")
	reqFrame = nextACPFrame(t, s, ws, "acp.permission.requested", 60*time.Second)
	denyID, _ := reqFrame["id"].(string)
	if denyID == "" {
		t.Fatalf("deny-leg requested frame missing id: %v", reqFrame)
	}
	rpc := harness.DialRPC(t, s.SocketPath)
	res := rpc.CallResult("acp.permissions.resolve", map[string]any{"id": denyID, "decision": "deny"})
	if res["decision"] != "deny" {
		t.Errorf("rpc resolve echo = %v, want deny", res)
	}
	resFrame = nextACPFrame(t, s, ws, "acp.permission.resolved", 30*time.Second)
	if resFrame["id"] != denyID || resFrame["decision"] != "deny" {
		t.Fatalf("deny resolved frame = %v, want id %s decision deny", resFrame, denyID)
	}
	<-turnDone

	// --- LEG 3: unanswered -> relay_timeout (5 s) fail-closed: WS resolved
	// frame with decision deny, reason timeout.
	turnDone = runSendTurn("ignore")
	reqFrame = nextACPFrame(t, s, ws, "acp.permission.requested", 60*time.Second)
	timeoutID, _ := reqFrame["id"].(string)
	resFrame = nextACPFrame(t, s, ws, "acp.permission.resolved", 30*time.Second)
	if resFrame["id"] != timeoutID {
		t.Fatalf("timeout resolved frame id = %v, want %s", resFrame["id"], timeoutID)
	}
	if resFrame["decision"] != "deny" || resFrame["reason"] != "timeout" {
		t.Fatalf("timeout resolved frame = %v, want decision deny reason timeout", resFrame)
	}
	<-turnDone

	// --- CLI surface (repo wiring rule): `meept acp permissions list`
	// answers against the live daemon (empty now), and the bad-decision
	// typo is refused BEFORE any daemon call.
	out, _ := s.RunCLI(t, 30*time.Second, false, "acp", "permissions", "list", "--json")
	if !strings.Contains(out, "\"pending\"") {
		t.Fatalf("acp permissions list --json missing pending key:\n%s", out)
	}
	_, errOut := s.RunCLI(t, 30*time.Second, true, "acp", "permissions", "resolve", "perm-x", "maybe")
	if !strings.Contains(strings.ToLower(errOut), "allow") || !strings.Contains(strings.ToLower(errOut), "deny") {
		t.Fatalf("resolve with a typo decision must fail offline with the allow|deny hint, got:\n%s", errOut)
	}
}
