package tui

import (
	"testing"
	"time"
)

func TestFormatDuration_Hours(t *testing.T) {
	tests := []struct {
		in     time.Duration
		expect string
	}{
		{3*time.Hour + 12*time.Minute, "3h 12m"},
		{3*time.Hour, "3h"},
		{24*time.Hour, "24h"},
		{1*time.Hour + 5*time.Minute, "1h 5m"},
	}
	for _, tt := range tests {
		got := FormatDuration(tt.in)
		if got != tt.expect {
			t.Errorf("FormatDuration(%v) = %q, want %q", tt.in, got, tt.expect)
		}
	}
}

func TestFormatDuration_Minutes(t *testing.T) {
	tests := []struct {
		in     time.Duration
		expect string
	}{
		{45 * time.Minute, "45m"},
		{1 * time.Minute, "1m"},
		{59 * time.Minute, "59m"},
	}
	for _, tt := range tests {
		got := FormatDuration(tt.in)
		if got != tt.expect {
			t.Errorf("FormatDuration(%v) = %q, want %q", tt.in, got, tt.expect)
		}
	}
}

func TestFormatDuration_Seconds(t *testing.T) {
	tests := []struct {
		in     time.Duration
		expect string
	}{
		{90 * time.Second, "1m"}, // >= 1m, so "1m"
		{30 * time.Second, "30s"},
		{1 * time.Second, "1s"},
		{0, "0s"},
		{59 * time.Second, "59s"},
	}
	for _, tt := range tests {
		got := FormatDuration(tt.in)
		if got != tt.expect {
			t.Errorf("FormatDuration(%v) = %q, want %q", tt.in, got, tt.expect)
		}
	}
}

func TestFormatDuration_PastDue(t *testing.T) {
	if got := FormatDuration(-1 * time.Second); got != "resuming…" {
		t.Errorf("FormatDuration(-1s) = %q, want %q", got, "resuming…")
	}
	if got := FormatDuration(-5 * time.Hour); got != "resuming…" {
		t.Errorf("FormatDuration(-5h) = %q, want %q", got, "resuming…")
	}
}

// TestQuotaStatus_Badges verifies the quota status badge rendering.
func TestQuotaStatus_Badges(t *testing.T) {
	p := &AgentsPanel{}

	// Blocked state.
	block := p.quotaStatusBadge(nil, true)
	if block == "" {
		t.Error("expected non-empty blocked badge")
	}

	// Quota wait with future time.
	future := time.Now().Add(3*time.Hour + 12*time.Minute)
	wait := p.quotaStatusBadge(&future, false)
	if wait == "" {
		t.Error("expected non-empty quota wait badge")
	}

	// Past-due should render as resuming…
	past := time.Now().Add(-1 * time.Hour)
	resuming := p.quotaStatusBadge(&past, false)
	if resuming == "" {
		t.Error("expected non-empty resuming badge")
	}
}

// TestQuotaStatus_NoEpisode verifies that agents without quota state are
// unaffected (regression safety).
func TestQuotaStatus_NoEpisode(t *testing.T) {
	p := &AgentsPanel{}
	got := p.quotaStatusBadge(nil, false)
	if got != "" {
		t.Errorf("expected empty badge when no quota state, got %q", got)
	}
}

// TestAgentSummary_QuotaFields verifies the struct has the right JSON tags.
func TestAgentSummary_QuotaFields(t *testing.T) {
	a := AgentSummary{
		ID:             "agent-1",
		Name:           "test-agent",
		Role:           "coder",
		Status:         "running",
		Tier:           "tier_1_reactive",
		DriftScore:     0.01,
		DailyCostCents: 50,
		FindingsCount:  0,
	}
	if a.QuotaWaitUntil != nil {
		t.Error("expected QuotaWaitUntil to be nil by default")
	}
	if a.QuotaBlocked {
		t.Error("expected QuotaBlocked to be false by default")
	}
}
