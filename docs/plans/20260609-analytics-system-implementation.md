# Analytics System Implementation Plan

**Created:** 2026-06-09
**Priority:** Medium
**Estimated Effort:** 1-2 weeks
**Status:** Pending Approval

## Overview

Extend Meept's existing metrics system with agent performance analytics inspired by aider-ai/aider's benchmarking capabilities. This plan focuses on **self-hosted analytics** (no external services like PostHog) that integrate with Meept's existing SQLite metrics infrastructure.

## Goals

1. Track agent performance metrics (pass rate, error rates, iteration counts)
2. Monitor code quality metrics (syntax errors, well-formed responses)
3. Measure efficiency (time per case, tokens per task)
4. Provide actionable insights for agent optimization
5. Maintain privacy (no external data transmission)

## Metrics to Implement

Inspired by aider's benchmark system, adapted for Meept's architecture:

### Core Metrics

| Metric | Description | Collection Point |
|--------|-------------|------------------|
| `pass_rate` | % of tasks completed successfully | Agent loop completion |
| `well_formed_pct` | % of LLM responses with proper structure | Response parser |
| `syntax_errors` | Count of syntax errors in generated code | Linter integration |
| `indentation_errors` | Language-specific formatting errors | Linter |
| `lazy_responses` | Count of abbreviated/copied responses | Response analyzer |
| `context_exhausted` | Times context window was exceeded | LLM client |
| `task_timeouts` | Tasks that exceeded time limit | Orchestrator |
| `user_interventions` | Times user had to step in | User feedback |
| `seconds_per_task` | Average task completion time | Agent loop |
| `tokens_per_task` | Token consumption per task | LLM client |
| `cost_per_task` | Estimated cost (for API models) | LLM client |
| `reflection_success_rate` | % of auto-fixes that succeeded | Reflection engine |

---

## Architecture

```
┌────────────────────────────────────────────────────────────────┐
│                    Metrics Collection Layer                     │
├────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐    │
│  │ Agent Loop  │  │ LLM Client  │  │ Linter/Test Runner  │    │
│  │  Metrics    │  │  Metrics    │  │     Metrics         │    │
│  └──────┬──────┘  └──────┬──────┘  └──────────┬──────────┘    │
│         │                │                     │               │
│         └────────────────┼─────────────────────┘               │
│                          ▼                                     │
│              ┌───────────────────────┐                        │
│              │ Metrics Collector     │                        │
│              │ (existing: internal/) │                        │
│              └───────────┬───────────┘                        │
│                          │                                     │
│                          ▼                                     │
│              ┌───────────────────────┐                        │
│              │ SQLite Metrics Store  │                        │
│              │ (~/.meept/metrics.db) │                        │
│              └───────────┬───────────┘                        │
│                          │                                     │
│         ┌────────────────┼────────────────┐                   │
│         ▼                ▼                ▼                   │
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────────┐         │
│  │ CLI Queries │  │ Dashboard  │  │ Export Reports  │         │
│  └─────────────┘ └─────────────┘ └─────────────────┘         │
└────────────────────────────────────────────────────────────────┘
```

---

## Database Schema Extensions

Add to existing `internal/metrics/store.go`:

```sql
-- Agent task outcomes
CREATE TABLE IF NOT EXISTS agent_task_outcomes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    task_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    skill_name TEXT,

    -- Outcome
    status TEXT,  -- 'completed', 'failed', 'timeout', 'abandoned'
    success BOOLEAN,

    -- Performance
    iterations INTEGER,
    duration_ms INTEGER,
    tokens_input INTEGER,
    tokens_output INTEGER,
    estimated_cost_cents REAL,

    -- Quality metrics
    response_well_formed BOOLEAN,
    syntax_errors_count INTEGER,
    indentation_errors_count INTEGER,
    lazy_response_detected BOOLEAN,
    context_exhausted BOOLEAN,

    -- Reflection loop metrics
    reflection_iterations INTEGER,
    reflection_successful BOOLEAN,

    -- User interaction
    user_interventions INTEGER,
    user_satisfaction INTEGER,  -- 1-5, optional feedback

    -- Context
    model_id TEXT,
    edit_format TEXT,

    FOREIGN KEY (task_id) REFERENCES tasks(id)
);

-- Response quality tracking
CREATE TABLE IF NOT EXISTS response_quality (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    task_id TEXT,
    agent_id TEXT,
    message_id TEXT,

    -- Structure validation
    is_well_formed BOOLEAN,
    parse_errors TEXT,  -- JSON array of error messages

    -- Content analysis
    has_code_blocks BOOLEAN,
    has_explanations BOOLEAN,
    is_lazy BOOLEAN,  -- Abbreviated response (e.g., "rest of code...")
    lazy_reason TEXT,

    -- Token analysis
    token_count INTEGER,
    code_token_pct REAL,

    FOREIGN KEY (task_id) REFERENCES tasks(id)
);

-- Error tracking
CREATE TABLE IF NOT EXISTS agent_errors (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    task_id TEXT,
    agent_id TEXT,
    error_type TEXT,  -- 'syntax', 'indentation', 'runtime', 'timeout', 'parse'
    error_message TEXT,
    file_path TEXT,
    line_number INTEGER,
    stack_trace TEXT,
    resolved BOOLEAN,
    resolution_method TEXT,

    FOREIGN KEY (task_id) REFERENCES tasks(id)
);

-- Model performance by task type
CREATE TABLE IF NOT EXISTS model_performance (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    model_id TEXT,
    task_type TEXT,  -- 'coding', 'debugging', 'planning', 'research'
    edit_format TEXT,

    -- Aggregated metrics (pre-computed for performance)
    tasks_count INTEGER,
    success_rate REAL,
    avg_duration_ms INTEGER,
    avg_tokens INTEGER,
    avg_cost_cents REAL,

    UNIQUE(model_id, task_type, edit_format, date(timestamp))
);

-- Indexes for common queries
CREATE INDEX idx_outcomes_agent ON agent_task_outcomes(agent_id, timestamp);
CREATE INDEX idx_outcomes_status ON agent_task_outcomes(status);
CREATE INDEX idx_errors_type ON agent_errors(error_type);
CREATE INDEX idx_model_perf ON model_performance(model_id, task_type);
```

---

## Implementation

### 1. Metrics Collector Extensions (`internal/metrics/collector.go`)

```go
package metrics

import (
    "database/sql"
    "time"

    "github.com/caimlas/meept/internal/llm"
)

// AgentTaskMetrics tracks agent performance
type AgentTaskMetrics struct {
    TaskID              string
    AgentID             string
    SkillName           string
    Status              string  // "completed", "failed", "timeout"
    Success             bool
    Iterations          int
    DurationMs          int64
    TokensInput         int
    TokensOutput        int
    EstimatedCostCents  float64
    ResponseWellFormed  bool
    SyntaxErrors        int
    IndentationErrors   int
    LazyResponse        bool
    ContextExhausted    bool
    ReflectionIterations int
    ReflectionSuccess   bool
    UserInterventions   int
    ModelID             string
    EditFormat          string
}

// Collector extends existing metrics collection
type Collector struct {
    db           *sql.DB
    logger       *slog.Logger
    flushQueue   chan *AgentTaskMetrics
    flushTicker  *time.Ticker
}

// NewCollector creates or extends the metrics collector
func NewCollector(dbPath string, logger *slog.Logger) (*Collector, error) {
    // ... existing initialization ...

    c := &Collector{
        db:         db,
        logger:     logger,
        flushQueue: make(chan *AgentTaskMetrics, 1000),
        flushTicker: time.NewTicker(60 * time.Second),
    }

    go c.flushLoop()

    return c, nil
}

// RecordAgentTask inserts agent task metrics
func (c *Collector) RecordAgentTask(m *AgentTaskMetrics) error {
    select {
    case c.flushQueue <- m:
        return nil
    default:
        // Queue full, flush synchronously
        c.flush()
        c.flushQueue <- m
        return nil
    }
}

// flush writes queued metrics to database
func (c *Collector) flush() {
    // ... implementation ...
}

func (c *Collector) flushLoop() {
    for {
        select {
        case <-c.flushTicker.C:
            c.flush()
        case <-shutdown:
            c.flush()
            return
        }
    }
}
```

**File:** `internal/metrics/collector.go` (MODIFY)

---

### 2. Response Quality Analyzer (`internal/metrics/analyzer.go`)

```go
package metrics

import (
    "regexp"
    "strings"

    "github.com/caimlas/meept/internal/llm"
)

// ResponseAnalyzer checks LLM response quality
type ResponseAnalyzer struct {
    lazyPatterns []*regexp.Regexp
}

// NewResponseAnalyzer creates the analyzer
func NewResponseAnalyzer() *ResponseAnalyzer {
    return &ResponseAnalyzer{
        lazyPatterns: []*regexp.Regexp{
            regexp.MustCompile(`(?i)//\s*rest of code`),
            regexp.MustCompile(`(?i)#\s*rest of the file`),
            regexp.MustCompile(`(?i)/\*\s*\.+\s*\*/`),
            regexp.MustCompile(`(?i)\.\.\.\s*existing code`),
            regexp.MustCompile(`(?i)#\s*\.\.\.`),
            regexp.MustCompile(`(?i)//\s*etc\.`),
        },
    }
}

// AnalyzeResponse evaluates response quality
type ResponseQuality struct {
    WellFormed      bool
    ParseErrors     []string
    HasCodeBlocks   bool
    HasExplanations bool
    IsLazy          bool
    LazyReason      string
    TokenCount      int
    CodeTokenPct    float64
}

// Analyze evaluates a single response
func (a *ResponseAnalyzer) Analyze(response string, tokenCount int) *ResponseQuality {
    q := &ResponseQuality{
        TokenCount: tokenCount,
    }

    // Check for code blocks
    q.HasCodeBlocks = strings.Contains(response, "```")

    // Check for explanations (conversational markers)
    conversationalMarkers := []string{"here's", "i'll", "let me", "sure!", "of course"}
    for _, marker := range conversationalMarkers {
        if strings.Contains(strings.ToLower(response), marker) {
            q.HasExplanations = true
            break
        }
    }

    // Check for lazy responses
    for _, pattern := range a.lazyPatterns {
        if pattern.MatchString(response) {
            q.IsLazy = true
            q.LazyReason = "abbreviated code block"
            break
        }
    }

    // Calculate code token percentage
    if q.HasCodeBlocks {
        codeBlocks := extractCodeBlocks(response)
        codeTokens := countTokens(strings.Join(codeBlocks, "\n"))
        q.CodeTokenPct = float64(codeTokens) / float64(tokenCount)
    }

    return q
}

// isWellFormed checks if response follows expected structure
func (a *ResponseAnalyzer) isWellFormed(response string, editFormat string) bool {
    switch editFormat {
    case "editblock":
        return strings.Contains(response, "<<<<<<< SEARCH") &&
               strings.Contains(response, ">>>>>>> REPLACE")
    case "editblock-fenced":
        return strings.Contains(response, "```") &&
               strings.Contains(response, "<<<<<<< SEARCH")
    case "udiff":
        return strings.Contains(response, "--- a/") &&
               strings.Contains(response, "+++ b/")
    default:
        return true  // Unknown format, assume OK
    }
}
```

**File:** `internal/metrics/analyzer.go` (NEW)

---

### 3. Agent Loop Integration (`internal/agent/orchestrator.go`)

```go
// In the agent loop, add metrics collection hooks:

type Orchestrator struct {
    // ... existing fields ...
    metricsCollector *metrics.Collecter
    responseAnalyzer *metrics.ResponseAnalyzer
}

func (o *Orchestrator) runSingleIteration(ctx context.Context) (*IterationResult, error) {
    startTime := time.Now()

    // ... existing LLM call ...

    result := &IterationResult{
        // ... existing fields ...
    }

    // NEW: Analyze response quality
    quality := o.responseAnalyzer.Analyze(llmResponse, tokensUsed)
    result.ResponseWellFormed = quality.WellFormed
    result.IsLazy = quality.IsLazy

    // NEW: Check for syntax errors (if auto-lint enabled)
    if o.config.AutoLint {
        lintErrors := o.linter.Check(o.editedFiles)
        result.SyntaxErrors = countSyntaxErrors(lintErrors)
        result.IndentationErrors = countIndentationErrors(lintErrors)
    }

    // Record metrics
    o.metricsCollector.RecordAgentTask(&metrics.AgentTaskMetrics{
        TaskID:             o.currentTask.ID,
        AgentID:            o.agentID,
        Status:             result.Status,
        Success:            result.Success,
        Iterations:         result.Iterations,
        DurationMs:         time.Since(startTime).Milliseconds(),
        TokensInput:        result.TokensIn,
        TokensOutput:       result.TokensOut,
        ResponseWellFormed: quality.WellFormed,
        SyntaxErrors:       result.SyntaxErrors,
        IsLazy:             quality.IsLazy,
        ContextExhausted:   result.ContextExhausted,
        ModelID:            o.model.ModelID,
        EditFormat:         o.editFormat,
    })

    return result, nil
}
```

**File:** `internal/agent/orchestrator.go` (MODIFY)

---

### 4. CLI Commands for Analytics (`cmd/meept/analytics.go`)

```go
package main

import (
    "encoding/json"
    "fmt"
    "os"
    "text/tabwriter"
    "time"
)

// Analytics commands
var analyticsCmd = &cobra.Command{
    Use:   "analytics",
    Short: "View agent analytics and performance metrics",
}

var analyticsSummaryCmd = &cobra.Command{
    Use:   "summary",
    Short: "Show performance summary",
    RunE: func(cmd *cobra.Command, args []string) error {
        db, err := openMetricsDB()
        if err != nil {
            return err
        }
        defer db.Close()

        rows, err := db.Query(`
            SELECT
                agent_id,
                COUNT(*) as total_tasks,
                SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) as successes,
                (SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) * 100.0 / COUNT(*)) as success_rate,
                AVG(duration_ms) as avg_duration_ms,
                AVG(tokens_input + tokens_output) as avg_tokens
            FROM agent_task_outcomes
            WHERE timestamp > datetime('now', '-7 days')
            GROUP BY agent_id
            ORDER BY success_rate DESC
        `)
        if err != nil {
            return err
        }
        defer rows.Close()

        w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
        fmt.Fprintln(w, "AGENT\tTASKS\tSUCCESS\tRATE\tAVG DUR\tAVG TOKENS")

        for rows.Next() {
            var agentID string
            var tasks, successes int
            var rate, avgDur, avgTokens float64

            if err := rows.Scan(&agentID, &tasks, &successes, &rate, &avgDur, &avgTokens); err != nil {
                return err
            }

            fmt.Fprintf(w, "%s\t%d\t%d\t%.1f%%\t%.0fms\t%.0f\n",
                agentID, tasks, successes, rate, avgDur, avgTokens)
        }

        return w.Flush()
    },
}

var analyticsErrorsCmd = &cobra.Command{
    Use:   "errors",
    Short: "Show error breakdown",
    RunE: func(cmd *cobra.Command, args []string) error {
        db, err := openMetricsDB()
        if err != nil {
            return err
        }
        defer db.Close()

        rows, err := db.Query(`
            SELECT
                error_type,
                COUNT(*) as count,
                COUNT(DISTINCT task_id) as affected_tasks,
                SUM(CASE WHEN resolved = 1 THEN 1 ELSE 0 END) as resolved_count
            FROM agent_errors
            WHERE timestamp > datetime('now', '-7 days')
            GROUP BY error_type
            ORDER BY count DESC
        `)
        if err != nil {
            return err
        }
        defer rows.Close()

        w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
        fmt.Fprintln(w, "ERROR TYPE\tCOUNT\tTASKS\tRESOLVED")

        for rows.Next() {
            var errType string
            var count, tasks, resolved int
            if err := rows.Scan(&errType, &count, &tasks, &resolved); err != nil {
                return err
            }
            fmt.Fprintf(w, "%s\t%d\t%d\t%d\n", errType, count, tasks, resolved)
        }

        return w.Flush()
    },
}

var analyticsModelCmd = &cobra.Command{
    Use:   "models",
    Short: "Compare model performance",
    RunE: func(cmd *cobra.Command, args []string) error {
        db, err := openMetricsDB()
        if err != nil {
            return err
        }
        defer db.Close()

        rows, err := db.Query(`
            SELECT
                model_id,
                COUNT(*) as tasks,
                (SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) * 100.0 / COUNT(*)) as success_rate,
                AVG(estimated_cost_cents) as avg_cost,
                AVG(duration_ms) as avg_duration
            FROM agent_task_outcomes
            WHERE timestamp > datetime('now', '-30 days')
            GROUP BY model_id
            ORDER BY success_rate DESC
        `)
        if err != nil {
            return err
        }
        defer rows.Close()

        w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
        fmt.Fprintln(w, "MODEL\tTASKS\tSUCCESS RATE\tAVG COST\tAVG DUR")

        for rows.Next() {
            var modelID string
            var tasks int
            var rate, avgCost, avgDur float64
            if err := rows.Scan(&modelID, &tasks, &rate, &avgCost, &avgDur); err != nil {
                return err
            }
            fmt.Fprintf(w, "%s\t%d\t%.1f%%\t$%.3f\t%.0fms\n",
                modelID, tasks, rate, avgCost, avgDur)
        }

        return w.Flush()
    },
}

var analyticsExportCmd = &cobra.Command{
    Use:   "export",
    Short: "Export analytics data",
    RunE: func(cmd *cobra.Command, args []string) error {
        db, err := openMetricsDB()
        if err != nil {
            return err
        }
        defer db.Close()

        rows, err := db.Query(`
            SELECT * FROM agent_task_outcomes
            WHERE timestamp > datetime('now', '-30 days')
        `)
        if err != nil {
            return err
        }
        defer rows.Close()

        // Get column names
        columns, _ := rows.Columns()

        var results []map[string]interface{}
        for rows.Next() {
            values := make([]interface{}, len(columns))
            valuePtrs := make([]interface{}, len(columns))
            for i := range values {
                valuePtrs[i] = &values[i]
            }

            if err := rows.Scan(valuePtrs...); err != nil {
                return err
            }

            rowMap := make(map[string]interface{})
            for i, col := range columns {
                rowMap[col] = values[i]
            }
            results = append(results, rowMap)
        }

        return json.NewEncoder(os.Stdout).Encode(results)
    },
}

func init() {
    rootCmd.AddCommand(analyticsCmd)
    analyticsCmd.AddCommand(analyticsSummaryCmd)
    analyticsCmd.AddCommand(analyticsErrorsCmd)
    analyticsCmd.AddCommand(analyticsModelCmd)
    analyticsCmd.AddCommand(analyticsExportCmd)
}
```

**File:** `cmd/meept/analytics.go` (NEW)

---

### 5. Benchmark Framework (`internal/benchmark/framework.go`)

```go
package benchmark

import (
    "context"
    "encoding/json"
    "os"
    "time"

    "github.com/caimlas/meept/internal/agent"
    "github.com/caimlas/meept/internal/metrics"
)

// BenchmarkConfig holds benchmark parameters
type BenchmarkConfig struct {
    Tasks       []BenchmarkTask
    Model       string
    EditFormat  string
    NumTests    int
    MaxThreads  int
    Timeout     time.Duration
}

// BenchmarkTask is a single benchmark exercise
type BenchmarkTask struct {
    ID          string
    Description string
    Setup       string  // Shell command to prepare environment
    TestCommand string  // Shell command to run tests
    ExpectedFiles []string
}

// BenchmarkResult holds results for a benchmark run
type BenchmarkResult struct {
    Timestamp       string
    Model           string
    EditFormat      string
    CommitHash      string
    MeeptVersion    string

    // Aggregate metrics
    PassRate        float64  // % of tasks with all tests passing
    WellFormedPct   float64  // % of well-formed responses
    NumMalformed    int
    SyntaxErrors    int
    IndentationErrors int
    LazyResponses   int
    ContextExhausted int
    TaskTimeouts    int
    UserAsks        int  // Times model asked for clarification

    // Per-task results
    TaskResults     []TaskResult
}

// TaskResult holds per-task metrics
type TaskResult struct {
    TaskID          string
    Success         bool
    DurationSeconds float64
    TokensUsed      int
    Iterations      int
    TestPassed      bool
    TestOutput      string
    FilesChanged    []string
}

// Framework runs benchmarks
type Framework struct {
    config     BenchmarkConfig
    agentLoop  *agent.Orchestrator
    collector  *metrics.Collector
    logger     *slog.Logger
}

// Run executes the benchmark
func (f *Framework) Run(ctx context.Context) (*BenchmarkResult, error) {
    result := &BenchmarkResult{
        Timestamp:   time.Now().UTC().Format(time.RFC3339),
        Model:       f.config.Model,
        EditFormat:  f.config.EditFormat,
        TaskResults: make([]TaskResult, 0, len(f.config.Tasks)),
    }

    // Run tasks in parallel (up to MaxThreads)
    sem := make(chan struct{}, f.config.MaxThreads)

    for _, task := range f.config.Tasks {
        sem <- struct{}{}  // Acquire semaphore

        go func(t BenchmarkTask) {
            defer func() { <-sem }()  // Release semaphore

            taskResult := f.runSingleTask(ctx, t)
            result.TaskResults = append(result.TaskResults, taskResult)

            // Update aggregates
            if taskResult.Success {
                result.PassRate++
            }
            // ... update other aggregates ...
        }(task)
    }

    // Wait for all tasks to complete
    for i := 0; i < cap(sem); i++ {
        sem <- struct{}{}
    }

    // Calculate percentages
    result.PassRate = result.PassRate / float64(len(f.config.Tasks)) * 100
    // ... calculate other percentages ...

    return result, nil
}

// Save writes results to YAML/JSON file
func (r *BenchmarkResult) Save(path string) error {
    data, err := json.MarshalIndent(r, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(path, data, 0644)
}
```

**File:** `internal/benchmark/framework.go` (NEW)

---

## Configuration Schema

Add to `config/meept.json5`:

```json5
{
  analytics: {
    enabled: true,
    retention_days: 90,
    flush_interval_seconds: 60,

    // Quality analysis
    quality: {
      track_lazy_responses: true,
      track_well_formed: true,
      track_syntax_errors: true,
    },

    // Privacy (all local, no external)
    privacy: {
      anonymize_task_descriptions: false,
      include_code_snippets: false,
    },

    // Aggregation
    aggregation: {
      compute_hourly_rollups: true,
      compute_daily_rollups: true,
    },
  },

  benchmark: {
    enabled: true,
    default_threads: 4,
    default_timeout_seconds: 300,
    output_dir: "~/.meept/benchmarks",
  },
}
```

---

## CLI Commands

```bash
# Summary dashboard
meept analytics summary
meept analytics summary --days 30

# Error breakdown
meept analytics errors --type syntax
meept analytics errors --agent debugger

# Model comparison
meept analytics models
meept analytics models --task-type coding

# Export for external analysis
meept analytics export --format json > analytics.json
meept analytics export --format csv > analytics.csv

# Run benchmarks
meept benchmark run --tasks exercism --model claude-sonnet
meept benchmark run --tasks swe-bench --threads 8
```

---

## Privacy Considerations

**No external services.** All analytics stored locally in `~/.meept/metrics.db`.

Optional future extensions (user-enabled):
- Export to Prometheus/Grafana
- Local HTTP dashboard
- SQLite-to-CSV export for BI tools

---

## Testing Plan

### Unit Tests
1. Response quality analyzer (lazy detection, well-formed checks)
2. Metrics collector (queue handling, flush behavior)
3. Database schema (indexes, queries)

### Integration Tests
1. Agent loop → metrics collection end-to-end
2. CLI commands → database queries
3. Benchmark framework → task execution

---

## Success Criteria

- [ ] All core metrics collected automatically
- [ ] Response quality analyzer detects lazy/well-formed responses
- [ ] CLI analytics commands return accurate data
- [ ] Benchmark framework can run Exercism-style tasks
- [ ] No external network calls for analytics
- [ ] Performance overhead < 5%
- [ ] Comprehensive test coverage

---

## Related Documentation

- `docs/reference/metrics.md` — Existing metrics system
- `docs/plans/auto-lint-test-reflection-implementation.md` — Linter integration
- `internal/metrics/store.go` — Current metrics schema
