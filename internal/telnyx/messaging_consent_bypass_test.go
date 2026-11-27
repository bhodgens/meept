package telnyx

// ============================================================================
// M10 fix-wave pins (bughunt 2026-11-27, M10/T-4): the TCPA consent-reply
// send path (SendConsentReply + WithConsentReplyBypass) must reach the
// inbound sender even when the M14 recipient allowlist is EMPTY or does
// not contain the sender — the operator cannot pre-list arbitrary inbound
// numbers. The bypass skips ONLY the allowlist gate:
//   - a regular SendSMS2 on the same bypass-enabled client keeps the full
//     fail-closed M14 posture (empty allowlist still refuses);
//   - SendConsentReply on a client WITHOUT WithConsentReplyBypass refuses
//     identically (the bypass is a construction choice, not per-call);
//   - each actually-bypassed send logs ONE Warn naming the sender + kind;
//   - the volume caps still consume budget on the bypass path.
// ============================================================================

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

// httptestServerForStub wraps the shared send2Stub in an httptest server
// with the usual cleanup.
func httptestServerForStub(t *testing.T, stub *send2Stub) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	return srv
}

// newBypassClient builds a bypass-enabled client against an httptest stub
// with an EMPTY allowlist (the shipped operator default under M14).
func newBypassClient(t *testing.T, srvURL string, logs *bytes.Buffer) *Client {
	t.Helper()
	c, err := NewClient(srvURL,
		WithAPIKey("test-key"),
		WithProfileID(send2Profile),
		WithFromNumber(send2From),
		WithConsentReplyBypass(),
		WithMessagingLogger(slog.New(slog.NewTextHandler(logs, nil))))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// TestM10ConsentReplyEmptyAllowlistDelivers pins the decided semantics:
// an EMPTY allowlist (fail-closed for normal sends) must NOT refuse the
// consent-reply path — zero refusal, the POST reaches the stub.
func TestM10ConsentReplyEmptyAllowlistDelivers(t *testing.T) {
	stub := &send2Stub{}
	srv := httptestServerForStub(t, stub)
	var logs bytes.Buffer
	c := newBypassClient(t, srv.URL, &logs)

	res, err := c.SendConsentReply(context.Background(), validSend2Req(), "stop")
	if err != nil {
		t.Fatalf("SendConsentReply with empty allowlist err = %v; want delivered (M10 bypass)", err)
	}
	if res.ProviderMessageID == "" {
		t.Errorf("consent reply returned empty provider id: %+v", res)
	}
	if hits := stub.hitCount(); hits != 1 {
		t.Fatalf("stub POSTs for consent reply = %d; want 1", hits)
	}
	if !strings.Contains(logs.String(), "consent reply sent outside recipient allowlist") {
		t.Errorf("bypass Warn missing from logs: %s", logs.String())
	}
}

// TestM10NormalSendStillRefusesEmptyAllowlist pins the unchanged M14
// posture: the SAME bypass-enabled client still refuses a REGULAR
// SendSMS2 with an empty allowlist, with zero HTTP POSTs.
func TestM10NormalSendStillRefusesEmptyAllowlist(t *testing.T) {
	stub := &send2Stub{}
	srv := httptestServerForStub(t, stub)
	var logs bytes.Buffer
	c := newBypassClient(t, srv.URL, &logs)

	_, err := c.SendSMS2(context.Background(), validSend2Req())
	if !errors.Is(err, ErrRecipientNotAllowed) {
		t.Fatalf("regular send err = %v; want errors.Is(ErrRecipientNotAllowed) (M14 unchanged)", err)
	}
	if !strings.Contains(err.Error(), "fail-closed") {
		t.Errorf("refusal %q lost the fail-closed marker", err.Error())
	}
	if hits := stub.hitCount(); hits != 0 {
		t.Fatalf("stub POSTs for refused regular send = %d; want 0", hits)
	}
}

// TestM10BypassLogsWarnPerSend pins the operator-visibility rule: ONE
// loud Warn per bypassed send carrying the sender and the reply kind.
func TestM10BypassLogsWarnPerSend(t *testing.T) {
	stub := &send2Stub{}
	srv := httptestServerForStub(t, stub)
	var logs bytes.Buffer
	c := newBypassClient(t, srv.URL, &logs)

	_, err := c.SendConsentReply(context.Background(), validSend2Req(), "start")
	if err != nil {
		t.Fatalf("first consent reply: %v", err)
	}
	first := logs.String()
	if n := strings.Count(first, "consent reply sent outside recipient allowlist"); n != 1 {
		t.Fatalf("bypass warns after one send = %d; want 1: %s", n, first)
	}
	if !strings.Contains(first, validSend2Req().To) || !strings.Contains(first, "start") {
		t.Errorf("bypass warn missing sender or kind: %s", first)
	}

	_, err = c.SendConsentReply(context.Background(), validSend2Req(), "help")
	if err != nil {
		t.Fatalf("second consent reply: %v", err)
	}
	if n := strings.Count(logs.String(), "consent reply sent outside recipient allowlist"); n != 2 {
		t.Fatalf("bypass warns after two sends = %d; want 2 (one per send): %s", n, logs.String())
	}
}

// TestM10NoBypassOptInStillRefuses pins that SendConsentReply is NOT a
// per-call escape hatch: a client built without WithConsentReplyBypass
// refuses exactly like SendSMS2 under M14.
func TestM10NoBypassOptInStillRefuses(t *testing.T) {
	stub := &send2Stub{}
	srv := httptestServerForStub(t, stub)
	c, err := NewClient(srv.URL,
		WithAPIKey("test-key"),
		WithMessagingLogger(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.SendConsentReply(context.Background(), validSend2Req(), "stop")
	if !errors.Is(err, ErrRecipientNotAllowed) {
		t.Fatalf("no-opt-in consent reply err = %v; want ErrRecipientNotAllowed", err)
	}
	if hits := stub.hitCount(); hits != 0 {
		t.Fatalf("stub POSTs without opt-in = %d; want 0", hits)
	}
}

// TestM10BypassStillConsumesCapBudget pins that the bypass skips ONLY the
// allowlist: with maxPerCall=1 the second consent reply is refused by the
// cap, not silently admitted.
func TestM10BypassStillConsumesCapBudget(t *testing.T) {
	stub := &send2Stub{}
	srv := httptestServerForStub(t, stub)
	c, err := NewClient(srv.URL,
		WithAPIKey("test-key"),
		WithProfileID(send2Profile),
		WithFromNumber(send2From),
		WithAllowedNumbers([]string{validSend2Req().To}), // allowlisted: isolate the cap
		WithSendCaps(1, 0),
		WithConsentReplyBypass(),
		WithMessagingLogger(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.SendConsentReply(context.Background(), validSend2Req(), "stop"); err != nil {
		t.Fatalf("first capped consent reply: %v", err)
	}
	_, err = c.SendConsentReply(context.Background(), validSend2Req(), "stop")
	if !errors.Is(err, ErrSendCapExceeded) {
		t.Fatalf("second capped consent reply err = %v; want ErrSendCapExceeded (cap NOT bypassed)", err)
	}
}
