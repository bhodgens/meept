//go:build e2e

// Suite office-skills: the 06-office-skills branch e2e gate (T1-T4 rows of
// docs/plans/20261102-dsh-convergence/06-office-skills/00T-tests.md).
// Asserts the TOOL BEHAVIOR CONTRACT (contracts C11.2-C11.4) through the
// live daemon surface - it NEVER imports the concrete docx/pptx library, so
// the suite is identical regardless of what the research leaf resolved.
//
// T1-T3 drive the office_* tools through REAL agent turns: the fake LLM
// emits scripted tool calls (create -> read) and the daemon executes the real
// tools against the session working dir. The office tools are granted to a
// seeded user-tier agent (aastub, alphabetically first holder of the code
// lane), mirroring the tools-filesystem / tools-memory grant-seeding pattern.
//
// T4 asserts the requires-tools gate through skills.execute (the "skill
// <name> requires unavailable tool(s): <list>" contract, C11.1) — the same
// hermetic surface skills-discovery-02 uses. Skill invocation in the chat
// dispatcher is slash-command-only, so a bare chat prompt cannot reach the
// skill executor; the RPC drives the executor directly.
package office_skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// toolResultTexts collects the content of every role=tool message the daemon
// has sent back to the fake LLM — the observable end state of a tool call.
func toolResultTexts(f *harness.FakeLLM) []string {
	var out []string
	for _, body := range f.Requests() {
		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if role, _ := msg["role"].(string); role == "tool" {
				if c, _ := msg["content"].(string); c != "" {
					out = append(out, c)
				}
			}
		}
	}
	return out
}

// toolResultsJoined is toolResultTexts joined for substring assertions.
func toolResultsJoined(f *harness.FakeLLM) string {
	return strings.Join(toolResultTexts(f), "\n")
}

// seedOfficeAgent writes a user-tier agent ("aastub", alphabetically the first
// holder of the code lane) carrying the office tool grants, so the per-agent
// filtered registry exposes them to real code-lane turns. Mirrors
// seedGrantAgent in the tools-filesystem suite.
func seedOfficeAgent(t *testing.T, st *harness.Stack, grants []string) {
	t.Helper()
	dir := filepath.Join(st.MeeptHome, "agents", "aastub")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir aastub agent dir: %v", err)
	}
	var tools strings.Builder
	for _, g := range grants {
		tools.WriteString("  - " + g + "\n")
	}
	body := "---\n" +
		"id: aastub\n" +
		"name: E2E Office Specialist\n" +
		"role: executor\n" +
		"description: e2e-only agent holding the office tool grants\n" +
		"intents: [code]\n" +
		"enabled: true\n" +
		"can_delegate: false\n" +
		"additional_tools:\n" +
		tools.String() +
		"capabilities:\n  - reasoning\n" +
		"max_iterations: 8\n" +
		"timeout_seconds: 120\n" +
		"---\n\n# E2E Office Specialist\n\nFollow the user's request briefly.\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write aastub AGENT.md: %v", err)
	}
}

// startWithOfficeGrants boots a stack whose roster includes the aastub agent
// carrying the given grants, registers the project, and returns the stack.
func startWithOfficeGrants(t *testing.T, name string, grants []string) *harness.Stack {
	t.Helper()
	s := harness.Start(t, harness.WithPreBootHook(func(st *harness.Stack) error {
		seedOfficeAgent(t, st, grants)
		return nil
	}))
	s.RegisterProject(t, "office-e2e")
	return s
}

// TestT1_OfficeDocxRoundTrip (C11.2): office_docx create -> read round-trip
// returns the final absolute path and the reopened document contains the
// requested heading/paragraph text.
func TestT1_OfficeDocxRoundTrip(t *testing.T) {
	s := startWithOfficeGrants(t, "docx-roundtrip", []string{"office_docx"})
	sessionID := s.CreateSession(t, "docx-roundtrip", s.ProjectDir)
	// Code lane heuristic (imperative + artifact noun) routes to aastub.
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned office"}`)

	s.Fake.EnqueueToolCalls(
		harness.ToolCall{
			Name:      "office_docx",
			Arguments: `{"action":"create","path":"probe.docx","blocks":[{"type":"heading","text":"PROBE-HEADING","level":"1"},{"type":"paragraph","text":"PROBE-PARA"}]}`,
		},
		harness.ToolCall{
			Name:      "office_docx",
			Arguments: `{"action":"read","path":"probe.docx"}`,
		},
	)

	reply := s.ChatTurn(t, sessionID,
		"Call the office_docx tool to create probe.docx, then read it back and report both raw results.",
		5*time.Minute)
	lower := strings.ToLower(reply)
	if strings.Contains(lower, "unknown tool") || strings.Contains(lower, "tool not found") ||
		strings.Contains(lower, "not registered") {
		t.Fatalf("office_docx not registered: %s", reply)
	}
	res := toolResultsJoined(s.Fake)
	if !strings.Contains(res, "PROBE-HEADING") || !strings.Contains(res, "PROBE-PARA") {
		t.Errorf("docx read-back missing requested content; tool results:\n%s", res)
	}
	if !strings.Contains(res, "probe.docx") {
		t.Errorf("docx result missing the final path; tool results:\n%s", res)
	}
}

// TestT2_OfficePptxRoundTrip (C11.3): office_pptx create -> read round-trip
// returns the final path and the reopened deck has the requested slide
// title/body.
func TestT2_OfficePptxRoundTrip(t *testing.T) {
	s := startWithOfficeGrants(t, "pptx-roundtrip", []string{"office_pptx"})
	sessionID := s.CreateSession(t, "pptx-roundtrip", s.ProjectDir)
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned office"}`)

	s.Fake.EnqueueToolCalls(
		harness.ToolCall{
			Name:      "office_pptx",
			Arguments: `{"action":"create","path":"probe.pptx","slides":[{"title":"PROBE-TITLE","body":["PROBE-BODY"],"notes":"PROBE-NOTES"}]}`,
		},
		harness.ToolCall{
			Name:      "office_pptx",
			Arguments: `{"action":"read","path":"probe.pptx"}`,
		},
	)

	reply := s.ChatTurn(t, sessionID,
		"Call the office_pptx tool to create probe.pptx, then read it back and report both raw results.",
		5*time.Minute)
	lower := strings.ToLower(reply)
	if strings.Contains(lower, "unknown tool") || strings.Contains(lower, "tool not found") ||
		strings.Contains(lower, "not registered") {
		t.Fatalf("office_pptx not registered: %s", reply)
	}
	res := toolResultsJoined(s.Fake)
	if !strings.Contains(res, "PROBE-TITLE") || !strings.Contains(res, "PROBE-BODY") {
		t.Errorf("pptx read-back missing requested slide content; tool results:\n%s", res)
	}
}

// TestT3_OfficeWorkbookRoundTrip (C11.4): office_workbook create -> read
// round-trip returns the final path, sheet names, and typed cell values;
// formulas are preserved, not evaluated.
func TestT3_OfficeWorkbookRoundTrip(t *testing.T) {
	s := startWithOfficeGrants(t, "xlsx-roundtrip", []string{"office_workbook"})
	sessionID := s.CreateSession(t, "xlsx-roundtrip", s.ProjectDir)
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned office"}`)

	s.Fake.EnqueueToolCalls(
		harness.ToolCall{
			Name:      "office_workbook",
			Arguments: `{"action":"create","path":"probe.xlsx","sheets":[{"name":"PROBE-SHEET","headers":["name","count"],"rows":[["widget",3],["gadget",5]]}]}`,
		},
		harness.ToolCall{
			Name:      "office_workbook",
			Arguments: `{"action":"read","path":"probe.xlsx"}`,
		},
	)

	reply := s.ChatTurn(t, sessionID,
		"Call the office_workbook tool to create probe.xlsx, then read it back and report both raw results.",
		5*time.Minute)
	lower := strings.ToLower(reply)
	if strings.Contains(lower, "unknown tool") || strings.Contains(lower, "tool not found") ||
		strings.Contains(lower, "not registered") {
		t.Fatalf("office_workbook not registered: %s", reply)
	}
	res := toolResultsJoined(s.Fake)
	for _, want := range []string{"PROBE-SHEET", "widget", "gadget"} {
		if !strings.Contains(res, want) {
			t.Errorf("xlsx read-back missing %q; tool results:\n%s", want, res)
		}
	}
}

// skillsExecuteRPC calls the skills.execute RPC; returns (content, errString).
// This is the same hermetic surface skills-discovery-02 uses — the chat
// dispatcher's skill invocation is slash-command-only, so the RPC is the
// reachable way to drive the skill executor (and thus checkRequiredTools).
func skillsExecuteRPC(t *testing.T, s *harness.Stack, name, input string) (string, string) {
	t.Helper()
	client := harness.DialRPC(t, s.SocketPath)
	params, _ := json.Marshal(map[string]string{"name": name, "input": input})
	resp := client.Call("skills.execute", json.RawMessage(params))
	if e, ok := resp["error"]; ok && e != nil {
		if msg, ok := e.(string); ok {
			return "", msg
		}
		raw, _ := json.Marshal(e)
		return "", string(raw)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("skills.execute returned non-object result: %v", resp["result"])
	}
	content, _ := result["content"].(string)
	return content, ""
}

// TestT4_RequiresToolsGate (F14d, C11.1): with
// skills.validate_prerequisites=true, executing a skill whose requires-tools
// lists an UNREGISTERED tool is refused with the exact contract message
// "skill <name> requires unavailable tool(s): <list>".
func TestT4_RequiresToolsGate(t *testing.T) {
	const marker = "office-probe"
	s := harness.Start(t,
		harness.WithConfigOverlay(map[string]any{
			"skills.validate_prerequisites": true,
		}),
		harness.WithPreBootHook(func(st *harness.Stack) error {
			// Drop a probe skill whose required tool does not exist.
			dir := filepath.Join(st.MeeptHome, "skills", marker)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(
				"---\nname: office-probe\ndescription: probe skill for the requires-tools gate\nrequires-tools:\n  - office_docx_missing_probe\n---\n\nRun the probe.\n"), 0o644)
		}))
	s.RegisterProject(t, "office-gate")

	// No fake tool rigging needed: the gate fires pre-LLM inside the skill
	// executor (checkRequiredTools), so the RPC error carries the contract
	// message and ZERO completion requests reach the LLM.
	_, errText := skillsExecuteRPC(t, s, "office-probe", "run the gated probe skill")
	if !strings.Contains(errText, "requires unavailable tool") {
		t.Fatalf("requires-tools gate did not fire with the contract message: %s", errText)
	}
	if !strings.Contains(errText, "office_docx_missing_probe") {
		t.Errorf("gate message missing the unavailable tool name: %s", errText)
	}
}
