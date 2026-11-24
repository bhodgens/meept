package daemon

// Messaging webhook hook arming (plan 20261110-telnyx-texting, leaf
// 03-inbound-store/02, master.md §6) — the dead-seam gap closer (D6).
//
// Before this file the daemon armed the comm/http messaging webhook with
// ONLY the signature verifier (voice_wiring.go §10b): OnInbound and
// OnFinalized were never assigned anywhere, so inbound texts were parsed
// and dropped, and a blocked SMSAttempt's DLR wait could only ever time
// out to "unavailable". buildMessagingDeps constructs the consumer seams:
//
//	OnInbound:   store.AppendEvent (dup event id -> ack-ok, no publish) +
//	             bus.PublishT(sms.InboundTopic) + reply-classifier seam (§7,
//	             inserted by leaf 04/01 — see the marker inside).
//	OnFinalized: store.AppendEvent (audit row, direction outbound) +
//	             store.UpsertState (monotonic §4) +
//	             bus.PublishT(sms.StateTopic) + DLR resolve via
//	             telnyx.SMSAttempt.ResolvePending (§6).
//
// A nil Store (inbound_enabled=false — the D3 kill switch, §8) yields
// BOTH callbacks nil: the handler degrades to parse-and-ack,
// byte-identical to the fully unwired route (gate §T4 golden).
//
// WS CLASSIFICATION: this file publishes on the typed sms.* lanes
// (internal/sms/topics.go) — an ingest fan-out, not a websocket bridge.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	commhttp "github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/sms"
	"github.com/caimlas/meept/internal/telnyx"
	voicetelnyx "github.com/caimlas/meept/internal/voice/telnyx"
)

// messagingUnrestrictedWarning is the exact operator-facing hazard string
// for issue #165 slice 5. It is a single deterministic Warn at arming time
// (never per-send): a default-disabled boot never reaches this code, so
// zero-config startup stays byte-identical.
const messagingUnrestrictedWarning = "WARNING: outbound messaging has no recipient allowlist or send cap; any E.164 number may be contacted"

// messagingWarnOnce makes the warning idempotent per process. The arming
// path may call warnIfMessagingUnrestricted from more than one site (the
// webhook-deps builder and the enabled-block in voice_wiring.go); the
// operator must see exactly one line, not a duplicate.
var messagingWarnOnce sync.Once

// warnIfMessagingUnrestricted emits the exact startup warning when outbound
// messaging is enabled but NEITHER an allowlist NOR any volume cap is
// configured. Config is passed explicitly (no global/config read here) so
// the decision is deterministic and the warning cannot fire on a
// default-disabled boot. A nil logger degrades to slog.Default(). At most
// one warning is emitted per process regardless of caller count.
func warnIfMessagingUnrestricted(logger *slog.Logger, cfg config.MessagingConfig) {
	if !cfg.Enabled {
		return
	}
	if len(cfg.AllowedNumbers) > 0 || cfg.MaxMessagesPerCall > 0 || cfg.MaxMessagesPerDay > 0 {
		return
	}
	messagingWarnOnce.Do(func() {
		if logger == nil {
			logger = slog.Default()
		}
		logger.Warn(messagingUnrestrictedWarning)
	})
}

// webhookArmTimeout bounds a single store write driven by a webhook
// event. The SQLite handle serializes on one connection with a 5s busy
// timeout; a longer lock means something is wedged and the handler's
// 500-redelivery path (nonce released first) is the honest posture.
const webhookArmTimeout = 5 * time.Second

// messagingArmingDeps is the arming input (gate §T3 pinned shape). Store
// nil => inbound disabled => buildMessagingDeps returns zero-value deps
// (D3). Attempts may be nil (no live DLR map owner yet); the production
// wiring instead registers the SMSAttempt lazily through dlrAttemptSlot
// (see slot) because the contact chain — which owns the attempt — is
// built AFTER WireVoice returns.
type messagingArmingDeps struct {
	Store    *sms.Store
	Nonce    *voicetelnyx.NonceStore
	Bus      *bus.MessageBus
	Attempts *telnyx.SMSAttempt
	Logger   *slog.Logger

	// ReplyHook (leaf 04/02, §K3 pin): the consent reply glue, invoked
	// SYNCHRONOUSLY with (msg, ClassifyReply(msg.Text)) for every action
	// — including ReplyOther — after the store append + InboundTopic
	// publish. Errors are logged + swallowed (never a redelivery storm).
	// Nil => no reply handling at all (the pre-04/02 arming shape,
	// byte-identical). Production late-arms through replySlot instead
	// (same D6 reasoning as slot below: the contact chain — which owns
	// the store handle + messaging client the hook closes over — is
	// built AFTER WireVoice returns); direct field wins when both set.
	ReplyHook func(voicetelnyx.Message, sms.ReplyAction) error

	// replySlot is the late-registration seam for the production consent
	// hook (wiring-only; the gates inject ReplyHook directly). Read
	// per-event so BuildContactChain's newConsentReplyHook arm takes
	// effect without rebuilding the route.
	replySlot *consentReplySlot

	// slot is the late-registration seam for the live SMSAttempt
	// (wiring-only; the gate injects Attempts directly). Read per-event
	// so a BuildContactChain registration after WireVoice takes effect
	// without rebuilding the route.
	slot *dlrAttemptSlot
}

// dlrAttemptSlot carries the live *telnyx.SMSAttempt to the armed
// OnFinalized closure. mutex-guarded pointer handoff only — no I/O under
// the lock (repo rule).
type dlrAttemptSlot struct {
	mu  sync.Mutex
	att *telnyx.SMSAttempt
}

func (s *dlrAttemptSlot) set(a *telnyx.SMSAttempt) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.att = a
	s.mu.Unlock()
}

func (s *dlrAttemptSlot) get() *telnyx.SMSAttempt {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.att
}

// consentReplySlot carries the production *consent reply hook* to the
// armed OnInbound closure (leaf 04/02). Same mutex-guarded pointer
// handoff shape as dlrAttemptSlot — no I/O under the lock (repo rule).
// nil-safe both ways: an unwired slot reads nil, i.e. no reply handling.
type consentReplySlot struct {
	mu   sync.Mutex
	hook func(voicetelnyx.Message, sms.ReplyAction) error
}

func (s *consentReplySlot) set(h func(voicetelnyx.Message, sms.ReplyAction) error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.hook = h
	s.mu.Unlock()
}

func (s *consentReplySlot) get() func(voicetelnyx.Message, sms.ReplyAction) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hook
}

// setConsentReplyHook registers the production consent reply hook into
// the voice wiring's arming slot (leaf 04/02). Called from
// BuildContactChain — the chain is the single construction site that has
// BOTH the contacts store handle and the messaging telnyx.Client the hook
// closes over, and it runs AFTER WireVoice armed the route. Nil wiring:
// no-op (voice never wired). A nil hook on a disabled block keeps the
// slot inert: OnInbound skips reply handling entirely.
func (w *VoiceWiring) setConsentReplyHook(h func(voicetelnyx.Message, sms.ReplyAction) error) {
	if w == nil {
		return
	}
	w.replySlot.set(h)
}

// setSMSAttempt registers the contact chain's live SMSAttempt (the DLR
// pending-map owner) into the voice wiring's messaging arming slot, so
// the armed OnFinalized seam can resolve blocked Attempts — the D6 fix
// end-to-end. Called from BuildContactChain at the single attempt-
// construction site. Nil wiring/slot: no-op (messaging never armed).
func (w *VoiceWiring) setSMSAttempt(a *telnyx.SMSAttempt) {
	if w == nil {
		return
	}
	// Issue #165 slice 5: BuildContactChain calls this on EVERY production
	// boot where messaging is armed, AFTER WireVoice stored the resolved
	// daily send counter (w.dailyCounter, built from the voice state dir)
	// and the structured logger (w.logger) on the wiring — so this is the
	// earliest reliable site to check the messaging config once with the
	// wiring's own state. Emit the unrestricted-messaging warning through
	// the wiring's structured logger (falls back to slog.Default for
	// hand-built wirings); warnIfMessagingUnrestricted is process-once.
	warnIfMessagingUnrestricted(w.logger, w.Config().Messaging)
	w.dlrSlot.set(a)
}

// buildMessagingDeps assembles the comm/http consumer seams per master
// §6. The returned deps carry OnInbound/OnFinalized (+ Nonce when a
// store + nonce are armed); Verify is the wiring site's responsibility
// (the gate injects its own discriminator).
func buildMessagingDeps(d messagingArmingDeps) commhttp.MessagingWebhookDeps {
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if d.Store == nil {
		// Inbound disabled (D3): deps unset entirely — parse-and-ack,
		// byte-identical to the nil-deps route (§T4 golden). No nonce
		// dedupe either: nothing downstream consumes the event.
		return commhttp.MessagingWebhookDeps{}
	}
	store := d.Store
	resolve := func(providerMessageID, status string) {
		att := d.Attempts
		if att == nil {
			att = d.slot.get()
		}
		if att != nil {
			att.ResolvePending(providerMessageID, status)
		}
	}

	deps := commhttp.MessagingWebhookDeps{}
	if d.Nonce != nil {
		deps.Nonce = d.Nonce
	}

	deps.OnInbound = func(msg voicetelnyx.Message) error {
		ctx, cancel := context.WithTimeout(context.Background(), webhookArmTimeout)
		defer cancel()
		now := time.Now().UTC()
		applied, err := store.AppendEvent(ctx, sms.Event{
			EventID:    msg.EventID,
			Event:      msg.Event,
			Direction:  msg.Direction,
			Src:        msg.From,
			Dst:        msg.To,
			Body:       msg.Text,
			ProviderTS: smsProviderStamp(msg, now),
			ReceivedAt: now.Format(time.RFC3339Nano),
		})
		if err != nil {
			// Transient store failure: surface it so the handler
			// Releases the nonce and 500s -> Telnyx redelivers.
			logger.Warn("daemon: messaging inbound store append failed", "event_id", msg.EventID, "error", err)
			return fmt.Errorf("daemon: messaging inbound store: %w", err)
		}
		if !applied {
			// Duplicate event id (INSERT OR IGNORE): ack-ok, no
			// fan-out — the redelivery guard inside the glue, behind
			// the handler's nonce dedupe.
			logger.Info("daemon: messaging inbound duplicate event acked", "event_id", msg.EventID)
			return nil
		}
		if d.Bus != nil {
			bus.PublishT(d.Bus, sms.InboundTopic, "messaging", sms.InboundMessage{
				EventID: msg.EventID,
				From:    msg.From,
				To:      msg.To,
				Text:    msg.Text,
				At:      smsEventTime(msg, now),
			})
		}

		// -- SEAM (leaf 04/01): the §7 reply/consent classifier hooks
		// HERE, after the durable append + InboundTopic fan-out: STOP /
		// START / HELP / YES keyword handling (contact.OptOut flip,
		// confirmation replies, PendingConfirms resolve). Deliberately
		// placed after the store+publish so classification can never
		// drop a durably-received message, and its failures cannot turn
		// an already-stored event into a redelivery storm.
		//
		// Leaf 04/02 arms it: ClassifyReply runs on every NEWLY-APPLIED
		// inbound (the dup-ack above returned before this point), and a
		// non-nil hook is invoked synchronously for EVERY action — the
		// hook switches internally. A hook error is logged and swallowed
		// (§K3 poison pin): the row is durable, the fan-out fired, so
		// 200-ack is the honest posture and redelivery would only spin
		// the nonce guard.
		hook := d.ReplyHook
		if hook == nil {
			hook = d.replySlot.get()
		}
		if hook != nil {
			if err := hook(msg, sms.ClassifyReply(msg.Text)); err != nil {
				logger.Warn("daemon: messaging reply hook failed", "event_id", msg.EventID, "error", err)
			}
		}

		return nil
	}

	deps.OnFinalized = func(msg voicetelnyx.Message) error {
		ctx, cancel := context.WithTimeout(context.Background(), webhookArmTimeout)
		defer cancel()
		now := time.Now().UTC()
		stamp := smsProviderStamp(msg, now)
		errText := smsEventError(msg)

		// Audit leg: the terminal DLR (and interack sent/queued/sending)
		// gets its own sms_events_raw row, direction outbound (gate §T3
		// raw-SQL assertion). Idempotent via the event-id insert-ignore.
		if _, err := store.AppendEvent(ctx, sms.Event{
			EventID:    msg.EventID,
			Event:      msg.Event,
			Direction:  msg.Direction,
			Src:        msg.From,
			Dst:        msg.To,
			Body:       msg.Text,
			ProviderTS: stamp,
			ReceivedAt: now.Format(time.RFC3339Nano),
		}); err != nil {
			logger.Warn("daemon: messaging finalized store append failed", "event_id", msg.EventID, "error", err)
			return fmt.Errorf("daemon: messaging finalized store: %w", err)
		}

		// State leg: monotonic upsert (§4) — out-of-order writes are
		// ignored and never fan out. Empty provider id: nothing to key
		// (the DLR resolution key is payload.id; skip the state table).
		if msg.ProviderMessageID != "" {
			applied, err := store.UpsertState(ctx, sms.State{
				ProviderMessageID: msg.ProviderMessageID,
				Dst:               msg.To,
				Status:            msg.Status,
				Err:               errText,
				UpdatedAt:         stamp,
				EventID:           msg.EventID,
			})
			if err != nil {
				logger.Warn("daemon: messaging state upsert failed", "event_id", msg.EventID, "message_id", msg.ProviderMessageID, "error", err)
				return fmt.Errorf("daemon: messaging state upsert: %w", err)
			}
			if applied && d.Bus != nil {
				bus.PublishT(d.Bus, sms.StateTopic, "messaging", sms.StateChanged{
					ProviderMessageID: msg.ProviderMessageID,
					Status:            msg.Status,
					Err:               errText,
				})
			}
		}

		// DLR leg (D6): wake any blocked Attempt BEFORE answering 202.
		// ResolvePending is terminal-status-only (sent/queued/sending
		// interacks are ignored there).
		resolve(msg.ProviderMessageID, msg.Status)
		return nil
	}

	return deps
}

// smsProviderStamp renders the provider event timestamp the store's
// monotonic guard compares (RFC3339Nano, the house stamp). Parse fails
// soft to zero IssuedAt, so an absent/zero timestamp falls back to the
// webhook receipt time — the ordering the daemon actually observed.
func smsProviderStamp(msg voicetelnyx.Message, now time.Time) string {
	if msg.IssuedAt.IsZero() {
		return now.Format(time.RFC3339Nano)
	}
	return msg.IssuedAt.UTC().Format(time.RFC3339Nano)
}

// smsEventTime is the InboundMessage fan-out timestamp (real time, same
// fallback as the provider stamp).
func smsEventTime(msg voicetelnyx.Message, now time.Time) time.Time {
	if msg.IssuedAt.IsZero() {
		return now
	}
	return msg.IssuedAt.UTC()
}

// smsEventError renders the payload's error list into the state table's
// err column (empty on success / no errors).
func smsEventError(msg voicetelnyx.Message) string {
	if len(msg.Errors) == 0 {
		return ""
	}
	parts := make([]string, 0, len(msg.Errors))
	for _, e := range msg.Errors {
		parts = append(parts, fmt.Sprintf("%s %s", e.Code, strings.TrimSpace(e.Description)))
	}
	return strings.Join(parts, "; ")
}
