package daemon

// Consent reply glue (plan 20261110-telnyx-texting, leaf 04-replies-
// consent/02, master.md §7 + D5; gate: §K3 in sms_reply_glue_test.go).
//
// newConsentReplyHook builds the production ReplyHook the armed messaging
// OnInbound seam invokes SYNCHRONOUSLY after the durable store append +
// InboundTopic publish (messaging_arming.go). The hook switches on the
// sms.ClassifyReply action:
//
//	ReplyStop  -> contact opt-out SET (read-modify-Put, second-wins,
//	               contact/store.go:221-258) + courtesy confirmation.
//	               Unknown number: NO ghost record, confirmation still
//	               sends (02-glue task 1). Already-opted-out contact:
//	               silent (the confirmation dedupes store-read-side —
//	               the record's own flag is the dedupe state, no nonce
//	               needed: a repeat STOP from a set-flag contact is a
//	               no-op by construction).
//	ReplyStart -> opt-out CLEARED (existing record only) + confirmation
//	               (STOP/START share cfg.StopReply per master §7; the
//	               sms pkg's symmetric default wording covers START when
//	               the operator left stop_reply empty).
//	ReplyHelp  -> cfg.HelpReply sent iff non-empty AND HelpThrottle
//	               .CheckFresh(from) — one per contact per 24h (NonceStore
//	               ttl-24h reuse, master §7). Empty cfg = always silent.
//	ReplyYes   -> Confirms.Resolve(from); a hit publishes the #132
//	               dispatch ack on sms.StateTopic (ProviderMessageID =
//	               pending.Ref, Status = "ack" — the two-string-slot v1
//	               reuse, no new payload type). One-shot registry: a
//	               second YES publishes nothing.
//	ReplyOther -> no side effects.
//
// CONFIRMATION SENDS ARE DIRECT (02-glue task 2): the STOP/START/HELP
// confirmations go through telnyx.Client.SendSMS2 on the hook's own
// sender, NEVER the contact channel — the channel consults the D5
// MessagingOptOut guard and would refuse to deliver the very STOP
// confirmation that must reach an just-opted-out number (TCPA courtesy-
// confirmation exception). Every send-failure leg is best-effort: the
// consent STATE change already landed durably in contacts.db, so a
// returned error logs at Warn and the hook continues to the next leg
// (and the seam itself swallows hook errors regardless — §K3).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/contact"
	"github.com/caimlas/meept/internal/sms"
	"github.com/caimlas/meept/internal/telnyx"
	voicetelnyx "github.com/caimlas/meept/internal/voice/telnyx"
)

// consentAckStatus is the Status slot value of the #132 dispatch ack
// published on sms.StateTopic (gate §K3: "Status = \"ack\"" verbatim;
// v1 reuses the DLR payload's two string slots).
const consentAckStatus = "ack"

// consentReplyTimeout bounds one hook invocation's contact-store reads
// and writes. The webhook seam gives the whole OnInbound leg
// webhookArmTimeout; the store handle serializes on MaxOpenConns(1)
// with a 5s busy timeout, so a shorter per-leg cap keeps one slow read
// from starving the store's single writer.
const consentReplyTimeout = 3 * time.Second

// consentReplyDeps is the production hook's input (shape FROZEN by the
// §K3 gate — field names and types are the pinned contract).
type consentReplyDeps struct {
	Contacts *contact.SQLiteStore
	Sender   *telnyx.Client
	Bus      *bus.MessageBus
	// HelpThrottle is a voicetelnyx.NewNonceStore(24h, n) instance —
	// CheckFresh(from) == "this contact has not HELPed in the window".
	HelpThrottle *voicetelnyx.NonceStore
	Confirms     *sms.PendingConfirms
	// StopReply / HelpReply are cfg [voice.messaging] stop_reply /
	// help_reply. Empty StopReply falls back to the sms-pkg ported
	// wording defaults; empty HelpReply keeps HELP SILENT (the auto-
	// reply arms only on operator-set text).
	StopReply string
	HelpReply string
	Logger    *slog.Logger
}

// newConsentReplyHook builds the ReplyHook (nil-safe on every dep: a
// missing sub-dep only disables its leg — Contacts kills the opt-out
// toggles, Sender kills every confirmation send, Confirms kills YES,
// HelpThrottle fails-open the HELP throttle to "throttled"... see the
// per-leg guards). The seam guarantees the hook only ever sees newly-
// applied, already-stored events, so each leg runs its state change
// first and the send second.
func newConsentReplyHook(d consentReplyDeps) func(voicetelnyx.Message, sms.ReplyAction) error {
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	log := logger.With("component", "sms-consent-reply")

	return func(msg voicetelnyx.Message, action sms.ReplyAction) error {
		switch action {
		case sms.ReplyStop:
			return consentStop(d, log, msg)
		case sms.ReplyStart:
			return consentStart(d, log, msg)
		case sms.ReplyHelp:
			return consentHelp(d, log, msg)
		case sms.ReplyYes:
			return consentYes(d, log, msg)
		default: // ReplyOther (and any future action): inert.
			return nil
		}
	}
}

// consentStop implements the STOP leg: SET MessagingOptOut on an existing
// record (read-modify-Put; unknown numbers create NO ghost row) and send
// the courtesy confirmation through the DIRECT sender — even to the
// just-set opt-out (TCPA courtesy exception, 02-glue task 2; the contact
// channel's D5 consult would refuse exactly this message).
//
// Dedupe posture: the contact record's flag IS the dedupe state. A
// second STOP from an already-opted contact re-asserts the flag (a
// no-op Put skipped entirely when already set) and sends NOTHING new —
// no stop-confirm nonce needed, and unknown numbers cannot dedupe
// against a record.
func consentStop(d consentReplyDeps, log *slog.Logger, msg voicetelnyx.Message) error {
	text := consentStopText(d.StopReply)
	var firstErr error
	if d.Contacts != nil {
		ctx, cancel := context.WithTimeout(context.Background(), consentReplyTimeout)
		defer cancel()
		rec, err := d.Contacts.GetByPhone(ctx, msg.From)
		switch {
		case err == nil:
			if rec.MessagingOptOut {
				// Already opted out: confirmation dedupes (second
				// STOP sends nothing, flag stays set).
				log.Debug("daemon: duplicate STOP from opted-out contact; no confirmation", "event_id", msg.EventID)
				return nil
			}
			rec.MessagingOptOut = true
			if err := d.Contacts.Put(ctx, rec); err != nil {
				firstErr = fmt.Errorf("daemon: consent STOP opt-out write: %w", err)
			}
		case errors.Is(err, contact.ErrUnknownContact):
			// Polite-unknown posture: confirm the request, create no
			// record (junk inbound must not mint ghost rows).
			log.Info("daemon: STOP from unknown number; confirmation only", "event_id", msg.EventID)
		default:
			firstErr = fmt.Errorf("daemon: consent STOP contact lookup: %w", err)
		}
	}
	if err := consentSend(d, log, msg.From, text); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// consentStart implements the symmetric START leg: CLEAR MessagingOptOut
// on an existing record (unknown numbers: nothing to clear, and — unlike
// STOP — no confirmation either is wrong; the gate only pins the seeded
// contact path, so the unknown number still gets the courtesy text
// without creating a row) + confirmation send via the DIRECT sender.
// STOP/START share the one cfg key (master §7: no separate start_reply).
func consentStart(d consentReplyDeps, log *slog.Logger, msg voicetelnyx.Message) error {
	text := consentStartText(d.StopReply)
	var firstErr error
	if d.Contacts != nil {
		ctx, cancel := context.WithTimeout(context.Background(), consentReplyTimeout)
		defer cancel()
		rec, err := d.Contacts.GetByPhone(ctx, msg.From)
		switch {
		case err == nil:
			if !rec.MessagingOptOut {
				// Already subscribed: dedupe the confirmation exactly
				// like the STOP mirror.
				log.Debug("daemon: START from already-subscribed contact; no confirmation", "event_id", msg.EventID)
				return nil
			}
			rec.MessagingOptOut = false
			if err := d.Contacts.Put(ctx, rec); err != nil {
				firstErr = fmt.Errorf("daemon: consent START opt-in write: %w", err)
			}
		case errors.Is(err, contact.ErrUnknownContact):
			log.Info("daemon: START from unknown number; confirmation only", "event_id", msg.EventID)
		default:
			firstErr = fmt.Errorf("daemon: consent START contact lookup: %w", err)
		}
	}
	if err := consentSend(d, log, msg.From, text); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// consentHelp implements the HELP leg: cfg.HelpReply empty => SILENT
// (auto-reply arms only on operator-set text); non-empty => send iff
// the per-contact 24h throttle says fresh. CheckFresh records the key
// on true, so the second HELP inside the window reads false. A nil
// throttle fails CLOSED (silent) — sending unthrottled HELP invites the
// exact flood the window exists to prevent.
func consentHelp(d consentReplyDeps, log *slog.Logger, msg voicetelnyx.Message) error {
	if strings.TrimSpace(d.HelpReply) == "" {
		return nil
	}
	if d.HelpThrottle == nil || !d.HelpThrottle.CheckFresh(msg.From) {
		log.Debug("daemon: HELP throttled; no reply", "event_id", msg.EventID)
		return nil
	}
	return consentSend(d, log, msg.From, d.HelpReply)
}

// consentYes implements the #132 dispatch-ack leg: resolve the pending
// confirmation for the sender and publish the ack on sms.StateTopic
// (ProviderMessageID = pending ref the dispatch tree registered with,
// Status = "ack"). NO SMS send — the ack is a bus event. Registry
// one-shot + miss posture mean a second/unregistered YES is silent.
func consentYes(d consentReplyDeps, log *slog.Logger, msg voicetelnyx.Message) error {
	if d.Confirms == nil {
		return nil
	}
	pending, ok := d.Confirms.Resolve(msg.From)
	if !ok || pending == nil {
		log.Debug("daemon: YES without pending confirmation; ignored", "event_id", msg.EventID)
		return nil
	}
	if d.Bus == nil {
		return fmt.Errorf("daemon: consent YES resolved but no bus to ack: ref %s", pending.Ref)
	}
	bus.PublishT(d.Bus, sms.StateTopic, "consent-reply", sms.StateChanged{
		ProviderMessageID: pending.Ref,
		Status:            consentAckStatus,
	})
	return nil
}

// consentSend is the DIRECT courtesy send (02-glue task 2). It goes
// through the hook's own telnyx.Client.SendSMS2 — NEVER the contact
// channel — because the channel consults the D5 MessagingOptOut guard
// BEFORE dialing and would refuse to deliver the confirmation to the
// number that just opted out. The TCPA courtesy-confirmation exception
// is exactly this path. Failures return the error for the seam to log;
// E.164-invalid senders are skipped (SendSMS2 would refuse anyway).
func consentSend(d consentReplyDeps, log *slog.Logger, to, text string) error {
	if d.Sender == nil || text == "" || to == "" {
		return nil
	}
	if !telnyx.ValidE164(to) {
		log.Debug("daemon: consent reply skipped; non-E.164 sender", "to_len", len(to))
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := d.Sender.SendSMS2(ctx, telnyx.SendRequest{To: to, Text: text}); err != nil {
		log.Warn("daemon: consent confirmation send failed", "error", err)
		return fmt.Errorf("daemon: consent confirmation send: %w", err)
	}
	return nil
}

// consentStopText / consentStartText resolve the STOP/START confirmation
// wording: cfg text when set, else the sms-pkg ported defaults (master
// §7 — the STOP default references START and vice versa, so an
// unconfigured operator still sends compliant wording).
func consentStopText(cfg string) string {
	if strings.TrimSpace(cfg) != "" {
		return cfg
	}
	return sms.ConsentDefaultStopReply
}

func consentStartText(cfg string) string {
	if strings.TrimSpace(cfg) != "" {
		return cfg
	}
	return sms.ConsentDefaultStartReply
}
