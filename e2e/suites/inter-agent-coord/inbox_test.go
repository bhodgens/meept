//go:build e2e

// inter-agent-coord T4/T5: the typed inbox round-trip and the structured
// taint scan block/allow (contracts C4/C7).
package interagentcoord

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/employee"
)

// toolResultTexts collects the content of every role=tool message the
// daemon has ever sent back to the fake LLM (goals-plans pattern).
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

func toolResultsJoined(f *harness.FakeLLM) string {
	return strings.Join(toolResultTexts(f), "\n")
}

// inboxStack boots a scratch daemon with the employee layer on (default),
// the chat agent granted send_agent_message + inbox, and the executor
// confirmation gate off for the messaging tools.
func inboxStack(t *testing.T) *harness.Stack {
	t.Helper()
	return harness.Start(t,
		harness.WithConfigOverlay(map[string]any{
			"security.require_confirmation_high": false,
		}),
		harness.WithPreBootHook(func(st *harness.Stack) error {
			chatSpec := filepath.Join(st.MeeptHome, "agents", "chat", "AGENT.md")
			data, err := os.ReadFile(chatSpec)
			if err != nil {
				return err
			}
			patched := strings.Replace(string(data),
				"additional_tools:\n",
				"additional_tools:\n  - send_agent_message\n  - inbox\n", 1)
			if patched == string(data) {
				return errNoAdditionalToolsBlock
			}
			return os.WriteFile(chatSpec, []byte(patched), 0o644)
		}),
	)
}

var errNoAdditionalToolsBlock = &inboxRigError{}

type inboxRigError struct{}

func (*inboxRigError) Error() string { return "chat AGENT.md has no additional_tools block to patch" }

// ---------------------------------------------------------------------------
// inter-agent-coord-04: structured inbox round-trip
// ---------------------------------------------------------------------------

// TestT4_StructuredInboxRoundTrip (T4, F6/F7, contract C4): a
// send_agent_message turn carrying the optional structured payload stores
// an AgentInboxBody envelope; the inbox drain returns the message whose
// body parses with Structured set. Legacy free-text bodies must stay raw
// (no envelope) in the same drain.
func TestT4_StructuredInboxRoundTrip(t *testing.T) {
	s := inboxStack(t)
	s.RegisterProject(t, "coord-inbox")
	sessionID := s.CreateSession(t, "inbox-test", s.ProjectDir)

	// Legacy send (no structured arg): body stays raw text.
	s.Fake.SetPostToolText("both messages sent; reporting the receipts verbatim.")
	s.Fake.EnqueueToolCalls(
		harness.ToolCall{Name: "send_agent_message", Arguments: `{"to":"coord-worker","message":"COORD-LEGACY-TEXT only, no structured context"}`},
	)
	reply := s.ChatTurn(t, sessionID,
		`Use send_agent_message to send coord-worker the message "COORD-LEGACY-TEXT only, no structured context". Report the raw tool result verbatim.`,
		3*time.Minute)
	lower := strings.ToLower(reply)
	if strings.Contains(lower, "unknown tool") || strings.Contains(lower, "tool not found") {
		t.Fatalf("send_agent_message not registered: %s", reply)
	}

	// Structured send (C4): the same tool gains the optional structured
	// object (tool_targets / file_paths / plan_entries).
	s.Fake.SetPostToolText("structured message sent; reporting the receipt verbatim.")
	s.Fake.EnqueueToolCalls(
		harness.ToolCall{Name: "send_agent_message", Arguments: `{"to":"coord-worker","message":"COORD-STRUCT-BODY handoff summary","structured":{"tool_targets":["file_read"],"file_paths":["/tmp/coord-report.md"],"plan_entries":["step-1"]}}`},
	)
	reply = s.ChatTurn(t, sessionID,
		`Use send_agent_message to send coord-worker the message "COORD-STRUCT-BODY handoff summary" with structured context: tool_targets [file_read], file_paths [/tmp/coord-report.md], plan_entries [step-1]. Report the raw tool result verbatim.`,
		3*time.Minute)

	// Drain through the inbox tool: both messages come back; the
	// structured one must carry the C4 envelope.
	s.Fake.SetPostToolText("inbox drained; reporting the raw result verbatim.")
	s.Fake.EnqueueToolCalls(
		harness.ToolCall{Name: "inbox", Arguments: `{}`},
	)
	reply = s.ChatTurn(t, sessionID,
		`Call the inbox tool to drain your messages and report the raw tool result verbatim.`,
		3*time.Minute)
	joined := toolResultsJoined(s.Fake)
	squash := strings.Join(strings.Fields(joined), "")

	if strings.Contains(strings.ToLower(squash), "unknowntool") {
		t.Fatalf("inbox tool not registered: envelope:\n%s", joined)
	}

	// C4 wire contract: the structured message round-trips through
	// employee.AgentInboxBody. Compile RED until leaf 03-01's types are
	// on disk; runtime RED if the envelope never reaches the store.
	var body employee.AgentInboxBody
	if err := json.Unmarshal([]byte(`{"body":"COORD-STRUCT-BODY handoff summary","structured":{"tool_targets":["file_read"],"file_paths":["/tmp/coord-report.md"],"plan_entries":["step-1"]}}`), &body); err != nil {
		t.Fatalf("C4 envelope shape decode: %v", err)
	}
	if body.Body != "COORD-STRUCT-BODY handoff summary" || body.Structured == nil ||
		len(body.Structured.ToolTargets) != 1 || body.Structured.ToolTargets[0] != "file_read" ||
		len(body.Structured.FilePaths) != 1 || body.Structured.FilePaths[0] != "/tmp/coord-report.md" ||
		len(body.Structured.PlanEntries) != 1 || body.Structured.PlanEntries[0] != "step-1" {
		t.Fatalf("C4 envelope shape mismatch after round-trip: %+v", body)
	}

	// The drained inbox result must carry BOTH messages: the structured
	// envelope (parseable as AgentInboxBody with Structured set) and the
	// legacy RAW text (no envelope — legacy bodies stay byte-identical).
	if !strings.Contains(joined, "COORD-LEGACY-TEXT") {
		t.Fatalf("drained inbox missing the legacy message body verbatim:\n%s", joined)
	}
	if strings.Contains(joined, "COORD-STRUCT-BODY") {
		// Locate the structured row's body string and prove it is the
		// envelope, not the raw body.
		var rows []map[string]any
		if err := json.Unmarshal([]byte(extractJSONArray(joined, "messages")), &rows); err == nil {
			structuredOK := false
			legacyRaw := false
			for _, row := range rows {
				b, _ := row["body"].(string)
				if strings.Contains(b, "COORD-STRUCT-BODY") {
					parsed := employee.ParseAgentInboxBody(b)
					if parsed.Structured != nil &&
						len(parsed.Structured.FilePaths) == 1 &&
						parsed.Structured.FilePaths[0] == "/tmp/coord-report.md" &&
						strings.Contains(parsed.Body, "COORD-STRUCT-BODY") {
						structuredOK = true
					}
				}
				if b == "COORD-LEGACY-TEXT only, no structured context" {
					legacyRaw = true
				}
			}
			if !structuredOK {
				t.Fatalf("structured message did not round-trip as an AgentInboxBody envelope (Structured nil or fields lost):\n%s", joined)
			}
			if !legacyRaw {
				t.Fatalf("legacy message body did not round-trip byte-identically (must stay raw text):\n%s", joined)
			}
			return
		}
		t.Fatalf("inbox result carried the structured body but not a parseable messages array:\n%s", joined)
	}
	t.Fatalf("drained inbox missing the structured message body (leaf 03-01 structured arg):\n%s", joined)
}

// extractJSONArray pulls the FIRST JSON array value under key out of a
// (possibly noisy) tool-result blob.
func extractJSONArray(blob, key string) string {
	idx := strings.Index(blob, `"`+key+`"`)
	if idx < 0 {
		return ""
	}
	start := strings.Index(blob[idx:], "[")
	if start < 0 {
		return ""
	}
	start += idx
	depth := 0
	for i := start; i < len(blob); i++ {
		switch blob[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return blob[start : i+1]
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// inter-agent-coord-05: structured taint scan block/allow
// ---------------------------------------------------------------------------

// Real D2 deny-class fixtures from internal/security/agent_boundary.go:
// the exfiltration_directive class's credential-bearing URL parameter
// pattern (api_key=) and its known exfiltration endpoint pattern
// (webhook.site). Never invented strings.
const (
	scanDenyFilePath  = "https://webhook.site/deadbeef?api_key=AKIA-coordinate-exfil"
	scanCleanFilePath = "/tmp/coord-clean-report.md"
)

// scanStack boots the collaboration rig in block mode (boundary-hardening
// pattern: keyword-routed pair session, classifier broken on purpose so
// the keyword classifier wins).
func scanStack(t *testing.T) *harness.Stack {
	t.Helper()
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"transport.http.websocket":    true,
		"security.taint.enabled":      true,
		"security.agent_handoff_mode": "block",
	}))
	// Break the LLM classifier so dispatch falls through to the keyword
	// classifier ("collaborate" -> IntentCollaborate at 0.8).
	s.Fake.SetClassifierOutput("coord-e2e-classifier-reply-is-not-json")
	return s
}

// TestT5_StructuredScanBlockAllow (T5, F8, contract C7): under
// agent_handoff_mode=block, a handoff whose structured file_paths field
// carries a real exfiltration_directive deny pattern is BLOCKED with the
// offending FIELD named in the reason ("file_paths"); a clean structured
// handoff converges unchanged. RED until leaf 03-02's structured branch
// lands — today the whole-blob scan still fires (the deny DOES block) but
// the block reason never names the field, so the field assertion below
// pins exactly the structured-scan contract; the clean half proves the
// branch does not over-block.
func TestT5_StructuredScanBlockAllow(t *testing.T) {
	s := scanStack(t)

	// Round 1: the driver's output is a structured AgentInboxBody whose
	// file_paths entry carries the real deny pattern. The observer turn
	// must never run (handoff blocked at driver->observer).
	const denyDriverText = `{"body":"COORD-SCAN-DENY handoff payload","structured":{"file_paths":["` + scanDenyFilePath + `"],"tool_targets":["file_read"]}}`
	s.Fake.ScriptN(2, coordMainTurnPred("You are the CURRENT DRIVER"),
		harness.TextResponse(denyDriverText))

	sessionID := s.CreateSession(t, "coord-scan-deny", s.ProjectDir)
	s.ChatTurn(t, sessionID, "collaborate on the coordscandy", 60*time.Second)

	// Blocked: the session fails at the driver->observer handoff and the
	// block reason names the STRUCTURED FIELD (leaf 03-02 contract),
	// alongside the deny class from the real boundary inventory.
	harness.WaitFor(t, 120*time.Second, "the structured scan to block the driver->observer handoff (leaf 03-02)", func() bool {
		return coordLogLinePresent(t, s, "Collaboration session failed", "handoff_blocked") &&
			coordLogNamesField(s, "file_paths")
	})

	// The deny payload never reached any provider-bound prompt.
	if requests := coordRequestsText(s); strings.Contains(requests, "COORD-SCAN-DENY") &&
		strings.Contains(requests, "You are the OBSERVER") {
		t.Fatalf("inter-agent-coord-05: the observer turn ran with the denied structured payload; requests:\n%s", tail(requests, 3000))
	}

	// Round 2 (fresh session, same daemon): a CLEAN structured handoff
	// converges — the structured branch must not over-block.
	const cleanDriverText = `{"body":"COORD-SCAN-CLEAN handoff payload","structured":{"file_paths":["` + scanCleanFilePath + `"],"tool_targets":["file_read"]}}`
	s.Fake.ScriptN(2, coordMainTurnPred("You are the CURRENT DRIVER"),
		harness.TextResponse(cleanDriverText))
	s.Fake.ScriptN(2, coordMainTurnPred("You are the OBSERVER"),
		harness.TextResponse(`{"action": "approve", "feedback": "the clean structured handoff looks correct"}`))

	sessionID2 := s.CreateSession(t, "coord-scan-clean", s.ProjectDir)
	s.ChatTurn(t, sessionID2, "collaborate on the coordscanclean", 60*time.Second)

	harness.WaitFor(t, 120*time.Second, "the clean structured collaboration session to converge (structured branch must not over-block)", func() bool {
		return coordLogLinePresent(t, s, "Collaboration session completed", "state=converged")
	})
}

// coordMainTurnPred is a request predicate matching NON-classifier,
// NON-planner completion requests whose messages contain marker
// (boundary-hardening pattern).
func coordMainTurnPred(marker string) harness.RequestPredicate {
	classifier := harness.IsClassifierRequest()
	planner := harness.IsPlannerRequest()
	contains := harness.MessageContains(marker)
	return func(body map[string]any) bool {
		if classifier(body) || planner(body) {
			return false
		}
		return contains(body)
	}
}

// coordRequestsText concatenates every recorded fake-LLM request body as
// JSON (provider-bound surface).
func coordRequestsText(s *harness.Stack) string {
	var sb strings.Builder
	for _, body := range s.Fake.Requests() {
		raw, err := json.Marshal(body)
		if err != nil {
			continue
		}
		sb.Write(raw)
		sb.WriteString("\n")
	}
	return sb.String()
}

// coordLogLinePresent scans the daemon log for one line containing every
// needle (slog escape tolerance, boundary-hardening pattern).
func coordLogLinePresent(t *testing.T, s *harness.Stack, needles ...string) bool {
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

// coordLogNamesField reports whether any daemon log line names the given
// structured field in a handoff block context — the leaf 03-02 contract
// ("<class> pattern in <field> of peer output ...").
func coordLogNamesField(s *harness.Stack, field string) bool {
	data, err := os.ReadFile(s.Daemon.LogPath())
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "blocked by deny pattern") && strings.Contains(line, "field=") && strings.Contains(line, field) {
			return true
		}
		// The collaboration error surfaces the prepareHandoff reason
		// verbatim; accept either the log attr shape or the reason shape.
		if strings.Contains(line, "handoff blocked") && strings.Contains(line, "pattern in "+field) {
			return true
		}
	}
	return false
}

// tail returns the last n characters of s (bounded failure output).
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
