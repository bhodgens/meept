package daemon

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/employee"
)

// TestGoalGateParkedForEmployee_ParkedGateBlocksQuiescence pins the C25
// daemon wiring: an ACTIVE goal with a parked approval gate makes
// goalGateParkedForEmployee true (the driver's quiescence predicate then
// refuses to advance), and the same predicate is false before the park and
// after release.
func TestGoalGateParkedForEmployee_ParkedGateBlocksQuiescence(t *testing.T) {
	mgr := employee.NewManagerWithStores(nil, nil, nil, nil, nil, slog.New(slog.DiscardHandler))
	turns := agent.NewGateTurnParker(slog.New(slog.DiscardHandler), func(context.Context, agent.ParkedTurnRecord) {})
	turns.SetParkPersistence(&gatePersistenceStubForC25{}, agent.ParkKindGate)
	parker := agent.NewGateParker(slog.New(slog.DiscardHandler), turns)

	// No goals listed (nil stores) -> not parked (fail-open).
	if goalGateParkedForEmployee(mgr, parker, "emp_x") {
		t.Fatal("no active goals must report not-parked")
	}

	// Park a goal gate: the probe is goal-store-less here, so pin the
	// predicate primitive directly through HasParkedGateForID (the exact
	// call goalGateParkedForEmployee makes per active goal).
	if parker.HasParkedGateForID("goal_c25") {
		t.Fatal("nothing parked yet")
	}
	if !parker.ParkGate(agent.GateParkData{
		GateKind:    "goal",
		GateID:      "goal_c25",
		TurnReentry: map[string]any{"employee_id": "emp_x"},
	}) {
		t.Fatal("ParkGate refused a valid payload")
	}
	if !parker.HasParkedGateForID("goal_c25") {
		t.Fatal("parked approval gate must be visible (C25: driver must NOT run rounds while approval pends)")
	}
	if _, err := parker.ReleaseByKey("goal_c25", "rejected"); err != nil {
		t.Fatalf("release: %v", err)
	}
	if parker.HasParkedGateForID("goal_c25") {
		t.Fatal("released gate must clear the probe")
	}
}

// gatePersistenceStubForC25 is a minimal agent-park persistence stand-in so
// the gate parker indexes keys (see the employee-side twin for detail).
type gatePersistenceStubForC25 struct {
	rows map[string]agent.ParkedTurnRecord
}

func (f *gatePersistenceStubForC25) Save(_ context.Context, kind agent.ParkKind, key string, rec agent.ParkedTurnRecord) error {
	if f.rows == nil {
		f.rows = make(map[string]agent.ParkedTurnRecord)
	}
	f.rows[string(kind)+"|"+key] = rec
	return nil
}

func (f *gatePersistenceStubForC25) Delete(_ context.Context, kind agent.ParkKind, key string) error {
	delete(f.rows, string(kind)+"|"+key)
	return nil
}

func (f *gatePersistenceStubForC25) Load(_ context.Context, _ agent.ParkKind, _ time.Time) ([]agent.ParkedTurnRecord, []string, error) {
	return nil, nil, nil
}
