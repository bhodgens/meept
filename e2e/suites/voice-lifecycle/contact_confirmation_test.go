//go:build e2e

// voice-outbound-confirmation-02 (#165 follow-up): an out-of-allowlist
// contact target must be classified as requiring the dedicated RiskHigh
// confirmation action; an in-allowlist target must not. This pins the
// compiled allowlist predicate and the action wiring through the REAL
// builtin contact tool's permission shape, with no daemon or Telnyx POST.
package voicelifecycle

import (
	"testing"

	"github.com/caimlas/meept/internal/daemon"
	"github.com/caimlas/meept/internal/voice"
	"github.com/caimlas/meept/pkg/security"
)

func TestContactOutOfAllowlistActionClassification(t *testing.T) {
	// The dedicated action must be RiskHigh with a confirmation verdict,
	// while the ordinary contact action stays MEDIUM/no-confirm.
	outRule, ok := security.OutOfAllowlistRules[security.ContactOutOfAllowlistAction]
	if !ok {
		t.Fatal("contact_out_of_allowlist rule is not registered")
	}
	if outRule.RiskLevel != security.RiskHigh || !outRule.RequiresConfirmation {
		t.Fatalf("out-of-allowlist rule = risk %v confirm %v; want RiskHigh/true", outRule.RiskLevel, outRule.RequiresConfirmation)
	}
	contactRule := security.BuiltinRules["contact"]
	if contactRule.RiskLevel == security.RiskHigh || contactRule.RequiresConfirmation {
		t.Fatalf("static contact row must stay low-risk; got %+v", contactRule)
	}

	// The wiring's allowlist predicate must use the same normalization as
	// the voice dial gate: an E.164 target matches despite formatting.
	w := &daemon.ContactChainWiring{}
	w.SetAllowedNumbersForTest([]string{"+15550000001"})
	if !w.AllowedTargetForTest("15550000001") {
		t.Error("normalized in-allowlist target must be allowed")
	}
	w.SetAllowedNumbersForTest([]string{"+15550000001"})
	if w.AllowedTargetForTest("+15550000002") {
		t.Error("out-of-allowlist target must be refused")
	}
	if voice.NormalizeE164("+15550000001") == "" {
		t.Error("NormalizeE164 must accept a valid E.164 target")
	}
}
