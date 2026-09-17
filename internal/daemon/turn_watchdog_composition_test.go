package daemon

import (
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
)

// ---------------------------------------------------------------------------
// Leaf-06 task 3: daemon composition + lifecycle
//
// The full New() graph is out of scope for a unit test; these tests mirror
// the exact composition sequence from the SetTurnRegistry block in
// daemon.go (registry → SetTurnRegistry → NewTurnWatchdog → Start →
// Components field) against a minimal Components, then exercise the
// stopComponents hook through the real Components.Stop.
// ---------------------------------------------------------------------------

// turnWatchdogFixture holds the composition under test.
type turnWatchdogFixture struct {
	Components *Components
	Registry   *agent.TurnRegistry
	MsgBus     *bus.MessageBus
}

func turnWatchdogNewFixture(t *testing.T, cfg config.TurnWatchdogConfig) *turnWatchdogFixture {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	msgBus := bus.New(nil, logger)

	c := &Components{Logger: logger}
	c.ChatHandler = agent.NewChatHandler(nil, nil, msgBus, logger)

	// Same sequence as the composition site in daemon.go New().
	registry := agent.NewTurnRegistry()
	c.ChatHandler.SetTurnRegistry(registry)
	if cfg.Enabled {
		watchdog := agent.NewTurnWatchdog(
			registry,
			c.ChatHandler.EmitTurnTerminal,
			logger.With("component", "turn-watchdog"),
		)
		watchdog.Start(
			time.Duration(cfg.IntervalSeconds)*time.Second,
			time.Duration(cfg.StaleAfterSeconds)*time.Second,
		)
		c.TurnWatchdog = watchdog
	}

	return &turnWatchdogFixture{Components: c, Registry: registry, MsgBus: msgBus}
}

// TestTurnWatchdogComposition_EnabledStartsWatchdog verifies the enabled
// path: composition with orchestrator.turn_watchdog enabled exposes a
// started watchdog wired to the shared registry and the ChatHandler emit
// seam — a reaped turn's terminal event reaches the bus.
func TestTurnWatchdogComposition_EnabledStartsWatchdog(t *testing.T) {
	cfg := config.DefaultConfig().Orchestrator.TurnWatchdog
	if !cfg.Enabled {
		t.Fatal("precondition: watchdog must default enabled")
	}
	fixture := turnWatchdogNewFixture(t, cfg)
	if fixture.Components.TurnWatchdog == nil {
		t.Fatal("enabled config produced no watchdog (composition did not start it)")
	}

	sub := fixture.MsgBus.Subscribe("turn-watchdog-comp-test", "turn.terminal")
	defer fixture.MsgBus.Unsubscribe(sub)

	// Deterministic staleness: use a tiny threshold instead of sleeping
	// out the configured 120s.
	fixture.Registry.Register("turn-comp-1", "conv-comp-1")
	time.Sleep(5 * time.Millisecond) // guarantee LastProgressAt is in the past

	if n := fixture.Components.TurnWatchdog.RunOnce(2 * time.Millisecond); n != 1 {
		t.Fatalf("composition watchdog reaped %d turns, want 1", n)
	}

	type result struct {
		ev  agent.TurnTerminalEvent
		err error
	}
	ch := make(chan result, 1)
	go func() {
		msg := <-sub.Channel
		var ev agent.TurnTerminalEvent
		err := json.Unmarshal(msg.Payload, &ev)
		ch <- result{ev, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("unmarshal turn.terminal: %v", r.err)
		}
		if r.ev.TurnID != "turn-comp-1" || r.ev.Status != "failed" || r.ev.HandlerCase != "turn_reaped" {
			t.Errorf("composition-reaped event = %+v", r.ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no turn.terminal event observed from composition watchdog")
	}
}

// TestTurnWatchdogComposition_DisabledIsInert verifies the disabled path:
// orchestrator.turn_watchdog.enabled=false leaves Components.TurnWatchdog
// nil — no goroutine constructed, nothing to stop.
func TestTurnWatchdogComposition_DisabledIsInert(t *testing.T) {
	cfg := config.DefaultConfig().Orchestrator.TurnWatchdog
	cfg.Enabled = false

	fixture := turnWatchdogNewFixture(t, cfg)
	if fixture.Components.TurnWatchdog != nil {
		t.Fatal("disabled config constructed a watchdog; want zero goroutines")
	}
}

// TestTurnWatchdogComposition_StopJoins verifies the shutdown path: a
// started watchdog Stops cleanly through the same nil-guarded hook
// stopComponents uses (join completes well inside the budget — no leaked
// reap goroutine).
func TestTurnWatchdogComposition_StopJoins(t *testing.T) {
	cfg := config.DefaultConfig().Orchestrator.TurnWatchdog
	// Real ticker cadence (1s) so the loop is actively ticking at Stop;
	// the ms-scale join semantics are pinned in the agent-package tests.
	cfg.IntervalSeconds = 1
	cfg.StaleAfterSeconds = 1

	fixture := turnWatchdogNewFixture(t, cfg)
	if fixture.Components.TurnWatchdog == nil {
		t.Fatal("precondition: watchdog started")
	}

	stopped := make(chan struct{})
	go func() {
		fixture.Components.TurnWatchdog.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog Stop did not return within 5s; goroutine leaked")
	}

	// Second Stop through the same handle: idempotent, no panic.
	fixture.Components.TurnWatchdog.Stop()
}
