package signal

// Gate tests for the signal MESSAGING surface (contract C3 Send/Typing +
// envelope demux; feature F5). Written BEFORE implementation
// (00T-gate-tests.md, T5 signal half). RED until leaf 10/11 land.
//
// Pinned rules:
//   - Send returns the message timestamp (ts int64) from the signal-cli
//     `send` result.
//   - Typing(recipient, stop) issues `sendTyping` with the stop flag.
//   - Inbound "envelope" notifications demux to DataMessage{sourceUuid,
//     body, attachments, timestamp}; (sourceUuid, timestamp) dedupe — a
//     replayed envelope is NOT surfaced twice.

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestClientSendReturnsTimestamp pins the Send signature: the stub's
// `send` result carries the timestamp the client must surface.
func TestClientSendReturnsTimestamp(t *testing.T) {
	srv := startStubRPC(t)
	srv.mu.Lock()
	srv.respond = func(method string, id int) any {
		if method == "send" {
			return map[string]any{"timestamp": int64(1731000000123)}
		}
		return map[string]any{}
	}
	srv.mu.Unlock()

	c, err := Dial(srv.socketPath())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() }) //nolint:errcheck // test teardown

	ts, err := c.Send("+15550000014", "hello from meept", nil)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if ts != 1731000000123 {
		t.Errorf("Send ts = %d, want 1731000000123", ts)
	}
	if srv.count("send") != 1 {
		t.Errorf("stub saw %d send requests, want 1", srv.count("send"))
	}
}

// TestClientTyping pins the typing seam: stop=false starts, stop=true
// stops; both ride `sendTyping`.
func TestClientTyping(t *testing.T) {
	srv := startStubRPC(t)
	srv.mu.Lock()
	srv.respond = func(string, int) any { return map[string]any{} }
	srv.mu.Unlock()

	c, err := Dial(srv.socketPath())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() }) //nolint:errcheck // test teardown

	if err := c.Typing("+15550000015", false); err != nil {
		t.Fatalf("Typing(start): %v", err)
	}
	if err := c.Typing("+15550000015", true); err != nil {
		t.Fatalf("Typing(stop): %v", err)
	}
	if srv.count("sendTyping") != 2 {
		t.Errorf("stub saw %d sendTyping requests, want 2", srv.count("sendTyping"))
	}
}

// TestEnvelopeDemuxAndDedupe pins the envelope notification demux: an
// inbound DataMessage surfaces with sourceUuid/body/timestamp; a REPLAY
// of the same (sourceUuid, timestamp) envelope is deduped (surfaced once).
func TestEnvelopeDemuxAndDedupe(t *testing.T) {
	srv := startStubRPC(t)
	srv.mu.Lock()
	srv.respond = func(string, int) any { return map[string]any{} }
	srv.mu.Unlock()

	c, err := Dial(srv.socketPath())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() }) //nolint:errcheck // test teardown

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	msgs := c.Messages(ctx)

	const (
		uuid = "dddd4444-2222-3333-4444-555555555555"
		ts   = int64(1731000000999)
	)
	srv.pushEnvelope(uuid, "ping from signal", ts)
	srv.pushEnvelope(uuid, "ping from signal", ts) // replayed envelope
	srv.pushEnvelope(uuid, "distinct second message", ts+1)

	var got []DataMessage
	deadline := time.After(4 * time.Second)
	for len(got) < 2 {
		select {
		case m, ok := <-msgs:
			if !ok {
				t.Fatalf("message stream closed early; got %v", got)
			}
			got = append(got, m)
		case <-deadline:
			t.Fatalf("timed out collecting messages; got %v (dedupe failed?)", got)
		case <-ctx.Done():
			t.Fatalf("ctx done collecting messages; got %v", got)
		}
	}
	// Draining briefly to give a (broken) duplicate a chance to surface.
	time.Sleep(100 * time.Millisecond)
	if len(got) != 2 {
		t.Fatalf("surfaced %d messages, want 2 (replayed envelope must be deduped)", len(got))
	}
	if got[0].SourceUUID != uuid || got[0].Body != "ping from signal" || got[0].Timestamp != ts {
		t.Errorf("message 1 = %+v, want sourceUuid %s body ping ts %d", got[0], uuid, ts)
	}
	if got[1].Body != "distinct second message" {
		t.Errorf("message 2 = %+v, want the distinct second message", got[1])
	}
}

// pushEnvelope writes one signal-cli "envelope" notification (params
// envelope carries sourceUuid + dataMessage).
func (s *stubRPCServer) pushEnvelope(sourceUUID, body string, ts int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	conn := s.firstConnLocked()
	params := map[string]any{
		"account": "+15559999999",
		"envelope": map[string]any{
			"sourceUuid": sourceUUID,
			"timestamp":  ts,
			"dataMessage": map[string]any{
				"timestamp":   ts,
				"message":     body,
				"attachments": []any{},
				"groupInfo":   nil,
				"destination": nil,
			},
		},
	}
	bodyBytes, err := json.Marshal(params)
	if err != nil {
		return
	}
	n, err := json.Marshal(stubNotification{JSONRPC: "2.0", Method: "envelope", Params: bodyBytes})
	if err != nil {
		return
	}
	_, _ = conn.Write(append(n, '\n')) //nolint:errcheck // test stub
}
