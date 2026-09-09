package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/plan"
)

// --- Test doubles -----------------------------------------------------------

type stubDraftSource struct {
	drafts map[string]stubDraft
	seals  []stubSealCall
}

type stubDraft struct {
	markdown   string
	sealedHash string
}

type stubSealCall struct {
	taskID string
	hash   string
}

func newStubDraftSource() *stubDraftSource {
	return &stubDraftSource{drafts: map[string]stubDraft{}}
}

func (s *stubDraftSource) DraftFor(taskID string) (string, string, bool) {
	d, ok := s.drafts[taskID]
	return d.markdown, d.sealedHash, ok
}

func (s *stubDraftSource) SealDraft(taskID, hash string) error {
	if _, ok := s.drafts[taskID]; !ok {
		return errors.New("no draft")
	}
	s.seals = append(s.seals, stubSealCall{taskID: taskID, hash: hash})
	s.drafts[taskID] = stubDraft{markdown: s.drafts[taskID].markdown, sealedHash: hash}
	return nil
}

// sealableMarkdown is a minimal sealable two-phase dialect document.
const sealableMarkdown = `# Plan: Stub

## Meta

- task_id: task-1
- version: 1
- status: draft
- updated: 2026-09-06

## Goal

Do the thing.

## Decisions

## Open Questions

## Phases

### Phase 1: Build

Build it.

**Produces:**

- ` + "`stub-thing`" + ` (file) — the built thing

**Consumes:** none

**Steps:**

1. Build the thing [code]

### Phase 2: Ship

Ship it.

**Produces:**

- ` + "`stub-release`" + ` (decision) — the release decision

**Consumes:**

- ` + "`stub-thing`" + ` (file) — the built thing

**Steps:**

1. Decide to ship [plan]
`

// flatPhases mirrors plan.CompileSealed's output for sealableMarkdown
// (assembled by the real compiler in the tests that need it).
func mustCompile(t *testing.T, md string, maxPhases int) *plan.CompiledPlan {
	t.Helper()
	cp, err := plan.CompileSealed(md, maxPhases)
	if err != nil {
		t.Fatalf("CompileSealed: %v", err)
	}
	return cp
}

// --- plan.seal: happy path (flat) -------------------------------------------

func TestPlanSeal_HappyPathFlat(t *testing.T) {
	src := newStubDraftSource()
	src.drafts["task-1"] = stubDraft{markdown: sealableMarkdown}

	var compiled *plan.CompiledPlan
	var persistedTaskID string
	var persistedPhases int
	var sealCalled, execCalled bool

	h := &PlanSealHandler{
		DraftSource: src,
		Compile: func(markdown string, maxPhases int) (any, string, []string, []CompileProblemView, error) {
			cp := mustCompile(t, markdown, maxPhases)
			compiled = cp
			return cp.Phases, cp.Hash, cp.Warnings, nil, nil
		},
		Persist: func(taskID string, phases any, _ *plan.EmittedTree) error {
			persistedTaskID = taskID
			if specs, ok := phases.([]plan.PhaseSpec); ok {
				persistedPhases = len(specs)
			}
			return nil
		},
		Execute: func(taskID string) error {
			execCalled = true
			return nil
		},
		MaxPhases: 10,
	}

	raw, err := json.Marshal(map[string]any{"task_id": "task-1"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	res, err := h.handleSeal(context.Background(), raw)
	if err != nil {
		t.Fatalf("handleSeal: %v", err)
	}
	if compiled == nil {
		t.Fatal("Compile not called")
	}
	if persistedTaskID != "task-1" || persistedPhases != 2 {
		t.Errorf("Persist called with taskID=%q phases=%d; want task-1/2", persistedTaskID, persistedPhases)
	}
	if !execCalled {
		t.Error("Execute not called")
	}
	// SealDraft stamped the compiled hash BEFORE Execute.
	if len(src.seals) != 1 || src.seals[0].hash != compiled.Hash || src.seals[0].taskID != "task-1" {
		t.Errorf("SealDraft calls = %+v; want [task-1:%s]", src.seals, compiled.Hash)
	}

	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("result type %T", res)
	}
	if m["status"] != "sealed" {
		t.Errorf("status = %v, want sealed", m["status"])
	}
	if m["hash"] != compiled.Hash {
		t.Errorf("hash = %v, want %s", m["hash"], compiled.Hash)
	}
	if m["mode"] != "flat" {
		t.Errorf("mode = %v, want flat (2 phases × 1 step ≤ flat total)", m["mode"])
	}
	if sealCalled || false {
		_ = sealCalled // silence unused in refactor; tracking via src.seals
	}
}

// --- plan.seal: problems reply (NOT an error) --------------------------------

func TestPlanSeal_ProblemsAreAResult(t *testing.T) {
	src := newStubDraftSource()
	// Non-empty Open Questions → compile problems.
	bad := strings.Replace(sealableMarkdown, "## Open Questions\n\n## Phases",
		"## Open Questions\n\n- unresolved thing?\n\n## Phases", 1)
	src.drafts["task-1"] = stubDraft{markdown: bad}

	var persistCalled, execCalled, sealCalled bool
	h := &PlanSealHandler{
		DraftSource: src,
		Compile: func(markdown string, maxPhases int) (any, string, []string, []CompileProblemView, error) {
			_, err := plan.CompileSealed(markdown, maxPhases)
			var ce *plan.CompileError
			if !errors.As(err, &ce) {
				t.Fatalf("want *CompileError, got %v", err)
			}
			views := make([]CompileProblemView, 0, len(ce.Problems))
			for _, p := range ce.Problems {
				views = append(views, CompileProblemView{Line: p.Line, Message: p.Message})
			}
			return nil, "", nil, views, nil
		},
		Persist:   func(string, any, *plan.EmittedTree) error { persistCalled = true; return nil },
		Execute:   func(string) error { execCalled = true; return nil },
		MaxPhases: 10,
	}

	raw, _ := json.Marshal(map[string]any{"task_id": "task-1"})
	res, err := h.handleSeal(context.Background(), raw)
	if err != nil {
		t.Fatalf("problems must be a RESULT, not an error; got err=%v", err)
	}
	m := res.(map[string]any)
	if m["status"] != "problems" {
		t.Errorf("status = %v, want problems", m["status"])
	}
	probs, ok := m["problems"].([]CompileProblemView)
	if !ok || len(probs) == 0 {
		t.Fatalf("problems missing or wrong type: %v", m["problems"])
	}
	var foundOpenQ bool
	for _, p := range probs {
		if strings.Contains(p.Message, "Open Questions") {
			foundOpenQ = true
		}
	}
	if !foundOpenQ {
		t.Errorf("expected the Open Questions problem in %+v", probs)
	}
	if persistCalled || execCalled || sealCalled {
		t.Error("problems path must not persist, execute, or seal")
	}
	if len(src.seals) != 0 {
		t.Errorf("problems path sealed %+v; draft must stay draft", src.seals)
	}
}

// --- plan.seal: no draft → error ---------------------------------------------

func TestPlanSeal_NoDraft(t *testing.T) {
	h := &PlanSealHandler{
		DraftSource: newStubDraftSource(),
		Compile: func(string, int) (any, string, []string, []CompileProblemView, error) {
			t.Fatal("Compile must not run without a draft")
			return nil, "", nil, nil, nil
		},
	}
	raw, _ := json.Marshal(map[string]any{"task_id": "missing"})
	_, err := h.handleSeal(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "no draft") {
		t.Errorf("want 'no draft' error, got %v", err)
	}
}

// --- plan.seal: tree gate → EmitTree called ----------------------------------

func TestPlanSeal_TreeModeGate(t *testing.T) {
	src := newStubDraftSource()
	src.drafts["task-1"] = stubDraft{markdown: sealableMarkdown}

	// Build a compiled plan big enough to trip the tree gate: 2 phases × 4
	// steps (per-phase cap sizing 3) → tree.
	big := plan.CompiledPlan{}
	for _, name := range []string{"alpha", "beta"} {
		steps := make([]plan.StepSpec, 4)
		for i := range steps {
			steps[i] = plan.StepSpec{Description: "step"}
		}
		big.Phases = append(big.Phases, plan.PhaseSpec{Name: name, Steps: steps})
	}
	big.Hash = "big-hash"

	var gotTree *plan.EmittedTree
	h := &PlanSealHandler{
		DraftSource: src,
		Compile: func(string, int) (any, string, []string, []CompileProblemView, error) {
			return big.Phases, big.Hash, nil, nil, nil
		},
		Persist: func(_ string, phases any, tree *plan.EmittedTree) error {
			gotTree = tree
			if specs, ok := phases.([]plan.PhaseSpec); !ok || len(specs) != 2 {
				t.Errorf("tree path must persist flat phases too; got %T", phases)
			}
			return nil
		},
		Execute:   func(string) error { return nil },
		MaxPhases: 10,
	}
	// Deterministic gate override via ShouldEmitTree injection: the handler
	// consults ShouldEmitTree(phases) — for the real func, 2×4 steps → tree.
	h.ShouldEmitTree = nil // use the real gate

	raw, _ := json.Marshal(map[string]any{"task_id": "task-1"})
	res, err := h.handleSeal(context.Background(), raw)
	if err != nil {
		t.Fatalf("handleSeal: %v", err)
	}
	if gotTree == nil {
		t.Fatal("tree gate: EmitTree not called (expected tree for 2 phases × 4 steps)")
	}
	if gotTree.Root == "" || len(gotTree.Leaves) == 0 {
		t.Error("emitted tree empty")
	}
	m := res.(map[string]any)
	if m["mode"] != "tree" {
		t.Errorf("mode = %v, want tree", m["mode"])
	}
	if m["hash"] != "big-hash" {
		t.Errorf("hash = %v, want big-hash", m["hash"])
	}
}

func TestPlanSeal_TreeGateErrorsBubble(t *testing.T) {
	src := newStubDraftSource()
	src.drafts["task-1"] = stubDraft{markdown: sealableMarkdown}
	// Empty phases → EmitTree's errEmptyPlan.
	h := &PlanSealHandler{
		DraftSource: src,
		Compile: func(string, int) (any, string, []string, []CompileProblemView, error) {
			return []plan.PhaseSpec{}, "h", nil, nil, nil
		},
		Persist:   func(string, any, *plan.EmittedTree) error { return nil },
		Execute:   func(string) error { return nil },
		MaxPhases: 10,
	}
	raw, _ := json.Marshal(map[string]any{"task_id": "task-1"})
	if _, err := h.handleSeal(context.Background(), raw); err == nil {
		t.Error("EmitTree error must surface (cannot seal an empty plan)")
	}
}

// --- plan.draft: save + get ---------------------------------------------------

func TestPlanDraftRPC_SaveAndClose(t *testing.T) {
	var savedID, savedMD string
	save := func(taskID, markdown string) error {
		savedID, savedMD = taskID, markdown
		return nil
	}
	get := func(taskID string) (string, int, bool) {
		if taskID == savedID {
			return savedMD, 3, true
		}
		return "", 0, false
	}
	h := &PlanSealHandler{SaveDraft: save, GetDraft: get}

	// save
	raw, _ := json.Marshal(map[string]any{"task_id": "t-9", "markdown": "body"})
	res, err := h.handleDraft(context.Background(), raw)
	if err != nil {
		t.Fatalf("handleDraft save: %v", err)
	}
	if savedID != "t-9" || savedMD != "body" {
		t.Errorf("save delivered (%q,%q)", savedID, savedMD)
	}
	if m := res.(map[string]any); m["status"] != "saved" || m["version"] != 3 {
		t.Errorf("save reply = %v", m)
	}

	// get (no markdown key)
	raw, _ = json.Marshal(map[string]any{"task_id": "t-9"})
	res, err = h.handleDraft(context.Background(), raw)
	if err != nil {
		t.Fatalf("handleDraft get: %v", err)
	}
	m := res.(map[string]any)
	if m["status"] != "draft" || m["markdown"] != "body" || m["version"] != 3 {
		t.Errorf("get reply = %v", m)
	}

	// get miss → error
	raw, _ = json.Marshal(map[string]any{"task_id": "nope"})
	if _, err = h.handleDraft(context.Background(), raw); err == nil {
		t.Error("get miss: want error")
	}
}

// --- nil-guarded injected funcs ----------------------------------------------

func TestPlanSeal_NilFuncsError(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"task_id": "t-1"})
	h := &PlanSealHandler{}
	if _, err := h.handleSeal(context.Background(), raw); err == nil {
		t.Error("nil Compile: want error")
	}
	h2 := &PlanSealHandler{Compile: func(string, int) (any, string, []string, []CompileProblemView, error) {
		return nil, "", nil, nil, nil
	}}
	if _, err := h2.handleSeal(context.Background(), raw); err == nil {
		t.Error("nil DraftSource: want error")
	}
	h3 := &PlanSealHandler{}
	if _, err := h3.handleDraft(context.Background(), raw); err == nil {
		t.Error("nil GetDraft/SaveDraft: want error")
	}
}

func TestPlanSeal_MissingTaskID(t *testing.T) {
	h := &PlanSealHandler{DraftSource: newStubDraftSource()}
	raw, _ := json.Marshal(map[string]any{})
	if _, err := h.handleSeal(context.Background(), raw); err == nil {
		t.Error("missing task_id: want error")
	}
	if _, err := h.handleDraft(context.Background(), raw); err == nil {
		t.Error("missing task_id: want error")
	}
}

// --- registration does not collide -------------------------------------------

func TestRegisterPlanSealMethods(t *testing.T) {
	h := &PlanSealHandler{}
	srv := New(&Config{SocketPath: "/tmp/plan-seal-test.sock"}, nil, discardLogger())
	h.RegisterPlanSealMethods(srv)
	if _, ok := srv.handlers["plan.seal"]; !ok {
		t.Error("plan.seal not registered")
	}
	if _, ok := srv.handlers["plan.draft"]; !ok {
		t.Error("plan.draft not registered")
	}
	// No collision with the plan-lifecycle handler's methods.
	for _, method := range []string{"plan.approve", "plan.reject", "plan.confirm", "plan.list", "plan.get"} {
		if _, ok := srv.handlers[method]; ok {
			t.Errorf("plan.seal handler must not register %s", method)
		}
	}
}

// --- idempotent persist on seal retry (LOW-dup) ------------------------------

// TestPlanSeal_DoublePersistSkipped pins persist idempotency: the first seal
// attempt persists, then fails at Execute (leaving the persisted phase rows
// in place); the retry recompiles the SAME draft → same hash, and Persist
// must NOT run again (no duplicated plan_phases rows).
func TestPlanSeal_DoublePersistSkipped(t *testing.T) {
	src := newStubDraftSource()
	src.drafts["task-1"] = stubDraft{markdown: sealableMarkdown}

	persistCalls := 0
	execCalls := 0
	h := &PlanSealHandler{
		DraftSource: src,
		Compile: func(markdown string, maxPhases int) (any, string, []string, []CompileProblemView, error) {
			cp := mustCompile(t, markdown, maxPhases)
			return cp.Phases, cp.Hash, nil, nil, nil
		},
		Persist: func(string, any, *plan.EmittedTree) error {
			persistCalls++
			return nil
		},
		Execute: func(string) error {
			execCalls++
			if execCalls == 1 {
				return errors.New("simulated execute failure after persist")
			}
			return nil
		},
	}

	raw, _ := json.Marshal(map[string]any{"task_id": "task-1"})
	// First attempt: persist runs, then Execute fails.
	if _, err := h.handleSeal(context.Background(), raw); err == nil {
		t.Fatal("first attempt: want execute error")
	}
	if persistCalls != 1 {
		t.Fatalf("first attempt: persistCalls = %d, want 1", persistCalls)
	}

	// Retry: same draft → same hash → Persist skipped.
	res, err := h.handleSeal(context.Background(), raw)
	if err != nil {
		t.Fatalf("retry handleSeal: %v", err)
	}
	if persistCalls != 1 {
		t.Errorf("retry re-persisted: persistCalls = %d, want 1 (idempotent)", persistCalls)
	}
	if execCalls != 2 {
		t.Errorf("execCalls = %d, want 2", execCalls)
	}
	if m := res.(map[string]any); m["status"] != "sealed" {
		t.Errorf("retry status = %v, want sealed", m["status"])
	}

	// A CHANGED draft (different hash) must persist again — the marker is
	// per (taskID, hash), not a blanket skip.
	src.drafts["task-1"] = stubDraft{markdown: sealableMarkdown + "\n"}
	if _, err := h.handleSeal(context.Background(), raw); err != nil {
		t.Fatalf("changed-draft seal: %v", err)
	}
	if persistCalls != 2 {
		t.Errorf("changed hash did not re-persist: persistCalls = %d, want 2", persistCalls)
	}
}

// TestPlanSeal_PersistFailureNotMarked pins the marker's write point: a
// failed Persist records nothing, so the immediate retry of the same draft
// DOES call Persist again.
func TestPlanSeal_PersistFailureNotMarked(t *testing.T) {
	src := newStubDraftSource()
	src.drafts["task-1"] = stubDraft{markdown: sealableMarkdown}

	persistCalls := 0
	h := &PlanSealHandler{
		DraftSource: src,
		Compile: func(markdown string, maxPhases int) (any, string, []string, []CompileProblemView, error) {
			cp := mustCompile(t, markdown, maxPhases)
			return cp.Phases, cp.Hash, nil, nil, nil
		},
		Persist: func(string, any, *plan.EmittedTree) error {
			persistCalls++
			return errors.New("transient persist failure")
		},
		Execute: func(string) error { return nil },
	}
	raw, _ := json.Marshal(map[string]any{"task_id": "task-1"})
	for i := 0; i < 2; i++ {
		if _, err := h.handleSeal(context.Background(), raw); err == nil {
			t.Fatalf("attempt %d: want persist error", i+1)
		}
	}
	if persistCalls != 2 {
		t.Errorf("persistCalls = %d, want 2 (failed persists must not set the marker)", persistCalls)
	}
}

// --- maxPhases fallback unified with the planner default (LOW-maxPhases) -----

func TestPlanSealHandler_MaxPhasesFallback(t *testing.T) {
	// Injected value wins.
	h := &PlanSealHandler{MaxPhases: 5}
	if got := h.maxPhases(); got != 5 {
		t.Errorf("maxPhases with MaxPhases=5 = %d, want 5", got)
	}
	// Zero falls back to 12 — StrategicPlanner.MaxPhases()'s default
	// (agent/plan_draft.go); rpc cannot import agent, so this pins the
	// documented constant instead.
	h = &PlanSealHandler{}
	if got := h.maxPhases(); got != 12 {
		t.Errorf("maxPhases zero-value fallback = %d, want 12 (planner default)", got)
	}
	// The cap reaches the compiler: the zero fallback passes 12 through
	// to the injected Compile.
	var cappedWith int
	src := newStubDraftSource()
	src.drafts["task-1"] = stubDraft{markdown: sealableMarkdown}
	h2 := &PlanSealHandler{
		DraftSource: src,
		Compile: func(_ string, maxPhases int) (any, string, []string, []CompileProblemView, error) {
			cappedWith = maxPhases
			// Problems (non-nil) short-circuit before Persist/Execute.
			return nil, "", nil, []CompileProblemView{{Line: 1, Message: "probe"}}, nil
		},
		Persist: func(string, any, *plan.EmittedTree) error { return nil },
		Execute: func(string) error { return nil },
	}
	rawProbe, _ := json.Marshal(map[string]any{"task_id": "task-1"})
	_, _ = h2.handleSeal(context.Background(), rawProbe)
	if cappedWith != 12 {
		t.Errorf("zero-value handler passed maxPhases=%d to compile, want 12", cappedWith)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
