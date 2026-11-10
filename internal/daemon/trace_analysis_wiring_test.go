package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/scheduler"
)

// writeTraceAnalysisFixture writes a two-trace JSONL fixture: one trace
// with an error span (tool_error finding) and one refusal span (refusal
// finding), plus a repeated tool (redundant_args finding).
func writeTraceAnalysisFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "traces.jsonl")
	type span struct {
		TraceID   string `json:"trace_id"`
		SpanID    string `json:"span_id"`
		StartTime string `json:"start_time"`
		EndTime   string `json:"end_time"`
		Service   string `json:"service,omitempty"`
		Model     string `json:"model,omitempty"`
		HasError  bool   `json:"has_error,omitempty"`
		ErrorType string `json:"error_type,omitempty"`
		ToolName  string `json:"tool_name,omitempty"`
	}
	now := time.Now().UTC().Format(time.RFC3339)
	spans := []span{
		{TraceID: "trace-a", SpanID: "a-1", StartTime: now, EndTime: now, ToolName: "shell_execute", HasError: true, ErrorType: "exit_nonzero", Model: "test"},
		{TraceID: "trace-a", SpanID: "a-2", StartTime: now, EndTime: now, ToolName: "web_fetch", HasError: true, ErrorType: "timeout", Model: "test"},
		{TraceID: "trace-b", SpanID: "b-1", StartTime: now, EndTime: now, ToolName: "tool_denied_by_policy", Model: "test"},
		{TraceID: "trace-b", SpanID: "b-2", StartTime: now, EndTime: now, ToolName: "read_file", Model: "test"},
		{TraceID: "trace-b", SpanID: "b-3", StartTime: now, EndTime: now, ToolName: "read_file", Model: "test"},
		{TraceID: "trace-b", SpanID: "b-4", StartTime: now, EndTime: now, ToolName: "read_file", Model: "test"},
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for _, s := range spans {
		if err := enc.Encode(s); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestTraceAnalysisDisabledByDefault pins the kill-switch posture: the
// config default and a Components construction with the knob untouched
// must register no trace-analysis job (deadcode wave-2 WIRE contract).
func TestTraceAnalysisDisabledByDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.SelfImprove.TraceAnalysis.Enabled {
		t.Fatal("selfimprove.trace_analysis.enabled must default to false (opt-in)")
	}
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)
	tmp := t.TempDir()
	cfg.Daemon.DataDir = tmp

	comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}
	if comps.Scheduler == nil {
		t.Fatal("scheduler should exist by default")
	}
	if _, ok := comps.Scheduler.GetJob(traceAnalysisJobID); ok {
		t.Fatal("trace-analysis job must NOT be registered when the knob is off (default)")
	}
}

// TestTraceAnalysisWiringRegistersSchedulerJob pins the enabled path
// through the REAL NewComponents construction: knob on -> the periodic
// job exists on the live scheduler with an @every schedule and interval
// job type.
func TestTraceAnalysisWiringRegistersSchedulerJob(t *testing.T) {
	fixture := writeTraceAnalysisFixture(t)
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)
	tmp := t.TempDir()

	cfg := config.DefaultConfig()
	cfg.Daemon.DataDir = tmp
	cfg.SelfImprove.Enabled = true
	cfg.SelfImprove.TraceAnalysis.Enabled = true
	cfg.SelfImprove.TraceAnalysis.TraceStorePath = fixture
	cfg.SelfImprove.TraceAnalysis.Interval = time.Hour

	comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}
	if comps.Scheduler == nil {
		t.Fatal("scheduler should exist by default")
	}
	job, ok := comps.Scheduler.GetJob(traceAnalysisJobID)
	if !ok {
		t.Fatal("trace-analysis job must be registered when the knob is on")
	}
	if job.Type != scheduler.JobTypeInterval {
		t.Fatalf("job type: got %s, want interval", job.Type)
	}
	if job.Schedule != "@every 1h0m0s" {
		t.Fatalf("schedule: got %s, want @every 1h0m0s", job.Schedule)
	}
}

// TestTraceAnalysisJobNotRegisteredWithoutScheduler pins the guard: knob
// on but scheduler disabled -> no panic, no job, warn-skip.
func TestTraceAnalysisJobNotRegisteredWithoutScheduler(t *testing.T) {
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)
	tmp := t.TempDir()

	cfg := config.DefaultConfig()
	cfg.Daemon.DataDir = tmp
	cfg.SelfImprove.Enabled = true
	cfg.SelfImprove.TraceAnalysis.Enabled = true
	cfg.Scheduler.Enabled = false

	comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}
	if comps.Scheduler != nil {
		t.Fatal("scheduler must be nil when cfg.Scheduler.Enabled is false")
	}
}

// TestResolveTraceStorePath pins the path resolution: explicit config path
// wins; empty falls back to <data_dir>/traces.jsonl.
func TestResolveTraceStorePath(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Daemon.DataDir = t.TempDir()

	if got, want := resolveTraceStorePath(cfg), filepath.Join(cfg.Daemon.DataDir, "traces.jsonl"); got != want {
		t.Fatalf("default: got %s, want %s", got, want)
	}
	cfg.SelfImprove.TraceAnalysis.TraceStorePath = "/custom/traces.jsonl"
	if got := resolveTraceStorePath(cfg); got != "/custom/traces.jsonl" {
		t.Fatalf("explicit: got %s", got)
	}
}
