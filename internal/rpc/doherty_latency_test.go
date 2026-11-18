package rpc

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"
)

// Doherty-threshold latency-budget tests for the synchronous RPC dispatch path.
//
// IBM's Doherty research (1980s) found user productivity rises sharply once
// system response time drops under ~400ms. These tests enforce that budget at
// p95 (NOT mean — means hide the tail stalls users actually feel) on the RPC
// methods the TUI/GUI call on the user's behalf while a chat turn runs.
//
// Budget resolution: DOHERTY_BUDGET_MS (default 400) scaled by
// DOHERTY_BUDGET_SCALE (default 1.0). CI sets DOHERTY_BUDGET_SCALE=2 on shared
// runners rather than loosening the committed default.
//
// These tests are skipped under -short (`make test` runs -short) to bound the
// canonical suite's wall time; run them explicitly with:
//
//	go test -p 2 ./internal/rpc/ -run TestDoherty -v

// dohertyBudget returns the p95 latency budget for user-visible synchronous
// RPC dispatch: DOHERTY_BUDGET_MS (default 400) * DOHERTY_BUDGET_SCALE
// (default 1.0).
func dohertyBudget(t *testing.T) time.Duration {
	t.Helper()
	ms := 400.0
	if v := os.Getenv("DOHERTY_BUDGET_MS"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			t.Fatalf("bad DOHERTY_BUDGET_MS %q: %v", v, err)
		}
		ms = f
	}
	scale := 1.0
	if v := os.Getenv("DOHERTY_BUDGET_SCALE"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			t.Fatalf("bad DOHERTY_BUDGET_SCALE %q: %v", v, err)
		}
		scale = f
	}
	return time.Duration(ms * scale * float64(time.Millisecond))
}

func p95Dur(durs []time.Duration) time.Duration {
	sorted := append([]time.Duration(nil), durs...)
	slices.Sort(sorted)
	idx := max(int(float64(len(sorted))*0.95)-1, 0)
	return sorted[idx]
}

// dohertyFixture is one user-visible synchronous RPC method under budget.
type dohertyFixture struct {
	name   string
	method string
	params string
}

// dohertyFixtures covers the always-available, dependency-free status/config
// surfaces the TUI polls while a chat turn runs. Every fixture must stay
// registerable with no external daemon state (same posture the handler unit
// tests use); heavier surfaces (chat submit, delegation, cluster) carry their
// own timing contracts and are out of scope here.
func dohertyFixtures() []dohertyFixture {
	return []dohertyFixture{
		{name: "notifications.get_dnd", method: "notifications.get_dnd", params: `{}`},
		{name: "notifications.set_dnd", method: "notifications.set_dnd", params: `{"enabled":false}`},
		{name: "intake.status (disabled default)", method: "intake.status", params: `{}`},
	}
}

// buildDohertyServer registers every fixture's handler on a fresh Server with
// the same minimal wiring the existing handler tests use.
func buildDohertyServer(t *testing.T) *Server {
	t.Helper()
	srv := New(&Config{SocketPath: ""}, nil, nil)
	NewNotificationsHandler(&fakeDNDController{}).RegisterNotificationsHandlers(srv)
	NewIntakeStatusHandler(nil, nil).RegisterIntakeStatusMethods(srv)
	return srv
}

// TestDohertyRPCDispatchLatency enforces the T1 feedback budget (default
// 400ms, p95 of 100 samples per method after warmup) on synchronous RPC
// dispatch. A regression here is felt directly by TUI/GUI users: any method
// the interface awaits synchronously that starts stalling above the budget
// breaks user flow.
func TestDohertyRPCDispatchLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("latency-budget suite: skipped under -short; run with -run TestDoherty")
	}

	srv := buildDohertyServer(t)
	budget := dohertyBudget(t)

	for _, fx := range dohertyFixtures() {
		t.Run(fx.name, func(t *testing.T) {
			params := json.RawMessage(fx.params)

			// Warmup: cold caches inflate early samples.
			for range 10 {
				if _, err := srv.CallMethod(context.Background(), fx.method, params); err != nil {
					t.Fatalf("warmup %s: %v", fx.method, err)
				}
			}

			var durs []time.Duration
			for range 100 {
				start := time.Now()
				if _, err := srv.CallMethod(context.Background(), fx.method, params); err != nil {
					t.Fatalf("%s: %v", fx.method, err)
				}
				durs = append(durs, time.Since(start))
			}

			p95 := p95Dur(durs)
			t.Logf("%s: p95=%v (budget %v, n=%d)", fx.method, p95, budget, len(durs))
			if p95 > budget {
				t.Errorf("%s: p95 latency %v exceeds Doherty budget %v", fx.method, p95, budget)
			}
		})
	}
}

// TestDohertyT0AckLatency enforces the 100ms acknowledgment tier on the DND
// toggle round-trip: a state flip the UI renders immediately must never sit
// above 100ms at p95, independent of the 400ms feedback gate.
func TestDohertyT0AckLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("latency-budget suite: skipped under -short; run with -run TestDoherty")
	}

	srv := buildDohertyServer(t)
	scale := 1.0
	if v := os.Getenv("DOHERTY_BUDGET_SCALE"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			t.Fatalf("bad DOHERTY_BUDGET_SCALE %q: %v", v, err)
		}
		scale = f
	}
	ackBudget := time.Duration(100 * scale * float64(time.Millisecond))
	params := json.RawMessage(`{"enabled":false}`)

	for range 10 { // warmup
		if _, err := srv.CallMethod(context.Background(), "notifications.set_dnd", params); err != nil {
			t.Fatalf("warmup: %v", err)
		}
	}

	var durs []time.Duration
	for range 100 {
		start := time.Now()
		if _, err := srv.CallMethod(context.Background(), "notifications.set_dnd", params); err != nil {
			t.Fatalf("set_dnd: %v", err)
		}
		durs = append(durs, time.Since(start))
	}

	p95 := p95Dur(durs)
	t.Logf("notifications.set_dnd: p95=%v (ack budget %v, n=%d)", p95, ackBudget, len(durs))
	if p95 > ackBudget {
		t.Errorf("set_dnd ack: p95 %v exceeds 100ms acknowledgment budget %v", p95, ackBudget)
	}
}
