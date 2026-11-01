package pii

import (
	"reflect"
	"strings"
	"testing"
)

// TestDetector_EmailPhoneInMixedSentence covers the leaf T1 primary
// given/when/then: a mixed sentence yields ordered, non-overlapping spans
// with the right classes.
func TestDetector_EmailPhoneInMixedSentence(t *testing.T) {
	d := NewDetector(Config{Enabled: true})
	spans := d.Detect("contact a@b.com or 555-0100 today")

	var email, phone *Span
	for i := range spans {
		switch spans[i].Class {
		case ClassEmailAddress:
			email = &spans[i]
		case ClassPhoneNumber:
			phone = &spans[i]
		}
	}
	if email == nil {
		t.Fatalf("email not detected in %q; spans=%+v", "contact a@b.com or 555-0100 today", spans)
	}
	if phone == nil {
		t.Fatalf("phone not detected; spans=%+v", spans)
	}
	if email.Value != "a@b.com" {
		t.Errorf("email span value = %q; want %q", email.Value, "a@b.com")
	}
	if phone.Value != "555-0100" {
		t.Errorf("phone span value = %q; want %q", phone.Value, "555-0100")
	}
	assertOrderedNonOverlapping(t, spans)
}

// TestDetector_CreditCardLuhn covers leaf T1: 4111111111111111 matches
// (passes Luhn), 0000000000000000 does not (fails Luhn).
func TestDetector_CreditCardLuhn(t *testing.T) {
	d := NewDetector(Config{Enabled: true, Classes: []string{ClassCreditCard}})

	spans := d.Detect("card 4111111111111111 on file")
	if len(spans) != 1 {
		t.Fatalf("valid PAN: got %d spans (%+v); want 1", len(spans), spans)
	}
	if spans[0].Class != ClassCreditCard || spans[0].Value != "4111111111111111" {
		t.Fatalf("valid PAN span = %+v; want CREDIT_CARD 4111111111111111", spans[0])
	}

	if spans := d.Detect("card 0000000000000000 on file"); len(spans) != 0 {
		t.Fatalf("non-card 16-digit number matched: %+v; Luhn must reject it", spans)
	}

	// Spaced PAN form with Luhn-valid digits also matches.
	if spans := d.Detect("card 4111 1111 1111 1111 on file"); len(spans) != 1 {
		t.Fatalf("spaced PAN not detected: %+v", spans)
	}
}

// TestDetector_LongestFirstNonOverlap covers leaf T1: overlapping/nested
// matches resolve longest-first and spans never overlap.
func TestDetector_LongestFirstNonOverlap(t *testing.T) {
	d := NewDetector(Config{Enabled: true})

	// The 16-digit PAN must not also produce nested phone spans.
	spans := d.Detect("pan 4111111111111111 and ssn 123-45-6789")
	for _, s := range spans {
		if s.Class == ClassPhoneNumber {
			t.Fatalf("phone span nested inside other number runs: %+v", spans)
		}
	}
	var ssn bool
	for _, s := range spans {
		if s.Class == ClassSSN && s.Value == "123-45-6789" {
			ssn = true
		}
	}
	if !ssn {
		t.Fatalf("SSN not detected: %+v", spans)
	}
	assertOrderedNonOverlapping(t, spans)
}

// TestDetector_DisabledConfigReturnsNoSpans covers leaf T1: disabled
// config detects nothing.
func TestDetector_DisabledConfigReturnsNoSpans(t *testing.T) {
	d := NewDetector(Config{Enabled: false})
	spans := d.Detect("mail a@b.com, ssn 123-45-6789, card 4111111111111111")
	if len(spans) != 0 {
		t.Fatalf("disabled detector returned spans: %+v", spans)
	}
}

// TestDetector_ClassFilter pins Config.Classes: only selected classes run.
func TestDetector_ClassFilter(t *testing.T) {
	d := NewDetector(Config{Enabled: true, Classes: []string{ClassEmailAddress}})
	spans := d.Detect("mail a@b.com or 555-0100")
	if len(spans) != 1 || spans[0].Class != ClassEmailAddress {
		t.Fatalf("class filter leaked other classes: %+v", spans)
	}
}

// TestDetector_BrokerTokenOutOfScope pins the leaf note: MEEPT_SECRET
// broker token shapes are NOT matched by any detector class.
func TestDetector_BrokerTokenOutOfScope(t *testing.T) {
	d := NewDetector(Config{Enabled: true})
	for _, text := range []string{
		"use MEEPT_SECRET:db-password-123 from the broker",
		"MEEPT_SECRET:prod-api-key",
	} {
		if spans := d.Detect(text); len(spans) != 0 {
			t.Fatalf("broker token shape matched in %q: %+v", text, spans)
		}
	}
}

// TestDetector_RejectsSubstringsOfDigitRuns pins the phone adjacency
// validation: digit substrings of longer runs never match.
func TestDetector_RejectsSubstringsOfDigitRuns(t *testing.T) {
	d := NewDetector(Config{Enabled: true})
	// 16 digits failing Luhn: no phone sub-span may appear either.
	if spans := d.Detect("ref 0000000000000000 end"); len(spans) != 0 {
		t.Fatalf("digit-run substrings matched: %+v", spans)
	}
}

// TestDetector_IPv4AndIBAN covers the D1 default set beyond the leaf's
// minimum four classes.
func TestDetector_IPv4AndIBAN(t *testing.T) {
	d := NewDetector(Config{Enabled: true})

	spans := d.Detect("host 10.0.0.1 and iban DE89 3704 0044 0532 0130 00")
	var ip, iban bool
	for i := range spans {
		switch spans[i].Class {
		case ClassIPv4:
			if spans[i].Value == "10.0.0.1" {
				ip = true
			}
		case ClassIBAN:
			iban = true
		}
	}
	if !ip {
		t.Fatalf("IPv4 not detected: %+v", spans)
	}
	if !iban {
		t.Fatalf("IBAN not detected: %+v", spans)
	}
	// Invalid octets are rejected by the IPv4 class (the digits may still
	// legitimately match another class, e.g. a dotted phone shape).
	for _, sp := range d.Detect("bad ip 999.999.999.999") {
		if sp.Class == ClassIPv4 {
			t.Fatalf("invalid IPv4 matched: %+v", sp)
		}
	}
	assertOrderedNonOverlapping(t, spans)
}

// TestDetector_PluggableRecognizer pins the Recognizer seam: an appended
// non-regex recognizer contributes spans like built-ins.
func TestDetector_PluggableRecognizer(t *testing.T) {
	d := NewDetector(Config{Enabled: true, Classes: []string{}})
	d.AddRecognizer(recognizerFunc(func(text string) []Span {
		idx := strings.Index(text, "ACME-EMP-ID")
		if idx < 0 {
			return nil
		}
		return []Span{{Start: idx, End: idx + len("ACME-EMP-ID"), Class: "EMP_ID", Value: "ACME-EMP-ID"}}
	}))

	spans := d.Detect("badge ACME-EMP-ID issued")
	if len(spans) != 1 || spans[0].Class != "EMP_ID" {
		t.Fatalf("custom recognizer span missing: %+v", spans)
	}
}

// recognizerFunc adapts a function to the Recognizer interface.
type recognizerFunc func(text string) []Span

func (f recognizerFunc) Detect(text string) []Span { return f(text) }

func assertOrderedNonOverlapping(t *testing.T, spans []Span) {
	t.Helper()
	if !reflect.ValueOf(spans).IsValid() {
		t.Fatal("nil span slice")
	}
	for i := 1; i < len(spans); i++ {
		prev, cur := spans[i-1], spans[i]
		if cur.Start < prev.End {
			t.Fatalf("overlapping spans: %+v then %+v", prev, cur)
		}
		if cur.Start < prev.Start {
			t.Fatalf("unordered spans: %+v then %+v", prev, cur)
		}
	}
}
