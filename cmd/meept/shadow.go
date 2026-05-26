package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/shadow"
	"github.com/spf13/cobra"
)

func newShadowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shadow",
		Short: "Shadow training management",
		Long: `Manage shadow training data, examples, and adapters.

Shadow training enables model improvement through:
- Teacher model shadowing of student responses
- Quality scoring and preference pair generation
- Few-shot example extraction for in-context learning
- Training data export for external fine-tuning`,
	}

	cmd.AddCommand(newShadowStatusCmd())
	cmd.AddCommand(newShadowExportCmd())
	cmd.AddCommand(newShadowExportDBCmd())
	cmd.AddCommand(newShadowExamplesCmd())
	cmd.AddCommand(newShadowAdaptersCmd())

	return cmd
}

func newShadowStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   cmdStatus,
		Short: "Show shadow training status and statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Load config and create manager
			manager, err := createShadowManager()
			if err != nil {
				return err
			}
			defer manager.Close()

			if !manager.IsEnabled() {
				fmt.Println("shadow training: disabled")
				fmt.Println("\nTo enable, add [shadow] section to ~/.meept/meept.toml")
				return nil
			}

			stats, err := manager.GetStats(ctx)
			if err != nil {
				return fmt.Errorf("failed to get stats: %w", err)
			}

			fmt.Println("shadow training status")
			fmt.Println("=====================")
			fmt.Printf("total records:     %d\n", stats.TotalRecords)
			fmt.Printf("high quality:      %d\n", stats.HighQualityCount)
			fmt.Printf("preference pairs:  %d\n", stats.PreferencePairs)
			fmt.Printf("few-shot examples: %d\n", stats.FewShotExamples)
			fmt.Printf("avg quality:       %.2f\n", stats.AvgQualityScore)
			fmt.Printf("teacher queries:   %d (today)\n", stats.TeacherQueries)
			fmt.Printf("teacher cost:      $%.2f (today)\n", stats.TeacherCostToday)

			if len(stats.RecordsByDomain) > 0 {
				fmt.Println("\nrecords by domain:")
				for domain, count := range stats.RecordsByDomain {
					fmt.Printf("  %s: %d\n", domain, count)
				}
			}

			if len(stats.RecordsByTaskType) > 0 {
				fmt.Println("\nrecords by task type:")
				for taskType, count := range stats.RecordsByTaskType {
					fmt.Printf("  %s: %d\n", taskType, count)
				}
			}

			return nil
		},
	}
}

func newShadowExportCmd() *cobra.Command {
	var (
		format       string
		minQuality   float64
		minMargin    float64
		since        string
		output       string
		markExported bool
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export training data",
		Long: `Export shadow training data in various formats:
  - jsonl: General purpose JSONL format
  - dpo: Direct Preference Optimization format (chosen/rejected pairs)
  - openai: OpenAI fine-tuning format
  - alpaca: Alpaca instruction format`,
		Example: `  meept shadow export --format=dpo --min-quality=0.8
  meept shadow export --format=openai --since=2026-02-01
  meept shadow export --format=jsonl --output=/tmp/training.jsonl`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			manager, err := createShadowManager()
			if err != nil {
				return err
			}
			defer manager.Close()

			if !manager.IsEnabled() {
				return fmt.Errorf("shadow training is not enabled")
			}

			opts := shadow.ExportOptions{
				Format:         shadow.ExportFormat(format),
				MinQuality:     minQuality,
				MinMargin:      minMargin,
				OutputPath:     output,
				MarkAsExported: markExported,
			}

			if since != "" {
				t, err := time.Parse("2006-01-02", since)
				if err != nil {
					return fmt.Errorf("invalid date format: %w", err)
				}
				opts.Since = &t
			}

			fmt.Printf("exporting %s format...\n", format)

			result, err := manager.Export(ctx, opts)
			if err != nil {
				return err
			}

			fmt.Printf("exported %d records to %s\n", result.RecordsExported, result.OutputPath)
			fmt.Printf("duration: %s\n", result.Duration.Round(time.Millisecond))

			return nil
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "jsonl", "Output format (jsonl, dpo, openai, alpaca)")
	cmd.Flags().Float64VarP(&minQuality, "min-quality", "q", 0.0, "Minimum quality score")
	cmd.Flags().Float64VarP(&minMargin, "min-margin", "m", 0.0, "Minimum preference margin (for DPO)")
	cmd.Flags().StringVar(&since, "since", "", "Only export records since date (YYYY-MM-DD)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file path")
	cmd.Flags().BoolVar(&markExported, "mark-exported", true, "Mark records as exported")

	return cmd
}

func newShadowExamplesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "examples",
		Short: "Manage few-shot examples",
	}

	cmd.AddCommand(newShadowExamplesListCmd())
	cmd.AddCommand(newShadowExamplesPruneCmd())
	cmd.AddCommand(newShadowExamplesRebuildCmd())

	return cmd
}

func newShadowExamplesListCmd() *cobra.Command {
	var (
		domain   string
		taskType string
		limit    int
	)

	cmd := &cobra.Command{
		Use:   cmdList,
		Short: "List few-shot examples",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			manager, err := createShadowManager()
			if err != nil {
				return err
			}
			defer manager.Close()

			if !manager.IsEnabled() {
				return fmt.Errorf("shadow training is not enabled")
			}

			examples, err := manager.GetFewShotExamples(ctx,
				shadow.Domain(domain),
				shadow.TaskType(taskType),
				"", // No specific query
				limit,
			)
			if err != nil {
				return err
			}

			if len(examples) == 0 {
				fmt.Println("no examples found")
				return nil
			}

			fmt.Printf("found %d examples:\n\n", len(examples))
			for _, ex := range examples {
				fmt.Printf("id: %s\n", ex.ID)
				fmt.Printf("domain: %s, task: %s, quality: %.2f, uses: %d\n",
					ex.Domain, ex.TaskType, ex.QualityScore, ex.UsageCount)
				fmt.Printf("query: %s\n", truncate(ex.UserMessage, 80))
				fmt.Printf("response: %s\n", truncate(ex.AssistantResponse, 80))
				fmt.Println("---")
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&domain, "domain", "d", "", "Filter by domain")
	cmd.Flags().StringVarP(&taskType, "task-type", "t", "", "Filter by task type")
	cmd.Flags().IntVarP(&limit, "limit", "n", 10, "Maximum examples to show")

	return cmd
}

func newShadowExamplesPruneCmd() *cobra.Command {
	var maxAgeDays int

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove old examples",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			manager, err := createShadowManager()
			if err != nil {
				return err
			}
			defer manager.Close()

			if !manager.IsEnabled() {
				return fmt.Errorf("shadow training is not enabled")
			}

			count, err := manager.PruneExamples(ctx, maxAgeDays)
			if err != nil {
				return err
			}

			fmt.Printf("pruned %d examples older than %d days\n", count, maxAgeDays)
			return nil
		},
	}

	cmd.Flags().IntVar(&maxAgeDays, "max-age", 30, "Maximum age in days")

	return cmd
}

func newShadowExamplesRebuildCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rebuild",
		Short: "Rebuild examples from training data",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			manager, err := createShadowManager()
			if err != nil {
				return err
			}
			defer manager.Close()

			if !manager.IsEnabled() {
				return fmt.Errorf("shadow training is not enabled")
			}

			fmt.Println("rebuilding examples from training data...")

			if err := manager.RebuildExamples(ctx); err != nil {
				return err
			}

			fmt.Println("examples rebuilt successfully")
			return nil
		},
	}
}

func newShadowExportDBCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export-db",
		Short: "Export portable training database",
		Long: `Copy the training.db file for use on a separate training machine.

The training.db is self-contained and portable. It includes:
  - Raw shadow records with full message history
  - Pre-computed preference pairs
  - Quality scores and metadata
  - Export tracking (what was already exported)

Example workflow:
  1. meept shadow export-db --output=/mnt/shared/training.db
  2. On training machine: use training.db with Axolotl, LLaMA-Factory, etc.
  3. Copy trained adapter back and register with 'meept shadow adapters register'`,
		Example: `  meept shadow export-db --output=/tmp/training.db
  meept shadow export-db --output=/mnt/shared/meept-training.db`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			manager, err := createShadowManager()
			if err != nil {
				return err
			}
			defer manager.Close()

			if !manager.IsEnabled() {
				return fmt.Errorf("shadow training is not enabled")
			}

			output, _ := cmd.Flags().GetString("output")
			if output == "" {
				timestamp := time.Now().Format("20060102")
				output = fmt.Sprintf("training_%s.db", timestamp)
			}

			// Expand ~ in output path
			if strings.HasPrefix(output, "~") {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("failed to expand home directory: %w", err)
				}
				output = filepath.Join(home, output[1:])
			}

			fmt.Printf("exporting training database...\n")
			fmt.Printf("  source: %s\n", manager.TrainingDBPath())
			fmt.Printf("  output: %s\n", output)

			if err := manager.ExportDatabase(ctx, output); err != nil {
				return err
			}

			// Get file size
			info, err := os.Stat(output)
			if err == nil {
				fmt.Printf("  size:   %.1f MB\n", float64(info.Size())/(1024*1024))
			}

			fmt.Println("export complete")
			return nil
		},
	}

	cmd.Flags().StringP("output", "o", "", "Output file path (default: training_YYYYMMDD.db)")

	return cmd
}

func newShadowAdaptersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "adapters",
		Short: "Manage LoRA/soft-prompt adapters",
	}

	cmd.AddCommand(newShadowAdaptersListCmd())
	cmd.AddCommand(newShadowAdaptersRegisterCmd())
	cmd.AddCommand(newShadowAdaptersActivateCmd())
	cmd.AddCommand(newShadowAdaptersTrainCmd())

	return cmd
}

func newShadowAdaptersListCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   cmdList,
		Short: "List registered adapters",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			manager, err := createShadowManager()
			if err != nil {
				return err
			}
			defer manager.Close()

			if !manager.IsEnabled() {
				return fmt.Errorf("shadow training is not enabled")
			}

			adapters, err := manager.ListAdapters(ctx)
			if err != nil {
				return err
			}

			if len(adapters) == 0 {
				fmt.Println("no adapters registered")
				return nil
			}

			if jsonOutput {
				data, _ := json.MarshalIndent(adapters, "", "  ")
				fmt.Println(string(data))
				return nil
			}

			fmt.Printf("found %d adapters:\n\n", len(adapters))
			for _, a := range adapters {
				active := ""
				if a.IsActive {
					active = " [active]"
				}
				fmt.Printf("name: %s%s\n", a.Name, active)
				fmt.Printf("  id: %s\n", a.ID)
				fmt.Printf("  base: %s, type: %s\n", a.ModelBase, a.AdapterType)
				fmt.Printf("  path: %s\n", a.AdapterPath)
				fmt.Printf("  records: %d\n", a.TrainingRecords)
				fmt.Println()
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	return cmd
}

func newShadowAdaptersRegisterCmd() *cobra.Command {
	var (
		name        string
		modelBase   string
		adapterType string
	)

	cmd := &cobra.Command{
		Use:   "register <path>",
		Short: "Register a new adapter",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			adapterPath := args[0]

			// Expand path
			if strings.HasPrefix(adapterPath, "~") {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("failed to expand home directory: %w", err)
				}
				adapterPath = filepath.Join(home, adapterPath[1:])
			}

			// Validate path exists
			if _, err := os.Stat(adapterPath); err != nil {
				return fmt.Errorf("adapter path not found: %w", err)
			}

			// Generate name if not provided
			if name == "" {
				name = filepath.Base(adapterPath)
			}

			manager, err := createShadowManager()
			if err != nil {
				return err
			}
			defer manager.Close()

			if !manager.IsEnabled() {
				return fmt.Errorf("shadow training is not enabled")
			}

			adapter := shadow.NewAdapter(name, modelBase, adapterType, adapterPath)

			if err := manager.RegisterAdapter(ctx, adapter); err != nil {
				return err
			}

			fmt.Printf("registered adapter: %s\n", adapter.Name)
			fmt.Printf("  id: %s\n", adapter.ID)
			fmt.Printf("  base model: %s\n", adapter.ModelBase)
			fmt.Printf("  type: %s\n", adapter.AdapterType)

			return nil
		},
	}

	cmd.Flags().StringVarP(&name, "name", "n", "", "Adapter name (defaults to directory name)")
	cmd.Flags().StringVarP(&modelBase, "base", "b", "", "Base model this adapts (required)")
	cmd.Flags().StringVarP(&adapterType, "type", "t", "lora", "Adapter type (lora, soft_prompt)")
	_ = cmd.MarkFlagRequired("base")

	return cmd
}

func newShadowAdaptersActivateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "activate <id-or-name>",
		Short: "Activate an adapter",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			idOrName := args[0]

			manager, err := createShadowManager()
			if err != nil {
				return err
			}
			defer manager.Close()

			if !manager.IsEnabled() {
				return fmt.Errorf("shadow training is not enabled")
			}

			// First try to find by name or ID
			adapters, err := manager.ListAdapters(ctx)
			if err != nil {
				return err
			}

			var adapterID string
			for _, a := range adapters {
				if a.ID == idOrName || a.Name == idOrName {
					adapterID = a.ID
					break
				}
			}

			if adapterID == "" {
				return fmt.Errorf("adapter not found: %s", idOrName)
			}

			if err := manager.ActivateAdapter(ctx, adapterID); err != nil {
				return err
			}

			fmt.Printf("activated adapter: %s\n", idOrName)
			return nil
		},
	}
}

func newShadowAdaptersTrainCmd() *cobra.Command {
	var (
		baseModel   string
		adapterType string
		adapterName string
		exportFirst bool
		execute     bool
		backend     string
		outputDir   string
		scriptOnly  bool
	)

	cmd := &cobra.Command{
		Use:   "train",
		Short: "Train a LoRA adapter using collected shadow data",
		Long: `Train a LoRA adapter using DPO (Direct Preference Optimization) on collected shadow training data.

This command can:
  1. Check that enough preference pairs exist
  2. Export DPO-format training data
  3. Execute training using available backends (unsloth, axolotl, trl, llama-factory)
  4. Generate training scripts for manual execution

Training backends (auto-detected if not specified):
  - unsloth:       Fast LoRA training with memory optimizations
  - axolotl:       Flexible training framework with many options
  - trl:           Hugging Face TRL library for DPO
  - llama-factory: Easy-to-use training toolkit`,
		Example: `  # Show training status and instructions
  meept shadow adapters train --base=llama3.2

  # Export data and execute training
  meept shadow adapters train --base=llama3.2 --execute --export

  # Generate training script only
  meept shadow adapters train --base=llama3.2 --script-only --backend=unsloth

  # Full training with custom name
  meept shadow adapters train --base=llama3.2 --name=my-adapter --execute`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			manager, err := createShadowManager()
			if err != nil {
				return err
			}
			defer manager.Close()

			if !manager.IsEnabled() {
				return fmt.Errorf("shadow training is not enabled")
			}

			// Check preference pair count
			pairCount, err := manager.GetPreferencePairCount(ctx)
			if err != nil {
				return fmt.Errorf("failed to count preference pairs: %w", err)
			}

			cfg := manager.Config()
			threshold := cfg.Adapters.TrainThreshold
			if threshold <= 0 {
				threshold = 500
			}

			fmt.Printf("preference pairs available: %d\n", pairCount)
			fmt.Printf("training threshold:         %d\n", threshold)

			if pairCount < threshold && !execute {
				fmt.Printf("\nnot enough data for training (need %d more pairs)\n", threshold-pairCount)
				fmt.Println("continue using meept to collect more shadow training data")
				fmt.Println("\nuse --execute to force training with current data")
				return nil
			}

			fmt.Println("\nsufficient data available for training")

			// Create trainer
			trainer := shadow.NewTrainer(&cfg.Adapters, nil)

			// Detect available backends
			backends, _ := trainer.DetectBackend()
			if len(backends) > 0 {
				fmt.Printf("\navailable training backends: %v\n", backends)
			} else {
				fmt.Println("\nno training backends detected")
				fmt.Println("install one of: unsloth, axolotl, trl, llama-factory")
			}

			// Determine output directory
			if outputDir == "" {
				outputDir, err = expandShadowPath(cfg.Adapters.AdapterDir)
				if err != nil {
					return err
				}
			}

			// Generate adapter name if not provided
			if adapterName == "" {
				timestamp := time.Now().Format("20060102-150405")
				adapterName = fmt.Sprintf("%s-adapter-%s", strings.ReplaceAll(baseModel, "/", "-"), timestamp)
			}

			// Export DPO data
			var dataPath string
			if exportFirst || execute {
				timestamp := time.Now().Format("20060102")
				exportDir, err := expandShadowPath(cfg.Export.OutputDir)
				if err != nil {
					return err
				}
				dataPath = filepath.Join(exportDir, fmt.Sprintf("dpo_%s.jsonl", timestamp))

				result, err := manager.Export(ctx, shadow.ExportOptions{
					Format:         shadow.FormatDPO,
					OutputPath:     dataPath,
					MarkAsExported: execute, // Only mark as exported if actually training
				})
				if err != nil {
					return fmt.Errorf("failed to export training data: %w", err)
				}

				fmt.Printf("\nexported %d pairs to %s\n", result.RecordsExported, result.OutputPath)
				dataPath = result.OutputPath
			}

			// Prepare training options
			trainOpts := shadow.TrainOptions{
				BaseModel:   baseModel,
				DataPath:    dataPath,
				OutputDir:   outputDir,
				AdapterName: adapterName,
				OnOutput: func(line string) {
					fmt.Printf("  %s\n", line)
				},
			}

			if backend != "" {
				trainOpts.Backend = shadow.TrainerBackend(backend)
			}

			// Generate script only
			if scriptOnly {
				if trainOpts.Backend == "" {
					trainOpts.Backend = shadow.TrainerUnsloth // Default
				}
				script, err := trainer.GenerateTrainingScript(trainOpts)
				if err != nil {
					return fmt.Errorf("failed to generate script: %w", err)
				}

				scriptPath := filepath.Join(outputDir, fmt.Sprintf("train_%s.py", trainOpts.Backend))
				if trainOpts.Backend == shadow.TrainerAxolotl {
					scriptPath = filepath.Join(outputDir, "axolotl_config.yaml")
				}

				//nolint:gosec // user config directory/file permissions
				if err := os.MkdirAll(outputDir, 0o755); err != nil {
					return fmt.Errorf("failed to create output directory: %w", err)
				}

				//nolint:gosec // user config directory/file permissions
				if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
					return fmt.Errorf("failed to write script: %w", err)
				}

				fmt.Printf("\ntraining script written to: %s\n", scriptPath)
				fmt.Println("\nto run manually:")
				if trainOpts.Backend == shadow.TrainerAxolotl {
					fmt.Printf("  accelerate launch -m axolotl.cli.train %s\n", scriptPath)
				} else {
					fmt.Printf("  python3 %s\n", scriptPath)
				}
				return nil
			}

			// Execute training
			if execute {
				if dataPath == "" {
					return fmt.Errorf("no training data available; use --export to export data first")
				}

				fmt.Printf("\nstarting training...\n")
				fmt.Printf("  base model:   %s\n", baseModel)
				fmt.Printf("  adapter name: %s\n", adapterName)
				fmt.Printf("  output dir:   %s\n", outputDir)
				fmt.Println()

				result, err := trainer.Train(ctx, trainOpts)
				if err != nil {
					return fmt.Errorf("training failed: %w", err)
				}

				if result.Success {
					fmt.Println("\ntraining completed successfully!")
					fmt.Printf("  adapter path: %s\n", result.AdapterPath)
					fmt.Printf("  final loss:   %.4f\n", result.FinalLoss)
					fmt.Printf("  duration:     %s\n", result.Duration.Round(time.Second))

					fmt.Println("\nto register and activate the adapter:")
					fmt.Printf("  meept shadow adapters register %s --base=%s\n", result.AdapterPath, baseModel)
					fmt.Printf("  meept shadow adapters activate %s\n", adapterName)
				} else {
					fmt.Printf("\ntraining failed: %s\n", result.ErrorMessage)
				}

				return nil
			}

			// Print training instructions (default behavior)
			fmt.Printf("\ntraining instructions for %s (%s):\n", baseModel, adapterType)
			fmt.Println("================================================")
			fmt.Println()
			fmt.Println("option 1: automated training (recommended)")
			fmt.Println("   meept shadow adapters train --base=" + baseModel + " --execute --export")
			fmt.Println()
			fmt.Println("option 2: generate script and run manually")
			fmt.Println("   meept shadow adapters train --base=" + baseModel + " --script-only --export")
			fmt.Println()
			fmt.Println("option 3: manual training with axolotl")
			fmt.Println("   1. meept shadow export --format=dpo --min-quality=0.8")
			fmt.Printf("   2. axolotl train --base-model=%s --method=dpo \\\n", baseModel)
			fmt.Println("        --dataset=<exported_dpo_file>.jsonl")
			fmt.Println()
			fmt.Println("after training:")
			fmt.Printf("   meept shadow adapters register <adapter_path> --base=%s --type=%s\n", baseModel, adapterType)
			fmt.Println("   meept shadow adapters activate <adapter_name>")

			return nil
		},
	}

	cmd.Flags().StringVarP(&baseModel, "base", "b", "llama3.2", "Base model to fine-tune")
	cmd.Flags().StringVarP(&adapterType, "type", "t", "lora", "Adapter type (lora, soft_prompt)")
	cmd.Flags().StringVarP(&adapterName, "name", "n", "", "Name for the trained adapter")
	cmd.Flags().BoolVar(&exportFirst, "export", false, "Export DPO data before training")
	cmd.Flags().BoolVar(&execute, "execute", false, "Execute training (requires GPU)")
	cmd.Flags().StringVar(&backend, "backend", "", "Training backend (unsloth, axolotl, trl, llama-factory)")
	cmd.Flags().StringVarP(&outputDir, "output", "o", "", "Output directory for adapter")
	cmd.Flags().BoolVar(&scriptOnly, "script-only", false, "Generate training script without executing")

	return cmd
}

// Helper functions

func createShadowManager() (*shadow.Manager, error) {
	// Load config from meept.toml
	cfg, err := config.LoadDefault()
	if err != nil {
		// Fall back to defaults if config can't be loaded
		cfg = config.DefaultConfig()
	}

	// Convert config.ShadowConfig to shadow.Config
	shadowCfg := cliConvertShadowConfig(cfg.Shadow)

	// Ensure enabled for CLI usage (data dir must exist to read from)
	shadowCfg.Enabled = true

	return shadow.NewManager(shadow.ManagerConfig{
		Config: shadowCfg,
		Logger: nil, // Use default
	})
}

// cliConvertShadowConfig converts config.ShadowConfig to shadow.Config.
func cliConvertShadowConfig(cfg config.ShadowConfig) *shadow.Config {
	return &shadow.Config{
		Enabled: cfg.Enabled,
		DataDir: cfg.DataDir,
		Shadowing: shadow.ShadowingConfig{
			Mode:          shadow.ShadowMode(cfg.Shadowing.Mode),
			MinComplexity: shadow.Complexity(cfg.Shadowing.MinComplexity),
			Domains:       cfg.Shadowing.Domains,
			TaskTypes:     cfg.Shadowing.TaskTypes,
			SampleRate:    cfg.Shadowing.SampleRate,
			QueueSize:     cfg.Shadowing.QueueSize,
			WorkerCount:   cfg.Shadowing.WorkerCount,
		},
		Teacher: shadow.TeacherConfig{
			Model:             cfg.Teacher.Model,
			FallbackModel:     cfg.Teacher.FallbackModel,
			Temperature:       cfg.Teacher.Temperature,
			MaxTokens:         cfg.Teacher.MaxTokens,
			TimeoutSeconds:    cfg.Teacher.TimeoutSeconds,
			MaxDailyQueries:   cfg.Teacher.MaxDailyQueries,
			MaxDailyCost:      cfg.Teacher.MaxDailyCost,
			RequestsPerMinute: cfg.Teacher.RequestsPerMinute,
		},
		Quality: shadow.QualityConfig{
			Method:               shadow.QualityMethod(cfg.Quality.Method),
			HighQualityThreshold: cfg.Quality.HighQualityThreshold,
			TrainableThreshold:   cfg.Quality.TrainableThreshold,
			PreferenceMargin:     cfg.Quality.PreferenceMargin,
			HeuristicWeights: shadow.HeuristicWeights{
				Relevance:    cfg.Quality.HeuristicWeights.Relevance,
				Completeness: cfg.Quality.HeuristicWeights.Completeness,
				Correctness:  cfg.Quality.HeuristicWeights.Correctness,
				Style:        cfg.Quality.HeuristicWeights.Style,
			},
			EvalPromptTemplate: cfg.Quality.EvalPromptTemplate,
		},
		Examples: shadow.ExamplesConfig{
			Enabled:          cfg.Examples.Enabled,
			MaxPerCategory:   cfg.Examples.MaxPerCategory,
			MinQuality:       cfg.Examples.MinQuality,
			DefaultCount:     cfg.Examples.DefaultCount,
			MaxCount:         cfg.Examples.MaxCount,
			SimilarityWeight: cfg.Examples.SimilarityWeight,
			RecencyWeight:    cfg.Examples.RecencyWeight,
			QualityWeight:    cfg.Examples.QualityWeight,
			MaxContextTokens: cfg.Examples.MaxContextTokens,
		},
		Export: shadow.ExportConfig{
			OutputDir:                cfg.Export.OutputDir,
			Formats:                  cfg.Export.Formats,
			MinRecords:               cfg.Export.MinRecords,
			IncludeLowQuality:        cfg.Export.IncludeLowQuality,
			Deduplicate:              cfg.Export.Deduplicate,
			DedupSimilarityThreshold: cfg.Export.DedupSimilarityThreshold,
		},
		Adapters: shadow.AdaptersConfig{
			Enabled:        cfg.Adapters.Enabled,
			OllamaEndpoint: cfg.Adapters.OllamaEndpoint,
			AutoTrain:      cfg.Adapters.AutoTrain,
			TrainThreshold: cfg.Adapters.TrainThreshold,
			TrainSchedule:  cfg.Adapters.TrainSchedule,
			AdapterDir:     cfg.Adapters.AdapterDir,
			LoRA: shadow.LoRAConfig{
				Rank:                 cfg.Adapters.LoRA.Rank,
				Alpha:                cfg.Adapters.LoRA.Alpha,
				Dropout:              cfg.Adapters.LoRA.Dropout,
				TargetModules:        cfg.Adapters.LoRA.TargetModules,
				LearningRate:         cfg.Adapters.LoRA.LearningRate,
				Epochs:               cfg.Adapters.LoRA.Epochs,
				BatchSize:            cfg.Adapters.LoRA.BatchSize,
				GradientAccumulation: cfg.Adapters.LoRA.GradientAccumulation,
				WarmupRatio:          cfg.Adapters.LoRA.WarmupRatio,
				MaxGradNorm:          cfg.Adapters.LoRA.MaxGradNorm,
			},
			DPO: shadow.DPOConfig{
				Beta:     cfg.Adapters.DPO.Beta,
				LossType: cfg.Adapters.DPO.LossType,
			},
		},
	}
}

func expandShadowPath(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to expand home directory: %w", err)
		}
		return filepath.Join(home, path[1:]), nil
	}
	return path, nil
}

func truncate(s string, maxLen int) string {
	// Remove newlines for display
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")

	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}
