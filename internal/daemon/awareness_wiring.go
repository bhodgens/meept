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
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/moment"
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

	mu   sync.Mutex
	defs map[string]aw2CachedDef
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
func buildAwareness(cfg *config.Config, employees EmployeeDefinitionResolver, logger *slog.Logger) (*moment.Provider, func(employeeID string) func() string) {
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
		return g.provider.Render(fresh)
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
		// Weather + day-state feeds land in sibling leaves (03-moment/02,
		// 02-daystate/02); the omission rules keep those lines out until
		// then. PersonaDayState is consumed there via daystate.Tone.
	}
	g.mu.Lock()
	g.defs[employeeID] = aw2CachedDef{def: def, cached: time.Now()}
	g.mu.Unlock()
	return &def, true
}
