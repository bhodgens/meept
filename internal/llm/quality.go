package llm

// Quality dimension (Contract C2, sovereign-router-parity leaf 01).
//
// Governing rule inherited from the Sovereign Router design: "leaderboards
// nominate, our own probes score." Quality scores come from MEASUREMENT
// only — the config records provenance (quality_source /
// quality_verified_at) — never from vendor documentation. The shipped
// config/models.json5 seeds NO model (plan Q1 ruling): an absent quality
// block unmarshals to the zero Quality, and every selection path that
// consults this dimension falls back to its pre-leaf pure-cost order
// byte-identically. The feature is opt-in by construction.

import "math"

// QualityClass names one measured quality dimension. The five classes are
// the frozen C2 vocabulary and match the Sovereign Router registry's
// quality shape.
type QualityClass string

const (
	QualityChat        QualityClass = "chat"
	QualityCode        QualityClass = "code"
	QualityReasoning   QualityClass = "reasoning"
	QualityTooling     QualityClass = "tooling"
	QualityLongContext QualityClass = "long-context"
)

// Quality carries the five measured scores (0-5), one per class. The JSON
// tags are the config surface inside a models.json5 per-model "quality"
// block; the zero value is the shipped default (no model declares quality,
// so the dimension is inert).
type Quality struct {
	Chat        int `json:"chat"`
	Code        int `json:"code"`
	Reasoning   int `json:"reasoning"`
	Tooling     int `json:"tooling"`
	LongContext int `json:"long-context"`
}

// QualityFor returns the measured score for class. Unknown class -> 0
// (an unmeasured or unrecognized class can never pass a non-zero floor).
func (q Quality) QualityFor(c QualityClass) int {
	switch c {
	case QualityChat:
		return q.Chat
	case QualityCode:
		return q.Code
	case QualityReasoning:
		return q.Reasoning
	case QualityTooling:
		return q.Tooling
	case QualityLongContext:
		return q.LongContext
	default:
		return 0
	}
}

// IsZero reports whether no class carries a score. Every selection path
// treats an all-zero Quality as "dimension not measured": the floor never
// binds and the comparator's quality term vanishes (flag-off
// byte-identical, Contract C2 inertness rule).
func (q Quality) IsZero() bool {
	return q == Quality{}
}

// QualityFloorFor maps a classified difficulty (1..5) to the minimum
// acceptable score for a capability. FROZEN table (master Contract C2,
// quoted verbatim — do not paraphrase):
//
//	difficulty <= 0  -> 0      // unclassified: feature inert
//	difficulty 1..2  -> 1
//	difficulty 3     -> 2
//	difficulty 4     -> 3
//	difficulty 5     -> 4
//	capability not in the five classes -> 0
func QualityFloorFor(difficulty int, capability string) int {
	if _, ok := qualityClassForCapability(capability); !ok {
		return 0
	}
	switch {
	case difficulty <= 0:
		return 0
	case difficulty <= 2:
		return 1
	case difficulty == 3:
		return 2
	case difficulty == 4:
		return 3
	default:
		return 4
	}
}

// qualityClassForCapability maps a capability string to its quality class.
// Only the exact five class names map; everything else ("completion",
// "tool_use", "vision", …) has no measured dimension. Unexported: the
// vocabulary is frozen and later leaves (02/05/09) consume it through the
// chain, not by re-deriving it.
func qualityClassForCapability(capability string) (QualityClass, bool) {
	switch QualityClass(capability) {
	case QualityChat, QualityCode, QualityReasoning, QualityTooling, QualityLongContext:
		return QualityClass(capability), true
	default:
		return "", false
	}
}

// firstQualityClass returns the first capability in caps that maps to a
// quality class, or "" when none does (floor 0 = inert).
func firstQualityClass(caps []string) QualityClass {
	for _, c := range caps {
		if class, ok := qualityClassForCapability(c); ok {
			return class
		}
	}
	return ""
}

// costEpsilon bounds float cost equality (repo pitfall 26: never compare
// float64 with bare !=). Costs are human-authored $/M decimals; only
// float-representation noise lives inside the band.
const costEpsilon = 1e-9

// byCostThenQuality is THE shared selection comparator (frozen contract,
// gateway.py:746-759 shape): estimated turn cost ascending, then higher
// quality first, then lower latency first. FindCheapest and the
// ResolveForSkill escalation sort both call it — no duplicated sort logic.
//
// Cost is the per-turn estimate EstInput*in + EstOutput*out (the
// gateway.py:746 estimate_cost shape), NOT TotalCost() — TotalCost is a
// $/M-token rate whose plain comparison would rank a local zero-$ model
// over a cheap paid one even when the paid model's per-turn cost is
// lower, and would leave the quality term unreachable whenever rate sums
// differ (T2's cheap rate 2 vs dear rate 10). A zero estimate (no caller
// estimate available) ranks as free: quality then breaks the tie, which
// is the measured-scores-beat-unknown-cost posture the plan pins.
//
// latency is 0 for every model in v1: ModelConfig has no per-model latency
// field yet, and leaf 01 must not invent a latency config key. The
// registry.json latency_p50_ms column of the Sovereign Router registry is
// the named future source; until it lands the third term compares 0 == 0
// and never orders. With all-zero quality the quality term vanishes and
// the order reduces to the legacy TotalCost() ascending order (exact
// cost ties were already unordered under sort.Slice).
func byCostThenQuality(a, b *ModelConfig, class QualityClass, estIn, estOut int) bool {
	ca := turnCostEstimate(a, estIn, estOut)
	cb := turnCostEstimate(b, estIn, estOut)
	if math.Abs(ca-cb) > costEpsilon {
		return ca < cb
	}
	// No caller estimate (estIn/estOut both 0): the per-turn estimate is
	// 0 for every model and carries no ordering information, so fall back
	// to the $/M rate sum — the exact pre-leaf TotalCost() order — before
	// consulting quality. This keeps live-priced configurations
	// (SetPricingSyncer) ordering by price when quality is tied, and an
	// unmeasured set ordering exactly as before the leaf.
	if estIn == 0 && estOut == 0 {
		ta, tb := a.TotalCost(), b.TotalCost()
		if math.Abs(ta-tb) > costEpsilon {
			return ta < tb
		}
	}
	qa, qb := a.Quality.QualityFor(class), b.Quality.QualityFor(class)
	if qa != qb {
		return qa > qb // higher quality first
	}
	// Latency tiebreak: 0 == 0 for every model in v1 — never reorders.
	return false
}

// turnCostEstimate prices one estimated turn: per-million rates applied to
// the estimated token counts (gateway.py estimate_cost without the cached
// term — meept's ModelConfig has no cached-input rate in v1).
func turnCostEstimate(m *ModelConfig, estIn, estOut int) float64 {
	return (float64(estIn)*m.CostPerMillionInput + float64(estOut)*m.CostPerMillionOutput) / 1_000_000.0
}
