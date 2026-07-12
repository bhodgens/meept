package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/learning"
	"github.com/spf13/cobra"
)

func newLearningCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "learning",
		Short: "lora learning pipeline management",
		Long: `Manage the lora learning pipeline: capture, consolidate, train.

Captures agent research trajectories, scores them, and produces
domain-routed training datasets. Train lora adapters from captured data.`,
	}

	cmd.AddCommand(newLearningTrainCmd())
	cmd.AddCommand(newLearningStatusCmd())
	cmd.AddCommand(newLearningListCmd())
	cmd.AddCommand(newLearningDatasetStatsCmd())
	cmd.AddCommand(newLearningConsolidateCmd())
	cmd.AddCommand(newLearningSnapshotCmd())
	cmd.AddCommand(newLearningFeedbackCmd())
	cmd.AddCommand(newLearningAutoTrainCmd())

	return cmd
}

func newLearningTrainCmd() *cobra.Command {
	var model string
	cmd := &cobra.Command{
		Use:   "train [domain]",
		Short: "train a lora adapter for a domain",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			domain := args[0]
			return runTraining(domain, model)
		},
	}
	cmd.Flags().StringVar(&model, "model", "lfm2.5-8b", "base model to train (lfm2.5-8b or lfm2.5-1.2b)")
	return cmd
}

func newLearningStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "show learning pipeline status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return showLearningStatus()
		},
	}
}

func newLearningListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "list trained adapters",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listAdapters()
		},
	}
}

func newLearningDatasetStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dataset-stats [domain]",
		Short: "show dataset statistics for a domain",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return showDatasetStats(args[0])
		},
	}
}

func newLearningConsolidateCmd() *cobra.Command {
	var minQuality float64
	cmd := &cobra.Command{
		Use:   "consolidate",
		Short: "process raw captures into domain datasets",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConsolidate(minQuality)
		},
	}
	cmd.Flags().Float64Var(&minQuality, "min-quality", 0.6, "minimum quality score to include")
	return cmd
}

// learningPaths resolves data and adapter directories from config (with
// expanded ~), falling back to ~/.meept defaults when config is unavailable.
func learningPaths() (learningDir, adaptersDir string, cfg *config.Config, err error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", "", nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	learningDir = filepath.Join(homeDir, ".meept", "learning")
	adaptersDir = filepath.Join(homeDir, ".meept", "adapters")

	cfg, loadErr := config.LoadDefault()
	if loadErr != nil {
		// Config optional for offline/status use; keep home defaults.
		return learningDir, adaptersDir, nil, nil
	}
	if cfg.Learning.DataDir != "" {
		learningDir = cfg.Learning.DataDir
	}
	if cfg.Learning.AdaptersDir != "" {
		adaptersDir = cfg.Learning.AdaptersDir
	}
	return learningDir, adaptersDir, cfg, nil
}

func runConsolidate(minQuality float64) error {
	learningDir, _, cfg, err := learningPaths()
	if err != nil {
		return err
	}

	rawPath := filepath.Join(learningDir, "raw_captures.jsonl")
	datasetsDir := filepath.Join(learningDir, "datasets")

	if err := os.MkdirAll(datasetsDir, 0o755); err != nil {
		return fmt.Errorf("create datasets dir: %w", err)
	}

	// Load retention config; fall back to 100MB default on error.
	maxDatasetMB := 100
	keepVersions := 3
	if cfg != nil {
		if cfg.Learning.Retention.MaxDatasetSizeMB > 0 {
			maxDatasetMB = cfg.Learning.Retention.MaxDatasetSizeMB
		}
		if cfg.Learning.Retention.KeepVersions > 0 {
			keepVersions = cfg.Learning.Retention.KeepVersions
		}
		if minQuality <= 0 && cfg.Learning.Capture.MinQualityScore > 0 {
			minQuality = cfg.Learning.Capture.MinQualityScore
		}
	}
	maxDatasetBytes := int64(maxDatasetMB) * 1024 * 1024

	stats, err := learning.Consolidate(rawPath, datasetsDir, minQuality, maxDatasetBytes)
	if err != nil {
		return fmt.Errorf("consolidate failed: %w", err)
	}

	fmt.Println("consolidation complete")
	fmt.Println("=====================")
	fmt.Printf("processed:  %d\n", stats.Processed)
	fmt.Printf("added:      %d\n", stats.Added)
	fmt.Printf("skipped:    %d\n", stats.Skipped)
	fmt.Printf("duplicates: %d\n", stats.Duplicates)

	versionsDir := filepath.Join(learningDir, "versions")
	for _, domain := range stats.DomainsTouched {
		snap, err := learning.CreateSnapshot(domain, datasetsDir, versionsDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: snapshot %s failed: %v\n", domain, err)
			continue
		}
		fmt.Printf("snapshot:  %s v%d (%d examples, md5=%s)\n", domain, snap.Version, snap.ExampleCount, snap.MD5[:8])
		if pruned, err := learning.PruneOldVersions(domain, versionsDir, keepVersions); err != nil {
			fmt.Fprintf(os.Stderr, "warning: prune %s versions: %v\n", domain, err)
		} else if pruned > 0 {
			fmt.Printf("pruned:    %s removed %d old version(s) (keep=%d)\n", domain, pruned, keepVersions)
		}
	}

	// Update metadata.json with current provenance/stats.
	meta, err := learning.LoadMetadata(learningDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: load metadata: %v\n", err)
		meta = &learning.LearningMetadata{}
	}
	meta.LastConsolidatedAt = time.Now().UTC()
	meta, err = learning.RefreshDomainStats(learningDir, meta)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: refresh domain stats: %v\n", err)
	}
	if n, err := countLines(rawPath); err == nil {
		meta.RawCapturesCount = n
	}
	if err := learning.SaveMetadata(learningDir, meta); err != nil {
		fmt.Fprintf(os.Stderr, "warning: save metadata: %v\n", err)
	}

	// Training readiness / auto-train when domains cross auto_train_threshold.
	if cfg != nil && meta != nil {
		threshold := cfg.Learning.Training.AutoTrainThreshold
		if threshold <= 0 {
			threshold = 500
		}
		ready := learning.DomainsReadyForTrain(meta, threshold)
		if cfg.Learning.Training.ManualOnly {
			for _, domain := range ready {
				ds := meta.DomainStats[domain]
				fmt.Printf("ready:     %s has %d examples (threshold=%d); train with: meept learning train %s\n",
					domain, ds.ExampleCount, threshold, domain)
			}
		} else if len(ready) > 0 {
			model := cfg.Learning.Training.DefaultModel
			if model == "" {
				model = "lfm2.5-8b"
			}
			fmt.Printf("auto-train: manual_only=false; training %d domain(s) with %s\n", len(ready), model)
			for _, domain := range ready {
				ds := meta.DomainStats[domain]
				fmt.Printf("auto-train: %s (%d examples)...\n", domain, ds.ExampleCount)
				if err := learning.MarkAutoTrainStarted(learningDir, domain, model, ds.ExampleCount); err != nil {
					fmt.Fprintf(os.Stderr, "auto-train: mark started failed for %s: %v\n", domain, err)
				}
				if err := runTraining(domain, model); err != nil {
					fmt.Fprintf(os.Stderr, "auto-train failed for %s: %v\n", domain, err)
					if err := learning.MarkAutoTrainFailed(learningDir, domain, model, ds.ExampleCount); err != nil {
						fmt.Fprintf(os.Stderr, "auto-train: mark failed failed for %s: %v\n", domain, err)
					}
					continue
				}
				if err := learning.MarkAutoTrainCompleted(learningDir, domain, model, ds.ExampleCount); err != nil {
					fmt.Fprintf(os.Stderr, "auto-train: mark completed failed for %s: %v\n", domain, err)
				}
				fmt.Printf("auto-train: %s completed\n", domain)
			}
		}
	}

	return nil
}

func newLearningFeedbackCmd() *cobra.Command {
	var trajectoryID string
	cmd := &cobra.Command{
		Use:   "feedback <session_id> <positive|negative|neutral>",
		Short: "apply user feedback to captured research trajectories",
		Long: `Update TaskOutcome.UserFeedback on raw captures for a session.

Positive feedback raises quality score (+0.15) during consolidate;
negative lowers it (-0.2). Neutral clears feedback.

Examples:
  meept learning feedback sess-abc positive
  meept learning feedback sess-abc negative --trajectory=ltraj-xyz`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeedback(args[0], args[1], trajectoryID)
		},
	}
	cmd.Flags().StringVar(&trajectoryID, "trajectory", "", "optional trajectory id (default: all for session)")
	return cmd
}

func runFeedback(sessionID, feedback, trajectoryID string) error {
	learningDir, _, _, err := learningPaths()
	if err != nil {
		return err
	}
	result, err := learning.ApplyUserFeedback(learningDir, sessionID, trajectoryID, feedback)
	if err != nil {
		return err
	}
	label, _ := learning.NormalizeFeedback(feedback)
	fmt.Printf("feedback applied: %s\n", label)
	fmt.Printf("  session:     %s\n", sessionID)
	if trajectoryID != "" {
		fmt.Printf("  trajectory:  %s\n", trajectoryID)
	}
	fmt.Printf("  matched:     %d\n", result.Matched)
	fmt.Printf("  updated:     %d\n", result.Updated)
	if result.Matched == 0 {
		fmt.Println("  hint: no captures for this session; check session id in raw_captures.jsonl")
	} else {
		fmt.Println("  hint: re-run `meept learning consolidate` to re-score into datasets")
	}
	return nil
}

func newLearningAutoTrainCmd() *cobra.Command {
	var force bool
	var model string
	cmd := &cobra.Command{
		Use:   "auto-train",
		Short: "train adapters for domains at or above auto_train_threshold",
		Long: `Train every domain whose dataset size meets learning.training.auto_train_threshold.

Respects last-auto-train metadata so the same data size is not re-trained
unless --force is set. Works even when manual_only=true (explicit invocation).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAutoTrain(model, force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "re-train even if last auto-train covered current size")
	cmd.Flags().StringVar(&model, "model", "", "base model (default: config training.default_model)")
	return cmd
}

func runAutoTrain(model string, force bool) error {
	learningDir, _, cfg, err := learningPaths()
	if err != nil {
		return err
	}
	if model == "" {
		model = "lfm2.5-8b"
		if cfg != nil && cfg.Learning.Training.DefaultModel != "" {
			model = cfg.Learning.Training.DefaultModel
		}
	}
	threshold := 500
	if cfg != nil && cfg.Learning.Training.AutoTrainThreshold > 0 {
		threshold = cfg.Learning.Training.AutoTrainThreshold
	}

	meta, err := learning.LoadMetadata(learningDir)
	if err != nil {
		return fmt.Errorf("load metadata: %w", err)
	}
	meta, err = learning.RefreshDomainStats(learningDir, meta)
	if err != nil {
		return fmt.Errorf("refresh domain stats: %w", err)
	}

	var domains []string
	if force {
		for domain, ds := range meta.DomainStats {
			if ds.ExampleCount >= threshold {
				domains = append(domains, domain)
			}
		}
	} else {
		domains = learning.DomainsReadyForTrain(meta, threshold)
	}
	if len(domains) == 0 {
		fmt.Printf("no domains ready (threshold=%d)\n", threshold)
		return nil
	}

	fmt.Printf("auto-train: %d domain(s), model=%s, threshold=%d\n", len(domains), model, threshold)
	var firstErr error
	for _, domain := range domains {
		ds := meta.DomainStats[domain]
		fmt.Printf("training %s (%d examples)...\n", domain, ds.ExampleCount)
		if err := learning.MarkAutoTrainStarted(learningDir, domain, model, ds.ExampleCount); err != nil {
			fmt.Fprintf(os.Stderr, "mark started failed %s: %v\n", domain, err)
		}
		if err := runTraining(domain, model); err != nil {
			fmt.Fprintf(os.Stderr, "failed %s: %v\n", domain, err)
			if err := learning.MarkAutoTrainFailed(learningDir, domain, model, ds.ExampleCount); err != nil {
				fmt.Fprintf(os.Stderr, "mark failed failed %s: %v\n", domain, err)
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := learning.MarkAutoTrainCompleted(learningDir, domain, model, ds.ExampleCount); err != nil {
			fmt.Fprintf(os.Stderr, "mark completed failed %s: %v\n", domain, err)
		}
		fmt.Printf("completed %s\n", domain)
	}
	return firstErr
}

func newLearningSnapshotCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "snapshot [domain]",
		Short: "create a versioned snapshot of a domain dataset",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSnapshot(args[0])
		},
	}
}

func runSnapshot(domain string) error {
	learningDir, _, _, err := learningPaths()
	if err != nil {
		return err
	}

	datasetsDir := filepath.Join(learningDir, "datasets")
	versionsDir := filepath.Join(learningDir, "versions")

	snap, err := learning.CreateSnapshot(domain, datasetsDir, versionsDir)
	if err != nil {
		return fmt.Errorf("snapshot failed: %w", err)
	}

	fmt.Printf("snapshot:  %s v%d (%d examples, md5=%s)\n", domain, snap.Version, snap.ExampleCount, snap.MD5[:8])
	return nil
}

// nextAdapterVersion returns the next free version number for
// {adaptersDir}/{domain}/{model}-vN so re-training never silently overwrites.
func nextAdapterVersion(adaptersDir, domain, model string) (int, error) {
	domainDir := filepath.Join(adaptersDir, domain)
	entries, err := os.ReadDir(domainDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 1, nil
		}
		return 0, err
	}
	prefix := model + "-v"
	maxVer := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		numStr := strings.TrimPrefix(name, prefix)
		n := 0
		valid := true
		for _, c := range numStr {
			if c < '0' || c > '9' {
				valid = false
				break
			}
			n = n*10 + int(c-'0')
		}
		if valid && n > maxVer {
			maxVer = n
		}
	}
	return maxVer + 1, nil
}

func runTraining(domain, model string) error {
	learningDir, adaptersDir, cfg, err := learningPaths()
	if err != nil {
		return err
	}

	if model == "" {
		model = "lfm2.5-8b"
		if cfg != nil && cfg.Learning.Training.DefaultModel != "" {
			model = cfg.Learning.Training.DefaultModel
		}
	}

	datasetPath := filepath.Join(learningDir, "datasets", domain+".jsonl")
	if _, err := os.Stat(datasetPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("dataset not found: %s", datasetPath)
		}
		return fmt.Errorf("stat dataset: %w", err)
	}

	ver, err := nextAdapterVersion(adaptersDir, domain, model)
	if err != nil {
		return fmt.Errorf("resolve adapter version: %w", err)
	}
	outputDir := filepath.Join(adaptersDir, domain, fmt.Sprintf("%s-v%d", model, ver))
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	// Shell out to python training script (relative to CWD / project root).
	scriptPath := filepath.Join("scripts", "train_lora.py")
	if _, err := os.Stat(scriptPath); err != nil {
		return fmt.Errorf("training script not found at %s (run from project root): %w", scriptPath, err)
	}
	trainArgs := []string{
		scriptPath,
		"--model", model,
		"--dataset", datasetPath,
		"--output", outputDir,
	}
	// Prefer stock YAML so train_lora uses official HF model ids / hyperparams.
	configName := map[string]string{
		"lfm2.5-8b":  "lora_lfm2.5_8b.yaml",
		"lfm2.5-1.2b": "lora_lfm2.5_1.2b.yaml",
	}[model]
	if configName != "" {
		cfgPath := filepath.Join("config", "training", configName)
		if _, err := os.Stat(cfgPath); err == nil {
			trainArgs = append(trainArgs, "--config", cfgPath)
		}
	}
	trainCmd := exec.Command("python", trainArgs...)
	trainCmd.Stdout = os.Stdout
	trainCmd.Stderr = os.Stderr

	fmt.Printf("training %s adapter for domain '%s' (v%d)...\n", model, domain, ver)
	if err := trainCmd.Run(); err != nil {
		return fmt.Errorf("training failed: %w (ensure python deps via scripts/setup-training.sh)", err)
	}

	fmt.Printf("adapter saved to %s\n", outputDir)

	// Post-training hook: write training_meta.json and regenerate adapter registry.
	// Pass adapters + datasets roots so custom config paths produce a registry
	// where the daemon loads it (parent of adapters_dir).
	hookPath := filepath.Join("hooks", "on_adapter_trained.sh")
	if _, err := os.Stat(hookPath); err == nil {
		datasetsDir := filepath.Join(learningDir, "datasets")
		hookCmd := exec.Command("bash", hookPath, domain, model, outputDir, adaptersDir, datasetsDir)
		hookCmd.Stdout = os.Stdout
		hookCmd.Stderr = os.Stderr
		if err := hookCmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: post-training hook failed: %v\n", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "warning: post-training hook not found at %s; registry not regenerated\n", hookPath)
	}

	return nil
}

func showLearningStatus() error {
	learningDir, adaptersDir, _, err := learningPaths()
	if err != nil {
		return err
	}

	fmt.Println("learning pipeline status")
	fmt.Println("=======================")
	fmt.Printf("data dir:       %s\n", learningDir)
	fmt.Printf("adapters dir:   %s\n", adaptersDir)

	// Raw captures count.
	rawPath := filepath.Join(learningDir, "raw_captures.jsonl")
	if count, err := countLines(rawPath); err != nil {
		fmt.Printf("raw captures:   0 (file not found)\n")
	} else {
		fmt.Printf("raw captures:   %d\n", count)
	}

	// Datasets.
	datasetsDir := filepath.Join(learningDir, "datasets")
	entries, err := os.ReadDir(datasetsDir)
	if err != nil {
		fmt.Printf("datasets:       none\n")
	} else {
		fmt.Println("datasets:")
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			domain := strings.TrimSuffix(e.Name(), ".jsonl")
			count, _ := countLines(filepath.Join(datasetsDir, e.Name()))
			fmt.Printf("  %-20s %d examples\n", domain, count)
		}
	}

	// Adapters.
	adapterEntries, err := os.ReadDir(adaptersDir)
	if err != nil {
		fmt.Printf("adapters:       none\n")
	} else {
		adapterCount := 0
		for _, e := range adapterEntries {
			if e.IsDir() {
				subEntries, _ := os.ReadDir(filepath.Join(adaptersDir, e.Name()))
				adapterCount += len(subEntries)
			}
		}
		fmt.Printf("adapters:       %d\n", adapterCount)
	}

	// Metadata provenance.
	if meta, err := learning.LoadMetadata(learningDir); err == nil && meta != nil {
		fmt.Println("metadata:")
		if !meta.LastConsolidatedAt.IsZero() {
			fmt.Printf("  last consolidated: %s\n", meta.LastConsolidatedAt.Format(time.RFC3339))
		} else {
			fmt.Printf("  last consolidated: (never)\n")
		}
		fmt.Printf("  raw captures:      %d\n", meta.RawCapturesCount)
		if len(meta.DomainStats) > 0 {
			fmt.Println("  domain stats:")
			for domain, ds := range meta.DomainStats {
				fmt.Printf("    %-20s %d examples, %d bytes\n", domain, ds.ExampleCount, ds.Bytes)
			}
		}
		if len(meta.LastAutoTrain) > 0 {
			fmt.Println("  last auto-train:")
			for domain, rec := range meta.LastAutoTrain {
				fmt.Printf("    %-20s status=%s examples=%d model=%s at=%s\n",
					domain, rec.Status, rec.ExampleCount, rec.Model, rec.TrainedAt.Format(time.RFC3339))
			}
		}
	}

	if pending, err := learning.ListPendingAutoTrain(learningDir); err == nil && len(pending) > 0 {
		fmt.Println("pending auto-train:")
		for _, p := range pending {
			fmt.Printf("  %-20s model=%s examples=%d enqueued=%s\n",
				p.Domain, p.Model, p.ExampleCount, p.EnqueuedAt.Format(time.RFC3339))
		}
	}

	return nil
}

func listAdapters() error {
	_, adaptersDir, _, err := learningPaths()
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(adaptersDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("no adapters found")
			return nil
		}
		return fmt.Errorf("read adapters dir: %w", err)
	}

	fmt.Println("trained adapters")
	fmt.Println("================")
	found := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		domain := e.Name()
		subEntries, _ := os.ReadDir(filepath.Join(adaptersDir, domain))
		for _, se := range subEntries {
			if !se.IsDir() {
				continue
			}
			found = true
			fmt.Printf("  %-12s  %s\n", domain, se.Name())
		}
	}
	if !found {
		fmt.Println("  (none)")
	}
	return nil
}

func showDatasetStats(domain string) error {
	learningDir, _, _, err := learningPaths()
	if err != nil {
		return err
	}

	datasetPath := filepath.Join(learningDir, "datasets", domain+".jsonl")

	f, err := os.Open(datasetPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("dataset '%s' not found\n", domain)
			return nil
		}
		return fmt.Errorf("open dataset: %w", err)
	}
	defer f.Close()

	totalCount := 0
	qualitySum := 0.0

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var example struct {
			Metadata struct {
				Domain       string  `json:"domain"`
				QualityScore float64 `json:"quality_score"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &example); err != nil {
			continue
		}
		totalCount++
		qualitySum += example.Metadata.QualityScore
	}

	fmt.Printf("dataset stats: %s\n", domain)
	fmt.Println("========================")
	fmt.Printf("total examples:  %d\n", totalCount)
	if totalCount > 0 {
		fmt.Printf("avg quality:     %.2f\n", qualitySum/float64(totalCount))
	}

	return nil
}

func countLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}
