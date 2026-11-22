//go:build e2e

// Suite payload-strip: hermetic live gate for plan
// 20261121-interactive-usability leaf 07 (master C-5).
//
// A real meept-daemon boots over scratch stores seeded through the REAL
// on-disk store constructors (pre-boot hook opens tasks.db at the exact
// path the boot resolves, writes a task whose step carries the strategic
// planner's scaffolding injection and a machine evidence envelope, and
// closes every handle before boot). The suite then drives BOTH transports:
//
//	RPC task.get / task.steps  (daemon Unix socket, framed JSON-RPC)
//	HTTP GET /api/v1/tasks/{id}[/steps]  (daemon TLS listener)
//
// and asserts NEITHER response carries "## Session context", "## Your
// Task", or raw claims/evidence JSON - the client renders the clean
// description and the prose-recovered result. Models-less rig: no chat
// turn is needed, the strip is a pure payload-boundary concern.
package payloadstrip

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
)

const (
	strippedTaskID = "ps-task-1"
	strippedStepID = "ps-step-1"
)

// The exact injection shape strategic.go produces, plus the machine
// envelope the executor's step result carries.
const (
	rawStepDesc = "## Session context (most recent work in this conversation)\n" +
		"- earlier: wrote the parser\n- earlier: fixed the build\n\n" +
		"## Your Task\n\n" +
		"write the summary"
	rawStepResult = `job job-ps-1 completed by agent coder: {"status":"success","accomplished":["summary written"],"observations":["docs updated"]}`
)

// seedStores opens the REAL task store at the path the daemon boot
// resolves (harness pre-boot hook contract), writes the fixtures, and
// closes the handle before returning so the boot never sees a foreign
// lock.
func seedStores(st *harness.Stack) error {
	logger := slog.Default()
	reg, err := task.NewRegistry(st.TasksDBPath(), bus.New(nil, logger), logger)
	if err != nil {
		return fmt.Errorf("seed: open task registry: %w", err)
	}
	ts := reg.Store()
	base := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	if err := ts.Create(&task.Task{
		ID:        strippedTaskID,
		Name:      "payload strip fixture task",
		State:     task.StatePending,
		CreatedAt: base,
		UpdatedAt: base,
	}); err != nil {
		_ = reg.Close()
		return fmt.Errorf("seed: create task: %w", err)
	}
	if err := reg.StepStore().Create(&task.TaskStep{
		ID:          strippedStepID,
		TaskID:      strippedTaskID,
		Description: rawStepDesc,
		Result:      rawStepResult,
		State:       task.StepCompleted,
		Sequence:    0,
		CreatedAt:   base,
		UpdatedAt:   base,
	}); err != nil {
		_ = reg.Close()
		return fmt.Errorf("seed: create step: %w", err)
	}
	if err := reg.Close(); err != nil {
		return fmt.Errorf("seed: close task registry: %w", err)
	}
	return nil
}

func startStack(t *testing.T) *harness.Stack {
	t.Helper()
	return harness.Start(t,
		harness.WithPreBootHook(seedStores),
		// Models-less scratch arming: the classifier boot gate is
		// explicitly disarmed (the fake provider has no lifecycle
		// blocks, but the explicit false keeps the models-less rule).
		harness.WithConfigOverlay(map[string]any{
			"orchestrator.classifier_boot_fail_fast": false,
		}),
	)
}

// scaffoldingMarkers are the internal-only strings that must never leave
// the daemon through a task payload.
var scaffoldingMarkers = []string{
	"## Session context",
	"## Your Task",
	`"accomplished"`,
	`"claims"`,
	`"evidence"`,
	"completed by agent",
}

// wantClean marks response bodies that must carry BOTH the clean
// description and the prose-recovered result (the step-bearing endpoints).
// Marker-free bodies that legitimately lack step rows (task.get returns the
// bare task row) pass assertMarkerFree.
func assertClean(t *testing.T, what, body string) {
	t.Helper()
	assertMarkerFree(t, what, body)
	if !strings.Contains(body, "write the summary") {
		t.Fatalf("%s lost the clean description \"write the summary\"; body:\n%s", what, body)
	}
	if !strings.Contains(body, "- summary written") {
		t.Fatalf("%s lost the prose-recovered result; body:\n%s", what, body)
	}
}

func assertMarkerFree(t *testing.T, what, body string) {
	t.Helper()
	for _, marker := range scaffoldingMarkers {
		if strings.Contains(body, marker) {
			t.Fatalf("%s leaked internal marker %q; body:\n%s", what, marker, body)
		}
	}
}

// decodeResponseSteps marshals the generic RPC result back through the
// task response shapes so the assertions run over the decoded fields.
func rpcResultBody(t *testing.T, res map[string]any) string {
	t.Helper()
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("re-encode rpc result: %v", err)
	}
	return string(b)
}

// T1 (RPC): task.get + task.steps over the daemon's Unix socket carry
// neither scaffolding marker nor raw machine envelope; the rendered step
// text is the clean description.
func TestPayloadStrip_RPCResponses(t *testing.T) {
	s := startStack(t)
	c := harness.DialRPC(t, s.SocketPath)

	res := c.CallResult("task.get", map[string]any{"id": strippedTaskID})
	assertMarkerFree(t, "rpc task.get", rpcResultBody(t, res))

	res = c.CallResult("task.steps", map[string]any{"task_id": strippedTaskID})
	assertClean(t, "rpc task.steps", rpcResultBody(t, res))

	res = c.CallResult("task.list_extended", nil)
	assertClean(t, "rpc task.list_extended", rpcResultBody(t, res))
}

// T2 (HTTP): GET /api/v1/tasks/{id} and /api/v1/tasks/{id}/steps carry
// neither marker; the GUI detail view (which consumes exactly these
// endpoints) stays clean WITHOUT client changes.
func TestPayloadStrip_HTTPResponses(t *testing.T) {
	s := startStack(t)

	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
		},
	}

	fetch := func(path string) string {
		t.Helper()
		resp, err := client.Get(s.HTTPBaseURL() + path) //nolint:noctx // bounded by client Timeout
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status %d: %s", path, resp.StatusCode, body)
		}
		return string(body)
	}

	assertMarkerFree(t, "http GET /tasks/{id} (bare task row)", fetch("/api/v1/tasks/"+strippedTaskID))
	assertClean(t, "http GET /tasks/{id}/steps", fetch("/api/v1/tasks/"+strippedTaskID+"/steps"))
	assertMarkerFree(t, "http GET /tasks (bare task rows)", fetch("/api/v1/tasks"))
}

// T3 (store invariant): the strip is display-only. Reread the seeded row
// through a fresh store handle and assert the RAW scaffolding is still on
// disk - the planner keeps its context.
func TestPayloadStrip_StoreRowsUntouched(t *testing.T) {
	s := startStack(t)
	_ = s // boot happened (proves the daemon is healthy over the seeded data)

	logger := slog.Default()
	reg, err := task.NewRegistry(s.TasksDBPath(), bus.New(nil, logger), logger)
	if err != nil {
		t.Fatalf("reopen task registry: %v", err)
	}
	defer func() { _ = reg.Close() }()

	steps, err := reg.StepStore().ListByTaskID(strippedTaskID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("stored steps = %d, want 1", len(steps))
	}
	if steps[0].Description != rawStepDesc {
		t.Fatalf("stored description was mutated:\n%q\nwant\n%q", steps[0].Description, rawStepDesc)
	}
	if steps[0].Result != rawStepResult {
		t.Fatalf("stored result was mutated:\n%q\nwant\n%q", steps[0].Result, rawStepResult)
	}
	_ = context.Background
}
