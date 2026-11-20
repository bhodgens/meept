package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/activity"
	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/rpc"
	pkgid "github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
)

// newActivityWiringFixture builds minimal Components (the
// newWiringTestComponents pattern) plus a bus and RPC server, without the
// full NewComponents stack.
func newActivityWiringFixture(t *testing.T) (*Components, *bus.MessageBus, *rpc.Server) {
	t.Helper()
	c := &Components{
		Config: &config.Config{},
		Logger: slog.New(slog.DiscardHandler),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	t.Cleanup(c.cancel)
	msgBus := bus.New(nil, slog.New(slog.DiscardHandler))
	srv := rpc.New(&rpc.Config{SocketPath: ""}, nil, slog.New(slog.DiscardHandler))
	return c, msgBus, srv
}

func callWiring(t *testing.T, srv *rpc.Server, method, params string) error {
	t.Helper()
	_, err := srv.CallMethod(context.Background(), method, json.RawMessage(params))
	return err
}

// TestActivityWiring_RegistersAllMethods verifies T4: after wiring, all
// five methods answer (registered exactly once — the double-registration
// probe inside registerActivityRPC logs and skips instead of overwriting).
func TestActivityWiring_RegistersAllMethods(t *testing.T) {
	f := newActivityRPCFixture(t) // seeded stores
	c, msgBus, srv := newActivityWiringFixture(t)
	c.TaskRegistry = f.reg
	c.StepStore = f.steps
	c.SessionStore = f.sess
	c.EmployeeManager = f.mgr
	c.EmployeeAuditStore = f.audit

	wireActivityComponents(c, msgBus, srv, c.Logger)

	if c.ActivityAggregator == nil {
		t.Fatal("ActivityAggregator not set by wiring")
	}
	if c.ActivityRing == nil {
		t.Fatal("ActivityRing not set by wiring")
	}
	for _, tc := range []struct{ method, params string }{
		{"activity.query", `{}`},
		{"activity.recent", `{}`},
		{"activity.overview", `{}`},
		{"activity.findings", `{}`},
		{"agents.review", `{"employee_id":"emp-1"}`},
	} {
		if err := callWiring(t, srv, tc.method, tc.params); err != nil {
			t.Errorf("%s not callable after wiring: %v", tc.method, err)
		}
	}
}

// TestActivityWiring_DoubleRegistrationSkips verifies the collision-safety
// rule: calling wireActivityComponents twice must not panic and must leave
// every method still answering.
func TestActivityWiring_DoubleRegistrationSkips(t *testing.T) {
	f := newActivityRPCFixture(t)
	c, msgBus, srv := newActivityWiringFixture(t)
	c.TaskRegistry = f.reg
	c.StepStore = f.steps
	c.SessionStore = f.sess
	c.EmployeeManager = f.mgr
	c.EmployeeAuditStore = f.audit

	wireActivityComponents(c, msgBus, srv, c.Logger)
	ringBefore := c.ActivityRing
	wireActivityComponents(c, msgBus, srv, c.Logger) // must log-and-skip, no panic

	if c.ActivityRing == ringBefore {
		t.Fatal("second wiring must still construct fresh handles (the RPC surface is what skips)")
	}
	for _, tc := range []struct{ method, params string }{
		{"activity.query", `{}`},
		{"activity.recent", `{}`},
		{"agents.review", `{"employee_id":"emp-1"}`},
	} {
		if err := callWiring(t, srv, tc.method, tc.params); err != nil {
			t.Errorf("%s broken after double registration: %v", tc.method, err)
		}
	}
}

// TestActivityWiring_AllNilHandlesNeverPanics verifies the typed-nil rule:
// wiring an empty Components (every handle nil, PlanManager nil) succeeds
// and every method degrades to a clean RPC error.
func TestActivityWiring_AllNilHandlesNeverPanics(t *testing.T) {
	c, msgBus, srv := newActivityWiringFixture(t)

	wireActivityComponents(c, msgBus, srv, c.Logger)

	if c.ActivityAggregator == nil || c.ActivityRing == nil {
		t.Fatal("wiring with nil handles must still construct aggregator + ring")
	}
	// managerBacked marks the methods whose handler requires a live
	// EmployeeManager: with the handle nil they must return a clean
	// RPC error, never panic and never succeed.
	managerBacked := map[string]bool{
		"activity.overview": true,
		"activity.findings": true,
		"agents.review":     true,
	}
	cases := []struct {
		method string
		params string
	}{
		{"activity.query", `{}`},
		{"activity.overview", `{}`},
		{"activity.findings", `{}`},
		{"agents.review", `{"employee_id":"x"}`},
	}
	for _, tc := range cases {
		// activity.query with a live-but-empty aggregator returns an
		// empty result, NOT an error; the manager-backed methods must
		// error cleanly (a nil EmployeeManager is a configuration
		// error, never a panic and never a silent success).
		err := callWiring(t, srv, tc.method, tc.params)
		if !managerBacked[tc.method] {
			if err != nil {
				t.Errorf("%s: %v", tc.method, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: nil manager must produce a clean RPC error, got nil", tc.method)
		} else if err.Error() == "" {
			t.Errorf("%s: RPC error must be non-empty", tc.method)
		}
	}
	if err := callWiring(t, srv, "activity.recent", `{}`); err != nil {
		t.Errorf("activity.recent must work with a live ring: %v", err)
	}
}

// TestActivityWiring_NilComponentsAndServer verifies the top-level guards.
func TestActivityWiring_NilComponentsAndServer(t *testing.T) {
	wireActivityComponents(nil, nil, nil, nil) // must not panic

	c, _, _ := newActivityWiringFixture(t)
	wireActivityComponents(c, nil, nil, c.Logger) // nil server: no-op
	if c.ActivityAggregator != nil {
		t.Error("nil server must be a full no-op")
	}
}

// TestActivityWiring_RingReceivesBusEvents proves the ring is live: an
// agent.progress publish must surface through activity.recent.
func TestActivityWiring_RingReceivesBusEvents(t *testing.T) {
	c, msgBus, srv := newActivityWiringFixture(t)
	wireActivityComponents(c, msgBus, srv, c.Logger)

	msgBus.Publish("agent.progress", &models.BusMessage{
		ID:        "probe-1",
		Type:      models.MessageTypeEvent,
		Topic:     "agent.progress",
		Source:    "wiring-test",
		Timestamp: time.Now().UTC(),
		Payload:   []byte(`{"agent_id":"emp-9","stage":"thinking"}`),
	})

	deadline := 100
	for range deadline {
		result, err := srv.CallMethod(context.Background(), "activity.recent", json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("activity.recent: %v", err)
		}
		b, _ := json.Marshal(result)
		var shaped struct {
			Records []activity.ActivityRecord `json:"records"`
		}
		if err := json.Unmarshal(b, &shaped); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(shaped.Records) > 0 {
			return
		}
		// The publish->subscriber->ring hop is async; a tight spin can
		// exhaust all 100 iterations before the bus goroutine is scheduled
		// (reproduced on a clean HEAD worktree under load). Yield between
		// polls; the budget stays ~2s worst case.
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("ring stayed empty after agent.progress publish")
}

// TestActivityWiring_StopDrainsExecutionProjector pins the issue #160 M5
// shutdown ordering on the REAL seam: wireActivityComponents opens the
// execution store and subscribes the projector; stopActivityRing must
// drain the projector's buffered events BEFORE closing the store, so every
// delivered result row survives shutdown. Events are published with
// PublishBlockingT (channel delivery guaranteed), n exceeds the bus
// buffer, and the stop path runs immediately - the exact race window the
// old unsubscribe-then-Close ordering lost rows in. Rows are counted on a
// fresh handle for this test's unique agent id, isolated from other
// tests' runs in the shared pinned MEEPT_HOME.
func TestActivityWiring_StopDrainsExecutionProjector(t *testing.T) {
	c, msgBus, srv := newActivityWiringFixture(t)
	wireActivityComponents(c, msgBus, srv, c.Logger)
	if c.ExecutionStore == nil {
		t.Skip("execution store did not open in this environment")
	}

	const n = 120 // above the bus default buffer (100): forces a backlog
	// Unique per run: the pinned test-home DB survives across runs, so a
	// stable agent id would double-count on the second run.
	agentID := pkgid.Generate("m5drain")
	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < n; i++ {
		bus.PublishBlockingT(msgBus, agent.TopicBotResult, "bot", agent.ExecutionResultEvent{
			AgentID: agentID, Status: "completed", TokensUsed: i,
			DurationMS: 1, Timestamp: now,
		})
	}

	// Immediate shutdown seam - the M5 race: stop while the projector
	// reader may still be mid-drain.
	stopActivityRing(c, msgBus)
	if c.ExecutionStore != nil {
		t.Fatal("stopActivityRing must clear the store handle")
	}

	reopened, err := activity.OpenExecutionStore(config.MeeptPath("execution_runs.db"), nil)
	if err != nil {
		t.Fatalf("reopen execution store: %v", err)
	}
	defer func() {
		if cerr := reopened.Close(); cerr != nil {
			t.Errorf("reopen close: %v", cerr)
		}
	}()
	_, total, err := reopened.List(context.Background(), activity.ExecutionQuery{AgentID: agentID, Limit: 1})
	if err != nil {
		t.Fatalf("list after shutdown: %v", err)
	}
	if total != n {
		t.Fatalf("M5 regression: %d of %d buffered runs survived stopActivityRing (lost=%d)",
			total, n, n-total)
	}
}
