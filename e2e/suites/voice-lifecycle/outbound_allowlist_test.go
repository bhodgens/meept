//go:build e2e

// Issue #165 (F-03) slice 3 hermetic gate: an outbound dial to a number
// OUTSIDE the configured allowlist must be refused BEFORE any Telnyx HTTP
// POST. The fake Telnyx call-control server records every request path;
// the assertion is that zero "/calls" paths were recorded after the
// refusal. This exercises the real Dialer through its narrow seams (no
// daemon, no sockets, no vendor), so it is deterministic and fast.
package voicelifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/voice"
	"github.com/caimlas/meept/internal/voice/telnyx"
	"github.com/caimlas/meept/pkg/security"
)

// countingDialController is a DialController that records every DialOut
// call. DialOut would be the Telnyx POST /calls in production; here it is
// a pure counter so the test can assert zero egress without any network.
type countingDialController struct {
	dials int
}

func (c *countingDialController) DialOut(_ context.Context, _, _, _ string, _ int) (string, error) {
	c.dials++
	return "cc-e2e-out", nil
}
func (c *countingDialController) AnswerCall(_ context.Context, _ string) error { return nil }
func (c *countingDialController) StartStreaming(_ context.Context, _, _ string, _ telnyx.Codec) error {
	return nil
}
func (c *countingDialController) HangupCall(_ context.Context, _ string) error { return nil }

// captureAudit records audit writes. A refused dial must leave no audit
// row - the allowlist gate fires before the audit seam.
type captureAudit struct {
	rows int
}

func (a *captureAudit) WriteAudit(_ context.Context, _ map[string]any) error {
	a.rows++
	return nil
}

// TestOutboundDialOutsideAllowlistPostsNothing proves the issue #165
// acceptance criterion "out-of-allowlist number never reaches the Telnyx
// POST" for the VOICE leg. The allowlist is non-empty and the target is
// not in it; Dial must return ErrNumberNotAllowed with zero DialOut calls
// and zero audit rows. The public URL and connection id are deliberately
// VALID, so the ONLY reason for refusal is the allowlist - if the gate
// were moved after those checks or after the POST, the test would fail.
func TestOutboundDialOutsideAllowlistPostsNothing(t *testing.T) {
	cfg := voice.DialConfig{
		Enabled:         true,
		AllowedNumbers:  []string{"+15550000001"},
		PublicStreamURL: "wss://media.example/stream",
		ConnectionID:    "conn-e2e",
		FromNumber:      "+15550000009",
		TimeoutSecs:     30,
	}
	controller := &countingDialController{}
	audit := &captureAudit{}
	ledger := effects.NewMemoryLedger()
	d := voice.NewDialer(cfg, controller, nil, voice.NewRegistry(4), ledger, audit, nil)

	_, err := d.Dial(context.Background(), voice.DialRequest{
		EmployeeID: "emp-e2e",
		Target:     "+15550000999", // NOT in the allowlist
		RequestID:  "req-outside-allowlist-e2e",
	})
	if !errors.Is(err, voice.ErrNumberNotAllowed) {
		t.Fatalf("out-of-allowlist dial error = %v, want ErrNumberNotAllowed", err)
	}

	// The refusal must be side-effect-free: no HTTP POST (DialOut), no
	// audit row, and no persisted effect reservation.
	if got := controller.dials; got != 0 {
		t.Errorf("DialOut called %d time(s) for an out-of-allowlist target; want 0 (no Telnyx POST)", got)
	}
	if got := audit.rows; got != 0 {
		t.Errorf("audit rows written on refusal = %d, want 0 (gate must fire before the audit seam)", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	key := effects.EffectKey("voice.dial_out", "emp-e2e", "+15550000999", "req-outside-allowlist-e2e")
	if rec, lerr := ledger.Get(ctx, key); lerr == nil && rec != nil {
		t.Errorf("effect reservation %+v exists for a refused dial; want none", rec)
	}
}

// TestOutOfAllowlistContactRequiresConfirmation pins issue #165 slice 4:
// the dedicated out-of-allowlist contact action carries HIGH risk and an
// unconditional confirmation requirement, while the static `contact` row
// stays MEDIUM/confirmation-free (changing it would gate every contact
// call). The contact runner reports the dedicated action at call time;
// this suite pins the checker's verdict for that report.
func TestOutOfAllowlistContactRequiresConfirmation(t *testing.T) {
	pc := security.NewPermissionChecker(security.Config{})
	got := pc.CheckPermission(security.ContactOutOfAllowlistAction, map[string]string{
		"agent_id": "emp-e2e",
	})
	if got.Allowed {
		t.Fatalf("out-of-allowlist contact was allowed without confirmation: %+v", got)
	}
	if !got.NeedsConfirm {
		t.Errorf("out-of-allowlist contact NeedsConfirm = false, want true")
	}
	if got.RuleSource != "confirmation_gate" {
		t.Errorf("out-of-allowlist contact RuleSource = %q, want confirmation_gate", got.RuleSource)
	}
	if got.EffectiveRisk != security.RiskHigh {
		t.Errorf("out-of-allowlist contact EffectiveRisk = %v, want RiskHigh", got.EffectiveRisk)
	}

	rule, ok := security.OutOfAllowlistRules[security.ContactOutOfAllowlistAction]
	if !ok {
		t.Fatal("OutOfAllowlistRules has no contact_out_of_allowlist row")
	}
	if rule.Action != security.ContactOutOfAllowlistAction || rule.RiskLevel != security.RiskHigh || !rule.RequiresConfirmation {
		t.Errorf("out-of-allowlist rule = %+v, want {action contact_out_of_allowlist, RiskHigh, RequiresConfirmation:true}", rule)
	}
	baseline := security.BuiltinRules["contact"]
	if baseline.RiskLevel != security.RiskMedium || baseline.RequiresConfirmation {
		t.Errorf("static contact row = %+v, want {RiskMedium, RequiresConfirmation:false}", baseline)
	}
}
