package builtin

import (
	"strings"
	"testing"
)

func TestParseTime(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantH   int
		wantM   int
		wantErr bool
	}{
		{"3pm", "3pm", 15, 0, false},
		{"9:00am", "9:00am", 9, 0, false},
		{"14:30", "14:30", 14, 30, false},
		{"15", "15", 15, 0, false},
		{"3:30pm", "3:30pm", 15, 30, false},
		{"12am", "12am", 0, 0, false},
		{"12pm", "12pm", 12, 0, false},
		{"9:05am", "9:05am", 9, 5, false},
		{"invalid", "invalid", 0, 0, true},
		{"", "", 0, 0, true},
		{"garbage 123", "garbage 123", 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, m, err := parseTime(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTime(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
				return
			}
			if !tt.wantErr && (h != tt.wantH || m != tt.wantM) {
				t.Errorf("parseTime(%q) = (%d, %d), want (%d, %d)", tt.in, h, m, tt.wantH, tt.wantM)
			}
		})
	}
}

// TestBuildCronExpression_DayOfMonthDefault verifies that an absent
// day_of_month defaults to 1 (cron field "0 9 1 * *").
func TestBuildCronExpression_DayOfMonthDefault(t *testing.T) {
	tool := &CronCreateTool{}
	expr, err := tool.buildCronExpression(map[string]any{
		"interval": "monthly",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default at_time is 9:00am → "0 9 1 * *"
	if expr != "0 9 1 * *" {
		t.Errorf("expected '0 9 1 * *', got %q", expr)
	}
}

// TestBuildCronExpression_DayOfMonthOutOfRange verifies that an explicit
// out-of-range day_of_month returns an error rather than silently defaulting.
func TestBuildCronExpression_DayOfMonthOutOfRange(t *testing.T) {
	tool := &CronCreateTool{}
	_, err := tool.buildCronExpression(map[string]any{
		"interval":      "monthly",
		"day_of_month":  float64(0),
	})
	if err == nil {
		t.Fatal("expected error for day_of_month=0, got nil")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected 'out of range' error, got %v", err)
	}

	_, err = tool.buildCronExpression(map[string]any{
		"interval":      "monthly",
		"day_of_month":  float64(32),
	})
	if err == nil {
		t.Fatal("expected error for day_of_month=32, got nil")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected 'out of range' error, got %v", err)
	}
}

// TestBuildCronExpression_DayOfMonthValid verifies that a valid explicit
// day_of_month is honored.
func TestBuildCronExpression_DayOfMonthValid(t *testing.T) {
	tool := &CronCreateTool{}
	expr, err := tool.buildCronExpression(map[string]any{
		"interval":      "monthly",
		"day_of_month":  float64(15),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if expr != "0 9 15 * *" {
		t.Errorf("expected '0 9 15 * *', got %q", expr)
	}
}
