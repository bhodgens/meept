//go:build e2e

// Package acppersonaroute is the hermetic e2e suite for the acp-employees
// chain at the DAEMON level (manifest scenarios acp-persona-route-01..03):
// an agent definition with executor: acp + a persona, a catalog entry in
// acp_agents.json5 pointing at the fakeagent stub built from
// internal/acp/testdata, and a chat message that addresses the agent BY
// NAME. The end-to-end assertions prove the full chain —
//
//	name match (named_agent_route) -> RouteToAgent -> per-agent session
//	loop -> runTurnProducer executor branch -> GetOrCreatePersona ->
//	session/new (mcpServers fix) -> session/prompt -> shared
//	post-processing -> the user-facing reply is the HARNESS's text
//
// — and not the old defect shape (a local-LLM canned reply or a planner
// task ack answering a "have bob ..." turn).
package acppersonaroute

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// stubAgentBin builds the fake ACP agent from internal/acp testdata — the
// same subprocess acp-wire uses — with the new "prompt" mode: it answers
// session/prompt with "acp-fake reply to: <prompt>" (a deterministic text
// that cannot be confused with the bare echo or any canned local-LLM
// reply) and emits a tool_call-ish session/update first. It accepts the
// mcpServers field on session/new (the fakeagent is lenient, but the
// daemon only sends session/new once the mcpServers fix landed — commit
// c872ac00 — so a successful handshake IS that fix's proof).
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

// bobAgentDef is the agent definition under test: a custom roster agent
// named bob whose turns route through the external ACP harness (persona
// "bob" on the "acp-e2e" catalog entry). Written into the SANDBOX
// MEEPT_HOME user tier (~/.meept/agents/bob/AGENT.md — the harness
// redirects HOME so os.UserHomeDir() IS the sandbox home, the same tier
// acp-wire patches for the chat spec). The harness seeds the repo roster
// into the same dir before this hook runs, so bob lands as a new spec.
const bobAgentDef = `---
id: bob
name: bob
role: executor
description: acp-persona-route e2e fixture — a slaved external ACP employee
executor: acp
acp:
  agent: acp-e2e
  persona: bob
  permission_mode: permissive
enabled: true
---

# bob

E2E fixture agent. Every turn is forwarded verbatim to the ACP harness.
`

// personaStack boots the scratch daemon with: [acp] enabled + permissive
// (no relay: prompt replies never block), the fakeagent catalog entry,
// bob's agent definition, and the reply lane pinned so the FAKE LOCAL LLM
// texts are distinguishable from the harness reply.
func personaStack(t *testing.T, bin string) *harness.Stack {
	t.Helper()
	s := harness.Start(t,
		harness.WithConfigOverlay(map[string]any{
			// request_handoff / tool work runs through the executor's
			// confirmation gate; the acp-wire suite established the same
			// off-gate for this scratch stack (the subject here is the
			// named-route-to-ACP chain, not the confirmation layer).
			"security.require_confirmation_high": false,
			"acp": map[string]any{
				"enabled":         true,
				"permission_mode": "permissive",
				"dial_timeout":    30,
				"call_timeout":    60,
				"max_agents":      4,
			},
		}),
		harness.WithPreBootHook(func(st *harness.Stack) error {
			// The catalog is JSON-valid JSON5 at the default
			// $MEEPT_HOME/acp_agents.json5 path.
			catalog := fmt.Sprintf(
				`{"agents":[{"id":"acp-e2e","command":[%q,"-mode","prompt"],"enabled":true}]}`, bin)
			if err := os.WriteFile(filepath.Join(st.MeeptHome, "acp_agents.json5"),
				[]byte(catalog), 0o600); err != nil {
				return err
			}
			bobDir := filepath.Join(st.MeeptHome, "agents", "bob")
			if err := os.MkdirAll(bobDir, 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(bobDir, "AGENT.md"),
				[]byte(bobAgentDef), 0o644)
		}),
	)

	// Pin every fallback lane's fake-LLM text to a string that CANNOT be
	// the harness reply. If the named route is broken (the 2026-10-23
	// defect shape), the turn answers with one of these (or a task ack)
	// and the assertions below fire.
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned code"}`)
	s.Fake.SetChatText("LOCAL-LLM-CANNED-REPLY — the fake local model answered; the named ACP route did NOT fire")
	s.Fake.SetPostToolText("LOCAL-LLM-CANNED-REPLY — the fake local model answered; the named ACP route did NOT fire")

	// Wiring proof at boot: the daemon logged the executor injection.
	logBytes, err := os.ReadFile(s.Daemon.LogPath())
	if err != nil {
		t.Fatalf("read daemon log: %v", err)
	}
	if !strings.Contains(string(logBytes), "acp executor wired into the agent registry") {
		t.Fatalf("acp executor not wired into the registry at boot; daemon log:\n%s", logBytes)
	}
	return s
}

// waitForACPLive polls the acp.list RPC surface until the daemon reports
// the ACP subsystem enabled with the fakeagent catalog entry present.
// NOTE: handleList keys running-state lookup by the exact catalog id, so
// persona-lane sessions (composite label "acp-e2e/bob") do not surface
// there — the harness reply text in the route test IS the persona-session
// proof (only GetOrCreatePersona spawns the fakeagent).
func waitForACPLive(t *testing.T, s *harness.Stack, wantCatalogID string, timeout time.Duration) {
	t.Helper()
	rpc := harness.DialRPC(t, s.SocketPath)
	deadline := time.Now().Add(timeout)
	var seen []any
	for time.Now().Before(deadline) {
		res := rpc.CallResult("acp.list", map[string]any{})
		if enabled, _ := res["enabled"].(bool); !enabled {
			t.Fatalf("acp.list reports enabled=false while [acp] is configured on: %v", res)
		}
		seen, _ = res["agents"].([]any)
		for _, a := range seen {
			row, _ := a.(map[string]any)
			if id, _ := row["id"].(string); id == wantCatalogID {
				return
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("acp.list never reported catalog entry %q within %v (agents: %v)\ndaemon log tail:\n%s",
		wantCatalogID, timeout, seen, s.Daemon.LogTail())
}

// ---------------------------------------------------------------------------
// acp-persona-route-01: named-agent chat turn routes through the external
// ACP harness — the full chain, asserted on the user-facing reply.
// ---------------------------------------------------------------------------

func TestNamedRouteTurnRepliesFromExternalHarness(t *testing.T) {
	bin := stubAgentBin(t)
	s := personaStack(t, bin)

	s.RegisterProject(t, "acp-persona-route")
	sessionID := s.CreateSession(t, "acp-route", s.ProjectDir)

	// The classifier is pinned to CODE: the named route must happen
	// BEFORE any classifier call (the deterministic matcher runs ahead of
	// the whole chain). A turn that reaches the LLM would dispatch the
	// code lane as a planner task — exactly the 2026-10-23 defect shape —
	// and produce a task ack / canned reply instead of the harness text.
	reply := s.ChatTurn(t, sessionID, "have bob say hello from the acp harness", 150*time.Second)

	// 1. The reply IS the fake harness's deterministic text. Proves:
	//    name match -> RouteToAgent -> per-agent session loop ->
	//    executor branch -> session/new + session/prompt -> shared
	//    post-processing (reply guard passed the prose through).
	if !strings.Contains(reply, "acp-fake reply to:") {
		t.Fatalf("acp-persona-route-01: reply is not the external harness text: %q\ndaemon log tail:\n%s",
			reply, s.Daemon.LogTail())
	}

	// 2. NOT the old defect shape: no planner task was created for a
	//    named-chat turn (RouteToAgent runs the loop inline).
	if tasks := harness.Tasks(t, s.TasksDBPath()); len(tasks) != 0 {
		t.Fatalf("acp-persona-route-01: named route dispatched async planner work; task rows: %+v", tasks)
	}

	// 3. NOT the local LLM: the canned pinned chat text never surfaced.
	if strings.Contains(reply, "LOCAL-LLM-CANNED-REPLY") ||
		(strings.Contains(reply, "Task ") && strings.Contains(reply, "completed") && len(reply) < 60) {
		t.Fatalf("acp-persona-route-01: reply is the canned local-model / task-ack shape, not the harness: %q", reply)
	}

	// 4. The ACP subsystem + catalog entry are live on the RPC surface
	//    (wiring proof; the persona session itself is proven by the
	//    harness reply text above — acp.list keys running state by the
	//    bare catalog id, so a persona-lane session shows as the entry).
	waitForACPLive(t, s, "acp-e2e", 15*time.Second)

	// 5. The prompt text rode session/prompt intact (the harness echoes
	//    what it received).
	if !strings.Contains(reply, "say hello from the acp harness") {
		t.Errorf("acp-persona-route-01: harness reply missing the echoed prompt text: %q", reply)
	}
}

// ---------------------------------------------------------------------------
// acp-persona-route-02: an addressed name that is NOT on the roster falls
// through to the normal classification chain (registry-gated matcher).
// ---------------------------------------------------------------------------

func TestUnknownNameFallsThroughToClassification(t *testing.T) {
	bin := stubAgentBin(t)
	s := personaStack(t, bin)

	s.RegisterProject(t, "acp-persona-route")
	sessionID := s.CreateSession(t, "acp-route-fallthrough", s.ProjectDir)

	// "unknownbot" matches the have-frame slot, but it resolves against
	// no registry id/name -> the matcher misses -> normal chain. With the
	// classifier pinned to chat, the normal chain answers with the fake
	// LLM's canned chat text.
	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat"}`)
	reply := s.ChatTurn(t, sessionID, "have unknownbot say hello", 150*time.Second)

	if !strings.Contains(reply, "LOCAL-LLM-CANNED-REPLY") {
		t.Fatalf("acp-persona-route-02: unknown name hijacked the turn — reply is not the fall-through chat text: %q\ndaemon log tail:\n%s",
			reply, s.Daemon.LogTail())
	}
	// The ACP executor never entered: no harness text anywhere.
	if strings.Contains(reply, "acp-fake reply to:") {
		t.Fatalf("acp-persona-route-02: a non-roster name routed through the external harness: %q", reply)
	}
	// Fall-through chat is inline: no planner task.
	if tasks := harness.Tasks(t, s.TasksDBPath()); len(tasks) != 0 {
		t.Fatalf("acp-persona-route-02: fall-through chat created task rows: %+v", tasks)
	}
}

// ---------------------------------------------------------------------------
// acp-persona-route-03: request_handoff to an unknown target fails with the
// roster names listed (task-less by design), and a named executor: acp
// agent is a valid handoff target that resolves through the registry.
// ---------------------------------------------------------------------------

// handoffLinePresent scans the daemon log for a line that contains every
// needle (slog may escape quotes inside error strings differently per
// handler, so assertions key on quote-free fragments, line by line).
func handoffLinePresent(t *testing.T, s *harness.Stack, needles ...string) bool {
	t.Helper()
	data, err := os.ReadFile(s.Daemon.LogPath())
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		all := true
		for _, n := range needles {
			if !strings.Contains(line, n) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// fakeRequestMessageTexts extracts every message content string (any
// role) from the fake LLM's recorded chat/completions requests — the
// tool-result envelopes ride these turns when the loop continued after a
// tool call.
func fakeRequestMessageTexts(f *harness.FakeLLM) []string {
	var out []string
	for _, body := range f.Requests() {
		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			mm, _ := m.(map[string]any)
			if content, _ := mm["content"].(string); content != "" {
				out = append(out, content)
			}
		}
	}
	return out
}

// findHandoffRosterError scans the turn reply first (request_handoff is a
// TerminatingTool: the executor lane ends the turn and ships the tool
// result JSON verbatim, so the unknown-target error rides the USER-facing
// reply), then the recorded model request history. Returns the text
// carrying the roster-listed error, or "" when nothing matched.
func findHandoffRosterError(f *harness.FakeLLM, reply string) string {
	candidates := append([]string{reply}, fakeRequestMessageTexts(f)...)
	for _, c := range candidates {
		if strings.Contains(c, "not found. Available agents:") ||
			strings.Contains(c, "is not a known agent; available agents:") {
			return c
		}
	}
	return ""
}

func TestHandoffUnknownTargetListsRoster(t *testing.T) {
	bin := stubAgentBin(t)
	s := personaStack(t, bin)
	s.RegisterProject(t, "acp-persona-route")
	sessionID := s.CreateSession(t, "acp-route-handoff", s.ProjectDir)

	// A real task row for the handoff envelope (created over the RPC
	// surface, NOT through the planner, so no task lane can answer the
	// turns below).
	rpc := harness.DialRPC(t, s.SocketPath)
	created := rpc.CallResult("task.create", map[string]any{
		"name":        "acp-persona-route-handoff-task",
		"description": "fixture task for the request_handoff unknown-target scenario",
	})
	taskID, _ := created["id"].(string)
	if taskID == "" {
		t.Fatalf("task.create returned no id: %v", created)
	}

	// Turn 1 — the DEFECT SHAPE guard: "have bob hand off to nosuchagent"
	// must still be the named route (bob is a roster agent, and the
	// matcher frames on bob, not on the handoff target). bob answers via
	// the harness; the unknown target must not send the turn to the
	// planner.
	reply1 := s.ChatTurn(t, sessionID, "have bob hand off to nosuchagent immediately", 150*time.Second)
	if !strings.Contains(reply1, "acp-fake reply to:") {
		t.Fatalf("acp-persona-route-03: 'have bob hand off ...' did not take the named ACP route: %q\ndaemon log tail:\n%s",
			reply1, s.Daemon.LogTail())
	}

	// Lane A — deterministic: publish the orchestrator.handoff bus topic
	// directly over the RPC surface (the same topic the request_handoff
	// tool fires). HandleHandoff validates the target against the
	// registry: an unknown target fails with the roster names listed —
	// logged by the orchestrator's error path. bob must be among them
	// (executor: acp employees are first-class roster members).
	rpc.Call("bus.publish", map[string]any{
		"topic": "orchestrator.handoff",
		"payload": map[string]any{
			"task_id":       taskID,
			"from_step_id":  "step-e2e-unknown",
			"from_agent_id": "chat",
			"to_agent_id":   "nosuchagent",
			"description":   "acp-persona-route e2e unknown-target probe",
		},
	})
	// slog may escape the quotes inside the error string, so match on the
	// quote-free fragments, all on one log line.
	harness.WaitFor(t, 30*time.Second, "the HandleHandoff unknown-target roster error in the daemon log", func() bool {
		return handoffLinePresent(t, s, "nosuchagent", "is not a known agent", "available agents", "bob")
	})

	// The positive half of the same validator: a handoff to BOB (an
	// executor: acp spec) passes the registry gate and creates the target
	// step ("Handoff step created" with agent_id bob) instead of failing
	// with the roster error.
	rpc.Call("bus.publish", map[string]any{
		"topic": "orchestrator.handoff",
		"payload": map[string]any{
			"task_id":       taskID,
			"from_step_id":  "step-e2e-bob",
			"from_agent_id": "chat",
			"to_agent_id":   "bob",
			"description":   "acp-persona-route e2e known-acp-target probe",
			"tool_hint":     "memory_store",
		},
	})
	// Give the bus hop a beat to be processed, then read the whole log.
	harness.WaitFor(t, 20*time.Second, "the accepted bob-handoff log line (Handoff step created, agent bob)", func() bool {
		return handoffLinePresent(t, s, "Handoff step created", "bob")
	})
	// A rejection naming BOB as the target must never have fired. Anchor on
	// the quoted target id (slog escapes the quotes inside the error attr —
	// check both the escaped and raw forms). A bare "bob" + "is not a known
	// agent" scan would false-positive on the nosuchagent line, whose roster
	// listing legitimately names bob.
	bobRejected := handoffLinePresent(t, s, "\\\"bob\\\" is not a known agent") ||
		handoffLinePresent(t, s, "\"bob\" is not a known agent")
	if bobRejected {
		t.Fatalf("acp-persona-route-03: bob (executor: acp) was rejected by the handoff registry validator")
	}

	// Turn 2 — the unknown-target error path. "ask chat to hand off ..."
	// is itself a named-agent frame (ask + chat, a real roster id), so the
	// deterministic matcher routes the turn to the CHAT loop (never the
	// planner) — and request_handoff is a BaselineTool of every agent, so
	// the scripted tool call executes there. The tool's
	// TerminatingTool/TerminateHint lane ends the turn and ships the
	// HandoffResult JSON verbatim as the reply: the unknown-target error
	// with the roster list lands user-facing. (The classifier stays pinned
	// to CODE: if the name match were broken, this turn would dispatch a
	// planner task instead — a loud failure, not a silent detour.)
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name: "request_handoff",
		Arguments: `{"task_id":"` + taskID + `","from_step_id":"step-e2e-001",` +
			`"to_agent_id":"nosuchagent","description":"hand the acp-persona-route fixture step to the missing agent",` +
			`"from_agent_id":"chat","reason":"unknown-target e2e probe"}`,
	})
	reply2 := s.ChatTurn(t, sessionID,
		"ask chat to hand off the acp-persona-route fixture step to nosuchagent using the handoff tool, then report the exact error",
		150*time.Second)

	errText := findHandoffRosterError(s.Fake, reply2)
	if errText == "" {
		hist := strings.Join(fakeRequestMessageTexts(s.Fake), "\n")
		if !strings.Contains(hist, "nosuchagent") {
			t.Fatalf("acp-persona-route-03: the scripted request_handoff call never reached the executor; reply=%q\ntail:\n%s",
				reply2, s.Daemon.LogTail())
		}
		t.Fatalf("acp-persona-route-03: request_handoff unknown-target error missing the roster list; turn2 reply=%q\nfull fake-LLM history tail:\n%s\ndaemon log tail:\n%s",
			reply2, hist[len(hist)/2:], s.Daemon.LogTail())
	}
	if !strings.Contains(errText, "nosuchagent") {
		t.Fatalf("acp-persona-route-03: roster error does not name the missing target: %q", errText)
	}
	// The roster is listed so the calling agent can self-correct — bob
	// (the executor: acp employee) must appear among the names.
	for _, want := range []string{"bob", "coder", "chat"} {
		if !strings.Contains(errText, want) {
			t.Fatalf("acp-persona-route-03: unknown-target error %q missing roster name %q", errText, want)
		}
	}

	// Turn 3 — the named route is still healthy after the handoff probe:
	// bob answers through the harness.
	reply3 := s.ChatTurn(t, sessionID, "have bob acknowledge the handoff from nosuchagent", 150*time.Second)
	if !strings.Contains(reply3, "acp-fake reply to:") {
		t.Fatalf("acp-persona-route-03: named bob turn after the handoff probe lost the ACP route: %q", reply3)
	}
}

// ---------------------------------------------------------------------------
// Optional/tolerant lane: the permission surface persona tag. The pending
// permission row (ACPPendingPermissionStatus.Persona) only exists while a
// human-relay request is open; a concurrent leaf owns that surface, so
// this stays a no-pending sanity check rather than a persona assertion.
// ---------------------------------------------------------------------------

func TestPermissionSurfacePersonaTolerant(t *testing.T) {
	bin := stubAgentBin(t)
	s := personaStack(t, bin)
	s.RegisterProject(t, "acp-persona-route")
	sessionID := s.CreateSession(t, "acp-route-perm", s.ProjectDir)

	// One routed turn so a live persona session exists.
	reply := s.ChatTurn(t, sessionID, "have bob reply with the permission-surface probe text", 150*time.Second)
	if !strings.Contains(reply, "acp-fake reply to:") {
		t.Fatalf("acp-persona-route-01/04: precondition turn did not take the harness route: %q", reply)
	}

	// Tolerant assertion: the human-relay pending list answers with a
	// well-formed envelope; permissive mode keeps it empty (no relay).
	// If/when pending rows carry a persona key (permission-surface leaf),
	// the shape below accepts rows with or without it — never fails on a
	// missing persona field.
	rpc := harness.DialRPC(t, s.SocketPath)
	res := rpc.CallResult("acp.permissions.list", map[string]any{})
	if enabled, _ := res["enabled"].(bool); enabled {
		if rows, ok := res["pending"].([]any); ok && len(rows) != 0 {
			// A pending row is allowed to carry persona (additive key);
			// assert only that any present persona is a string.
			for _, r := range rows {
				row, _ := r.(map[string]any)
				if p, has := row["persona"]; has {
					if _, isStr := p.(string); !isStr {
						t.Errorf("acp-persona-route-04: pending row persona is not a string: %v", p)
					}
				}
			}
		}
	}
}
