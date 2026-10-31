// Package config — otlp_traces.go: OTLPTracesConfig (leaf 07-langfuse-sink,
// contract C5). Lives in its own file rather than schema.go so it cannot
// collide with concurrent sibling WIP hunks in that shared file; same
// package, same gendoc conventions.
//
// Sibling of metrics.otlp (opt-in, default OFF per docs/agents/opt-in-
// defaults.md): when disabled there is NO exporter, NO span producer, and
// ZERO network activity — the wiring constructs nothing at all.
package config

import "time"

// DefaultOTLPTracesEndpoint is the base URL used when Endpoint is empty at
// normalization time (a plain OTLP receiver; a Langfuse install would point
// at http://langfuse:3000/api/public/otel).
const DefaultOTLPTracesEndpoint = "http://localhost:4318"

// OTLPTracesConfig configures the opt-in OTLP/HTTP trace exporter
// (metrics.otlp_traces). Disabled by default: no spans are produced, no
// network calls are made, and boot logs exactly one Warn line stating that
// trace export is disabled. When enabled, chat turns and task steps become
// OTLP/HTTP protobuf spans POSTed to <endpoint>/v1/traces (stdlib
// net/http only — no otel SDK dependency;
// internal/metrics.OTLPTraceHTTPSink is the bridge). Aimed at (but not
// coupled to) self-hosted Langfuse v3+, which accepts OTLP traces at
// /api/public/otel with the "x-langfuse-ingestion-version: 4" header
// (pass it via Headers).
//
//gendoc:section metrics
//gendoc:desc OTLP trace export (metrics.otlp_traces): opt-in turn/step span export to an OTLP receiver or self-hosted Langfuse.
//gendoc:example [metrics.otlp_traces] enabled = true, endpoint = "http://langfuse:3000/api/public/otel"
type OTLPTracesConfig struct {
	// Enabled turns the trace exporter on. Default false — opt-in.
	Enabled bool `json:"enabled" toml:"enabled"`
	// Endpoint is the OTLP receiver base URL (spans POST to
	// <endpoint>/v1/traces). Default "http://localhost:4318" when
	// enabled with no explicit value (normalized at the load boundary).
	Endpoint string `json:"endpoint" toml:"endpoint"`
	// Headers are extra HTTP headers attached to every export request
	// (auth, x-langfuse-ingestion-version: 4, ...). Secret material
	// arrives ONLY here and is never logged.
	Headers map[string]string `json:"headers,omitempty" toml:"headers,omitempty"`
	// SampleRate is the fraction of traces exported, 0..1. Default 1.0;
	// values outside the range clamp to it. The decision is
	// deterministic per trace ID, so all spans of one trace sample
	// identically.
	SampleRate float64 `json:"sample_rate,omitempty" toml:"sample_rate,omitempty"`
	// ExportInterval is the batch flush cadence. Zero/negative means the
	// 5s default (normalized at the config load boundary).
	ExportInterval time.Duration `json:"export_interval,omitempty" toml:"export_interval,omitempty"`
}

// Defaults for metrics.otlp_traces.
const (
	DefaultOTLPTracesSampleRate    = 1.0
	DefaultOTLPTracesExportIntrvl  = 5 * time.Second
	defaultOTLPTracesHeaderTrimLen = 0 // placeholder to keep the const block gofmt-stable
)

// NormalizeOTLPTracesDefaults clamps invalid metrics.otlp_traces values to
// defaults: an empty endpoint takes http://localhost:4318, a sample rate
// outside (0, 1] clamps (0 -> disabled-by-rate is NOT applied — the
// documented way to disable is enabled=false, so a zero rate means the
// default 1.0; negatives and >1 clamp to 1.0), and a zero/negative export
// interval takes the 5s default. Enabled is left as-is — default false is
// the documented opt-in posture. Idempotent on valid values.
func NormalizeOTLPTracesDefaults(c *OTLPTracesConfig) {
	if c.Endpoint == "" {
		c.Endpoint = DefaultOTLPTracesEndpoint
	}
	if c.SampleRate <= 0 || c.SampleRate > 1 {
		c.SampleRate = DefaultOTLPTracesSampleRate
	}
	if c.ExportInterval <= 0 {
		c.ExportInterval = DefaultOTLPTracesExportIntrvl
	}
}
