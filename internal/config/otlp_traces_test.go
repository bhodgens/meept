package config

import (
	"testing"
	"time"
)

// T2: metrics.otlp_traces normalization clamps defaults at the load
// boundary (contract C5 sibling of metrics.otlp).
func TestOTLPTracesDefaults(t *testing.T) {
	t.Run("zero values take defaults", func(t *testing.T) {
		var c OTLPTracesConfig
		NormalizeOTLPTracesDefaults(&c)
		if c.Endpoint != "http://localhost:4318" {
			t.Errorf("endpoint = %q", c.Endpoint)
		}
		if c.SampleRate != 1.0 {
			t.Errorf("sample_rate = %v, want 1.0", c.SampleRate)
		}
		if c.ExportInterval != 5*time.Second {
			t.Errorf("export_interval = %v, want 5s", c.ExportInterval)
		}
		if c.Enabled {
			t.Error("Enabled must never be flipped by normalization (opt-in)")
		}
	})

	t.Run("out-of-range sample rate clamps", func(t *testing.T) {
		c := OTLPTracesConfig{SampleRate: 1.5}
		NormalizeOTLPTracesDefaults(&c)
		if c.SampleRate != 1.0 {
			t.Errorf("sample_rate 1.5 -> %v, want 1.0", c.SampleRate)
		}
		c = OTLPTracesConfig{SampleRate: -2}
		NormalizeOTLPTracesDefaults(&c)
		if c.SampleRate != 1.0 {
			t.Errorf("sample_rate -2 -> %v, want 1.0", c.SampleRate)
		}
	})

	t.Run("valid values are idempotent", func(t *testing.T) {
		c := OTLPTracesConfig{
			Enabled:        true,
			Endpoint:       "http://langfuse:3000/api/public/otel",
			Headers:        map[string]string{"x-langfuse-ingestion-version": "4"},
			SampleRate:     0.25,
			ExportInterval: 10 * time.Second,
		}
		before := c.SampleRate
		NormalizeOTLPTracesDefaults(&c)
		if c.SampleRate != before || c.ExportInterval != 10*time.Second || c.Endpoint != "http://langfuse:3000/api/public/otel" {
			t.Errorf("normalization disturbed valid values: %+v", c)
		}
	})

	t.Run("default config leaves traces disabled", func(t *testing.T) {
		cfg := DefaultConfig()
		if cfg.Metrics.OTLPTraces.Enabled {
			t.Fatal("metrics.otlp_traces.enabled must default false (opt-in hard requirement)")
		}
	})
}
