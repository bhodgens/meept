//go:build e2e

// Package toolstasks drives the full task-tool lifecycle through REAL agent
// turns: task_create -> task_update -> task_get -> task_list, asserting the
// observable end state in both the tool-result envelopes and tasks.db.
package toolstasks

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// toolResultTexts collects the content of every role=tool message the daemon
// has ever sent back to the fake LLM.
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

// createdTaskID scans the tool-result envelopes for the task_id returned by
// task_create (the create must precede any update/get in the scripted turn).
func createdTaskID(f *harness.FakeLLM) string {
	for _, raw := range toolResultTexts(f) {
		var env struct {
			Result struct {
				TaskID string `json:"task_id"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(raw), &env); err == nil && env.Result.TaskID != "" {
			return env.Result.TaskID
		}
	}
	return ""
}

// tools-tasks-01: full lifecycle. Turn 1 creates the task (and lands a row in
// tasks.db); the harness then reads the real task id from the store; turn 2
// updates, gets, and lists it.
func TestTaskLifecycleCreateUpdateGetList(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "tasks-lifecycle", s.ProjectDir)

	const taskName = "e2e-lifecycle-task"

	// Turn 1: create.
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "task_create",
		Arguments: `{"name":"` + taskName + `","description":"lifecycle e2e fixture task"}`,
	})
	s.ChatTurn(t, sessionID,
		"Create a report task named "+taskName+" with description \"lifecycle e2e fixture task\" using the task tool",
		120*time.Second)

	res1 := toolResultsJoined(s.Fake)
	if !strings.Contains(res1, taskName) {
		t.Fatalf("task_create result missing the task name; results:\n%s", res1)
	}
	if !strings.Contains(res1, "pending") {
		t.Fatalf("task_create result missing the pending state; results:\n%s", res1)
	}

	// The tool's create must be visible in tasks.db (same store the daemon
	// uses for dispatched tasks).
	var createdID string
	for _, task := range harness.Tasks(t, s.TasksDBPath()) {
		if task.Name == taskName {
			createdID = task.ID
		}
	}
	if createdID == "" {
		t.Fatalf("created task %q not found in tasks.db; rows: %+v", taskName, harness.Tasks(t, s.TasksDBPath()))
	}
	// Cross-check: the id the tool returned matches the persisted row.
	if toolID := createdTaskID(s.Fake); toolID != "" && toolID != createdID {
		t.Fatalf("task_create returned id %q but tasks.db has %q", toolID, createdID)
	}

	// Turn 2: update -> get -> list, using the persisted id.
	s.Fake.EnqueueToolCalls(
		harness.ToolCall{
			Name: "task_update",
			Arguments: `{"id":"` + createdID + `","state":"completed",` +
				`"description":"lifecycle e2e fixture task (done)"}`,
		},
		harness.ToolCall{
			Name:      "task_get",
			Arguments: `{"task_id":"` + createdID + `"}`,
		},
		harness.ToolCall{
			Name:      "task_list",
			Arguments: `{"limit":50}`,
		},
	)
	s.ChatTurn(t, sessionID,
		"Update the report task "+createdID+" to completed, then get it and list tasks so I can write the final report file",
		120*time.Second)

	res2 := toolResultsJoined(s.Fake)
	if !strings.Contains(res2, "completed") {
		t.Fatalf("turn-2 results missing the completed state; results:\n%s", res2)
	}
	if !strings.Contains(res2, createdID) {
		t.Fatalf("turn-2 results missing the task id %s; results:\n%s", createdID, res2)
	}
	if !strings.Contains(res2, "lifecycle e2e fixture task (done)") {
		t.Fatalf("task_get result missing the updated description; results:\n%s", res2)
	}

	// The persisted row reflects the update.
	var sawCompleted bool
	for _, task := range harness.Tasks(t, filepath.Clean(s.TasksDBPath())) {
		if task.ID == createdID {
			sawCompleted = task.State == "completed"
		}
	}
	if !sawCompleted {
		t.Fatalf("task %s not completed in tasks.db after task_update", createdID)
	}
}
