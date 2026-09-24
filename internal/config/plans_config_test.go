package config

import (
	"encoding/json"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// TestDefaultConfig_PlansParallelPhasesDefault verifies the serial default
// (phase-frontier-parallel Contract C): absent key means ParallelPhases ==
// false, preserving strict serial phases byte-for-byte.
func TestDefaultConfig_PlansParallelPhasesDefault(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Plans.ParallelPhases {
		t.Error("plans.parallel_phases default = true, want false (serial default)")
	}
	if err := cfg.Plans.Validate(); err != nil {
		t.Errorf("default plans config should validate: %v", err)
	}
}

// TestPlansConfig_ParallelPhasesValidate verifies the flag does not break
// Validate in either state — a bool needs no validation, but the combined
// config (mode + flag) must still pass through unchanged.
func TestPlansConfig_ParallelPhasesValidate(t *testing.T) {
	tests := []struct {
		name string
		cfg  PlansConfig
	}{
		{
			name: "false validates",
			cfg:  PlansConfig{Mode: "always", ParallelPhases: false},
		},
		{
			name: "true validates",
			cfg:  PlansConfig{Mode: "always", ParallelPhases: true},
		},
		{
			name: "true with empty mode validates",
			cfg:  PlansConfig{ParallelPhases: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

// TestPlansConfig_ParallelPhasesTOMLKey pins the exact TOML key
// plans.parallel_phases: absent key decodes false, key present decodes true,
// and a round trip preserves the value. The wrapper mirrors the real load
// path (Config.Plans under the `plans` TOML table).
func TestPlansConfig_ParallelPhasesTOMLKey(t *testing.T) {
	type doc struct {
		Plans PlansConfig `toml:"plans"`
	}

	// Absent key -> false.
	var absent doc
	if err := toml.Unmarshal([]byte("[plans]\nmode = \"always\"\n"), &absent); err != nil {
		t.Fatalf("toml.Unmarshal absent: %v", err)
	}
	if absent.Plans.ParallelPhases {
		t.Error("absent parallel_phases key decoded true, want false")
	}

	// Key present -> true.
	var present doc
	if err := toml.Unmarshal([]byte("[plans]\nmode = \"always\"\nparallel_phases = true\n"), &present); err != nil {
		t.Fatalf("toml.Unmarshal present: %v", err)
	}
	if !present.Plans.ParallelPhases {
		t.Error("parallel_phases = true decoded false, want true")
	}

	// Round trip preserves the value and the key name.
	data, err := toml.Marshal(present)
	if err != nil {
		t.Fatalf("toml.Marshal: %v", err)
	}
	var out doc
	if err := toml.Unmarshal(data, &out); err != nil {
		t.Fatalf("toml.Unmarshal round trip: %v", err)
	}
	if !out.Plans.ParallelPhases {
		t.Errorf("round trip lost parallel_phases; marshaled:\n%s", data)
	}
}

// TestPlansConfig_PlanCompilerEnabledDefault verifies the default: the
// plan-compiler pipeline is opt-in (plan-compiler Contract C). Absent key
// means PlanCompilerEnabled == false and the legacy JSON spec_plan path
// stays byte-identical.
func TestPlansConfig_PlanCompilerEnabledDefault(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Plans.PlanCompilerEnabled {
		t.Error("plans.plan_compiler_enabled default = true, want false (opt-in)")
	}
	if err := cfg.Plans.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// TestPlansConfig_PlanCompilerEnabledTOMLKey pins the exact TOML key
// plans.plan_compiler_enabled: absent key decodes false, key present decodes
// true, and a round trip preserves the value.
func TestPlansConfig_PlanCompilerEnabledTOMLKey(t *testing.T) {
	type doc struct {
		Plans PlansConfig `toml:"plans"`
	}

	// Absent key -> false.
	var absent doc
	if err := toml.Unmarshal([]byte("[plans]\nmode = \"always\"\n"), &absent); err != nil {
		t.Fatalf("toml.Unmarshal absent: %v", err)
	}
	if absent.Plans.PlanCompilerEnabled {
		t.Error("absent plan_compiler_enabled key decoded true, want false")
	}

	// Key present -> true.
	var present doc
	if err := toml.Unmarshal([]byte("[plans]\nmode = \"always\"\nplan_compiler_enabled = true\n"), &present); err != nil {
		t.Fatalf("toml.Unmarshal present: %v", err)
	}
	if !present.Plans.PlanCompilerEnabled {
		t.Error("plan_compiler_enabled = true decoded false, want true")
	}

	// Round trip preserves the value and the key name.
	data, err := toml.Marshal(present)
	if err != nil {
		t.Fatalf("toml.Marshal: %v", err)
	}
	var out doc
	if err := toml.Unmarshal(data, &out); err != nil {
		t.Fatalf("toml.Unmarshal round trip: %v", err)
	}
	if !out.Plans.PlanCompilerEnabled {
		t.Errorf("round trip lost plan_compiler_enabled; marshaled:\n%s", data)
	}
}

// TestPlansConfig_PlanCompilerEnabledJSONKey pins the exact JSON key
// plans.plan_compiler_enabled (meept.json5 load path).
func TestPlansConfig_PlanCompilerEnabledJSONKey(t *testing.T) {
	// Absent key -> false.
	var absent struct {
		Plans PlansConfig `json:"plans"`
	}
	if err := json.Unmarshal([]byte(`{"plans":{"mode":"always"}}`), &absent); err != nil {
		t.Fatalf("json.Unmarshal absent: %v", err)
	}
	if absent.Plans.PlanCompilerEnabled {
		t.Error("absent plan_compiler_enabled key decoded true, want false")
	}

	// Key present -> true.
	var present struct {
		Plans PlansConfig `json:"plans"`
	}
	if err := json.Unmarshal([]byte(`{"plans":{"mode":"always","plan_compiler_enabled":true}}`), &present); err != nil {
		t.Fatalf("json.Unmarshal present: %v", err)
	}
	if !present.Plans.PlanCompilerEnabled {
		t.Error("plan_compiler_enabled = true decoded false, want true")
	}
}

// TestPlansConfig_ParallelPhasesJSONKey pins the exact JSON key
// plans.parallel_phases (meept.json5 load path).
func TestPlansConfig_ParallelPhasesJSONKey(t *testing.T) {
	// Absent key -> false.
	var absent struct {
		Plans PlansConfig `json:"plans"`
	}
	if err := json.Unmarshal([]byte(`{"plans":{"mode":"always"}}`), &absent); err != nil {
		t.Fatalf("json.Unmarshal absent: %v", err)
	}
	if absent.Plans.ParallelPhases {
		t.Error("absent parallel_phases key decoded true, want false")
	}

	// Key present -> true.
	var present struct {
		Plans PlansConfig `json:"plans"`
	}
	if err := json.Unmarshal([]byte(`{"plans":{"mode":"always","parallel_phases":true}}`), &present); err != nil {
		t.Fatalf("json.Unmarshal present: %v", err)
	}
	if !present.Plans.ParallelPhases {
		t.Error("parallel_phases = true decoded false, want true")
	}
}

// TestPlansConfig_TieredIterationDefaults pins the tiered-iteration leaf 02
// knobs: self_seal_enabled defaults FALSE (ships dark), and
// complex_max_critique_rounds clamps to 2 at the load boundary — a
// non-positive value means "default", never "unbounded" or "zero rounds".
func TestPlansConfig_TieredIterationDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Plans.SelfSealEnabled {
		t.Error("plans.self_seal_enabled default = true, want false (dark launch)")
	}
	NormalizePlansDefaults(&cfg.Plans)
	if cfg.Plans.ComplexMaxCritiqueRounds != 2 {
		t.Errorf("complex_max_critique_rounds default = %d, want 2", cfg.Plans.ComplexMaxCritiqueRounds)
	}

	// Explicit values survive the clamp; non-positive values clamp to 2.
	explicit := PlansConfig{ComplexMaxCritiqueRounds: 5}
	NormalizePlansDefaults(&explicit)
	if explicit.ComplexMaxCritiqueRounds != 5 {
		t.Errorf("explicit rounds = %d after clamp, want 5", explicit.ComplexMaxCritiqueRounds)
	}
	zero := PlansConfig{}
	NormalizePlansDefaults(&zero)
	if zero.ComplexMaxCritiqueRounds != 2 {
		t.Errorf("zero rounds = %d after clamp, want 2", zero.ComplexMaxCritiqueRounds)
	}
	negative := PlansConfig{ComplexMaxCritiqueRounds: -3}
	NormalizePlansDefaults(&negative)
	if negative.ComplexMaxCritiqueRounds != 2 {
		t.Errorf("negative rounds = %d after clamp, want 2", negative.ComplexMaxCritiqueRounds)
	}
	if err := negative.Validate(); err != nil {
		t.Errorf("Validate() after clamp = %v, want nil", err)
	}
}
