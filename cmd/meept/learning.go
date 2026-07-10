package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

func runConsolidate(minQuality float64) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	learningDir := filepath.Join(homeDir, ".meept", "learning")
	rawPath := filepath.Join(learningDir, "raw_captures.jsonl")
	datasetsDir := filepath.Join(learningDir, "datasets")

	if err := os.MkdirAll(datasetsDir, 0o755); err != nil {
		return fmt.Errorf("create datasets dir: %w", err)
	}

	// Load retention config; fall back to 100MB default on error.
	maxDatasetMB := 100
	if cfg, err := config.LoadDefault(); err == nil {
		maxDatasetMB = cfg.Learning.Retention.MaxDatasetSizeMB
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

	// Snapshot each domain that received additions.
	versionsDir := filepath.Join(learningDir, "versions")
	for _, domain := range stats.DomainsTouched {
		snap, err := learning.CreateSnapshot(domain, datasetsDir, versionsDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: snapshot %s failed: %v\n", domain, err)
			continue
		}
		fmt.Printf("snapshot:  %s v%d (%d examples, md5=%s)\n", domain, snap.Version, snap.ExampleCount, snap.MD5[:8])
	}

	return nil
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
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	learningDir := filepath.Join(homeDir, ".meept", "learning")
	datasetsDir := filepath.Join(learningDir, "datasets")
	versionsDir := filepath.Join(learningDir, "versions")

	snap, err := learning.CreateSnapshot(domain, datasetsDir, versionsDir)
	if err != nil {
		return fmt.Errorf("snapshot failed: %w", err)
	}

	fmt.Printf("snapshot:  %s v%d (%d examples, md5=%s)\n", domain, snap.Version, snap.ExampleCount, snap.MD5[:8])
	return nil
}

func runTraining(domain, model string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	datasetPath := filepath.Join(homeDir, ".meept", "learning", "datasets", domain+".jsonl")
	if _, err := os.Stat(datasetPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("dataset not found: %s", datasetPath)
		}
		return fmt.Errorf("stat dataset: %w", err)
	}

	outputDir := filepath.Join(homeDir, ".meept", "adapters", domain, model+"-v1")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	// Best-effort: shell out to python training script.
	scriptPath := filepath.Join("scripts", "train_lora.py")
	trainCmd := exec.Command("python", scriptPath,
		"--model", model,
		"--dataset", datasetPath,
		"--output", outputDir,
	)
	trainCmd.Stdout = os.Stdout
	trainCmd.Stderr = os.Stderr

	fmt.Printf("training %s adapter for domain '%s'...\n", model, domain)
	if err := trainCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "training failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "ensure python and scripts/train_lora.py are available\n")
		return nil // non-fatal: report error but don't panic
	}

	fmt.Printf("adapter saved to %s\n", outputDir)
	return nil
}

func showLearningStatus() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	learningDir := filepath.Join(homeDir, ".meept", "learning")
	fmt.Println("learning pipeline status")
	fmt.Println("=======================")

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
	adaptersDir := filepath.Join(homeDir, ".meept", "adapters")
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

	return nil
}

func listAdapters() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	adaptersDir := filepath.Join(homeDir, ".meept", "adapters")
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
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	datasetsDir := filepath.Join(homeDir, ".meept", "learning", "datasets")
	datasetPath := filepath.Join(datasetsDir, domain+".jsonl")

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
	domains := map[string]int{}

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
		domains[example.Metadata.Domain]++
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
