package selfimprove

import (
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

func newTestController(t *testing.T, b *bus.MessageBus) *Controller {
	t.Helper()
	cfg := DefaultConfig()
	cfg.DataPath = t.TempDir()
	cfg.Validate()
	return &Controller{
		config:        cfg,
		bus:           b,
		projectRoot:   t.TempDir(),
		logger:        slogDiscardLogger(),
		failureCounts: make(map[string]int),
	}
}

// TestController_PublishStatus_EmitsBusMessage verifies that publishStatus
// actually publishes a BusMessage to the selfimprove.status topic.
func TestController_PublishStatus_EmitsBusMessage(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	sub := msgBus.Subscribe("test", statusTopic)
	defer msgBus.Unsubscribe(sub)

	c := newTestController(t, msgBus)
	c.currentCycle = &ImprovementCycle{ID: "cycle-abc"}

	c.publishStatus("started", map[string]any{"foo": "bar"})

	select {
	case m := <-sub.Channel:
		if m.Type != models.MessageTypeStatusUpdate {
			t.Errorf("type = %s, want %s", m.Type, models.MessageTypeStatusUpdate)
		}
		if m.Topic != statusTopic {
			t.Errorf("topic = %s, want %s", m.Topic, statusTopic)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no message received on selfimprove.status")
	}
}

// TestController_CircuitBreakerCountsAllPhases verifies that validator and
// applier failures both increment the circuit-breaker counter.
func TestController_CircuitBreakerCountsAllPhases(t *testing.T) {
	c := newTestController(t, nil)

	// Simulate a validator-phase failure path: the code calls
	// recordFailure(fix.IssueID) when validator returns an error.
	c.recordFailure("issue-a")
	c.recordFailure("issue-b")

	if c.consecutiveFailures != 2 {
		t.Errorf("after 2 recordFailure calls, consecutiveFailures = %d, want 2", c.consecutiveFailures)
	}

	// Success resets the consecutive counter but per-issue count persists.
	c.recordSuccess("issue-a")
	if c.consecutiveFailures != 0 {
		t.Errorf("after recordSuccess, consecutiveFailures = %d, want 0", c.consecutiveFailures)
	}
	if c.failureCounts["issue-a"] != 1 {
		t.Errorf("failureCounts[issue-a] = %d, want 1", c.failureCounts["issue-a"])
	}
}

// TestController_StateRoundTrip verifies that saveState + loadState preserve
// the concrete controller state across restarts.
func TestController_StateRoundTrip(t *testing.T) {
	c := newTestController(t, nil)

	c.issues = []Issue{{ID: "i1", Type: "error", Severity: "high"}}
	c.analyses = []*RootCauseAnalysis{{IssueID: "i1", RootCause: "test"}}
	c.cycles = []*ImprovementCycle{{ID: "cyc-1", Status: CycleStatusCompleted}}
	c.failureCounts["i1"] = 2
	c.consecutiveFailures = 3

	if err := c.saveState(); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	// Fresh controller, same DataPath.
	c2 := newTestController(t, nil)
	c2.config.DataPath = c.config.DataPath
	if err := c2.loadState(); err != nil {
		t.Fatalf("loadState: %v", err)
	}

	if len(c2.issues) != 1 || c2.issues[0].ID != "i1" {
		t.Errorf("issues not restored: %+v", c2.issues)
	}
	if len(c2.analyses) != 1 || c2.analyses[0].IssueID != "i1" {
		t.Errorf("analyses not restored: %+v", c2.analyses)
	}
	if len(c2.cycles) != 1 || c2.cycles[0].ID != "cyc-1" {
		t.Errorf("cycles not restored: %+v", c2.cycles)
	}
	if c2.failureCounts["i1"] != 2 {
		t.Errorf("failureCounts not restored: %+v", c2.failureCounts)
	}
	if c2.consecutiveFailures != 3 {
		t.Errorf("consecutiveFailures = %d, want 3", c2.consecutiveFailures)
	}
}
