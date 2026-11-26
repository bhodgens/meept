//go:build e2e

// Suite telnyx-sms (issue #165 slices 1-2, F-03): outbound SMS recipient
// allowlist + per-call/per-day caps. Hermetic: a stub Telnyx messaging
// API counts every POST through the real telnyx.Client.SendSMS2 path;
// a refusal with zero POSTs proves pre-POST enforcement. The clock is
// injected through the client's existing WithMessagingClock seam so UTC
// date rollover is deterministic without sleeping.
package telnyxsms

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/telnyx"
)

// egressStub is the minimal Telnyx /v2/messages stub: every hit is
// counted, and a canned data.id/status envelope is returned. The hit
// count is the assertion surface for "refused BEFORE any HTTP POST".
type egressStub struct {
	mu    sync.Mutex
	hits  int
	last  string
	clock time.Time // shared with the injected client clock below
}

func (s *egressStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.hits++
	s.last = r.URL.Path
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"data":{"id":"out-msg-1","status":"queued"}}`))
}

func (s *egressStub) hitCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits
}

// newEgressClient builds a real telnyx.Client against the stub with an
// injected clock and logger capture. All egress options are passed
// through so each test picks its own allowlist/caps.
func newEgressClient(t *testing.T, stub *egressStub, logs *bytes.Buffer, opts ...telnyx.Option) *telnyx.Client {
	t.Helper()
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	base := []telnyx.Option{
		telnyx.WithAPIKey("test-key"),
		telnyx.WithMessagingClock(func() time.Time { return stub.clock }),
		telnyx.WithMessagingLogger(slog.New(slog.NewTextHandler(logs, nil))),
	}
	base = append(base, opts...)
	c, err := telnyx.NewClient(srv.URL, base...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func mustSend(t *testing.T, c *telnyx.Client, to string) telnyx.SendResult {
	t.Helper()
	res, err := c.SendSMS2(context.Background(), telnyx.SendRequest{To: to, Text: "hello"})
	if err != nil {
		t.Fatalf("SendSMS2(%s): %v", to, err)
	}
	return res
}

// telnyx-sms-04: a recipient outside a non-empty allowlist is refused
// with ErrRecipientNotAllowed, and NO POST reaches the stub. An
// in-list recipient goes through. Also pins the warning silence when a
// full allowlist + both caps are configured.
func TestTelnyxSMSOutboundAllowlistRefusalPrePOST(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	stub := &egressStub{clock: time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)}
	var logs bytes.Buffer
	client := newEgressClient(t, stub, &logs,
		telnyx.WithAllowedNumbers([]string{"+15551230001"}),
		telnyx.WithSendCaps(10, 100),
	)

	// Full config => no construction warning.
	if strings.Contains(logs.String(), telnyx.NoRecipientAllowlistWarning) {
		t.Fatalf("unexpected security warning with full allowlist+caps: %s", logs.String())
	}

	// Out-of-list: fail closed, no POST, sentinel.
	res, err := client.SendSMS2(context.Background(), telnyx.SendRequest{To: "+15551239999", Text: "bad"})
	if !errors.Is(err, telnyx.ErrRecipientNotAllowed) {
		t.Fatalf("out-of-list err = %v, want errors.Is(ErrRecipientNotAllowed)", err)
	}
	if res != (telnyx.SendResult{}) {
		t.Fatalf("out-of-list result = %+v, want zero value", res)
	}
	if got := stub.hitCount(); got != 0 {
		t.Fatalf("stub POSTs after allowlist refusal = %d, want 0 (pre-POST gate)", got)
	}

	// In-list: admitted, one POST, real provider id parsed.
	got := mustSend(t, client, "+15551230001")
	if got.ProviderMessageID != "out-msg-1" {
		t.Fatalf("in-list result = %+v, want provider id out-msg-1", got)
	}
	if hits := stub.hitCount(); hits != 1 {
		t.Fatalf("stub POSTs after admitted send = %d, want 1", hits)
	}
}

// telnyx-sms-05: an empty allowlist and unset caps keep the pre-#165
// behavior (any E.164 accepted) but emit the frozen security warning
// exactly once at construction.
func TestTelnyxSMSOutboundUnsetConfigWarning(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	stub := &egressStub{clock: time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)}
	var logs bytes.Buffer
	client := newEgressClient(t, stub, &logs)

	// The exact literal must appear, once, in the captured slog output.
	if n := strings.Count(logs.String(), telnyx.NoRecipientAllowlistWarning); n != 1 {
		t.Fatalf("security warning emitted %d times, want 1: %s", n, logs.String())
	}
	// M14 fail-closed flip (was: backward-compatible sends to any E.164):
	// an unset allowlist now REFUSES every recipient, before any POST.
	if _, err := client.SendSMS2(context.Background(), telnyx.SendRequest{To: "+15551230001", Text: "hello"}); !errors.Is(err, telnyx.ErrRecipientNotAllowed) {
		t.Fatalf("unset-allowlist send err = %v, want errors.Is(ErrRecipientNotAllowed) (fail-closed, M14)", err)
	}
	if _, err := client.SendSMS2(context.Background(), telnyx.SendRequest{To: "+15551230001", Text: "hello"}); !errors.Is(err, telnyx.ErrRecipientNotAllowed) {
		t.Fatalf("unset-allowlist send err = %v, want ErrRecipientNotAllowed (fail-closed, M14)", err)
	}
	if hits := stub.hitCount(); hits != 0 {
		t.Fatalf("stub POSTs with unset config = %d, want 0 (fail-closed, M14)", hits)
	}
	if n := strings.Count(logs.String(), telnyx.NoRecipientAllowlistWarning); n != 1 {
		t.Fatalf("security warning emitted %d times after sends, want 1 (construction only)", n)
	}
}

// telnyx-sms-06: per-call cap trips before the HTTP POST; a refusal
// consumes no budget, so sends up to the cap still succeed and the
// (cap+1)-th is refused with ErrSendCapExceeded.
func TestTelnyxSMSOutboundPerCallCapRefusalPrePOST(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	stub := &egressStub{clock: time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)}
	var logs bytes.Buffer
	client := newEgressClient(t, stub, &logs,
		telnyx.WithAllowedNumbers([]string{"+15551230001"}),
		telnyx.WithSendCaps(2, 100),
	)

	mustSend(t, client, "+15551230001")
	mustSend(t, client, "+15551230001")
	if hits := stub.hitCount(); hits != 2 {
		t.Fatalf("stub POSTs after 2 admitted sends = %d, want 2", hits)
	}
	_, err := client.SendSMS2(context.Background(), telnyx.SendRequest{To: "+15551230001", Text: "over cap"})
	if !errors.Is(err, telnyx.ErrSendCapExceeded) {
		t.Fatalf("over per-call cap err = %v, want errors.Is(ErrSendCapExceeded)", err)
	}
	if hits := stub.hitCount(); hits != 2 {
		t.Fatalf("stub POSTs after cap refusal = %d, want 2 (cap refused before POST)", hits)
	}
}

// telnyx-sms-07: per-day cap is enforced before POST and the counter
// resets on UTC date change (via the injected clock), so a new UTC day
// gets a fresh allowance.
func TestTelnyxSMSOutboundPerDayCapAndUTCReset(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	stub := &egressStub{clock: time.Date(2026, 11, 22, 23, 59, 0, 0, time.UTC)}
	var logs bytes.Buffer
	client := newEgressClient(t, stub, &logs,
		telnyx.WithAllowedNumbers([]string{"+15551230001"}),
		telnyx.WithSendCaps(100, 1),
	)

	mustSend(t, client, "+15551230001")
	_, err := client.SendSMS2(context.Background(), telnyx.SendRequest{To: "+15551230001", Text: "same day"})
	if !errors.Is(err, telnyx.ErrSendCapExceeded) {
		t.Fatalf("same UTC day err = %v, want errors.Is(ErrSendCapExceeded)", err)
	}
	if hits := stub.hitCount(); hits != 1 {
		t.Fatalf("stub POSTs after same-day refusal = %d, want 1", hits)
	}

	// Cross UTC midnight: the day counter resets, a new send is admitted.
	stub.clock = stub.clock.Add(2 * time.Minute)
	mustSend(t, client, "+15551230001")
	if hits := stub.hitCount(); hits != 2 {
		t.Fatalf("stub POSTs after UTC date change = %d, want 2 (counter reset)", hits)
	}
	_, err = client.SendSMS2(context.Background(), telnyx.SendRequest{To: "+15551230001", Text: "again"})
	if !errors.Is(err, telnyx.ErrSendCapExceeded) {
		t.Fatalf("post-reset second send err = %v, want errors.Is(ErrSendCapExceeded)", err)
	}
	if hits := stub.hitCount(); hits != 2 {
		t.Fatalf("stub POSTs after post-reset refusal = %d, want 2", hits)
	}
}
