package agent

import (
	"reflect"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// TestEffectiveFilterConfig verifies the daemon -> agent override chain
// (output-filters leaf 04 Task 2), mirroring verification-config semantics:
// agent scalars win when explicitly set; the agent Filters list replaces
// the daemon list only when non-empty.
func TestEffectiveFilterConfig(t *testing.T) {
	daemon := config.OutputFiltersConfig{
		Enabled:          true,
		MaxPasses:        2,
		MaxFilterRetries: 2,
		Filters:          []string{"json_format"},
	}

	tests := []struct {
		name  string
		agent *FilterConfig
		want  FilterConfig
	}{
		{
			name:  "daemon only: nil agent",
			agent: nil,
			want: FilterConfig{
				Enabled:          true,
				MaxPasses:        2,
				MaxFilterRetries: 2,
				Filters:          []string{"json_format"},
			},
		},
		{
			name:  "daemon only: zero-value agent",
			agent: &FilterConfig{},
			want: FilterConfig{
				Enabled:          true,
				MaxPasses:        2,
				MaxFilterRetries: 2,
				Filters:          []string{"json_format"},
			},
		},
		{
			name: "agent override: enabled + filters replace daemon",
			agent: &FilterConfig{
				Enabled: true,
				Filters: []string{"language_en", "lint_go"},
			},
			want: FilterConfig{
				Enabled:          true,
				MaxPasses:        2,
				MaxFilterRetries: 2,
				Filters:          []string{"language_en", "lint_go"},
			},
		},
		{
			name: "agent partial: only max_passes set",
			agent: &FilterConfig{
				MaxPasses: 5,
			},
			want: FilterConfig{
				Enabled:          true,
				MaxPasses:        5,
				MaxFilterRetries: 2,
				Filters:          []string{"json_format"},
			},
		},
		{
			name: "agent partial: only max_filter_retries set",
			agent: &FilterConfig{
				MaxFilterRetries: 7,
			},
			want: FilterConfig{
				Enabled:          true,
				MaxPasses:        2,
				MaxFilterRetries: 7,
				Filters:          []string{"json_format"},
			},
		},
		{
			name: "agent empty filters do not clear daemon list",
			agent: &FilterConfig{
				Enabled: true,
				Filters: nil,
			},
			want: FilterConfig{
				Enabled:          true,
				MaxPasses:        2,
				MaxFilterRetries: 2,
				Filters:          []string{"json_format"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EffectiveFilterConfig(daemon, tc.agent)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("EffectiveFilterConfig() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestEffectiveFilterConfig_DisabledByDefault pins the frozen default: a
// zero-value daemon config keeps the stage off.
func TestEffectiveFilterConfig_DisabledByDefault(t *testing.T) {
	got := EffectiveFilterConfig(config.OutputFiltersConfig{}, nil)
	if got.Enabled {
		t.Error("Enabled = true for zero-value daemon config; want false")
	}
	if got.MaxPasses != 0 || got.MaxFilterRetries != 0 {
		t.Errorf("caps = %d/%d, want 0/0 passthrough (defaults applied downstream)",
			got.MaxPasses, got.MaxFilterRetries)
	}
}

// TestDefaultFilterConfig verifies the default config mirrors the daemon
// defaults: disabled, caps 2/2, no filters.
func TestDefaultFilterConfig(t *testing.T) {
	got := DefaultFilterConfig()
	want := FilterConfig{Enabled: false, MaxPasses: 2, MaxFilterRetries: 2, Filters: nil}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DefaultFilterConfig() = %+v, want %+v", got, want)
	}
}

// TestMaxFilterRetriesOrDefault verifies the filterRetryLimiter seam
// default: 0 and negative fall back to 2 (master.md Contract 3), explicit
// values pass through.
func TestMaxFilterRetriesOrDefault(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"zero defaults to 2", 0, 2},
		{"negative defaults to 2", -3, 2},
		{"explicit 1 passes through", 1, 1},
		{"explicit 5 passes through", 5, 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fc := FilterConfig{MaxFilterRetries: tc.in}
			if got := fc.MaxFilterRetriesOrDefault(); got != tc.want {
				t.Errorf("MaxFilterRetriesOrDefault(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}
