package llm

import (
	"net/http"
	"testing"
	"time"
)

// retryAfterGMT is an explicit fixed zone for all fixture dates so tests are
// never time.Now-relative (leaf rule: GMT-fixed fixtures only).
var retryAfterGMT = time.FixedZone("GMT", 0)

// TestParseRetryAfter_Forms covers the four RFC7231/RFC3339 Retry-After
// header forms (DECISIONS.md D6) in spec order, plus the not-present and
// clamped-past cases. Date-form results are compared as exact instants
// (zone-independent via .Equal); delta-form results are exact durations.
func TestParseRetryAfter_Forms(t *testing.T) {
	futureDate := time.Date(2027, time.December, 31, 23, 59, 59, 0, retryAfterGMT)
	pastDate := time.Date(2020, time.January, 1, 0, 0, 0, 0, retryAfterGMT)

	tests := []struct {
		name        string
		header      http.Header
		wantPresent bool
		wantDate    time.Time // expected date instant (date forms; zero when delta form)
		wantDelta   time.Duration
		wantPast    bool // assert delta < 0 instead of exact (past date)
	}{
		{
			name:        "delta-seconds",
			header:      http.Header{"Retry-After": []string{"120"}},
			wantPresent: true,
			wantDelta:   120 * time.Second,
		},
		{
			name:        "IMF-fixdate",
			header:      http.Header{"Retry-After": []string{"Fri, 31 Dec 2027 23:59:59 GMT"}},
			wantPresent: true,
			wantDate:    futureDate,
		},
		{
			name:        "RFC850",
			header:      http.Header{"Retry-After": []string{"Friday, 31-Dec-27 23:59:59 GMT"}},
			wantPresent: true,
			wantDate:    futureDate,
		},
		{
			name:        "asctime",
			header:      http.Header{"Retry-After": []string{"Fri Dec 31 23:59:59 2027"}},
			wantPresent: true,
			wantDate:    futureDate,
		},
		{
			name:        "RFC3339 date tolerated",
			header:      http.Header{"Retry-After": []string{"2027-12-31T23:59:59Z"}},
			wantPresent: true,
			wantDate:    time.Date(2027, time.December, 31, 23, 59, 59, 0, time.UTC),
		},
		{
			name:        "garbage not present",
			header:      http.Header{"Retry-After": []string{"not-a-date"}},
			wantPresent: false,
		},
		{
			name:        "no header not present",
			header:      http.Header{},
			wantPresent: false,
		},
		{
			name:        "nil header not present",
			header:      nil,
			wantPresent: false,
		},
		{
			name:        "past date present with negative delta (caller clamps)",
			header:      http.Header{"Retry-After": []string{"Wed, 01 Jan 2020 00:00:00 GMT"}},
			wantPresent: true,
			wantDate:    pastDate,
			wantPast:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			date, delta, present := ParseRetryAfter(tt.header)
			if present != tt.wantPresent {
				t.Fatalf("present = %v, want %v", present, tt.wantPresent)
			}
			if !tt.wantPresent {
				return
			}
			if !tt.wantDate.IsZero() {
				if !date.Equal(tt.wantDate) {
					t.Errorf("date = %v, want %v", date, tt.wantDate)
				}
				return
			}
			if tt.wantPast {
				if delta >= 0 {
					t.Errorf("delta = %v, want negative (past date)", delta)
				}
				return
			}
			if delta != tt.wantDelta {
				t.Errorf("delta = %v, want %v", delta, tt.wantDelta)
			}
		})
	}
}

// TestParseRetryAfter_ProviderHeaders pins the provider-specific reset
// headers preserved verbatim from parseQuotaResetHeader (errors_quota.go):
// the anthropic-ratelimit-* family (RFC3339 dates) and the X-Codex-* family
// (delta seconds). Retry-After must take precedence over all of them.
func TestParseRetryAfter_ProviderHeaders(t *testing.T) {
	tests := []struct {
		name        string
		header      http.Header
		wantPresent bool
		wantDelta   time.Duration // expected when delta-form header
		wantDate    time.Time     // expected when date-form header
	}{
		{
			name:        "anthropic-ratelimit-tokens-reset",
			header:      http.Header{"Anthropic-Ratelimit-Tokens-Reset": []string{"2027-12-31T23:59:59Z"}},
			wantPresent: true,
			wantDate:    time.Date(2027, time.December, 31, 23, 59, 59, 0, time.UTC),
		},
		{
			name:        "anthropic-ratelimit-requests-reset",
			header:      http.Header{"Anthropic-Ratelimit-Requests-Reset": []string{"2027-12-31T23:59:59Z"}},
			wantPresent: true,
			wantDate:    time.Date(2027, time.December, 31, 23, 59, 59, 0, time.UTC),
		},
		{
			name:        "anthropic-ratelimit-concurrent-reset",
			header:      http.Header{"Anthropic-Ratelimit-Concurrent-Reset": []string{"2027-12-31T23:59:59Z"}},
			wantPresent: true,
			wantDate:    time.Date(2027, time.December, 31, 23, 59, 59, 0, time.UTC),
		},
		{
			name:        "X-Codex-Primary-Reset-At delta seconds",
			header:      http.Header{"X-Codex-Primary-Reset-At": []string{"90"}},
			wantPresent: true,
			wantDelta:   90 * time.Second,
		},
		{
			name:        "X-Codex-Primary-Reset-After-Seconds delta seconds",
			header:      http.Header{"X-Codex-Primary-Reset-After-Seconds": []string{"45"}},
			wantPresent: true,
			wantDelta:   45 * time.Second,
		},
		{
			name: "Retry-After beats provider headers (precedence)",
			header: http.Header{
				"Retry-After":                      []string{"10"},
				"Anthropic-Ratelimit-Tokens-Reset": []string{"2027-12-31T23:59:59Z"},
				"X-Codex-Primary-Reset-At":         []string{"3600"},
			},
			wantPresent: true,
			wantDelta:   10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			date, delta, present := ParseRetryAfter(tt.header)
			if present != tt.wantPresent {
				t.Fatalf("present = %v, want %v", present, tt.wantPresent)
			}
			if !tt.wantPresent {
				return
			}
			if !tt.wantDate.IsZero() {
				if !date.Equal(tt.wantDate) {
					t.Errorf("date = %v, want %v", date, tt.wantDate)
				}
				return
			}
			if delta != tt.wantDelta {
				t.Errorf("delta = %v, want %v", delta, tt.wantDelta)
			}
		})
	}
}
