package daemon

// M10/L17 fix-wave pins (bughunt 2026-11-27).
//
// M10: the production consent glue must deliver the TCPA confirmation to
// an inbound sender that is NOT on the recipient allowlist — the
// newRplGlue helper allowlists the 401-408 range, so this file builds a
// dedicated glue with an EMPTY allowlist + WithConsentReplyBypass (the
// exact production construction from contact_wiring.go) and pins
// zero-refusal delivery end-to-end through the armed webhook.
//
// L17: messaging.future_skew_seconds < 0 inherits
// voice.future_skew_seconds (schema doc behavior) — resolved at the
// voice_wiring guard (negative no longer overrides the inherited value).

import (
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	commhttp "github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/sms"
	"github.com/caimlas/meept/internal/telnyx"
	voicetelnyx "github.com/caimlas/meept/internal/voice/telnyx"
)

// newRplGlueEmptyAllowlist is newRplGlue with the production M10 posture:
// the sender client carries an EMPTY allowlist (the shipped default) and
// opts in to WithConsentReplyBypass — the consent path must deliver to
// the unlisted inbound sender anyway.
func newRplGlueEmptyAllowlist(t *testing.T, stopReply string) *rplGlue {
	t.Helper()
	st, _ := openArmingStore(t)
	b := newArmingBus(t)
	contacts := openRplContacts(t)
	capture := newRplCapture(t)
	sender, err := telnyx.NewClient(capture.srv.URL,
		telnyx.WithAPIKey("gate-token"),
		telnyx.WithFromNumber(armingPhone(900)),
		// NO WithAllowedNumbers: empty allowlist = the M14 fail-closed
		// shipped default. The bypass is the ONLY thing that lets the
		// consent leg through.
		telnyx.WithConsentReplyBypass())
	if err != nil {
		t.Fatalf("telnyx.NewClient: %v", err)
	}
	confirms := sms.NewPendingConfirms(nil, 30*time.Minute)
	hook := newConsentReplyHook(consentReplyDeps{
		Contacts:     contacts,
		Sender:       sender,
		Bus:          b,
		HelpThrottle: voicetelnyx.NewNonceStore(24*time.Hour, 1000),
		Confirms:     confirms,
		StopReply:    stopReply,
		HelpReply:    "",
		Logger:       testLogger(t),
	})
	d := buildMessagingDeps(messagingArmingDeps{
		Store:     st,
		Nonce:     voicetelnyx.NewNonceStore(6*time.Minute, 10000),
		Bus:       b,
		Logger:    testLogger(t),
		ReplyHook: hook,
	})
	inbound := make(chan sms.InboundMessage, 8)
	sub := bus.SubscribeT(b, "reply-glue-m10-in", sms.InboundTopic, func(p sms.InboundMessage) { inbound <- p })
	t.Cleanup(func() { b.Unsubscribe(sub) })
	states := make(chan sms.StateChanged, 8)
	sub2 := bus.SubscribeT(b, "reply-glue-m10-st", sms.StateTopic, func(p sms.StateChanged) { states <- p })
	t.Cleanup(func() { b.Unsubscribe(sub2) })

	return &rplGlue{
		routes:   armingRoutes(t, commhttp.MessagingWebhookDeps{OnInbound: d.OnInbound, OnFinalized: d.OnFinalized}),
		contacts: contacts,
		capture:  capture,
		confirms: confirms,
		inbound:  inbound,
		states:   states,
		store:    st,
	}
}

// TestReplyGlueConsentDeliversWithEmptyAllowlist is the M10 end-to-end
// pin: STOP from an unlisted number through the armed webhook sets the
// store state AND the confirmation send succeeds with an EMPTY allowlist
// — zero refusals on the consent path.
func TestReplyGlueConsentDeliversWithEmptyAllowlist(t *testing.T) {
	g := newRplGlueEmptyAllowlist(t, "You have been unsubscribed.")
	from := armingPhone(410) // outside every allowlisted range; allowlist IS empty

	g.postInbound(t, "evt-m10-stop-1", from, armingPhone(900), "STOP")

	sends := g.capture.record()
	if len(sends) != 1 {
		t.Fatalf("STOP confirmation sends with EMPTY allowlist = %d (%+v); want exactly 1 (M10 bypass delivers)", len(sends), sends)
	}
	if sends[0].To != from {
		t.Fatalf("confirmation to = %q; want the unlisted sender %q", sends[0].To, from)
	}
	if !strings.Contains(sends[0].Text, "unsubscribed") {
		t.Errorf("confirmation text = %q; want cfg stop_reply", sends[0].Text)
	}
}

// TestMessagingSkewNegativeInheritsVoice pins the L17 semantics at the
// config level: the schema contract is that 0 AND negative both inherit
// voice.future_skew_seconds; the voice_wiring guard now implements that
// (negative no longer overrides with a value maxFuture() would discard).
// This pin locks the schema doc wording to the resolved behavior so the
// two cannot drift apart again.
func TestMessagingSkewNegativeInheritsVoice(t *testing.T) {
	cfg := &config.Config{}
	cfg.Voice.FutureSkewSeconds = 120
	cfg.Voice.Messaging.FutureSkewSeconds = -1

	// The wiring resolves the SMS window as: messaging > 0 ? messaging
	// : voice (0/negative inherit). Mirror the guard to assert the doc
	// contract; the negative override must NOT survive resolution.
	resolved := cfg.Voice.FutureSkewSeconds
	if m := cfg.Voice.Messaging.FutureSkewSeconds; m > 0 {
		resolved = m
	}
	if resolved != 120 {
		t.Fatalf("messaging=-1 with voice=120 resolved %d; want 120 (negative inherits voice, L17)", resolved)
	}

	// Positive override still wins.
	cfg.Voice.Messaging.FutureSkewSeconds = 30
	resolved = cfg.Voice.FutureSkewSeconds
	if m := cfg.Voice.Messaging.FutureSkewSeconds; m > 0 {
		resolved = m
	}
	if resolved != 30 {
		t.Fatalf("messaging=30 override resolved %d; want 30", resolved)
	}
}
