package employee

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
)

// testAuditStoreHelper returns a fresh AuditStore backed by a temp-file
// SQLite DB. The store is closed on test cleanup.
func testAuditStoreHelper(t *testing.T) *AuditStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	store, err := NewAuditStore(dbPath)
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// ---------------------------------------------------------------------------
// G5: employee.goal.health metric emission tests
// ---------------------------------------------------------------------------

// metricCapture is a minimal EmitMetricFunc that records calls for assertion.
type metricCapture struct {
	mu     sync.Mutex
	calls  []metricCall
}

type metricCall struct {
	name  string
	value float64
	tags  map[string]string
}

func (c *metricCapture) fn() EmitMetricFunc {
	return func(name string, value float64, tags map[string]string) {
		c.mu.Lock()
		defer c.mu.Unlock()
		// Defensive copy of tags so the test sees a stable snapshot.
		tagCopy := make(map[string]string, len(tags))
		for k, v := range tags {
			tagCopy[k] = v
		}
		c.calls = append(c.calls, metricCall{name: name, value: value, tags: tagCopy})
	}
}

func (c *metricCapture) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func (c *metricCapture) get(idx int) metricCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	if idx < 0 || idx >= len(c.calls) {
		return metricCall{}
	}
	return c.calls[idx]
}

// TestReflect_EmitsGoalHealthMetric_NoStore verifies that Reflect does NOT
// emit the metric when no GoalStore is configured (no goal to attach to).
func TestReflect_EmitsGoalHealthMetric_NoStore(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{"health":"healthy","reasoning":"CI is green"}`)
	capture := &metricCapture{}

	loop := NewGoalLoop("emp-metric-test", testTier2Constitution(), nil, nil).
		WithReflector(reflector)
	loop.SetEmitMetricFunc(capture.fn())

	result := &bot.BotExecutionResult{
		BotID:   "emp-metric-test",
		Output:  "tests passed",
		Success: true,
	}
	_, err := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result)
	if err != nil {
		t.Fatalf("Reflect error: %v", err)
	}

	// Without a GoalStore, no goal is updated, so no metric is emitted.
	if capture.len() != 0 {
		t.Errorf("expected 0 metric calls without goalStore, got %d", capture.len())
	}
}

// TestReflect_EmitsGoalHealthMetric_WithGoalStore verifies that Reflect
// emits the employee.goal.health metric when a GoalStore with an active goal
// is configured, tagged with the correct goal_id and employee_id.
func TestReflect_EmitsGoalHealthMetric_WithGoalStore(t *testing.T) {
	store := testGoalStore(t)
	seedBot(t, store, "emp-metric-2")

	goal := &Goal{
		ID:         "goal_metric_test",
		EmployeeID: "emp-metric-2",
		Title:      "keep CI green",
		Mandate:    "ensure tests pass",
		State:      GoalActive,
		Source:     SourceUser,
		Health:     GoalUnknown,
	}
	if err := store.Create(context.Background(), goal); err != nil {
		t.Fatalf("Create goal: %v", err)
	}

	reflector := newStubReflector()
	reflector.queueResponse(`{"health":"at_risk","reasoning":"flaky"}`)
	capture := &metricCapture{}

	loop := NewGoalLoop("emp-metric-2", testTier2Constitution(), store, nil).
		WithReflector(reflector)
	loop.SetEmitMetricFunc(capture.fn())

	result := &bot.BotExecutionResult{
		BotID:   "emp-metric-2",
		Output:  "some tests failed",
		Success: true,
	}
	health, err := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result)
	if err != nil {
		t.Fatalf("Reflect error: %v", err)
	}
	if health != GoalAtRisk {
		t.Fatalf("health = %s, want at_risk", health.String())
	}

	if capture.len() != 1 {
		t.Fatalf("expected 1 metric call, got %d", capture.len())
	}
	call := capture.get(0)
	if call.name != "employee.goal.health" {
		t.Errorf("metric name = %q, want %q", call.name, "employee.goal.health")
	}
	if call.value != float64(GoalAtRisk) {
		t.Errorf("metric value = %v, want %v (GoalAtRisk=%d)", call.value, float64(GoalAtRisk), GoalAtRisk)
	}
	if call.tags["goal_id"] != "goal_metric_test" {
		t.Errorf("tag goal_id = %q, want %q", call.tags["goal_id"], "goal_metric_test")
	}
	if call.tags["employee_id"] != "emp-metric-2" {
		t.Errorf("tag employee_id = %q, want %q", call.tags["employee_id"], "emp-metric-2")
	}
}

// TestReflect_EmitsGoalHealthMetric_Broken verifies the metric value for
// GoalBroken (2) after consecutive failures hit the threshold.
func TestReflect_EmitsGoalHealthMetric_Broken(t *testing.T) {
	store := testGoalStore(t)
	seedBot(t, store, "emp-broken")

	goal := &Goal{
		ID:         "goal_broken_test",
		EmployeeID: "emp-broken",
		Title:      "mission",
		Mandate:    "stay alive",
		State:      GoalActive,
		Source:     SourceUser,
	}
	if err := store.Create(context.Background(), goal); err != nil {
		t.Fatalf("Create goal: %v", err)
	}

	capture := &metricCapture{}
	recorder := &pauseRecorder{}
	loop := NewGoalLoop("emp-broken", testTier2Constitution(), store, nil).
		WithReflector(newStubReflector()).
		WithPauseFunc(recorder.fn()).
		WithMaxConsecutiveFailures(2)
	loop.SetEmitMetricFunc(capture.fn())

	fail := &bot.BotExecutionResult{Success: false, Error: "dead"}

	// Fail 1: at_risk (value 1)
	loop.Reflect(context.Background(), PlanRef{ID: "p1"}, fail)
	c1 := capture.get(0)
	if c1.value != float64(GoalAtRisk) {
		t.Errorf("failure 1 metric value = %v, want %v", c1.value, float64(GoalAtRisk))
	}

	// Fail 2: broken (value 2)
	loop.Reflect(context.Background(), PlanRef{ID: "p1"}, fail)
	c2 := capture.get(1)
	if c2.value != float64(GoalBroken) {
		t.Errorf("failure 2 metric value = %v, want %v (GoalBroken=%d)", c2.value, float64(GoalBroken), GoalBroken)
	}
	if c2.tags["goal_id"] != "goal_broken_test" {
		t.Errorf("failure 2 tag goal_id = %q, want %q", c2.tags["goal_id"], "goal_broken_test")
	}
}

// TestGoalHealth_EnumValues verifies that the GoalHealth enum values match
// the spec (0/1/2/3 for healthy/at_risk/broken/unknown).
func TestGoalHealth_EnumValues(t *testing.T) {
	if GoalHealthy != 0 {
		t.Errorf("GoalHealthy = %d, want 0", GoalHealthy)
	}
	if GoalAtRisk != 1 {
		t.Errorf("GoalAtRisk = %d, want 1", GoalAtRisk)
	}
	if GoalBroken != 2 {
		t.Errorf("GoalBroken = %d, want 2", GoalBroken)
	}
	if GoalUnknown != 3 {
		t.Errorf("GoalUnknown = %d, want 3", GoalUnknown)
	}
}

// ---------------------------------------------------------------------------
// G6: AuditStore.PruneOlderThan tests
// ---------------------------------------------------------------------------

// TestPruneOlderThan_RemovesOldFindings verifies that findings older than
// the retention period are deleted, and recent ones are kept.
func TestPruneOlderThan_RemovesOldFindings(t *testing.T) {
	store := testAuditStoreHelper(t)

	now := time.Now().UTC()

	// Create old findings (100 and 95 days ago) and recent findings.
	old1 := AuditFinding{
		ID: "audit_prune_old1", EmployeeID: "e1",
		Severity: SeverityWarning, Checkpoint: CheckpointPostTurn,
		DetectedAt: now.Add(-100 * 24 * time.Hour),
	}
	old2 := AuditFinding{
		ID: "audit_prune_old2", EmployeeID: "e1",
		Severity: SeverityCritical, Checkpoint: CheckpointPreExec,
		DetectedAt: now.Add(-95 * 24 * time.Hour),
	}
	recent1 := AuditFinding{
		ID: "audit_prune_recent1", EmployeeID: "e1",
		Severity: SeverityInfo, Checkpoint: CheckpointPostTurn,
		DetectedAt: now.Add(-10 * 24 * time.Hour),
	}
	recent2 := AuditFinding{
		ID: "audit_prune_recent2", EmployeeID: "e1",
		Severity: SeverityWarning, Checkpoint: CheckpointPeriodic,
		DetectedAt: now.Add(-1 * 24 * time.Hour),
	}
	for _, f := range []AuditFinding{old1, old2, recent1, recent2} {
		if err := store.Create(context.Background(), f); err != nil {
			t.Fatalf("Create finding %s: %v", f.ID, err)
		}
	}

	// Prune with 90 day retention.
	pruned, err := store.PruneOlderThan(context.Background(), 90)
	if err != nil {
		t.Fatalf("PruneOlderThan: %v", err)
	}
	if pruned != 2 {
		t.Errorf("pruned count = %d, want 2", pruned)
	}

	// Verify only recent findings remain.
	findings, err := store.List(context.Background(), AuditListFilter{Limit: 100})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("remaining findings = %d, want 2", len(findings))
	}
	// List is newest-first.
	if findings[0].ID != "audit_prune_recent2" {
		t.Errorf("first remaining = %q, want audit_prune_recent2", findings[0].ID)
	}
	if findings[1].ID != "audit_prune_recent1" {
		t.Errorf("second remaining = %q, want audit_prune_recent1", findings[1].ID)
	}
}

// TestPruneOlderThan_ZeroRetention verifies that zero retention is a no-op.
func TestPruneOlderThan_ZeroRetention(t *testing.T) {
	store := testAuditStoreHelper(t)

	store.Create(context.Background(), AuditFinding{
		ID: "audit_zero_1", EmployeeID: "e1",
		Severity: SeverityInfo, Checkpoint: CheckpointPostTurn,
		DetectedAt: time.Now().UTC().Add(-200 * 24 * time.Hour),
	})

	pruned, err := store.PruneOlderThan(context.Background(), 0)
	if err != nil {
		t.Fatalf("PruneOlderThan(0): %v", err)
	}
	if pruned != 0 {
		t.Errorf("pruned with retention=0: %d, want 0", pruned)
	}
}

// ---------------------------------------------------------------------------
// G7: Goal.AttachFinding tests
// ---------------------------------------------------------------------------

// TestAttachFinding_AppendsAndCaps verifies that AttachFinding appends
// finding IDs and respects the recentFindingsMax cap.
func TestAttachFinding_AppendsAndCaps(t *testing.T) {
	g := &Goal{ID: "g1", EmployeeID: "e1"}

	// Attach a few findings.
	g.AttachFinding("finding_1")
	g.AttachFinding("finding_2")
	g.AttachFinding("finding_3")

	list := g.RecentFindingsList()
	if len(list) != 3 {
		t.Fatalf("len = %d, want 3", len(list))
	}
	if list[0] != "finding_1" || list[2] != "finding_3" {
		t.Errorf("list order = %v, want [finding_1, finding_2, finding_3]", list)
	}

	// Attach enough to exceed the cap.
	for i := 0; i < recentFindingsMax+10; i++ {
		g.AttachFinding("bulk_finding")
	}
	list = g.RecentFindingsList()
	if len(list) != recentFindingsMax {
		t.Errorf("after overflow: len = %d, want %d (cap)", len(list), recentFindingsMax)
	}
}

// TestAttachFinding_EmptyIgnored verifies that empty finding IDs are
// ignored.
func TestAttachFinding_EmptyIgnored(t *testing.T) {
	g := &Goal{ID: "g1", EmployeeID: "e1"}
	g.AttachFinding("")
	if len(g.RecentFindingsList()) != 0 {
		t.Error("empty finding ID should be ignored")
	}
}

// TestAttachFinding_PersistRoundTrip verifies that recent_findings is
// persisted and read back correctly through the GoalStore.
func TestAttachFinding_PersistRoundTrip(t *testing.T) {
	store := testGoalStore(t)
	seedBot(t, store, "e1")

	g := &Goal{
		ID:         "goal_persist_test",
		EmployeeID: "e1",
		Title:      "test goal",
		Mandate:    "test",
		State:      GoalActive,
		Source:     SourceUser,
	}
	g.AttachFinding("finding_a")
	g.AttachFinding("finding_b")

	if err := store.Create(context.Background(), g); err != nil {
		t.Fatalf("Create: %v", err)
	}

	loaded, err := store.Get(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	list := loaded.RecentFindingsList()
	if len(list) != 2 {
		t.Fatalf("loaded findings len = %d, want 2", len(list))
	}
	if list[0] != "finding_a" || list[1] != "finding_b" {
		t.Errorf("loaded findings = %v, want [finding_a, finding_b]", list)
	}

	// Add another finding and Update.
	loaded.AttachFinding("finding_c")
	if err := store.Update(context.Background(), loaded); err != nil {
		t.Fatalf("Update: %v", err)
	}

	loaded2, err := store.Get(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	list2 := loaded2.RecentFindingsList()
	if len(list2) != 3 {
		t.Fatalf("after update: findings len = %d, want 3", len(list2))
	}
}
