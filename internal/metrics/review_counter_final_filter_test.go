package metrics_test

// L1 (bughunt 2026-11-27) pins: the legacy review.* counter lane must count
// FINAL review cycles only. 713b26e4 routes pair rounds and adversarial
// hook verdicts onto the same step.review_completed bus lane with
// is_final=false; before the fix the collector bumped review.completed /
// review.pass_rate / review.revision_rate / review.escalation_rate for
// every round, inflating the counters with superseded rounds and
// re-verifications while the L7 SQL rollups (is_final = 1) stayed honest.
//
// Verdict persistence is explicitly NOT filtered: every round's verdict
// rows persist verbatim (the rollups do their own is_final filtering),
// so the non-final pin also asserts the rows still land.

import (
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/pkg/models"
)

// handleReviewPayload feeds one review payload through the collector's
// step.review_completed path (the review_persist_gate_test.go mechanism).
func handleReviewPayload(t *testing.T, store *metrics.Store, payload map[string]any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	messageBus := bus.New(nil, slog.New(slog.DiscardHandler))
	c := metrics.NewCollector(store, messageBus, metrics.DefaultCollectorConfig())
	defer c.Shutdown()

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "review-manager", nil)
	if err != nil {
		t.Fatalf("NewBusMessage: %v", err)
	}
	msg.Payload = raw
	messageBus.Publish("step.review_completed", msg)
}

// counterSum reads the SUM of a metric's values from metrics_live (the
// counters are recorded via Store.Record -> flush -> metrics_live).
func counterSum(t *testing.T, store *metrics.Store, name string) float64 {
	t.Helper()
	var sum float64
	if err := store.DB().Get(&sum, `SELECT COALESCE(SUM(value), 0) FROM metrics_live WHERE metric_name = ?`, name); err != nil {
		t.Fatalf("sum %s: %v", name, err)
	}
	return sum
}

// reviewCounterFixture is a minimal valid review payload: no verdicts, so
// the assertion isolates the counter lane (any verdicts would add a
// model_review_verdicts write but no counter writes of their own).
func reviewCounterFixture(status string, isFinal bool) map[string]any {
	return map[string]any{
		"status":         status,
		"reviewer":       "code-reviewer",
		"confidence":     0.9,
		"revision_count": 1,
		"step_id":        "step-l1-pin",
		"task_id":        "task-l1-pin",
		"is_final":       isFinal,
	}
}

// TestL1_Collector_NonFinalReviewDoesNotBumpCounters pins the L1 fix: a
// review event with is_final=false must not bump ANY legacy review
// counter (review.completed, pass_rate, revision_rate, escalation_rate,
// revision_cycles), while the same event with is_final=true bumps them
// exactly as before.
func TestL1_Collector_NonFinalReviewDoesNotBumpCounters(t *testing.T) {
	store := newPersistGateStore(t)

	// Three superseded (non-final) rounds: a rejection and a needs-info so
	// both the rejected and needs_info counter branches are exercised.
	handleReviewPayload(t, store, reviewCounterFixture("rejected", false))
	handleReviewPayload(t, store, reviewCounterFixture("needs_info", false))
	handleReviewPayload(t, store, reviewCounterFixture("approved", false))

	for _, name := range []string{
		"review.completed",
		"review.pass_rate",
		"review.revision_rate",
		"review.escalation_rate",
		"review.confidence",
		"review.revision_cycles",
	} {
		if got := counterSum(t, store, name); got != 0 {
			t.Errorf("non-final round bumped %s: sum = %v, want 0", name, got)
		}
	}

	// The final round bumps the counters exactly as the legacy contract:
	// one completed, one pass_rate (approved), one confidence sample.
	handleReviewPayload(t, store, reviewCounterFixture("approved", true))

	if got := counterSum(t, store, "review.completed"); got != 1 {
		t.Errorf("final round review.completed = %v, want 1", got)
	}
	if got := counterSum(t, store, "review.pass_rate"); got != 1 {
		t.Errorf("final round review.pass_rate = %v, want 1", got)
	}
	if got := counterSum(t, store, "review.confidence"); got != 0.9 {
		t.Errorf("final round review.confidence = %v, want 0.9 (one confidence sample)", got)
	}
}

// TestL1_Collector_NonFinalVerdictRowsStillPersist pins the boundary of the
// L1 fix: the filter covers ONLY the counter lane. A non-final round's
// verdict rows must still persist to model_review_verdicts verbatim
// (is_final=false stored as-is) — the L7 rollups honour is_final in SQL,
// so dropping rows here would erase superseded rounds from the table.
func TestL1_Collector_NonFinalVerdictRowsStillPersist(t *testing.T) {
	store := newPersistGateStore(t)

	payload := reviewCounterFixture("rejected", false)
	payload["reviewed_model_id"] = "glm-x"
	payload["judge_agent_id"] = "code-reviewer"
	payload["verdicts"] = []map[string]any{
		{
			"rubric_id":  "",
			"scope":      "model",
			"category":   "incomplete_implementation",
			"severity":   "blocking",
			"detail":     "pair-review rejected round 1",
			"confidence": 0.7,
		},
	}
	handleReviewPayload(t, store, payload)

	rows, err := store.FinalVerdictsForStep("step-l1-pin")
	if err != nil {
		t.Fatalf("FinalVerdictsForStep: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("non-final round persisted %d verdict rows, want 1 (row persistence is NOT filtered by is_final)", len(rows))
	}
	if rows[0].IsFinal {
		t.Errorf("stored is_final = true, want false (verbatim persistence)")
	}
}
