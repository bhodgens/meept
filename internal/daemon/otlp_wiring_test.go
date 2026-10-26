package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// TestWireOTLPExporter_DisabledIsInert pins the opt-in posture at the
// wiring seam: metrics.otlp.enabled=false (the default) constructs NO
// exporter, and the boot log carries exactly ONE Warn line mentioning the
// disabled state — zero steady-state noise.
func TestWireOTLPExporter_DisabledIsInert(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	c := &Components{
		Config: &config.Config{},
		Logger: logger,
	}
	c.wireOTLPExporter(logger)

	if c.OTLPExporter != nil {
		t.Fatal("OTLPExporter constructed with metrics.otlp.enabled=false — knob not gating the constructor")
	}

	warns := 0
	for _, line := range bytes.Split(logBuf.Bytes(), []byte("\n")) {
		if bytes.Contains(line, []byte("OTLP metrics export disabled")) {
			warns++
		}
	}
	if warns != 1 {
		t.Errorf("disabled boot logged %d 'OTLP metrics export disabled' lines, want exactly 1:\n%s", warns, logBuf.String())
	}

	// stopOTLPExporter on nil must be a clean no-op.
	c.stopOTLPExporter()
}

// TestWireOTLPExporter_EnabledExports pins the enabled path through the
// REAL wiring function: exporter constructed, started, and metric batches
// reach the configured receiver endpoint.
func TestWireOTLPExporter_EnabledExports(t *testing.T) {
	var hits atomic.Int32
	var mu sync.Mutex
	var batch []map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		var p struct {
			Metrics []map[string]any `json:"metrics"`
		}
		_ = json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		batch = append(batch, p.Metrics...)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := config.DefaultConfig()
	cfg.Metrics.OTLP.Enabled = true
	cfg.Metrics.OTLP.Endpoint = srv.URL
	cfg.Metrics.OTLP.Interval = 50 * time.Millisecond

	c := &Components{Config: cfg, Logger: logger}
	c.wireOTLPExporter(logger)
	if c.OTLPExporter == nil {
		t.Fatal("OTLPExporter nil with metrics.otlp.enabled=true — exporter not wired")
	}
	if !c.OTLPExporter.IsStarted() {
		t.Fatal("exporter not started by wiring")
	}

	// Drive one real record + batch cycle through the started exporter.
	c.OTLPExporter.RecordToolCall("wired-tool")
	c.OTLPExporter.RecordTokenUsage(42)

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(batch)
		mu.Unlock()
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("receiver got %d metrics, want >= 2 (immediate + batch). hits=%d\nlog:\n%s",
				n, hits.Load(), logBuf.String())
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Shutdown flushes and closes via the REAL shutdown path helper.
	c.stopOTLPExporter()
	if c.OTLPExporter.IsStarted() {
		t.Error("exporter still started after stopOTLPExporter")
	}
}

// TestStopComponentsStopsOTLPExporter proves stopComponents (the daemon's
// real shutdown path) stops a wired exporter and tolerates a nil one.
func TestStopComponentsStopsOTLPExporter(t *testing.T) {
	// Nil case: must not panic.
	c := &Components{
		Config: &config.Config{},
		Logger: slog.Default(),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop with nil exporter: %v", err)
	}

	// Wired case: Stop must leave the exporter stopped.
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg := config.DefaultConfig()
	cfg.Metrics.OTLP.Enabled = true
	cfg.Metrics.OTLP.Endpoint = "http://127.0.0.1:1" // unreachable; Stop must not hang
	cfg.Metrics.OTLP.Interval = time.Hour

	c2 := &Components{Config: cfg, Logger: logger}
	c2.wireOTLPExporter(logger)
	if c2.OTLPExporter == nil {
		t.Fatal("exporter not wired")
	}
	c2.ctx, c2.cancel = context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		_ = c2.Stop(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() hung with a wired OTLP exporter")
	}
	if c2.OTLPExporter.IsStarted() {
		t.Error("exporter still started after Components.Stop")
	}
}
