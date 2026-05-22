package eval

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// TestResult represents the outcome of a single classification test.
type TestResult struct {
	Input       string    `json:"input"`
	Expected    string    `json:"expected_intent"`
	Predicted   string    `json:"predicted_intent"`
	Confidence  float64   `json:"confidence"`
	IsCorrect   bool      `json:"is_correct"`
	Latency     time.Duration `json:"latency"`
	Tokens      int       `json:"tokens,omitempty"`
	Error       string    `json:"error,omitempty"`
	Reasoning   string    `json:"reasoning,omitempty"`
	Category    string    `json:"category"`
}

// CategoryMetrics holds aggregated metrics for a single intent category.
type CategoryMetrics struct {
	CategoryName     string  `json:"category"`
	TestCount        int     `json:"test_count"`
	Correct          int     `json:"correct"`
	Incorrect        int     `json:"incorrect"`
	Errors           int     `json:"errors"`
	Accuracy         float64 `json:"accuracy"`
	AvgConfidence    float64 `json:"avg_confidence"`
	AvgLatency       float64 `json:"avg_latency_ms"`
	TotalTokens      int     `json:"total_tokens"`
	MinLatency       float64 `json:"min_latency_ms"`
	MaxLatency       float64 `json:"max_latency_ms"`
	TruePositives     int    `json:"-"`
	FalsePositives    int    `json:"-"`
	FalseNegatives    int    `json:"-"`
}

func (cm *CategoryMetrics) calculatePrecision() float64 {
	if cm.TruePositives+cm.FalsePositives == 0 {
		return 0
	}
	return float64(cm.TruePositives) / float64(cm.TruePositives+cm.FalsePositives)
}

func (cm *CategoryMetrics) calculateRecall() float64 {
	if cm.TruePositives+cm.FalseNegatives == 0 {
		return 0
	}
	return float64(cm.TruePositives) / float64(cm.TruePositives+cm.FalseNegatives)
}

func (cm *CategoryMetrics) calculateF1() float64 {
	p := cm.calculatePrecision()
	r := cm.calculateRecall()
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

// ModelMetrics holds overall metrics for a single model under evaluation.
type ModelMetrics struct {
	ModelName      string         `json:"model_name"`
	TotalTests     int            `json:"total_tests"`
	TotalCorrect   int            `json:"total_correct"`
	TotalIncorrect int            `json:"total_incorrect"`
	TotalErrors    int            `json:"total_errors"`
	OverallAccuracy float64       `json:"overall_accuracy"`
	AvgConfidence  float64        `json:"avg_confidence"`
	AvgLatencyMs   float64        `json:"avg_latency_ms"`
	TotalTokens    int            `json:"total_tokens"`
	CategoryBreakdown []CategoryMetrics `json:"category_breakdown"`
	PerIntent      map[string]PerIntentMetrics `json:"per_intent_metrics"`
}

// PerIntentMetrics tracks confusion matrix counts for a single intent.
type PerIntentMetrics struct {
	TruePos  int `json:"true_pos"`
	FalsePos int `json:"false_pos"`
	FalseNeg int `json:"false_neg"`
}

// BenchmarkResults holds the comparison between two models.
type BenchmarkResults struct {
	BenchmarkName string          `json:"benchmark_name"`
	RunAt         time.Time       `json:"run_at"`
	Config        BenchmarkConfig `json:"config"`
	ModelA        ModelMetrics    `json:"model_a"`
	ModelB        ModelMetrics    `json:"model_b"`
	VariantInfo   string          `json:"variant_info,omitempty"`
}

// MetricsCollector safely aggregates test results in real-time.
type MetricsCollector struct {
	mu       sync.Mutex
	results  []TestResult
	category map[string]*CategoryMetrics
}

// NewMetricsCollector creates a new empty collector.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		results:  make([]TestResult, 0),
		category: make(map[string]*CategoryMetrics),
	}
}

// AddResult records a single test result (thread-safe).
func (mc *MetricsCollector) AddResult(r TestResult) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.results = append(mc.results, r)

	cat, ok := mc.category[r.Category]
	if !ok {
		cat = &CategoryMetrics{CategoryName: r.Category}
		mc.category[r.Category] = cat
	}

	cat.TestCount++
	if r.Error != "" {
		cat.Errors++
		return
	}

	if r.IsCorrect {
		cat.Correct++

		// Track per-intent confusion metrics for precision/recall/F1
		// True positive: expected == predicted == r.Expected
		if r.Expected == r.Predicted {
			cat.TruePositives++
		}

		// False positive for predicted intent
		// (handled at aggregation time)

		// Confidence and latency tracking
		cat.AvgConfidence += r.Confidence
		cat.AvgLatency += float64(r.Latency.Milliseconds())
		cat.TotalTokens += r.Tokens

		if cat.MinLatency == 0 || float64(r.Latency.Milliseconds()) < cat.MinLatency {
			cat.MinLatency = float64(r.Latency.Milliseconds())
		}
		if float64(r.Latency.Milliseconds()) > cat.MaxLatency {
			cat.MaxLatency = float64(r.Latency.Milliseconds())
		}
	} else {
		cat.Incorrect++
		cat.FalsePositives++ // counting wrong predictions as FP for this category

		// False negative for expected intent
		cat.FalseNegatives++
	}
}

// Results returns a copy of all collected results.
func (mc *MetricsCollector) Results() []TestResult {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	cp := make([]TestResult, len(mc.results))
	copy(cp, mc.results)
	return cp
}

// GenerateModelMetrics computes aggregated ModelMetrics from collected results.
func (mc *MetricsCollector) GenerateModelMetrics(modelName string) ModelMetrics {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	overall := ModelMetrics{
		ModelName: modelName,
		PerIntent: make(map[string]PerIntentMetrics),
	}

	overall.TotalTests = len(mc.results)
	overall.TotalCorrect = 0
	overall.TotalIncorrect = 0
	overall.TotalErrors = 0

	var totalConfidence float64
	var totalLatency float64
	var totalTokens int

	// Initialize per-intent maps for all intents seen
	for _, r := range mc.results {
		if _, ok := overall.PerIntent[r.Expected]; !ok {
			overall.PerIntent[r.Expected] = PerIntentMetrics{}
		}
	}

	var catBreakdown []CategoryMetrics
	for _, cat := range mc.category {
		cm := *cat

		if cm.TestCount > 0 {
			cm.Accuracy = float64(cm.Correct) / float64(cm.TestCount)
			cm.AvgConfidence /= float64(cm.TestCount)
			cm.AvgLatency /= float64(cm.TestCount)
		}

		catBreakdown = append(catBreakdown, cm)
	}

	overall.CategoryBreakdown = catBreakdown

	for _, r := range mc.results {
		if r.Error != "" {
			overall.TotalErrors++
			continue
		}

		if r.IsCorrect {
			overall.TotalCorrect++
			totalConfidence += r.Confidence
		} else {
			overall.TotalIncorrect++
		}

		totalLatency += float64(r.Latency.Milliseconds())
		totalTokens += r.Tokens

		// Update per-intent confusion counts
		if r.IsCorrect {
			pi := overall.PerIntent[r.Expected]
			pi.TruePos++
			overall.PerIntent[r.Expected] = pi
		} else {
			// FN for expected intent, FP for predicted intent
			pi := overall.PerIntent[r.Expected]
			pi.FalseNeg++
			overall.PerIntent[r.Expected] = pi
			if _, ok := overall.PerIntent[r.Predicted]; !ok {
				overall.PerIntent[r.Predicted] = PerIntentMetrics{}
			}
			fi := overall.PerIntent[r.Predicted]
			fi.FalsePos++
			overall.PerIntent[r.Predicted] = fi
		}
	}

	if overall.TotalTests > 0 {
		overall.OverallAccuracy = float64(overall.TotalCorrect) / float64(overall.TotalTests)
	}
	if overall.TotalCorrect > 0 {
		overall.AvgConfidence = totalConfidence / float64(overall.TotalCorrect)
	}
	if overall.TotalTests > 0 {
		overall.AvgLatencyMs = totalLatency / float64(overall.TotalTests)
	}
	overall.TotalTokens = totalTokens

	return overall
}

// ScoreModel computes a weighted composite score for a model.
// Weights: accuracy 60%, precision 20%, recall 20%.
func ScoreModel(m ModelMetrics) float64 {
	return scoreModel(m)
}

// GetScore is an alias for ScoreModel, exported for use from cmd package.
// Deprecated: use ScoreModel instead.
func GetScore(m ModelMetrics) float64 {
	return scoreModel(m)
}

// scoreModel computes a weighted composite score for a model.
// Weights: accuracy 60%, precision 20%, recall 20%.
func scoreModel(m ModelMetrics) float64 {
	// Compute overall precision and recall from per-intent metrics
	var totalTP, totalFP, totalFN int
	for _, pi := range m.PerIntent {
		totalTP += pi.TruePos
		totalFP += pi.FalsePos
		totalFN += pi.FalseNeg
	}

	var precision, recall float64
	if totalTP+totalFP > 0 {
		precision = float64(totalTP) / float64(totalTP+totalFP)
	}
	if totalTP+totalFN > 0 {
		recall = float64(totalTP) / float64(totalTP+totalFN)
	}

	// Weighted composite: 60% accuracy, 20% precision, 20% recall
	return 0.6*m.OverallAccuracy + 0.2*precision + 0.2*recall
}

// GenerateComparisonReport creates a human-readable comparison between two model metrics.
func GenerateComparisonReport(a, b ModelMetrics, benchmarkName string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Classifier Benchmark Comparison: %s\n\n", benchmarkName))
	sb.WriteString(fmt.Sprintf("| Metric | %s | %s |\n", a.ModelName, b.ModelName))
	sb.WriteString(fmt.Sprintf("|--------|------|------|\n"))
	sb.WriteString(fmt.Sprintf("| Overall Accuracy | %.1f%% | %.1f%% |\n", a.OverallAccuracy*100, b.OverallAccuracy*100))
	sb.WriteString(fmt.Sprintf("| Avg Confidence (correct) | %.1f%% | %.1f%% |\n", a.AvgConfidence*100, b.AvgConfidence*100))
	sb.WriteString(fmt.Sprintf("| Avg Latency | %.0f ms | %.0f ms |\n", a.AvgLatencyMs, b.AvgLatencyMs))
	sb.WriteString(fmt.Sprintf("| Total Tokens Used | %d | %d |\n", a.TotalTokens, b.TotalTokens))
	sb.WriteString(fmt.Sprintf("| Total Errors | %d | %d |\n", a.TotalErrors, b.TotalErrors))

	// Score comparison
	scoreA := scoreModel(a)
	scoreB := scoreModel(b)
	sb.WriteString(fmt.Sprintf("| **Weighted Score** | **%.3f** | **%.3f** |\n", scoreA, scoreB))

	leader := a.ModelName
	if scoreB > scoreA {
		leader = b.ModelName
	}
	sb.WriteString(fmt.Sprintf("\n**Winner: %s**\n\n", leader))

	// Category breakdown
	sb.WriteString("### Per-Category Accuracy\n\n")
	sb.WriteString("| Category | ")
	sb.WriteString(a.ModelName)
	sb.WriteString(" | ")
	sb.WriteString(b.ModelName)
	sb.WriteString(" |\n")
	sb.WriteString("|----------|")
	sb.WriteString("---|")
	sb.WriteString("---|\n")

	// Merge category names from both models
	catSet := make(map[string]bool)
	for _, c := range a.CategoryBreakdown {
		catSet[c.CategoryName] = true
	}
	for _, c := range b.CategoryBreakdown {
		catSet[c.CategoryName] = true
	}
	for name := range catSet {
		var ca, cb *CategoryMetrics
		for _, c := range a.CategoryBreakdown {
			if c.CategoryName == name {
				ca = &c
				break
			}
		}
		for _, c := range b.CategoryBreakdown {
			if c.CategoryName == name {
				cb = &c
				break
			}
		}
		accA := "N/A"
		accB := "N/A"
		if ca != nil {
			accA = fmt.Sprintf("%.1f%%", ca.Accuracy*100)
		}
		if cb != nil {
			accB = fmt.Sprintf("%.1f%%", cb.Accuracy*100)
		}
		sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", name, accA, accB))
	}

	sb.WriteString("\n*Generated by Meept Classifier Benchmark*\n")
	return sb.String()
}

// FormatMarkdown produces a full markdown report for BenchmarkResults.
func FormatMarkdown(results *BenchmarkResults) string {
	var sb strings.Builder

	sb.WriteString("# Classifier Benchmark Report\n\n")
	sb.WriteString(fmt.Sprintf("**Benchmark:** %s\n\n", results.BenchmarkName))
	sb.WriteString(fmt.Sprintf("**Run at:** %s\n\n", results.RunAt.Format(time.RFC3339)))
	if results.VariantInfo != "" {
		sb.WriteString(fmt.Sprintf("**Variant:** %s\n\n", results.VariantInfo))
	}

	sb.WriteString("## Model A vs Model B\n\n")

	// Score rows first
	scoreA := scoreModel(results.ModelA)
	scoreB := scoreModel(results.ModelB)
	sb.WriteString("| Metric | Model A | Model B |\n")
	sb.WriteString("|--------|---------|---------|\n")
	sb.WriteString(fmt.Sprintf("| **Weighted Score** | **%.3f** | **%.3f** |\n", scoreA, scoreB))
	sb.WriteString(fmt.Sprintf("| Overall Accuracy | %.1f%% | %.1f%% |\n", results.ModelA.OverallAccuracy*100, results.ModelB.OverallAccuracy*100))
	sb.WriteString(fmt.Sprintf("| Avg Confidence | %.1f%% | %.1f%% |\n", results.ModelA.AvgConfidence*100, results.ModelB.AvgConfidence*100))
	sb.WriteString(fmt.Sprintf("| Avg Latency (ms) | %.0f | %.0f |\n", results.ModelA.AvgLatencyMs, results.ModelB.AvgLatencyMs))
	sb.WriteString(fmt.Sprintf("| Total Tokens | %d | %d |\n", results.ModelA.TotalTokens, results.ModelB.TotalTokens))
	sb.WriteString(fmt.Sprintf("| Errors / Non-errors | %d/%d | %d/%d |\n",
		results.ModelA.TotalErrors, results.ModelA.TotalTests-results.ModelA.TotalErrors,
		results.ModelB.TotalErrors, results.ModelB.TotalTests-results.ModelB.TotalErrors))

	winner := results.ModelA.ModelName
	if math.Abs(scoreA-scoreB) < 0.001 {
		winner = "Tie"
	} else if scoreB > scoreA {
		winner = results.ModelB.ModelName
	}
	sb.WriteString(fmt.Sprintf("\n**Winner: %s**\n\n", winner))

	// Per-category
	sb.WriteString("## Per-Category Breakdown\n\n")
	for _, catA := range results.ModelA.CategoryBreakdown {
		var catB *CategoryMetrics
		for _, c := range results.ModelB.CategoryBreakdown {
			if c.CategoryName == catA.CategoryName {
				catB = &c
				break
			}
		}

		sb.WriteString(fmt.Sprintf("### %s\n\n", catA.CategoryName))
		sb.WriteString(fmt.Sprintf("| Metric | Model A | Model B |\n"))
		sb.WriteString(fmt.Sprintf("|--------|---------|---------|\n"))
		testB := 0
		if catB != nil {
			testB = catB.TestCount
		}
		accB2 := 0.0
		if catB != nil {
			accB2 = catB.Accuracy
		}
		sb.WriteString(fmt.Sprintf("| Tests | %d | %d |\n", catA.TestCount, testB))
		sb.WriteString(fmt.Sprintf("| Accuracy | %.1f%% | %.1f%% |\n", catA.Accuracy*100, accB2*100))
		sb.WriteString("\n")
	}

	sb.WriteString("\n---\n*Report generated by Meept Classifier Benchmark*\n")
	return sb.String()
}
