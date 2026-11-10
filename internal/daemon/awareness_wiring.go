package daemon

// Situational-awareness wiring (plan 20261110-situational leaf 01-moment/02):
// builds the moment.Provider from the [awareness] config and hands back the
// per-loop moment renderers that (*agent.AgentLoop).SetMomentRenderer
// consumes — chat lane adds the rendered block as the volatile "moment"
// builder section, voice/override lane appends it after any Global Rules
// suffix (00T-moment gate §3/§4).
//
// Disabled posture ([awareness] enabled = false, the default): buildAwareness
// returns a nil Provider + nil resolver, no renderer is ever installed, and
// every prompt stays byte-identical to the pre-tree shape (D1 golden).
//
// This leaf carries ONLY the time/location half of the block: Weather is nil
// (the weather feed is leaf 03-moment/02) and DayState is empty (the daystate
// feed is leaf 02-daystate/02) — moment.Render's omission rules make the
// absent lines vanish, so the block is exactly
//
//	# Moment
//	It is 14:02 MST, Monday, at <LocationLabel>.
//
// per employee def. Defs come from the employee's BotDefinition awareness
// fields (Timezone, LocationLabel; the persona map feeds daystate tone in
// the sibling leaf). Each def is cached per employee id with a short TTL so
// a bot-store edit reaches the next turn without a per-turn SQL read.

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/activity"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/daystate"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/ledger"
	"github.com/caimlas/meept/internal/moment"
	"github.com/caimlas/meept/internal/voice"
	"github.com/caimlas/meept/internal/weather"
)

// aw2DefCacheTTL bounds how long a resolved employee moment def is reused.
// The def carries only BotDefinition fields (slow-changing, operator-edited),
// so a minute is well inside "edit reaches the next turn" UX while keeping
// the per-turn render cost at zero SQL.
const aw2DefCacheTTL = time.Minute

// aw2CachedDef is one employee's moment-def cache entry. absent is the
// negative cache (unknown/unresolvable employee -> inert renderer, zero
// repeated SQL per turn).
type aw2CachedDef struct {
	def    moment.EmployeeDef
	cached time.Time
	absent bool
}

// aw2Glue is the state behind the resolver closure buildAwareness returns.
type aw2Glue struct {
	provider  *moment.Provider
	employees EmployeeDefinitionResolver
	logger    *slog.Logger

	// daystate + weather feeds (sibling leaves 02-daystate/02,
	// 03-weather/02). nil = that feed stays absent (omission rules keep
	// the lines out of the block).
	daystate daystate.Source
	wcache   *weather.Cache

	mu   sync.Mutex
	defs map[string]aw2CachedDef
}

// aw2Feeds are the optional situational feeds a caller attaches to the
// glue (leaf 02-daystate/02 + 03-weather/02). Zero-value/nil fields keep
// the corresponding moment line absent, matching the 01-moment/02 inert
// posture. Passed variadically so the pinned 3-arg buildAwareness call
// shape (gate aw2*) stays byte-compatible.
type aw2Feeds struct {
	DayState daystate.Source // nil -> no Day state line
	WCache   *weather.Cache  // nil -> no Weather line
}

// buildAwareness constructs the situational-awareness glue (02-wiring.md
// Task 1): returns (*moment.Provider, per-employee renderer resolver) when
// cfg.Awareness.Enabled, else (nil, nil) — the D1 inert posture. The
// resolver maps an employee id (the loop's agent id; voice clones are
// stamped with the bound employee id by the turn runner) to the
// SetMomentRenderer-shaped closure; unknown ids resolve to a renderer that
// returns "" (section omitted, prompt unchanged).
//
// Clock: real time.Now on every render (the Provider re-reads each turn, so
// day/week transitions cannot go stale — 02-wiring.md Task 5). Default
// location: cfg.Scheduler.Timezone -> UTC fallback (the def-level fallback
// chain def.Timezone -> defaultLoc -> UTC lives inside moment.Provider;
// moment.Render WARNs once per bad def-level zone).
func buildAwareness(cfg *config.Config, employees EmployeeDefinitionResolver, logger *slog.Logger, feeds ...aw2Feeds) (*moment.Provider, func(employeeID string) func() string) {
	if cfg == nil || !cfg.Awareness.Enabled {
		return nil, nil
	}
	if employees == nil {
		if logger != nil {
			logger.Warn("awareness enabled but no employee definition source wired (moment block inert)")
		}
		return nil, nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	provider := moment.New(time.Now, aw2SchedulerLoc(cfg, logger), logger.With("component", "moment"))
	g := &aw2Glue{
		provider:  provider,
		employees: employees,
		logger:    logger.With("component", "awareness"),
		defs:      make(map[string]aw2CachedDef),
	}
	if len(feeds) > 0 {
		g.daystate = feeds[0].DayState
		g.wcache = feeds[0].WCache
	}
	return provider, g.rendererFor
}

// aw2SchedulerLoc resolves cfg.Scheduler.Timezone into the Provider's
// default location: valid IANA name -> that zone; empty/unparseable -> UTC.
// A bad scheduler timezone is daemon config, so it logs one WARN here (the
// def-level bad-zone WARN belongs to moment.Render).
func aw2SchedulerLoc(cfg *config.Config, logger *slog.Logger) *time.Location {
	tz := ""
	if cfg != nil {
		tz = cfg.Scheduler.Timezone
	}
	if tz == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		if logger != nil {
			logger.Warn("awareness: invalid scheduler.timezone, defaulting to UTC", "timezone", tz, "error", err)
		}
		return time.UTC
	}
	return loc
}

// rendererFor returns the moment renderer closure for one employee id, or
// nil when the id is empty or resolves to NO employee definition (unknown
// "default"-template ids, deleted employees, transient store errors) — nil
// means SetMomentRenderer installs the pinned inert seam (D1: zero SQL per
// turn on those loops, byte-identical prompts). A resolvable id returns a
// closure that renders the block fresh on every call (per-turn clock).
// Def resolution is cached (aw2DefCacheTTL), so stamping this on every
// voice turn costs at most one GetEmployee per employee per minute.
func (g *aw2Glue) rendererFor(employeeID string) func() string {
	if g == nil || employeeID == "" {
		return nil
	}
	if _, ok := g.defFor(employeeID); !ok {
		return nil
	}
	return func() string {
		// Re-resolve per render so a bot-store tz/label edit lands within
		// the TTL; the fresh def read is cache-hit cost.
		fresh, ok := g.defFor(employeeID)
		if !ok {
			return ""
		}
		block := g.provider.Render(fresh)
		// Live sanity hook (03-weather/02 task 3): one DEBUG line per
		// render, grep 'moment render'. DEBUG so a production INFO log
		// never carries prompt content.
		g.logger.Debug("moment render", "employee_id", employeeID, "block", block)
		return block
	}
}

// defFor resolves (and caches) the employee's moment def from the
// BotDefinition fields. Fail-soft: a lookup error logs at debug and caches
// the absence until the TTL — a turn is never blocked or dropped because
// the awareness read failed (same posture as the voice persona stamp).
func (g *aw2Glue) defFor(employeeID string) (*moment.EmployeeDef, bool) {
	g.mu.Lock()
	entry, ok := g.defs[employeeID]
	fresh := ok && time.Since(entry.cached) < aw2DefCacheTTL
	g.mu.Unlock()
	if fresh {
		if entry.absent {
			return nil, false
		}
		def := entry.def
		return &def, true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	emp, err := g.employees.GetEmployee(ctx, employeeID)
	if err != nil || emp == nil {
		g.logger.Debug("awareness: employee definition unavailable for moment def",
			"employee_id", employeeID, "error", err)
		g.mu.Lock()
		g.defs[employeeID] = aw2CachedDef{cached: time.Now(), absent: true}
		g.mu.Unlock()
		return nil, false
	}

	def := moment.EmployeeDef{
		Timezone:      emp.Timezone,
		LocationLabel: emp.LocationLabel,
	}

	// Day-state feed (leaf 02-daystate/02): Signals -> Compute -> Tone
	// (persona override map from BotDefinition.PersonaDayState). All
	// sources failing -> DayState stays "" and the renderer omits the
	// line (master §Signals-source; §F omission posture). Healthy-but-
	// zero signals still render "clear" (distinct posture).
	if g.daystate != nil {
		sigs, healthy := g.daySignals(ctx, employeeID)
		if healthy {
			st := daystate.Compute(sigs)
			def.DayState = string(st)
			def.OpenCommitments = sigs.OpenCommitments
			def.Overdue = sigs.Overdue
			def.BlockedGoals = sigs.BlockedGoals
			def.Tone = daystate.Tone(st, emp.PersonaDayState)
		}
	}

	// Weather feed (leaf 03-weather/02): read-only peek of the shared
	// lat/lon-keyed cache (master Q3). Cold cache -> no Weather line;
	// a render NEVER triggers or blocks on an upstream fetch — the
	// weather-refresh job owns warming. Alerts come from the ratified
	// per-trade thresholds in internal/weather.
	if g.wcache != nil && len(emp.Weather.Trades) > 0 {
		if o, ok := g.wcache.PeekOutlook(emp.Weather.Lat, emp.Weather.Lon); ok {
			outlook := o
			def.Weather = &outlook
			def.WeatherAlerts = weather.AlertsFor(emp.Weather.Trades, o)
		}
	}

	g.mu.Lock()
	g.defs[employeeID] = aw2CachedDef{def: def, cached: time.Now()}
	g.mu.Unlock()
	return &def, true
}

// daySignals runs the daystate source and reports whether ANY underlying
// store answered (health). Sources that implement the health seam are
// probed with SignalsWithHealth; plain daystate.Source implementations
// fall back to "no error = healthy".
func (g *aw2Glue) daySignals(ctx context.Context, employeeID string) (daystate.Signals, bool) {
	if hs, ok := g.daystate.(dayStateHealthSource); ok {
		sigs, healthy, err := hs.SignalsWithHealth(ctx, employeeID)
		if err != nil {
			g.logger.Debug("awareness: daystate source failed (line omitted)",
				"employee_id", employeeID, "error", err)
			return daystate.Signals{}, false
		}
		return sigs, healthy
	}
	sigs, err := g.daystate.Signals(ctx, employeeID)
	if err != nil {
		g.logger.Debug("awareness: daystate source failed (line omitted)",
			"employee_id", employeeID, "error", err)
		return daystate.Signals{}, false
	}
	return sigs, true
}

// ---------------------------------------------------------------------------
// composite daystate.Source over the daemon stores (§E frozen seam)
// ---------------------------------------------------------------------------

// dayStateSourceBudget is the per-underlying-source context budget
// (master D-contract: read-only, bounded). Inline-compute posture for
// master Q1: worst case 4 sources x 2s = 8s, but ONLY when a store
// wedges; all four are local SQLite reads (sub-millisecond healthy), and
// the def cache (aw2DefCacheTTL) amortises the pass to at most one per
// employee per minute, fail-soft so a wedged source costs its 2s then
// contributes zero. A warm-cache daystate job remains an option for a
// later leaf (plan master Q1 note).
const dayStateSourceBudget = 2 * time.Second

// activityDayStateLimit is the frozen activity query limit for the
// day-state signal (gate §E pins Query.Limit == 1000).
const activityDayStateLimit = 1000

// ledgerOpenQuerier / activeGoalLister / callDayCounter /
// activityQuerySource are the §E seam interfaces — satisfied by the real
// *ledger.Store, *employee.GoalStore, *voice.CallStore and
// *activity.Aggregator, and by the gate's fakes.
type (
	ledgerOpenQuerier interface {
		QueryOpen(ctx context.Context, employeeID string) ([]ledger.State, error)
	}
	activeGoalLister interface {
		ListActive(ctx context.Context, employeeID string) ([]*employee.Goal, error)
	}
	callDayCounter interface {
		TodayCountsByEmployee(ctx context.Context, employeeID string, loc *time.Location) (voice.CallDayCounts, error)
	}
	activityQuerySource interface {
		Query(ctx context.Context, q activity.Query) ([]activity.ActivityRecord, int, error)
	}
)

// dayStateSourceDeps is the frozen §E construction seam for the
// composite source: the clock and location seams plus the four stores
// (interface-typed so the gate fakes and the real stores both wire in)
// and a logger. Nil optional fields skip their source (zero
// contribution); nil Now -> time.Now; nil Loc -> UTC; nil Logger ->
// slog.Default.
type dayStateSourceDeps struct {
	Now      func() time.Time
	Loc      *time.Location
	Ledger   ledgerOpenQuerier
	Goals    activeGoalLister
	Calls    callDayCounter
	Activity activityQuerySource
	Logger   *slog.Logger
}

// ledgerDayStateSource implements daystate.Source over the four daemon
// stores. Fail-soft posture: any source error zeroes that source's
// contribution and logs at DEBUG; Signals NEVER returns an error and
// NEVER blocks a turn.
type ledgerDayStateSource struct {
	deps dayStateSourceDeps
}

// dayStateHealthSource is the internal health seam the moment glue probes
// (optional-interface, not part of the frozen daystate.Source contract):
// the bool reports whether at least one underlying source answered, which
// distinguishes the §F "all sources fail -> omit the line" posture from a
// healthy-but-empty "clear" day.
type dayStateHealthSource interface {
	SignalsWithHealth(ctx context.Context, employeeID string) (daystate.Signals, bool, error)
}

var (
	_ daystate.Source      = (*ledgerDayStateSource)(nil)
	_ dayStateHealthSource = (*ledgerDayStateSource)(nil)
)

// newLedgerDayStateSource builds the composite source (frozen §E
// constructor name).
func newLedgerDayStateSource(d dayStateSourceDeps) daystate.Source {
	return newDayStateSource(d)
}

// newDayStateSource is the concrete-typed constructor used by production
// wiring (so the caller keeps the health seam).
func newDayStateSource(d dayStateSourceDeps) *ledgerDayStateSource {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Loc == nil {
		d.Loc = time.UTC
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	return &ledgerDayStateSource{deps: d}
}

// Signals implements daystate.Source — per the frozen contract it always
// returns a nil error (fail-soft); zero Signals when nothing answered.
func (s *ledgerDayStateSource) Signals(ctx context.Context, employeeID string) (daystate.Signals, error) {
	sigs, _, _ := s.SignalsWithHealth(ctx, employeeID)
	return sigs, nil
}

// SignalsWithHealth runs the four sources with the bounded, fail-soft
// posture and reports (signals, anySourceAnswered, nil).
func (s *ledgerDayStateSource) SignalsWithHealth(ctx context.Context, employeeID string) (daystate.Signals, bool, error) {
	var sigs daystate.Signals
	healthy := 0

	if s.deps.Ledger != nil {
		now := s.deps.Now()
		rows, err := s.queryOpen(ctx, employeeID)
		if err != nil {
			s.debugFail("ledger", employeeID, err)
		} else {
			healthy++
			sigs.OpenCommitments = len(rows)
			for _, r := range rows {
				if !r.Due.IsZero() && r.Due.Before(now) {
					sigs.Overdue++
				}
			}
		}
	}

	if s.deps.Goals != nil {
		goals, err := s.listActiveGoals(ctx, employeeID)
		if err != nil {
			s.debugFail("goals", employeeID, err)
		} else {
			healthy++
			for _, g := range goals {
				if g == nil {
					continue
				}
				switch g.State {
				case employee.GoalActive:
					sigs.ActiveGoals++
				case employee.GoalBlocked:
					sigs.BlockedGoals++
				}
			}
		}
	}

	if s.deps.Calls != nil {
		counts, err := s.callCounts(ctx, employeeID)
		if err != nil {
			s.debugFail("calls", employeeID, err)
		} else {
			healthy++
			sigs.CallsToday = counts.Today
			sigs.CallsFailedToday = counts.Failed
		}
	}

	if s.deps.Activity != nil {
		n, err := s.activityCount(ctx, employeeID)
		if err != nil {
			s.debugFail("activity", employeeID, err)
		} else {
			healthy++
			sigs.ActivityItemsToday = n
		}
	}

	return sigs, healthy > 0, nil
}

func (s *ledgerDayStateSource) queryOpen(ctx context.Context, employeeID string) ([]ledger.State, error) {
	sctx, cancel := context.WithTimeout(ctx, dayStateSourceBudget)
	defer cancel()
	return s.deps.Ledger.QueryOpen(sctx, employeeID)
}

func (s *ledgerDayStateSource) listActiveGoals(ctx context.Context, employeeID string) ([]*employee.Goal, error) {
	sctx, cancel := context.WithTimeout(ctx, dayStateSourceBudget)
	defer cancel()
	return s.deps.Goals.ListActive(sctx, employeeID)
}

func (s *ledgerDayStateSource) callCounts(ctx context.Context, employeeID string) (voice.CallDayCounts, error) {
	sctx, cancel := context.WithTimeout(ctx, dayStateSourceBudget)
	defer cancel()
	return s.deps.Calls.TodayCountsByEmployee(sctx, employeeID, s.deps.Loc)
}

func (s *ledgerDayStateSource) activityCount(ctx context.Context, employeeID string) (int, error) {
	sctx, cancel := context.WithTimeout(ctx, dayStateSourceBudget)
	defer cancel()
	now := s.deps.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.deps.Loc)
	records, _, err := s.deps.Activity.Query(sctx, activity.Query{
		AgentID: employeeID,
		Since:   midnight,
		Limit:   activityDayStateLimit,
	})
	if err != nil {
		return 0, err
	}
	return len(records), nil
}

func (s *ledgerDayStateSource) debugFail(source, employeeID string, err error) {
	s.deps.Logger.Debug("daystate: source failed (zero contribution)",
		"source", source, "employee_id", employeeID, "error", err)
}

// ---------------------------------------------------------------------------
// production composite wiring (typed-nil safe)
// ---------------------------------------------------------------------------

// buildDayStateSource wires the composite over the Components handles.
// Every dep is a lazy adapter resolved at query time: the awareness site
// in NewComponents runs BEFORE the ledger/voice/activity wiring lands, and
// a subsystem that never comes up (disabled voice, no aggregator) surfaces
// as an ordinary source error -> zero contribution via the fail-soft path,
// instead of a construction-time nil crash or a permanently-missing dep.
func buildDayStateSource(c *Components, loc *time.Location, logger *slog.Logger) *ledgerDayStateSource {
	return newDayStateSource(dayStateSourceDeps{
		Loc:      loc,
		Ledger:   lazyLedgerQuerier{components: c},
		Goals:    lazyGoalLister{components: c},
		Calls:    lazyCallCounter{components: c},
		Activity: lazyActivityQuerier{components: c},
		Logger:   logger,
	})
}

// lazyLedgerQuerier defers to c.EmployeeLedger (built later in
// NewComponents by buildLedger; nil -> source error -> zero).
type lazyLedgerQuerier struct{ components *Components }

func (l lazyLedgerQuerier) QueryOpen(ctx context.Context, employeeID string) ([]ledger.State, error) {
	if l.components == nil || l.components.EmployeeLedger == nil {
		return nil, errors.New("employee ledger not wired")
	}
	return l.components.EmployeeLedger.QueryOpen(ctx, employeeID)
}

// lazyGoalLister defers to c.EmployeeGoalStore (nil when the employee
// stack is disabled).
type lazyGoalLister struct{ components *Components }

func (l lazyGoalLister) ListActive(ctx context.Context, employeeID string) ([]*employee.Goal, error) {
	if l.components == nil || l.components.EmployeeGoalStore == nil {
		return nil, errors.New("employee goal store not wired")
	}
	return l.components.EmployeeGoalStore.ListActive(ctx, employeeID)
}

// lazyCallCounter defers to the voice stack's CallStore, which is only
// live after WireVoice (later in NewComponents). A disabled/absent voice
// block is an ordinary source error -> zero contribution.
type lazyCallCounter struct{ components *Components }

func (l lazyCallCounter) TodayCountsByEmployee(ctx context.Context, employeeID string, loc *time.Location) (voice.CallDayCounts, error) {
	if l.components == nil || l.components.Voice == nil || l.components.Voice.Employee == nil || l.components.Voice.Employee.store == nil {
		return voice.CallDayCounts{}, errors.New("voice call store not wired")
	}
	return l.components.Voice.Employee.store.TodayCountsByEmployee(ctx, employeeID, loc)
}

// lazyActivityQuerier defers to c.ActivityAggregator, which daemon.go
// wires after NewComponents returns (wireActivityComponents).
type lazyActivityQuerier struct{ components *Components }

func (l lazyActivityQuerier) Query(ctx context.Context, q activity.Query) ([]activity.ActivityRecord, int, error) {
	if l.components == nil || l.components.ActivityAggregator == nil {
		return nil, 0, errors.New("activity aggregator not wired")
	}
	return l.components.ActivityAggregator.Query(ctx, q)
}
