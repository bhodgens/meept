package config

import (
	"testing"
	"time"
)

// TestPlansConfig_RequireApprovalDefaultsTrue pins leaf 10 (20261121
// interactive-usability, master C-6): plans.approval.require_approval
// defaults TRUE — an install with no [plans] block (or no approval block)
// gets the human gate; an explicit `require_approval: false` in the user's
// config still wins (the sentinel decode is exact).
func TestPlansConfig_RequireApprovalDefaultsTrue(t *testing.T) {
	// DefaultConfig must carry the true default.
	cfg := DefaultConfig()
	if !cfg.Plans.Approval.RequireApproval {
		t.Error("DefaultConfig plans.approval.require_approval = false, want true")
	}

	// Normalize must preserve it.
	NormalizePlansDefaults(&cfg.Plans)
	if !cfg.Plans.Approval.RequireApproval {
		t.Error("NormalizePlansDefaults flipped require_approval off; the default must survive normalization")
	}

	// A config file that never mentions require_approval loads with the
	// gate ON (full Config roundtrip through the JSON5 loader).
	var loaded Config
	if err := LoadJSON5Bytes([]byte(`{"plans":{}}`), &loaded); err != nil {
		t.Fatalf("LoadJSON5Bytes plans{}: %v", err)
	}
	NormalizePlansDefaults(&loaded.Plans)
	if !loaded.Plans.Approval.RequireApproval {
		t.Error("config with bare [plans] block loaded require_approval=false, want true")
	}

	// A config with NO plans block at all likewise loads gated.
	var bare Config
	if err := LoadJSON5Bytes([]byte(`{}`), &bare); err != nil {
		t.Fatalf("LoadJSON5Bytes {}: %v", err)
	}
	NormalizePlansDefaults(&bare.Plans)
	if !bare.Plans.Approval.RequireApproval {
		t.Error("config with no [plans] block loaded require_approval=false, want true")
	}

	// Explicit false survives the default: decode through the raw field
	// (json.Unmarshal leaves an absent bool false and a present false
	// identical, so the sentinel is what distinguishes them).
	var explicit Config
	if err := LoadJSON5Bytes([]byte(`{"plans":{"approval":{"require_approval":false}}}`), &explicit); err != nil {
		t.Fatalf("LoadJSON5Bytes explicit false: %v", err)
	}
	if !explicit.Plans.requireApprovalSet {
		t.Error("explicit require_approval=false did not arm the sentinel; explicit false would be clobbered by the default")
	}
	NormalizePlansDefaults(&explicit.Plans)
	if explicit.Plans.Approval.RequireApproval {
		t.Error("explicit require_approval=false was flipped to true; explicit false must win")
	}

	// An explicit true also survives.
	var explicitTrue Config
	if err := LoadJSON5Bytes([]byte(`{"plans":{"approval":{"require_approval":true}}}`), &explicitTrue); err != nil {
		t.Fatalf("LoadJSON5Bytes explicit true: %v", err)
	}
	NormalizePlansDefaults(&explicitTrue.Plans)
	if !explicitTrue.Plans.Approval.RequireApproval {
		t.Error("explicit require_approval=true was flipped to false")
	}
}

// TestPlansConfig_RequireApprovalSentinelAbsentKey pins the sentinel
// mechanics directly: the unexported requireApprovalSet field arms ONLY
// through the loader's presence probe, so Normalize can tell "absent" from
// "false" — a bare struct literal (no decode) behaves like absent.
func TestPlansConfig_RequireApprovalSentinelAbsentKey(t *testing.T) {
	// Struct literal (no decode): sentinel down; Normalize flips on.
	literal := PlansConfig{Approval: PlansApprovalConfig{RequireApproval: false}}
	NormalizePlansDefaults(&literal)
	if !literal.Approval.RequireApproval {
		t.Error("constructed-in-code require_approval=false was honored; only a config FILE may opt out")
	}

	// Explicit false decoded through the loader: sentinel arms; Normalize
	// leaves the value alone.
	var explicit Config
	if err := LoadJSON5Bytes([]byte(`{"plans":{"approval":{"require_approval":false}}}`), &explicit); err != nil {
		t.Fatalf("LoadJSON5Bytes explicit: %v", err)
	}
	if !explicit.Plans.requireApprovalSet {
		t.Error("sentinel did not arm on explicit require_approval=false")
	}
	NormalizePlansDefaults(&explicit.Plans)
	if explicit.Plans.Approval.RequireApproval {
		t.Error("NormalizePlansDefaults clobbered explicit require_approval=false")
	}

	// Explicit true decoded through the loader: sentinel arms, value kept.
	var explicitTrue Config
	if err := LoadJSON5Bytes([]byte(`{"plans":{"approval":{"require_approval":true}}}`), &explicitTrue); err != nil {
		t.Fatalf("LoadJSON5Bytes explicit true: %v", err)
	}
	NormalizePlansDefaults(&explicitTrue.Plans)
	if !explicitTrue.Plans.Approval.RequireApproval {
		t.Error("explicit require_approval=true lost through the loader")
	}
}

// TestPlansConfig_StalledExecutingSweepDefaults pins the new
// plans.stalled_executing_sweep knob (leaf 10 task 5): a duration field
// defaulting to 6h at DefaultConfig, and 0 in a struct literal means
// "disabled" — never defaulted by Normalize (the daemon wiring decides).
func TestPlansConfig_StalledExecutingSweepDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Plans.StalledExecutingSweep != 6*60*60*time.Second {
		t.Errorf("stalled_executing_sweep default = %v, want 6h", cfg.Plans.StalledExecutingSweep)
	}

	// Explicit "0" in a config file stays 0 (disabled) — the loader never
	// invents a window over an explicit zero.
	var loaded Config
	if err := LoadJSON5Bytes([]byte(`{"plans":{"stalled_executing_sweep":"0s"}}`), &loaded); err != nil {
		t.Fatalf("LoadJSON5Bytes zero sweep: %v", err)
	}
	if loaded.Plans.StalledExecutingSweep != 0 {
		t.Errorf("explicit stalled_executing_sweep=0 loaded as %v, want 0 (disabled)", loaded.Plans.StalledExecutingSweep)
	}

	// A quoted duration string parses through the loader's duration
	// preprocessing.
	var dur Config
	if err := LoadJSON5Bytes([]byte(`{"plans":{"stalled_executing_sweep":"30m"}}`), &dur); err != nil {
		t.Fatalf("LoadJSON5Bytes 30m sweep: %v", err)
	}
	if dur.Plans.StalledExecutingSweep != 30*time.Minute {
		t.Errorf("stalled_executing_sweep \"30m\" loaded as %v, want 30m", dur.Plans.StalledExecutingSweep)
	}
}
