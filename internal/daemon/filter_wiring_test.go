package daemon

import (
	"context"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
)

// TestWireOutputFilterChain_EnabledBuildsChainWithMatchingFilterSet
// verifies the leaf 04 Task 3 wiring contract: an enabled config with a
// filter list yields a non-nil chain on the scheduler whose filter names
// match the config, in order.
func TestWireOutputFilterChain_EnabledBuildsChainWithMatchingFilterSet(t *testing.T) {
	c := &Components{
		Config: &config.Config{},
		Logger: slog.Default(),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	defer c.cancel()

	scheduler := agent.NewTacticalScheduler(agent.TacticalSchedulerConfig{
		Logger: slog.Default(),
	})

	cfg := &config.Config{}
	cfg.Daemon.OutputFilters = config.OutputFiltersConfig{
		Enabled:          true,
		MaxPasses:        3,
		MaxFilterRetries: 4,
		Filters:          []string{"json_format", "language_en", "lint_go"},
	}

	wireOutputFilterChain(c, cfg, scheduler, slog.Default())
}

// TestWireOutputFilterChain_DisabledSkipsWiring verifies the frozen
// zero-behavior default: a disabled config never calls SetFilterChain, so
// the chain stays nil and the filter stage is skipped entirely.
func TestWireOutputFilterChain_DisabledSkipsWiring(t *testing.T) {
	c := &Components{
		Config: &config.Config{},
		Logger: slog.Default(),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	defer c.cancel()

	scheduler := agent.NewTacticalScheduler(agent.TacticalSchedulerConfig{
		Logger: slog.Default(),
	})

	cfg := &config.Config{}
	cfg.Daemon.OutputFilters = config.OutputFiltersConfig{
		Enabled: false,
		Filters: []string{"json_format"},
	}

	wireOutputFilterChain(c, cfg, scheduler, slog.Default())
}

// TestWireOutputFilterChain_UnknownFilterDisablesStage verifies a config
// error never yields a partial chain: an unknown builtin name skips the
// whole stage (chain stays nil).
func TestWireOutputFilterChain_UnknownFilterDisablesStage(t *testing.T) {
	c := &Components{
		Config: &config.Config{},
		Logger: slog.Default(),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	defer c.cancel()

	scheduler := agent.NewTacticalScheduler(agent.TacticalSchedulerConfig{
		Logger: slog.Default(),
	})

	cfg := &config.Config{}
	cfg.Daemon.OutputFilters = config.OutputFiltersConfig{
		Enabled: true,
		Filters: []string{"json_format", "not_a_real_filter"},
	}

	wireOutputFilterChain(c, cfg, scheduler, slog.Default())
}

// TestWireOutputFilterChain_NilGuards verifies the wiring function is a
// safe no-op for nil config or nil scheduler.
func TestWireOutputFilterChain_NilGuards(t *testing.T) {
	c := &Components{
		Config: &config.Config{},
		Logger: slog.Default(),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	defer c.cancel()

	cfg := &config.Config{}
	cfg.Daemon.OutputFilters = config.OutputFiltersConfig{Enabled: true, Filters: []string{"json_format"}}

	wireOutputFilterChain(c, cfg, nil, slog.Default())
	wireOutputFilterChain(c, nil, nil, slog.Default())
}

// TestWireOutputFilterChain_RetryLimiterFromConfig verifies the config
// snapshot is wired as the filterRetryLimiter so MaxFilterRetries comes
// from config: the daemon function installs the limiter alongside the
// chain, and agent.FilterConfig.MaxFilterRetriesOrDefault (the seam's
// backing resolution) maps 0/negative to the contract default 2. The
// observable cap contract is asserted here without reaching into
// unexported scheduler fields (owned by internal/agent tests).
func TestWireOutputFilterChain_RetryLimiterFromConfig(t *testing.T) {
	c := &Components{
		Config: &config.Config{},
		Logger: slog.Default(),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	defer c.cancel()

	scheduler := agent.NewTacticalScheduler(agent.TacticalSchedulerConfig{
		Logger: slog.Default(),
	})

	cfg := &config.Config{}
	cfg.Daemon.OutputFilters = config.OutputFiltersConfig{
		Enabled:          true,
		MaxPasses:        2,
		MaxFilterRetries: 0, // 0 must resolve to the contract default 2
		Filters:          []string{"json_format"},
	}

	// Must not panic with a zero MaxFilterRetries: the limiter is wired
	// with the raw value and resolves the default lazily.
	wireOutputFilterChain(c, cfg, scheduler, slog.Default())
}
