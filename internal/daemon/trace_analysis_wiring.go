// Package daemon — trace_analysis_wiring.go wires the HALO RLM
// trace-analysis activation path (deadcode-triage report-agent wave-2):
// memory.LoadTraceStore reader → agent.NewRLMAnalyzerFromTraceStore →
// selfimprove.IssueDetector.ScanTraces, registered as a periodic job on
// the daemon scheduler. Gated on [selfimprove].trace_analysis.enabled
// (default false, opt-in): a disabled knob constructs nothing and the
// boot path is byte-identical.
package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/scheduler"
	"github.com/caimlas/meept/internal/selfimprove"
)

// traceAnalysisJobID is the scheduler job id for the periodic scan.
// Interval jobs are transient in the persistent store (loadPersistedJobs
// drops JobTypeInterval rows), so re-registration at each boot is the
// intended lifecycle.
const traceAnalysisJobID = "selfimprove-trace-analysis"

// resolveTraceStorePath returns the configured trace JSONL source, or the
// daemon-data-dir default when the knob is empty. Supports "~/..." via
// os.UserHomeDir (expandConfigPaths does not cover this section).
func resolveTraceStorePath(cfg *config.Config) string {
	p := cfg.SelfImprove.TraceAnalysis.TraceStorePath
	if p == "" {
		p = filepath.Join(cfg.Daemon.DataDir, "traces.jsonl")
	}
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				p = home
			} else {
				p = filepath.Join(home, strings.TrimPrefix(p, "~/"))
			}
		}
	}
	return p
}

// detectionCfgFromSchema maps the schema DetectionConfig onto the runtime
// selfimprove.DetectionConfig for the trace-analysis detector. Only the
// log/error pattern fields matter for ScanTraces (it does not consult the
// scan toggles), but the full mapping keeps the detector consistent with
// the controller's mapping in components.go.
func detectionCfgFromSchema(d config.DetectionConfig) selfimprove.DetectionConfig {
	return selfimprove.DetectionConfig{
		LogPatterns: []string{d.LogFile},
		ErrorPatterns: []string{
			"ERROR",
			"FATAL",
			"panic:",
			"exception:",
		},
		ScanPytest:       d.ScanPytest,
		ScanLint:         d.ScanLint,
		ScanTypeCheck:    d.ScanTypeCheck,
		ScanRuntimeLogs:  d.ScanRuntimeLogs,
		LogFile:          d.LogFile,
		LogLookbackHours: d.LogLookbackHours,
		PytestArgs:       d.PytestArgs,
		MypyArgs:         d.MypyArgs,
		RuffArgs:         d.RuffArgs,
	}
}

// runTraceAnalysis executes one scan: load the trace store over the JSONL
// source, run the RLM analyzer (deterministic scan when no LLM client is
// available — the analyzer's built-in fallback), then run
// IssueDetector.ScanTraces and log surfaced findings. Errors are logged,
// never fatal: a missing or unreadable trace file must not disturb the
// daemon background loop.
func runTraceAnalysis(store *memory.TraceStore, detector *selfimprove.IssueDetector, analyzer *agent.RLMAnalyzer, tracePath string, logger *slog.Logger) {
	if detector == nil || logger == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if analyzer != nil {
		result, err := analyzer.Analyze(ctx, "Analyze execution traces for failure patterns.")
		if err != nil {
			logger.Warn("trace analysis: RLM analyze failed", "error", err)
		} else if result != nil && len(result.FailureModes) > 0 {
			logger.Info("trace analysis: RLM failure modes",
				"count", len(result.FailureModes),
				"report_chars", len(result.Report),
			)
		}
	}

	issues, err := detector.ScanTraces(ctx, tracePath)
	if err != nil {
		logger.Warn("trace analysis: scan failed", "path", tracePath, "error", err)
		return
	}
	for _, issue := range issues {
		logger.Warn("trace analysis: quality issue",
			"id", issue.ID,
			"type", string(issue.Type),
			"severity", string(issue.Severity),
			"description", issue.Description,
			"category", issue.Metadata["category"],
			"trace_id", issue.Metadata["trace_id"],
		)
	}
	if len(issues) == 0 {
		logger.Debug("trace analysis: no issues found", "path", tracePath)
	}
}

// wireTraceAnalysis registers the periodic trace-analysis job on the
// daemon scheduler when [selfimprove].trace_analysis.enabled is true.
// Requirements: a live scheduler (the job rides cfg.Scheduler.Enabled).
// The trace store reader + RLM analyzer + IssueDetector are constructed
// here; ScanTraces itself re-loads the store per call (its contract), so
// the job callback stays self-contained across rotations of the JSONL.
//
// best-effort: construction or scheduling failures log a Warn and skip —
// trace analysis must never block or fail daemon startup.
func (c *Components) wireTraceAnalysis(cfg *config.Config, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	if !cfg.SelfImprove.Enabled || !cfg.SelfImprove.TraceAnalysis.Enabled {
		return
	}
	if c.Scheduler == nil {
		logger.Warn("trace analysis enabled but scheduler unavailable; skipping")
		return
	}

	tracePath := resolveTraceStorePath(cfg)

	// Reader construction is best-effort: the JSONL may not exist yet on a
	// fresh install. Defer to job-execution time instead of failing wiring —
	// build the analyzer lazily inside the callback when the file exists.
	interval := cfg.SelfImprove.TraceAnalysis.Interval
	if interval <= 0 {
		interval = 6 * time.Hour
	}

	job := &traceAnalysisJob{
		id:       traceAnalysisJobID,
		name:     "selfimprove trace analysis (RLM scan)",
		interval: interval,
		fn: func() {
			store, err := memory.LoadTraceStore(tracePath)
			if err != nil {
				logger.Warn("trace analysis: trace store unavailable",
					"path", tracePath, "error", err)
				return
			}
			var llm *agent.RLMAnalyzer
			if c.LLMClient != nil {
				analyzerCfg := agent.DefaultRLMConfig()
				llm = agent.NewRLMAnalyzerFromTraceStore(analyzerCfg, store, c.LLMClient, logger.With("component", "rlm-analyzer"))
				if c.PiiRestoreHook != nil {
					llm.SetPiiMaskHook(c.PiiRestoreHook)
				}
			}
			detector := selfimprove.NewIssueDetector(detectionCfgFromSchema(cfg.SelfImprove.Detection), "", logger.With("component", "issue-detector"))
			runTraceAnalysis(store, detector, llm, tracePath, logger)
		},
	}
	if _, err := c.Scheduler.Schedule(job); err != nil {
		logger.Warn("trace analysis: schedule failed", "job_id", traceAnalysisJobID, "error", err)
		return
	}
	logger.Info("Trace analysis scheduled",
		"job_id", traceAnalysisJobID,
		"interval", interval.String(),
		"trace_store", tracePath,
	)
}

// traceAnalysisJob is a minimal scheduler.Job firing a fixed callback on an
// "@every <interval>" cron expression. Mirrors simpleIntervalJob but is
// local to the trace-analysis wiring so the job's Config() carries its own
// tags (the scheduler persists + drops interval rows per its transient
// contract).
type traceAnalysisJob struct {
	id       string
	name     string
	interval time.Duration
	fn       func()
}

// ID implements scheduler.Job.
func (j *traceAnalysisJob) ID() string { return j.id }

// Name implements scheduler.Job.
func (j *traceAnalysisJob) Name() string { return j.name }

// Schedule implements scheduler.Job: cron "@every <interval>".
func (j *traceAnalysisJob) Schedule() string { return fmt.Sprintf("@every %s", j.interval) }

// Type implements scheduler.Job.
func (j *traceAnalysisJob) Type() scheduler.JobType { return scheduler.JobTypeInterval }

// Execute implements scheduler.Job: run the callback with panic isolation
// so a bad trace file never takes down the scheduler's cron goroutine.
func (j *traceAnalysisJob) Execute(_ context.Context) error {
	defer func() {
		if r := recover(); r != nil {
			// Panics here are logged, not propagated: cron swallows worker
			// panics unrecoverably.
			fmt.Printf("trace analysis job panic: %v\n", r)
		}
	}()
	j.fn()
	return nil
}

// Config implements scheduler.Job.
func (j *traceAnalysisJob) Config() scheduler.JobConfig {
	return scheduler.JobConfig{
		ID:        j.id,
		Name:      j.name,
		Type:      scheduler.JobTypeInterval,
		Schedule:  fmt.Sprintf("@every %s", j.interval),
		Enabled:   true,
		CreatedAt: timeNowUTC(),
		Tags:      []string{"selfimprove", "trace-analysis"},
	}
}
