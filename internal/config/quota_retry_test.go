package config

import (
	"testing"
	"time"
)

func TestQuotaRetryConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	qrc := cfg.LLM.QuotaRetry
	if !qrc.Enabled {
		t.Errorf("QuotaRetry.Enabled = false, want true")
	}
	if qrc.MaxWait != DefaultQuotaRetryMaxWait {
		t.Errorf("QuotaRetry.MaxWait = %v, want %v", qrc.MaxWait, DefaultQuotaRetryMaxWait)
	}
	if qrc.DefaultEstimate != DefaultQuotaRetryDefaultEstimate {
		t.Errorf("QuotaRetry.DefaultEstimate = %v, want %v", qrc.DefaultEstimate, DefaultQuotaRetryDefaultEstimate)
	}
	if qrc.DeferCheckInterval != DefaultQuotaRetryDeferCheckInterval {
		t.Errorf("QuotaRetry.DeferCheckInterval = %v, want %v", qrc.DeferCheckInterval, DefaultQuotaRetryDeferCheckInterval)
	}
}

func TestNormalizeQuotaRetryDefaults(t *testing.T) {
	tests := []struct {
		name  string
		input QuotaRetryConfig
		want  QuotaRetryConfig
	}{
		{
			name:  "zero value gets defaults",
			input: QuotaRetryConfig{},
			want: QuotaRetryConfig{
				MaxWait:            DefaultQuotaRetryMaxWait,
				DefaultEstimate:    DefaultQuotaRetryDefaultEstimate,
				DeferCheckInterval: DefaultQuotaRetryDeferCheckInterval,
			},
		},
		{
			name: "explicit enabled false stays false",
			input: QuotaRetryConfig{
				Enabled: false,
			},
			want: QuotaRetryConfig{
				Enabled:            false,
				MaxWait:            DefaultQuotaRetryMaxWait,
				DefaultEstimate:    DefaultQuotaRetryDefaultEstimate,
				DeferCheckInterval: DefaultQuotaRetryDeferCheckInterval,
			},
		},
		{
			name: "partial override keeps defaults for others",
			input: QuotaRetryConfig{
				Enabled:         true,
				MaxWait:         2 * time.Hour,
				DefaultEstimate: 30 * time.Minute,
			},
			want: QuotaRetryConfig{
				Enabled:            true,
				MaxWait:            2 * time.Hour,
				DefaultEstimate:    30 * time.Minute,
				DeferCheckInterval: DefaultQuotaRetryDeferCheckInterval,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.input
			NormalizeQuotaRetryDefaults(&got)
			if got.Enabled != tt.want.Enabled {
				t.Errorf("Enabled = %v, want %v", got.Enabled, tt.want.Enabled)
			}
			if got.MaxWait != tt.want.MaxWait {
				t.Errorf("MaxWait = %v, want %v", got.MaxWait, tt.want.MaxWait)
			}
			if got.DefaultEstimate != tt.want.DefaultEstimate {
				t.Errorf("DefaultEstimate = %v, want %v", got.DefaultEstimate, tt.want.DefaultEstimate)
			}
			if got.DeferCheckInterval != tt.want.DeferCheckInterval {
				t.Errorf("DeferCheckInterval = %v, want %v", got.DeferCheckInterval, tt.want.DeferCheckInterval)
			}
		})
	}
}

func TestQuotaRetryConfigZeroValueSafe(t *testing.T) {
	// Zero-value should not panic and should apply defaults
	var zero QuotaRetryConfig
	NormalizeQuotaRetryDefaults(&zero)
	if zero.MaxWait != DefaultQuotaRetryMaxWait {
		t.Errorf("MaxWait = %v, want %v", zero.MaxWait, DefaultQuotaRetryMaxWait)
	}
}
