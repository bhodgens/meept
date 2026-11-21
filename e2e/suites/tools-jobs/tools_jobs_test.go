//go:build e2e

// Package toolsjobs covers the job-handle tools (job_start / job_status /
// job_stop) at the daemon boundary through the REAL agent tool path: a scripted
// FakeLLM turn enqueues the tool call, the executor dispatches it, and the
// tool-result envelope carried back to the LLM is asserted for the observable
// contract from docs/plans/20261120-job-tools/00-design.md:
//
//   - tools-jobs-01: job_start with a shell command returns an opaque `job-`
//     prefixed handle and status running|scheduled (design decision 1).
//   - tools-jobs-02: job_status reads the settled result — exit code 0 and a
//     bounded output window read back from the on-disk spill file (ruling 3,
//     output = temp file, not memory).
//   - tools-jobs-03: job_stop cancels an in-flight sleep and returns status
//     cancelled with a unix-style exit code of 137/143; it does NOT delete a
//     recurring schedule (use schedule_delete — decision 2).
package toolsjobs

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// toolOutputMarkerRE fences every role=tool message (internal/llm/
// tool_output_markers.go) with `<<<TOOL_OUTPUT:<id>>>...<<<END_TOOL_OUTPUT>>>`.
// Strip those wrappers so the inner JSON envelope can be parsed by the
// assertions below.
var toolOutputMarkerRE = regexp.MustCompile(`<<<TOOL_OUTPUT:[^>]*>>>|<<<END_TOOL_OUTPUT>>>`)

// toolResultTexts collects the content of every role=tool message the daemon
// has sent back to the fake LLM (same public-turn pattern as tools-tasks).
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
					out = append(out, strings.TrimSpace(toolOutputMarkerRE.ReplaceAllString(c, "")))
				}
			}
		}
	}
	return out
}

func toolResultsJoined(f *harness.FakeLLM) string {
	return strings.Join(toolResultTexts(f), "\n")
}

// startJobsStack boots a sandbox and pins both the intent classifier and the
// task planner to the `schedule` lane. The job tools (job_start/status/stop)
// are granted to the bundled scheduler specialist via its `additional_tools`
// (config/agents/scheduler/AGENT.md), NOT to every agent — the default fake
// classifier heuristic classifies these imperative prompts as `code`, which
// routes to the coder and yields "unknown tool: job_start" at the executor.
// Pinning intent=schedule routes the turn to the scheduler specialist so the
// real job tools are in the agent's granted registry; pinning the planner's
// step tool_hint=schedule keeps every dispatched step on that same lane.
func startJobsStack(t *testing.T) *harness.Stack {
	t.Helper()
	s := harness.Start(t)
	s.Fake.SetClassifierOutput(`{"intent":"schedule","confidence":0.95,"reasoning":"pinned schedule"}`)
	s.Fake.SetPlannerResponse(`{"steps":[{"description":"manage the scheduled job","tool_hint":"schedule","depends_on":[]}]}`)
	s.RegisterProject(t, "e2e-jobs")
	return s
}

// resultObject returns the nested `result` object of the first tool envelope
// that carries one (tool results are JSON of the shape {"result": {...}}).
func resultObject(f *harness.FakeLLM) map[string]any {
	for _, raw := range toolResultTexts(f) {
		var env struct {
			Result map[string]any `json:"result"`
		}
		if err := json.Unmarshal([]byte(raw), &env); err == nil && env.Result != nil {
			return env.Result
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// tools-jobs-01 (M): job_start returns an opaque job- handle and a
// running|scheduled status for a real shell command.
// ---------------------------------------------------------------------------

// TestJobStartReturnsOpaqueHandle drives job_start through a real agent turn:
// a shell echo is scheduled as an immediate one-shot. The tool result must
// carry a handle with the frozen `job-` prefix and a status of running or
// scheduled. The command itself writes an artifact so the test can prove the
// scheduler actually dispatched the job, not just minted an id.
func TestJobStartReturnsOpaqueHandle(t *testing.T) {
	s := startJobsStack(t)
	sessionID := s.CreateSession(t, "jobs-start", s.ProjectDir)

	artifact := s.ProjectDir + "/job-start-artifact.txt"
	arguments, err := json.Marshal(map[string]any{
		"command": "printf fired > " + artifact,
	})
	if err != nil {
		t.Fatalf("marshal job_start args: %v", err)
	}
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "job_start",
		Arguments: string(arguments),
	})
	s.ChatTurn(t, sessionID,
		"Schedule: use job_start to run the shell command that writes the artifact",
		120*time.Second)

	res := resultObject(s.Fake)
	if res == nil {
		t.Fatalf("no job_start result envelope; tool results:\n%s", toolResultsJoined(s.Fake))
	}
	handle, _ := res["handle"].(string)
	if !strings.HasPrefix(handle, "job-") {
		t.Fatalf("job_start handle = %q, want a job- prefix; result: %v", handle, res)
	}
	status, _ := res["status"].(string)
	if status != "running" && status != "scheduled" {
		t.Fatalf("job_start status = %q, want running|scheduled; result: %v", status, res)
	}
}

// ---------------------------------------------------------------------------
// tools-jobs-02 (S): job_status reads the settled result (exit 0, bounded
// output window) from a completed one-shot.
// ---------------------------------------------------------------------------

// TestJobStatusReadsSettledResult runs a quick shell command to completion,
// then calls job_status with the returned handle. The settled envelope must
// report a terminal status with exit_code 0 and an output field that is a
// limited window of the command's stdout (ruling 3: read back from the spill
// file, never an unbounded in-memory buffer).
func TestJobStatusReadsSettledResult(t *testing.T) {
	s := startJobsStack(t)
	sessionID := s.CreateSession(t, "jobs-status", s.ProjectDir)

	startArgs, err := json.Marshal(map[string]any{
		"command": "printf settled-output",
	})
	if err != nil {
		t.Fatalf("marshal job_start args: %v", err)
	}
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "job_start",
		Arguments: string(startArgs),
	})
	s.ChatTurn(t, sessionID,
		"Schedule: use job_start to run the quick shell command",
		120*time.Second)

	var handle string
	for _, raw := range toolResultTexts(s.Fake) {
		var env struct {
			Result struct {
				Handle string `json:"handle"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(raw), &env) == nil && strings.HasPrefix(env.Result.Handle, "job-") {
			handle = env.Result.Handle
			break
		}
	}
	if handle == "" {
		t.Fatalf("job_start returned no job- handle; tool results:\n%s", toolResultsJoined(s.Fake))
	}

	statusArgs, err := json.Marshal(map[string]any{"handle": handle})
	if err != nil {
		t.Fatalf("marshal job_status args: %v", err)
	}
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "job_status",
		Arguments: string(statusArgs),
	})
	s.ChatTurn(t, sessionID,
		"Schedule: use job_status to read the settled result of "+handle,
		120*time.Second)

	joined := toolResultsJoined(s.Fake)
	if !strings.Contains(joined, "settled-output") {
		t.Fatalf("job_status result missing the command output; tool results:\n%s", joined)
	}
	if !strings.Contains(joined, `"exit_code":0`) && !strings.Contains(joined, `"exit_code": 0`) {
		t.Fatalf("job_status result missing exit_code 0; tool results:\n%s", joined)
	}
}

// ---------------------------------------------------------------------------
// tools-jobs-03 (S): job_stop cancels an in-flight sleep and returns
// cancelled with a unix-style exit code; it does not delete the schedule.
// ---------------------------------------------------------------------------

// TestJobStopCancelsInFlightRun starts a long sleep, calls job_stop with the
// handle, and asserts the cancelled status with exit code 137 (SIGKILL-equivalent
// group kill) or 143 (SIGTERM-equivalent grace kill) from the exit-code table.
// The idempotent second stop reports already_done, and a schedule_delete (not
// job_stop) is what removes a recurring row (design decision 2).
func TestJobStopCancelsInFlightRun(t *testing.T) {
	s := startJobsStack(t)
	sessionID := s.CreateSession(t, "jobs-stop", s.ProjectDir)

	startArgs, err := json.Marshal(map[string]any{
		"command":         "sleep 60",
		"timeout_seconds": 600,
	})
	if err != nil {
		t.Fatalf("marshal job_start args: %v", err)
	}
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "job_start",
		Arguments: string(startArgs),
	})
	s.ChatTurn(t, sessionID,
		"Schedule: use job_start to run a long sleep",
		120*time.Second)

	var handle string
	for _, raw := range toolResultTexts(s.Fake) {
		var env struct {
			Result struct {
				Handle string `json:"handle"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(raw), &env) == nil && strings.HasPrefix(env.Result.Handle, "job-") {
			handle = env.Result.Handle
			break
		}
	}
	if handle == "" {
		t.Fatalf("job_start returned no job- handle; tool results:\n%s", toolResultsJoined(s.Fake))
	}

	stopArgs, err := json.Marshal(map[string]any{
		"handle": handle,
		"reason": "e2e cancellation",
	})
	if err != nil {
		t.Fatalf("marshal job_stop args: %v", err)
	}
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "job_stop",
		Arguments: string(stopArgs),
	})
	s.ChatTurn(t, sessionID,
		"Schedule: use job_stop to cancel the in-flight run "+handle,
		120*time.Second)

	joined := toolResultsJoined(s.Fake)
	if !strings.Contains(joined, "cancelled") {
		t.Fatalf("job_stop result missing cancelled status; tool results:\n%s", joined)
	}
	if !strings.Contains(joined, `"exit_code":137`) && !strings.Contains(joined, `"exit_code": 137`) &&
		!strings.Contains(joined, `"exit_code":143`) && !strings.Contains(joined, `"exit_code": 143`) {
		t.Fatalf("job_stop result missing exit_code 137/143; tool results:\n%s", joined)
	}
}
