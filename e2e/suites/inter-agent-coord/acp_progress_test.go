//go:build e2e

// inter-agent-coord T3: an ACP plan update reaches WS (contract C5).
//
// The suite ships its OWN scripted ACP stub (testdata/planstub — a
// standalone stdio JSON-RPC main, the leaf's "scripted persona" route) so
// the scenario never depends on the shared internal/acp testdata fixture
// gaining a plan mode. On session/prompt the stub emits a session/update
// with sessionUpdate "plan" (the C5 AgentProgressUpdate wire shape),
// then the reply chunk, then stopReason end_turn.
//
// The bridge (leaf 02-02) must forward the plan update to agent.progress
// with stage "acp"; the WS relay classifies agent.progress as
// agent_progress (the ws-classification-06 shape).
package interagentcoord

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/acp"
)

// planStubMain is the scripted ACP agent: initialize + session/new
// handshake, then per prompt: plan session/update -> reply chunk ->
// stopReason end_turn. The struct tags are spliced with backtick
// concatenation (a Go raw string cannot contain backticks); the body is
// byte-identical to testdata/planstub-verify/main.go, the verified copy.
const planStubMain = "package main\n\nimport (\n	\"bufio\"\n	\"encoding/json\"\n	\"os\"\n)\n\nconst planMarker = \"" + planMarker + "\"\n\ntype msg struct {\n" +
	"	JSONRPC string          `json:\"jsonrpc\"`\n" +
	"	ID      *int64          `json:\"id,omitempty\"`\n" +
	"	Method  string          `json:\"method,omitempty\"`\n" +
	"	Params  json.RawMessage `json:\"params,omitempty\"`\n" +
	"	Result  json.RawMessage `json:\"result,omitempty\"`\n" +
	"}\n" +
	`
func write(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func reply(id *int64, result any) {
	if id == nil {
		return
	}
	raw, _ := json.Marshal(result)
	write(msg{JSONRPC: "2.0", ID: id, Result: raw})
}

func main() {
	reader := bufio.NewReader(os.Stdin)
	dec := json.NewDecoder(reader)
	sessionID := "sess-planstub"
	for {
		var m msg
		if err := dec.Decode(&m); err != nil {
			return
		}
		switch m.Method {
		case "initialize":
			reply(m.ID, map[string]any{
				"protocolVersion": 1,
				"agentInfo":       map[string]string{"name": "planstub"},
			})
		case "session/new":
			reply(m.ID, map[string]any{"sessionId": sessionID})
		case "session/prompt":
			plan, _ := json.Marshal(map[string]any{
				"sessionId": sessionID,
				"update": map[string]any{
					"sessionUpdate": "plan",
					"planId":        "plan-coord-1",
					"content": map[string]any{
						"entries": []map[string]string{
							{"title": planMarker, "status": "in_progress"},
						},
					},
				},
			})
			write(msg{JSONRPC: "2.0", Method: "session/update", Params: plan})
			chunk, _ := json.Marshal(map[string]any{
				"sessionId": sessionID,
				"update": map[string]any{
					"sessionUpdate": "agent_message_chunk",
					"content":       map[string]string{"type": "text", "text": "planstub reply"},
				},
			})
			write(msg{JSONRPC: "2.0", Method: "session/update", Params: chunk})
			reply(m.ID, map[string]any{"stopReason": "end_turn"})
		case "session/cancel":
		default:
			if m.ID != nil && m.Method == "" {
				continue
			}
		}
	}
}
`

// planMarker pins the plan identity the WS assertion scans for.
const planMarker = "COORD-PLAN-STEP-1"

// planStubBin builds the scripted stub from the suite's testdata dir.
func planStubBin(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "planstub-src")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("planstub mkdir: %v", err)
	}
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte(planStubMain), 0o644); err != nil {
		t.Fatalf("planstub write: %v", err)
	}
	bin := filepath.Join(dir, "planstub")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build planstub: %v\n%s", err, out)
	}
	return bin
}

// repoRoot resolves the module root relative to this file's location.
func repoRoot() string {
	_, thisFile, _, _ := runtime.Caller(0) //nolint:dogsled // identity probe
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}

// coordAgentDef is the executor: acp roster fixture (acp-persona-route
// shape): every turn is forwarded verbatim to the ACP harness.
const coordAgentDef = `---
id: coord-acp
name: coord-acp
role: executor
description: inter-agent-coord e2e fixture — every turn forwarded to the ACP harness
executor: acp
acp:
  agent: acp-coord
  persona: coord
  permission_mode: permissive
enabled: true
---

# coord-acp

E2E fixture agent.
`

// acpStack boots a scratch daemon with [acp] enabled + permissive, the
// planstub catalog entry, and the coord-acp roster agent.
func acpStack(t *testing.T, bin string) *harness.Stack {
	t.Helper()
	s := harness.Start(t,
		harness.WithConfigOverlay(map[string]any{
			"transport.http.websocket":           true,
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
			catalog := fmt.Sprintf(
				`{"agents":[{"id":"acp-coord","command":[%q],"enabled":true}]}`, bin)
			if err := os.WriteFile(filepath.Join(st.MeeptHome, "acp_agents.json5"),
				[]byte(catalog), 0o600); err != nil {
				return err
			}
			dir := filepath.Join(st.MeeptHome, "agents", "coord-acp")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(coordAgentDef), 0o644)
		}),
	)
	return s
}

// ---------------------------------------------------------------------------
// inter-agent-coord-03: ACP plan update reaches WS
// ---------------------------------------------------------------------------

// TestT3_ACPPlanUpdateReachesWS (T3, F4, contract C5): an ACP session
// sends a session/update with sessionUpdate "plan"; the WS stream carries
// the forwarded agent.progress frame with stage "acp" and the plan
// rendered in its detail.
//
// RED today: the plan payload below is marshaled from acp.AgentProgressUpdate
// (the C5 contract type, leaf 02-02) — COMPILE RED until that type lands;
// after it does, onNotice's missing plan branch keeps the waitFor below
// timing out — RUNTIME RED until 02-02 forwards plan events through
// emitACPUpdate.
func TestT3_ACPPlanUpdateReachesWS(t *testing.T) {
	bin := planStubBin(t)
	s := acpStack(t, bin)
	s.RegisterProject(t, "coord-acp-plan")
	sessionID := s.CreateSession(t, "acp-plan-test", s.ProjectDir)

	// The C5 wire shape the plan branch must consume (compile-pinned):
	// sessionUpdate discriminates the variant; title/content carry the
	// renderable payload the bridge renders into the progress detail.
	update := acp.AgentProgressUpdate{
		SessionUpdate: "plan",
		PlanID:        "plan-coord-1",
		Content:       json.RawMessage(`{"entries":[{"title":"` + planMarker + `","status":"in_progress"}]}`),
		Title:         planMarker,
	}
	if update.SessionUpdate != "plan" || update.Title != planMarker {
		t.Fatalf("C5 AgentProgressUpdate construction mismatch: %+v", update)
	}

	collector := collectWS(t, s, 4*time.Minute)

	reply := s.ChatTurn(t, sessionID, "have coord-acp say hello from the acp harness", 3*time.Minute)
	if !strings.Contains(reply, "planstub reply") {
		t.Fatalf("inter-agent-coord-03: the turn did not route through the ACP harness: %q\ndaemon log tail:\n%s",
			reply, s.Daemon.LogTail())
	}

	plan := collector.waitFor(t,
		`agent.progress (agent_progress) frame carrying the ACP plan update with detail "`+planMarker+`" (leaf 02-02 plan branch)`,
		2*time.Minute,
		func(ev coordFrame) bool {
			if frameSourceTopic(ev) != "agent.progress" || frameString(ev, "stage") != "acp" {
				return false
			}
			return strings.Contains(string(ev.Data), planMarker)
		})

	if plan.Type != "agent_progress" {
		t.Fatalf("the acp plan frame classified as type %q, want agent_progress (legacy agent.progress classification)", plan.Type)
	}
	// The existing acp progress attribution keys must ride the frame
	// (publishACPUpdate shape) so the GUI/TUI can attribute the plan.
	if got := frameString(plan, "agent_id"); got != "coord-acp" {
		t.Fatalf("plan frame payload[agent_id] = %q, want coord-acp", got)
	}
	if got := frameString(plan, "acp_agent"); got != "acp-coord" {
		t.Fatalf("plan frame payload[acp_agent] = %q, want acp-coord", got)
	}
}
