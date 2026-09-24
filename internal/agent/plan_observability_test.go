package agent

// Leaf-04 observability pins (tiered-iteration: observability leaf).
//
//   - Log line: exactly ONE structured "plan tier selected" line per
//     plan request, carrying tier + task_id (+ replan_attempt) — the
//     human/audit view of the tier decision the strategic_planner.tier
//     metric counts.
//   - Fallback metric coverage: every tier_complex_fallback site emits
//     a reason label, and the transport-degradation reasons
//     (draft_transport / revise_transport) fire from the critique loop
//     on planner transport failure.
//   - Critique summary surfacing: after the flow, the draft presented
//     to the human reviewer carries critique_rounds_used,
//     known_risks_count, and critique_outcome — on the draft bag
//     itself (so plan.draft get / the seal request show the verdict)
//     and on the task metadata companions.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

// captureLogger returns an slog.Logger writing into buf.
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// TestPlanTierSelected_LogLineOncePerRequest pins the leaf-04 log line:
// a quick_plan Plan() request emits exactly one "plan tier selected"
// line, with the tier and task_id attributes.
func TestPlanTierSelected_LogLineOncePerRequest(t *testing.T) {
	var buf bytes.Buffer
	goodPlan := `{"steps": [{"description": "step one"}]}`
	chatter := &repairCaptureChatter{resps: []string{goodPlan}}
	sp := newPlanRepairTestPlanner(t, chatter)
	sp.logger = captureLogger(&buf)

	tsk := newTestTask("task-tier-log", "create a file named notes.md")
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

	logLineCount(t, &buf, "plan tier selected", tsk.ID, "trivial")
}

// TestPlanTierSelected_LogLinePlanMode pins the same line on the plan
// mode path (compiler flag on): one line, tier + task_id.
func TestPlanTierSelected_LogLinePlanMode(t *testing.T) {
	var buf bytes.Buffer
	sp, store := newDraftTestPlanner(t)
	sp.SetPlanCompilerEnabled(true)
	sp.logger = captureLogger(&buf)

	tsk := seedDraftTask(t, store, "task-tier-log-plan")
	if err := sp.Plan(context.Background(), PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-tier-log",
		Input:     "Build avatar upload with local storage",
		Intent:    "code",
		Mode:      "plan",
	}); err != nil {
		t.Fatalf("Plan: %v", err)
	}

	logLineCount(t, &buf, "plan tier selected", tsk.ID, "standard")
}

// logLineCount asserts exactly one msg line mentioning taskID and the
// tier value exists in buf.
func logLineCount(t *testing.T, buf *bytes.Buffer, msg, taskID, tier string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	hits := 0
	for _, ln := range lines {
		if strings.Contains(ln, "msg=\""+msg+"\"") &&
			strings.Contains(ln, "task_id="+taskID) &&
			strings.Contains(ln, "tier="+tier) {
			hits++
		}
	}
	if hits != 1 {
		t.Errorf("%q lines mentioning task_id=%s tier=%s = %d, want exactly 1; log:\n%s",
			msg, taskID, tier, hits, buf.String())
	}
}

// TestCritiqueFlow_TransportFallbackEmitsReason pins the leaf-04
// fallback-label coverage: a draft-call transport failure in the
// critique loop records tier_complex_fallback with a transport reason
// (draft_transport).
func TestCritiqueFlow_TransportFallbackEmitsReason(t *testing.T) {
	store := newCritiqueMetricStore(t)
	chatter := &repairCaptureChatter{
		resps: []string{"irrelevant"},
		errs:  []error{context.DeadlineExceeded},
	}
	sp := newCritiqueTestPlanner(t, chatter, true, 2)
	sp.metricsStore = store
	tsk := seedCritiqueTask(t, sp, "task-obs-transport")

	res, err := sp.PlanCritiqueFlow(context.Background(), critiqueFlowRequest(tsk, "quick_plan"), &PlanCritiqueInput{})
	if err != nil {
		t.Fatalf("PlanCritiqueFlow: %v", err)
	}
	if res.Action != critiqueActionFallback {
		t.Fatalf("action = %q, want %q", res.Action, critiqueActionFallback)
	}
	if got := critiqueMetricTotal(t, store, "strategic_planner.tier_complex_fallback", map[string]string{"reason": "draft_transport"}); got != 1 {
		t.Errorf("tier_complex_fallback{reason=draft_transport} = %d, want 1", got)
	}
}

// TestCritiqueFlow_ReviseTransportFallbackEmitsReason pins the revise
// half of the transport coverage: the draft succeeds, the critic
// returns blocking (forcing a revise), the revise call transport-fails
// → tier_complex_fallback{reason=revise_transport}.
func TestCritiqueFlow_ReviseTransportFallbackEmitsReason(t *testing.T) {
	store := newCritiqueMetricStore(t)
	chatter := &repairCaptureChatter{
		resps: []string{
			compileableDraft("Avatar upload"),                                 // draft fill
			objectionsJSON("blocking|## Phases|phase 1 lacks rollback steps"), // critic: blocking → revise
		},
		errs: []error{nil, nil, context.DeadlineExceeded}, // revise call fails
	}
	sp := newCritiqueTestPlanner(t, chatter, true, 2)
	sp.metricsStore = store
	tsk := seedCritiqueTask(t, sp, "task-obs-revise")

	res, err := sp.PlanCritiqueFlow(context.Background(), critiqueFlowRequest(tsk, "quick_plan"), &PlanCritiqueInput{})
	if err != nil {
		t.Fatalf("PlanCritiqueFlow: %v", err)
	}
	if res.Action != critiqueActionFallback {
		t.Fatalf("action = %q, want %q", res.Action, critiqueActionFallback)
	}
	if got := critiqueMetricTotal(t, store, "strategic_planner.tier_complex_fallback", map[string]string{"reason": "revise_transport"}); got != 1 {
		t.Errorf("tier_complex_fallback{reason=revise_transport} = %d, want 1", got)
	}
}

// TestCritiqueFlow_FallbackReasonsComplete pins the leaf-04 label
// contract: the fallback metric's reason vocabulary is exactly
// {flow_disabled, draft_transport, revise_transport} across the code —
// an unnamed or renamed reason breaks the metric's label completeness.
func TestCritiqueFlow_FallbackReasonsComplete(t *testing.T) {
	// Scan the source for every tier_complex_fallback recordMetric call
	// and extract its reason literal. The metric is defined by exactly
	// the leaf doc's label set plus the store-level flow_error.
	reasons, err := fallbackReasonsInSource()
	if err != nil {
		t.Fatalf("scan sources: %v", err)
	}
	want := map[string]bool{
		"flow_disabled":    true, // Plan(): flag off (leaf 01)
		"draft_transport":  true, // critique loop: draft call failed
		"revise_transport": true, // critique loop: revise call failed
		"flow_error":       true, // Plan(): runCritiqueFlowForPlan returned an error
	}
	if len(reasons) == 0 {
		t.Fatal("no tier_complex_fallback emit sites found; the fallback metric sites vanished")
	}
	for _, r := range reasons {
		if !want[r] {
			t.Errorf("unexpected tier_complex_fallback reason %q; allowed: %v", r, keysOf(want))
		}
	}
	// Every documented reason must have at least one emit site.
	found := map[string]bool{}
	for _, r := range reasons {
		found[r] = true
	}
	for r := range want {
		if !found[r] {
			t.Errorf("documented tier_complex_fallback reason %q has no emit site", r)
		}
	}
}

// fallbackReasonsInSource scans the production sources for every
// tier_complex_fallback recordMetric site and extracts its reason
// literal, so a renamed, removed, or added label turns the pin red.
// Paths are resolved relative to the package dir (go test runs there).
func fallbackReasonsInSource() ([]string, error) {
	files := []string{"strategic.go", "plan_critique_loop.go"}
	marker := "strategic_planner.tier_complex_fallback"
	var reasons []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.Contains(line, marker) {
				continue
			}
			idx := strings.Index(line, `"reason": "`)
			if idx < 0 {
				continue
			}
			rest := line[idx+len(`"reason": "`):]
			end := strings.Index(rest, `"`)
			if end < 0 {
				continue
			}
			reasons = append(reasons, rest[:end])
		}
	}
	return reasons, nil
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestCritiqueFlow_CritiqueSummaryOnDraft pins the leaf-04 surfacing
// contract: after the flow, the persisted draft carries
// critique_rounds_used, known_risks_count, and critique_outcome —
// both on the draft bag (what plan.draft get presents) and as task
// metadata companions.
func TestCritiqueFlow_CritiqueSummaryOnDraft(t *testing.T) {
	store := newCritiqueMetricStore(t)
	chatter := &repairCaptureChatter{
		resps: []string{
			compileableDraft("Avatar upload"),                       // draft fill
			objectionsJSON("advisory|## Decisions|add a benchmark"), // critic round 1: clean; advisory → Known Risks
		},
	}
	sp := newCritiqueTestPlanner(t, chatter, true, 2)
	sp.metricsStore = store
	tsk := seedCritiqueTask(t, sp, "task-obs-summary")

	res, err := sp.PlanCritiqueFlow(context.Background(), critiqueFlowRequest(tsk, "quick_plan"), &PlanCritiqueInput{})
	if err != nil {
		t.Fatalf("PlanCritiqueFlow: %v", err)
	}
	if res.Action != critiqueActionHandled {
		t.Fatalf("action = %q, want %q", res.Action, critiqueActionHandled)
	}
	if res.RoundsUsed != 1 {
		t.Errorf("rounds_used = %d, want 1", res.RoundsUsed)
	}

	fresh, err := sp.taskStore.GetByID(tsk.ID)
	if err != nil || fresh == nil {
		t.Fatalf("GetByID: %v", err)
	}

	// The draft bag carries the summary — this is what the interactive
	// plan.draft get surface reads.
	d, ok := draftFromMetadataForTest(t, fresh)
	if !ok {
		t.Fatal("no draft on task after flow")
	}
	if d.CritiqueRoundsUsed != 1 {
		t.Errorf("draft.CritiqueRoundsUsed = %d, want 1", d.CritiqueRoundsUsed)
	}
	if d.KnownRisksCount != 1 {
		t.Errorf("draft.KnownRisksCount = %d, want 1 (the advisory objection)", d.KnownRisksCount)
	}
	if d.CritiqueOutcome != critiqueOutcomeClean {
		t.Errorf("draft.CritiqueOutcome = %q, want %q", d.CritiqueOutcome, critiqueOutcomeClean)
	}

	// The metadata companions agree.
	if got := metadataIntForTest(t, fresh, critiqueRoundsUsedMetadataKey); got != 1 {
		t.Errorf("metadata %s = %d, want 1", critiqueRoundsUsedMetadataKey, got)
	}
	if got := metadataIntForTest(t, fresh, knownRisksCountMetadataKey); got != 1 {
		t.Errorf("metadata %s = %d, want 1", knownRisksCountMetadataKey, got)
	}
	if got := metadataStringForTest(t, fresh, critiqueOutcomeMetadataKey); got != critiqueOutcomeClean {
		t.Errorf("metadata %s = %q, want %q", critiqueOutcomeMetadataKey, got, critiqueOutcomeClean)
	}
}

// metadataIntForTest returns an int-valued metadata entry (0 when
// absent/unparsable).
func metadataIntForTest(t *testing.T, tsk *task.Task, key string) int {
	t.Helper()
	raw, ok := metadataRawForTest(t, tsk, key)
	if !ok {
		return 0
	}
	var n int
	if json.Unmarshal(raw, &n) != nil {
		return 0
	}
	return n
}
