package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/contact"
	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/sms"
	"github.com/caimlas/meept/internal/telnyx"
	"github.com/caimlas/meept/internal/tools/builtin"
	"github.com/caimlas/meept/internal/voice"
	voicetelnyx "github.com/caimlas/meept/internal/voice/telnyx"
)

// consentHelpWindow is the per-contact HELP auto-reply throttle window
// (master §7: one HELP reply per contact per 24h — NonceStore ttl reuse,
// CheckFresh == send). consentHelpMax bounds the in-memory window.
const (
	consentHelpWindow = 24 * time.Hour
	consentHelpMax    = 10000
	// consentPendingTTL bounds a pending #132 confirmation: Resolve
	// drops older entries (lazy expiry in sms.PendingConfirms).
	consentPendingTTL = 30 * time.Minute
)

// ContactChainWiring assembles the employee contact chain (leaf 02 T4):
// the contact Store, the ChainRunner over the four channel adapters, and
// the string-shaped seam the contact tool consumes. Built by
// BuildContactChain (components.go, voice Start closure) once the voice
// wiring exists; a nil result (contact disabled, store unopenable)
// leaves the contact tool unregistered - the feature is opt-in, never a
// boot failure.
type ContactChainWiring struct {
	Store  *contact.SQLiteStore
	Runner *contact.ChainRunner

	// SMSConfirms (leaf 04/02): the pending-confirmation registry the
	// armed consent hook consumes on YES. #132's dispatch tree
	// REGISTERs here (Register(ref, to, text)) before asking a contact
	// to confirm; the inbound YES reply resolves + acks it on
	// sms.StateTopic. Nil when the sms channel never armed (messaging
	// disabled / client failure) — dispatch treats nil as "no ack lane".
	SMSConfirms *sms.PendingConfirms
}

// Run implements builtin.ContactChainRunner (the string-shaped seam:
// resolve -> chain -> rendered lowercase report, one line per attempt).
func (w *ContactChainWiring) Run(person, message string, channels []string) string {
	if w == nil || w.Store == nil || w.Runner == nil {
		return "contact: contact chain is not wired (contact feature disabled)"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	rec, err := w.Store.Resolve(ctx, person)
	if err != nil {
		return fmt.Sprintf("contact: unknown contact %q (no record by id, phone, or signal uuid)", person)
	}
	override := normalizeChannelOverride(channels)
	report := w.Runner.Run(ctx, rec, message, override)
	return renderChainReport(person, report)
}

// normalizeChannelOverride maps the tool's string list onto C1
// channels, dropping unknown names (fail-soft: an unknown channel name
// narrows the override rather than failing the run).
func normalizeChannelOverride(channels []string) []contact.Channel {
	if len(channels) == 0 {
		return nil
	}
	out := make([]contact.Channel, 0, len(channels))
	for _, c := range channels {
		trimmed := contact.Channel(strings.TrimSpace(c))
		switch trimmed {
		case contact.ChannelTelnyxVoice, contact.ChannelSignalVoice,
			contact.ChannelSignalMsg, contact.ChannelTelnyxSMS:
			out = append(out, trimmed)
		}
	}
	return out
}

// renderChainReport renders the ChainReport as lowercase per-channel
// outcome lines (C6; the UI convention). Attempts render in run order.
func renderChainReport(person string, rep contact.ChainReport) string {
	var b strings.Builder
	for _, att := range rep.Attempts {
		line := fmt.Sprintf("%s: %s", att.Channel, att.Outcome)
		if att.Err != "" && att.Outcome != string(voice.OutcomeAnswered) {
			// Non-terminal outcomes carry their err for honest reporting
			// but the line stays lowercase and short.
			line += fmt.Sprintf(" (%s)", att.Err)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	if len(rep.Attempts) == 0 {
		b.WriteString(fmt.Sprintf("contact: no channels attempted for %s\n", person))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// BuildContactChain assembles the chain from the voice wiring's live
// parts. Called from the voice Start closure (components.go) after
// WireVoice returns: w.Outcomes (armed by the release hook) and
// w.Signal (the signal leg, nil when disabled) are both live. Fail-soft:
// an error degrades the contact feature (nil wiring, logged reason),
// never the boot.
func BuildContactChain(cfg *config.Config, w *VoiceWiring, ledger effects.Ledger, audit *auditlog.Store, broker brokerValue, log *slog.Logger) (*ContactChainWiring, error) {
	if w == nil || w.outcomes == nil {
		return nil, fmt.Errorf("contact wiring: voice wiring or outcome collector missing")
	}
	baseDir := config.MeeptPath("contact")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("contact wiring: store dir: %w", err)
	}
	store, err := contact.NewStore(filepath.Join(baseDir, "contacts.db"))
	if err != nil {
		return nil, fmt.Errorf("contact wiring: store: %w", err)
	}

	// telnyx_sms client + consent glue (leaf 04/02 hoist): the leaf-12
	// attempt (broker key; disabled config = DisabledSMSAttempt refuses
	// before any HTTP). Leaf 02-send-leg/02: the attempt sends through
	// SendSMS2 with a per-message webhook_url derived from
	// [voice.messaging].webhook_base_url — exactly the path
	// internal/comm/http registers the messaging webhook on. Empty base
	// -> empty WebhookURL -> the body key is omitted and Telnyx's
	// account-level webhook applies (D3: disabled-default posture stays
	// byte-identical). Leaf 04/02 hoists the construction out of the
	// attempts closure: the SAME client doubles as the consent hook's
	// DIRECT sender (the TCPA courtesy-confirmation path that bypasses
	// the D5 opt-out consult — sms_reply_glue.go), so one key
	// resolution, one client per enabled messaging block.
	var smsClient *telnyx.Client
	var smsConfirms *sms.PendingConfirms
	if cfg.Voice.Messaging.Enabled {
		apiKey := ""
		if broker != nil {
			if v, err := broker("telnyx_api_key"); err == nil {
				apiKey = v
			}
		}
		client, cerr := telnyx.NewClient(telnyxBaseURL(),
			telnyx.WithAPIKey(apiKey),
			telnyx.WithProfileID(cfg.Voice.Messaging.ProfileID),
			telnyx.WithFromNumber(cfg.Voice.Messaging.FromNumber))
		if cerr != nil {
			log.Warn("contact wiring: telnyx messaging client failed; sms channel skipped", "error", cerr)
		} else {
			smsClient = client
			smsConfirms = sms.NewPendingConfirms(nil, consentPendingTTL)
			// Consent reply glue (leaf 04/02): build the production
			// ReplyHook over the chain's live parts (ONE contacts.db
			// handle, the hoisted sender client, the wiring's bus) and
			// late-arm it through the slot the armed OnInbound closure
			// reads per event — same D6 timing as setSMSAttempt: the
			// chain is built AFTER WireVoice returns. Empty StopReply
			// falls back to the sms-pkg wording defaults; empty
			// HelpReply keeps HELP silent (the hook's own guards).
			// Arming is INDEPENDENT of inbound_enabled at the hook
			// level: the webhook seam itself only fires when armed
			// (voice_wiring.go §6), so a hook built for an enabled
			// messaging block is inert until the D3 switch opens.
			w.setConsentReplyHook(newConsentReplyHook(consentReplyDeps{
				Contacts:     store,
				Sender:       client,
				Bus:          w.bus,
				HelpThrottle: voicetelnyx.NewNonceStore(consentHelpWindow, consentHelpMax),
				Confirms:     smsConfirms,
				StopReply:    cfg.Voice.Messaging.StopReply,
				HelpReply:    cfg.Voice.Messaging.HelpReply,
				Logger:       log,
			}))
			log.Info("contact wiring: consent reply hook armed",
				"inbound_enabled", cfg.Voice.Messaging.InboundEnabled)
		}
	}

	attempts := func(store *contact.SQLiteStore) map[contact.Channel]contact.ChannelAttempt {
		m := make(map[contact.Channel]contact.ChannelAttempt, 4)
		// telnyx_voice: the EXISTING dial path (dialForTool, gated by
		// allowlist + effects ledger as today) + the outcome collector
		// fed from the release hook.
		if dial := w.dialForTool(); dial != nil {
			m[contact.ChannelTelnyxVoice] = voice.NewTelnyxVoiceAttempt(dial, w.outcomes.Lookup)
		}
		// signal_voice + signal_msg: the Signal leg (nil when disabled).
		if w.Signal != nil && w.Signal.Leg != nil {
			leg := w.Signal.Leg
			// Leaf 03 (tree 20261110-voice-callleg): the voice
			// attempt places through the CallLeg contract —
			// voice.NewSignalAdapter(leg), constructed HERE at
			// the single leg-construction site (signal_wiring.go
			// arms the leg; this is its only production caller).
			// The adapter calls THROUGH the same leg: identical
			// gates, identical effect keys. Its Events() lane is
			// intentionally unconsumed by the chain today
			// (terminal classification still rides w.outcomes).
			m[contact.ChannelSignalVoice] = leg.VoiceAttempt(voice.NewSignalAdapter(leg), w.outcomes.Lookup)
			m[contact.ChannelSignalMsg] = leg.MessageAttempt()
		}
		// telnyx_sms: the attempt over the hoisted client (nil client —
		// messaging disabled or client construction failure — leaves
		// the channel unregistered, the historical degraded posture).
		if smsClient != nil {
			var webhookURL string
			if base := strings.TrimRight(cfg.Voice.Messaging.WebhookBaseURL, "/"); base != "" {
				webhookURL = base + "/api/v1/voice/messaging/webhook"
			}
			att := telnyx.NewSMSAttempt(smsClient, telnyx.SMSAttemptConfig{
				FromNumber:  cfg.Voice.Messaging.FromNumber,
				ProfileID:   cfg.Voice.Messaging.ProfileID,
				WaitSeconds: cfg.Voice.Messaging.DLRWaitSeconds,
				WebhookURL:  webhookURL,
			}, log)
			// D6 late-arming (leaf 03/02): the chain is built AFTER
			// WireVoice, so hand the live DLR pending-map owner to
			// the armed messaging OnFinalized seam through the slot
			// the closure reads per event.
			w.setSMSAttempt(att)
			m[contact.ChannelTelnyxSMS] = att
		}
		return m
	}

	delay := time.Duration(cfg.Contact.HopDelayMS) * time.Millisecond
	delays := map[contact.Channel]time.Duration{
		contact.ChannelTelnyxVoice: delay,
		contact.ChannelSignalVoice: delay,
		contact.ChannelSignalMsg:   delay,
	}
	runner := contact.NewChainRunner(attempts(store), delays)
	if ledger != nil || audit != nil {
		runner = runner.WithSinks(contact.ChainSinks{Effects: ledger, Audit: audit})
	}
	return &ContactChainWiring{Store: store, Runner: runner, SMSConfirms: smsConfirms}, nil
}

// Close releases the contacts store (daemon Stop path).
func (w *ContactChainWiring) Close() {
	if w != nil && w.Store != nil {
		_ = w.Store.Close()
	}
}

// brokerValue is the narrow secrets seam (the components SecretBroker's
// Value method shape); the concrete *secrets.Broker satisfies it.
type brokerValue func(key string) (string, error)

// Compile-time pins: the wiring satisfies the tool seam; the landed
// adapters satisfy the chain seam (leaf 11a/11b/12 surfaces).
var (
	_ builtin.ContactChainRunner = (*ContactChainWiring)(nil)
	_ contact.ChannelAttempt     = (*telnyx.SMSAttempt)(nil)
)
