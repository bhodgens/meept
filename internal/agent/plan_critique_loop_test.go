package agent

// TierComplex critique-loop pins (tiered-iteration leaf 02).
//
// Deterministic critic/planner scripting: repairCaptureChatter (the
// plan-repair pin's fake) returns canned responses in order and records
// every prompt, so each pin drives a specific loop shape:
//
//	call 1 = draft fill        → a compilable plan-dialect v1 document
//	call 2 = critic round 1    → {"objections": [...]}
//	call 3 = revise round 1    → revised compilable document
//	... and so on; the last scripted response is reused on exhaustion
//	(the chatter's fallback), which is how the exhausted pin keeps the
//	critic returning blocking items forever.
//
// The loop calls RunOnce on the SAME planner loop for draft, critic, and
// revise — one scripted sequence per flow invocation.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/task"
)

// Compile-time check: the scripted fake satisfies the chatter seam.
var _ llm.Chatter = (*repairCaptureChatter)(nil)

// compileableDraft is a minimal plan-dialect v1 document that
// plan.CompileSealed accepts. Substituting %s into the title keeps each
// pin's draft distinct.
func compileableDraft(title string) string {
	return `# Plan: ` + title + `

## Meta

- task_id: t-critique
- version: 1
- status: draft
- updated: 2026-09-23

## Goal

Produce ` + title + ` end to end.

## Decisions

- Decision: do it directly — Rationale: scope is small and testable.

## Open Questions

## Phases

### Phase 1: Build

Build the artifact.

**Produces:**

- ` + "`" + `critique-artifact` + "`" + ` (file) — the finished artifact

**Consumes:** none

**Steps:**

1. Implement the artifact end to end [code]
2. Verify the artifact [analyze] (needs: Phase1.S1)
`
}

// objectionsJSON renders a critic payload from shorthand: each entry is
// "severity|section|objection".
func objectionsJSON(entries ...string) string {
	var sb strings.Builder
	sb.WriteString(`{"objections": [`)
	for i, e := range entries {
		parts := strings.SplitN(e, "|", 3)
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(`{"section": `)
		sb.WriteString(quoteJSON(parts[1]))
		sb.WriteString(`, "objection": `)
		sb.WriteString(quoteJSON(parts[2]))
		sb.WriteString(`, "severity": `)
		sb.WriteString(quoteJSON(parts[0]))
		sb.WriteString(`}`)
	}
	sb.WriteString(`]}`)
	return sb.String()
}

func quoteJSON(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

// newCritiqueTestPlanner builds a StrategicPlanner over the scripted
// chatter with the plan compiler pipeline on and the critique knobs set.
// The chatter rides SetCritiqueChatter (the machine-to-machine seam) so
// the scripted responses reach the loop verbatim — the agent loop's
// reply guards would eat a plan document for its "- updated:" meta line.
func newCritiqueTestPlanner(t *testing.T, chatter *repairCaptureChatter, selfSeal bool, maxRounds int) *StrategicPlanner {
	t.Helper()
	sp := newPlanRepairTestPlanner(t, chatter)
	sp.SetPlanCompilerEnabled(true)
	sp.SetSelfSealEnabled(selfSeal)
	sp.SetCritiqueMaxRounds(maxRounds)
	sp.SetCritiqueChatter(chatter)
	return sp
}

// seedCritiqueTask creates a planning-state task for the flow.
func seedCritiqueTask(t *testing.T, sp *StrategicPlanner, id string) *task.Task {
	t.Helper()
	tsk := newTestTask(id, "comprehensive avatar upload feature")
	tsk.ID = id
	tsk.SetState(task.StatePlanning)
	if err := sp.taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return tsk
}

// critiqueFlowRequest is the TierComplex request shape every pin uses
// (quick_plan unless the pin overrides the mode).
func critiqueFlowRequest(tsk *task.Task, mode string) PlanRequest {
	return PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-critique",
		Input:     "comprehensive avatar upload feature",
		Intent:    "code",
		Mode:      mode,
	}
}

// critiqueMetricTotal sums metrics_live rows for name with a tags["k"]=v
// filter. Both values present in tags must match.
func critiqueMetricTotal(t *testing.T, store *metrics.Store, name string, want map[string]string) int {
	t.Helper()
	type row struct {
		Name  string `db:"metric_name"`
		Tags  string `db:"tags"`
		Value int    `db:"value"`
	}
	var rows []row
	if err := store.DB().Select(&rows,
		`SELECT metric_name, tags, value FROM metrics_live WHERE metric_name = ?`, name); err != nil {
		t.Fatalf("select metric rows: %v", err)
	}
	total := 0
	for _, r := range rows {
		var tags map[string]string
		if err := json.Unmarshal([]byte(r.Tags), &tags); err != nil {
			t.Fatalf("unmarshal tags %q: %v", r.Tags, err)
		}
		match := true
		for k, v := range want {
			if tags[k] != v {
				match = false
				break
			}
		}
		if match {
			total += r.Value
		}
	}
	return total
}

func newCritiqueMetricStore(t *testing.T) *metrics.Store {
	t.Helper()
	store, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close metrics store: %v", err)
		}
	})
	return store
}

// TestCritiqueLoop_CleanRound1SealsSelf pins pin 1: a clean critique on
// round 1 → exactly 1 critic call, the draft seals through the full
// pipeline, and the seal carries provenance planner-self.
func TestCritiqueLoop_CleanRound1SealsSelf(t *testing.T) {
	store := newCritiqueMetricStore(t)
	chatter := &repairCaptureChatter{
		resps: []string{
			compileableDraft("Avatar upload"), // draft fill
			objectionsJSON(),                  // critic round 1: clean
		},
	}
	sp := newCritiqueTestPlanner(t, chatter, true, 2)
	sp.metricsStore = store
	tsk := seedCritiqueTask(t, sp, "task-critique-clean")

	res, err := sp.PlanCritiqueFlow(context.Background(), critiqueFlowRequest(tsk, "quick_plan"), &PlanCritiqueInput{})
	if err != nil {
		t.Fatalf("PlanCritiqueFlow: %v", err)
	}
	if res.Action != critiqueActionHandled {
		t.Fatalf("action = %q, want %q", res.Action, critiqueActionHandled)
	}
	if res.RoundsUsed != 1 {
		t.Errorf("rounds_used = %d, want 1 (clean on round 1)", res.RoundsUsed)
	}

	// Exactly 2 LLM calls: draft fill + one critic pass. No revise.
	if got := chatter.callCount(); got != 2 {
		t.Errorf("LLM calls = %d, want exactly 2 (draft + 1 critic)", got)
	}

	// Sealed: the draft carries the sealed hash and the task reached
	// executing.
	fresh, err := sp.taskStore.GetByID(tsk.ID)
	if err != nil || fresh == nil {
		t.Fatalf("GetByID: %v", err)
	}
	d, ok := draftFromMetadataForTest(t, fresh)
	if !ok {
		t.Fatal("no draft on task after flow")
	}
	if d.SealedHash == "" {
		t.Error("draft SealedHash empty; the self-seal never stamped the hash")
	}
	if fresh.State != task.StateExecuting {
		t.Errorf("task state = %q, want executing after self-seal", fresh.State)
	}
	if got := metadataStringForTest(t, fresh, sealProvenanceMetadataKey); got != sealProvenancePlannerSelf {
		t.Errorf("sealed_by = %q, want %q", got, sealProvenancePlannerSelf)
	}
	if _, ok := metadataRawForTest(t, fresh, critiqueRoundsUsedMetadataKey); !ok {
		t.Error("critique_rounds_used metadata missing")
	}

	// Provenance metric.
	if got := critiqueMetricTotal(t, store, "strategic_planner.seal_provenance", map[string]string{"by": sealProvenancePlannerSelf}); got != 1 {
		t.Errorf("seal_provenance{by=planner-self} = %d, want 1", got)
	}
	if got := critiqueMetricTotal(t, store, "strategic_planner.critique_outcome", map[string]string{"outcome": critiqueOutcomeClean}); got != 1 {
		t.Errorf("critique_outcome{outcome=clean} = %d, want 1", got)
	}
}

// TestCritiqueLoop_BlockingThenClean pins pin 2: a blocking objection
// forces a revise round, the second critique is clean, the draft seals,
// and rounds_used=2.
func TestCritiqueLoop_BlockingThenClean(t *testing.T) {
	store := newCritiqueMetricStore(t)
	chatter := &repairCaptureChatter{
		resps: []string{
			compileableDraft("Avatar upload"),                                 // draft fill
			objectionsJSON("blocking|## Phases|phase 1 lacks rollback steps"), // critic round 1: blocking
			compileableDraft("Avatar upload v2"),                              // revise round 1
			objectionsJSON(),                                                  // critic round 2: clean
		},
	}
	sp := newCritiqueTestPlanner(t, chatter, true, 2)
	sp.metricsStore = store
	tsk := seedCritiqueTask(t, sp, "task-critique-blocking")

	res, err := sp.PlanCritiqueFlow(context.Background(), critiqueFlowRequest(tsk, "quick_plan"), &PlanCritiqueInput{})
	if err != nil {
		t.Fatalf("PlanCritiqueFlow: %v", err)
	}
	if res.Action != critiqueActionHandled {
		t.Fatalf("action = %q, want %q", res.Action, critiqueActionHandled)
	}
	if res.RoundsUsed != 2 {
		t.Errorf("rounds_used = %d, want 2 (blocking → revise → clean)", res.RoundsUsed)
	}

	// 4 LLM calls: draft, critic 1, revise, critic 2.
	if got := chatter.callCount(); got != 4 {
		t.Errorf("LLM calls = %d, want exactly 4", got)
	}

	// The revise prompt carried the objection.
	if !strings.Contains(chatter.promptAt(2), "phase 1 lacks rollback steps") {
		t.Errorf("revise prompt lost the blocking objection; got %.300s", chatter.promptAt(2))
	}

	fresh, err := sp.taskStore.GetByID(tsk.ID)
	if err != nil || fresh == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fresh.State != task.StateExecuting {
		t.Errorf("task state = %q, want executing (clean on round 2 still self-seals)", fresh.State)
	}
	if got := metadataStringForTest(t, fresh, sealProvenanceMetadataKey); got != sealProvenancePlannerSelf {
		t.Errorf("sealed_by = %q, want %q", got, sealProvenancePlannerSelf)
	}
}

// TestCritiqueLoop_ExhaustedSealsWithKnownRisks pins pin 3: rounds
// exhausted with open blocking objections → the Known Risks section is
// present, the draft still seals, and the provenance marker is
// planner-self.
func TestCritiqueLoop_ExhaustedSealsWithKnownRisks(t *testing.T) {
	store := newCritiqueMetricStore(t)
	blocking := objectionsJSON("blocking|## Phases|phase 1 lacks rollback steps")
	chatter := &repairCaptureChatter{
		resps: []string{
			compileableDraft("Avatar upload"), // draft fill
			blocking,                          // critic round 1: blocking
			compileableDraft("Avatar upload"), // revise round 1
			blocking,                          // critic round 2: still blocking → exhausted
		},
	}
	sp := newCritiqueTestPlanner(t, chatter, true, 2)
	sp.metricsStore = store
	tsk := seedCritiqueTask(t, sp, "task-critique-exhausted")

	res, err := sp.PlanCritiqueFlow(context.Background(), critiqueFlowRequest(tsk, "quick_plan"), &PlanCritiqueInput{})
	if err != nil {
		t.Fatalf("PlanCritiqueFlow: %v", err)
	}
	if res.Action != critiqueActionHandled {
		t.Fatalf("action = %q, want %q (exhausted still seals)", res.Action, critiqueActionHandled)
	}
	if res.KnownRisks != 1 {
		t.Errorf("known_risks = %d, want 1", res.KnownRisks)
	}

	// 4 LLM calls: draft, critic 1, revise 1, critic 2. No third round
	// and no second revise (round cap).
	if got := chatter.callCount(); got != 4 {
		t.Errorf("LLM calls = %d, want exactly 4 (round cap 2)", got)
	}

	fresh, err := sp.taskStore.GetByID(tsk.ID)
	if err != nil || fresh == nil {
		t.Fatalf("GetByID: %v", err)
	}
	d, ok := draftFromMetadataForTest(t, fresh)
	if !ok {
		t.Fatal("no draft on task after flow")
	}
	if d.SealedHash == "" {
		t.Error("draft SealedHash empty; exhausted drafts still seal")
	}
	if !strings.Contains(d.Markdown, knownRisksHeading) {
		t.Error("draft missing the Known Risks section on exhausted sealing")
	}
	if !strings.Contains(d.Markdown, "phase 1 lacks rollback steps") {
		t.Error("Known Risks section lost the blocking objection text")
	}
	if got := metadataStringForTest(t, fresh, sealProvenanceMetadataKey); got != sealProvenancePlannerSelf {
		t.Errorf("sealed_by = %q, want %q", got, sealProvenancePlannerSelf)
	}
	if got := critiqueMetricTotal(t, store, "strategic_planner.critique_outcome", map[string]string{"outcome": critiqueOutcomeExhausted}); got != 1 {
		t.Errorf("critique_outcome{outcome=exhausted} = %d, want 1", got)
	}
}

// TestCritiqueLoop_MalformedCriticFailOpen pins pin 4: malformed critic
// output → fail-open (zero objections that round → clean seal) + the
// critic_fail metric. A plan is never blocked because the critic
// misformatted.
func TestCritiqueLoop_MalformedCriticFailOpen(t *testing.T) {
	store := newCritiqueMetricStore(t)
	chatter := &repairCaptureChatter{
		resps: []string{
			compileableDraft("Avatar upload"),
			"I cannot respond in JSON format, the draft looks mostly fine though.",
		},
	}
	sp := newCritiqueTestPlanner(t, chatter, true, 2)
	sp.metricsStore = store
	tsk := seedCritiqueTask(t, sp, "task-critique-malformed")

	res, err := sp.PlanCritiqueFlow(context.Background(), critiqueFlowRequest(tsk, "quick_plan"), &PlanCritiqueInput{})
	if err != nil {
		t.Fatalf("PlanCritiqueFlow: %v", err)
	}
	if res.Action != critiqueActionHandled {
		t.Fatalf("action = %q, want %q (fail-open still seals)", res.Action, critiqueActionHandled)
	}
	if res.RoundsUsed != 1 {
		t.Errorf("rounds_used = %d, want 1 (malformed critic = zero objections)", res.RoundsUsed)
	}
	if got := critiqueMetricTotal(t, store, "strategic_planner.critique_outcome", map[string]string{"outcome": critiqueOutcomeCriticFail}); got != 1 {
		t.Errorf("critique_outcome{outcome=critic_fail} = %d, want 1", got)
	}
	// Zero objections: no revise call.
	if got := chatter.callCount(); got != 2 {
		t.Errorf("LLM calls = %d, want 2 (fail-open skips the revise)", got)
	}
}

// TestCritiqueFlow_FlagOffQuickPlanFallsBack pins pin 5: with the
// self-seal flag off (default), a TierComplex quick_plan request keeps
// leaf 01's single-shot fallback — Warn logged, tier_complex_fallback
// metric, NO critique loop entries and NO seal.
func TestCritiqueFlow_FlagOffQuickPlanFallsBack(t *testing.T) {
	store := newCritiqueMetricStore(t)
	chatter := &repairCaptureChatter{
		resps: []string{`{"steps": [{"description": "step one"}]}`},
	}
	sp := newCritiqueTestPlanner(t, chatter, false, 2) // flag OFF
	sp.metricsStore = store
	tsk := seedCritiqueTask(t, sp, "task-critique-flagoff")

	// ReplanAttempt=2 forces TierComplex despite the single-artifact-free
	// input.
	req := critiqueFlowRequest(tsk, "quick_plan")
	req.ReplanAttempt = 2
	if err := sp.Plan(context.Background(), req); err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// Single-shot proceeded: never more than 2 entries (initial + one
	// repair retry) — the critique loop never entered.
	if got := chatter.callCount(); got < 1 || got > 2 {
		t.Errorf("LLM calls = %d, want 1-2 (single-shot fallback)", got)
	}
	if got := critiqueMetricTotal(t, store, "strategic_planner.tier_complex_fallback", map[string]string{"reason": "flow_disabled"}); got != 1 {
		t.Errorf("tier_complex_fallback{reason=flow_disabled} = %d, want 1", got)
	}

	fresh, err := sp.taskStore.GetByID(tsk.ID)
	if err != nil || fresh == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if _, ok := draftFromMetadataForTest(t, fresh); ok {
		t.Error("flag-off quick_plan must not create/seal a draft")
	}
	if raw, ok := metadataRawForTest(t, fresh, sealProvenanceMetadataKey); ok {
		t.Errorf("flag-off quick_plan must not stamp seal provenance; got %s", raw)
	}
}

// TestCritiqueFlow_FlagOffPlanModeWaitsAtSeal pins pin 6: with the flag
// off, plan mode still RUNS the critique rounds, but the flow ends at the
// human seal step — no self-seal, task stays in planning, refined draft
// on the task.
func TestCritiqueFlow_FlagOffPlanModeWaitsAtSeal(t *testing.T) {
	chatter := &repairCaptureChatter{
		resps: []string{
			compileableDraft("Avatar upload"),
			objectionsJSON("advisory|## Decisions|storage choice could use a benchmark"),
		},
	}
	sp := newCritiqueTestPlanner(t, chatter, false, 2) // flag OFF
	tsk := seedCritiqueTask(t, sp, "task-critique-planwait")

	req := critiqueFlowRequest(tsk, "plan")
	req.ReplanAttempt = 2
	if err := sp.Plan(context.Background(), req); err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// 2 LLM calls: draft fill + one critic pass (clean after advisory).
	if got := chatter.callCount(); got != 2 {
		t.Errorf("LLM calls = %d, want 2 (draft + critic; no revise needed)", got)
	}

	fresh, err := sp.taskStore.GetByID(tsk.ID)
	if err != nil || fresh == nil {
		t.Fatalf("GetByID: %v", err)
	}
	// Waiting at the seal step: still planning, NOT executing.
	if fresh.State != task.StatePlanning {
		t.Errorf("task state = %q, want planning (waits at the seal step)", fresh.State)
	}
	d, ok := draftFromMetadataForTest(t, fresh)
	if !ok {
		t.Fatal("no draft on task; the refined draft must await the human seal")
	}
	if d.SealedHash != "" {
		t.Error("draft carries a sealed hash; flag-off plan mode must not self-seal")
	}
	// Advisory objections roll into Known Risks unconditionally.
	if !strings.Contains(d.Markdown, knownRisksHeading) {
		t.Error("advisory objection did not roll into Known Risks")
	}
	// Provenance proposed as user (the human is the default sealer).
	if got := metadataStringForTest(t, fresh, sealProvenanceMetadataKey); got != sealProvenanceUser {
		t.Errorf("sealed_by = %q, want %q (proposed, human default)", got, sealProvenanceUser)
	}
}

// TestCritiqueFlow_FlagOnPlanModeStillWaits pins the mode split on the
// happy path: even with the flag ON, plan mode waits at the seal step —
// autonomy is quick_plan-only; plan mode gets the refined draft plus a
// planner-self proposed provenance.
func TestCritiqueFlow_FlagOnPlanModeStillWaits(t *testing.T) {
	chatter := &repairCaptureChatter{
		resps: []string{
			compileableDraft("Avatar upload"),
			objectionsJSON(),
		},
	}
	sp := newCritiqueTestPlanner(t, chatter, true, 2) // flag ON
	tsk := seedCritiqueTask(t, sp, "task-critique-planflag")

	req := critiqueFlowRequest(tsk, "plan")
	req.ReplanAttempt = 2
	if err := sp.Plan(context.Background(), req); err != nil {
		t.Fatalf("Plan: %v", err)
	}

	fresh, err := sp.taskStore.GetByID(tsk.ID)
	if err != nil || fresh == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fresh.State != task.StatePlanning {
		t.Errorf("task state = %q, want planning (plan mode never bypasses the seal request)", fresh.State)
	}
	if got := metadataStringForTest(t, fresh, sealProvenanceMetadataKey); got != sealProvenancePlannerSelf {
		t.Errorf("sealed_by = %q, want %q (self-seal proposed as the default)", got, sealProvenancePlannerSelf)
	}
}

// TestCritiqueFlow_CompileRejectionReviseThenHonestFailure pins pin 7:
// the compiler rejecting the sealed draft feeds ONE revise round; if the
// revision still fails to compile, the task fails honestly with the
// problems.
func TestCritiqueFlow_CompileRejectionReviseThenHonestFailure(t *testing.T) {
	// Draft fill returns a structurally broken document (no Phases
	// section body): the critic is clean, but compile rejects with
	// "no phases".
	broken := strings.Replace(compileableDraft("Avatar upload"), "### Phase 1: Build", "### Phase X: Build", 1)
	chatter := &repairCaptureChatter{
		resps: []string{
			broken,           // draft fill: broken phase heading
			objectionsJSON(), // critic: clean
			broken,           // compile-fix revise: STILL broken
		},
	}
	sp := newCritiqueTestPlanner(t, chatter, true, 2)
	tsk := seedCritiqueTask(t, sp, "task-critique-compilefail")

	_, err := sp.PlanCritiqueFlow(context.Background(), critiqueFlowRequest(tsk, "quick_plan"), &PlanCritiqueInput{})
	if err == nil {
		t.Fatal("PlanCritiqueFlow must fail honestly when the compile-fix revise still does not compile")
	}
	if !strings.Contains(err.Error(), "compile failed") {
		t.Errorf("error = %q, want the compile-failure text", err.Error())
	}

	// 3 LLM calls: draft, critic, ONE compile-fix revise. No second try.
	if got := chatter.callCount(); got != 3 {
		t.Errorf("LLM calls = %d, want exactly 3 (one compile-fix revise, no more)", got)
	}

	// The revise prompt carried the compiler problems.
	if !strings.Contains(chatter.promptAt(2), "Compiler problems to fix") {
		t.Errorf("compile-fix revise prompt missing the problems section; got %.300s", chatter.promptAt(2))
	}

	// The task failed honestly (StateFailed + task.failed event).
	fresh, err := sp.taskStore.GetByID(tsk.ID)
	if err != nil || fresh == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fresh.State != task.StateFailed {
		t.Errorf("task state = %q, want failed (honest failure)", fresh.State)
	}
}

// TestCritiqueFlow_CompileRejectionRecovered pins the recovery half of
// the compile-rejection rule: when the compile-fix revise produces a
// compilable document, the flow proceeds to seal.
func TestCritiqueFlow_CompileRejectionRecovered(t *testing.T) {
	broken := strings.Replace(compileableDraft("Avatar upload"), "### Phase 1: Build", "### Phase X: Build", 1)
	chatter := &repairCaptureChatter{
		resps: []string{
			broken,                            // draft fill: broken
			objectionsJSON(),                  // critic: clean
			compileableDraft("Avatar upload"), // compile-fix revise: fixed
		},
	}
	sp := newCritiqueTestPlanner(t, chatter, true, 2)
	tsk := seedCritiqueTask(t, sp, "task-critique-compilerecover")

	res, err := sp.PlanCritiqueFlow(context.Background(), critiqueFlowRequest(tsk, "quick_plan"), &PlanCritiqueInput{})
	if err != nil {
		t.Fatalf("PlanCritiqueFlow: %v", err)
	}
	if res.Action != critiqueActionHandled {
		t.Fatalf("action = %q, want %q", res.Action, critiqueActionHandled)
	}
	fresh, err := sp.taskStore.GetByID(tsk.ID)
	if err != nil || fresh == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fresh.State != task.StateExecuting {
		t.Errorf("task state = %q, want executing after recovered compile", fresh.State)
	}
}

// TestCritiqueLoop_AdvisoryRollsIntoKnownRisksUnconditionally pins the
// advisory rule: advisory-only objections never trigger a revise, but
// they always land in Known Risks — including on a clean (sealing) round.
func TestCritiqueLoop_AdvisoryRollsIntoKnownRisksUnconditionally(t *testing.T) {
	chatter := &repairCaptureChatter{
		resps: []string{
			compileableDraft("Avatar upload"),
			objectionsJSON("advisory|## Decisions|storage choice could use a benchmark"),
		},
	}
	sp := newCritiqueTestPlanner(t, chatter, true, 2)
	tsk := seedCritiqueTask(t, sp, "task-critique-advisory")

	res, err := sp.PlanCritiqueFlow(context.Background(), critiqueFlowRequest(tsk, "quick_plan"), &PlanCritiqueInput{})
	if err != nil {
		t.Fatalf("PlanCritiqueFlow: %v", err)
	}
	if res.Action != critiqueActionHandled {
		t.Fatalf("action = %q, want %q (advisory never blocks)", res.Action, critiqueActionHandled)
	}
	if res.KnownRisks != 1 {
		t.Errorf("known_risks = %d, want 1", res.KnownRisks)
	}
	if got := chatter.callCount(); got != 2 {
		t.Errorf("LLM calls = %d, want 2 (advisory does not trigger a revise)", got)
	}
	fresh, err := sp.taskStore.GetByID(tsk.ID)
	if err != nil || fresh == nil {
		t.Fatalf("GetByID: %v", err)
	}
	d, ok := draftFromMetadataForTest(t, fresh)
	if !ok {
		t.Fatal("no draft on task")
	}
	if !strings.Contains(d.Markdown, "storage choice could use a benchmark") {
		t.Error("Known Risks lost the advisory objection text")
	}
	var risks int
	if raw, ok := metadataRawForTest(t, fresh, knownRisksCountMetadataKey); ok {
		if json.Unmarshal(raw, &risks) != nil {
			t.Fatalf("known_risks_count unparsable: %s", raw)
		}
	}
	if risks != 1 {
		t.Errorf("known_risks_count metadata = %d, want 1", risks)
	}
}

// TestCritiqueFlow_TransportFailureFallsBack pins the degradation rule:
// a planner transport failure on the draft call reports fallback (the
// caller takes the legacy single-shot path) instead of hard-failing the
// task.
func TestCritiqueFlow_TransportFailureFallsBack(t *testing.T) {
	chatter := &repairCaptureChatter{
		resps: []string{"irrelevant"},
		errs:  []error{context.DeadlineExceeded},
	}
	sp := newCritiqueTestPlanner(t, chatter, true, 2)
	tsk := seedCritiqueTask(t, sp, "task-critique-transport")

	res, err := sp.PlanCritiqueFlow(context.Background(), critiqueFlowRequest(tsk, "quick_plan"), &PlanCritiqueInput{})
	if err != nil {
		t.Fatalf("PlanCritiqueFlow must report fallback, not error: %v", err)
	}
	if res.Action != critiqueActionFallback {
		t.Fatalf("action = %q, want %q", res.Action, critiqueActionFallback)
	}
}

// TestCritiqueFlow_ToolCoverageBlocks pins the assembly-time pre-check
// integration: a draft step hinting an unregistered tool yields a
// synthetic blocking objection that drives a revise round — no critic
// call is spent on that class.
func TestCritiqueFlow_ToolCoverageBlocks(t *testing.T) {
	badHint := strings.Replace(
		compileableDraft("Avatar upload"),
		"1. Implement the artifact end to end [code]",
		"1. Implement the artifact end to end [quantum_frobnicator]",
		1)
	good := compileableDraft("Avatar upload")
	chatter := &repairCaptureChatter{
		resps: []string{
			badHint,          // draft fill: unknown tool hint
			objectionsJSON(), // critic: clean (the BLOCKING comes from the pre-check)
			good,             // revise round 1: valid hints
			objectionsJSON(), // critic round 2: clean
		},
	}
	sp := newCritiqueTestPlanner(t, chatter, true, 2)
	tsk := seedCritiqueTask(t, sp, "task-critique-toolcov")

	// Evidence carries the registry names so the assembly-time pre-check
	// can decide (decided WITHOUT spending a critic call on the class).
	input, inputErr := BuildPlanCritiqueInput(CritiqueInputSources{
		Registry: newCritiqueToolRegistry(t, "code", "analyze"),
	})
	if inputErr != nil {
		t.Fatalf("BuildPlanCritiqueInput: %v", inputErr)
	}

	res, err := sp.PlanCritiqueFlow(context.Background(), critiqueFlowRequest(tsk, "quick_plan"), input)
	if err != nil {
		t.Fatalf("PlanCritiqueFlow: %v", err)
	}
	if res.Action != critiqueActionHandled {
		t.Fatalf("action = %q, want %q", res.Action, critiqueActionHandled)
	}
	if res.RoundsUsed != 2 {
		t.Errorf("rounds_used = %d, want 2 (tool-coverage blocking forced a revise)", res.RoundsUsed)
	}
	if !strings.Contains(chatter.promptAt(2), "quantum_frobnicator") {
		t.Errorf("revise prompt lost the tool-coverage objection; got %.300s", chatter.promptAt(2))
	}
}

// TestCritiqueConfigSetters pin the nil-guarded setters and the clamp:
// SetCritiqueMaxRounds ignores non-positive values (the load boundary
// owns the default), and the nil receiver never panics.
func TestCritiqueConfigSetters(t *testing.T) {
	var sp *StrategicPlanner
	sp.SetSelfSealEnabled(true) // must not panic
	sp.SetCritiqueMaxRounds(0)  // must not panic
	if sp.selfSealFlag() {
		t.Error("nil receiver selfSealFlag = true, want false")
	}
	if got := sp.critiqueRoundsCap(); got != 2 {
		t.Errorf("nil receiver rounds cap = %d, want default 2", got)
	}

	sp2 := newPlanRepairTestPlanner(t, &repairCaptureChatter{})
	sp2.SetCritiqueMaxRounds(0)
	if got := sp2.critiqueRoundsCap(); got != 2 {
		t.Errorf("non-positive cap kept = %d, want default 2", got)
	}
	sp2.SetCritiqueMaxRounds(5)
	if got := sp2.critiqueRoundsCap(); got != 5 {
		t.Errorf("cap = %d, want 5", got)
	}
	sp2.SetCritiqueMaxRounds(-3)
	if got := sp2.critiqueRoundsCap(); got != 5 {
		t.Errorf("cap after negative set = %d, want 5 (negative values ignored)", got)
	}
}

// TestCriticObjectionSeverityParsing pins the objection contract:
// blocking/advisory split, unknown severities downgrade to advisory (a
// critic typo must never silently block a seal), and both payload shapes
// parse.
func TestCriticObjectionSeverityParsing(t *testing.T) {
	objs, err := parseCriticObjections(objectionsJSON(
		"blocking|## Phases|missing rollback",
		"advisory|## Decisions|add a benchmark",
		"SEVERITY_TYPO|## Goal|weird severity",
	))
	if err != nil {
		t.Fatalf("parseCriticObjections: %v", err)
	}
	blocking, advisory := splitObjections(objs)
	if len(blocking) != 1 {
		t.Errorf("blocking = %d, want 1", len(blocking))
	}
	if len(advisory) != 2 {
		t.Errorf("advisory = %d, want 2 (unknown severity downgrades)", len(advisory))
	}

	// Bare-array shape (the leaf's literal contract) also parses.
	objs, err = parseCriticObjections(`[{"section": "## Meta", "objection": "x", "severity": "blocking"}]`)
	if err != nil {
		t.Fatalf("parseCriticObjections (bare array): %v", err)
	}
	if len(objs) != 1 || !objs[0].blocking() {
		t.Errorf("bare-array parse = %+v, want one blocking objection", objs)
	}

	// Prose-only output is an error → fail-open at the call site.
	if _, err := parseCriticObjections("prose only, no objections payload"); err == nil {
		t.Error("prose-only output must fail to parse (caller fail-opens)")
	}
}
