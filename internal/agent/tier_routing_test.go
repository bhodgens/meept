package agent

// Tier-routing pins (tiered-iteration leaf 01).
//
// Plan() consults tierForRequest BEFORE choosing a planning path:
//   - quick_plan: the tier is computed first, the strategic_planner.tier
//     metric is emitted per request, and TierComplex falls back to
//     single-shot with a Warn + tier_complex_fallback metric (dark launch
//     until leaf 2 lands).
//   - plan: the tier is computed before the compiler-flag branch; on
//     TierComplex the seeded draft gains a complexity marker in metadata.
//
// The replan-attempt policy lives in tierForRequest (ReplanAttempt >= 2
// forces TierComplex); EvaluatePlanComplexity stays pure. The escalation
// level rides task metadata (escalation_level key) as the single source
// of truth for both replan sites.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/task"
)

// draftFromMetadataForTest reads the draft presence off a task's metadata
// (mirrors draftFromMetadata without the planner receiver).
func draftFromMetadataForTest(t *testing.T, tsk *task.Task) (*PlanDraft, bool) {
	t.Helper()
	if tsk == nil || len(tsk.Metadata) == 0 {
		return nil, false
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(tsk.Metadata, &meta); err != nil {
		return nil, false
	}
	raw, ok := meta[planDraftMetadataKey]
	if !ok {
		return nil, false
	}
	d, err := unmarshalPlanDraft(raw)
	if err != nil {
		return nil, false
	}
	return d, true
}

// metadataStringForTest returns a string-valued metadata entry ("" when
// absent/unparsable).
func metadataStringForTest(t *testing.T, tsk *task.Task, key string) string {
	t.Helper()
	raw, ok := metadataRawForTest(t, tsk, key)
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// metadataRawForTest returns the raw JSON for a metadata key.
func metadataRawForTest(t *testing.T, tsk *task.Task, key string) (json.RawMessage, bool) {
	t.Helper()
	if tsk == nil || len(tsk.Metadata) == 0 {
		return nil, false
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(tsk.Metadata, &meta); err != nil {
		return nil, false
	}
	raw, ok := meta[key]
	return raw, ok
}

// TestTierForRequest_ReplanAttemptForcesComplex pins the precedence rule:
// a second (or later) replan attempt forces TierComplex even when the
// input matches the single-artifact shape, and ReplanAttempt=0 leaves the
// evaluator's verdict untouched.
func TestTierForRequest_ReplanAttemptForcesComplex(t *testing.T) {
	singleArtifact := "create a file named notes.md"

	// attempt >= 2 forces Complex regardless of the input shape.
	req := PlanRequest{Input: singleArtifact, ReplanAttempt: 2}
	if got := tierForRequest(req); got != TierComplex {
		t.Errorf("ReplanAttempt=2 single-artifact tier = %q, want TierComplex (precedence pin)", got)
	}
	req.ReplanAttempt = 3
	if got := tierForRequest(req); got != TierComplex {
		t.Errorf("ReplanAttempt=3 tier = %q, want TierComplex", got)
	}

	// attempt = 1 (first replan) keeps the evaluator's signal: the
	// single-artifact shape still routes trivial.
	req.ReplanAttempt = 1
	if got := tierForRequest(req); got != TierTrivial {
		t.Errorf("ReplanAttempt=1 single-artifact tier = %q, want TierTrivial (evaluator signal)", got)
	}

	// attempt = 0 (not a replan / unknown) = evaluator signal only.
	req.ReplanAttempt = 0
	if got := tierForRequest(req); got != TierTrivial {
		t.Errorf("ReplanAttempt=0 single-artifact tier = %q, want TierTrivial", got)
	}

	// The evaluator itself is untouched by ReplanAttempt: an attempt>=2
	// request that skips tierForRequest's policy still classifies by the
	// 3 pure signals.
	pure := EvaluatePlanComplexity(PlanRequest{Input: singleArtifact, ReplanAttempt: 2})
	if pure != TierTrivial {
		t.Errorf("EvaluatePlanComplexity saw ReplanAttempt and returned %q; evaluator must stay pure (3-signal)", pure)
	}
}

// TestReplanAttemptFromTask pins the metadata round-trip and the
// never-guess rule: absent key, malformed metadata, and unparsable values
// all read as 0 (unknown), never a fabricated count.
func TestReplanAttemptFromTask(t *testing.T) {
	if got := replanAttemptFromTask(nil); got != 0 {
		t.Errorf("nil task attempt = %d, want 0", got)
	}

	// Absent key -> 0.
	tsk := newTestTask("task-attempt-none", "x")
	if got := replanAttemptFromTask(tsk); got != 0 {
		t.Errorf("absent key attempt = %d, want 0", got)
	}

	// Written level reads back.
	tsk.Metadata = mergeMetadata(tsk.Metadata, map[string]json.RawMessage{
		escalationLevelMetadataKey: json.RawMessage(`2`),
	})
	if got := replanAttemptFromTask(tsk); got != 2 {
		t.Errorf("level 2 attempt = %d, want 2", got)
	}

	// Unparsable value -> 0 (never guess).
	tsk.Metadata = mergeMetadata(tsk.Metadata, map[string]json.RawMessage{
		escalationLevelMetadataKey: json.RawMessage(`"two"`),
	})
	if got := replanAttemptFromTask(tsk); got != 0 {
		t.Errorf("unparsable attempt = %d, want 0", got)
	}

	// Malformed outer metadata -> 0.
	tsk.Metadata = json.RawMessage(`not-json`)
	if got := replanAttemptFromTask(tsk); got != 0 {
		t.Errorf("malformed metadata attempt = %d, want 0", got)
	}
}

// TestQuickPlan_TrivialTierSinglePhaseUnchanged pins the dark-launch
// contract for trivial requests: the tier metric is emitted, no fallback
// metric fires, and planSinglePhase runs unchanged (exactly one planner
// LLM call, no repair, no extra entries).
func TestQuickPlan_TrivialTierSinglePhaseUnchanged(t *testing.T) {
	goodPlan := `{"steps": [{"description": "step one"}]}`
	chatter := &repairCaptureChatter{resps: []string{goodPlan}}
	sp := newPlanRepairTestPlanner(t, chatter)

	tsk := newTestTask("task-tier-trivial", "create a file named notes.md")
	if err := sp.taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := sp.Plan(context.Background(), PlanRequest{
		TaskID: tsk.ID,
		Input:  "create a file named notes.md",
		Intent: string(IntentQuickPlan),
		Mode:   "quick_plan",
	}); err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// planSinglePhase invoked exactly once, untouched by the tier gate.
	if got := chatter.callCount(); got != 1 {
		t.Errorf("planner LLM calls = %d, want exactly 1 (planSinglePhase unchanged)", got)
	}
}

// TestQuickPlan_ComplexTierFallsBackSingleShot pins the TierComplex dark
// launch: a complex quick_plan request (second replan attempt) proceeds
// single-shot — call count 1-2 (initial + at most one repair retry from
// planSinglePhase's existing hardening), never more — while the fallback
// metric records the reason.
func TestQuickPlan_ComplexTierFallsBackSingleShot(t *testing.T) {
	store, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	goodPlan := `{"steps": [{"description": "step one"}]}`
	chatter := &repairCaptureChatter{resps: []string{goodPlan}}
	sp := newPlanRepairTestPlanner(t, chatter)
	sp.metricsStore = store

	tsk := newTestTask("task-tier-complex", "create a file named notes.md")
	if err := sp.taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := sp.Plan(context.Background(), PlanRequest{
		TaskID:        tsk.ID,
		Input:         "create a file named notes.md",
		Intent:        string(IntentQuickPlan),
		Mode:          "quick_plan",
		IsReplan:      true,
		ReplanAttempt: 2, // forces TierComplex despite the single-artifact input
	}); err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// Single-shot proceeded: 1 call when the first response parses; the
	// cap is 2 (one repair retry inside planSinglePhase). Never more —
	// the complex tier must NOT add planner entries in this dark launch.
	if got := chatter.callCount(); got < 1 || got > 2 {
		t.Errorf("planner LLM calls = %d, want 1-2 (single-shot with at most one repair retry)", got)
	}

	// The tier metric was emitted with tier=complex, and the fallback
	// metric carries reason=flow_disabled.
	type row struct {
		Name  string `db:"metric_name"`
		Tags  string `db:"tags"`
		Value int    `db:"value"`
	}
	var rows []row
	if err := store.DB().Select(&rows,
		`SELECT metric_name, tags, value FROM metrics_live WHERE metric_name IN ('strategic_planner.tier', 'strategic_planner.tier_complex_fallback')`); err != nil {
		t.Fatalf("select metric rows: %v", err)
	}
	tierComplexSeen, fallbackSeen := false, false
	for _, r := range rows {
		var tags map[string]string
		if err := json.Unmarshal([]byte(r.Tags), &tags); err != nil {
			t.Fatalf("unmarshal tags %q: %v", r.Tags, err)
		}
		switch r.Name {
		case "strategic_planner.tier":
			if tags["tier"] == "complex" {
				tierComplexSeen = true
			}
		case "strategic_planner.tier_complex_fallback":
			if tags["reason"] == "flow_disabled" {
				fallbackSeen = true
			}
		}
	}
	if !tierComplexSeen {
		t.Errorf("strategic_planner.tier metric with tier=complex not recorded; rows: %+v", rows)
	}
	if !fallbackSeen {
		t.Errorf("strategic_planner.tier_complex_fallback metric with reason=flow_disabled not recorded; rows: %+v", rows)
	}
}

// TestPlan_ComplexTierMarksDraftMetadata pins the plan-mode routing: with
// the compiler flag on, a TierComplex request still seeds the draft AND
// the task metadata gains the complexity: complex marker; a trivial
// request seeds the draft WITHOUT the marker.
func TestPlan_ComplexTierMarksDraftMetadata(t *testing.T) {
	sp, store := newDraftTestPlanner(t)
	sp.SetPlanCompilerEnabled(true)

	// Complex: ReplanAttempt=2 forces the tier.
	tsk := seedDraftTask(t, store, "task-plan-tier-complex")
	if err := sp.Plan(context.Background(), PlanRequest{
		TaskID:        tsk.ID,
		SessionID:     "sess-plan-tier",
		Input:         "Build avatar upload with local storage",
		Intent:        "code",
		Mode:          "plan",
		IsReplan:      true,
		ReplanAttempt: 2,
	}); err != nil {
		t.Fatalf("Plan (complex): %v", err)
	}

	fresh, err := store.GetByID(tsk.ID)
	if err != nil || fresh == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if _, ok := draftFromMetadataForTest(t, fresh); !ok {
		t.Fatal("complex plan request: no draft seeded; the seed must still happen")
	}
	if got := metadataStringForTest(t, fresh, "complexity"); got != "complex" {
		t.Errorf("complexity metadata = %q, want %q", got, "complex")
	}

	// Trivial: same path, no marker.
	trivial := seedDraftTask(t, store, "task-plan-tier-trivial")
	if err := sp.Plan(context.Background(), PlanRequest{
		TaskID:    trivial.ID,
		SessionID: "sess-plan-tier",
		Input:     "notes.md",
		Intent:    "code",
		Mode:      "plan",
	}); err != nil {
		t.Fatalf("Plan (trivial): %v", err)
	}
	freshTrivial, err := store.GetByID(trivial.ID)
	if err != nil || freshTrivial == nil {
		t.Fatalf("GetByID (trivial): %v", err)
	}
	if _, ok := draftFromMetadataForTest(t, freshTrivial); !ok {
		t.Fatal("trivial plan request: no draft seeded")
	}
	if raw, ok := metadataRawForTest(t, freshTrivial, "complexity"); ok {
		t.Errorf("trivial plan request must not carry a complexity marker; got %q", raw)
	}
}

// TestEscalateWritesLevelToTaskMetadata pins the single-source-of-truth
// write: Escalate records the incremented level on the task's metadata,
// and triggerReplan's PlanRequest carries it as ReplanAttempt.
func TestEscalateWritesLevelToTaskMetadata(t *testing.T) {
	sp := newPlanRepairTestPlanner(t, &repairCaptureChatter{
		resps: []string{`{"steps": [{"description": "smaller step"}]}`},
	})
	msgBus := bus.New(nil, slogDiscardLogger())
	t.Cleanup(func() { msgBus.Close() })

	em := NewEscalationManager(EscalationManagerConfig{
		Config:    EscalationConfig{Enabled: true, MaxEscalationLevels: 5},
		Planner:   sp,
		TaskStore: sp.taskStore,
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	})

	tsk := newTestTask("task-escalation-meta", "do the thing")
	if err := sp.taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	for want := 1; want <= 3; want++ {
		if err := em.Escalate(context.Background(), FailureContext{
			TaskID:  tsk.ID,
			StepID:  "step-1",
			AgentID: config.AgentIDCoder,
			Error:   "boom",
			Stage:   "execution",
		}); err != nil {
			t.Fatalf("Escalate #%d: %v", want, err)
		}
		stored, err := sp.taskStore.GetByID(tsk.ID)
		if err != nil || stored == nil {
			t.Fatalf("GetByID #%d: %v", want, err)
		}
		if got := replanAttemptFromTask(stored); got != want {
			t.Errorf("level #%d on task metadata = %d, want %d", want, got, want)
		}
		if got := em.GetEscalationLevel(tsk.ID); got != want {
			t.Errorf("in-memory level #%d = %d, want %d (metadata and map must agree)", want, got, want)
		}
	}
}

// TestReplanFailedTaskCarriesReplanAttempt pins the orchestrator replan
// site: ReplanFailedTask reads the escalation level from task metadata
// into the PlanRequest (no guessed counts).
func TestReplanFailedTaskCarriesReplanAttempt(t *testing.T) {
	sp, store := newDraftTestPlanner(t)
	sp.registry = newEmptyPlanTestRegistry(&repairCaptureChatter{
		resps: []string{`{"steps": [{"description": "retry step"}]}`},
	})

	tsk := seedDraftTask(t, store, "task-replan-attempt")
	tsk.Metadata = mergeMetadata(tsk.Metadata, map[string]json.RawMessage{
		escalationLevelMetadataKey: json.RawMessage(`2`),
	})
	if err := store.Update(tsk); err != nil {
		t.Fatalf("persist metadata: %v", err)
	}

	// tierForRequest must classify the resulting request as complex via
	// the metadata level alone (single-artifact input would be trivial).
	if got := tierForRequest(PlanRequest{
		Input:         "create a file named notes.md",
		IsReplan:      true,
		ReplanAttempt: replanAttemptFromTask(tsk),
	}); got != TierComplex {
		t.Errorf("tier from metadata level 2 = %q, want TierComplex", got)
	}
}
