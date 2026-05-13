// Package shadow implements the Shadow Training system for model improvement.
// It enables teacher model shadowing, quality scoring, and training data collection.
package shadow

import (
	"slices"
	"time"
)

// ShadowMode defines how shadowing is performed.
//nolint:revive // stutter with package name is intentional for API clarity
type ShadowMode string

const (
	// ModeAsync returns student response immediately, shadows in background.
	ModeAsync ShadowMode = "async"
	// ModeSync waits for both student and teacher responses.
	ModeSync ShadowMode = "sync"
	// ModeSelective only shadows based on criteria.
	ModeSelective ShadowMode = "selective"
)

// QualityMethod defines the quality scoring method.
type QualityMethod string

const (
	// MethodHeuristic uses fast pattern-based scoring.
	MethodHeuristic QualityMethod = "heuristic"
	// MethodTeacherEval uses teacher model for evaluation.
	MethodTeacherEval QualityMethod = "teacher_eval"
	// MethodHybrid combines heuristic pre-filter with teacher for borderline.
	MethodHybrid QualityMethod = "hybrid"
)

// Complexity represents task complexity levels.
type Complexity string

const (
	ComplexitySimple   Complexity = "simple"
	ComplexityModerate Complexity = "moderate"
	ComplexityComplex  Complexity = "complex"
)

// Config holds the complete shadow training configuration.
type Config struct {
	// Master switch
	Enabled bool   `toml:"enabled"`
	DataDir string `toml:"data_dir"`

	// Sub-configurations
	Shadowing ShadowingConfig `toml:"shadowing"`
	Teacher   TeacherConfig   `toml:"teacher"`
	Quality   QualityConfig   `toml:"quality"`
	Examples  ExamplesConfig  `toml:"examples"`
	Export    ExportConfig    `toml:"export"`
	Adapters  AdaptersConfig  `toml:"adapters"`
}

// ShadowingConfig controls when and how responses are shadowed.
//nolint:revive // stutter with package name is intentional for API clarity
type ShadowingConfig struct {
	Mode          ShadowMode `toml:"mode"`
	MinComplexity Complexity `toml:"min_complexity"`
	Domains       []string   `toml:"domains"`
	TaskTypes     []string   `toml:"task_types"`
	SampleRate    float64    `toml:"sample_rate"`
	QueueSize     int        `toml:"queue_size"`
	WorkerCount   int        `toml:"worker_count"`
}

// TeacherConfig configures the teacher model.
type TeacherConfig struct {
	Model             string  `toml:"model"`
	FallbackModel     string  `toml:"fallback_model"`
	Temperature       float64 `toml:"temperature"`
	MaxTokens         int     `toml:"max_tokens"`
	TimeoutSeconds    int     `toml:"timeout_seconds"`
	MaxDailyQueries   int     `toml:"max_daily_queries"`
	MaxDailyCost      float64 `toml:"max_daily_cost"`
	RequestsPerMinute int     `toml:"requests_per_minute"`
}

// HeuristicWeights defines scoring dimension weights.
type HeuristicWeights struct {
	Relevance    float64 `toml:"relevance"`
	Completeness float64 `toml:"completeness"`
	Correctness  float64 `toml:"correctness"`
	Style        float64 `toml:"style"`
}

// QualityConfig configures quality scoring.
type QualityConfig struct {
	Method               QualityMethod    `toml:"method"`
	HighQualityThreshold float64          `toml:"high_quality_threshold"`
	TrainableThreshold   float64          `toml:"trainable_threshold"`
	PreferenceMargin     float64          `toml:"preference_margin"`
	HeuristicWeights     HeuristicWeights `toml:"heuristic_weights"`
	EvalPromptTemplate   string           `toml:"eval_prompt_template"`
}

// ExamplesConfig configures few-shot example management.
type ExamplesConfig struct {
	Enabled          bool    `toml:"enabled"`
	MaxPerCategory   int     `toml:"max_per_category"`
	MinQuality       float64 `toml:"min_quality"`
	DefaultCount     int     `toml:"default_count"`
	MaxCount         int     `toml:"max_count"`
	SimilarityWeight float64 `toml:"similarity_weight"`
	RecencyWeight    float64 `toml:"recency_weight"`
	QualityWeight    float64 `toml:"quality_weight"`
	MaxContextTokens int     `toml:"max_context_tokens"`
}

// ExportConfig configures training data export.
type ExportConfig struct {
	OutputDir                string   `toml:"output_dir"`
	Formats                  []string `toml:"formats"`
	MinRecords               int      `toml:"min_records"`
	IncludeLowQuality        bool     `toml:"include_low_quality"`
	Deduplicate              bool     `toml:"deduplicate"`
	DedupSimilarityThreshold float64  `toml:"dedup_similarity_threshold"`
}

// LoRAConfig configures LoRA training parameters.
type LoRAConfig struct {
	Rank                 int      `toml:"rank"`
	Alpha                int      `toml:"alpha"`
	Dropout              float64  `toml:"dropout"`
	TargetModules        []string `toml:"target_modules"`
	LearningRate         float64  `toml:"learning_rate"`
	Epochs               int      `toml:"epochs"`
	BatchSize            int      `toml:"batch_size"`
	GradientAccumulation int      `toml:"gradient_accumulation"`
	WarmupRatio          float64  `toml:"warmup_ratio"`
	MaxGradNorm          float64  `toml:"max_grad_norm"`
}

// DPOConfig configures DPO training parameters.
type DPOConfig struct {
	Beta     float64 `toml:"beta"`
	LossType string  `toml:"loss_type"`
}

// AdaptersConfig configures adapter management.
type AdaptersConfig struct {
	Enabled        bool       `toml:"enabled"`
	OllamaEndpoint string     `toml:"ollama_endpoint"`
	AutoTrain      bool       `toml:"auto_train"`
	TrainThreshold int        `toml:"train_threshold"`
	TrainSchedule  string     `toml:"train_schedule"`
	AdapterDir     string     `toml:"adapter_dir"`
	LoRA           LoRAConfig `toml:"lora"`
	DPO            DPOConfig  `toml:"dpo"`
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Enabled: false,
		DataDir: "~/.meept/shadow",
		Shadowing: ShadowingConfig{
			Mode:          ModeAsync,
			MinComplexity: ComplexityModerate,
			Domains:       []string{},
			TaskTypes:     []string{},
			SampleRate:    0.5,
			QueueSize:     1000,
			WorkerCount:   2,
		},
		Teacher: TeacherConfig{
			Model:             "",
			FallbackModel:     "",
			Temperature:       0.0,
			MaxTokens:         4096,
			TimeoutSeconds:    120,
			MaxDailyQueries:   500,
			MaxDailyCost:      10.0,
			RequestsPerMinute: 30,
		},
		Quality: QualityConfig{
			Method:               MethodHybrid,
			HighQualityThreshold: 0.85,
			TrainableThreshold:   0.6,
			PreferenceMargin:     0.1,
			HeuristicWeights: HeuristicWeights{
				Relevance:    0.30,
				Completeness: 0.25,
				Correctness:  0.35,
				Style:        0.10,
			},
			EvalPromptTemplate: "",
		},
		Examples: ExamplesConfig{
			Enabled:          true,
			MaxPerCategory:   100,
			MinQuality:       0.8,
			DefaultCount:     3,
			MaxCount:         5,
			SimilarityWeight: 0.7,
			RecencyWeight:    0.2,
			QualityWeight:    0.1,
			MaxContextTokens: 2000,
		},
		Export: ExportConfig{
			OutputDir:                "~/.meept/shadow/exports",
			Formats:                  []string{"jsonl", "dpo"},
			MinRecords:               100,
			IncludeLowQuality:        false,
			Deduplicate:              true,
			DedupSimilarityThreshold: 0.95,
		},
		Adapters: AdaptersConfig{
			Enabled:        false,
			OllamaEndpoint: "http://localhost:11434",
			AutoTrain:      false,
			TrainThreshold: 500,
			TrainSchedule:  "",
			AdapterDir:     "~/.meept/shadow/adapters",
			LoRA: LoRAConfig{
				Rank:                 16,
				Alpha:                32,
				Dropout:              0.05,
				TargetModules:        []string{"q_proj", "v_proj", "k_proj", "o_proj"},
				LearningRate:         2e-4,
				Epochs:               3,
				BatchSize:            4,
				GradientAccumulation: 4,
				WarmupRatio:          0.03,
				MaxGradNorm:          1.0,
			},
			DPO: DPOConfig{
				Beta:     0.1,
				LossType: "sigmoid",
			},
		},
	}
}

// Timeout returns the teacher timeout as a duration.
func (c *TeacherConfig) Timeout() time.Duration {
	return time.Duration(c.TimeoutSeconds) * time.Second
}

// IsEnabled returns true if shadow training is fully configured.
func (c *Config) IsEnabled() bool {
	return c.Enabled && c.Teacher.Model != ""
}

// ShouldShadow determines if a request should be shadowed based on criteria.
func (c *Config) ShouldShadow(domain, taskType string, complexity Complexity) bool {
	if !c.IsEnabled() {
		return false
	}

	cfg := c.Shadowing

	// Check complexity threshold
	if !meetsComplexity(complexity, cfg.MinComplexity) {
		return false
	}

	// Check domain filter
	if len(cfg.Domains) > 0 && !contains(cfg.Domains, domain) {
		return false
	}

	// Check task type filter
	if len(cfg.TaskTypes) > 0 && !contains(cfg.TaskTypes, taskType) {
		return false
	}

	return true
}

// meetsComplexity checks if the actual complexity meets or exceeds the threshold.
func meetsComplexity(actual, threshold Complexity) bool {
	order := map[Complexity]int{
		ComplexitySimple:   1,
		ComplexityModerate: 2,
		ComplexityComplex:  3,
	}
	return order[actual] >= order[threshold]
}

// contains checks if a slice contains a string.
func contains(slice []string, item string) bool {
	return slices.Contains(slice, item)
}
