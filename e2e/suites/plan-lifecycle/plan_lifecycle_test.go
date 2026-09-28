//go:build e2e

// Package planlifecycle covers the internal/plan lifecycle at the daemon
// boundary (manifest scenarios plan-lifecycle-01..03): plan creation lands a
// real markdown file under the project's docs/plans (project-relative default
// per docs/configuration/plans.md), the approval path rewrites the file's
// status through the atomic read-modify-write (filelock) path, and rejection
// cancels the plan while the tracking file stays parseable. Every observation
// point re-reads and re-parses the file — a torn or unparseable plan.md is a
// failure, not an implementation detail.
//
// Plan discovery on the dispatch path is file-first: the daemon's chat path
// links plans to a thread-scoped conversation id (not the caller-visible
// session id), so the suites watch the project's docs/plans directory for the
// new plan markdown, parse it for its plan_id, and track the store state via
// plan.get — the same file the plan system itself treats as the source of
// truth.
package planlifecycle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/plan"
)

// rpc runs one JSON-RPC call against the sandbox daemon and returns the
// unwrapped result object (failing the test on an RPC error envelope).
func rpc(t *testing.T, s *harness.Stack, method string, params any) map[string]any {
	t.Helper()
	client := harness.DialRPC(t, s.SocketPath)
	return client.CallResult(method, params)
}

// str fetches a string field from a decoded RPC result map.
func str(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// daemonPath resolves a daemon-CWD-relative path (the dispatch path stores
// plan files relative to the daemon working directory) against the sandbox
// work root.
func daemonPath(s *harness.Stack, fp string) string {
	if fp == "" || filepath.IsAbs(fp) {
		return fp
	}
	return filepath.Join(s.Work, fp)
}

// requireParses reads the plan file and parses it, failing the test on any
// read or parse error (the file must be intact at every observation point).
func requireParses(t *testing.T, path, wantPlanID string) *plan.ParsedPlan {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("plan file missing at observation point: %v", err)
	}
	parsed, err := plan.ParsePlanContent(string(data))
	if err != nil {
		t.Fatalf("plan file %s does not parse: %v\ncontent:\n%s", path, err, data)
	}
	if wantPlanID != "" && parsed.PlanID != wantPlanID {
		t.Fatalf("plan file %s carries plan_id %q, want %q", path, parsed.PlanID, wantPlanID)
	}
	return parsed
}

// waitForPlanFile waits until a plan markdown file appears in the watched
// roots, returning its path. The dispatch path stores files relative to the
// daemon working directory (no project path is passed); plan.create with an
// explicit project_path stores them under that project. Every observation
// re-parses the file — the file must be intact on every poll.
func waitForPlanFile(t *testing.T, s *harness.Stack, timeout time.Duration) string {
	t.Helper()
	roots := []string{
		filepath.Join(s.ProjectDir, "docs", "plans"),
		filepath.Join(s.Work, "docs", "plans"),
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, dir := range roots {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
					continue
				}
				return filepath.Join(dir, e.Name())
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("no plan markdown appeared under %v within %s", roots, timeout)
	return ""
}

// waitForPlanState polls plan.get until the plan reaches the given state.
func waitForPlanState(t *testing.T, s *harness.Stack, planID, state string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if got := str(rpc(t, s, "plan.get", map[string]any{"id": planID}), "state"); got == state {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("plan %s never reached state %q within %s", planID, state, timeout)
}

// planSuite overlays plan mode so the dispatch path creates and submits a
// plan, parking it at pending_approval for the operator (the CLI approve /
// reject surface).
func planSuite(t *testing.T) *harness.Stack {
	t.Helper()
	return harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"plans.mode":                      "always",
		"plans.approval.require_approval": true,
	}))
}

// ---------------------------------------------------------------------------
// plan-lifecycle-01 (S): plan.create lands the file in the project's
// docs/plans and the store and the file agree
// ---------------------------------------------------------------------------

// TestPlanCreateLandsProjectRelativeFile pins the project-relative default:
// plan.create with an explicit project_path writes docs/plans/<slug>.md
// INSIDE that project directory, the file carries the plan's id and a status
// line, and the store row is readable and in draft state after creation.
func TestPlanCreateLandsProjectRelativeFile(t *testing.T) {
	s := harness.Start(t)
	projectPath := s.ProjectDir

	created := rpc(t, s, "plan.create", map[string]any{
		"title":        "OAuth token refresh rework",
		"description":  "e2e plan lifecycle probe",
		"project_path": projectPath,
	})
	planID := str(created, "id")
	if planID == "" {
		t.Fatalf("plan.create returned no id: %v", created)
	}
	filePath := str(created, "file_path")
	if filePath == "" {
		t.Fatalf("plan.create returned no file_path: %v", created)
	}

	// Project-relative default: the file lives under <project>/docs/plans/
	// with the slugified title as its name.
	wantDir := filepath.Join(projectPath, "docs", "plans")
	if !strings.HasPrefix(filePath, wantDir+string(filepath.Separator)) {
		t.Fatalf("plan file_path = %q, want it under %s (project-relative default)", filePath, wantDir)
	}
	if base := filepath.Base(filePath); base != "oauth-token-refresh-rework.md" {
		t.Fatalf("plan file name = %q, want the slugified title oauth-token-refresh-rework.md", base)
	}

	// First observation point: the file exists, parses, and identifies the
	// plan. A status line is present (written before or after the draft
	// transition — either is honest; the frontmatter must exist).
	parsed := requireParses(t, filePath, planID)
	if parsed.Status == "" {
		t.Fatalf("plan file %s has no status line in the Meta section", filePath)
	}

	// The store row agrees with the file's identity and is in draft state
	// (create advances planning -> draft in the store).
	got := rpc(t, s, "plan.get", map[string]any{"id": planID})
	if state := str(got, "state"); state != "draft" {
		t.Fatalf("plan.get state = %q, want draft", state)
	}
	if fp := str(got, "file_path"); fp != filePath {
		t.Fatalf("plan.get file_path = %q, want %q", fp, filePath)
	}

	// Second observation point: the file is still intact after the store
	// transition.
	requireParses(t, filePath, planID)
}

// ---------------------------------------------------------------------------
// plan-lifecycle-02 (M): the approval path rewrites the plan file's status
// through the atomic write path — the file exists and parses at EVERY
// observation point while the status flips pending_approval -> executing
// ---------------------------------------------------------------------------

// TestApprovalPathUpdatesPlanFileAtomically drives the full approval path:
// a dispatch-path plan parks at pending_approval (require_approval), the
// operator approves via plan.approve, the plan synthesizes into a task, and
// the tracking file's status becomes executing — parseable on every poll.
func TestApprovalPathUpdatesPlanFileAtomically(t *testing.T) {
	s := planSuite(t)
	sessionID := s.CreateSession(t, "plan-approve", s.ProjectDir)

	s.ChatTurn(t, sessionID,
		"help me understand how renewable energy credits work", 90*time.Second)

	// The dispatch-path plan parks at pending_approval; its tracking file
	// lands in the watched roots. Discover the file, then confirm the
	// store's pending_approval state through plan.get.
	filePath := waitForPlanFile(t, s, 30*time.Second)
	parsed := requireParses(t, filePath, "")
	planID := parsed.PlanID
	if planID == "" {
		t.Fatalf("plan file %s carries no plan_id", filePath)
	}
	waitForPlanState(t, s, planID, "pending_approval", 30*time.Second)

	// Cross-check: the store knows the same plan with a file path that
	// resolves to the file we found.
	got := rpc(t, s, "plan.get", map[string]any{"id": planID})
	if fp := str(got, "file_path"); daemonPath(s, fp) != filePath {
		t.Fatalf("store file_path %q does not resolve to the discovered plan file %s", fp, filePath)
	}

	rpc(t, s, "plan.approve", map[string]any{"plan_id": planID, "by": "e2e"})

	got = rpc(t, s, "plan.get", map[string]any{"id": planID})
	if state := str(got, "state"); state != "executing" {
		t.Fatalf("plan state after approve = %q, want executing (plan: %v)", state, got)
	}
	taskID := str(got, "task_id")
	if taskID == "" {
		t.Fatalf("plan has no synthesized task_id after approval: %v", got)
	}

	// The synthesized parent task is real and reachable through the task
	// RPC surface.
	taskRow := rpc(t, s, "task.get", map[string]any{"id": taskID})
	if str(taskRow, "id") != taskID {
		t.Fatalf("task.get(%s) did not return the synthesized task: %v", taskID, taskRow)
	}

	// The approval path rewrites the file's status to executing via the
	// atomic read-parse-write sequence. Poll until the flip lands; every
	// read must parse — a torn file at any instant is a failure.
	deadline := time.Now().Add(15 * time.Second)
	for {
		parsed := requireParses(t, filePath, planID)
		if parsed.Status == "executing" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("plan file status never reached executing (last %q)", parsed.Status)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// plan-lifecycle-03 (S): rejection cancels the plan; the tracking file stays
// parseable
// ---------------------------------------------------------------------------

// TestRejectCancelsPlanAndKeepsFileIntact pins the reject path: a
// pending_approval plan rejected through plan.reject lands in cancelled, and
// the project's tracking file remains on disk and parseable (rejection never
// destroys the audit trail).
func TestRejectCancelsPlanAndKeepsFileIntact(t *testing.T) {
	s := planSuite(t)
	sessionID := s.CreateSession(t, "plan-reject", s.ProjectDir)

	s.ChatTurn(t, sessionID,
		"explain the tradeoffs between event sourcing and crud storage", 90*time.Second)

	filePath := waitForPlanFile(t, s, 30*time.Second)
	planID := requireParses(t, filePath, "").PlanID
	waitForPlanState(t, s, planID, "pending_approval", 30*time.Second)

	rpc(t, s, "plan.reject", map[string]any{
		"plan_id": planID,
		"by":      "e2e",
		"reason":  "not ready for execution",
	})

	got := rpc(t, s, "plan.get", map[string]any{"id": planID})
	if state := str(got, "state"); state != "cancelled" {
		t.Fatalf("plan state after reject = %q, want cancelled", state)
	}

	// The audit file survives the rejection and still parses.
	parsed := requireParses(t, filePath, planID)
	if parsed.Status == "" {
		t.Fatalf("plan file %s lost its status line after rejection", filePath)
	}
}
