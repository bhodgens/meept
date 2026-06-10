// Package benchmark provides a framework for running benchmarks to test agent
// performance on structured tasks (similar to aider's benchmark system).
package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"

	_ "modernc.org/sqlite" //nolint:revive // blank import for side effects
)

// BenchmarkConfig defines the configuration for running benchmarks.
type BenchmarkConfig struct {
	Tasks       []BenchmarkTask `json:"tasks"`
	Model       string          `json:"model"`
	EditFormat  string          `json:"edit_format"`
	NumTests    int             `json:"num_tests"`
	MaxThreads  int             `json:"max_threads"`
	Timeout     time.Duration   `json:"timeout"`
}

// BenchmarkTask defines a single benchmark task to run.
type BenchmarkTask struct {
	ID            string   `json:"id"`
	Description   string   `json:"description"`
	Setup         string   `json:"setup"`         // Shell command to prepare environment
	TestCommand   string   `json:"test_command"` // Shell command to run tests
	ExpectedFiles []string `json:"expected_files"`
}

// BenchmarkResult contains the aggregated results of a benchmark run.
type BenchmarkResult struct {
	Timestamp          string       `json:"timestamp"`
	Model              string       `json:"model"`
	EditFormat         string       `json:"edit_format"`
	CommitHash         string       `json:"commit_hash"`
	MeeptVersion       string       `json:"meept_version"`
	PassRate           float64      `json:"pass_rate"`
	WellFormedPct      float64      `json:"well_formed_pct"`
	NumMalformed       int          `json:"num_malformed"`
	SyntaxErrors       int          `json:"syntax_errors"`
	IndentationErrors  int          `json:"indentation_errors"`
	LazyResponses      int          `json:"lazy_responses"`
	ContextExhausted   int          `json:"context_exhausted"`
	TaskTimeouts       int          `json:"task_timeouts"`
	UserAsks           int          `json:"user_asks"`
	TaskResults        []TaskResult `json:"task_results"`
}

// TaskResult contains the result of a single task execution.
type TaskResult struct {
	TaskID          string   `json:"task_id"`
	Success         bool     `json:"success"`
	DurationSeconds float64  `json:"duration_seconds"`
	TokensUsed      int      `json:"tokens_used"`
	Iterations      int      `json:"iterations"`
	TestPassed      bool     `json:"test_passed"`
	TestOutput      string   `json:"test_output"`
	FilesChanged    []string `json:"files_changed"`
}

// Framework runs benchmarks and collects results.
type Framework struct {
	config        BenchmarkConfig
	results       []TaskResult
	mu            sync.Mutex
	semaphore     chan struct{}
	WorkingDir    string // Working directory for benchmark execution
}

// NewFramework creates a new benchmark framework with the given configuration.
func NewFramework(cfg BenchmarkConfig) *Framework {
	if cfg.MaxThreads <= 0 {
		cfg.MaxThreads = 4
	}
	if cfg.NumTests <= 0 {
		cfg.NumTests = 1
	}
	return &Framework{
		config:    cfg,
		results:   make([]TaskResult, 0, len(cfg.Tasks)*cfg.NumTests),
		semaphore: make(chan struct{}, cfg.MaxThreads),
	}
}

// Run executes all benchmarks with parallel execution support.
func (f *Framework) Run(ctx context.Context) (*BenchmarkResult, error) {
	startTime := time.Now()

	// Get commit hash
	commitHash := f.getCommitHash()

	// Get Meept version
	meeptVersion := f.getMeeptVersion()

	// Create work group for parallel execution
	var wg sync.WaitGroup
	taskChan := make(chan *BenchmarkTask, len(f.config.Tasks)*f.config.NumTests)

	// Start workers
	for i := 0; i < f.config.MaxThreads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskChan {
				select {
				case <-ctx.Done():
					return
				default:
					f.runTask(ctx, task)
				}
			}
		}()
	}

	// Queue all tasks
	for _, task := range f.config.Tasks {
		for i := 0; i < f.config.NumTests; i++ {
			select {
			case <-ctx.Done():
				break
			default:
				taskCopy := task
				taskChan <- &taskCopy
			}
		}
	}
	close(taskChan)

	// Wait for all workers to complete
	wg.Wait()

	// Calculate aggregates
	result := f.calculateResults(startTime, commitHash, meeptVersion)

	return result, nil
}

// runTask executes a single benchmark task.
func (f *Framework) runTask(ctx context.Context, task *BenchmarkTask) {
	// Acquire semaphore
	f.semaphore <- struct{}{}
	defer func() { <-f.semaphore }()

	result := TaskResult{
		TaskID: task.ID,
	}

	// Run setup command if specified
	if task.Setup != "" {
		if err := f.runCommand(task.Setup); err != nil {
			result.Success = false
			f.addResult(result)
			return
		}
	}

	// Run the benchmark task with timeout
	taskCtx, cancel := context.WithTimeout(ctx, f.config.Timeout)
	defer cancel()

	// Execute the benchmark (this would typically call the Meept agent)
	result.DurationSeconds = f.executeBenchmarkTask(taskCtx, task, &result)

	// Run test command if specified
	if task.TestCommand != "" {
		output, err := f.runCommandOutput(task.TestCommand)
		result.TestPassed = err == nil
		result.TestOutput = output
	}

	result.Success = result.TestPassed
	f.addResult(result)
}

// executeBenchmarkTask simulates running a benchmark task.
// In a real implementation, this would invoke the Meept agent.
func (f *Framework) executeBenchmarkTask(ctx context.Context, task *BenchmarkTask, result *TaskResult) float64 {
	start := time.Now()

	// Simulate task execution - in real implementation would call Meept API or CLI
	// For now, we simulate a basic execution
	select {
	case <-ctx.Done():
		return time.Since(start).Seconds()
	case <-time.After(100 * time.Millisecond): // Simulate some work
	}

	result.Iterations = 1
	result.TokensUsed = 1000 // Placeholder

	// Check for expected files
	result.FilesChanged = task.ExpectedFiles

	return time.Since(start).Seconds()
}

// runCommand runs a shell command with proper shell parsing and working directory support.
func (f *Framework) runCommand(cmd string) error {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return fmt.Errorf("empty command")
	}

	execCmd := exec.Command("sh", "-c", cmd)
	if f.WorkingDir != "" {
		execCmd.Dir = f.WorkingDir
	}
	return execCmd.Run()
}

// runCommandOutput runs a shell command and returns its output with proper shell parsing and working directory support.
func (f *Framework) runCommandOutput(cmd string) (string, error) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return "", fmt.Errorf("empty command")
	}

	execCmd := exec.Command("sh", "-c", cmd)
	if f.WorkingDir != "" {
		execCmd.Dir = f.WorkingDir
	}
	output, err := execCmd.CombinedOutput()
	return string(output), err
}

// addResult adds a task result to the results slice.
func (f *Framework) addResult(result TaskResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results = append(f.results, result)
}

// calculateResults calculates the aggregated benchmark results.
func (f *Framework) calculateResults(startTime time.Time, commitHash, meeptVersion string) *BenchmarkResult {
	f.mu.Lock()
	defer f.mu.Unlock()

	total := len(f.results)
	if total == 0 {
		return &BenchmarkResult{
			Timestamp:     startTime.Format(time.RFC3339),
			Model:         f.config.Model,
			EditFormat:    f.config.EditFormat,
			CommitHash:    commitHash,
			MeeptVersion:  meeptVersion,
			PassRate:      0,
			WellFormedPct: 100,
			TaskResults:   f.results,
		}
	}

	passed := 0
	totalTokens := 0
	totalDuration := 0.0

	for _, r := range f.results {
		if r.Success {
			passed++
		}
		totalTokens += r.TokensUsed
		totalDuration += r.DurationSeconds
	}

	passRate := float64(passed) / float64(total) * 100
	wellFormedPct := 100 - float64(0) // Would track actual metrics

	return &BenchmarkResult{
		Timestamp:          startTime.Format(time.RFC3339),
		Model:              f.config.Model,
		EditFormat:         f.config.EditFormat,
		CommitHash:         commitHash,
		MeeptVersion:       meeptVersion,
		PassRate:           passRate,
		WellFormedPct:      wellFormedPct,
		NumMalformed:       0,
		SyntaxErrors:       0,
		IndentationErrors:  0,
		LazyResponses:      0,
		ContextExhausted:   0,
		TaskTimeouts:       0,
		UserAsks:           0,
		TaskResults:        f.results,
	}
}

// getCommitHash returns the current git commit hash.
func (f *Framework) getCommitHash() string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "unknown"
	}
	return strings.TrimSpace(out.String())
}

// getMeeptVersion returns the Meept version.
func (f *Framework) getMeeptVersion() string {
	// Check for version info in binary or environment
	version := os.Getenv("MEEPT_VERSION")
	if version != "" {
		return version
	}
	return "dev"
}

// Save writes the benchmark results to a JSON file.
func (r *BenchmarkResult) Save(path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal results: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write results: %w", err)
	}

	return nil
}

// SaveToDB saves the benchmark results to the database.
func (r *BenchmarkResult) SaveToDB(dbPath string) error {
	db, err := sqlx.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Initialize schema
	schema := `
	CREATE TABLE IF NOT EXISTS model_performance (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
		model_id TEXT,
		task_type TEXT,
		edit_format TEXT,
		tasks_count INTEGER,
		success_rate REAL,
		avg_duration_ms INTEGER,
		avg_tokens INTEGER,
		avg_cost_cents REAL,
		UNIQUE(model_id, task_type, edit_format, date(timestamp))
	);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Calculate aggregates
	var totalDuration float64
	var totalTokens int
	tasksCount := len(r.TaskResults)

	for _, tr := range r.TaskResults {
		totalDuration += tr.DurationSeconds
		totalTokens += tr.TokensUsed
	}

	avgDurationMs := int((totalDuration / float64(tasksCount)) * 1000)
	avgTokens := totalTokens / tasksCount

	// Insert aggregated result
	_, err = db.Exec(`
		INSERT OR REPLACE INTO model_performance
		(timestamp, model_id, task_type, edit_format, tasks_count, success_rate, avg_duration_ms, avg_tokens)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		r.Timestamp,
		r.Model,
		"benchmark", // task_type
		r.EditFormat,
		tasksCount,
		r.PassRate,
		avgDurationMs,
		avgTokens,
	)

	if err != nil {
		return fmt.Errorf("failed to insert results: %w", err)
	}

	return nil
}

// LoadResults loads benchmark results from a JSON file.
func LoadResults(path string) (*BenchmarkResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read results: %w", err)
	}

	var result BenchmarkResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal results: %w", err)
	}

	return &result, nil
}

// DefaultBenchmarkConfig returns a default benchmark configuration.
func DefaultBenchmarkConfig() *BenchmarkConfig {
	return &BenchmarkConfig{
		Model:      "claude-sonnet-4-20250514",
		EditFormat: "whole",
		NumTests:   1,
		MaxThreads: 4,
		Timeout:    5 * time.Minute,
		Tasks:      []BenchmarkTask{},
	}
}