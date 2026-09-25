//go:build e2e

// Package toolsfilesystem drives the filesystem tool family through REAL
// agent turns (the fake LLM emits scripted tool calls; the daemon executes
// the real tools) and asserts observable end state: files on disk, tool-result
// text carried back into the follow-up LLM request, and safety refusals.
//
// Coverage map (manifest scenarios):
//
//	tools-filesystem-01  file_write relative to session workdir   — TestFileWriteCreatesArtifactInSessionWorkdir
//	tools-filesystem-02  file_read hashline output                — TestFileReadReturnsHashlineAnchors (file_edit leg deferred, see TestFileEditStaleAnchorFlow)
//	tools-filesystem-03  file_edit stale anchor                   — deferred (t.Skip; no roster agent grants file_edit)
//	tools-filesystem-04  file_grep/file_find relative workdir     — TestFileGrepAndFindResolveAgainstSessionWorkdir
//	tools-filesystem-05  unbound-session ErrNoWorkingDir sentinel — deferred (t.Skip; schema gate shadows the sentinel)
//	tools-filesystem-06  shell runs in the session workdir        — TestShellRunsRealCommandInWorkdir
//	tools-filesystem-07  shell refuses destructive command        — TestShellRefusesDestructiveCommand
package toolsfilesystem

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
// has ever sent back to the fake LLM. Tool-result envelopes are the observable
// end state of a tool call: they are what the model actually sees.
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

// mkStack boots a stack, registers the project, and binds a code-intent
// session to the project dir (the coder lane holds the filesystem grants).
func mkStack(t *testing.T, name string) *harness.Stack {
	t.Helper()
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	_ = s.CreateSession(t, name, s.ProjectDir)
	return s
}

// chatCode sends a turn through the project-bound session of the stack. The
// message must read as an imperative file request so the fake classifier
// heuristic pins intent=code (coder lane holds file_write/file_read/shell).
func chatCode(t *testing.T, s *harness.Stack, sessionID, msg string) string {
	t.Helper()
	return s.ChatTurn(t, sessionID, msg, 120*time.Second)
}

// startWithAgentGrants boots a sandbox whose roster additionally contains a
// custom user-tier agent ("aastub") that owns the given lane and holds the
// given tool grants. "aastub" sorts before every bundled specialist that
// shares the lane (coder, analyst, librarian, ...), so the lane routing
// index — first sorted spec declaring the lane wins — routes pinned turns to
// it. Must be called INSIDE a WithPreBootHook so the file exists before the
// roster loads at boot.
func seedGrantAgent(t *testing.T, st *harness.Stack, grants []string) {
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
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write aastub AGENT.md: %v", err)
	}
}

// startWithAgentGrants boots a stack whose roster includes the aastub agent
// carrying the given grants, registers the project, and binds a session.
func startWithAgentGrants(t *testing.T, name string, grants []string) *harness.Stack {
	t.Helper()
	s := harness.Start(t, harness.WithPreBootHook(func(st *harness.Stack) error {
		seedGrantAgent(t, st, grants)
		return nil
	}))
	s.RegisterProject(t, "e2e-project")
	return s
}

// tools-filesystem-01: file_write writes a real file; the artifact lands in
// the registered project dir with the scripted content, and the task store
// records a terminal step. (Absolute path: the dispatched step-job path does
// not inject the session working dir into the tool context, so relative
// paths resolve to the daemon cwd — the workdir-relative behavior is covered
// by the explore-lane grep/find test and the shell working_dir test.)
func TestFileWriteCreatesArtifactInSessionWorkdir(t *testing.T) {
	s := mkStack(t, "fs-write")
	sessionID := s.CreateSession(t, "fs-write", s.ProjectDir)

	artifact := filepath.Join(s.ProjectDir, "note.txt")
	s.Fake.SetPostToolText("Wrote the file " + artifact + " for you.")
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "file_write",
		Arguments: `{"path":"` + artifact + `","content":"written by e2e","direct":true}`,
	})

	chatCode(t, s, sessionID, "Create a file named fs-write/note.txt containing the line written by e2e")

	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatalf("artifact %s not created: %v\ndaemon log tail:\n%s", artifact, err, s.Daemon.LogTail())
	}
	if got := strings.TrimSpace(string(data)); got != "written by e2e" {
		t.Fatalf("artifact content = %q, want %q", got, "written by e2e")
	}

	// The tool-result evidence text the model saw confirms the write.
	if res := toolResultsJoined(s.Fake); !strings.Contains(res, "Successfully wrote") {
		t.Fatalf("tool results missing write confirmation:\n%s", res)
	}

	// Terminal step recorded in tasks.db.
	found := false
	for _, task := range harness.Tasks(t, s.TasksDBPath()) {
		for _, step := range harness.Steps(t, s.TasksDBPath(), task.ID) {
			if step.State == "completed" || step.State == "approved" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("no completed step in the task store after file_write turn")
	}
}

// tools-filesystem-02 (read leg): file_read returns hashline-tagged content —
// the LINE:TAG:HASH| anchors file_edit consumes. The file_edit half of the
// scenario is deferred (see TestFileEditStaleAnchorFlow).
func TestFileReadReturnsHashlineAnchors(t *testing.T) {
	s := mkStack(t, "fs-read")
	sessionID := s.CreateSession(t, "fs-read", s.ProjectDir)

	pre := filepath.Join(s.ProjectDir, "anchor.txt")
	if err := os.WriteFile(pre, []byte("first line\nsecond line\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "file_read",
		Arguments: `{"path":"` + pre + `"}`,
	})

	chatCode(t, s, sessionID, "Create a summary doc after you read the file anchor.txt and report its lines")

	res := toolResultsJoined(s.Fake)
	// Hashline format: LINE:TAG:HASH|content (tag is 4 hex chars, hash 2 letters).
	if !strings.Contains(res, "1:") || !strings.Contains(res, "|first line") {
		t.Fatalf("file_read result missing hashline anchors for line 1:\n%s", res)
	}
	if !strings.Contains(res, "2:") || !strings.Contains(res, "|second line") {
		t.Fatalf("file_read result missing hashline anchors for line 2:\n%s", res)
	}
}

// tools-filesystem-03: file_edit stale-anchor rejection. The e2e sandbox
// extends the roster with a custom user-tier agent ("aastub", alphabetically
// first holder of the code lane) that grants file_edit — no internal/ change
// needed, just roster seeding via the harness pre-boot hook.
func TestFileEditStaleAnchorFlow(t *testing.T) {
	s := startWithAgentGrants(t, "fs-edit", []string{"file_edit", "file_read"})
	sessionID := s.CreateSession(t, "fs-edit", s.ProjectDir)

	target := filepath.Join(s.ProjectDir, "editme.txt")
	if err := os.WriteFile(target, []byte("first line\nsecond line\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	// No file_read first ⇒ no read-cache snapshot, so stale-anchor recovery
	// cannot kick in: the anchor mismatch must produce the clean rejection
	// with fresh hashline content for a retry.
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "file_edit",
		Arguments: `{"path":"` + target + `","edits":[{"op":"replace","anchor":"1:zz","content":"hacked line"}]}`,
	})

	chatCode(t, s, sessionID, "Create a summary of editme.txt after editing it with the file_edit tool")

	res := toolResultsJoined(s.Fake)
	if !strings.Contains(res, "Edit rejected") || !strings.Contains(res, "do not match the current file") {
		t.Fatalf("stale anchor not rejected with the retry contract; results:\n%s", res)
	}
	if !strings.Contains(res, "|second line") {
		t.Fatalf("rejection does not carry fresh hashline content for retry:\n%s", res)
	}
	// The edit was NOT applied.
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("target vanished: %v", err)
	}
	if strings.Contains(string(data), "hacked line") {
		t.Fatalf("rejected edit was applied anyway:\n%s", data)
	}
}

// tools-filesystem-04: file_grep and file_find resolve their default search
// root (".") against the session working dir, not the daemon's process cwd.
// The explore agent holds the file_grep/file_find grants, so the classifier is
// pinned to intent=explore.
func TestFileGrepAndFindResolveAgainstSessionWorkdir(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "fs-search", s.ProjectDir)

	if err := os.WriteFile(filepath.Join(s.ProjectDir, "haystack.txt"), []byte("the zebra codebar hides here\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	s.Fake.SetClassifierOutput(`{"intent":"explore","confidence":0.95,"reasoning":"pinned explore"}`)
	s.Fake.EnqueueToolCalls(
		harness.ToolCall{
			Name:      "file_grep",
			Arguments: `{"pattern":"zebra","output_mode":"content"}`,
		},
		harness.ToolCall{
			Name:      "file_find",
			Arguments: `{"pattern":"haystack.txt"}`,
		},
	)

	s.ChatTurn(t, sessionID, "Search the project for the zebra pattern and list where the haystack file lives", 120*time.Second)

	res := toolResultsJoined(s.Fake)
	if !strings.Contains(res, "zebra codebar") {
		t.Fatalf("file_grep result missing the seeded match; results:\n%s", res)
	}
	if !strings.Contains(res, "haystack.txt") {
		t.Fatalf("file_find result missing the seeded file; results:\n%s", res)
	}
}

// tools-filesystem-05: unbound session surfaces the ErrNoWorkingDir sentinel.
// DEFERRED: through the real agent path the registry-level schema gate fires
// first — list_directory declares path as required, so a path-less scripted
// call is rejected with invalid_args before the tool can return the sentinel.
// The sentinel remains a unit-level contract (internal/tools/workdir_ctx.go).
func TestUnboundSessionNoWorkingDirSentinel(t *testing.T) {
	t.Skip("deferred: the registry schema gate rejects path-less list_directory with " +
		"invalid_args before tools.NoWorkingDirError can fire; the ErrNoWorkingDir " +
		"sentinel is only reachable when the schema gate is bypassed (unit-level contract).")
}

// tools-filesystem-06: shell runs a real command inside the session workdir —
// the redirected output file lands in the project dir and stdout comes back.
func TestShellRunsRealCommandInWorkdir(t *testing.T) {
	s := mkStack(t, "fs-shell")
	sessionID := s.CreateSession(t, "fs-shell", s.ProjectDir)

	outFile := filepath.Join(s.ProjectDir, "shell-e2e.txt")
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name: "shell",
		Arguments: `{"command":"printf shell-ran-here > shell-e2e.txt","working_dir":"` +
			s.ProjectDir + `","timeout":15}`,
	})

	chatCode(t, s, sessionID, "Create a report file named shell-e2e.txt by running a shell printf command in the project")

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("shell output file %s not created: %v\ntool results:\n%s\ndaemon log tail:\n%s",
			outFile, err, toolResultsJoined(s.Fake), s.Daemon.LogTail())
	}
	if got := strings.TrimSpace(string(data)); got != "shell-ran-here" {
		t.Fatalf("shell output = %q, want %q", got, "shell-ran-here")
	}
	if res := toolResultsJoined(s.Fake); !strings.Contains(res, "shell-ran-here") {
		t.Fatalf("shell stdout not carried in the tool result:\n%s", res)
	}
}

// tools-filesystem-07: shell refuses a destructive command. `rm -rf` elevates
// to HIGH risk; with require_confirmation_high the executor denies the call
// with the confirm-gate message and the target file survives.
func TestShellRefusesDestructiveCommand(t *testing.T) {
	s := mkStack(t, "fs-rm")
	sessionID := s.CreateSession(t, "fs-rm", s.ProjectDir)

	victim := filepath.Join(s.ProjectDir, "precious.txt")
	if err := os.WriteFile(victim, []byte("still here"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name: "shell",
		Arguments: `{"command":"rm -rf precious.txt","working_dir":"` +
			s.ProjectDir + `","timeout":15}`,
	})

	chatCode(t, s, sessionID, "Create a clean report by first running a shell command to rm -rf precious.txt in the project")

	res := toolResultsJoined(s.Fake)
	if !strings.Contains(res, "requires user confirmation") {
		t.Fatalf("destructive command not refused with the confirm gate; results:\n%s", res)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("victim file was deleted despite the refusal: %v", err)
	}

	// The refusal must NOT be a success envelope.
	for _, raw := range toolResultTexts(s.Fake) {
		var env struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal([]byte(raw), &env); err == nil && env.Error != "" {
			if env.Success {
				t.Fatalf("refusal carried a success envelope: %s", raw)
			}
			if !strings.Contains(env.Error, "confirmation") {
				t.Fatalf("refusal error text unexpected: %s", raw)
			}
		}
	}
}
