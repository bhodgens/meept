package lite

import (
	"strings"
	"testing"
	"time"
)

func TestNewStatusBar(t *testing.T) {
	sb := NewStatusBar()
	if sb == nil {
		t.Fatal("NewStatusBar returned nil")
	}
	if sb.tokensMax != 128000 {
		t.Errorf("default tokensMax = %d, want 128000", sb.tokensMax)
	}
	if sb.width != 80 {
		t.Errorf("default width = %d, want 80", sb.width)
	}
}

func TestSetSize(t *testing.T) {
	sb := NewStatusBar()
	sb.SetSize(120)
	if sb.width != 120 {
		t.Errorf("width = %d, want 120", sb.width)
	}
}

func TestSetModel(t *testing.T) {
	sb := NewStatusBar()
	sb.SetModel("claude-sonnet")
	if sb.modelName != "claude-sonnet" {
		t.Errorf("modelName = %q, want %q", sb.modelName, "claude-sonnet")
	}
}

func TestSetTokens(t *testing.T) {
	sb := NewStatusBar()
	sb.SetTokens(5000, 100000)
	if sb.tokensUsed != 5000 {
		t.Errorf("tokensUsed = %d, want 5000", sb.tokensUsed)
	}
	if sb.tokensMax != 100000 {
		t.Errorf("tokensMax = %d, want 100000", sb.tokensMax)
	}
}

func TestSetCost(t *testing.T) {
	sb := NewStatusBar()
	sb.SetCost(150) // $1.50
	if sb.costCents != 150 {
		t.Errorf("costCents = %d, want 150", sb.costCents)
	}
}

func TestSetStartTime(t *testing.T) {
	sb := NewStatusBar()
	now := time.Now().Add(-5 * time.Minute)
	sb.SetStartTime(now)
	if sb.startTime != now {
		t.Errorf("startTime = %v, want %v", sb.startTime, now)
	}
}

func TestCalculatePercent(t *testing.T) {
	tests := []struct {
		name       string
		tokensUsed int
		tokensMax  int
		want       int
	}{
		{"zero tokens", 0, 100000, 0},
		{"50 percent", 50000, 100000, 50},
		{"100 percent", 100000, 100000, 100},
		{"over 100 percent", 120000, 100000, 100},
		{"zero max", 1000, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := NewStatusBar()
			sb.SetTokens(tt.tokensUsed, tt.tokensMax)
			got := sb.calculatePercent()
			if got != tt.want {
				t.Errorf("calculatePercent() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetPercentColor(t *testing.T) {
	sb := NewStatusBar()

	// Test color thresholds
	tests := []struct {
		percent int
		want    string // color string
	}{
		{10, "#10B981"},  // green
		{49, "#10B981"},  // green
		{50, "#F59E0B"},  // yellow
		{79, "#F59E0B"},  // yellow
		{80, "#F97316"},  // orange
		{94, "#F97316"},  // orange
		{95, "#EF4444"},  // red
		{100, "#EF4444"}, // red
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.percent)), func(t *testing.T) {
			got := string(sb.getPercentColor(tt.percent))
			if got != tt.want {
				t.Errorf("getPercentColor(%d) = %s, want %s", tt.percent, got, tt.want)
			}
		})
	}
}

func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{500, "500"},
		{999, "999"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{9999, "10.0k"},
		{10000, "10k"},
		{128000, "128k"},
		{999999, "999k"},
		{1000000, "1.0M"},
		{2500000, "2.5M"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatTokenCount(tt.n)
			if got != tt.want {
				t.Errorf("formatTokenCount(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{30 * time.Second, "30s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m00s"},
		{90 * time.Second, "1m30s"},
		{3*time.Minute + 42*time.Second, "3m42s"},
		{59*time.Minute + 59*time.Second, "59m59s"},
		{60 * time.Minute, "1h00m"},
		{61 * time.Minute, "1h01m"},
		{2*time.Hour + 30*time.Minute, "2h30m"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatDuration(tt.d)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestAbbreviateModelName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"claude-3-opus", "opus"},
		{"claude-3-sonnet", "sonnet"},
		{"claude-3-haiku", "haiku"},
		{"claude-3.5-sonnet", "sonnet-3.5"},
		{"claude-opus-4", "opus-4"},
		{"gpt-4", "gpt4"},
		{"gpt-4-turbo", "gpt4t"},
		{"gpt-4o", "gpt4o"},
		{"gemini-pro", "gemini"},
		{"unknown-model", "unknown-"},
		{"short", "short"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := abbreviateModelName(tt.name)
			if got != tt.want {
				t.Errorf("abbreviateModelName(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestGetModelDisplay(t *testing.T) {
	sb := NewStatusBar()

	// Empty model name
	sb.SetModel("")
	got := sb.getModelDisplay(20)
	if got != "unknown" {
		t.Errorf("getModelDisplay for empty = %q, want %q", got, "unknown")
	}

	// Full model name fits
	sb.SetModel("claude-sonnet")
	got = sb.getModelDisplay(20)
	if got != "claude-sonnet" {
		t.Errorf("getModelDisplay = %q, want %q", got, "claude-sonnet")
	}

	// Truncation needed
	sb.SetModel("very-long-model-name-that-needs-truncation")
	got = sb.getModelDisplay(15)
	if len(got) > 15 {
		t.Errorf("getModelDisplay length = %d, want <= 15", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("getModelDisplay should end with ..., got %q", got)
	}

	// Abbreviation for short maxLen
	sb.SetModel("claude-3-opus")
	got = sb.getModelDisplay(8)
	if got != "opus" {
		t.Errorf("getModelDisplay abbreviated = %q, want %q", got, "opus")
	}
}

func TestViewResponsive(t *testing.T) {
	sb := NewStatusBar()
	sb.SetModel("claude-sonnet")
	sb.SetTokens(23000, 128000)
	sb.SetCost(2) // $0.02
	sb.SetStartTime(time.Now().Add(-3*time.Minute - 42*time.Second))

	// Test full layout
	sb.SetSize(80)
	view := sb.View()
	// Should contain model name, bar, cost, duration
	if !strings.Contains(view, "claude-sonnet") {
		t.Errorf("full layout should contain model name, got: %s", view)
	}
	if !strings.Contains(view, "[") {
		t.Errorf("full layout should contain bar, got: %s", view)
	}
	if !strings.Contains(view, "$0.02") {
		t.Errorf("full layout should contain cost, got: %s", view)
	}

	// Test compact layout
	sb.SetSize(60)
	view = sb.View()
	// Should not contain visual bar in compact mode
	// But should still have percentage

	// Test minimal layout
	sb.SetSize(40)
	view = sb.View()
	// Should be very short, just model + percent
	if !strings.Contains(view, "sonnet") {
		t.Errorf("minimal layout should contain abbreviated model name, got: %s", view)
	}
	// Should not contain dollar sign in minimal
	if strings.Contains(view, "$") {
		t.Errorf("minimal layout should not contain cost, got: %s", view)
	}
}

func TestRenderBar(t *testing.T) {
	sb := NewStatusBar()

	tests := []struct {
		width      int
		percent    int
		wantEmpty  bool
		wantPrefix string
		wantSuffix string
	}{
		{10, 0, false, "[", "]"},
		{10, 50, false, "[", "]"},
		{10, 100, false, "[", "]"},
		{8, 25, false, "[", "]"},
		{3, 50, false, "[", "]"}, // minimal useful bar: "[-]" or "[=]"
		{2, 50, true, "", ""},    // too small (no room for content), returns empty
		{1, 50, true, "", ""},    // too small, returns empty
		{0, 50, true, "", ""},    // too small, returns empty
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			bar := sb.renderBar(tt.width, tt.percent)
			if tt.wantEmpty {
				if bar != "" {
					t.Errorf("bar for width %d should be empty, got: %q", tt.width, bar)
				}
				return
			}
			if !strings.HasPrefix(bar, tt.wantPrefix) || !strings.HasSuffix(bar, tt.wantSuffix) {
				t.Errorf("bar should be wrapped in brackets, got: %q", bar)
			}
		})
	}
}

func TestUpdate(t *testing.T) {
	sb := NewStatusBar()
	cmd := sb.Update(nil)
	if cmd != nil {
		t.Error("Update should return nil")
	}
}
