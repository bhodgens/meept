package main

// 09-cli-affordances TEST-GATE (task 5): `meept plans list --state <s>`
// filters client-side and `meept plans revise <id>` drives plan.revise.
// RED before plans.go grows either. Pattern follows kanban_test.go: a
// fake transport.Client driven through the real cobra RunE path via the
// plansConnectDaemon seam.

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/transport"
)

type recordedPlansCall struct {
	method string
	params map[string]any
}

type fakePlansClient struct {
	transport.Client // nil-embedded; only Call/Close are used (panics loudly if reached)
	responses        map[string]json.RawMessage
	calls            []recordedPlansCall
}

func newFakePlansClient() *fakePlansClient {
	return &fakePlansClient{responses: map[string]json.RawMessage{}}
}

func (f *fakePlansClient) Call(method string, params any) (json.RawMessage, error) {
	m, _ := params.(map[string]any)
	f.calls = append(f.calls, recordedPlansCall{method: method, params: m})
	if raw, ok := f.responses[method]; ok {
		return raw, nil
	}
	return json.RawMessage(`{}`), nil
}

func (f *fakePlansClient) Close() error { return nil }

func plansListResult(plans ...map[string]any) json.RawMessage {
	b, err := json.Marshal(map[string]any{"plans": plans})
	if err != nil {
		panic(err)
	}
	return b
}

func runPlansCmd(t *testing.T, f *fakePlansClient, args ...string) string {
	t.Helper()
	prev := plansConnectDaemon
	plansConnectDaemon = func() (transport.Client, error) { return f, nil }
	t.Cleanup(func() { plansConnectDaemon = prev })

	// The plans commands print via fmt to os.Stdout (not the cobra
	// writer), so capture the process stdout around Execute.
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	os.Stdout = w

	cmd := newPlansCmd()
	cmd.SetArgs(args)
	execErr := cmd.Execute()

	w.Close()
	os.Stdout = oldStdout
	var out bytes.Buffer
	if _, err := out.ReadFrom(r); err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	if execErr != nil {
		t.Fatalf("plans %v: %v (output: %s)", args, execErr, out.String())
	}
	return out.String()
}

func seedPlansListResponse(f *fakePlansClient) {
	f.responses["plan.list"] = plansListResult(
		map[string]any{"id": "p1", "title": "approved plan", "state": "approved", "phases": []any{}},
		map[string]any{"id": "p2", "title": "pending plan", "state": "pending_approval", "phases": []any{}},
		map[string]any{"id": "p3", "title": "done plan", "state": "completed", "phases": []any{}},
	)
}

// TestPlansListStateFilter pins the --state flag: client-side filter over
// plan.list rows; only matching states print, and the total reflects the
// filtered count.
func TestPlansListStateFilter(t *testing.T) {
	f := newFakePlansClient()
	seedPlansListResponse(f)

	out := runPlansCmd(t, f, "list", "--state", "approved")
	if !strings.Contains(out, "p1") || !strings.Contains(out, "approved plan") {
		t.Fatalf("09-gate: filtered list lost the matching plan:\n%s", out)
	}
	if strings.Contains(out, "pending plan") || strings.Contains(out, "done plan") {
		t.Fatalf("09-gate: --state approved leaked non-matching plans:\n%s", out)
	}
	if !strings.Contains(out, "Total: 1") {
		t.Fatalf("09-gate: total must reflect the filtered count:\n%s", out)
	}
}

// TestPlansListNoStateShowsAll pins the default: without --state every
// plan prints (byte-identical legacy behavior).
func TestPlansListNoStateShowsAll(t *testing.T) {
	f := newFakePlansClient()
	seedPlansListResponse(f)

	out := runPlansCmd(t, f, "list")
	for _, want := range []string{"p1", "p2", "p3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("09-gate: unfiltered list missing %s:\n%s", want, out)
		}
	}
}

// TestPlansListJSONIgnoresStateFilter pins the --json passthrough: the
// raw RPC envelope prints unfiltered (the filter is a rendering-side
// affordance; --json preserves the full payload for scripting).
func TestPlansListJSONIgnoresStateFilter(t *testing.T) {
	f := newFakePlansClient()
	seedPlansListResponse(f)

	out := runPlansCmd(t, f, "list", "--json", "--state", "approved")
	var env struct {
		Plans []map[string]any `json:"plans"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("09-gate: --json output does not decode: %v\n%s", err, out)
	}
	if len(env.Plans) != 3 {
		t.Fatalf("09-gate: --json must pass the full envelope through, got %d plans", len(env.Plans))
	}
}

// TestPlansReviseSubcommand pins `meept plans revise <id>`: it calls
// plan.revise{plan_id} and reports the resulting state.
func TestPlansReviseSubcommand(t *testing.T) {
	f := newFakePlansClient()
	f.responses["plan.revise"] = plansListResult()
	// plan.revise reads back the plan; serve a GetPlan-shaped row.
	f.responses["plan.revise"] = mustJSON(map[string]any{"id": "p9", "title": "revised plan", "state": "draft"})

	out := runPlansCmd(t, f, "revise", "p9")

	var reviseCalls int
	for _, c := range f.calls {
		if c.method != "plan.revise" {
			continue
		}
		reviseCalls++
		if id, _ := c.params["plan_id"].(string); id != "p9" {
			t.Errorf("09-gate: plan.revise plan_id = %v, want p9", c.params["plan_id"])
		}
	}
	if reviseCalls != 1 {
		t.Fatalf("09-gate: plan.revise calls = %d, want 1 (calls: %+v)", reviseCalls, f.calls)
	}
	if !strings.Contains(out, "revised plan") {
		t.Fatalf("09-gate: revise output must name the plan:\n%s", out)
	}
}

// TestPlansReviseRegistered pins the command tree: `revise` exists on the
// plans command with the id argument.
func TestPlansReviseRegistered(t *testing.T) {
	cmd := newPlansCmd()
	var revise *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "revise" {
			revise = sub
			break
		}
	}
	if revise == nil {
		t.Fatal("09-gate: plans revise subcommand not registered")
	}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
