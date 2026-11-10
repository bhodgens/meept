// Package daemon — weather_refresh.go implements plan 20261110-situational
// leaf 03-weather/02: the scheduled `weather-refresh` job. Each pass lists
// the bot definitions, keeps those with Weather.Trades set (D5: only
// trade-listed locations need weather), dedupes to distinct (lat, lon)
// points, and warms the shared weather.Cache once per point (master Q3:
// two employees at one shop share a single fetch). A per-location fetch
// error is logged and skipped — the stale-on-error posture inside the
// cache plus this pass's continue-on-error make one bad location never
// abort the refresh. Registration is guarded by [awareness].enabled AND at
// least one trade-listed employee, so a daemon without weather-aware bots
// schedules nothing (D1 opt-in).
package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/scheduler"
	"github.com/caimlas/meept/internal/weather"
)

// weatherRefreshJobID is the scheduler job id pinned by gate §W4.
const weatherRefreshJobID = "weather-refresh"

// WeatherRefreshJob runs one cache-warming pass over the bot store's
// weather-aware definitions. Constructed by the registration below; the
// gate tests call RunOnce directly.
type WeatherRefreshJob struct {
	fetcher weather.Fetcher
	store   *bot.Store
	logger  *slog.Logger
}

// NewWeatherRefreshJob builds the refresh pass (frozen §W4 constructor).
// A nil fetcher or store degrades at run time: RunOnce reports the miss
// per pass (WARN) and warms nothing — registration already refuses both
// nil shapes, this guards direct/test construction.
func NewWeatherRefreshJob(fetcher weather.Fetcher, store *bot.Store, logger *slog.Logger) *WeatherRefreshJob {
	if logger == nil {
		logger = slog.Default()
	}
	return &WeatherRefreshJob{fetcher: fetcher, store: store, logger: logger}
}

// RunOnce executes one warming pass: List definitions, skip nil/zero-
// Weather and no-Trades defs (D5), dedupe by exact (Lat, Lon), fetch each
// distinct point once, and return the number of locations refreshed.
// Only a store.List failure surfaces as an error; a per-location fetch
// failure logs "weather refresh" and continues.
func (j *WeatherRefreshJob) RunOnce(ctx context.Context) (int, error) {
	if j.store == nil {
		return 0, fmt.Errorf("weather refresh: no bot store")
	}
	if j.fetcher == nil {
		j.logger.Warn("weather refresh: no fetcher wired; pass skipped")
		return 0, nil
	}

	defs, err := j.store.List(ctx)
	if err != nil {
		j.logger.Error("weather refresh: bot store list failed", "error", err)
		return 0, err
	}

	// Dedupe by EXACT (lat, lon) (gate §W: one fetch per distinct point;
	// the cache's own 0.01-degree bucketing may coarsen further, which is
	// harmless).
	type point struct{ lat, lon float64 }
	distinct := make(map[point]bool, len(defs))
	for i := range defs {
		def := &defs[i]
		if def == nil {
			continue
		}
		if len(def.Weather.Trades) == 0 {
			continue
		}
		if def.Weather.Lat == 0 && def.Weather.Lon == 0 {
			continue
		}
		distinct[point{def.Weather.Lat, def.Weather.Lon}] = true
	}

	warmed := 0
	for p := range distinct {
		fctx, cancel := context.WithTimeout(ctx, weather.DefaultTimeout+time.Second)
		_, ferr := j.fetcher.Fetch(fctx, p.lat, p.lon)
		cancel()
		if ferr != nil {
			// Per-location failure: log and keep going (stale-on-error
			// inside the cache already covers previously-warmed points).
			j.logger.Warn("weather refresh: location fetch failed",
				"lat", p.lat, "lon", p.lon, "error", ferr)
			continue
		}
		warmed++
	}

	if warmed > 0 || len(distinct) > 0 {
		j.logger.Debug("weather refresh pass complete",
			"locations", len(distinct), "warmed", warmed)
	}
	return warmed, nil
}

// registerWeatherRefreshJob schedules the periodic cache-warming pass on
// the daemon scheduler (ledger-integrity precedent). Guards, each
// registering NOTHING: awareness disabled (D1), nil bot store, nil
// scheduler (WARN), and zero definitions with Weather.Trades set (D5).
// A nil Components.WeatherFetcher still registers when the guards pass —
// the pass then logs and skips (frozen §W4 note).
func (c *Components) registerWeatherRefreshJob(cfg *config.Config, logger *slog.Logger) {
	if c == nil || cfg == nil || !cfg.Awareness.Enabled {
		return
	}
	if c.BotStore == nil {
		return
	}
	if c.Scheduler == nil {
		if logger != nil {
			logger.Warn("weather refresh: scheduler unavailable; job not registered")
		}
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	// At least one employee with Weather.Trades, else the job would be a
	// permanent no-op — register nothing.
	acfg := cfg.Awareness
	config.NormalizeAwarenessDefaults(&acfg)
	defs, err := c.BotStore.List(context.Background())
	if err != nil {
		logger.Warn("weather refresh: bot store list failed at registration; job not registered", "error", err)
		return
	}
	var withTrades int
	for i := range defs {
		if len(defs[i].Weather.Trades) > 0 {
			withTrades++
		}
	}
	if withTrades == 0 {
		return
	}

	interval := time.Duration(acfg.RefreshMinutes) * time.Minute

	job := &weatherRefreshJob{
		id:       weatherRefreshJobID,
		name:     fmt.Sprintf("situational weather cache refresh (%d employees)", withTrades),
		interval: interval,
		runner:   NewWeatherRefreshJob(c.WeatherFetcher, c.BotStore, logger.With("component", "weather-refresh")),
		logger:   logger.With("component", "weather-refresh"),
	}
	if _, err := c.Scheduler.Schedule(job); err != nil {
		logger.Warn("weather refresh: schedule failed", "job_id", weatherRefreshJobID, "error", err)
		return
	}
	logger.Info("Weather refresh job scheduled",
		"job_id", weatherRefreshJobID,
		"interval", interval.String(),
		"trade_listed_employees", withTrades,
	)
}

// weatherRefreshJob is the minimal scheduler.Job wrapper (mirrors
// ledgerIntegrityJob): an "@every <interval>" cron expression firing
// RunOnce with panic isolation, so a wedged weather endpoint never takes
// down the scheduler's cron goroutine.
type weatherRefreshJob struct {
	id       string
	name     string
	interval time.Duration
	runner   *WeatherRefreshJob
	logger   *slog.Logger
}

// ID implements scheduler.Job.
func (j *weatherRefreshJob) ID() string { return j.id }

// Name implements scheduler.Job.
func (j *weatherRefreshJob) Name() string { return j.name }

// Schedule implements scheduler.Job: cron "@every <interval>".
func (j *weatherRefreshJob) Schedule() string { return fmt.Sprintf("@every %s", j.interval) }

// Type implements scheduler.Job.
func (j *weatherRefreshJob) Type() scheduler.JobType { return scheduler.JobTypeInterval }

// Execute implements scheduler.Job: one warming pass with a bounded
// context and panic recovery.
func (j *weatherRefreshJob) Execute(_ context.Context) error {
	defer func() {
		if r := recover(); r != nil {
			j.logger.Error("weather refresh job panic recovered", "panic", r)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	_, err := j.runner.RunOnce(ctx)
	return err
}

// Config implements scheduler.Job.
func (j *weatherRefreshJob) Config() scheduler.JobConfig {
	return scheduler.JobConfig{
		ID:        j.id,
		Name:      j.name,
		Type:      scheduler.JobTypeInterval,
		Schedule:  j.Schedule(),
		Enabled:   true,
		CreatedAt: timeNowUTC(),
		Tags:      []string{"awareness", "weather"},
	}
}
