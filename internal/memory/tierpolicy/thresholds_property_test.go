package tierpolicy

// Property-based tests for the demotion threshold resolver
// (thresholds.go — contract C6, tree 02 leaf 03).
//
// Hand-rolled, zero new deps, deterministic seed (skill:
// software-development/property-based-testing; in-house pattern:
// strategist-usa go-shard-runner parse_csv_property_test.go).
//
// Properties (implementation contract refs are thresholds.go line
// numbers at the time of writing):
//
//	P1 idempotence: feeding an already-resolved threshold set through
//	   ResolveDemotionThresholds again is a no-op
//	   (thresholds.go:57-58 "Idempotent"; the resolver's mapping
//	   DemotionConfig -> DemotionThresholds is total and stable on its
//	   image, thresholds.go:59-68). Structural: ResolveDemotionThresholds
//	   is not idempotent AS WRITTEN on the fn-apply form — the documented
//	   no-op is the FORM Resolve(c) then field-wise copy into DemotionConfig
//	   then Resolve again (zero/negative fields are impossible in the
//	   image, so the second pass must return the input unchanged).
//	P2 default application (defaults conservation): any config with
//	   StaleDays <= 0 resolves StaleDays == DefaultDemotionStaleDays;
//	   any MinUsefulness outside (0,1) resolves to
//	   DefaultDemotionMinUsefulness (thresholds.go:61-66, contract C6
//	   "zero-value config => defaults").
//	P3 bound conservation (in-image): any positive StaleDays passes
//	   through EXACTLY; any MinUsefulness in (0,1) passes through
//	   EXACTLY — the resolver never invents values for in-range knobs
//	   (the merge must be mechanical; thresholds.go:7-9).
//	P4 no-panic: arbitrary float bits (NaN, ±Inf, subnormals) and any
//	   int never panic the resolver (thresholds.go:59-68).
//
// FIXED FINDING (property counterexample, 2026-11-06; F4): NaN/±Inf used
// to pass through the resolver verbatim — MinUsefulness=NaN fails both
// `<= 0` and `>= 1` (old thresholds.go:64), so it landed in the image as
// NaN, outside the documented default contract, and demote.go:221
// `score < th.MinUsefulness` with NaN is always-false: the demote gate
// silently never fired. Fixed: the resolver's range check is now the
// NaN-safe inverted form !(x > 0 && x < 1), routing NaN/±Inf (and <=0,
// and >=1) to DefaultDemotionMinUsefulness — the package convention is
// normalize-to-defaults, not error returns (contract C6). Asserted by
// TestResolveDemotion_NonFiniteResolvesToDefault below.
//
// Generator: seed 20261106, rand.New(rand.NewSource(...)), 5000 cases
// per property. On failure the seed and case print for replay.

import (
	"math"
	"math/rand"
	"testing"
)

const tierPropSeed = int64(20261106)

func randConfig(r *rand.Rand) DemotionConfig {
	stale := 0
	switch r.Intn(3) {
	case 0:
		stale = r.Intn(400) - 50 // includes zero/negative
	case 1:
		stale = DefaultDemotionStaleDays
	default:
		stale = r.Intn(3650)
	}
	var minU float64
	switch r.Intn(4) {
	case 0:
		minU = 0
	case 1:
		minU = float64(r.Intn(2000)-500) / 1000.0 // includes <=0 and >=1 band
	case 2:
		minU = float64(r.Intn(999)+1) / 1000.0 // in (0,1)
	default:
		minU = math.Float64frombits(r.Uint64()) // arbitrary bits: NaN/Inf/subnormal
	}
	return DemotionConfig{StaleDays: stale, MinUsefulness: minU}
}

func assertDefaults(t *testing.T, i int, in DemotionConfig, out DemotionThresholds) {
	t.Helper()
	if in.StaleDays <= 0 && out.StaleDays != DefaultDemotionStaleDays {
		t.Fatalf("case %d: StaleDays %d resolved to %d, want default %d", i, in.StaleDays, out.StaleDays, DefaultDemotionStaleDays)
	}
	// F4 fix: every out-of-(0,1) value — including NaN and ±Inf, which
	// the old `<= 0 || >= 1` check let through — resolves to the default.
	if !(in.MinUsefulness > 0 && in.MinUsefulness < 1) {
		if out.MinUsefulness != DefaultDemotionMinUsefulness {
			t.Fatalf("case %d: MinUsefulness %v resolved to %v, want default %v", i, in.MinUsefulness, out.MinUsefulness, DefaultDemotionMinUsefulness)
		}
	}
}

// TestResolveDemotion_IdempotenceOnImage ports P1: re-resolving a resolved
// set (round-tripped through the config shape) returns it unchanged —
// the documented idempotence contract (thresholds.go:57-58). The F4 fix
// made the image finite-only (NaN/±Inf resolve to the default), so plain
// struct equality suffices.
func TestResolveDemotion_IdempotenceOnImage(t *testing.T) {
	r := rand.New(rand.NewSource(tierPropSeed))
	for i := 0; i < 5000; i++ {
		cfg := randConfig(r)
		first := ResolveDemotionThresholds(cfg)
		again := ResolveDemotionThresholds(DemotionConfig(first))
		if again != first {
			t.Fatalf("case %d: not idempotent: first=%+v second=%+v (cfg=%+v)", i, first, again, cfg)
		}
		assertDefaults(t, i, cfg, first)
	}
}

// TestResolveDemotion_PassthroughInImage ports P2+P3: in-range knobs pass
// through exactly; out-of-range knobs land on the documented defaults.
func TestResolveDemotion_PassthroughInImage(t *testing.T) {
	r := rand.New(rand.NewSource(tierPropSeed + 1))
	for i := 0; i < 5000; i++ {
		cfg := randConfig(r)
		out := ResolveDemotionThresholds(cfg)
		if cfg.StaleDays > 0 && out.StaleDays != cfg.StaleDays {
			t.Fatalf("case %d: in-range StaleDays mutated: %d -> %d", i, cfg.StaleDays, out.StaleDays)
		}
		if cfg.MinUsefulness > 0 && cfg.MinUsefulness < 1 && out.MinUsefulness != cfg.MinUsefulness {
			t.Fatalf("case %d: in-range MinUsefulness mutated: %v -> %v", i, cfg.MinUsefulness, out.MinUsefulness)
		}
		assertDefaults(t, i, cfg, out)
	}
}

// TestResolveDemotion_NeverPanics ports P4: NaN/±Inf/subnormal knobs and
// extreme ints resolve without panicking, and the resolved value is never
// NaN in the ordinary finite-input stream.
func TestResolveDemotion_NeverPanics(t *testing.T) {
	specials := []float64{math.NaN(), math.Inf(1), math.Inf(-1), math.SmallestNonzeroFloat64, math.MaxFloat64}
	for i, f := range specials {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					t.Fatalf("special %d (%v) panicked: %v", i, f, rec)
				}
			}()
			out := ResolveDemotionThresholds(DemotionConfig{StaleDays: math.MaxInt32, MinUsefulness: f})
			// F4 fix: NaN never lands in the image — non-finite knobs
			// resolve to the default.
			if math.IsNaN(out.MinUsefulness) || math.IsInf(out.MinUsefulness, 0) {
				t.Fatalf("special %d (%v) resolved to non-finite %v — F4 regression", i, f, out.MinUsefulness)
			}
			out2 := ResolveDemotionThresholds(DemotionConfig{StaleDays: math.MinInt32, MinUsefulness: f})
			if out2.StaleDays != DefaultDemotionStaleDays {
				t.Fatalf("MinInt32 StaleDays resolved to %d, want default", out2.StaleDays)
			}
		}()
	}
	// NaN in the generator stream too (randConfig case 3 emits raw bits):
	// must not panic, and the OUTPUT must never be NaN for ANY input
	// (F4 fix: the image is finite-only).
	r := rand.New(rand.NewSource(tierPropSeed + 2))
	for i := 0; i < 2000; i++ {
		cfg := randConfig(r)
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					t.Fatalf("case %d panicked: %v (cfg=%+v)", i, rec, cfg)
				}
			}()
			out := ResolveDemotionThresholds(cfg)
			if math.IsNaN(out.MinUsefulness) || math.IsInf(out.MinUsefulness, 0) {
				t.Fatalf("case %d: input %v produced non-finite output %v — F4 regression", i, cfg.MinUsefulness, out.MinUsefulness)
			}
			again := ResolveDemotionThresholds(DemotionConfig(out))
			if again != out {
				t.Fatalf("case %d: not idempotent on image: %+v -> %+v", i, out, again)
			}
		}()
	}
}

// FIXED F4 (property counterexample 2026-11-06, header comment): NaN and
// ±Inf no longer pass through ResolveDemotionThresholds verbatim — the
// NaN-safe inverted range check routes them to the documented default,
// so the demote.go usefulness gate can never be silently disarmed.
func TestResolveDemotion_NonFiniteResolvesToDefault(t *testing.T) {
	for _, f := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		out := ResolveDemotionThresholds(DemotionConfig{StaleDays: 0, MinUsefulness: f})
		if out.MinUsefulness != DefaultDemotionMinUsefulness {
			t.Fatalf("%v resolved to %v, want default %v — F4 regression", f, out.MinUsefulness, DefaultDemotionMinUsefulness)
		}
		if out.StaleDays != DefaultDemotionStaleDays {
			t.Fatalf("%v: StaleDays %d, want default %d", f, out.StaleDays, DefaultDemotionStaleDays)
		}
	}
	// The image is finite-only now: idempotence holds with plain equality.
	out := ResolveDemotionThresholds(DemotionConfig{StaleDays: 7, MinUsefulness: math.NaN()})
	if again := ResolveDemotionThresholds(DemotionConfig(out)); again != out {
		t.Fatalf("not idempotent on image: %+v -> %+v", out, again)
	}
}
