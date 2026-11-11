// SMSAttempt: the telnyx_sms contact.ChannelAttempt adapter (contract
// C5, leaf 12 T4). Send via the messaging client, then wait a bounded,
// injectable time for the message.finalized DLR for the returned id:
//
//	delivered -> ("delivered", nil)
//	failed    -> ("failed", non-nil error)
//	timeout   -> ("unavailable", non-nil error) — the chain falls
//	             through; the DLR continues async and updates the
//	             recorded outcome for observability (best-effort, no
//	             goroutine leak: the waiter is the caller's bounded poll
//	             loop, which exits on timeout; the map entry absorbs the
//	             later DLR, never a detached goroutine).
//
// The pending map is a small mutex-guarded struct keyed by message id,
// fed by OnFinalized (the webhook receiver's DLR seam). Repo rule: no
// mutex across I/O — the lock guards only map reads/writes; all waiting
// and logging happen outside it. The map is bounded (evict-oldest above
// MaxPendingDLR) so a DLR flood cannot grow it without limit.
package telnyx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/contact"
)

// MaxPendingDLR bounds the pending DLR map (oldest entries evicted
// first). Generous by design: one entry per in-flight outbound SMS.
const MaxPendingDLR = 256

// dlrPollInterval is how often the bounded wait polls the pending map
// for the finalized DLR.
const dlrPollInterval = 100 * time.Millisecond

// DLROutcome is the recorded resolution of one sent message, kept for
// observability after the attempt has already returned (the async-DLR
// case) and readable via SMSAttempt.Outcome.
type DLROutcome struct {
	Status     string    // "delivered" | "failed"
	Err        string    // non-empty when Status == "failed"
	Resolved   bool      // true once a finalized DLR was observed
	ResolvedAt time.Time // when the DLR landed (zero until then)
	done       bool      // internal: guards against duplicate resolution
}

// pendingDLR is the in-memory pending map keyed by message id, fed by
// OnFinalized. All map access is inside mu; NO I/O happens under the
// lock (callers wait and log outside).
type pendingDLR struct {
	mu      sync.Mutex
	pending map[string]*DLROutcome // id -> outcome slot; presence = still waiting
	order   []string               // insertion order for bounded eviction
}

func newPendingDLR() *pendingDLR {
	return &pendingDLR{pending: make(map[string]*DLROutcome)}
}

// RegisterPending records that we are waiting for the DLR of id and
// returns the outcome slot the resolver will fill. Bounded: above
// MaxPendingDLR the OLDEST entry is evicted (its DLR, if it ever lands,
// resolves to ErrUnknownMessage — a logged observability event, never a
// panic).
func (p *pendingDLR) RegisterPending(id string) *DLROutcome {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := &DLROutcome{}
	p.pending[id] = out
	p.order = append(p.order, id)
	for len(p.order) > MaxPendingDLR {
		oldest := p.order[0]
		p.order = p.order[1:]
		delete(p.pending, oldest)
	}
	return out
}

// ResolveFinalized records a finalized DLR for id. Returns an error
// wrapping ErrUnknownMessage when no pending entry exists (late or
// duplicate DLR, evicted id, or a message this process never sent) —
// the caller logs and answers 200 so Telnyx does not redeliver.
func (p *pendingDLR) ResolveFinalized(id, status string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	out, ok := p.pending[id]
	if !ok || out.done {
		return fmt.Errorf("%w: %s", ErrUnknownMessage, id)
	}
	out.Status = status
	out.Resolved = true
	out.ResolvedAt = time.Now()
	if status != "delivered" {
		out.Err = fmt.Sprintf("telnyx messaging: DLR status %q for %s", status, id)
	}
	// The slot STAYS in the map: the waiter polls Outcome(id) by lookup,
	// so removing the entry would hide the resolution. Resolution is
	// marked on the slot instead; duplicate DLRs for the same id resolve
	// as unknown below. The map stays bounded via RegisterPending
	// eviction and Len counts only unresolved entries.
	out.done = true
	return nil
}

// done reports whether the slot was resolved (used to count only
// unresolved entries and to reject duplicate DLRs).
func (o *DLROutcome) isDone() bool { return o.done }

// OutcomeLocked snapshots an outcome slot under the lock (no I/O).
func (p *pendingDLR) Outcome(id string) (DLROutcome, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out, ok := p.pending[id]
	if !ok {
		return DLROutcome{}, false
	}
	return *out, true
}

// Len reports the number of entries still awaiting a DLR.
func (p *pendingDLR) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, out := range p.pending {
		if !out.done {
			n++
		}
	}
	return n
}

// sleepCtx sleeps d, returning early on ctx cancellation.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// SMSAttemptConfig sizes the adapter. WaitSeconds is the bounded DLR
// wait (config dlr_wait_seconds, default DefaultDLRWaitSeconds); Clock
// is injectable for tests (nil = time.Now). WebhookURL is the
// per-message webhook_url sent on every SendSMS2 (derived by the daemon
// wiring from [voice.messaging].webhook_base_url; empty = omit the key,
// keeping the account-level default).
type SMSAttemptConfig struct {
	FromNumber  string           // E.164 sender
	ProfileID   string           // Telnyx Messaging Profile id
	WaitSeconds int              // bounded DLR wait; <= 0 = DefaultDLRWaitSeconds
	Clock       func() time.Time // injectable clock (tests); nil = time.Now
	WebhookURL  string           // per-message webhook_url ("" = omit)
}

// SMSAttempt implements contact.ChannelAttempt for the telnyx_sms
// channel. It owns the pending DLR map: wire its OnFinalized into the
// messaging webhook receiver's OnFinalized seam.
type SMSAttempt struct {
	client  *Client
	pending *pendingDLR
	cfg     SMSAttemptConfig
	log     *slog.Logger
}

// Compile-time seam check: SMSAttempt is a contact.ChannelAttempt.
var _ contact.ChannelAttempt = (*SMSAttempt)(nil)

// NewSMSAttempt builds the adapter over an existing messaging Client
// (the daemon wiring constructs the client with the broker-resolved API
// key; the adapter never touches secrets).
func NewSMSAttempt(client *Client, cfg SMSAttemptConfig, logger *slog.Logger) *SMSAttempt {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.WaitSeconds <= 0 {
		cfg.WaitSeconds = DefaultDLRWaitSeconds
	}
	return &SMSAttempt{
		client:  client,
		pending: newPendingDLR(),
		cfg:     cfg,
		log:     logger,
	}
}

// OnFinalized feeds a message.finalized DLR into the pending map. The
// webhook receiver's OnFinalized seam calls this; an unknown id is a
// logged debug event (late DLR, eviction, or foreign message), never an
// error surfaced to Telnyx.
func (s *SMSAttempt) OnFinalized(msg Message) error {
	if err := s.pending.ResolveFinalized(msg.ID, msg.Status); err != nil {
		s.log.Debug("telnyx sms attempt: unresolved DLR",
			"message_id", msg.ID, "status", msg.Status, "error", err)
	}
	return nil
}

// Outcome reports the recorded DLR outcome for a message id (empty,
// false while still pending or unknown).
func (s *SMSAttempt) Outcome(id string) (DLROutcome, bool) {
	return s.pending.Outcome(id)
}

// DisabledSMSAttempt is the wired adapter used when
// [voice].messaging.enabled is false: every Attempt returns
// ("unavailable", ErrMessagingDisabled) BEFORE any HTTP call (T5 gate).
// It still satisfies contact.ChannelAttempt so the chain can register
// it and report the refusal honestly.
type DisabledSMSAttempt struct{}

var _ contact.ChannelAttempt = DisabledSMSAttempt{}

// Channel implements contact.ChannelAttempt (C2).
func (DisabledSMSAttempt) Channel() contact.Channel { return contact.ChannelTelnyxSMS }

// Attempt refuses before any HTTP call (ErrMessagingDisabled).
func (DisabledSMSAttempt) Attempt(context.Context, contact.Record, string) (string, error) {
	return contact.OutcomeUnavailable, ErrMessagingDisabled
}

// Channel implements contact.ChannelAttempt (C2).
func (s *SMSAttempt) Channel() contact.Channel { return contact.ChannelTelnyxSMS }

// Attempt sends the message to rec.Phone and waits (bounded) for the
// terminal DLR: delivered -> ("delivered", nil); failed -> ("failed",
// err); timeout -> ("unavailable", err). The context bounds the wait
// too: a cancelled context returns unavailable with the cancel reason.
//
// Consent guard (plan D5): a record with MessagingOptOut set is refused
// with contact.ErrOptedOut BEFORE any HTTP call, same posture as
// DisabledSMSAttempt's ErrMessagingDisabled refusal. Why the rejection
// is terminal-not-retryable: opt-out is deterministic consent state, not
// a transient transport failure — re-attempting the same contact on the
// messaging leg can never succeed until a START reply clears the flag
// (the consent glue re-Puts the record). No provider call is placed, so
// no idempotency key is consumed and no DLR slot is opened. The chain
// consumes it exactly like the other named failure outcomes: the
// attempt row records ("unavailable", err) with errors.Is(err,
// contact.ErrOptedOut) readable in the persisted err string, and the
// chain may still advance to NON-messaging channels (D5 defers the voice
// consult; only send legs are guarded today).
//
// Send path: SendSMS2 (hardened). IdempotencyKey is left empty so the
// client mints a fresh uuid per call: the contact chain's per-attempt
// effect key is computed AFTER Attempt returns (chain.go writeAttemptRow)
// and the frozen C2 ChannelAttempt signature carries no reservation key,
// so the fresh-key-per-logical-send posture is the correct mapping here.
// WebhookURL comes from config (webhook_base_url-derived); empty omits
// the body key.
func (s *SMSAttempt) Attempt(ctx context.Context, rec contact.Record, message string) (string, error) {
	if rec.MessagingOptOut {
		// Refusal BEFORE any HTTP call (D5) — see the terminal-not-
		// retryable rationale on Attempt.
		return contact.OutcomeUnavailable,
			fmt.Errorf("telnyx sms attempt: contact %s messaging opted out: %w", rec.ID, contact.ErrOptedOut)
	}
	from := s.cfg.FromNumber
	if !ValidE164(from) || !ValidE164(rec.Phone) {
		return contact.OutcomeUnavailable, fmt.Errorf("telnyx sms attempt: bad number (from %q, to %q): %w", from, rec.Phone, ErrInvalidE164)
	}
	if s.client == nil {
		return contact.OutcomeUnavailable, errors.New("telnyx sms attempt: nil messaging client")
	}

	result, err := s.client.SendSMS2(ctx, SendRequest{
		To:         rec.Phone,
		Text:       message,
		WebhookURL: s.cfg.WebhookURL,
	})
	if err != nil {
		// Send failure (network, carrier 4xx/5xx): the message may or may
		// not be in flight. Report failed (a concrete, per-attempt error
		// the chain reports honestly) rather than unavailable.
		return contact.OutcomeFailed, fmt.Errorf("telnyx sms attempt: send: %w", err)
	}
	id := result.ProviderMessageID

	// DLR wait. The outcome slot is registered lazily AFTER send returns
	// the id (no optimistic registration to undo on send failure); the
	// pointer is unused here but documents the slot the async DLR fills
	// (readable via Outcome(id)).
	_ = s.pending.RegisterPending(id)

	wait := time.Duration(s.cfg.WaitSeconds) * time.Second
	deadline := s.now().Add(wait)
	for {
		// Snapshot under the lock (Outcome takes it internally);
		// decisions + logging happen outside it.
		outcome, ok := s.pending.Outcome(id)
		if ok && outcome.Resolved {
			if outcome.Status == "delivered" {
				return contact.OutcomeDelivered, nil
			}
			return contact.OutcomeFailed, errors.New(outcome.Err)
		}
		if !s.now().Before(deadline) {
			// Bounded wait expired: the chain falls through on
			// unavailable. The slot STAYS registered, so the async DLR
			// still updates it for observability (readable via Outcome);
			// no goroutine was spawned to wait.
			s.log.Info("telnyx sms attempt: DLR wait expired",
				"message_id", id, "wait_seconds", s.cfg.WaitSeconds)
			return contact.OutcomeUnavailable,
				fmt.Errorf("telnyx messaging: no message.finalized for %s within %ds", id, s.cfg.WaitSeconds)
		}
		if cerr := sleepCtx(ctx, dlrPollInterval); cerr != nil {
			return contact.OutcomeUnavailable, fmt.Errorf("telnyx sms attempt: wait cancelled: %w", cerr)
		}
	}
}

// now reads the injectable clock (nil = time.Now).
func (s *SMSAttempt) now() time.Time {
	if s.cfg.Clock != nil {
		return s.cfg.Clock()
	}
	return time.Now()
}
