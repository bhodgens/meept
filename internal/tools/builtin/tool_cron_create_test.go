package builtin

import "testing"

func TestParseTime(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantH    int
		wantM    int
		wantErr  bool
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
