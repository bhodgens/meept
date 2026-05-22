// Command meept-classifier-test runs model-vs-model classification benchmarks.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/caimlas/meept/internal/eval"
	"github.com/caimlas/meept/internal/llm"
)

func main() {
	modelA := flag.String("model-a", "/Volumes/LLMs/lfm2.5-1.2b-combined-serialized-sft", "First model to benchmark (full path or model ID)")
	modelB := flag.String("model-b", "/Volumes/LLMs/alexgusevski/LFM2.5-1.2B-Instruct-Thinking-Claude-High-Reasoning-mlx-4Bit", "Second model to benchmark (full path or model ID)")
	outputDir := flag.String("output", "docs/eval", "Directory to write benchmark reports")
	detailed := flag.Bool("detailed", false, "Print verbose per-test output")
	benchmarkName := flag.String("name", "classifier-eval", "Benchmark name / label")
	baseURL := flag.String("base-url", "http://127.0.0.1:8080", "LLM server BaseURL for OpenAI-compatible API")
	apiKey := flag.String("api-key", "", "API key for the LLM server")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(*modelA, *modelB, *outputDir, *detailed, *benchmarkName, *baseURL, *apiKey, logger); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(modelA, modelB, outputDir string, detailed bool, benchmarkName, baseURL, apiKey string, logger *slog.Logger) error {
	// Load test corpus
	corpus, err := eval.LoadTestCorpus()
	if err != nil {
		return fmt.Errorf("loading test corpus: %w", err)
	}

	total := corpus.TotalCount()
	fmt.Printf("Loaded test corpus: %s (%d test cases across %d categories)\n",
		corpus.Name, total, len(corpus.Categories))

	if detailed {
		fmt.Printf("Categories: %v\n", corpus.CategoryNames())
	}

	// Create LLM client for classification requests
	client := llm.NewClient(&llm.ModelConfig{
		BaseURL: baseURL,
		APIKey:  apiKey,
	},
		llm.WithLogger(logger),
		llm.WithTimeout(30*time.Second),
	)

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	config := eval.BenchmarkConfig{
		BenchmarkName: benchmarkName,
		Logger:        logger,
	}

	runner := eval.NewBenchmarkRunner(client, config)

	fmt.Printf("\nBenchmarking Model A (%s)...\n", modelA)
	modelAMetrics := runner.Run(context.Background(), corpus, modelA)

	fmt.Printf("Benchmarking Model B (%s)...\n", modelB)
	modelBMetrics := runner.Run(context.Background(), corpus, modelB)

	results := &eval.BenchmarkResults{
		BenchmarkName: benchmarkName,
		RunAt:         time.Now(),
		ModelA:        modelAMetrics,
		ModelB:        modelBMetrics,
	}

	// Save JSON report
	jsonPath := filepath.Join(outputDir, "benchmark-results.json")
	if err := saveJSON(jsonPath, results); err != nil {
		return fmt.Errorf("saving JSON report: %w", err)
	}

	// Save Markdown report
	mdPath := filepath.Join(outputDir, "benchmark-report.md")
	md := eval.FormatMarkdown(results)
	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		return fmt.Errorf("saving markdown report: %w", err)
	}

	// Print summary
	printSummary(results)

	fmt.Printf("\nReports saved:\n")
	fmt.Printf("  JSON:  %s\n", jsonPath)
	fmt.Printf("  Markdown: %s\n", mdPath)

	return nil
}

func saveJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func printSummary(results *eval.BenchmarkResults) {
	fmt.Println("\n============================================")
	fmt.Printf("  Benchmark: %s\n", results.BenchmarkName)
	fmt.Println("============================================")

	// Score comparison
	scoreA := eval.ScoreModel(results.ModelA)
	scoreB := eval.ScoreModel(results.ModelB)

	fmt.Printf("\n  %-25s  %-15s  %-15s\n", "Metric", results.ModelA.ModelName, results.ModelB.ModelName)
	fmt.Printf("  %-25s  %-15s  %-15s\n", "-------------------------", "---------------", "---------------")
	fmt.Printf("  %-25s  %-15s  %-15s\n", "Weighted Score", fmt.Sprintf("%.3f", scoreA), fmt.Sprintf("%.3f", scoreB))
	fmt.Printf("  %-25s  %-15s  %-15s\n", "Accuracy", fmt.Sprintf("%.1f%%", results.ModelA.OverallAccuracy*100), fmt.Sprintf("%.1f%%", results.ModelB.OverallAccuracy*100))
	fmt.Printf("  %-25s  %-15s  %-15s\n", "Avg Confidence", fmt.Sprintf("%.1f%%", results.ModelA.AvgConfidence*100), fmt.Sprintf("%.1f%%", results.ModelB.AvgConfidence*100))
	fmt.Printf("  %-25s  %-15s  %-15s\n", "Avg Latency (ms)", fmt.Sprintf("%.0f", results.ModelA.AvgLatencyMs), fmt.Sprintf("%.0f", results.ModelB.AvgLatencyMs))
	fmt.Printf("  %-25s  %-15d  %-15d\n", "Errors", results.ModelA.TotalErrors, results.ModelB.TotalErrors)

	winner := results.ModelA.ModelName
	if scoreB > scoreA+0.001 {
		winner = results.ModelB.ModelName
	} else if math.Abs(scoreA-scoreB) < 0.001 {
		winner = "Tie"
	}
	fmt.Printf("\n  ** Winner: %s **\n", winner)

	// Per-category highlights
	fmt.Println("\nPer-Category Accuracy:")
	fmt.Printf("  %-20s  %-15s  %-15s\n", "Category", results.ModelA.ModelName, results.ModelB.ModelName)
	fmt.Printf("  %-20s  %-15s  %-15s\n", "--------------------", "---------------", "---------------")

	catSet := make(map[string]bool)
	for _, c := range results.ModelA.CategoryBreakdown {
		catSet[c.CategoryName] = true
	}
	for _, c := range results.ModelB.CategoryBreakdown {
		catSet[c.CategoryName] = true
	}
	for name := range catSet {
		var accA, accB float64
		for _, c := range results.ModelA.CategoryBreakdown {
			if c.CategoryName == name {
				accA = c.Accuracy
				break
			}
		}
		for _, c := range results.ModelB.CategoryBreakdown {
			if c.CategoryName == name {
				accB = c.Accuracy
				break
			}
		}
		fmt.Printf("  %-20s  %-15s  %-15s\n", name, fmt.Sprintf("%.1f%%", accA*100), fmt.Sprintf("%.1f%%", accB*100))
	}
}
