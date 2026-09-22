package daemon

import (
	"log/slog"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/validator"
)

// wireOutputFilterChain builds the milter-style output-filter chain from
// the effective filter config (output-filters tree, leaf 04 Task 3) and
// hands it to the tactical scheduler via SetFilterChain — ONLY when the
// stage is enabled and at least one filter is configured. A disabled
// config means SetFilterChain is never called, so the completion path is
// byte-identical to the pre-filter behavior (frozen zero-behavior
// default). The config snapshot is also wired as the filter-retry limiter
// so max_filter_retries comes from config.
func wireOutputFilterChain(c *Components, cfg *config.Config, scheduler *agent.TacticalScheduler, logger *slog.Logger) {
	if cfg == nil || scheduler == nil {
		return
	}
	ofs := cfg.Daemon.OutputFilters
	if !ofs.Enabled || len(ofs.Filters) == 0 {
		return
	}

	// Resolve each configured name against the builtin registry; an
	// unknown name is a config error logged at Warn with the valid set,
	// and skips the whole stage (never a partial chain).
	filters := make([]validator.OutputFilter, 0, len(ofs.Filters))
	for _, name := range ofs.Filters {
		f, err := validator.NewBuiltinFilter(name, validator.BuiltinConfig{})
		if err != nil {
			logger.Warn("invalid output_filters entry; filter stage disabled",
				"filter", name,
				"error", err,
			)
			return
		}
		filters = append(filters, f)
	}

	scheduler.SetFilterChain(validator.NewFilterChain(filters, ofs.MaxPasses))
	scheduler.SetFilterRetryLimiter(configFilterRetryLimiter{maxRetries: ofs.MaxFilterRetries})
	logger.Info("output filter chain wired",
		"filters", ofs.Filters,
		"max_passes", ofs.MaxPasses,
		"max_filter_retries", ofs.MaxFilterRetries,
	)
}

// configFilterRetryLimiter adapts the configured cap to the
// TacticalScheduler's filterRetryLimiter seam: a single method on the
// config value. 0/negative resolves to the contract default 2 via
// MaxFilterRetriesOrDefault.
type configFilterRetryLimiter struct {
	maxRetries int
}

func (l configFilterRetryLimiter) MaxFilterRetries() int {
	return agent.FilterConfig{MaxFilterRetries: l.maxRetries}.MaxFilterRetriesOrDefault()
}
