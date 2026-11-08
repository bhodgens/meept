//go:build e2e

// Suite tool-surface: the 04-tool-surface branch e2e gate (T5 row of
// docs/plans/20261102-dsh-convergence/04-tool-surface/00T-tests.md).
// Hermetic scratch-MEEPT_HOME daemon; asserts through the live RPC surface.
// Parked as .staged until the branch's implementation leaves land (voice
// precedent 537c92fd): activation is mv *_test.go.staged -> *_test.go plus
// the e2e/manifest.json entries the orchestrator adds at activation time.
//
// Coverage:
//
//	tool-surface-01  F9  declared-timeout tool -> TOOL_TIMEOUT result (live executor)
//	tool-surface-02  F10 lsp_navigate goToDefinition against a real LSP server
//	tool-surface-03  F11 a skill added under MEEPT_HOME/skills appears WITHOUT restart
//	tool-surface-04  F11 the catalog survives a daemon restart (durable, digest-equal)
package tool_surface

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// TestToolSurfaceF9_TimeoutThroughLiveExecutor: a tool with a declared
// TimeoutMs shorter than its runtime must surface the structured
// TOOL_TIMEOUT result through the live turn path, never a raw
// context.DeadlineExceeded string (contract B1 machine gear).
//
// The probe tool is gated behind a config flag the leaf 01 lands
// ([agent].tool_timeout_probe_enabled): the harness daemon registers it
// only when the flag is on, keeping production registries untouched.
func TestToolSurfaceF9_TimeoutThroughLiveExecutor(t *testing.T) {
	s := harness.Start(t,
		harness.WithConfigOverlay(map[string]any{
			"agent.tool_timeout_probe_enabled": true,
		}),
		// Route the turn to a stub specialist that holds the probe grant
		// (the bundled coder spec does not carry the e2e-only tool).
		harness.WithPreBootHook(seedGrantAgentHook(t, []string{"timeout_probe"})),
	)

	sessionID := s.CreateSession(t, "timeout-e2e", s.ProjectDir)

	// Drive the probe through the LIVE executor: enqueue the tool call so the
	// fake model invokes it, then a post-tool reply. The probe declares a
	// 1500ms cooperative budget; the 5000ms sleep makes the executor's derived
	// deadline fire and substitute the structured TOOL_TIMEOUT result (B1).
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "timeout_probe",
		Arguments: `{"sleep_ms": 5000}`,
	})
	s.Fake.SetPostToolText("the timeout_probe call reported its timeout result")

	reply := s.ChatTurn(t, sessionID,
		"Call the timeout_probe tool with sleep_ms 5000 exactly once and report its result.",
		2*time.Minute)

	// The structured TOOL_TIMEOUT is the tool RESULT the model sees (the
	// role=tool envelope), not the canned post-tool reply text — assert on
	// the envelope and gate against a raw deadline leak.
	results := toolResultsJoined(s.Fake)
	if !strings.Contains(results, "TOOL_TIMEOUT") &&
		!strings.Contains(results, "timed out after") {
		t.Fatalf("no TOOL_TIMEOUT surfaced in the tool result envelope:\nreply:  %s\nresults: %s", reply, results)
	}
	if strings.Contains(results, "context deadline exceeded") {
		t.Errorf("raw context.DeadlineExceeded leaked into the model-facing tool result: %s", results)
	}
}

// TestToolSurfaceF10_LSPNavigate: lsp_navigate goToDefinition against a
// real language server returns a location. SKIPs when gopls is absent
// (leaf note: hermetic CI runners may lack it).
func TestToolSurfaceF10_LSPNavigate(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not in harness PATH; leaf pins SKIP-not-fail for an absent LSP server")
	}

	s := harness.Start(t,
		// Enable the daemon code-intel/LSP layer and configure a gopls server
		// so lsp_navigate is registered and can spawn a real Go LSP server.
		// (gopls-absent CI + this overlay are the two SKIP-not-fail legs; the
		// harness never enables code-intel by default.)
		harness.WithConfigOverlay(map[string]any{
			"code_intel.enabled":                        true,
			"code_intel.lsp.auto_start_servers":         true,
			"code_intel.lsp.connection_timeout_seconds": 90,
			"code_intel.lsp.servers.gopls.command":      "gopls",
			"code_intel.lsp.servers.gopls.transport":    "stdio",
			"code_intel.lsp.servers.gopls.languages":    []any{"go"},
		}),
		harness.WithPreBootHook(seedGrantAgentHook(t, []string{"lsp_navigate"})),
	)

	// A tiny Go module the LSP server can index.
	mainGo := filepath.Join(s.ProjectDir, "main.go")
	writeFile(t, filepath.Join(s.ProjectDir, "go.mod"), "module lspprobe\n\ngo 1.24\n")
	writeFile(t, mainGo,
		"package main\n\nfunc helper() int { return 1 }\n\nfunc main() { _ = helper() }\n")

	sessionID := s.CreateSession(t, "lsp-e2e", s.ProjectDir)

	// Drive lsp_navigate through the live executor: goToDefinition from inside
	// the `helper()` call site (line 5, 1-based) back to its declaration.
	// The tool resolves the absolute path relative to the session workspace.
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name: "lsp_navigate",
		Arguments: fmt.Sprintf(
			`{"operation":"goToDefinition","file_path":%q,"line":5,"character":19}`,
			mainGo),
	})
	s.Fake.SetPostToolText("the lsp_navigate call returned a definition location")

	reply := s.ChatTurn(t, sessionID,
		"Use lsp_navigate with operation goToDefinition, file_path main.go, line 5, character 10 and report the raw result.",
		2*time.Minute)

	if !strings.Contains(toolResultsJoined(s.Fake), "main.go") {
		t.Fatalf("goToDefinition returned no location in main.go:\nreply:  %s\nresults: %s",
			reply, toolResultsJoined(s.Fake))
	}
}

// TestToolSurfaceF11_HotReload: a skill file added to the sandbox
// MEEPT_HOME skills dir appears in skills.list WITHOUT a daemon restart
// (fsnotify watcher -> re-Discover -> registry+index refresh, contract B3).
func TestToolSurfaceF11_HotReload(t *testing.T) {
	s := harness.Start(t, withSkillsRoot())

	// MeeptHome is Home/.meept; the user skills root is <MEEPT_HOME>/skills.
	skillsDir := filepath.Join(s.MeeptHome, "skills", "hot-e2e-skill")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(skillsDir, "SKILL.md"),
		"---\nname: hot-e2e-skill\ndescription: added while the daemon runs\n---\nhot body\n")

	rpc := harness.DialRPC(t, s.SocketPath)
	listSkillNames(t, rpc, "hot-e2e-skill", 10*time.Second) // fails the test if it never appears
}

// TestToolSurfaceF11_CatalogSurvivesRestart: the durable catalog is
// reconstructable after a daemon restart - skills.list over the SAME
// sandbox home returns the same visible name set after a process restart
// (digest equality is asserted by the unit gate T3b; here the live surface
// must not lose the skill across a restart).
func TestToolSurfaceF11_CatalogSurvivesRestart(t *testing.T) {
	s := harness.Start(t, withSkillsRoot())

	skillsDir := filepath.Join(s.MeeptHome, "skills", "durable-e2e-skill")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(skillsDir, "SKILL.md"),
		"---\nname: durable-e2e-skill\ndescription: survives restart\n---\nbody\n")

	rpc1 := harness.DialRPC(t, s.SocketPath)
	before := listSkillNames(t, rpc1, "durable-e2e-skill", 10*time.Second)

	// Restart: stop the daemon and boot a fresh one over the same sandbox
	// (the harness has no in-place restart; a second Start with the same
	// MEEPT_HOME is the process-restart analogue used by memory-persistence).
	s.Daemon.Stop()
	s2 := harness.Start(t, harness.WithPreBootHook(func(st *harness.Stack) error {
		// Point the new sandbox at the SAME home tree.
		if err := os.RemoveAll(st.MeeptHome); err != nil {
			return err
		}
		return copyDir(s.MeeptHome, st.MeeptHome)
	}))

	rpc2 := harness.DialRPC(t, s2.SocketPath)
	after := listSkillNames(t, rpc2, "durable-e2e-skill", 10*time.Second)

	if before != after {
		t.Errorf("visible skill set changed across restart:\nbefore: %s\nafter:  %s", before, after)
	}
}

// withSkillsRoot seeds the sandbox user skills root (<MEEPT_HOME>/skills)
// at boot so the daemon's hot-reload watcher registers it. In a hermetic
// sandbox the root would otherwise not exist, fsnotify never watches it,
// and a mid-run skill add could never surface (leaf 03 style: seed the
// skill dir the daemon is expected to watch).
func withSkillsRoot() harness.StartOption {
	return harness.WithPreBootHook(func(st *harness.Stack) error {
		return os.MkdirAll(filepath.Join(st.MeeptHome, "skills"), 0o755)
	})
}

// listSkillNames polls skills.list until the wanted skill appears (or the
// timeout fires, failing the test — a presence assertion, never weakened)
// and returns the visible skill names as a sorted, newline-joined string.
//
// The comparison is keyed on NAMES only: the raw skills.list JSON also
// embeds each skill's <MEEPT_HOME>-scoped absolute path, and two distinct
// sandbox homes (the restart case) can never produce byte-equal paths.
func listSkillNames(t *testing.T, rpc *harness.RPCClient, want string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		res := rpc.CallResult("skills.list", map[string]any{})
		names := resultSkillNames(res)
		joined := strings.Join(names, "\n")
		if strings.Contains(joined, want) {
			return joined
		}
		if time.Now().After(deadline) {
			t.Fatalf("skill %q never appeared in skills.list: %v", want, res)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// resultSkillNames extracts the sorted skill name list from a skills.list
// result map ({"skills":[{name,...},...],"count":N}).
func resultSkillNames(res map[string]any) []string {
	raw, ok := res["skills"].([]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if n, ok := m["name"].(string); ok && n != "" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// seedGrantAgentHook returns a pre-boot hook that seeds a custom executor
// specialist ("aastub") holding the given tool grants into the sandbox
// roster. The stub sorts before the bundled specialists sharing its lane
// (coder etc.), so an imperative code turn routes to it and its grant list
// decides which tools the real executor may invoke — the same pattern
// tools-filesystem uses to give a hermetic agent file/write grants without
// editing a production AGENT.md spec.
func seedGrantAgentHook(t *testing.T, grants []string) func(*harness.Stack) error {
	t.Helper()
	return func(st *harness.Stack) error {
		dir := filepath.Join(st.MeeptHome, "agents", "aastub")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		var tools strings.Builder
		for _, g := range grants {
			tools.WriteString("  - " + g + "\n")
		}
		body := "---\n" +
			"id: aastub\n" +
			"name: E2E Stub Specialist\n" +
			"role: executor\n" +
			"description: e2e-only agent holding extra tool grants\n" +
			"intents: [code]\n" +
			"enabled: true\n" +
			"can_delegate: false\n" +
			"additional_tools:\n" +
			tools.String() +
			"capabilities:\n  - reasoning\n" +
			"max_iterations: 8\n" +
			"timeout_seconds: 120\n" +
			"---\n\n# E2E Stub Specialist\n\nFollow the user's request briefly.\n"
		return os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(body), 0o644)
	}
}

// toolResultTexts collects the content of every role=tool message the daemon
// has sent back to the fake LLM. Tool-result envelopes are the observable end
// state of a tool call — the model-facing structured result (tools-filesystem
// pattern).
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}
