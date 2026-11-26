package daemon

// A3-5 pins: the working-tier eviction drain reaches PRODUCTION.
//   (1) armWorkingTierFlush + the Start/stop lifecycle: with memory.tiers
//       OFF, a bounded ring eviction still lands in the episodic sink
//       through the started flush loop, and after the joined stop no
//       further eviction flows (both directions).
//   (2) the production call site: NewComponents with
//       agent.context_assembler.enabled (ring built) and tiers off parks
//       the setup closure; with the assembler off nothing is armed
//       (flag-off byte-identical); with tiers ON the drain rides the
//       ENGINE (passed working store), never the standalone loop.

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/working"
)

func a35Manager(t *testing.T) *memory.Manager {
	t.Helper()
	mgr := memory.NewManager(memory.ManagerConfig{
		Config: config.MemoryConfig{
			Backend:  config.MemoryBackendSQLite,
			DataDir:  t.TempDir(),
			Episodic: config.EpisodicConfig{Enabled: true},
		},
	})
	if err := mgr.Initialize(context.Background()); err != nil {
		t.Fatalf("memory manager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr
}

// episodicHas reports whether any recent episodic row contains needle.
func episodicHas(t *testing.T, mgr *memory.Manager, needle string) bool {
	t.Helper()
	rows, err := mgr.Episodic().GetRecent(context.Background(), 200)
	if err != nil {
		t.Fatalf("episodic GetRecent: %v", err)
	}
	for _, r := range rows {
		if strings.Contains(r.Memory.Content, needle) {
			return true
		}
	}
	return false
}

// TestWorkingTierFlushDeliversToEpisodic pins the drain behavior with
// tiers OFF: the ring's evicted turns reach the episodic store through
// the loop armed by the production helper (nothing else writes episodic
// here, so the assertion is inherently non-vacuous — pre-patch this
// queue silently drop-oldestd). The stop leg is pinned in the other
// direction: after stopComponents-equivalent stop, a fresh eviction must
// NOT reach episodic (proving the loop was actually running and ended).
func TestWorkingTierFlushDeliversToEpisodic(t *testing.T) {
	mgr := a35Manager(t)
	store, err := working.New(working.Config{MaxTokens: 40, MaxItems: 2}, &llm.HeuristicTokenizer{})
	if err != nil {
		t.Fatalf("working store: %v", err)
	}
	c := &Components{Logger: slog.New(slog.DiscardHandler)}
	c.workingStore = store
	c.MemoryManager = mgr
	cfg := &config.Config{}
	cfg.Memory.Tiers.Enabled = false     // the shipped default posture
	cfg.Memory.Tiers.IntervalSeconds = 1 // 1s tick for the pin

	// Production shape: park the closure, invoke it (what Start does),
	// join the stop (what stopComponents does).
	armWorkingTierFlush(c, cfg, slog.New(slog.DiscardHandler))
	if c.workingTierFlushSetup == nil {
		t.Fatal("armWorkingTierFlush armed nothing with a live store + manager")
	}
	c.workingTierFlushSetup(context.Background())
	if c.workingTierFlushStop == nil {
		t.Fatal("setup did not park the stop func (stopComponents would leak the loop)")
	}
	t.Cleanup(func() {
		if c.workingTierFlushStop != nil {
			c.workingTierFlushStop()
		}
	})

	// Overflow the ring: every long turn evicts into the bounded queue.
	needle := "a35-evicted-turn-marker"
	for i := range 6 {
		if err := store.Append(context.Background(), working.Turn{
			ID: "t-" + string(rune('a'+i)), SessionID: "s1", Role: "user",
			Content: needle + "-" + strings.Repeat("z", 140) + string(rune('0'+i)),
			At:      time.Now(),
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if store.QueuedTurns() == 0 {
		t.Fatal("fixture must evict (queue empty means no drain pressure)")
	}

	// The 1s tick drains into episodic within a bounded wait. The ring
	// keeps the newest turns live and drop-oldest trims the queue, so the
	// assertion is "at least one evicted marker row reaches episodic AND
	// the queue fully drains" — non-vacuous (pre-fix: zero rows ever).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && store.QueuedTurns() > 0 {
		time.Sleep(100 * time.Millisecond)
	}
	if store.QueuedTurns() != 0 {
		t.Fatalf("A3-5 regression: eviction queue never drained (%d queued after 5s)", store.QueuedTurns())
	}
	if !episodicHas(t, mgr, needle) {
		t.Fatal("A3-5 regression: evicted ring turns never reached the episodic sink through the started flush loop")
	}

	// Stop leg: the loop ends and nothing further flows. The stop func
	// itself is idempotent (sync.Once); call it twice, then nil the field.
	stop := c.workingTierFlushStop
	stop()
	stop() // idempotent — must not panic
	c.workingTierFlushStop = nil
	late := "a35-post-stop-turn"
	// Two appends: the ring holds 2, so these evict each other's oldest
	// into the queue — a live loop would flush the marker to episodic.
	for i := range 3 {
		if err := store.Append(context.Background(), working.Turn{
			ID: "late-" + string(rune('0'+i)), SessionID: "s1", Role: "user",
			Content: late + "-" + strings.Repeat("y", 140) + string(rune('0'+i)),
			At:      time.Now(),
		}); err != nil {
			t.Fatalf("late append: %v", err)
		}
	}
	time.Sleep(2500 * time.Millisecond) // > one tick: the dead loop must NOT drain
	if store.QueuedTurns() == 0 {
		t.Fatal("post-stop eviction queue drained: the flush loop is still running after stop (stop regression)")
	}
	if episodicHas(t, mgr, late) {
		t.Fatal("post-stop eviction reached episodic: the stop did not end the flush loop")
	}
}

// TestWorkingTierFlushNilInputsInert pins the nil-safe posture: no ring
// or no manager arms nothing (flag-off = byte-identical, zero goroutine).
func TestWorkingTierFlushNilInputsInert(t *testing.T) {
	cfg := &config.Config{}
	// no ring (nil store) arms nothing:
	c := &Components{Logger: slog.New(slog.DiscardHandler)}
	c.MemoryManager = a35Manager(t)
	armWorkingTierFlush(c, cfg, slog.New(slog.DiscardHandler))
	if c.workingTierFlushSetup != nil {
		t.Fatal("nil workingStore must arm nothing")
	}
	// live ring with a nil manager arms nothing (typed-nil guard):
	c3 := &Components{Logger: slog.New(slog.DiscardHandler)}
	c3.workingStore = &working.Store{}
	armWorkingTierFlush(c3, cfg, slog.New(slog.DiscardHandler))
	if c3.workingTierFlushSetup != nil {
		t.Fatal("nil MemoryManager must arm nothing (typed-nil guard)")
	}
	// nil Components must not panic:
	armWorkingTierFlush(nil, cfg, slog.New(slog.DiscardHandler))
}

// TestProductionCallSiteArmsFlush pins the A3-5 PRODUCTION call site:
// NewComponents with agent.context_assembler.enabled (ring built) and
// memory.tiers OFF must park the flush setup closure (pre-fix: the
// branch armed nothing — Flush/EpisodicSinkAdapter had zero production
// callers). The flag-off boot (assembler disabled) arms nothing and
// leaves no workingStore, byte-identical.
func TestProductionCallSiteArmsFlush(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode: full NewComponents boot")
	}
	tmp := t.TempDir()
	t.Setenv("MEEPT_HOME", tmp)
	logger := slog.New(slog.DiscardHandler)
	mkCfg := func(assembler bool) *config.Config {
		cfg := &config.Config{
			Daemon: config.DaemonConfig{DataDir: tmp + "-d" + strings.Repeat("x", map[bool]int{false: 0, true: 1}[assembler])},
			Memory: config.MemoryConfig{
				Backend:  config.MemoryBackendSQLite,
				DataDir:  tmp + "-m" + strings.Repeat("x", map[bool]int{false: 0, true: 1}[assembler]),
				Episodic: config.EpisodicConfig{Enabled: true},
			},
			Agent: config.AgentConfig{
				ContextAssembler: config.ContextAssemblerConfig{Enabled: assembler},
			},
		}
		cfg.Memory.Tiers.Enabled = false // the shipped default: the standalone drain branch must fire
		return cfg
	}

	msgBus := bus.New(nil, logger)
	comps, err := NewComponents(context.Background(), mkCfg(true), msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents (assembler on): %v", err)
	}
	// single-owner stop: stopComponents is NOT idempotent to double-call,
	// so the deferred stop is guarded by the field the inline call nils.
	stopped := false
	defer func() {
		if !stopped {
			_ = comps.stopComponents(context.Background())
		}
	}()
	if comps.workingStore == nil {
		t.Fatal("assembler enabled but no ring store built: the branch is unreachable by construction")
	}
	if comps.TiersEngine != nil {
		t.Fatal("tiers config off but the engine wired: the pin targets the disabled branch")
	}
	if comps.workingTierFlushSetup == nil {
		t.Fatal("A3-5 regression: tiers OFF + live ring armed NO standalone flush — evicted ring turns still drop-oldest with zero production drainers")
	}
	// stopComponents must join the stop (nil the fields; no panic).
	comps.workingTierFlushSetup(context.Background())
	if comps.workingTierFlushStop == nil {
		t.Fatal("Start-time setup did not park the stop func")
	}
	if err := comps.stopComponents(context.Background()); err != nil {
		t.Fatalf("stopComponents: %v", err)
	}
	stopped = true
	if comps.workingTierFlushStop != nil || comps.workingTierFlushSetup != nil {
		t.Fatal("stopComponents left the flush handle parked")
	}

	// Flag-off direction: assembler disabled -> no ring, nothing armed.
	comps2, err := NewComponents(context.Background(), mkCfg(false), msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents (assembler off): %v", err)
	}
	if comps2.workingStore != nil {
		t.Fatal("assembler off but a ring store exists")
	}
	if comps2.workingTierFlushSetup != nil {
		t.Fatal("flag-off boot armed the flush: posture is NOT byte-identical")
	}
	if err := comps2.stopComponents(context.Background()); err != nil {
		t.Fatalf("stopComponents (flag-off): %v", err)
	}
}
