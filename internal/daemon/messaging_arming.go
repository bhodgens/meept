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
	"github.com/caimlas/meept/internal/sms"
	"github.com/caimlas/meept/internal/telnyx"
	voicetelnyx "github.com/caimlas/meept/internal/voice/telnyx"
)

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

// setSMSAttempt registers the contact chain's live SMSAttempt (the DLR
// pending-map owner) into the voice wiring's messaging arming slot, so
// the armed OnFinalized seam can resolve blocked Attempts — the D6 fix
// end-to-end. Called from BuildContactChain at the single attempt-
// construction site. Nil wiring/slot: no-op (messaging never armed).
func (w *VoiceWiring) setSMSAttempt(a *telnyx.SMSAttempt) {
	if w == nil {
		return
	}
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
