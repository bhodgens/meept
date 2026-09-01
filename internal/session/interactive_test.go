package session

import (
	"testing"
	"time"
)

// TestIsInteractive exercises the D11 interactivity rule: a session is
// interactive when it received a user message within the Q1 window or
// holds the client-declared Foreground flag. All cases inject a fixed
// clock so no test depends on real time.
func TestIsInteractive(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	window := 5 * time.Minute

	tests := []struct {
		name    string
		session *Session
		now     time.Time
		window  time.Duration
		want    bool
	}{
		{
			name:    "nil session is never interactive",
			session: nil,
			now:     now,
			window:  window,
			want:    false,
		},
		{
			name: "user message within window is interactive",
			session: &Session{
				LastUserMessageAt: now.Add(-2 * time.Minute),
			},
			now:    now,
			window: window,
			want:   true,
		},
		{
			name: "user message outside window is not interactive",
			session: &Session{
				LastUserMessageAt: now.Add(-6 * time.Minute),
			},
			now:    now,
			window: window,
			want:   false,
		},
		{
			name:    "zero user message timestamp is not interactive",
			session: &Session{},
			now:     now,
			window:  window,
			want:    false,
		},
		{
			name:    "window <= 0 and no foreground is not interactive",
			session: &Session{LastUserMessageAt: now},
			now:     now,
			window:  0,
			want:    false,
		},
		{
			name:    "window <= 0 with foreground is interactive",
			session: &Session{Foreground: true},
			now:     now,
			window:  0,
			want:    true,
		},
		{
			name: "foreground forces interactive regardless of age",
			session: &Session{
				Foreground:        true,
				LastUserMessageAt: now.Add(-48 * time.Hour),
			},
			now:    now,
			window: window,
			want:   true,
		},
		{
			name: "boundary exactly at window edge counts as within",
			session: &Session{
				LastUserMessageAt: now.Add(-window),
			},
			now:    now,
			window: window,
			want:   true,
		},
		{
			name: "negative window only foreground counts",
			session: &Session{
				LastUserMessageAt: now,
			},
			now:    now,
			window: -time.Minute,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsInteractive(tt.session, tt.now, tt.window)
			if got != tt.want {
				t.Fatalf("IsInteractive() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIsInteractive_ZeroTime verifies a session that has never received a
// user message (zero time) is not treated as interactive even with a huge
// window, guarding against the zero-time-as-epoch false positive.
func TestIsInteractive_ZeroTime(t *testing.T) {
	s := &Session{}
	if IsInteractive(s, time.Now(), 24*time.Hour) {
		t.Fatal("zero LastUserMessageAt must not count as interactive")
	}
}
