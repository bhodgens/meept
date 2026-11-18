package signalleg

// Tests for the signal MESSAGING channel (contact-channels plan, leaf 11b,
// contract C2 attempt implementation). They pin:
//
//   - Attempt order: allowlist check -> token-bucket throttle -> Send.
//   - Outcome mapping: ts>0 = "delivered"; rate-limit / privacy-setting
//     rejections = "unavailable" with nil err (the chain falls through,
//     never a daemon error); transport-level hard failure = "failed" with
//     the error surfaced.
//   - Recipient: rec.SignalUUID with fallback to rec.Phone.
//   - Throttle: default 3 messages/minute per account; the 4th immediate
//     send waits until the fake clock advances (injectable clock).

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/contact"
)

// fakeSender scripts Send results and records what the channel sent.
type fakeSender struct {
	mu      sync.Mutex
	recips  []string
	bodies  []string
	respond func(recipient, body string) (int64, error)
}

func (f *fakeSender) Send(recipient, message string, attachments []string) (int64, error) {
	f.mu.Lock()
	f.recips = append(f.recips, recipient)
	f.bodies = append(f.bodies, message)
	respond := f.respond
	f.mu.Unlock()
	if respond == nil {
		return 1731000000123, nil
	}
	return respond(recipient, message)
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.recips)
}

func (f *fakeSender) lastRecipient() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.recips) == 0 {
		return ""
	}
	return f.recips[len(f.recips)-1]
}

// fakeClock is the injectable clock for throttle tests: After returns a
// channel that fires only when the test advances time.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	pending []fakeTimer
}

type fakeTimer struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(1731000000, 0).UTC()} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.pending = append(c.pending, fakeTimer{at: c.now.Add(d), ch: ch})
	return ch
}

// Advance moves the clock forward and fires every timer that is due.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	now := c.now.Add(d)
	c.now = now
	var rest []fakeTimer
	for _, t := range c.pending {
		if !t.at.After(now) {
			t.ch <- now
		} else {
			rest = append(rest, t)
		}
	}
	c.pending = rest
	c.mu.Unlock()
}

func testRecord() contact.Record {
	return contact.Record{
		ID:          "c-sig",
		DisplayName: "Signal Contact",
		Phone:       "+15550001111",
		SignalUUID:  "aaaa1111-2222-3333-4444-555555555555",
	}
}

func newTestChannel(resp func(recipient, body string) (int64, error), clock clockSeam) *msgChannel {
	sender := &fakeSender{respond: resp}
	return newMsgChannel(sender, nil, defaultMsgPerMinute, clock, discardLogger())
}

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// TestMsgChannelDelivered pins the happy path: a successful Send with
// ts>0 maps to ("delivered", nil) and the recipient is the Signal UUID.
func TestMsgChannelDelivered(t *testing.T) {
	mc := newTestChannel(nil, realClock{})

	outcome, err := mc.Attempt(context.Background(), testRecord(), "dentist reminder")
	if err != nil {
		t.Fatalf("Attempt: %v (delivered carries nil err, C2)", err)
	}
	if outcome != contact.OutcomeDelivered {
		t.Errorf("outcome = %q, want %q", outcome, contact.OutcomeDelivered)
	}
	if got := mc.client.(*fakeSender).lastRecipient(); got != testRecord().SignalUUID {
		t.Errorf("recipient = %q, want the Signal UUID %q", got, testRecord().SignalUUID)
	}
}

// TestMsgChannelRateLimitedUnavailable pins the leaf's canonical mapping:
// a rate-limited send returns ("unavailable", nil err) - the chain falls
// through, never a daemon error.
func TestMsgChannelRateLimitedUnavailable(t *testing.T) {
	mc := newTestChannel(func(string, string) (int64, error) {
		return 0, errors.New("send failed: 429 too many requests")
	}, realClock{})

	outcome, err := mc.Attempt(context.Background(), testRecord(), "m")
	if err != nil {
		t.Fatalf("Attempt err = %v, want nil (rate-limit is unavailable, chain falls through)", err)
	}
	if outcome != contact.OutcomeUnavailable {
		t.Errorf("outcome = %q, want %q", outcome, contact.OutcomeUnavailable)
	}
}

// TestMsgChannelPrivacyRejectionUnavailable pins the abuse-posture rule:
// privacy-setting rejections arrive as send failures and map to
// "unavailable", NOT a daemon error.
func TestMsgChannelPrivacyRejectionUnavailable(t *testing.T) {
	for _, msg := range []string{
		"send failed: UnregisteredFailure: user is not registered",
		"send failed: recipient privacy settings block delivery",
	} {
		mc := newTestChannel(func(string, string) (int64, error) {
			return 0, errors.New(msg)
		}, realClock{})
		outcome, err := mc.Attempt(context.Background(), testRecord(), "m")
		if err != nil {
			t.Errorf("%q: Attempt err = %v, want nil (privacy rejection is channel-unavailable)", msg, err)
		}
		if outcome != contact.OutcomeUnavailable {
			t.Errorf("%q: outcome = %q, want %q", msg, outcome, contact.OutcomeUnavailable)
		}
	}
}

// TestMsgChannelHardFailureFailed pins the hard-failure mapping: a
// transport-level send error is ("failed", err != nil).
func TestMsgChannelHardFailureFailed(t *testing.T) {
	mc := newTestChannel(func(string, string) (int64, error) {
		return 0, errors.New("json-rpc: connection lost")
	}, realClock{})

	outcome, err := mc.Attempt(context.Background(), testRecord(), "m")
	if err == nil {
		t.Fatalf("Attempt err = nil, want the hard failure surfaced")
	}
	if outcome != contact.OutcomeFailed {
		t.Errorf("outcome = %q, want %q", outcome, contact.OutcomeFailed)
	}
}

// TestMsgChannelTsZeroUnavailable pins the no-confirmation rule: Send
// returning ts==0 without an error is NOT a confirmed delivery ->
// "unavailable".
func TestMsgChannelTsZeroUnavailable(t *testing.T) {
	mc := newTestChannel(func(string, string) (int64, error) {
		return 0, nil
	}, realClock{})

	outcome, err := mc.Attempt(context.Background(), testRecord(), "m")
	if err != nil {
		t.Fatalf("Attempt err = %v, want nil", err)
	}
	if outcome != contact.OutcomeUnavailable {
		t.Errorf("outcome = %q, want %q (ts==0 is not a delivery confirmation)", outcome, contact.OutcomeUnavailable)
	}
}

// TestMsgChannelAllowlistRefused pins gate order: the allowlist runs
// BEFORE the throttle and the send; a refused recipient consumes no token
// and invokes no send.
func TestMsgChannelAllowlistRefused(t *testing.T) {
	sender := &fakeSender{}
	mc := newMsgChannel(sender, func(uuid string) bool { return false }, defaultMsgPerMinute, realClock{}, discardLogger())

	outcome, err := mc.Attempt(context.Background(), testRecord(), "m")
	if !errors.Is(err, ErrRecipientNotAllowedlisted) {
		t.Fatalf("Attempt err = %v, want ErrRecipientNotAllowedlisted", err)
	}
	if outcome != contact.OutcomeFailed {
		t.Errorf("outcome = %q, want %q (policy refusal is not channel unavailability)", outcome, contact.OutcomeFailed)
	}
	if sender.count() != 0 {
		t.Errorf("allowlist-refused attempt issued %d sends, want 0", sender.count())
	}
}

// TestMsgChannelFallsBackToPhone pins the recipient rule: an empty
// SignalUUID falls back to rec.Phone.
func TestMsgChannelFallsBackToPhone(t *testing.T) {
	mc := newTestChannel(nil, realClock{})
	rec := testRecord()
	rec.SignalUUID = ""

	if _, err := mc.Attempt(context.Background(), rec, "m"); err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if got := mc.client.(*fakeSender).lastRecipient(); got != rec.Phone {
		t.Errorf("recipient = %q, want phone fallback %q", got, rec.Phone)
	}
}

// TestMsgChannelChannelConstant pins the seam key.
func TestMsgChannelChannelConstant(t *testing.T) {
	mc := newTestChannel(nil, realClock{})
	if mc.Channel() != contact.ChannelSignalMsg {
		t.Errorf("Channel() = %q, want %q", mc.Channel(), contact.ChannelSignalMsg)
	}
}

// TestMsgChannelThrottleFourthWaits pins T2's canonical Given/When/Then:
// three immediate sends pass; the 4th blocks until the fake clock
// advances a minute, then delivers.
func TestMsgChannelThrottleFourthWaits(t *testing.T) {
	clock := newFakeClock()
	mc := newTestChannel(nil, clock)
	rec := testRecord()
	ctx := context.Background()

	for i := range defaultMsgPerMinute {
		outcome, err := mc.Attempt(ctx, rec, "burst")
		if err != nil || outcome != contact.OutcomeDelivered {
			t.Fatalf("send %d: outcome %q err %v, want delivered/nil", i+1, outcome, err)
		}
	}

	// The 4th must NOT deliver immediately: it waits on the bucket.
	done := make(chan string, 1)
	go func() {
		outcome, _ := mc.Attempt(ctx, rec, "fourth")
		done <- outcome
	}()

	deadline := time.After(300 * time.Millisecond)
	select {
	case outcome := <-done:
		t.Fatalf("4th send delivered immediately with outcome %q (throttle missing)", outcome)
	case <-deadline:
	}

	// A different recipient does NOT bypass the per-account bucket.
	sender := mc.client.(*fakeSender)
	other := rec
	other.SignalUUID = "bbbb2222-2222-3333-4444-555555555555"
	clock.Advance(time.Minute) // the bucket refills
	select {
	case outcome := <-done:
		if outcome != contact.OutcomeDelivered {
			t.Fatalf("4th send outcome = %q after refill, want delivered", outcome)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("4th send never completed after clock refill")
	}
	if sender.count() != 4 {
		t.Errorf("sender saw %d sends, want 4", sender.count())
	}
}

// TestMsgChannelThrottleContextCancel pins the cancellation posture: a
// throttled Attempt aborts with a context error instead of delivering.
func TestMsgChannelThrottleContextCancel(t *testing.T) {
	clock := newFakeClock()
	mc := newTestChannel(nil, clock)
	rec := testRecord()

	for range defaultMsgPerMinute {
		_, _ = mc.Attempt(context.Background(), rec, "burst") //nolint:errcheck // seeded bursts
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := mc.Attempt(ctx, rec, "fourth")
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("throttled Attempt err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("cancel did not abort the throttled attempt")
	}
	if mc.client.(*fakeSender).count() != defaultMsgPerMinute {
		t.Errorf("sender saw %d sends, want %d (cancelled attempt must not send)", mc.client.(*fakeSender).count(), defaultMsgPerMinute)
	}
}

// TestDefaultMsgPerMinute pins the config default: a throttle configured
// with <=0 clamps to the [signal].msg_per_minute default of 3.
func TestDefaultMsgPerMinute(t *testing.T) {
	if defaultMsgPerMinute != 3 {
		t.Fatalf("defaultMsgPerMinute = %d, want 3 ([signal].msg_per_minute default)", defaultMsgPerMinute)
	}
	th := newMsgThrottle(0, newFakeClock())
	if th.limit != defaultMsgPerMinute {
		t.Errorf("throttle limit = %d, want the clamped default %d", th.limit, defaultMsgPerMinute)
	}
}

// TestMsgChannelZeroConfigClamps pins the wiring shortcut: a channel
// built with perMinute 0 still throttles at the default.
func TestMsgChannelZeroConfigClamps(t *testing.T) {
	clock := newFakeClock()
	sender := &fakeSender{}
	mc := newMsgChannel(sender, nil, 0, clock, discardLogger())
	rec := testRecord()

	for i := range defaultMsgPerMinute {
		if _, err := mc.Attempt(context.Background(), rec, "burst"); err != nil {
			t.Fatalf("send %d: %v", i+1, err)
		}
	}
	done := make(chan struct{}, 1)
	go func() {
		_, _ = mc.Attempt(context.Background(), rec, "fourth") //nolint:errcheck // throttled path
		done <- struct{}{}
	}()
	select {
	case <-done:
		t.Fatalf("4th send passed a zero-config throttle (clamp to default 3 failed)")
	case <-time.After(200 * time.Millisecond):
	}
}
