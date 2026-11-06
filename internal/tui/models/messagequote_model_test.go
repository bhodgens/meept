// Unit tests for the message-quote pure model (dsh-convergence leaf
// 04-tool-surface/05, task T1). The headless gesture gate lives in
// internal/tui/messagequote_test.go (gate 00T T4); this file pins the
// QuoteReference/QuoteLabel contract itself.
package models

import (
	"strings"
	"testing"
)

func TestQuoteReference_Basic(t *testing.T) {
	got := QuoteReference("msg-12", "first\nsecond")
	want := "\n> [msg msg-12] first\n> second"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestQuoteReference_EmptyText(t *testing.T) {
	got := QuoteReference("msg-7", "")
	want := "\n> [msg msg-7] "
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestQuoteReference_SanitizesID(t *testing.T) {
	// A hostile id must not inject new blockquote lines into the reply:
	// the newline is stripped so the whole reference stays on one line.
	got := QuoteReference("evil\n> injected", "x")
	// Exactly one newline total: the separator before the blockquote. A
	// newline smuggled through the id would make this two.
	if strings.Count(got, "\n") != 1 {
		t.Fatalf("id newline survived sanitization: %q", got)
	}
	if strings.Count(got, "\n>") != 1 {
		t.Fatalf("expected exactly one blockquote line, got %q", got)
	}
	if QuoteLabel("") != "[msg <unknown>] " {
		t.Fatalf("empty id label: %q", QuoteLabel(""))
	}
}

func TestQuoteReference_LeadingNewlineSeparates(t *testing.T) {
	// The quote always starts with a newline so it separates from prior
	// input instead of gluing onto the last typed line.
	if !strings.HasPrefix(QuoteReference("m", "t"), "\n") {
		t.Fatal("QuoteReference must begin with a separating newline")
	}
}
