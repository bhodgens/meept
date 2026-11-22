package plan

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/kanban"
)

// gatedPlanConfig is the lifecycle test config: explicit approval (so
// SubmitPlan parks at pending_approval instead of auto-expanding), revision
// allowed, signoff required — the full operator surface the publishers ride.
func gatedPlanConfig() config.PlansConfig {
	return config.PlansConfig{
		Mode: "threshold",
		Threshold: config.PlansThresholdConfig{
			MinSteps:           3,
			ComplexityKeywords: []string{"refactor"},
			AlwaysPlanIntents:  []string{"plan"},
		},
		Storage: config.PlansStorageConfig{
			DefaultPath:      "docs/plans",
			FilenameTemplate: "{{slug}}.md",
		},
		Approval: config.PlansApprovalConfig{
			RequireApproval: true,
			AllowRevision:   true,
			MaxRevisions:    3,
		},
		Confirmation: config.PlansConfirmationConfig{RequireSignoff: true},
	}
}

// newGatedPlanManager returns a PlanManager over a temp SQLite store with a
// live bus, a working counting task creator, and the kanban StateEvent gate
// ARMED, plus a raw subscriber on the plan-state topic (raw so the test
// owns the channel — SubscribeT's reader goroutine would race it). Mirrors
// internal/queue/kanban_events_test.go's newGatedQueue shape (leaf 02/04).
func newGatedPlanManager(t *testing.T, creator TaskCreator) (*PlanManager, *bus.Subscriber) {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "kanban-events.db"), logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	msgBus := bus.New(nil, logger)
	mgr := NewPlanManager(store, msgBus, gatedPlanConfig(), creator, logger)
	sub := msgBus.Subscribe("kanban-plan-events-test", kanban.TopicPlanState.Name)
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })
	mgr.SetKanbanEventPublishing(true)
	return mgr, sub
}

func drainPlanEvents(t *testing.T, sub *bus.Subscriber, n int) []kanban.StateEvent {
	t.Helper()
	out := make([]kanban.StateEvent, 0, n)
	deadline := time.After(2 * time.Second)
	for len(out) < n {
		select {
		case m, ok := <-sub.Channel:
			if !ok {
				t.Fatalf("channel closed after %d/%d events", len(out), n)
			}
			var ev kanban.StateEvent
			if err := json.Unmarshal(m.Payload, &ev); err != nil {
				t.Fatalf("decode StateEvent: %v", err)
			}
			out = append(out, ev)
		case <-deadline:
			t.Fatalf("timed out after %d/%d events", len(out), n)
		}
	}
	return out
}

func expectNoPlanEvent(t *testing.T, sub *bus.Subscriber) {
	t.Helper()
	select {
	case m := <-sub.Channel:
		t.Fatalf("unexpected plan StateEvent: %s", m.Payload)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestPlanTransitions_EveryEdgeHasPublisherAndReason walks planTransitions —
// the machine authority (C-P4) — and asserts every legal edge is reachable
// through a wired publisher path AND carries a non-empty StateEvent.Reason.
// The edge -> path map mirrors the wiring: transitionFrom/transition publish
// every table edge they are invoked for; the create write, the synthesis
// claim/release/park primitives, and the reclaimer cover the rest. A table
// edge nobody publishes is drift this test exists to catch.
func TestPlanTransitions_EveryEdgeHasPublisherAndReason(t *testing.T) {
	// Edge -> publisher path. Every from->to below names the call site
	// that fires publishPlanState for it (grep-verifiable in manager.go /
	// transitions.go).
	publisherPaths := map[PlanState]string{
		StatePlanning:        "RevisePlan transitionFrom",
		StateDraft:           "CreatePlan write + CompletePlanningRevision/claim-release transitionFrom",
		StatePendingApproval: "SubmitPlan transitionFrom",
		StateApproved:        "ApprovePlan transitionFrom + reclaimer reclaim",
		StateSynthesizing:    "Synthesize ClaimPlanSynthesis",
		StateExecuting:       "Synthesize success write",
		StateCompleted:       "completePlan transition",
		StateConfirmed:       "ConfirmPlan transition",
		StateCancelled:       "RejectPlan/CancelPlan transitionFrom/transition",
		StateFailed:          "failPlan transition + ParkPlanSynthesis",
	}
	edges := 0
	for from, tos := range planTransitions {
		if _, ok := publisherPaths[from]; !ok {
			t.Errorf("from-state %q missing from the publisher-path map", from)
		}
		for _, to := range tos {
			edges++
			if r := planEventReason(from, to); r == "" {
				t.Errorf("edge %s -> %s resolves to an empty StateEvent.Reason", from, to)
			}
			if _, ok := publisherPaths[to]; !ok {
				t.Errorf("edge %s -> %s targets a state with no publisher path", from, to)
			}
		}
	}
	if len(planTransitions) != len(publisherPaths) {
		t.Errorf("publisher-path map (%d) does not cover the table rows (%d)",
			len(publisherPaths), len(planTransitions))
	}
	if edges == 0 {
		t.Fatal("empty transition table — the walk proves nothing")
	}
}

// TestPlanPublish_LifecycleEdges drives the operator lifecycle through the
// real methods and pins each edge's From/To/Reason triple.
func TestPlanPublish_LifecycleEdges(t *testing.T) {
	ctx := context.Background()
	mgr, sub := newGatedPlanManager(t, &countingCreator{})

	p, err := mgr.CreatePlan(ctx, "kanban lifecycle", "d", "", t.TempDir(), "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	got := drainPlanEvents(t, sub, 1)
	if e := got[0]; e.Kind != kanban.EventKindPlan || e.ID != p.ID ||
		e.From != string(StatePlanning) || e.To != string(StateDraft) ||
		e.Reason != kanban.EventReasonCreate {
		t.Fatalf("create event: %+v", e)
	}

	if err := mgr.SubmitPlan(ctx, p.ID); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	e := drainPlanEvents(t, sub, 1)[0]
	if e.From != string(StateDraft) || e.To != string(StatePendingApproval) || e.Reason != kanban.EventReasonSubmit {
		t.Fatalf("submit event: %+v", e)
	}

	// ApprovePlan fires the approve edge and then Synthesize: claim
	// approved->synthesizing and the executing write, in one tail of events.
	// The lifecycle test expands a REAL phase: seed one so synthesis takes
	// the expansion path (leaf 10's zero-phase guard completes phase-less
	// plans honestly instead of synthesizing tasks).
	seedExpandablePhase(t, mgr, p.ID)
	if err := mgr.ApprovePlan(ctx, p.ID, "", "tester"); err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}
	trio := drainPlanEvents(t, sub, 3)
	want := [][4]string{
		{string(StatePendingApproval), string(StateApproved), kanban.EventReasonApprove, ""},
		{string(StateApproved), string(StateSynthesizing), kanban.EventReasonSynthesize, ""},
		{string(StateSynthesizing), string(StateExecuting), kanban.EventReasonExecuting, ""},
	}
	for i, w := range want {
		ev := trio[i]
		if ev.From != w[0] || ev.To != w[1] || ev.Reason != w[2] {
			t.Fatalf("approve-chain event %d: %+v want %v->%v reason %s", i, ev, w[0], w[1], w[2])
		}
	}

	// executing -> completed -> confirmed.
	if err := mgr.transition(ctx, p.ID, StateCompleted, kanban.EventReasonComplete); err != nil {
		t.Fatalf("transition completed: %v", err)
	}
	ev := drainPlanEvents(t, sub, 1)[0]
	if ev.From != string(StateExecuting) || ev.To != string(StateCompleted) || ev.Reason != kanban.EventReasonComplete {
		t.Fatalf("complete event: %+v", ev)
	}
	if err := mgr.ConfirmPlan(ctx, p.ID, "", "tester"); err != nil {
		t.Fatalf("ConfirmPlan: %v", err)
	}
	ev = drainPlanEvents(t, sub, 1)[0]
	if ev.From != string(StateCompleted) || ev.To != string(StateConfirmed) || ev.Reason != kanban.EventReasonConfirm {
		t.Fatalf("confirm event: %+v", ev)
	}
	// confirmed is terminal — a refused transition publishes nothing.
	if err := mgr.transition(ctx, p.ID, StateCancelled); err == nil {
		t.Fatal("expected terminal refusal")
	}
	expectNoPlanEvent(t, sub)
}

// TestPlanPublish_RejectReviseRework drives the reject and the revision
// rework loop (pending_approval -> cancelled / -> planning / planning ->
// draft).
func TestPlanPublish_RejectReviseRework(t *testing.T) {
	ctx := context.Background()
	mgr, sub := newGatedPlanManager(t, nil)

	p, err := mgr.CreatePlan(ctx, "kanban reject", "d", "", t.TempDir(), "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	drainPlanEvents(t, sub, 1)
	if err := mgr.SubmitPlan(ctx, p.ID); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	drainPlanEvents(t, sub, 1)

	if err := mgr.RevisePlan(ctx, p.ID, "", "more detail"); err != nil {
		t.Fatalf("RevisePlan: %v", err)
	}
	ev := drainPlanEvents(t, sub, 1)[0]
	if ev.From != string(StatePendingApproval) || ev.To != string(StatePlanning) || ev.Reason != kanban.EventReasonRevise {
		t.Fatalf("revise event: %+v", ev)
	}

	if err := mgr.CompletePlanningRevision(ctx, p.ID); err != nil {
		t.Fatalf("CompletePlanningRevision: %v", err)
	}
	ev = drainPlanEvents(t, sub, 1)[0]
	if ev.From != string(StatePlanning) || ev.To != string(StateDraft) || ev.Reason != kanban.EventReasonRevisionDone {
		t.Fatalf("revision-done event: %+v", ev)
	}

	// Resubmit and reject.
	if err := mgr.SubmitPlan(ctx, p.ID); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	drainPlanEvents(t, sub, 1)
	if err := mgr.RejectPlan(ctx, p.ID, "", "boss", "not now"); err != nil {
		t.Fatalf("RejectPlan: %v", err)
	}
	ev = drainPlanEvents(t, sub, 1)[0]
	if ev.From != string(StatePendingApproval) || ev.To != string(StateCancelled) || ev.Reason != kanban.EventReasonReject {
		t.Fatalf("reject event: %+v", ev)
	}

	// CancelPlan on a revisable state: cancelled -> planning via Revise
	// needs no re-submit; instead drive executing's cancel edge on a fresh
	// plan via the generic transition (derived reason = cancel).
	p2, err := mgr.CreatePlan(ctx, "kanban cancel", "d", "", t.TempDir(), "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	drainPlanEvents(t, sub, 1)
	if err := mgr.CancelPlan(ctx, p2.ID, "operator"); err != nil {
		t.Fatalf("CancelPlan: %v", err)
	}
	ev = drainPlanEvents(t, sub, 1)[0]
	if ev.From != string(StateDraft) || ev.To != string(StateCancelled) || ev.Reason != kanban.EventReasonCancel {
		t.Fatalf("cancel event: %+v", ev)
	}
}

// TestPlanPublish_SynthesisReleaseAndPark drives the failed-expansion lease
// exits: release (synthesizing -> draft and synthesizing -> approved) and
// park (synthesizing -> failed).
func TestPlanPublish_SynthesisReleaseAndPark(t *testing.T) {
	ctx := context.Background()

	// Release path, draft origin: a creator that fails every parent create.
	creator := &countingCreator{alwaysFailPar: true}
	mgr, sub := newGatedPlanManager(t, creator)
	p, err := mgr.CreatePlan(ctx, "release path", "d", "", t.TempDir(), "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	seedExpandablePhase(t, mgr, p.ID)
	drainPlanEvents(t, sub, 1) // planning -> draft (create)
	if err := mgr.Synthesize(ctx, p.ID); err == nil {
		t.Fatal("expected synthesis failure")
	}
	pair := drainPlanEvents(t, sub, 2)
	if e := pair[0]; e.From != string(StateDraft) || e.To != string(StateSynthesizing) || e.Reason != kanban.EventReasonSynthesize {
		t.Fatalf("claim (draft origin) event: %+v", e)
	}
	if e := pair[1]; e.From != string(StateSynthesizing) || e.To != string(StateDraft) || e.Reason != kanban.EventReasonRelease {
		t.Fatalf("release (draft origin) event: %+v", e)
	}

	// Release path, approved origin: the claim comes back to approved, not
	// draft (the approval must stand — releaseSynthesisTo branch).
	if err := mgr.store.SetPlanState(ctx, p.ID, StateApproved); err != nil {
		t.Fatalf("SetPlanState: %v", err)
	}
	if err := mgr.Synthesize(ctx, p.ID); err == nil {
		t.Fatal("expected synthesis failure")
	}
	pair = drainPlanEvents(t, sub, 2)
	if e := pair[0]; e.From != string(StateApproved) || e.To != string(StateSynthesizing) || e.Reason != kanban.EventReasonSynthesize {
		t.Fatalf("claim (approved origin) event: %+v", e)
	}
	if e := pair[1]; e.From != string(StateSynthesizing) || e.To != string(StateApproved) || e.Reason != kanban.EventReasonRelease {
		t.Fatalf("release (approved origin) event: %+v", e)
	}

	// Park path: attempts pre-set to the bound minus one, so the deferred
	// release decides park (synthesizing -> failed).
	mgr2, sub2 := newGatedPlanManager(t, &countingCreator{alwaysFailPar: true})
	p2, err := mgr2.CreatePlan(ctx, "park path", "d", "", t.TempDir(), "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	seedExpandablePhase(t, mgr2, p2.ID)
	drainPlanEvents(t, sub2, 1)
	if _, err := mgr2.store.(*SQLiteStore).DB().ExecContext(ctx,
		"UPDATE plans SET synthesis_attempts = ? WHERE id = ?", MaxSynthesisAttempts-1, p2.ID); err != nil {
		t.Fatalf("seed attempts: %v", err)
	}
	if err := mgr2.Synthesize(ctx, p2.ID); err == nil {
		t.Fatal("expected synthesis failure")
	}
	pair = drainPlanEvents(t, sub2, 2)
	if e := pair[1]; e.From != string(StateSynthesizing) || e.To != string(StateFailed) || e.Reason != kanban.EventReasonPark {
		t.Fatalf("park event: %+v", e)
	}
}

// TestPlanPublish_Reclaim drives the expired-lease reclaim
// (synthesizing -> approved) through RecoverStrandedSynthesis.
func TestPlanPublish_Reclaim(t *testing.T) {
	ctx := context.Background()
	mgr, sub := newGatedPlanManager(t, nil)

	p, err := mgr.CreatePlan(ctx, "stranded", "d", "", t.TempDir(), "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	drainPlanEvents(t, sub, 1)
	// Strand it: synthesizing with no task and no lease is reclaimable.
	if err := mgr.store.SetPlanState(ctx, p.ID, StateSynthesizing); err != nil {
		t.Fatalf("SetPlanState: %v", err)
	}
	n, err := mgr.RecoverStrandedSynthesis(ctx)
	if err != nil {
		t.Fatalf("RecoverStrandedSynthesis: %v", err)
	}
	if n != 1 {
		t.Fatalf("recovered = %d, want 1", n)
	}
	ev := drainPlanEvents(t, sub, 1)[0]
	if ev.From != string(StateSynthesizing) || ev.To != string(StateApproved) || ev.Reason != kanban.EventReasonReclaim {
		t.Fatalf("reclaim event: %+v", ev)
	}
}

// TestPlanPublish_GateOffIsSilent pins the opt-in default: with the gate
// unarmed the lifecycle runs but kanban.event.plan.state sees nothing.
func TestPlanPublish_GateOffIsSilent(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.DiscardHandler)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "gate-off.db"), logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	msgBus := bus.New(nil, logger)
	mgr := NewPlanManager(store, msgBus, gatedPlanConfig(), nil, logger)
	sub := msgBus.Subscribe("gate-off-plan", kanban.TopicPlanState.Name)
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })
	// Gate NOT armed — the default.

	p, err := mgr.CreatePlan(ctx, "quiet", "d", "", t.TempDir(), "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.SubmitPlan(ctx, p.ID); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	expectNoPlanEvent(t, sub)
}
