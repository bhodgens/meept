//go:build e2e

// Package smoke is the exemplar suite for the hermetic e2e harness: it
// boots the full stack (fake LLM + scratch daemon), registers a project,
// drives one chat turn that must create a real file, and asserts the
// user-facing contracts — non-stub reply, artifact on disk, completed
// step in tasks.db.
package smoke

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// TestDaemonBootsHealthy verifies the scratch daemon comes up with the
// fake LLM wired in: HTTP /health answers, the CLI can list sessions
// against the sandbox socket, and tasks.db exists in the sandbox state
// dir (never ~/.meept).
func TestDaemonBootsHealthy(t *testing.T) {
	s := harness.Start(t)

	health := s.HealthJSON(t)
	if health["status"] != "ok" {
		t.Fatalf("health status = %q, want ok", health["status"])
	}

	// CLI talks to the scratch daemon over the sandbox socket.
	out, _ := s.RunCLI(t, 30*time.Second, false, "session", "list", "--json")
	if !strings.Contains(out, "sessions") {
		t.Fatalf("session list --json output missing sessions key:\n%s", out)
	}

	if _, err := os.Stat(s.TasksDBPath()); err != nil {
		t.Fatalf("tasks.db not created in sandbox state dir: %v", err)
	}

	if s.Daemon.Pid() == 0 {
		t.Fatal("daemon pid not recorded")
	}
}

// TestChatTurnCreatesArtifact drives one full turn: submit a chat message
// asking to create a file in the registered project dir, await the
// terminal result, then assert:
//
//	A1 the reply is not the "Task ... completed." stub;
//	A4 the artifact file exists in the project dir with the right content;
//	S1 tasks.db shows a completed step for the dispatched task.
//
// The fake LLM scripts the executor turn to call the real file_write tool,
// so the daemon executes actual tools against the sandbox project dir.
func TestChatTurnCreatesArtifact(t *testing.T) {
	s := harness.Start(t)

	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "e2e-smoke", s.ProjectDir)

	// Script the executor turn: the tool-bearing request gets the
	// file_write call; the follow-up (post tool-result) gets the final
	// user-facing text naming the path.
	artifact := filepath.Join(s.ProjectDir, "hello.txt")
	s.Fake.SetPostToolText("Created hello.txt at " + artifact + " containing the word hello.")
	s.Fake.EnqueueFileWrite("call-1", artifact, "hello")

	// The turn classifies as code (imperative + artifact noun), dispatches
	// a task, and runs the scripted executor step.
	reply := s.ChatTurn(t, sessionID,
		"Create a file named hello.txt containing the word hello",
		120*time.Second)

	// A1: non-stub reply. The naive-user contract: machine-shaped output
	// ("Task ... completed.") must never become the user's reply.
	if strings.Contains(reply, "Task ") && strings.Contains(reply, "completed") {
		t.Fatalf("A1: reply is the completion stub: %q", reply)
	}
	if strings.TrimSpace(reply) == "" {
		t.Fatal("A1: reply is empty")
	}

	// A4: artifact exists in the registered project dir (not the daemon
	// cwd) with the scripted content.
	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatalf("A4: artifact %s not created: %v\nreply: %s\ndaemon log tail:\n%s",
			artifact, err, reply, s.Daemon.LogTail())
	}
	if got := strings.TrimSpace(string(data)); got != "hello" {
		t.Fatalf("A4: artifact content = %q, want %q", got, "hello")
	}

	// S1: task store shows a completed step.
	tasks := harness.Tasks(t, s.TasksDBPath())
	if len(tasks) == 0 {
		t.Fatalf("S1: no tasks recorded in %s", s.TasksDBPath())
	}
	// Terminal step states: "completed" (no review flow) or "approved"
	// (review passed) — both are successfully-terminal (internal/task/step.go
	// StepState.IsSuccessfullyTerminal).
	completedSteps := 0
	for _, task := range tasks {
		for _, step := range harness.Steps(t, s.TasksDBPath(), task.ID) {
			if step.State == "completed" || step.State == "approved" {
				completedSteps++
			}
		}
	}
	if completedSteps == 0 {
		var dump string
		for _, task := range tasks {
			dump += harness.FormatSteps(harness.Steps(t, s.TasksDBPath(), task.ID))
		}
		t.Fatalf("S1: no completed step in the task store; tasks: %+v\nsteps:\n%s", tasks, dump)
	}

	// The fake LLM actually served the executor turn (the tool call was
	// consumed).
	if s.Fake.RequestCount() == 0 {
		t.Fatal("fake LLM received no completion requests; the daemon never reached the model")
	}
}

// TestChatTurnAsyncTerminalResult exercises the async turn semantics at
// the HTTP surface: chat.submit acks immediately with a turn_id, and the
// task the turn dispatches completes in the store (the terminal event
// path), proving submit-ack and terminal-result are decoupled.
func TestChatTurnAsyncTerminalResult(t *testing.T) {
	s := harness.Start(t)

	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "e2e-async", s.ProjectDir)

	artifact := filepath.Join(s.ProjectDir, "async.txt")
	s.Fake.SetPostToolText("Wrote async.txt to " + artifact + ".")
	s.Fake.EnqueueFileWrite("call-a1", artifact, "async hello")

	ack := s.SubmitChatHTTP(t, sessionID,
		"Create a file named async.txt containing async hello")

	if accepted, _ := ack["accepted"].(bool); !accepted {
		t.Fatalf("submit not accepted: %+v", ack)
	}
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("ack missing turn_id: %+v", ack)
	}

	// The ack must arrive long before agent work finishes (async contract:
	// submit never blocks on the agent). By the time SubmitChatHTTP
	// returned, no artifact may exist yet is racy — instead assert the
	// terminal outcome arrives: the artifact lands and the task store
	// records a completed step.
	harness.WaitFor(t, 90*time.Second, "artifact from async turn", func() bool {
		_, err := os.Stat(artifact)
		return err == nil
	})

	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatalf("artifact read: %v\ndaemon log tail:\n%s", err, s.Daemon.LogTail())
	}
	if got := strings.TrimSpace(string(data)); got != "async hello" {
		t.Fatalf("artifact content = %q, want %q", got, "async hello")
	}

	foundCompleted := false
	for _, task := range harness.Tasks(t, s.TasksDBPath()) {
		harness.WaitFor(t, 30*time.Second, "terminal step for "+task.ID, func() bool {
			for _, step := range harness.Steps(t, s.TasksDBPath(), task.ID) {
				if step.State == "completed" || step.State == "approved" {
					return true
				}
			}
			return false
		})
		foundCompleted = true
	}
	if !foundCompleted {
		t.Fatal("no task with a terminal step after async turn")
	}
}
