//go:build e2e

// Suite agent-groups: the agent-groups plan test gate (docs/plans/
// 20261030-agent-groups/00T-e2e-agent-groups.md). Every test is RED today:
// the `groups` config section (internal/config/schema.go), the
// internal/groups resolver, the groups.* RPC surface, and the pkg/security
// fence stage / decision-audit absorption do not exist yet, so each test
// fails on a missing-implementation anchor — never on a harness breakage.
//
// Coverage map (manifest scenarios):
//
//	agent-groups-01  F1 deny-beats-allow        — TestAgentGroupsF1DenyBeatsAllow
//	agent-groups-02  F2 ungrouped backward compat — TestAgentGroupsF2UngroupedBackwardCompat
//	agent-groups-03  F3 assignment validation   — TestAgentGroupsF3AssignmentValidationRejectsUnknownTool
//	agent-groups-04  F4 membership mutation gate — TestAgentGroupsF4MutationGateDeniesNonPrivileged
//	agent-groups-05  F6 fence stage             — TestAgentGroupsF6FenceStageDeniesOutsideRoot
//	agent-groups-06  F7 decision audit log      — TestAgentGroupsF7DecisionLogRecordsGroupDeny
//	agent-groups-07  F5 memory ceiling          — f5_memory_ceiling_test.go
//	                                     (re-armed by leaf 05-memory-ceiling
//	                                     after #70 branch 01 landed)
//	agent-groups-08  F9 cluster dispatch identity — SKIPPED member (cluster_identity_member_test.go; dated)
//
// All assertions ride live surfaces (the fake-LLM tool-offer/tool-result
// projection, the CLI, the RPC socket, the daemon log) — never store
// internals. Fixtures are minimal: one grouped aastub agent per scenario.
package agentgroups

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// ---------------------------------------------------------------------------
// fixtures
// ---------------------------------------------------------------------------

// seedAgent writes a user-tier AGENT.md under the sandbox roster. "aastub"
// sorts before every bundled specialist sharing the lane, so a pinned
// intent routes to it (same pattern as tools-filesystem / knowledge suites).
// groups carries the `groups:` frontmatter membership (D1/C4); grants are
// the spec's additional_tools (the pre-groups allowlist the policy layers
// over — binding never widens the spec, D4/C3).
func seedAgent(t *testing.T, st *harness.Stack, id, intents string, grants, groups []string) {
	t.Helper()
	dir := filepath.Join(st.MeeptHome, "agents", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s agent dir: %v", id, err)
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("id: " + id + "\n")
	b.WriteString("name: E2E " + id + "\n")
	b.WriteString("role: executor\n")
	b.WriteString("description: e2e agent-groups gate agent\n")
	b.WriteString("intents: [" + intents + "]\n")
	if len(groups) > 0 {
		b.WriteString("groups: [" + strings.Join(groups, ", ") + "]\n")
	}
	b.WriteString("enabled: true\ncan_delegate: false\n")
	if len(grants) > 0 {
		b.WriteString("additional_tools:\n")
		for _, g := range grants {
			b.WriteString("  - " + g + "\n")
		}
	}
	b.WriteString("capabilities:\n  - reasoning\nmax_iterations: 8\ntimeout_seconds: 120\n")
	b.WriteString("---\n\nDo exactly what the user asks with the tools you have.\n")
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write %s AGENT.md: %v", id, err)
	}
}

// policy builds one C3 policy entry. extra carries the optional ceilings
// (fs_roots, memory_max_scope, network_class, allowed_models).
func policy(name string, allowed, denied []string, extra map[string]any) map[string]any {
	p := map[string]any{"name": name}
	if len(allowed) > 0 {
		p["tools_allowed"] = allowed
	}
	if len(denied) > 0 {
		p["tools_denied"] = denied
	}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

// binding builds one C3 binding entry (daemon scope).
func binding(subject, pol string) map[string]any {
	return map[string]any{"subject": subject, "policy": pol, "scope": "daemon"}
}

// groupsSection assembles the top-level `groups` config section (C3).
func groupsSection(policies, bindings []map[string]any, defaultManaged bool) map[string]any {
	return map[string]any{
		"policies":           policies,
		"bindings":           bindings,
		"default_managed":    defaultManaged,
		"decision_log_level": "deny-warn",
	}
}

// offeredToolNames lists every tool name the executor ever offered the model
// (the registry's projection into the requests' tools arrays).
func offeredToolNames(f *harness.FakeLLM) map[string]bool {
	names := map[string]bool{}
	for _, body := range f.Requests() {
		tools, ok := body["tools"].([]any)
		if !ok {
			continue
		}
		for _, raw := range tools {
			fn, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if f2, ok := fn["function"].(map[string]any); ok {
				fn = f2
			}
			if n, _ := fn["name"].(string); n != "" {
				names[n] = true
			}
		}
	}
	return names
}

// toolResultTexts collects the content of every role=tool message the daemon
// has ever sent back to the fake LLM (the observable end state of a tool
// call: what the model actually saw).
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

// daemonLog returns the daemon's slog output (the rotating
// <state-dir>/meept.log; the harness stdout capture only holds pre-logger
// output), falling back to the captured tail.
func daemonLog(s *harness.Stack) string {
	if data, err := os.ReadFile(filepath.Join(s.StateDir, "meept.log")); err == nil {
		return string(data)
	}
	return s.Daemon.LogTail()
}

// tail returns the last n characters of s for error messages.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}

// waitForLogLine polls the daemon log for one line matching match. Fatals
// with the log tail on timeout — the RED anchor for log-shaped contracts.
func waitForLogLine(t *testing.T, s *harness.Stack, desc string, timeout time.Duration, match func(line string) bool) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(daemonLog(s), "\n") {
			if match(line) {
				return line
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("daemon log never carried %s within %s; log tail:\n%s", desc, timeout, tail(daemonLog(s), 2000))
	return ""
}

// rpcErrorMessage extracts the error message from a JSON-RPC error envelope.
// Fatals when the call unexpectedly succeeded.
func rpcErrorMessage(t *testing.T, method string, resp map[string]any) string {
	t.Helper()
	e, ok := resp["error"]
	if !ok || e == nil {
		t.Fatalf("%s: expected an error envelope, got result: %v", method, resp["result"])
	}
	em, ok := e.(map[string]any)
	if !ok {
		return fmt.Sprint(e)
	}
	if m, ok := em["message"].(string); ok && m != "" {
		return m
	}
	return fmt.Sprint(em)
}

// ---------------------------------------------------------------------------
// agent-groups-01 (F1): deny beats allow
// ---------------------------------------------------------------------------

// TestAgentGroupsF1DenyBeatsAllow pins the frozen precedence (D4): a grouped
// agent whose binding policy allows file_read and denies shell must not be
// offered the denied tool at all (absent from GetDefinitions) and a scripted
// shell call must be uncallable — while the allowed tool keeps working.
//
// RED today: the `groups` config section and the internal/groups resolver do
// not exist, so the config is silently ignored and the spec grant of
// shell_execute still offers/executes shell.
func TestAgentGroupsF1DenyBeatsAllow(t *testing.T) {
	// Re-armed by leaf 04-tool-gates: the groups resolver now feeds the
	// dispatch intersection (filterTools), so this gate runs for real.
	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			seedAgent(t, st, "aastub", "code", []string{"file_read", "shell_execute"}, []string{"coders"})
			return os.WriteFile(filepath.Join(st.ProjectDir, "readme.txt"), []byte("grouped agent fixture\n"), 0o644)
		}),
		harness.WithConfigOverlay(map[string]any{
			"groups": groupsSection(
				[]map[string]any{policy("coders", []string{"file_read"}, []string{"shell"}, nil)},
				[]map[string]any{binding("group:coders", "coders")},
				false,
			),
		}),
	)
	s.RegisterProject(t, "e2e-project")
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned code"}`)
	sessionID := s.CreateSession(t, "ag-f1", s.ProjectDir)

	s.Fake.SetPostToolText("Grouped-agent probes complete.")
	s.Fake.EnqueueToolCalls(
		harness.ToolCall{Name: "file_read", Arguments: `{"path":"` + filepath.Join(s.ProjectDir, "readme.txt") + `"}`},
		harness.ToolCall{Name: "shell", Arguments: `{"command":"echo AGROUPS-F1-MARKER"}`},
	)
	_ = s.ChatTurn(t, sessionID, "Read readme.txt, then run echo AGROUPS-F1-MARKER through the shell tool", 120*time.Second)

	offered := offeredToolNames(s.Fake)
	if !offered["file_read"] {
		t.Fatalf("F1: policy-allowed tool file_read missing from offered definitions; offered: %v", offered)
	}
	if offered["shell"] {
		t.Fatalf("F1 deny-beats-allow: denied tool 'shell' is still offered to the grouped agent "+
			"(groups config section / internal/groups resolver absent); offered: %v", offered)
	}
	if strings.Contains(toolResultsJoined(s.Fake), "AGROUPS-F1-MARKER") {
		t.Fatalf("F1 deny-beats-allow: the denied shell command EXECUTED (marker present in tool results):\n%s",
			tail(toolResultsJoined(s.Fake), 800))
	}
}

// ---------------------------------------------------------------------------
// agent-groups-02 (F2): ungrouped backward compat
// ---------------------------------------------------------------------------

// TestAgentGroupsF2UngroupedBackwardCompat pins D7: with NO groups config and
// NO groups frontmatter, an agent keeps today's exact behavior — spec grants
// drive the tool set, artifacts land. The RED anchor is the managed-default
// knob: `security.groups.default_managed` must be resolvable (default false)
// through the live config surface, which fails until the groups config
// section exists.
func TestAgentGroupsF2UngroupedBackwardCompat(t *testing.T) {
	s := harness.Start(t) // no groups section anywhere
	s.RegisterProject(t, "e2e-project")
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned code"}`)
	sessionID := s.CreateSession(t, "ag-f2", s.ProjectDir)

	artifact := filepath.Join(s.ProjectDir, "f2.txt")
	s.Fake.SetPostToolText("Wrote the backward-compat artifact.")
	s.Fake.EnqueueFileWrite("call-f2", artifact, "backward compat")
	_ = s.ChatTurn(t, sessionID, "Create the file f2.txt containing: backward compat", 120*time.Second)

	if data, err := os.ReadFile(artifact); err != nil || strings.TrimSpace(string(data)) != "backward compat" {
		t.Fatalf("F2 backward compat: ungrouped agent lost its spec-granted file_write (err=%v, data=%q)",
			err, string(data))
	}
	if !offeredToolNames(s.Fake)["shell"] {
		t.Fatalf("F2 backward compat: ungrouped coder lost its spec-granted shell tool")
	}

	stdout, stderr := s.RunCLI(t, 30*time.Second, true, "config", "get", "security.groups.default_managed")
	if !strings.Contains(stdout+stderr, "false") {
		t.Fatalf("F2: security.groups.default_managed is not resolvable through the live config surface "+
			"(groups config section absent); it must exist and default to false.\nstdout: %s\nstderr: %s",
			stdout, stderr)
	}
}

// ---------------------------------------------------------------------------
// agent-groups-03 (F3): assignment-time validation
// ---------------------------------------------------------------------------

// TestAgentGroupsF3AssignmentValidationRejectsUnknownTool pins C6: a policy
// whose tool names do not resolve in the LIVE registry is rejected at
// groups.assign time with an error naming the offending tool. RED today: the
// groups.assign RPC does not exist (method not found).
func TestAgentGroupsF3AssignmentValidationRejectsUnknownTool(t *testing.T) {
	s := harness.Start(t)
	rpc := harness.DialRPC(t, s.SocketPath)
	resp := rpc.CallError("groups.assign", map[string]any{
		"agent_id": "coder",
		"policy": map[string]any{
			"name":          "probe_policy",
			"tools_allowed": []string{"file_read", "definitely_not_a_tool"},
		},
	})
	msg := rpcErrorMessage(t, "groups.assign", resp)
	if strings.Contains(msg, "method not found") {
		t.Fatalf("F3: groups.assign RPC is absent (missing implementation): %s", msg)
	}
	if !strings.Contains(msg, "definitely_not_a_tool") {
		t.Fatalf("F3: assignment validation must REJECT and NAME the unregistered tool; error was: %s", msg)
	}
}

// ---------------------------------------------------------------------------
// agent-groups-04 (F4): membership mutation gate
// ---------------------------------------------------------------------------

// TestAgentGroupsF4MutationGateDeniesNonPrivileged pins C5/D2: a non-admin
// agent cannot mutate membership. Two legs, both through live surfaces:
//
//  1. File backstop: with default_managed=true the users store is a blocked
//     path for non-admin agents — a spec-ALLOWED file_write to
//     $MEEPT_HOME/users.json5 must still be denied (isolates the backstop:
//     the policy itself permits file_write).
//  2. RPC gate: groups.assign from a caller that is neither the agent-update
//     agent nor a groups-admin human key is denied at the seam.
//
// RED today: leg 1 succeeds (no backstop); leg 2 is method-not-found.
func TestAgentGroupsF4MutationGateDeniesNonPrivileged(t *testing.T) {
	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			// aastub is NOT in security.groups.admin_agents (default
			// ["agent-update"]): it holds a plain file_write grant only.
			seedAgent(t, st, "aastub", "code", []string{"file_write"}, nil)
			return nil
		}),
		harness.WithConfigOverlay(map[string]any{
			"groups": groupsSection(
				[]map[string]any{policy("fw", []string{"file_write"}, nil, nil)},
				[]map[string]any{binding("agent:aastub", "fw")},
				true, // default_managed: the BlockedPaths backstop is armed
			),
		}),
	)
	s.RegisterProject(t, "e2e-project")
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned code"}`)
	sessionID := s.CreateSession(t, "ag-f4", s.ProjectDir)

	target := filepath.Join(s.MeeptHome, "users.json5")
	s.Fake.SetPostToolText("Roster write attempted.")
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "file_write",
		Arguments: `{"path":"` + target + `","content":"hacked by aastub","direct":true}`,
	})
	_ = s.ChatTurn(t, sessionID, "Write the membership roster into users.json5", 120*time.Second)

	// Leg 1: the file backstop.
	if data, err := os.ReadFile(target); err == nil && strings.Contains(string(data), "hacked by aastub") {
		t.Fatalf("F4 mutation gate: a non-privileged agent WROTE the users store "+
			"(groups BlockedPaths backstop absent): %s", target)
	}
	results := strings.ToLower(toolResultsJoined(s.Fake))
	if !strings.Contains(results, "denied") && !strings.Contains(results, "refus") && !strings.Contains(results, "not allowed") {
		t.Fatalf("F4 mutation gate: the users-store write was not denied through the live tool surface; results:\n%s",
			tail(toolResultsJoined(s.Fake), 800))
	}

	// Leg 2: the RPC mutation gate.
	rpc := harness.DialRPC(t, s.SocketPath)
	resp := rpc.CallError("groups.assign", map[string]any{
		"agent_id": "coder",
		"policy": map[string]any{
			"name":          "p2",
			"tools_allowed": []string{"file_read"},
		},
	})
	msg := rpcErrorMessage(t, "groups.assign", resp)
	if strings.Contains(msg, "method not found") {
		t.Fatalf("F4: groups.assign RPC is absent (missing implementation): %s", msg)
	}
	low := strings.ToLower(msg)
	if !strings.Contains(low, "denied") && !strings.Contains(low, "permission") && !strings.Contains(low, "admin") {
		t.Fatalf("F4: groups.assign from a non-privileged caller must be denied at the seam; error was: %s", msg)
	}
}

// ---------------------------------------------------------------------------
// agent-groups-05 (F6): fence stage (dead-engine absorption, C1)
// ---------------------------------------------------------------------------

// TestAgentGroupsF6FenceStageDeniesOutsideRoot pins C1: the group policy's
// fs_roots must fence file-writing tools CENTRALLY via the FenceStage in
// pkg/security.PermissionChecker — not through the per-tool SetFenceChecker
// opt-in wiring this scenario deliberately leaves disabled (the harness
// boots with fence_enabled=false, so no tool fences today).
//
// Probe: aastub's policy allows file_write with fs_roots=[ProjectDir]. A
// write inside the root succeeds; a write to the work root OUTSIDE the fence
// root (but inside allowed_paths, so path rules do not explain a denial)
// must be denied with a fence-shaped reason and never land on disk.
//
// RED today: the outside write succeeds — no fence stage exists.
func TestAgentGroupsF6FenceStageDeniesOutsideRoot(t *testing.T) {
	// Re-armed by leaf 04-tool-gates: the group fs_roots now arm the
	// central FenceStage (pkg/security), so this gate runs for real.
	var fsRoots []string
	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			fsRoots = []string{st.ProjectDir}
			seedAgent(t, st, "aastub", "code", []string{"file_write"}, []string{"coders"})
			return nil
		}),
		harness.WithConfigHook(func(cfg map[string]any) {
			cfg["groups"] = groupsSection(
				[]map[string]any{policy("coders", []string{"file_write"}, nil, map[string]any{"fs_roots": fsRoots})},
				[]map[string]any{binding("group:coders", "coders")},
				false,
			)
		}),
	)
	s.RegisterProject(t, "e2e-project")
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned code"}`)
	sessionID := s.CreateSession(t, "ag-f6", s.ProjectDir)

	inside := filepath.Join(s.ProjectDir, "inside.txt")
	outside := filepath.Join(s.Work, "escape.txt")
	s.Fake.SetPostToolText("Fence probes complete.")
	s.Fake.EnqueueToolCalls(
		harness.ToolCall{Name: "file_write", Arguments: `{"path":"` + inside + `","content":"in","direct":true}`},
		harness.ToolCall{Name: "file_write", Arguments: `{"path":"` + outside + `","content":"out","direct":true}`},
	)
	_ = s.ChatTurn(t, sessionID, "Write inside.txt in the project, then write escape.txt into the work root", 120*time.Second)

	if data, err := os.ReadFile(inside); err != nil || strings.TrimSpace(string(data)) != "in" {
		t.Fatalf("F6: the in-root write must succeed (err=%v, data=%q)", err, string(data))
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatalf("F6 fence stage: a write OUTSIDE the group fence root landed on disk "+
			"(no central FenceStage; per-tool fence wiring is disabled in this fixture): %s", outside)
	}
	low := strings.ToLower(toolResultsJoined(s.Fake))
	if !strings.Contains(low, "fence") && !strings.Contains(low, "outside") {
		t.Fatalf("F6: the outside-root write was not denied with a fence-shaped reason; results:\n%s",
			tail(toolResultsJoined(s.Fake), 800))
	}
}

// ---------------------------------------------------------------------------
// agent-groups-06 (F7): decision audit log (dead-engine absorption, C2)
// ---------------------------------------------------------------------------

// TestAgentGroupsF7DecisionLogRecordsGroupDeny pins C2: every
// checkPermission verdict is logged with rule_source; a group-deny deny
// carries rule_source=group_deny (denies at WARN). The line must appear in
// the live daemon log after a denied tool call.
//
// RED today: no groups resolution, no decision audit — the line never
// appears (and the shell call executes instead of being denied).
func TestAgentGroupsF7DecisionLogRecordsGroupDeny(t *testing.T) {
	// Re-armed by leaf 04-tool-gates: the group gate stamps
	// rule_source=group_deny on the C2 decision line, so this gate runs
	// for real.
	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			seedAgent(t, st, "aastub", "code", []string{"file_read", "shell_execute"}, []string{"coders"})
			return nil
		}),
		harness.WithConfigOverlay(map[string]any{
			"groups": groupsSection(
				[]map[string]any{policy("coders", []string{"file_read"}, []string{"shell"}, nil)},
				[]map[string]any{binding("group:coders", "coders")},
				false,
			),
		}),
	)
	s.RegisterProject(t, "e2e-project")
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned code"}`)
	sessionID := s.CreateSession(t, "ag-f7", s.ProjectDir)

	s.Fake.SetPostToolText("Decision-log probe complete.")
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "shell",
		Arguments: `{"command":"echo AGROUPS-F7-MARKER"}`,
	})
	_ = s.ChatTurn(t, sessionID, "Run echo AGROUPS-F7-MARKER through the shell tool", 120*time.Second)

	line := waitForLogLine(t, s, "a rule_source=group_deny decision line", 20*time.Second,
		func(l string) bool { return strings.Contains(l, "rule_source=group_deny") })
	if !strings.Contains(line, "decision=deny") {
		t.Fatalf("F7: group_deny decision line does not carry decision=deny: %s", line)
	}
	if !strings.Contains(line, "shell") {
		t.Fatalf("F7: group_deny decision line does not name the denied tool: %s", line)
	}
}
