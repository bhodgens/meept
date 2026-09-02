package llm

// Tests for the SSE scanner's reader robustness (tree 02 leaf 03).
//
// Background: sseScanner.Scan() used to discard bytes returned together
// with io.EOF (the common http-response-body pattern) and dropped the
// byte at the end of each read chunk. Chunked/streaming delivery — the
// normal production case — silently parsed as an empty response. These
// tests pin the fix across reader split behaviors.

import (
	"strings"
	"testing"
	"testing/iotest"
)

func countSSEEvents(t *testing.T, r interface {
	Read(p []byte) (int, error)
}) int {
	t.Helper()
	sc := newSSEScanner(r)
	n := 0
	for sc.Scan() {
		if ev := sc.Event(); ev == nil || ev.Data == "" {
			t.Errorf("event %d: empty event", n)
		}
		n++
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanner err: %v", err)
	}
	return n
}

func TestSSEScanner_AllEventsAtOnce(t *testing.T) {
	if got := countSSEEvents(t, strings.NewReader(anthropicSSEBody)); got != 5 {
		t.Errorf("events = %d, want 5", got)
	}
}

func TestSSEScanner_OneByteChunks(t *testing.T) {
	if got := countSSEEvents(t, iotest.OneByteReader(strings.NewReader(anthropicSSEBody))); got != 5 {
		t.Errorf("events = %d, want 5", got)
	}
}

func TestSSEScanner_HalfChunks(t *testing.T) {
	if got := countSSEEvents(t, iotest.HalfReader(strings.NewReader(anthropicSSEBody))); got != 5 {
		t.Errorf("events = %d, want 5", got)
	}
}

// TestSSEScanner_DataWithEOF is the regression test for the production
// bug: readers that return the final bytes TOGETHER with io.EOF (http
// response bodies do this) previously discarded those bytes, yielding
// zero events.
func TestSSEScanner_DataWithEOF(t *testing.T) {
	if got := countSSEEvents(t, iotest.DataErrReader(strings.NewReader(anthropicSSEBody))); got != 5 {
		t.Errorf("events = %d, want 5", got)
	}
}

func TestSSEScanner_NoTrailingNewline(t *testing.T) {
	trimmed := strings.TrimSuffix(anthropicSSEBody, "\n\n")
	if got := countSSEEvents(t, strings.NewReader(trimmed)); got != 5 {
		t.Errorf("events = %d, want 5 (EOF flush)", got)
	}
}
