package signalleg

// Signal messaging channel (contact-channels plan, leaf 11b, contract C2
// attempt implementation): the signal_msg contact.ChannelAttempt over the
// C3 Client.Send seam. Abuse posture (research archive): throttle outbound
// per account, treat privacy-setting rejections as channel-unavailable -
// never a daemon error; the contact chain falls through on "unavailable".
//
// Attempt order (pinned): allowlist check -> token-bucket throttle ->
// Client.Send(rec = rec.SignalUUID fallback rec.Phone, message).
// Outcome mapping: ts>0 = "delivered" (nil err); rate-limit /
// privacy-rejection / unconfirmed (ts==0) = "unavailable" (nil err, the
// chain falls through); transport-level hard failure = "failed" with the
// error surfaced.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/contact"
	"github.com/caimlas/meept/internal/signal"
)

// defaultMsgPerMinute is the [signal].msg_per_minute config default:
// three outbound messages per minute per Signal account.
const defaultMsgPerMinute = 3

// ErrRecipientNotAllowedlisted is the named refusal for a contact whose
// resolved Signal recipient is not on the configured allowlist. Policy
// refusals surface as ("failed", err) - they are configuration problems,
// not channel unavailability.
var ErrRecipientNotAllowedlisted = errors.New("signalleg: recipient not allowlisted")

// sendSeam is the Send surface msgchannel consumes (the concrete
// *signal.Client satisfies it; tests fake it). Kept local so the channel
// does not re-declare leaf 11a's seams.
type sendSeam interface {
	Send(recipient string, message string, attachments []string) (ts int64, err error)
}

// Compile-time pin: the C3 client implements the send seam.
var _ sendSeam = (*signal.Client)(nil)

// clockSeam abstracts time for the throttle (tests inject a fake clock).
type clockSeam interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// realClock is the production clock.
type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// msgThrottle is a per-account token bucket: limit tokens, refill one
// token every minute/limit. Zero/negative limit clamps to
// defaultMsgPerMinute.
type msgThrottle struct {
	limit  int
	clock  clockSeam
	window time.Duration

	mu       sync.Mutex
	tokens   int
	lastFill time.Time
}

func newMsgThrottle(perMinute int, clock clockSeam) *msgThrottle {
	limit := perMinute
	if limit <= 0 {
		limit = defaultMsgPerMinute
	}
	now := clock.Now()
	return &msgThrottle{
		limit:    limit,
		clock:    clock,
		window:   time.Minute,
		tokens:   limit,
		lastFill: now,
	}
}

// refill brings the bucket up to date for the elapsed time (one token per
// window/limit, capped at limit). Caller holds mu.
func (t *msgThrottle) refill(now time.Time) {
	if now.Before(t.lastFill) {
		return // clock went backwards; do not grant tokens for it
	}
	per := t.window / time.Duration(t.limit)
	if per <= 0 {
		per = time.Millisecond
	}
	elapsed := now.Sub(t.lastFill)
	t.tokens += int(elapsed / per)
	if t.tokens > t.limit {
		t.tokens = t.limit
	}
	t.lastFill = t.lastFill.Add(elapsed / per * per)
}

// acquire consumes one token or returns the wait until one refills.
func (t *msgThrottle) acquire() (wait time.Duration, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.clock.Now()
	t.refill(now)
	if t.tokens > 0 {
		t.tokens--
		return 0, true
	}
	per := t.window / time.Duration(t.limit)
	if per <= 0 {
		per = time.Millisecond
	}
	return per, false
}

// msgChannel is the signal_msg ChannelAttempt. It is the concrete type
// behind (*Leg).MessageAttempt - the Leg owns the shared seams (client,
// allowlist, logger); this type carries only the throttle state.
type msgChannel struct {
	client    sendSeam
	allowlist func(uuid string) bool
	throttle  *msgThrottle
	log       *slog.Logger
}

// newMsgChannel assembles the channel (deps resolved by the Leg).
func newMsgChannel(client sendSeam, allowlist func(string) bool, perMinute int, clock clockSeam, log *slog.Logger) *msgChannel {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &msgChannel{
		client:    client,
		allowlist: allowlist,
		throttle:  newMsgThrottle(perMinute, clock),
		log:       log,
	}
}

// Channel pins the seam key (C1 vocabulary).
func (m *msgChannel) Channel() contact.Channel { return contact.ChannelSignalMsg }

// signalRecipient resolves the outbound recipient: SignalUUID with a
// phone fallback (leaf doc: "rec.SignalUUID fallback rec.Phone").
func signalRecipient(rec contact.Record) string {
	if rec.SignalUUID != "" {
		return rec.SignalUUID
	}
	return rec.Phone
}

// classifySendError maps a Send error to the C2 outcome vocabulary:
// rate-limit and privacy/registration rejections are channel-unavailable
// (the chain falls through); everything else is a hard failure.
func classifySendError(err error) (outcome string, unavailable bool) {
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"429",
		"rate limit",
		"too many requests",
		"unregistered",
		"privacy",
		"not registered",
		"blocked",
	} {
		if strings.Contains(msg, marker) {
			return contact.OutcomeUnavailable, true
		}
	}
	return contact.OutcomeFailed, false
}

// Attempt implements contact.ChannelAttempt (C2: message channels are
// synchronous; (outcome, nil) = delivered, (outcome, err != nil) =
// non-stop failure with the named outcome).
func (m *msgChannel) Attempt(ctx context.Context, rec contact.Record, message string) (string, error) {
	recipient := signalRecipient(rec)

	// 1. Allowlist (before the throttle: a refusal consumes no token).
	if m.allowlist != nil && !m.allowlist(recipient) {
		m.log.Info("signal msg: recipient not allowlisted", "contact_id", rec.ID)
		return contact.OutcomeFailed, fmt.Errorf("%w: %s", ErrRecipientNotAllowedlisted, recipient)
	}

	// 2. Token-bucket throttle (per account).
	wait, ok := m.throttle.acquire()
	if !ok {
		m.log.Info("signal msg: throttled, waiting for bucket refill", "wait_ms", wait.Milliseconds())
		timer := m.throttle.clock.After(wait)
		select {
		case <-ctx.Done():
			return contact.OutcomeFailed, ctx.Err()
		case <-timer:
		}
	}

	// 3. Send (typing indicator omitted: optional polish, not pinned).
	ts, err := m.client.Send(recipient, message, nil)
	if err != nil {
		outcome, unavailable := classifySendError(err)
		if unavailable {
			// Privacy-setting / rate-limit rejections: channel
			// unavailable, NEVER a daemon error - the chain falls
			// through. No error surfaces.
			m.log.Info("signal msg: send unavailable", "contact_id", rec.ID, "outcome", outcome)
			return outcome, nil
		}
		return outcome, fmt.Errorf("signal msg: send to %s: %w", recipient, err)
	}
	if ts <= 0 {
		// No timestamp back = no delivery confirmation from
		// signal-cli; treat as unconfirmed -> unavailable.
		m.log.Info("signal msg: send returned no timestamp", "contact_id", rec.ID)
		return contact.OutcomeUnavailable, nil
	}
	return contact.OutcomeDelivered, nil
}
