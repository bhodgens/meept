package daemon

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/internal/activity"
	"github.com/caimlas/meept/internal/rpc"
)

// TestRegisterActivityRPC_ProbeDoesNotDispatch pins bughunt A1-11: the
// duplicate-registration probe in registerActivityRPC's register() closure
// used server.CallMethod(method, nil), which DISPATCHES a same-named
// existing handler with nil params (for activity.overview that was a full
// unbounded aggregation) and logs a spurious "rpc: method not found" Warn
// five times on every clean boot. The probe must be lookup-only
// (rpc.Server.HasMethod).
func TestRegisterActivityRPC_ProbeDoesNotDispatch(t *testing.T) {
	srv := rpc.New(&rpc.Config{SocketPath: ""}, nil, nil)

	// Pre-register a sentinel owner for one of the activity methods —
	// exactly the "sibling surface already owns the name" case.
	sentinelCalls := 0
	srv.RegisterHandler("activity.query", func(ctx context.Context, params json.RawMessage) (any, error) {
		sentinelCalls++
		return "sentinel", nil
	})

	registerActivityRPC(srv, activity.NewAggregator(nil, nil, nil, nil, nil, activity.Options{}),
		activity.NewRing(10), nil, nil)

	// The probe must NOT have executed the sentinel during registration.
	if sentinelCalls != 0 {
		t.Fatalf("register() probe dispatched the pre-existing handler %d time(s), want 0 (bughunt A1-11)", sentinelCalls)
	}

	// Skip-duplicate behavior preserved: the sentinel still owns the name.
	res, err := srv.CallMethod(context.Background(), "activity.query", nil)
	if err != nil {
		t.Fatalf("CallMethod: %v", err)
	}
	if res != "sentinel" {
		t.Fatalf("duplicate registration clobbered the sentinel: res = %v", res)
	}
	if sentinelCalls != 1 {
		t.Fatalf("sentinel calls = %d, want 1 (only the explicit CallMethod)", sentinelCalls)
	}

	// Non-colliding methods still register and dispatch their own handlers.
	if !srv.HasMethod("activity.recent") {
		t.Fatal("activity.recent not registered despite no collision")
	}
}
