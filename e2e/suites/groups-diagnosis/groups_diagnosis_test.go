//go:build e2e

// Suite groups-diagnosis: T2 of the agent-groups plan test gate
// (docs/plans/20261030-agent-groups/00T-e2e-agent-groups.md). RED today:
// the `meept groups explain|check|list` CLI family (cmd/meept/groups.go,
// leaf 06) and the groups.* RPC surface do not exist, so every assertion
// here fails on the missing-implementation anchor.
//
// Coverage map (manifest scenarios):
//
//	groups-diagnosis-01  F8 explain names binding+policy — TestGroupsDiagnosisF8ExplainNamesBindingAndPolicy
//	groups-diagnosis-02  F8 check reports injected defects — TestGroupsDiagnosisF8CheckReportsInjectedDefects
//	groups-diagnosis-03  F8 explain/list RPC surface     — TestGroupsDiagnosisF8ExplainRPCSurface
package groupsdiagnosis

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// seedCoder writes the C3-fixture agent: a grouped `coder`-like specialist
// whose policy (bound via group:coders) grants file_read and shell. The
// `groups:` frontmatter membership is the C4 surface.
func seedCoder(t *testing.T, st *harness.Stack) {
	t.Helper()
	dir := filepath.Join(st.MeeptHome, "agents", "aastub")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir aastub agent dir: %v", err)
	}
	body := `---
id: aastub
name: E2E Coder Fixture
role: executor
description: groups-diagnosis fixture agent
intents: [code]
groups: [coders]
enabled: true
can_delegate: false
additional_tools:
  - file_read
  - shell_execute
capabilities:
  - reasoning
max_iterations: 8
timeout_seconds: 120
---

Do exactly what the user asks with the tools you have.
`
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write aastub AGENT.md: %v", err)
	}
}

// c3Config is the C3 fixture config: one policy (coders: allow file_read +
// shell, deny web_fetch), one group binding, one agent membership via
// frontmatter.
func c3Config() map[string]any {
	return map[string]any{
		"groups": map[string]any{
			"policies": []map[string]any{
				{
					"name":          "coders",
					"tools_allowed": []string{"file_read", "shell"},
					"tools_denied":  []string{"web_fetch"},
				},
			},
			"bindings": []map[string]any{
				{"subject": "group:coders", "policy": "coders", "scope": "daemon"},
			},
			"default_managed":    false,
			"decision_log_level": "deny-warn",
		},
	}
}

// groupsCLIRan reports whether the combined CLI output looks like a real
// groups diagnosis (as opposed to the root command's connect/usage error
// a missing subcommand produces).
func groupsCLIRan(combined string) bool {
	low := strings.ToLower(combined)
	for _, marker := range []string{"policy", "binding", "group:", "rule_source", "\"grants\"", "\"defects\""} {
		if strings.Contains(low, marker) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// groups-diagnosis-01 (F8 explain)
// ---------------------------------------------------------------------------

// TestGroupsDiagnosisF8ExplainNamesBindingAndPolicy pins F8/leaf-06: given
// the C3 fixture config, `meept groups explain <agent>` names, per grant,
// the binding and the policy that produced it — the shell grant must be
// attributed to the coders policy via the group:coders binding.
//
// RED today: the meept groups subcommand family is absent.
func TestGroupsDiagnosisF8ExplainNamesBindingAndPolicy(t *testing.T) {
	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			seedCoder(t, st)
			return nil
		}),
		harness.WithConfigOverlay(c3Config()),
	)

	stdout, stderr := s.RunCLI(t, 30*time.Second, true, "groups", "explain", "aastub")
	if !groupsCLIRan(stdout + stderr) {
		t.Fatalf("F8 explain: `meept groups explain aastub` produced no groups-diagnosis output "+
			"(CLI subcommand absent — cmd/meept/groups.go missing);\nstdout: %s\nstderr: %s",
			tailOut(stdout), tailOut(stderr))
	}
	low := strings.ToLower(stdout + stderr)
	for _, want := range []string{"coders", "group:coders", "shell"} {
		if !strings.Contains(low, want) {
			t.Fatalf("F8 explain: output does not name %q (binding+policy attribution required);\nstdout: %s\nstderr: %s",
				want, tailOut(stdout), tailOut(stderr))
		}
	}
}

// ---------------------------------------------------------------------------
// groups-diagnosis-02 (F8 check)
// ---------------------------------------------------------------------------

// TestGroupsDiagnosisF8CheckReportsInjectedDefects pins F8/leaf-06 R5: given
// a config with injected defects (an unknown group in agent frontmatter, a
// dangling policy reference, an empty-allowlist managed policy),
// `meept groups check` exits non-zero and REPORTS each defect by name.
//
// RED today: the CLI subcommand is absent.
func TestGroupsDiagnosisF8CheckReportsInjectedDefects(t *testing.T) {
	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			seedCoder(t, st)
			// agent with an UNKNOWN group in frontmatter (C4 defect class).
			dir := filepath.Join(st.MeeptHome, "agents", "zzdefect")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir zzdefect: %v", err)
			}
			body := "---\nid: zzdefect\nname: Defect Fixture\nrole: executor\nintents: [debug]\n" +
				"groups: [ghost-group]\nenabled: true\ncan_delegate: false\n---\n\nDefect fixture.\n"
			return os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(body), 0o644)
		}),
		harness.WithConfigOverlay(map[string]any{
			"groups": map[string]any{
				"policies": []map[string]any{
					// dangling ref: bound policy does not exist; plus a
					// managed empty-allowlist policy (deny-everything surprise).
					{"name": "empty_managed", "tools_allowed": []string{}},
				},
				"bindings": []map[string]any{
					{"subject": "group:coders", "policy": "missing_policy", "scope": "daemon"},
				},
				"default_managed":    true,
				"decision_log_level": "deny-warn",
			},
		}),
	)

	stdout, stderr := s.RunCLI(t, 30*time.Second, true, "groups", "check", "--json")
	if !groupsCLIRan(stdout + stderr) {
		t.Fatalf("F8 check: `meept groups check --json` produced no groups-diagnosis output "+
			"(CLI subcommand absent);\nstdout: %s\nstderr: %s", tailOut(stdout), tailOut(stderr))
	}
	combined := strings.ToLower(stdout + stderr)
	for _, defect := range []string{"ghost-group", "missing_policy", "empty_managed"} {
		if !strings.Contains(combined, defect) {
			t.Fatalf("F8 check: injected defect %q not reported (R5 class missing);\nstdout: %s\nstderr: %s",
				defect, tailOut(stdout), tailOut(stderr))
		}
	}
}

// ---------------------------------------------------------------------------
// groups-diagnosis-03 (F8 RPC surface)
// ---------------------------------------------------------------------------

// TestGroupsDiagnosisF8ExplainRPCSurface pins the C5 RPC anchor: the
// groups.explain RPC exists and answers for a known agent with the same
// resolution the gates enforce. RED today: method not found.
func TestGroupsDiagnosisF8ExplainRPCSurface(t *testing.T) {
	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			seedCoder(t, st)
			return nil
		}),
		harness.WithConfigOverlay(c3Config()),
	)

	rpc := harness.DialRPC(t, s.SocketPath)
	resp := rpc.CallError("groups.explain", map[string]any{"agent_id": "aastub"})
	e, ok := resp["error"]
	if ok && e != nil {
		msg := ""
		if em, ok := e.(map[string]any); ok {
			if m, ok := em["message"].(string); ok {
				msg = m
			}
		}
		if strings.Contains(strings.ToLower(msg), "method not found") {
			t.Fatalf("F8: groups.explain RPC is absent (missing implementation): %s", msg)
		}
		t.Fatalf("F8: groups.explain exists but errored (unexpected at this gate stage): %v", e)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("F8: groups.explain result is not an object: %v", resp["result"])
	}
	for _, key := range []string{"agent_id", "bindings", "policy"} {
		if _, ok := result[key]; !ok {
			t.Fatalf("F8: groups.explain result missing stable key %q: %v", key, result)
		}
	}
}

// tailOut returns the tail of a CLI output for error messages.
func tailOut(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 400 {
		return s
	}
	return "..." + s[len(s)-400:]
}
