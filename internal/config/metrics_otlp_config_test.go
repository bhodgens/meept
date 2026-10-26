package config

import (
	"os"
	"testing"
	"time"
)

// TestMetricsOTLPDefaults pins the opt-in posture of metrics.otlp: the
// DefaultConfig baseline has Enabled=false (nothing leaves the host), an
// empty endpoint, and a zero interval. Load-boundary normalization clamps
// interval/endpoint to their defaults while Enabled stays false.
func TestMetricsOTLPDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Metrics.OTLP.Enabled {
		t.Error("Default metrics.otlp.enabled should be false (opt-in posture)")
	}
	if cfg.Metrics.OTLP.Endpoint != "" {
		t.Errorf("Default metrics.otlp.endpoint = %q, want empty before normalization", cfg.Metrics.OTLP.Endpoint)
	}
	if cfg.Metrics.OTLP.Interval != 0 {
		t.Errorf("Default metrics.otlp.interval = %v, want 0 before normalization", cfg.Metrics.OTLP.Interval)
	}

	// Normalization is what the loader applies: zero -> defaults.
	NormalizeOTLPMetricsDefaults(&cfg.Metrics.OTLP)
	if cfg.Metrics.OTLP.Endpoint != DefaultOTLPEndpoint {
		t.Errorf("normalized endpoint = %q, want %q", cfg.Metrics.OTLP.Endpoint, DefaultOTLPEndpoint)
	}
	if cfg.Metrics.OTLP.Interval != DefaultOTLPExportInterval {
		t.Errorf("normalized interval = %v, want %v", cfg.Metrics.OTLP.Interval, DefaultOTLPExportInterval)
	}
	if cfg.Metrics.OTLP.Enabled {
		t.Error("normalization must not flip enabled on")
	}

	// Idempotent on valid values.
	NormalizeOTLPMetricsDefaults(&cfg.Metrics.OTLP)
	if cfg.Metrics.OTLP.Endpoint != DefaultOTLPEndpoint || cfg.Metrics.OTLP.Interval != DefaultOTLPExportInterval {
		t.Error("NormalizeOTLPMetricsDefaults not idempotent")
	}
}

// TestMetricsOTLPRoundtrip loads a JSON5 config that enables the exporter
// with an explicit endpoint + duration-string interval, proving the knob
// survives the real load path (duration preprocessing, JSON5 comments) and
// reaches the consumer with final values.
func TestMetricsOTLPRoundtrip(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "meept-otlp-*.json5")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	content := `{
  // scratch config: metrics.otlp enabled
  "metrics": {
    "otlp": {
      "enabled": true,
      "endpoint": "http://collector:4318",
      "interval": "5s",
    },
  },
}`
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadJSON5Config(f.Name())
	if err != nil {
		t.Fatalf("LoadJSON5Config failed: %v", err)
	}
	if !cfg.Metrics.OTLP.Enabled {
		t.Error("metrics.otlp.enabled = false, want true after override")
	}
	if cfg.Metrics.OTLP.Endpoint != "http://collector:4318" {
		t.Errorf("metrics.otlp.endpoint = %q, want http://collector:4318", cfg.Metrics.OTLP.Endpoint)
	}
	if cfg.Metrics.OTLP.Interval != 5*time.Second {
		t.Errorf("metrics.otlp.interval = %v, want 5s (duration string must load)", cfg.Metrics.OTLP.Interval)
	}
}

// TestMetricsOTLPRoundtripDisabledOverride proves a user can explicitly
// disable after the template comment block, and that interval defaults
// apply when the user sets only enabled.
func TestMetricsOTLPRoundtripDisabledOverride(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "meept-otlp-*.json5")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	content := `{
  "metrics": { "otlp": { "enabled": false } },
}`
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadJSON5Config(f.Name())
	if err != nil {
		t.Fatalf("LoadJSON5Config failed: %v", err)
	}
	if cfg.Metrics.OTLP.Enabled {
		t.Error("metrics.otlp.enabled = true, want false")
	}
	// Normalized at the load boundary even when disabled.
	if cfg.Metrics.OTLP.Interval != DefaultOTLPExportInterval {
		t.Errorf("interval = %v, want normalized %v", cfg.Metrics.OTLP.Interval, DefaultOTLPExportInterval)
	}
	if cfg.Metrics.OTLP.Endpoint != DefaultOTLPEndpoint {
		t.Errorf("endpoint = %q, want normalized %q", cfg.Metrics.OTLP.Endpoint, DefaultOTLPEndpoint)
	}
}
