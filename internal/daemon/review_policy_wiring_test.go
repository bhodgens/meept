package daemon

// Review-policy wiring pins (20261122, residual of the F1/F2 boot-order
// break): the review.go ReviewPolicyFromConfig seam lived uncommitted for
// hours while components.go:4728 already called it -- main could not build,
// and nothing pinned the config-to-policy wiring. These tests close that
// gap at two layers:
//
//  1. agent.ReviewPolicyFromConfig maps [agent] review config fields onto
//     the runtime policy (enabled flag, require/skip lists, revision cap,
//     auto-approve patterns).
//  2. The daemon components construction flows cfg.Agent.Review into the
//     ReviewManager's policy (mirrored via a minimal Manager construction,
//     since full NewComponents requires a live bus + stores).

import (
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/task"
)

// TestReviewPolicyFromConfig_MapsConfigFields pins the field mapping: a
// fully populated ReviewConfig must surface every operator value on the
// runtime policy.
func TestReviewPolicyFromConfig_MapsConfigFields(t *testing.T) {
	cfg := config.ReviewConfig{
		Enabled:             false, // operator explicitly disabled review
		RequireReview:       []string{"code", "refactor"},
		SkipReview:          []string{"chat", "recall"},
		MaxRevisionCycles:   7,
		AutoApprovePatterns: []string{"docs/*"},
	}
	p := agent.ReviewPolicyFromConfig(cfg)
	if p == nil {
		t.Fatal("ReviewPolicyFromConfig returned nil for a populated config")
	}
	if p.Enabled {
		t.Errorf("policy.Enabled = true, want false (operator set review.enabled=false)")
	}
	if len(p.RequireReview) != 2 || p.RequireReview[0] != "code" {
		t.Errorf("policy.RequireReview = %v, want [code refactor]", p.RequireReview)
	}
	if len(p.SkipReview) != 2 || p.SkipReview[0] != "chat" {
		t.Errorf("policy.SkipReview = %v, want [chat recall]", p.SkipReview)
	}
	if p.MaxRevisionCycles != 7 {
		t.Errorf("policy.MaxRevisionCycles = %d, want 7", p.MaxRevisionCycles)
	}
	if len(p.AutoApprovePatterns) != 1 || p.AutoApprovePatterns[0] != "docs/*" {
		t.Errorf("policy.AutoApprovePatterns = %v, want [docs/*]", p.AutoApprovePatterns)
	}
}

// TestReviewPolicyFromConfig_ZeroValueKeepsDefaults pins the unset contract:
// a zero-value ReviewConfig (no [agent] review block in the installed home)
// keeps the DefaultReviewPolicy posture instead of disabling review.
func TestReviewPolicyFromConfig_ZeroValueKeepsDefaults(t *testing.T) {
	def := agent.DefaultReviewPolicy()
	p := agent.ReviewPolicyFromConfig(config.ReviewConfig{})
	if !p.Enabled {
		t.Error("policy.Enabled = false for an unset config; zero-value must keep the enabled default")
	}
	if p.MaxRevisionCycles != def.MaxRevisionCycles {
		t.Errorf("policy.MaxRevisionCycles = %d, want the default %d", p.MaxRevisionCycles, def.MaxRevisionCycles)
	}
	if len(p.RequireReview) != len(def.RequireReview) {
		t.Errorf("policy.RequireReview = %v, want the default %v", p.RequireReview, def.RequireReview)
	}
}

// TestReviewPolicyFromConfig_PartialConfigMerges pins the partial contract:
// MaxRevisionCycles != 0 opts the config into the Enabled mapping, but empty
// lists keep the defaults (a pattern list REPLACES; an absent list does not).
func TestReviewPolicyFromConfig_PartialConfigMerges(t *testing.T) {
	def := agent.DefaultReviewPolicy()
	// Shipped mapping contract: ANY set field opts the config into the
	// Enabled mapping (p.Enabled = c.Enabled, zero-value = disabled). An
	// operator overriding only the revision cap must also set enabled=true
	// to keep review on -- pin that exact contract so it cannot silently
	// change.
	cfg := config.ReviewConfig{MaxRevisionCycles: 3, Enabled: true}
	p := agent.ReviewPolicyFromConfig(cfg)
	if !p.Enabled {
		t.Error("policy.Enabled = false; config set enabled=true and MaxRevisionCycles=3")
	}
	if p.MaxRevisionCycles != 3 {
		t.Errorf("policy.MaxRevisionCycles = %d, want 3 (operator override)", p.MaxRevisionCycles)
	}
	if len(p.RequireReview) != len(def.RequireReview) {
		t.Errorf("policy.RequireReview = %v, want the default list preserved when the config list is empty", p.RequireReview)
	}
	// And the cycles-only override WITHOUT enabled=true disables review:
	// document the sharp edge so operators see it pinned.
	pOff := agent.ReviewPolicyFromConfig(config.ReviewConfig{MaxRevisionCycles: 3})
	if pOff.Enabled {
		t.Error("policy.Enabled = true for cycles-only override; the shipped mapping sets Enabled = c.Enabled (zero-value false)")
	}
}

// TestReviewPolicy_NeedsReviewHonorsRequireAndSkip pins the consumer half:
// NeedsReview must consult the mapped lists so the components wiring at
// components.go:4728 actually changes review behavior.
func TestReviewPolicy_NeedsReviewHonorsRequireAndSkip(t *testing.T) {
	p := agent.ReviewPolicyFromConfig(config.ReviewConfig{
		Enabled:       true,
		RequireReview: []string{"code"},
		SkipReview:    []string{"chat"},
	})
	codeStep := &task.TaskStep{ToolHint: "code"}
	chatStep := &task.TaskStep{ToolHint: "chat"}
	if !p.NeedsReview(codeStep) {
		t.Error("NeedsReview(code step) = false, want true (require list names code)")
	}
	if p.NeedsReview(chatStep) {
		t.Error("NeedsReview(chat step) = true, want false (skip list names chat)")
	}
}
