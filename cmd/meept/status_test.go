package main

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/tui/types"
)

// TestTokenBudgetText covers the shared token-budget formatter used by
// `meept daemon status`, the TUI status dashboard (internal/tui/models), and
// the lite /usage surface (cmd/meept-lite). The percentage is always relative
// to the CONFIGURED limit -- never to used+remaining.
func TestTokenBudgetText(t *testing.T) {
	tests := []struct {
		name        string
		used        int
		limit       int
		want        string
		wantLimited bool
		wantRatio   float64
	}{
		{
			name:        "limit disabled with usage",
			used:        12345,
			limit:       0,
			want:        "12345 (no limit)",
			wantLimited: false,
			wantRatio:   0,
		},
		{
			name:        "limit disabled and no usage",
			used:        0,
			limit:       0,
			want:        "0 (no limit)",
			wantLimited: false,
			wantRatio:   0,
		},
		{
			name:        "limit set and not reached",
			used:        5000,
			limit:       50000,
			want:        "5000 / 50000 (10.0%)",
			wantLimited: true,
			wantRatio:   0.1,
		},
		{
			name:        "limit set and exactly reached",
			used:        50000,
			limit:       50000,
			want:        "50000 / 50000 (100.0%)",
			wantLimited: true,
			wantRatio:   1.0,
		},
		{
			name:        "limit set and used zero",
			used:        0,
			limit:       50000,
			want:        "0 / 50000 (0.0%)",
			wantLimited: true,
			wantRatio:   0,
		},
		{
			name:        "limit set and exceeded",
			used:        60000,
			limit:       50000,
			want:        "60000 / 50000 (120.0%)",
			wantLimited: true,
			wantRatio:   1.0, // clamped so a progress bar never overflows
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := types.NewTokenBudget(tt.used, tt.limit)

			if text := got.Text(); text != tt.want {
				t.Errorf("Text() = %q, want %q", text, tt.want)
			}
			if got.Limited() != tt.wantLimited {
				t.Errorf("Limited() = %v, want %v", got.Limited(), tt.wantLimited)
			}
			if diff := math.Abs(got.Ratio() - tt.wantRatio); diff > 1e-9 {
				t.Errorf("Ratio() = %v, want %v", got.Ratio(), tt.wantRatio)
			}
		})
	}
}

// TestTokenBudgetDisabledNeverShowsPercent is the regression guard for the
// reported bug: when the hourly limit is DISABLED the daemon reports
// remaining=0 while usage keeps counting up, so a used+remaining denominator
// rendered "N / N (100.0%)". A disabled budget must print the used count with
// a no-limit note, no percentage, and no full bar.
func TestTokenBudgetDisabledNeverShowsPercent(t *testing.T) {
	for _, used := range []int{0, 1, 5000, 123456, 1000000} {
		tb := types.NewTokenBudget(used, 0)

		text := tb.Text()
		if strings.Contains(text, "%") {
			t.Errorf("used=%d, disabled limit: Text() = %q, must not contain a percentage", used, text)
		}
		if !strings.Contains(text, "(no limit)") {
			t.Errorf("used=%d, disabled limit: Text() = %q, want a 'no limit' note", used, text)
		}
		if tb.Limited() {
			t.Errorf("used=%d, disabled limit: Limited() = true, want false", used)
		}
		if tb.Ratio() != 0 {
			t.Errorf("used=%d, disabled limit: Ratio() = %v, want 0 (no full bar)", used, tb.Ratio())
		}
		// The exact pre-fix artifact: used / used at 100.0%.
		oldArtifact := fmt.Sprintf("%d / %d (100.0%%)", used, used)
		if text == oldArtifact {
			t.Errorf("used=%d, disabled limit: Text() reproduced the old 100%% artifact %q", used, oldArtifact)
		}
	}
}

// TestPrintStatusText_TokenLineDisabledLimit asserts the user-visible
// `meept daemon status` block for a disabled token budget.
func TestPrintStatusText_TokenLineDisabledLimit(t *testing.T) {
	status := &types.DaemonStatusResponse{
		Status:           "running",
		Model:            "test/model",
		TokensUsed:       12345,
		TokensRemaining:  0, // disabled budget: daemon reports 0 remaining
		HourlyTokenLimit: 0,
	}

	out := captureStdout(t, func() { printStatusText(status, 4242) })

	if !strings.Contains(out, "12345 (no limit)") {
		t.Errorf("expected '12345 (no limit)' in status output, got:\n%s", out)
	}
	if strings.Contains(out, "100.0%") {
		t.Errorf("disabled token budget must not render 100.0%%, got:\n%s", out)
	}
	if !strings.Contains(out, "token budget") {
		t.Errorf("expected the token budget block in status output, got:\n%s", out)
	}
}

// TestPrintStatusText_TokenLineConfiguredLimit asserts the user-visible
// `meept daemon status` block prints used against the LIMIT and its percentage.
func TestPrintStatusText_TokenLineConfiguredLimit(t *testing.T) {
	status := &types.DaemonStatusResponse{
		Status:           "running",
		Model:            "test/model",
		TokensUsed:       5000,
		TokensRemaining:  45000,
		HourlyTokenLimit: 50000,
	}

	out := captureStdout(t, func() { printStatusText(status, 4242) })

	if !strings.Contains(out, "5000 / 50000 (10.0%)") {
		t.Errorf("expected '5000 / 50000 (10.0%%)' in status output, got:\n%s", out)
	}
}
