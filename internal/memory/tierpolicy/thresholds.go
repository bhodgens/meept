// Demotion thresholds (contract C6, tree 02 leaf 03): the staleness and
// usefulness gates that decide when a semantic claim is demoted to the
// episodic tier (findings-memory-tiers.md R-B6).
//
// The config-schema plumbing (internal/config/schema.go [memory.tiers]
// .demotion block) is owned by the orchestrator merge - same posture as
// TiersConfig on leaf 02: this package owns the shape, its defaults, and
// the resolver so the schema merge stays mechanical. ALL KNOBS OPT-IN via
// the leaf-02 Enabled gate (docs/agents/opt-in-defaults.md).
//
// ASCII only.
package tierpolicy

// Demotion defaults (contract C6 / leaf T3): 30-day staleness window and
// a usefulness floor consistent with the useful-eviction floor concept
// (PlanUsefulEviction evicts the bottom floorPct=0.05 by score; the
// demotion floor is an absolute score so a single threshold works across
// differently-sized tiers). With the default weights (base 0.5, age
// penalty 0.005/day) an unvoted unaccessed claim decays to the 0.1 floor
// after ~80 days - staleness (30d) fires first, so the floor only claims
// voted-down or abandoned claims.
const (
	// DefaultDemotionStaleDays is the staleness window: a claim older
	// than this many days is demotion-eligible (R-B6 criterion 2).
	DefaultDemotionStaleDays = 30
	// DefaultDemotionMinUsefulness is the usefulness score floor
	// (R-B6 criterion 3): below it, a claim loses its semantic-tier
	// visibility (demoted, never deleted).
	DefaultDemotionMinUsefulness = 0.1
)

// DemotionThresholds is the resolved (default-applied) demotion gate set.
type DemotionThresholds struct {
	// StaleDays is the staleness window in days: claims older than this
	// are demotion-eligible with reason "stale" (R-B6 criterion 2).
	StaleDays int
	// MinUsefulness is the usefulness score floor: a claim scoring
	// below it is demotion-eligible with reason "low_usefulness"
	// (R-B6 criterion 3).
	MinUsefulness float64
}

// DemotionConfig is the raw [memory.tiers.demotion] knob set. Zero/negative
// values resolve to the documented defaults (contract C6: "zero-value
// config => defaults; negative rejected at resolve").
type DemotionConfig struct {
	// StaleDays overrides DefaultDemotionStaleDays. <= 0 resolves to the
	// default.
	StaleDays int
	// MinUsefulness overrides DefaultDemotionMinUsefulness. <= 0,
	// >= 1, or non-finite (NaN/±Inf) resolves to the default.
	MinUsefulness float64
}

// ResolveDemotionThresholds applies defaults to unset/negative fields
// (leaf T3 Given/When/Then: unset => 30d + documented floor; negative =>
// rejected, i.e. resolved back to the defaults). Idempotent: feeding an
// already-resolved threshold set through again is a no-op.
func ResolveDemotionThresholds(cfg DemotionConfig) DemotionThresholds {
	out := DemotionThresholds(cfg)
	if out.StaleDays <= 0 {
		out.StaleDays = DefaultDemotionStaleDays
	}
	// MinUsefulness must be a finite score in the open interval (0,1).
	// The inverted range check is NaN-safe: NaN fails both comparisons,
	// so !(x > 0 && x < 1) routes NaN (and ±Inf, and <=0, and >=1) to
	// the default instead of letting it pass through verbatim (F4 fix,
	// property-test finding 2026-11-06: a NaN floor made demote.go's
	// `score < th.MinUsefulness` gate silently never fire). Convention:
	// normalize to defaults — the resolver rejects by defaulting, it
	// has no error return (contract C6: "zero-value config =>
	// defaults; negative rejected at resolve").
	if !(out.MinUsefulness > 0 && out.MinUsefulness < 1) {
		out.MinUsefulness = DefaultDemotionMinUsefulness
	}
	return out
}
