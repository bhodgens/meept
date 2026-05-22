package eval

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
)

// BenchmarkConfig configures the benchmark runner.
type BenchmarkConfig struct {
	// BenchmarkName is a label for this benchmark run.
	BenchmarkName string
	// VariantInfo is a label to attach to the report (e.g., model config description).
	VariantInfo string
	// ClassifierTimeout overrides the default classifier timeout. Zero means use the default.
	ClassifierTimeout time.Duration
	// Logger for progress output. If nil, a basic logger is used.
	Logger *slog.Logger
}

func (c *BenchmarkConfig) logger() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// BenchmarkRunner executes classification benchmarks against a corpus.
type BenchmarkRunner struct {
	config BenchmarkConfig
	client *llm.Client
}

// NewBenchmarkRunner creates a new benchmark runner.
func NewBenchmarkRunner(client *llm.Client, config BenchmarkConfig) *BenchmarkRunner {
	return &BenchmarkRunner{
		config: config,
		client: client,
	}
}

// Run executes a benchmark against the test corpus and returns metrics.
func (r *BenchmarkRunner) Run(ctx context.Context, corpus *TestCorpus, modelName string) ModelMetrics {
	collector := NewMetricsCollector()
	logger := r.config.logger()
	total := corpus.TotalCount()

	if total == 0 {
		logger.Warn("empty test corpus, no tests run")
		return ModelMetrics{}
	}

	logger.Info("starting benchmark",
		"model", modelName,
		"total_tests", total,
		"benchmark", r.config.BenchmarkName,
	)

	// Create a model-specific client with the correct ModelID.
	// The shared client may not have ModelID set, and the LLM classifier
	// sends the request's "model" field from the client's Config.ModelID.
	origCfg := r.client.Config()
	modelClient := llm.NewClient(&llm.ModelConfig{
		BaseURL: origCfg.BaseURL,
		ModelID: modelName,
		APIKey:  origCfg.APIKey,
	},
		llm.WithTimeout(30*time.Second),
	)

	categories := corpus.CategoryNames()
	for _, catName := range categories {
		cases := corpus.Categories[catName]
		logger.Info("category", "category", catName, "count", len(cases))

		for i, tc := range cases {
			start := time.Now()

			classifier := agent.NewLLMClassifier(agent.LLMClassifierConfig{
				Client:  modelClient,
				Model:   modelName,
				Timeout: r.config.ClassifierTimeout,
				Logger:  &benchmarkLogger{logger: logger, prefix: modelName},
			}, nil)

			intent, err := classifier.Classify(ctx, tc.Input, nil)
			latency := time.Since(start)

			result := TestResult{
				Input:      tc.Input,
				Expected:   tc.ExpectedIntent,
				Category:   catName,
				Latency:    latency,
			}

			if err != nil {
				result.Error = err.Error()
				logger.Debug("classify error", "category", catName, "index", i, "input", truncate(tc.Input, 80), "error", err)
			} else {
				result.Predicted = intent.Type
				result.Confidence = intent.Confidence
				result.IsCorrect = intent.Type == tc.ExpectedIntent
			}

			collector.AddResult(result)

			// Progress indicator every 10 tests
			current := i + 1
			if current%10 == 0 || current == total {
				correct := 0
				for _, r := range collector.Results() {
					if r.IsCorrect {
						correct++
					}
				}
				acc := float64(correct) / float64(current) * 100
				logger.Info("progress", "model", modelName, "done", current, "total", total, "accuracy", fmt.Sprintf("%.1f", acc))
			}
		}
	}

	modelMetrics := collector.GenerateModelMetrics(modelName)
	logger.Info("benchmark complete",
		"model", modelName,
		"accuracy", fmt.Sprintf("%.2f", modelMetrics.OverallAccuracy*100),
		"tests", modelMetrics.TotalTests,
		"errors", modelMetrics.TotalErrors,
	)

	return modelMetrics
}

// RunComparison runs the benchmark for both models and returns a BenchmarkResults struct.
func (r *BenchmarkRunner) RunComparison(ctx context.Context, corpus *TestCorpus, modelA, modelB string) *BenchmarkResults {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	modelAMetrics := r.Run(ctx, corpus, modelA)
	modelBMetrics := r.Run(ctx, corpus, modelB)

	return &BenchmarkResults{
		BenchmarkName: r.config.BenchmarkName,
		RunAt:         time.Now(),
		Config:        r.config,
		ModelA:        modelAMetrics,
		ModelB:        modelBMetrics,
		VariantInfo:   r.config.VariantInfo,
	}
}

// benchmarkLogger adapts slog.Logger to the agent.Logger interface used by LLMClassifier.
type benchmarkLogger struct {
	logger *slog.Logger
	prefix string
}

func (l *benchmarkLogger) Debug(msg string, args ...any) {
	l.logger.Debug(l.prefix+" "+msg, args...)
}

func (l *benchmarkLogger) Warn(msg string, args ...any) {
	l.logger.Warn(l.prefix+" "+msg, args...)
}

func (l *benchmarkLogger) Error(msg string, args ...any) {
	l.logger.Error(l.prefix+" "+msg, args...)
}

func (l *benchmarkLogger) Info(msg string, args ...any) {
	l.logger.Info(l.prefix+" "+msg, args...)
}

// truncate shortens a string to max characters, adding ... if truncated.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
