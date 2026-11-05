//go:build e2e

// Suite tool-surface: the 04-tool-surface branch e2e gate (T5 row of
// docs/plans/20261102-dsh-convergence/04-tool-surface/00T-tests.md).
// Hermetic scratch-MEEPT_HOME daemon; asserts through the live RPC surface.
// Parked as .staged until the branch's implementation leaves land (voice
// precedent 537c92fd): activation is mv *_test.go.staged -> *_test.go plus
// the e2e/manifest.json entries the orchestrator adds at activation time.
//
// Coverage:
//   tool-surface-01  F9  declared-timeout tool -> TOOL_TIMEOUT result (live executor)
//   tool-surface-02  F10 lsp_navigate goToDefinition against a real LSP server
//   tool-surface-03  F11 a skill added under MEEPT_HOME/skills appears WITHOUT restart
//   tool-surface-04  F11 the catalog survives a daemon restart (durable, digest-equal)
package tool_surface

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"agent.tool_timeout_probe_enabled": true,
	}))

	sessionID := s.CreateSession(t, "timeout-e2e", s.ProjectDir)
	reply := s.ChatTurn(t, sessionID,
		"Call the timeout_probe tool with sleep_ms 5000 exactly once and report its result.",
		2*time.Minute)

	if !strings.Contains(reply, "TOOL_TIMEOUT") &&
		!strings.Contains(reply, "timed out after") {
		t.Fatalf("no TOOL_TIMEOUT surfaced in the turn reply: %s", reply)
	}
	if strings.Contains(reply, "context deadline exceeded") {
		t.Errorf("raw context.DeadlineExceeded leaked into the model-facing reply: %s", reply)
	}
}

// TestToolSurfaceF10_LSPNavigate: lsp_navigate goToDefinition against a
// real language server returns a location. SKIPs when gopls is absent
// (leaf note: hermetic CI runners may lack it).
func TestToolSurfaceF10_LSPNavigate(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not in harness PATH; leaf pins SKIP-not-fail for an absent LSP server")
	}

	s := harness.Start(t)

	// A tiny Go module the LSP server can index.
	writeFile(t, filepath.Join(s.ProjectDir, "go.mod"), "module lspprobe\n\ngo 1.24\n")
	writeFile(t, filepath.Join(s.ProjectDir, "main.go"),
		"package main\n\nfunc helper() int { return 1 }\n\nfunc main() { _ = helper() }\n")

	sessionID := s.CreateSession(t, "lsp-e2e", s.ProjectDir)
	reply := s.ChatTurn(t, sessionID,
		"Use lsp_navigate with operation goToDefinition, file_path main.go, line 5, character 10 and report the raw result.",
		2*time.Minute)

	if !strings.Contains(reply, "main.go") {
		t.Fatalf("goToDefinition returned no location in main.go: %s", reply)
	}
}

// TestToolSurfaceF11_HotReload: a skill file added to the sandbox
// MEEPT_HOME skills dir appears in skills.list WITHOUT a daemon restart
// (fsnotify watcher -> re-Discover -> SetIndex, contract B3).
func TestToolSurfaceF11_HotReload(t *testing.T) {
	s := harness.Start(t)

	// MeeptHome is Home/.meept; the user skills root is <MEEPT_HOME>/skills.
	skillsDir := filepath.Join(s.MeeptHome, "skills", "hot-e2e-skill")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(skillsDir, "SKILL.md"),
		"---\nname: hot-e2e-skill\ndescription: added while the daemon runs\n---\nhot body\n")

	rpc := harness.DialRPC(t, s.SocketPath)
	deadline := time.Now().Add(10 * time.Second)
	for {
		res := rpc.CallResult("skills.list", map[string]any{})
		body, _ := json.Marshal(res)
		if strings.Contains(string(body), "hot-e2e-skill") {
			return // appeared without a restart
		}
		if time.Now().After(deadline) {
			t.Fatalf("hot-added skill never appeared in skills.list: %s", body)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// TestToolSurfaceF11_CatalogSurvivesRestart: the durable catalog is
// reconstructable after a daemon restart - skills.list over the SAME
// sandbox home returns the same visible set (digest equality is asserted
// by the unit gate T3b; here the live surface must not lose the skill).
func TestToolSurfaceF11_CatalogSurvivesRestart(t *testing.T) {
	s := harness.Start(t)

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

// listSkillNames polls skills.list until want appears (or timeout) and
// returns the sorted name list as a single string.
func listSkillNames(t *testing.T, rpc *harness.RPCClient, want string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		res := rpc.CallResult("skills.list", map[string]any{})
		body, _ := json.Marshal(res)
		got := string(body)
		if strings.Contains(got, want) {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("skill %q never appeared in skills.list: %s", want, got)
		}
		time.Sleep(250 * time.Millisecond)
	}
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
