package daemon

// 00T-weather gate §W4 — plan 20261110-situational leaf 03-weather/00T:
// the weather-refresh scheduler job + [awareness] config table. TESTS ONLY;
// RED posture expected until leaf 03/02 lands (undefined symbols below).
// Copied registration-test idiom: ledger_integrity_test.go (nil-guard ->
// registers nothing, ListJobs schedule assertion) +
// employee_scheduler_adapter.go:49-85 simpleIntervalJob.
//
// Pinned seams (leaf 03/02 MUST provide EXACTLY these):
//
//   internal/config:
//     type AwarenessConfig struct {
//         Enabled        bool `json:"enabled"         toml:"enabled"`
//         RefreshMinutes int  `json:"refresh_minutes" toml:"refresh_minutes"`
//     }
//     // Config gains: Awareness AwarenessConfig `json:"awareness" toml:"awareness"`
//     const DefaultAwarenessRefreshMinutes = 30
//     func NormalizeAwarenessDefaults(c *AwarenessConfig) // non-positive -> 30
//     // DefaultConfig(): Enabled=false (D1 opt-in), RefreshMinutes=30.
//
//   internal/bot (frozen contract block): BotDefinition gains
//     Weather BotWeather  (zero value = no weather awareness)
//     type BotWeather struct { Lat, Lon float64; Trades []string }
//
//   internal/daemon:
//     const weatherRefreshJobID = "weather-refresh"
//     // Components gains: WeatherFetcher weather.Fetcher (nil -> job still
//     // registers when guards pass but Execute logs WARN and skips).
//     func NewWeatherRefreshJob(fetcher weather.Fetcher, store *bot.Store,
//         logger *slog.Logger) *WeatherRefreshJob
//     // (*WeatherRefreshJob).RunOnce(ctx) (warmed int, err error):
//     // List(ctx) definitions; skip nil/zero-Weather and no-Trades defs
//     // (D5: refresh only for trade-listed weather); dedupe by exact
//     // (Lat, Lon); fetch each distinct location once (stale-on-error
//     // inside the weather cache means fetch errors are logged, never
//     // abort the pass); return successful-fetch count, nil error unless
//     // store.List itself fails.
//     func (c *Components) registerWeatherRefreshJob(cfg *config.Config,
//         logger *slog.Logger)
//     // Guards (each -> register NOTHING): cfg.Awareness.Enabled false
//     // (D1), c.BotStore nil, scheduler nil (WARN), zero definitions with
//     // Weather.Trades non-empty. Interval:
//     // time.Duration(RefreshMinutes) * time.Minute (normalized).
//     // Job wrapper mirrors ledgerIntegrityJob:
//     type weatherRefreshJob struct {
//         id, name string; interval time.Duration
//         runner *WeatherRefreshJob; logger *slog.Logger
//     }
//
// No production network: the job tests inject a counting fake Fetcher.

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/scheduler"
	"github.com/caimlas/meept/internal/weather"
)

// wrCaptureHandler records "LEVEL message key=value" lines (weather-refresh
// copy of the liCaptureHandler idiom; wr- prefix avoids collisions with the
// concurrent sibling daemon test files and the existing captureHandler /
// tjCaptureHandler / liCaptureHandler).
type wrCaptureHandler struct {
	mu    sync.Mutex
	lines []string
}

func (h *wrCaptureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *wrCaptureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := r.Level.String() + " " + r.Message
	r.Attrs(func(a slog.Attr) bool {
		s += " " + a.Key + "=" + a.Value.String()
		return true
	})
	h.lines = append(h.lines, s)
	return nil
}
func (h *wrCaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *wrCaptureHandler) WithGroup(string) slog.Handler      { return h }

func (h *wrCaptureHandler) count(substr string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, l := range h.lines {
		if strings.Contains(l, substr) {
			n++
		}
	}
	return n
}

// newWeatherTestScheduler: real scheduler on a temp data dir — same
// construction as newLedgerTestScheduler (own copy; that helper is a
// concurrent sibling's file, and duplicate names across same-package files
// would break the build if one sibling renames it).
func newWeatherTestScheduler(t *testing.T) *scheduler.Scheduler {
	t.Helper()
	s, err := scheduler.NewScheduler(
		config.SchedulerConfig{Enabled: true},
		bus.New(bus.DefaultConfig(), slog.New(slog.DiscardHandler)),
		scheduler.WithDataDir(t.TempDir()),
		scheduler.WithLogger(slog.New(slog.DiscardHandler)),
	)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	return s
}

func newWeatherTestBotStore(t *testing.T) *bot.Store {
	t.Helper()
	st, err := bot.NewStore(filepath.Join(t.TempDir(), "bots.db"))
	if err != nil {
		t.Fatalf("bot.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func putWeatherBot(t *testing.T, st *bot.Store, id string, w bot.BotWeather) {
	t.Helper()
	def := bot.BotDefinition{
		ID:      id,
		Name:    id,
		Enabled: true,
		Weather: w,
	}
	if err := st.Create(context.Background(), def); err != nil {
		t.Fatalf("bot store Create(%s): %v", id, err)
	}
}

// wrFakeFetcher counts Fetch calls per (lat,lon).
type wrFakeFetcher struct {
	mu     sync.Mutex
	calls  map[[2]float64]int
	failAt [2]float64
	failOn bool
}

func newWRFakeFetcher() *wrFakeFetcher {
	return &wrFakeFetcher{calls: map[[2]float64]int{}}
}

func (f *wrFakeFetcher) Fetch(ctx context.Context, lat, lon float64) (weather.Outlook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := [2]float64{lat, lon}
	f.calls[key]++
	if f.failOn && key == f.failAt {
		return weather.Outlook{}, context.DeadlineExceeded
	}
	return weather.Outlook{NowC: 3, TodayHighC: 12, TodayLowC: -2, RainProbToday: 60, Fetched: time.Now()}, nil
}

func (f *wrFakeFetcher) countAt(lat, lon float64) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[[2]float64{lat, lon}]
}

func (f *wrFakeFetcher) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		n += c
	}
	return n
}

func weatherCfg(enabled bool, minutes int) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Awareness = config.AwarenessConfig{Enabled: enabled, RefreshMinutes: minutes}
	return cfg
}

// ---------------------------------------------------------------------------
// Gate row: [awareness] config parses (enabled default FALSE, refresh_minutes
// default 30) + load-boundary normalization
// ---------------------------------------------------------------------------

func TestWeatherRefresh_ConfigDefaults(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.Awareness.Enabled {
		t.Error("DefaultConfig: Awareness.Enabled = true, want false (D1 opt-in default)")
	}
	if cfg.Awareness.RefreshMinutes != 30 {
		t.Errorf("DefaultConfig: Awareness.RefreshMinutes = %d, want 30", cfg.Awareness.RefreshMinutes)
	}

	for _, in := range []int{0, -1, -30} {
		c := config.AwarenessConfig{Enabled: true, RefreshMinutes: in}
		config.NormalizeAwarenessDefaults(&c)
		if c.RefreshMinutes != 30 {
			t.Errorf("NormalizeAwarenessDefaults(%d): RefreshMinutes = %d, want 30", in, c.RefreshMinutes)
		}
		if !c.Enabled {
			t.Error("NormalizeAwarenessDefaults must not flip Enabled")
		}
	}
	c := config.AwarenessConfig{Enabled: true, RefreshMinutes: 15}
	config.NormalizeAwarenessDefaults(&c)
	if c.RefreshMinutes != 15 {
		t.Errorf("valid value must survive normalization: %d", c.RefreshMinutes)
	}
}

func TestWeatherRefresh_ConfigParsesFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.toml")
	body := "[awareness]\nenabled = true\nrefresh_minutes = 15\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if !cfg.Awareness.Enabled {
		t.Error("loaded config: Awareness.Enabled = false, want true")
	}
	if cfg.Awareness.RefreshMinutes != 15 {
		t.Errorf("loaded config: Awareness.RefreshMinutes = %d, want 15", cfg.Awareness.RefreshMinutes)
	}

	// Omitted table -> shipped defaults survive the load boundary.
	path2 := filepath.Join(dir, "empty.toml")
	if err := os.WriteFile(path2, []byte("[daemon]\ndata_dir = \"x\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg2, err := config.Load(path2)
	if err != nil {
		t.Fatalf("config.Load(empty): %v", err)
	}
	if cfg2.Awareness.Enabled || cfg2.Awareness.RefreshMinutes != 30 {
		t.Errorf("omitted [awareness]: got %+v, want {Enabled:false RefreshMinutes:30}", cfg2.Awareness)
	}
}

// ---------------------------------------------------------------------------
// Gate row: registration guarded by awareness.enabled AND >=1 employee def
// with Weather.Trades
// ---------------------------------------------------------------------------

func TestWeatherRefresh_DisabledRegistersNothing(t *testing.T) {
	st := newWeatherTestBotStore(t)
	putWeatherBot(t, st, "emp-rain", bot.BotWeather{Lat: 44.1, Lon: -103.3, Trades: []string{"concrete"}})

	c := &Components{
		Config:         weatherCfg(false, 30), // enabled=false even with weather employees
		Scheduler:      newWeatherTestScheduler(t),
		Logger:         slog.New(slog.DiscardHandler),
		BotStore:       st,
		WeatherFetcher: newWRFakeFetcher(),
	}
	c.registerWeatherRefreshJob(c.Config, slog.New(slog.DiscardHandler))
	if jobs := c.Scheduler.ListJobs(); len(jobs) != 0 {
		t.Fatalf("awareness disabled must register no job, got %d: %+v", len(jobs), jobs)
	}
}

func TestWeatherRefresh_NoTradeEmployeesRegisterNothing(t *testing.T) {
	st := newWeatherTestBotStore(t)
	// Zero Weather (no awareness) and weather-without-trades employees.
	putWeatherBot(t, st, "emp-plain", bot.BotWeather{})
	putWeatherBot(t, st, "emp-coords-only", bot.BotWeather{Lat: 44.1, Lon: -103.3})

	c := &Components{
		Config:         weatherCfg(true, 30),
		Scheduler:      newWeatherTestScheduler(t),
		Logger:         slog.New(slog.DiscardHandler),
		BotStore:       st,
		WeatherFetcher: newWRFakeFetcher(),
	}
	c.registerWeatherRefreshJob(c.Config, slog.New(slog.DiscardHandler))
	if jobs := c.Scheduler.ListJobs(); len(jobs) != 0 {
		t.Fatalf("no employee with Weather.Trades must register no job, got %d: %+v", len(jobs), jobs)
	}
}

func TestWeatherRefresh_NilBotStoreRegistersNothing(t *testing.T) {
	c := &Components{
		Config:         weatherCfg(true, 30),
		Scheduler:      newWeatherTestScheduler(t),
		Logger:         slog.New(slog.DiscardHandler),
		WeatherFetcher: newWRFakeFetcher(),
	}
	c.registerWeatherRefreshJob(c.Config, slog.New(slog.DiscardHandler))
	if jobs := c.Scheduler.ListJobs(); len(jobs) != 0 {
		t.Fatalf("nil BotStore must register no job, got %d: %+v", len(jobs), jobs)
	}
}

func TestWeatherRefresh_RegistersWithIntervalFromConfig(t *testing.T) {
	st := newWeatherTestBotStore(t)
	putWeatherBot(t, st, "emp-a", bot.BotWeather{Lat: 44.1, Lon: -103.3, Trades: []string{"concrete", "roofing"}})

	cfg := weatherCfg(true, 15)
	c := &Components{
		Config:         cfg,
		Scheduler:      newWeatherTestScheduler(t),
		Logger:         slog.New(slog.DiscardHandler),
		BotStore:       st,
		WeatherFetcher: newWRFakeFetcher(),
	}
	c.registerWeatherRefreshJob(cfg, slog.New(slog.DiscardHandler))

	var found bool
	for _, j := range c.Scheduler.ListJobs() {
		if j.ID == "weather-refresh" {
			found = true
			want := "@every " + (15 * time.Minute).String()
			if j.Schedule != want {
				t.Errorf("job schedule = %q, want %q", j.Schedule, want)
			}
		}
	}
	if !found {
		t.Fatalf("weather-refresh job not registered: %+v", c.Scheduler.ListJobs())
	}
}

// ---------------------------------------------------------------------------
// Gate row: run once -> cache warm (one fetch per distinct trades location)
// ---------------------------------------------------------------------------

func TestWeatherRefresh_RunOnceWarmsDistinctLocations(t *testing.T) {
	st := newWeatherTestBotStore(t)
	// Two employees share the shop location; one has its own; one has no
	// trades (D5: skip); one is weather-less.
	putWeatherBot(t, st, "emp-shop-1", bot.BotWeather{Lat: 44.08, Lon: -103.21, Trades: []string{"concrete"}})
	putWeatherBot(t, st, "emp-shop-2", bot.BotWeather{Lat: 44.08, Lon: -103.21, Trades: []string{"roofing"}})
	putWeatherBot(t, st, "emp-yard", bot.BotWeather{Lat: 43.5, Lon: -102.0, Trades: []string{"earthwork"}})
	putWeatherBot(t, st, "emp-notrades", bot.BotWeather{Lat: 50.0, Lon: -110.0})
	putWeatherBot(t, st, "emp-plain", bot.BotWeather{})

	fake := newWRFakeFetcher()
	job := NewWeatherRefreshJob(fake, st, slog.New(slog.DiscardHandler))
	warmed, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if warmed != 2 {
		t.Errorf("warmed = %d, want 2 distinct locations", warmed)
	}
	if got := fake.countAt(44.08, -103.21); got != 1 {
		t.Errorf("shop location fetches = %d, want 1 (two employees, one fetch)", got)
	}
	if got := fake.countAt(43.5, -102.0); got != 1 {
		t.Errorf("yard location fetches = %d, want 1", got)
	}
	if got := fake.total(); got != 2 {
		t.Errorf("total fetches = %d, want 2 (no-trades + weather-less skipped)", got)
	}
}

func TestWeatherRefresh_RunOnceFetchErrorDoesNotAbort(t *testing.T) {
	st := newWeatherTestBotStore(t)
	putWeatherBot(t, st, "emp-a", bot.BotWeather{Lat: 44.0, Lon: -103.0, Trades: []string{"concrete"}})
	putWeatherBot(t, st, "emp-b", bot.BotWeather{Lat: 45.0, Lon: -104.0, Trades: []string{"roofing"}})

	fake := newWRFakeFetcher()
	fake.failOn = true
	fake.failAt = [2]float64{44.0, -103.0}
	h := &wrCaptureHandler{}
	job := NewWeatherRefreshJob(fake, st, slog.New(h))
	warmed, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("per-location fetch errors must not abort the pass: %v", err)
	}
	if warmed != 1 {
		t.Errorf("warmed = %d, want 1 (one location failed, one succeeded)", warmed)
	}
	if got := fake.countAt(45.0, -104.0); got != 1 {
		t.Errorf("second location fetches = %d, want 1 after first failed", got)
	}
	if h.count("weather refresh") < 1 {
		t.Errorf("failed refresh must be logged; got %v", h.lines)
	}
}

// ---------------------------------------------------------------------------
// Gate row: scheduler-facing Execute (simpleIntervalJob idiom) + job identity
// ---------------------------------------------------------------------------

func TestWeatherRefresh_ExecuteRunsPass(t *testing.T) {
	st := newWeatherTestBotStore(t)
	putWeatherBot(t, st, "emp-a", bot.BotWeather{Lat: 44.0, Lon: -103.0, Trades: []string{"concrete"}})

	fake := newWRFakeFetcher()
	job := &weatherRefreshJob{
		id:       "weather-refresh",
		name:     "test",
		interval: 30 * time.Minute,
		runner:   NewWeatherRefreshJob(fake, st, slog.New(slog.DiscardHandler)),
		logger:   slog.New(slog.DiscardHandler),
	}
	if err := job.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := job.Schedule(); got != "@every 30m0s" {
		t.Errorf("Schedule() = %q, want @every 30m0s", got)
	}
	if jcfg := job.Config(); jcfg.ID != "weather-refresh" || jcfg.Type != scheduler.JobTypeInterval {
		t.Errorf("Config() = %+v, want id weather-refresh / type interval", jcfg)
	}
	if got := fake.total(); got != 1 {
		t.Errorf("Execute warmed %d locations, want 1", got)
	}
}
