// Package daemon — otlp_wiring.go: construction wiring for the opt-in
// OTLP metrics exporter (metrics.otlp). Split from components.go so the
// giant NewComponents body stays untouched; called from NewComponents
// immediately after the Components struct is initialized, BEFORE any
// other construction, so the config knob gates the constructor entirely
// (no construction-baked config seam).
package daemon

import (
	"log/slog"

	"github.com/caimlas/meept/internal/metrics"
)

// wireOTLPExporter constructs and starts the OTLP exporter when
// metrics.otlp.enabled=true. Behavior per product posture
// (docs/agents/opt-in-defaults.md):
//
//   - disabled (the default): exactly ONE Warn log line at boot, no
//     exporter, no goroutines, no network traffic. Metrics stay in the
//     per-host SQLite store (metrics.db).
//   - enabled: exporter built with the stdlib OTLP/HTTP JSON sink and
//     started; a sink construction failure downgrades to the disabled
//     posture (one Warn line, honest about the failure). Producers are
//     fed by Collector.RegisterOTLPExporter (daemon.go): typed agent
//     events (tool executions, turn ends, token usage) reach the
//     exporter's Record* methods.
//
// Never returns an error: metrics export is best-effort and must not
// block daemon boot.
func (c *Components) wireOTLPExporter(logger *slog.Logger) {
	otlpCfg := c.Config.Metrics.OTLP
	if !otlpCfg.Enabled {
		// One honest line at boot; nothing else for the lifetime of the
		// process (no steady-state noise).
		logger.Warn("OTLP metrics export disabled (metrics.otlp.enabled=false); metrics stay local",
			"endpoint", otlpCfg.Endpoint)
		return
	}

	sink := metrics.NewOTLPHTTPSink(otlpCfg.Endpoint, logger)
	exp := metrics.NewOTLPExporter(metrics.OTLPExporterConfig{
		Endpoint:       otlpCfg.Endpoint,
		ServiceName:    "meept-daemon",
		ExportInterval: otlpCfg.Interval, // config-load normalization clamps zero/negative to 30s
		Enabled:        true,
	}, sink)

	// Start is idempotent (atomic CAS) and spawns exactly one export
	// goroutine. The exporter's logger prefix keeps runtime lines
	// attributable without extra per-export logging.
	exp.Start()
	c.OTLPExporter = exp

	logger.Info("OTLP metrics export enabled",
		"endpoint", otlpCfg.Endpoint,
		"interval", otlpCfg.Interval.String(),
	)
}

// stopOTLPExporter flushes remaining batches and releases the sink.
// Safe on nil (every disabled boot); called from stopComponents on the
// daemon shutdown path so the final batch reaches the receiver.
func (c *Components) stopOTLPExporter() {
	if c.OTLPExporter != nil {
		c.OTLPExporter.Stop()
		c.Logger.Info("OTLP metrics exporter stopped")
	}
}
