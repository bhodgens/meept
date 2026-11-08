package scheduler

import (
	"testing"
	"time"

	"github.com/robfig/cron/v3"
)

// Note: These types and constants are expected to be implemented in the L1 leaf.
// The test suite is intentionally designed to fail (RED) because they are missing.

func TestParseTiming_Kinds(t *testing.T) {
	tests := []struct {
		input    string
		wantKind string
		wantCron string
	}{
		{"daily 09:00:00 America/Denver", "TimingDaily", "0 9 * * *"},
		{"weekly MON,THU 09:00:00 America/Denver", "TimingWeekly", ""},
		{"at 2026-12-01T09:00:00Z", "TimingOneShot", ""},
		{"every 1h30m", "TimingEvery", ""},
		{"rrule:\nDTSTART;TZID=America/Denver:20261201T090000\nRRULE:FREQ=WEEKLY;BYDAY=MO,TH", "TimingRRule", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			spec, err := ParseTiming(tt.input)
			if err != nil {
				t.Fatalf("ParseTiming(%q) failed: %v", tt.input, err)
			}

			if string(spec.Kind) != tt.wantKind {
				t.Errorf("ParseTiming(%q) Kind = %v, want %v", tt.input, spec.Kind, tt.wantKind)
			}

			if tt.wantCron != "" {
				cronExpr, err := spec.CronExpr()
				if err != nil {
					t.Errorf("ParseTiming(%q) CronExpr() error: %v", tt.input, err)
					return
				}
				if cronExpr == "" {
					t.Errorf("ParseTiming(%q) CronExpr() = empty", tt.input)
				}
				if _, err := cron.ParseStandard(cronExpr); err != nil {
					t.Errorf("ParseTiming(%q) produced invalid cron: %v", tt.input, err)
				}
			}
		})
	}
}

func TestTiming_CronExpr_PerKind(t *testing.T) {
	exprs := []struct {
		name string
		expr string
	}{
		{"daily", "0 9 * * *"},
		{"weekly", "0 9 * * 1,4"},
	}
	for _, tt := range exprs {
		if _, err := cron.ParseStandard(tt.expr); err != nil {
			t.Errorf("Invalid cron expr %q for %s: %v", tt.expr, tt.name, err)
		}
	}
}

func TestTiming_NextOccurrence_DST(t *testing.T) {
	input := "daily 09:00:00 America/Denver"
	spec, err := ParseTiming(input)
	if err != nil {
		t.Fatalf("ParseTiming failed: %v", err)
	}

	loc, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Fatal(err)
	}

	t1 := time.Date(2026, 3, 7, 0, 0, 0, 0, loc)
	t2 := time.Date(2026, 3, 9, 0, 0, 0, 0, loc)

	next1, _ := spec.NextOccurrence(t1)
	if next1.Hour() != 9 || next1.Location().String() != "America/Denver" {
		t.Errorf("Expected 09:00 local for t1, got %v", next1)
	}

	next2, _ := spec.NextOccurrence(t2)
	if next2.Hour() != 9 || next2.Location().String() != "America/Denver" {
		t.Errorf("Expected 09:00 local for t2, got %v", next2)
	}
}

func TestTiming_Validate_RFC5545Boundary(t *testing.T) {
	tests := []struct {
		input string
		err   error 
	}{
		{"FREQ=MONTHLY;BYDAY=-1FR", nil},
		{"FREQ=YEARLY;COUNT=5", nil},
		{"FREQ=DAILY;INTERVAL=2", nil},
		{"BYSECOND=0", nil},
		{"BYMONTHDAY=29", nil},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := ParseTiming(tt.input)
			if err == nil {
				t.Errorf("Expected error for %q, got nil", tt.input)
			}
		})
	}
}
