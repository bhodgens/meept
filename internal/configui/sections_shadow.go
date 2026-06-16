// internal/configui/sections_shadow.go
package configui

import (
	"strings"
)

func buildShadowFields() []Field {
	cfg := loadMainConfigOrFallback()
	s := &cfg.Shadow
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewTextField("data_dir", "data dir", s.DataDir),

		NewDrilldownField("shadowing", "shadowing", []DrilldownItem{
			{Name: "shadowing", Fields: []Field{
				NewSelectField("shadowing.mode", "mode", s.Shadowing.Mode, []string{"all", "sampled", "targeted"}),
				NewSelectField("shadowing.min_complexity", "min complexity", s.Shadowing.MinComplexity, []string{"low", "medium", "high"}),
				NewTextField("shadowing.domains", "domains", strings.Join(s.Shadowing.Domains, ",")),
				NewTextField("shadowing.task_types", "task types", strings.Join(s.Shadowing.TaskTypes, ",")),
				NewFloatField("shadowing.sample_rate", "sample rate", s.Shadowing.SampleRate),
				NewNumberField("shadowing.queue_size", "queue size", s.Shadowing.QueueSize),
				NewNumberField("shadowing.worker_count", "worker count", s.Shadowing.WorkerCount),
			}},
		}),

		NewDrilldownField("teacher", "teacher", []DrilldownItem{
			{Name: "teacher", Fields: []Field{
				NewTextField("teacher.model", "model", s.Teacher.Model),
				NewTextField("teacher.fallback_model", "fallback model", s.Teacher.FallbackModel),
				NewFloatField("teacher.temperature", "temperature", s.Teacher.Temperature),
				NewNumberField("teacher.max_tokens", "max tokens", s.Teacher.MaxTokens),
				NewNumberField("teacher.timeout_seconds", "timeout seconds", s.Teacher.TimeoutSeconds),
				NewNumberField("teacher.max_daily_queries", "max daily queries", s.Teacher.MaxDailyQueries),
				NewFloatField("teacher.max_daily_cost", "max daily cost", s.Teacher.MaxDailyCost),
				NewNumberField("teacher.requests_per_minute", "requests per minute", s.Teacher.RequestsPerMinute),
			}},
		}),

		NewDrilldownField("quality", "quality", []DrilldownItem{
			{Name: "quality", Fields: []Field{
				NewSelectField("quality.method", "method", s.Quality.Method, []string{"heuristic", "llm"}),
				NewFloatField("quality.high_quality_threshold", "high quality threshold", s.Quality.HighQualityThreshold),
				NewFloatField("quality.trainable_threshold", "trainable threshold", s.Quality.TrainableThreshold),
				NewFloatField("quality.preference_margin", "preference margin", s.Quality.PreferenceMargin),
				NewTextField("quality.eval_prompt_template", "eval prompt template", s.Quality.EvalPromptTemplate),
			}},
		}),

		NewDrilldownField("quality.heuristic_weights", "quality weights", []DrilldownItem{
			{Name: "quality weights", Fields: []Field{
				NewFloatField("quality.heuristic_weights.relevance", "relevance", s.Quality.HeuristicWeights.Relevance),
				NewFloatField("quality.heuristic_weights.completeness", "completeness", s.Quality.HeuristicWeights.Completeness),
				NewFloatField("quality.heuristic_weights.correctness", "correctness", s.Quality.HeuristicWeights.Correctness),
				NewFloatField("quality.heuristic_weights.style", "style", s.Quality.HeuristicWeights.Style),
			}},
		}),

		NewDrilldownField("examples", "examples", []DrilldownItem{
			{Name: "examples", Fields: []Field{
				NewToggleField("examples.enabled", "enabled", s.Examples.Enabled),
				NewNumberField("examples.max_per_category", "max per category", s.Examples.MaxPerCategory),
				NewFloatField("examples.min_quality", "min quality", s.Examples.MinQuality),
				NewNumberField("examples.default_count", "default count", s.Examples.DefaultCount),
				NewNumberField("examples.max_count", "max count", s.Examples.MaxCount),
				NewNumberField("examples.max_context_tokens", "max context tokens", s.Examples.MaxContextTokens),
				NewFloatField("examples.similarity_weight", "similarity weight", s.Examples.SimilarityWeight),
				NewFloatField("examples.recency_weight", "recency weight", s.Examples.RecencyWeight),
				NewFloatField("examples.quality_weight", "quality weight", s.Examples.QualityWeight),
			}},
		}),

		NewDrilldownField("export", "export", []DrilldownItem{
			{Name: "export", Fields: []Field{
				NewTextField("export.output_dir", "output dir", s.Export.OutputDir),
				NewMultiSelectField("export.formats", "formats", s.Export.Formats, []string{"jsonl", "json", "csv"}),
				NewNumberField("export.min_records", "min records", s.Export.MinRecords),
				NewToggleField("export.include_low_quality", "include low quality", s.Export.IncludeLowQuality),
				NewToggleField("export.deduplicate", "deduplicate", s.Export.Deduplicate),
				NewFloatField("export.dedup_similarity_threshold", "dedup similarity threshold", s.Export.DedupSimilarityThreshold),
			}},
		}),

		NewDrilldownField("adapters", "adapters", []DrilldownItem{
			{Name: "adapters", Fields: []Field{
				NewToggleField("adapters.enabled", "enabled", s.Adapters.Enabled),
				NewToggleField("adapters.auto_train", "auto train", s.Adapters.AutoTrain),
				NewTextField("adapters.ollama_endpoint", "ollama endpoint", s.Adapters.OllamaEndpoint),
				NewTextField("adapters.train_schedule", "train schedule", s.Adapters.TrainSchedule),
				NewTextField("adapters.adapter_dir", "adapter dir", s.Adapters.AdapterDir),
				NewNumberField("adapters.train_threshold", "train threshold", s.Adapters.TrainThreshold),
			}},
		}),

		NewDrilldownField("adapters.lora", "adapters lora", []DrilldownItem{
			{Name: "adapters lora", Fields: []Field{
				NewNumberField("adapters.lora.rank", "rank", s.Adapters.LoRA.Rank),
				NewNumberField("adapters.lora.alpha", "alpha", s.Adapters.LoRA.Alpha),
				NewFloatField("adapters.lora.dropout", "dropout", s.Adapters.LoRA.Dropout),
				NewTextField("adapters.lora.target_modules", "target modules", strings.Join(s.Adapters.LoRA.TargetModules, ",")),
				NewFloatField("adapters.lora.learning_rate", "learning rate", s.Adapters.LoRA.LearningRate),
				NewNumberField("adapters.lora.epochs", "epochs", s.Adapters.LoRA.Epochs),
				NewNumberField("adapters.lora.batch_size", "batch size", s.Adapters.LoRA.BatchSize),
				NewNumberField("adapters.lora.gradient_accumulation", "gradient accumulation", s.Adapters.LoRA.GradientAccumulation),
				NewFloatField("adapters.lora.warmup_ratio", "warmup ratio", s.Adapters.LoRA.WarmupRatio),
				NewFloatField("adapters.lora.max_grad_norm", "max grad norm", s.Adapters.LoRA.MaxGradNorm),
			}},
		}),

		NewDrilldownField("adapters.dpo", "adapters dpo", []DrilldownItem{
			{Name: "adapters dpo", Fields: []Field{
				NewFloatField("adapters.dpo.beta", "beta", s.Adapters.DPO.Beta),
				NewTextField("adapters.dpo.loss_type", "loss type", s.Adapters.DPO.LossType),
			}},
		}),
	}
}
