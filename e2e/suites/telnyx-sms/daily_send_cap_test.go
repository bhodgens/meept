//go:build e2e

// Issue #165 follow-up (durable daily caps), telnyx-sms hermetic gate.
// Pins restart persistence for the outbound SMS day cap with NO live
// network: a stub Telnyx /v2/messages counts every POST; a fresh telnyx
// client built over the SAME durable counter file refuses the next send
// at the cap. The old in-process perDayCount would reopen the cap.
package telnyxsms

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/telnyx"
	"github.com/caimlas/meept/internal/voice"
)

// capStub is a minimal /v2/messages stub: every hit is counted and a canned
// accepted envelope is returned. The hit count is the "before any POST"
// assertion surface.
type capStub struct {
	hits  int
	clock time.Time
}

func (s *capStub) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.hits++
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"data":{"id":"cap-msg-1","status":"queued"}}`))
}

// newCappedClient builds a client with maxPerDay=1 bound to the shared
// durable counter. The clock is fixed so the UTC day key is deterministic.
func newCappedClient(t *testing.T, stub *capStub, counter telnyx.DailySendCounter) (*telnyx.Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	var logs bytes.Buffer
	c, err := telnyx.NewClient(srv.URL,
		telnyx.WithAPIKey("test-key"),
		telnyx.WithMessagingClock(func() time.Time { return stub.clock }),
		telnyx.WithMessagingLogger(slog.New(slog.NewTextHandler(&logs, nil))),
		telnyx.WithAllowedNumbers([]string{telnyxCapTo}),
		telnyx.WithSendCaps(0, 1), // per-day cap 1, per-call unlimited
		telnyx.WithDailySendCounter(counter),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, srv
}

const telnyxCapTo = "+15551230002"

func sendCap(t *testing.T, c *telnyx.Client) error {
	t.Helper()
	_, err := c.SendSMS2(context.Background(), telnyx.SendRequest{To: telnyxCapTo, Text: "cap test"})
	return err
}

// TestDailySMSCapPersistsAcrossRestart pins that a fresh client over the
// SAME counter file cannot send again after the day cap is spent. First
// client sends once (admitted); a second, freshly constructed client with
// an in-process-only counter would admit another POST — with durable state
// it must refuse with ErrSendCapExceeded and zero new POSTs.
func TestDailySMSCapPersistsAcrossRestart(t *testing.T) {
	counterPath := filepath.Join(t.TempDir(), "sms", "daily_counters.json")
	stub := &capStub{clock: time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)}

	// First "daemon": one admitted send spends the day allowance.
	counter1 := voice.NewDailyCounter(counterPath)
	client1, _ := newCappedClient(t, stub, counter1)
	if err := sendCap(t, client1); err != nil {
		t.Fatalf("first send: %v", err)
	}
	if stub.hits != 1 {
		t.Fatalf("stub POSTs after first send = %d, want 1", stub.hits)
	}

	// Fresh counter + client over the same file (simulated restart).
	counter2 := voice.NewDailyCounter(counterPath)
	client2, _ := newCappedClient(t, stub, counter2)
	err := sendCap(t, client2)
	if !errors.Is(err, telnyx.ErrSendCapExceeded) {
		t.Fatalf("post-restart send error = %v, want errors.Is(ErrSendCapExceeded)", err)
	}
	if stub.hits != 1 {
		t.Errorf("stub POSTs after capped post-restart send = %d, want 1 (cap before POST)", stub.hits)
	}

	// The counter file is 0600 and durable.
	info, statErr := os.Stat(counterPath)
	if statErr != nil {
		t.Fatalf("stat counter file: %v", statErr)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("counter file perms = %o, want 600", perm)
	}
}

// TestDailySMSCapDayRolloverResetsDurableCounter pins that a new UTC day
// gets a fresh allowance even across a simulated restart, using the
// injected clock (no sleeping).
func TestDailySMSCapDayRolloverResetsDurableCounter(t *testing.T) {
	counterPath := filepath.Join(t.TempDir(), "sms", "daily_counters.json")
	stub := &capStub{clock: time.Date(2026, 11, 22, 23, 59, 0, 0, time.UTC)}
	counter := voice.NewDailyCounter(counterPath)
	client, _ := newCappedClient(t, stub, counter)

	if err := sendCap(t, client); err != nil {
		t.Fatalf("send on day 1: %v", err)
	}
	if err := sendCap(t, client); !errors.Is(err, telnyx.ErrSendCapExceeded) {
		t.Fatalf("second send day 1 error = %v, want ErrSendCapExceeded", err)
	}
	if stub.hits != 1 {
		t.Fatalf("stub POSTs day 1 = %d, want 1", stub.hits)
	}

	// Cross UTC midnight via the injected clock; the counter resets.
	stub.clock = stub.clock.Add(2 * time.Minute)
	if err := sendCap(t, client); err != nil {
		t.Fatalf("send on day 2 after rollover: %v", err)
	}
	if stub.hits != 2 {
		t.Errorf("stub POSTs after rollover = %d, want 2 (counter reset)", stub.hits)
	}
}
