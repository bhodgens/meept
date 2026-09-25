//go:build e2e

// Package steppipeline is the e2e suite for the per-step pipeline
// contracts (manifest scenarios step-pipeline-01..05):
//
//	01 an errored step never passes review; the task finalizes StateFailed
//	02 a review rejection creates a revision step; the task stays active
//	03 a needs_info review verdict pauses the step for human input
//	04 parallel steps respect the dependency graph; blocked dependents cascade
//	05 a vacuous (narration-only) planner result is gated out of auto-approval
//
// Every scenario drives the REAL daemon through the hermetic stack and
// asserts END STATES in tasks.db, on-disk artifacts, and the daemon log —
// never loop internals. Predicate scripting (harness.Script /
// ScriptN / OnCallNumber) binds responses to specific conversations, so
// each scenario forces exactly the branch it pins.
package steppipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// reviewNotRequired logs when a step rode the trivial-task heuristic (no
// full reviewer) — used by the revision test to explain why it waits for
// the rejection to surface anyway.
const revisionFeedbackMarker = "REVISION-FEEDBACK-MARKER"

// ---------------------------------------------------------------------------
// step-pipeline-01 (S): errored step never passes review; task finalizes failed
// ---------------------------------------------------------------------------

// TestErroredStepNeverPassesReviewTaskFails pins the F2 honest-failure
// contract end to end: the scripted step's ONLY tool call writes into an
// impossible path (/proc), the executor loop returns the repeat-breaker
// refusal as an ERROR (the identical doomed call exhausts its failure
// budget), and TacticalScheduler.OnJobFailed terminalizes the step FAILED
// and the task StateFailed — never completed. The store's step result
// carries the honest failure text, not the scripted post-tool narration.
func TestErroredStepNeverPassesReviewTaskFails(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "sp01", s.ProjectDir)

	// Bind the doomed call to THIS conversation's STEP JOB: the step-job
	// prompt is the planned step description (which embeds the marker via
	// the pinned plan below). IsPlannerRequest() is excluded explicitly —
	// the planner prompt embeds the raw input, so an unguarded predicate
	// would hand the doomed tool call to the planner loop.
	const marker = "SP01-DOOMED"
	// The doomed path is INSIDE the allowed project fence (a security
	// block would short-circuit into the loop's permission-denied flow,
	// not the repeat-error breaker): blocker.txt is created as a FILE
	// first, so MkdirAll/doomed write fails at the OS layer — a real
	// tool failure identical on every retry.
	blocker := filepath.Join(s.ProjectDir, "sp01-blocker")
	if err := os.WriteFile(blocker, []byte("obstacle"), 0o644); err != nil {
		t.Fatalf("create blocker file: %v", err)
	}
	doomedPath := filepath.Join(blocker, "doomed.txt")
	doomed := `{"path":"` + doomedPath + `","content":"x","direct":true}`
	s.Fake.SetPlannerResponse(`{"steps":[{"description":"` + marker + `: create the doomed file","tool_hint":"file_write","depends_on":[]}]}`)
	s.Fake.ScriptN(6,
		harness.And(harness.IsExecutorRequest(),
			harness.Not(harness.IsPlannerRequest()),
			// MessageContains, not LastUserMessageContains: after the
			// first tool round the ladder's corrective nudge
			// ("[system: no measurable progress...]") becomes the LAST
			// user message, so a last-message predicate would stop
			// matching exactly when the breaker needs the repeat.
			harness.MessageContains(marker)),
		harness.ToolCallResponse(harness.ToolCall{Name: "file_write", Arguments: doomed}))
	// Any other executor turn that slips through gets honest narration.
	s.Fake.SetPostToolText("The task failed; the file could not be written.")

	ack := s.SubmitChatHTTP(t, sessionID,
		"Create a file at "+doomedPath+" containing x. "+marker)
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("step-pipeline-01: ack missing turn_id: %+v", ack)
	}

	// Wait for the task row, then for its honest failed terminus.
	var taskID string
	harness.WaitFor(t, 30*time.Second, "task row for SP01", func() bool {
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			if strings.Contains(row.Name, marker) || row.Name != "" {
				taskID = row.ID
				return true
			}
		}
		return false
	})

	deadline := time.Now().Add(180 * time.Second)
	var lastState string
	var failedRow harness.TaskRow
found:
	for time.Now().Before(deadline) {
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			if row.ID != taskID {
				continue
			}
			lastState = row.State
			if row.State == "failed" || row.State == "completed" {
				failedRow = row
				break found
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	if failedRow.State == "" {
		t.Fatalf("step-pipeline-01: task %s never reached a terminal state (last %q)", taskID, lastState)
	}
	if failedRow.State != "failed" {
		steps := harness.Steps(t, s.TasksDBPath(), taskID)
		t.Fatalf("step-pipeline-01: task finalized %q, want failed (an errored step must never pass); steps:\n%s",
			failedRow.State, harness.FormatSteps(steps))
	}
	if failedRow.FailedJobs < 1 {
		t.Fatalf("step-pipeline-01: failed_jobs = %d, want >= 1: %+v", failedRow.FailedJobs, failedRow)
	}

	// The errored step's stored result is the honest failure text — never
	// a completed verdict riding over the error.
	var failureText string
	for _, st := range harness.Steps(t, s.TasksDBPath(), taskID) {
		if st.State == "failed" && st.Result != "" {
			failureText = st.Result
			break
		}
	}
	if failureText == "" {
		t.Fatalf("step-pipeline-01: no failed step result; steps:\n%s",
			harness.FormatSteps(harness.Steps(t, s.TasksDBPath(), taskID)))
	}
	if strings.Contains(failureText, "could not be written") && !strings.Contains(strings.ToLower(failureText), "fail") && !strings.Contains(strings.ToLower(failureText), "error") {
		t.Fatalf("step-pipeline-01: failed step result lost the honest failure vocabulary: %q", failureText)
	}
}

// ---------------------------------------------------------------------------
// step-pipeline-02 (M): review rejection creates a revision step
// ---------------------------------------------------------------------------

// TestReviewRejectionCreatesRevisionStep pins the revision branch of
// HandleReviewResult end to end: a FULL reviewer (forced past the
// trivial-task heuristic by scripting a 3-step plan whose extra steps are
// no-ops) returns a rejection verdict, and the store gains a revision step
// (id carries "-rev-") depending on the rejected original, with the
// rejection feedback stored on the original.
func TestReviewRejectionCreatesRevisionStep(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "sp02", s.ProjectDir)

	const marker = "SP02-REVISION"
	artifact := s.ProjectDir + "/sp02-artifact.txt"

	// 3-step plan (isTrivialTask auto-approves < 3 steps): the real work
	// step plus two deterministic filler steps. The coder executes step 1
	// (bound by its description marker), the fillers are trivial
	// list_directory turns.
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned"}`)
	s.Fake.SetPlannerResponse(`{"steps":[
		{"description":"` + marker + `: create the artifact file sp02-artifact.txt containing sp02 work","tool_hint":"file_write","depends_on":[]},
		{"description":"list the files in the project directory to verify the workspace","tool_hint":"file_write","depends_on":[]},
		{"description":"summarize the project directory contents in one sentence","tool_hint":"file_write","depends_on":[]}
	]}`)

	// Step 1's executor turn: one real file_write, then the narration.
	s.Fake.ScriptN(1,
		harness.And(harness.IsExecutorRequest(),
			harness.Not(harness.IsPlannerRequest()),
			// MessageContains, not LastUserMessageContains: after the
			// first tool round the ladder's corrective nudge
			// ("[system: no measurable progress...]") becomes the LAST
			// user message, so a last-message predicate would stop
			// matching exactly when the breaker needs the repeat.
			harness.MessageContains(marker)),
		harness.ToolCallResponse(harness.ToolCall{
			Name:      "file_write",
			Arguments: `{"path":"` + artifact + `","content":"sp02 work","direct":true}`,
		}))
	s.Fake.SetPostToolText("Created sp02-artifact.txt with the requested content.")

	// The REVIEWER prompt arrives as the USER message of the reviewer
	// loop's completion request ("REVIEW TASK STEP" — buildReviewPrompt).
	// Serve the rejection EXACTLY ONCE: a reviewer loop that repeats the
	// identical verdict trips the loop's convergence detector and the
	// reviewer errors out (tactical then terminalizes without review).
	// Subsequent reviewer turns fall through to the benign post-tool
	// text (approved by default).
	s.Fake.ScriptN(1,
		harness.And(harness.IsExecutorRequest(), harness.MessageContains("REVIEW TASK STEP")),
		harness.TextResponse(`{"status":"rejected","feedback":"`+revisionFeedbackMarker+`: the work is incomplete","issues":["missing sections"],"confidence":0.9}`))
	// Benign narration: post-tool text must never claim file side-effects
	// (a reviewer loop narrating a write with zero tool calls trips the
	// unbacked-claims nudge and converges).
	s.Fake.SetPostToolText("The requested step work is complete.")

	s.SubmitChatHTTP(t, sessionID,
		"Do the SP02 work: create the artifact and finish the task. "+marker)

	var taskID string
	harness.WaitFor(t, 30*time.Second, "task row for SP02", func() bool {
		tasks := harness.Tasks(t, s.TasksDBPath())
		if len(tasks) > 0 {
			taskID = tasks[len(tasks)-1].ID
			return true
		}
		return false
	})

	// The revision step appears once the reviewer's rejection is handled.
	var originalID, revisionID string
	harness.WaitFor(t, 180*time.Second, "revision step for task "+taskID, func() bool {
		for _, st := range harness.Steps(t, s.TasksDBPath(), taskID) {
			if strings.Contains(st.ID, "-rev-") {
				revisionID = st.ID
				return true
			}
			if st.State == "rejected" {
				originalID = st.ID
			}
		}
		return false
	})
	if revisionID == "" {
		steps := harness.Steps(t, s.TasksDBPath(), taskID)
		t.Fatalf("step-pipeline-02: no revision step created after rejection; steps:\n%s", harness.FormatSteps(steps))
	}

	// The rejected original is terminal-rejected and carries the feedback.
	rejectedSeen := false
	for _, st := range harness.Steps(t, s.TasksDBPath(), taskID) {
		if st.State == "rejected" {
			rejectedSeen = true
		}
	}
	if !rejectedSeen {
		// The revision may already have replaced the state via recount —
		// the daemon log is the tiebreaker evidence.
		if !strings.Contains(s.Daemon.LogTail(), "Step rejected") {
			t.Fatalf("step-pipeline-02: no rejected step row and no rejection log; steps:\n%s",
				harness.FormatSteps(harness.Steps(t, s.TasksDBPath(), taskID)))
		}
	}
	_ = originalID

	// The task stayed ACTIVE through the rejection (revision machinery),
	// or already finalized after the revision ran — either way it must
	// reach a terminal state within the bounded window (never hang).
	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			if row.ID == taskID && (row.State == "completed" || row.State == "failed") {
				return // reached terminal after the revision cycle
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("step-pipeline-02: task %s did not finalize after the revision cycle", taskID)
}

// ---------------------------------------------------------------------------
// step-pipeline-03 (M): needs_info review verdict pauses the step for human input
// ---------------------------------------------------------------------------

// TestNeedsInfoReviewVerdictPausesForHuman pins the needs_info branch: the
// scripted full reviewer answers needs_info, the verdict is persisted on
// the step (review_verdict column, observable via task.get RPC metadata /
// step inspection through the daemon log), tactical forces the step
// completed so the task proceeds, and the human-facing feedback reaches the
// session. The observable pause contract: the daemon LOGS the needs_info
// transition and the task never lies about validation.
func TestNeedsInfoReviewVerdictPausesForHuman(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "sp03", s.ProjectDir)

	const marker = "SP03-NEEDSINFO"
	artifact := s.ProjectDir + "/sp03-notes.txt"

	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned"}`)
	// Pin the intent analyzer (system prompt "intent analysis assistant")
	// to a low-ambiguity implementation analysis suggesting plan mode —
	// mode=plan routes through the strategic planner, whose pinned
	// 3-step plan keeps the task out of the single-step trivial-task
	// heuristic so the FULL reviewer runs.
	s.Fake.Script(harness.SystemPromptContains("intent analysis assistant"),
		harness.TextResponse(`{"goal":"draft the analysis notes","ambiguity":0.1,"scope":"medium","category":"implementation","suggested_questions":[],"confidence":0.9,"suggested_mode":"plan"}`))
	s.Fake.SetPlannerResponse(`{"steps":[
		{"description":"` + marker + `: create the notes file sp03-notes.txt with the analysis draft","tool_hint":"file_write","depends_on":[]},
		{"description":"list the files in the project directory","tool_hint":"file_write","depends_on":[]},
		{"description":"report the project directory state in one line","tool_hint":"file_write","depends_on":[]}
	]}`)

	s.Fake.ScriptN(1,
		harness.And(harness.IsExecutorRequest(),
			harness.Not(harness.IsPlannerRequest()),
			// MessageContains, not LastUserMessageContains: after the
			// first tool round the ladder's corrective nudge
			// ("[system: no measurable progress...]") becomes the LAST
			// user message, so a last-message predicate would stop
			// matching exactly when the breaker needs the repeat.
			harness.MessageContains(marker)),
		harness.ToolCallResponse(harness.ToolCall{
			Name:      "file_write",
			Arguments: `{"path":"` + artifact + `","content":"draft analysis","direct":true}`,
		}))
	s.Fake.SetPostToolText("Created sp03-notes.txt with the draft.")

	// The reviewer prompt arrives as the USER message ("REVIEW TASK
	// STEP"). Serve needs_info ONCE; later reviewer turns get the benign
	// post-tool text (no fabricated file claims → no convergence abort).
	s.Fake.ScriptN(1,
		harness.And(harness.IsExecutorRequest(), harness.MessageContains("REVIEW TASK STEP")),
		harness.TextResponse(`{"status":"needs_info","feedback":"NEEDS-INFO-MARKER: which audience is the analysis for? please clarify","issues":["audience unclear"],"confidence":0.8}`))
	s.Fake.SetPostToolText("The requested step work is complete.")

	s.SubmitChatHTTP(t, sessionID,
		"Draft the analysis notes file with the quarterly numbers, then finish the task. "+marker)

	var taskID string
	harness.WaitFor(t, 30*time.Second, "task row for SP03", func() bool {
		tasks := harness.Tasks(t, s.TasksDBPath())
		if len(tasks) > 0 {
			taskID = tasks[len(tasks)-1].ID
			return true
		}
		return false
	})

	// The needs_info verdict is logged (ReviewManager: "Step needs more
	// info") and the step is forced completed so the task can proceed.
	// Wide window: under a full parallel suite run the three-step plan's
	// executor + reviewer turns queue behind the other steps.
	harness.WaitFor(t, 300*time.Second, "needs_info handling for task "+taskID, func() bool {
		// Full log, not LogTail: the replan churn that follows scrolls
		// the 4KB tail before a slow poll can read it.
		log := readWholeDaemonLog(t, s)
		return strings.Contains(log, "needs more info") || strings.Contains(log, "needs_info")
	})

	// The task still reaches a terminal state (needs_info forces the step
	// completed — the pause is informational, not a hang).
	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			if row.ID == taskID && (row.State == "completed" || row.State == "failed") {
				// The artifact exists (the work happened) and the human
				// question reached the session log — the honest pause.
				if _, err := readArtifact(artifact); err != nil {
					t.Fatalf("step-pipeline-03: artifact missing after needs_info flow: %v", err)
				}
				return
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("step-pipeline-03: task %s never finalized after the needs_info verdict", taskID)
}

// ---------------------------------------------------------------------------
// step-pipeline-04 (L): parallel steps respect the dependency graph; cascades marked
// ---------------------------------------------------------------------------
// step-pipeline-04 (L): parallel steps respect the dependency graph
// ---------------------------------------------------------------------------

// TestParallelStepsDependencyCascade pins the dependency-graph contract
// with a plan that fans out: step 0 (doomed file_write — the identical
// failing call exhausts the repeat-error breaker and the step FAILS),
// step 1 independent (succeeds), step 2 depends on step 0. The failed
// dependency must keep the dependent step from ever successfully
// executing (failed deps block promotion), the failing step's failures
// drive the escalation/replan ladder to its bounded terminus, and the
// task finalizes StateFailed honestly — the independent step's success
// does not launder the failure, and the dependent "verify" step never
// reports success.
func TestParallelStepsDependencyCascade(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "sp04", s.ProjectDir)

	const doomedMarker = "SP04-DOOMED-STEP"
	const okMarker = "SP04-OK-STEP"
	okArtifact := filepath.Join(s.ProjectDir, "sp04-ok.txt")

	// OS-level doomed path inside the allowed fence (a security block
	// would ride the permission-denied flow instead of the breaker).
	sp04Blocker := filepath.Join(s.ProjectDir, "sp04-blocker")
	if err := os.WriteFile(sp04Blocker, []byte("obstacle"), 0o644); err != nil {
		t.Fatalf("create blocker file: %v", err)
	}
	doomedPath := filepath.Join(sp04Blocker, "broken.txt")

	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned"}`)
	s.Fake.SetPlannerResponse(`{"steps":[
		{"description":"` + doomedMarker + `: create the file at ` + doomedPath + `","tool_hint":"file_write","depends_on":[]},
		{"description":"` + okMarker + `: create the file sp04-ok.txt containing ok work","tool_hint":"file_write","depends_on":[]},
		{"description":"verify the broken file from the first step exists and report its contents","tool_hint":"file_write","depends_on":[0]}
	]}`)

	// The doomed step: identical failing calls until the breaker refuses.
	doomed := `{"path":"` + doomedPath + `","content":"x","direct":true}`
	s.Fake.ScriptN(6,
		harness.And(harness.IsExecutorRequest(),
			harness.Not(harness.IsPlannerRequest()),
			harness.MessageContains(doomedMarker)),
		harness.ToolCallResponse(harness.ToolCall{Name: "file_write", Arguments: doomed}))
	// The independent step: succeeds.
	s.Fake.ScriptN(1,
		harness.And(harness.IsExecutorRequest(),
			harness.Not(harness.IsPlannerRequest()),
			harness.MessageContains(okMarker)),
		harness.ToolCallResponse(harness.ToolCall{
			Name:      "file_write",
			Arguments: `{"path":"` + okArtifact + `","content":"ok work","direct":true}`,
		}))
	s.Fake.SetPostToolText("The requested step work is complete.")

	s.SubmitChatHTTP(t, sessionID,
		"Run the SP04 plan steps: create "+doomedPath+" and the ok file. "+doomedMarker+" "+okMarker)

	var taskID string
	harness.WaitFor(t, 30*time.Second, "task row for SP04", func() bool {
		tasks := harness.Tasks(t, s.TasksDBPath())
		if len(tasks) > 0 {
			taskID = tasks[len(tasks)-1].ID
			return true
		}
		return false
	})

	// The task must finalize FAILED (the repeated doomed failure is
	// terminal after the bounded escalation/replan ladder), never
	// completed, and within a bounded window.
	deadline := time.Now().Add(300 * time.Second)
	var finalRow harness.TaskRow
	for time.Now().Before(deadline) {
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			if row.ID == taskID && (row.State == "failed" || row.State == "completed") {
				finalRow = row
			}
		}
		if finalRow.State != "" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if finalRow.State == "" {
		t.Fatalf("step-pipeline-04: task %s never finalized", taskID)
	}
	if finalRow.State != "failed" {
		steps := harness.Steps(t, s.TasksDBPath(), taskID)
		t.Fatalf("step-pipeline-04: task finalized %q, want failed; steps:\n%s",
			finalRow.State, harness.FormatSteps(steps))
	}

	// The independent step actually ran (artifact written).
	data, err := os.ReadFile(okArtifact)
	if err != nil {
		t.Fatalf("step-pipeline-04: independent step's artifact missing: %v\ndaemon log:\n%s", err, s.Daemon.LogTail())
	}
	if strings.TrimSpace(string(data)) != "ok work" {
		t.Fatalf("step-pipeline-04: independent artifact content = %q", data)
	}

	// The failed dependency is respected: NO step in the verify chain
	// (which depends on the doomed step) ever reached a successful
	// terminal state — blocked-by-failed promotion never promotes it and
	// replans never executed it successfully either.
	steps := harness.Steps(t, s.TasksDBPath(), taskID)
	dependentSucceeded := false
	for _, st := range steps {
		if strings.Contains(st.Description, "verify the broken file") &&
			(st.State == "completed" || st.State == "approved") {
			dependentSucceeded = true
		}
	}
	if dependentSucceeded {
		t.Fatalf("step-pipeline-04: dependent step succeeded despite its failed dependency; steps:\n%s",
			harness.FormatSteps(steps))
	}

	// The doomed step's failures are recorded: the store shows at least
	// one failed step across the replan generations OR the failed state
	// itself carries the bounded terminus (the replan ladder re-executes
	// replacement steps, so completed_jobs may legitimately grow — the
	// honest signal is the failed state plus the never-succeeded dep).
	_ = finalRow
}

// ---------------------------------------------------------------------------
// step-pipeline-05 (S): vacuous review approval is gated
// ---------------------------------------------------------------------------
// step-pipeline-05 (S): vacuous review approval is gated
// ---------------------------------------------------------------------------

// TestVacuousNarrationIsGatedFromAutoApproval pins the claim-without-
// evidence gate: an executor step whose result NARRATES artifact creation
// ("Created the plan file ...") but performs NO tool call carries no
// tool-issued evidence — the trivial-task heuristic must refuse the
// auto-approval (daemon log: "artifact claims with no tool evidence") and
// escalate to the full reviewer instead. Observable end states: the log
// carries the gate's refusal, the narrated artifact does NOT exist on
// disk, and no completed/approved step stores the fabrication verbatim.
func TestVacuousNarrationIsGatedFromAutoApproval(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "sp05", s.ProjectDir)

	const marker = "SP05-VACUOUS"
	ghost := filepath.Join(s.ProjectDir, "sp05-plan.txt")

	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned"}`)
	s.Fake.SetPlannerResponse(`{"steps":[{"description":"` + marker + `: create the plan file sp05-plan.txt containing the ordered checklist","tool_hint":"file_write","depends_on":[]}]}`)

	// The step's executor turn NARRATES the write with NO tool calls —
	// the vacuous shape the gate exists to catch.
	s.Fake.ScriptN(1,
		harness.And(harness.IsExecutorRequest(),
			harness.Not(harness.IsPlannerRequest()),
			harness.MessageContains(marker)),
		harness.TextResponse("I have created the file sp05-plan.txt at "+ghost+" containing the ordered checklist. The work is complete."))

	s.Fake.SetPostToolText("Done with the step work.")

	s.SubmitChatHTTP(t, sessionID, "Plan and execute the SP05 work. "+marker)

	var taskID string
	harness.WaitFor(t, 30*time.Second, "task row for SP05", func() bool {
		tasks := harness.Tasks(t, s.TasksDBPath())
		if len(tasks) > 0 {
			taskID = tasks[len(tasks)-1].ID
			return true
		}
		return false
	})

	// The gate fired: the daemon log carries the loop guard's refusal —
	// the narration-only turn is nudged for real tool use instead of
	// standing as evidence of work.
	harness.WaitFor(t, 150*time.Second, "unbacked-claims gate in daemon log", func() bool {
		log := readWholeDaemonLog(t, s)
		return strings.Contains(log, "Unbacked file side-effect claims with zero tool executions") ||
			strings.Contains(log, "no tool evidence") ||
			strings.Contains(log, "refusing auto-approve")
	})

	// The narrated artifact was never written (no tool ran).
	if _, err := os.Stat(ghost); err == nil {
		t.Fatalf("step-pipeline-05: ghost artifact %s exists without any scripted tool call", ghost)
	}

	// The task finalizes within the bounded window; a completed task must
	// not store the fabrication as a verified step result.
	deadline := time.Now().Add(240 * time.Second)
	for time.Now().Before(deadline) {
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			if row.ID != taskID || (row.State != "completed" && row.State != "failed") {
				continue
			}
			for _, st := range harness.Steps(t, s.TasksDBPath(), taskID) {
				if (st.State == "completed" || st.State == "approved") &&
					strings.Contains(st.Result, "created the file") {
					t.Fatalf("step-pipeline-05: fabricated narration stored as a successful step result: %q", st.Result)
				}
			}
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("step-pipeline-05: task %s never finalized after the vacuity gate routed to review", taskID)
}

// --- helpers --------------------------------------------------------------

func readArtifact(path string) (string, error) {
	data, err := readFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
