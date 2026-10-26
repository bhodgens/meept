// Package metrics — otlp_http_sink.go: concrete OTLPMetricSink that POSTs
// MetricDatum batches as OTLP/HTTP JSON to the collector's /v1/metrics
// endpoint. Stdlib net/http only — the OTLPMetricSink bridge exists
// precisely to keep the otel SDK out of the dependency tree (go.mod stays
// untouched).
package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// OTLPHTTPSink implements OTLPMetricSink over plain HTTP POST. One batch =
// one request; per-request failures are logged (once per consecutive-failure
// streak to avoid steady-state log noise) and the batch is dropped — export
// is best-effort by design and never blocks the caller (Record* paths run
// off the agent loop via the immediate-export path).
type OTLPHTTPSink struct {
	endpoint string // full URL: <base>/v1/metrics
	client   *http.Client
	logger   *slog.Logger
	timeout  time.Duration

	mu          sync.Mutex
	consecFails int
}

// Compile-time bridge check.
var _ OTLPMetricSink = (*OTLPHTTPSink)(nil)

// NewOTLPHTTPSink builds a sink that POSTs to <endpoint>/v1/metrics. The
// endpoint is the OTLP receiver BASE URL (e.g. http://localhost:4317) —
// the /v1/metrics suffix is appended here, standard OTLP/HTTP path.
func NewOTLPHTTPSink(endpoint string, logger *slog.Logger) *OTLPHTTPSink {
	if logger == nil {
		logger = slog.Default()
	}
	endpoint = strings.TrimRight(endpoint, "/")
	return &OTLPHTTPSink{
		endpoint: endpoint + "/v1/metrics",
		client:   &http.Client{Timeout: 10 * time.Second},
		logger:   logger.With("component", "otlp-http-sink"),
		timeout:  10 * time.Second,
	}
}

// otlpExportPayload is the JSON wire shape POSTed per batch. Deliberately
// simple (name/value/type/timestamp/tags per datum) — this is meept's own
// OTLP-flavored export, not a spec-complete protobuf translation.
type otlpExportPayload struct {
	ServiceName string             `json:"service_name"`
	Metrics     []otlpExportMetric `json:"metrics"`
}

type otlpExportMetric struct {
	Name      string            `json:"name"`
	Value     float64           `json:"value"`
	Type      MetricType        `json:"type"`
	Timestamp time.Time         `json:"timestamp"`
	Tags      map[string]string `json:"tags,omitempty"`
}

// ExportMetrics serializes the batch and POSTs it to the collector.
// Context-aware: an already-canceled ctx (daemon shutdown) skips the send.
func (s *OTLPHTTPSink) ExportMetrics(ctx context.Context, data []MetricDatum) error {
	if len(data) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("otlp export skipped (context done): %w", err)
	}

	payload := otlpExportPayload{
		ServiceName: "meept-daemon",
		Metrics:     make([]otlpExportMetric, 0, len(data)),
	}
	for _, d := range data {
		payload.Metrics = append(payload.Metrics, otlpExportMetric{
			Name:      d.Name,
			Value:     d.Value,
			Type:      d.Type,
			Timestamp: d.Timestamp,
			Tags:      d.Tags,
		})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		s.noteFailure(fmt.Errorf("marshal batch: %w", err))
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		s.noteFailure(err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		s.noteFailure(err)
		return err
	}
	// Drain + close are best-effort cleanup on a body we never read; a
	// drain failure cannot change the export outcome (status is checked
	// below and errors already flow through noteFailure). The drain error
	// is intentionally not returned — closing a response body is the
	// canonical no-op-on-error cleanup in net/http usage.
	bodyDrainAndClose(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		err := fmt.Errorf("otlp receiver returned status %d", resp.StatusCode)
		s.noteFailure(err)
		return err
	}

	s.noteSuccess()
	return nil
}

// bodyDrainAndClose drains and closes an HTTP response body. It never
// fails the caller: drain errors are logged at Debug (connection reuse
// quality signal only) and the close error is ignored by contract
// (net/http bodies are closable even mid-error).
func bodyDrainAndClose(body io.ReadCloser) {
	_, copyErr := io.Copy(io.Discard, body)
	closeErr := body.Close()
	if copyErr != nil || closeErr != nil {
		slog.Debug("otlp sink: response body drain/close issue",
			"copy_error", copyErr, "close_error", closeErr)
	}
}

// Close is a no-op: the sink holds no persistent connections or buffers
// (each ExportMetrics is a self-contained request). Present to satisfy
// OTLPMetricSink.
func (s *OTLPHTTPSink) Close() error { return nil }

// noteFailure logs at Warn only on the FIRST failure of a consecutive
// streak; recovery logs one Info line. This keeps a dead receiver from
// producing steady-state log noise (every 30s forever) while still telling
// the operator the export path is down.
func (s *OTLPHTTPSink) noteFailure(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consecFails++
	if s.consecFails == 1 {
		s.logger.Warn("otlp metrics export failing", "endpoint", s.endpoint, "error", err)
	}
}

// noteSuccess resets the failure streak, logging once when a streak ends.
func (s *OTLPHTTPSink) noteSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.consecFails > 0 {
		s.logger.Info("otlp metrics export recovered",
			"endpoint", s.endpoint, "consecutive_failures", s.consecFails)
	}
	s.consecFails = 0
}
