package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/caimlas/meept/internal/benchmark"
	"github.com/spf13/cobra"
)

func newBenchmarkCmd() *cobra.Command {
	var (
		instancesFile string
		solverCmd     string
		timeout       time.Duration
		outputFile    string
		cacheDir      string
	)

	cmd := &cobra.Command{
		Use:   "benchmark",
		Short: "run swe-bench-style regression benchmarks",
		Long: `Run SWE-bench-style regression benchmarks against the meept agent harness.

Clones problem repositories at specific commits, feeds issue descriptions
to the solver command, and scores whether the agent produces a plausible
patch (edits apply cleanly, pre-existing tests still pass).

The solver command is any executable that accepts a problem statement and
modifies files in the working directory. Placeholders:
  {problem}   the problem statement
  {repo_dir}  the repository working directory

Examples:
  meept benchmark --instances testdata/benchmark/instances.json
  meept benchmark --solver "meept chat --oneshot" --instances my_instances.json
  meept benchmark --solver "aider --yes-always" --instances instances.json --timeout 15m`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load instances.
			instances, err := benchmark.LoadInstances(instancesFile)
			if err != nil {
				return fmt.Errorf("load instances: %w", err)
			}
			fmt.Printf("loaded %d benchmark instances from %s\n", len(instances), instancesFile)

			// Set up working directory.
			homeDir, _ := os.UserHomeDir()
			workDir := filepath.Join(homeDir, ".meept", "benchmark")
			if cacheDir != "" {
				workDir = cacheDir
			}
			if err := os.MkdirAll(workDir, 0o755); err != nil {
				return fmt.Errorf("create work dir: %w", err)
			}

			// Build solver.
			var solver benchmark.Solver
			if solverCmd != "" {
				solver = &benchmark.ExecSolver{
					Command: solverCmd,
					Timeout: timeout,
				}
			} else {
				// Default: use meept chat in oneshot mode.
				solver = &benchmark.ExecSolver{
					Command: "meept chat --oneshot",
					Timeout: timeout,
				}
			}

			// Build runner.
			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
			runner := benchmark.NewRunner(solver, workDir,
				benchmark.WithTimeout(timeout),
				benchmark.WithLogger(logger),
			)

			// Run benchmark.
			fmt.Printf("running benchmark (timeout: %v per instance)...\n\n", timeout)
			report, err := runner.Run(cmd.Context(), instances)
			if err != nil {
				return fmt.Errorf("benchmark run: %w", err)
			}

			// Print results.
			printReport(report)

			// Save report if output file specified.
			if outputFile != "" {
				if err := benchmark.SaveReport(outputFile, report); err != nil {
					return fmt.Errorf("save report: %w", err)
				}
				fmt.Printf("\nreport saved to %s\n", outputFile)
			}

			// Exit non-zero if any errors occurred (for CI integration).
			if report.Errors > 0 {
				return fmt.Errorf("%d instances had harness errors", report.Errors)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&instancesFile, "instances", "i", "testdata/benchmark/instances.json",
		"path to benchmark instances JSON file")
	cmd.Flags().StringVar(&solverCmd, "solver", "",
		"solver command (default: meept chat --oneshot)")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute,
		"per-instance timeout")
	cmd.Flags().StringVarP(&outputFile, "output", "o", "",
		"save report to JSON file")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", "",
		"working directory for repo checkouts (default: ~/.meept/benchmark)")

	return cmd
}

func printReport(report *benchmark.Report) {
	fmt.Println("══════════════════════════════════════════════════")
	fmt.Println("  benchmark results")
	fmt.Println("══════════════════════════════════════════════════")
	fmt.Printf("  total:     %d\n", report.Total)
	fmt.Printf("  resolved:  %d\n", report.Resolved)
	fmt.Printf("  plausible: %d\n", report.Plausible)
	fmt.Printf("  failed:    %d\n", report.Failed)
	fmt.Printf("  errors:    %d\n", report.Errors)
	fmt.Printf("  score:     %.1f%%\n", report.Score)
	fmt.Printf("  duration:  %v\n", report.CompletedAt.Sub(report.StartedAt).Round(time.Second))
	fmt.Println("══════════════════════════════════════════════════")
	fmt.Println()

	for _, r := range report.Results {
		icon := "✗"
		switch r.Status {
		case benchmark.StatusResolved:
			icon = "✓"
		case benchmark.StatusPlausible:
			icon = "~"
		case benchmark.StatusError:
			icon = "!"
		}
		fmt.Printf("  %s %-40s %-10s %v\n", icon, r.InstanceID, r.Status, r.Duration.Round(time.Millisecond))
		if r.Error != "" {
			fmt.Printf("    error: %s\n", benchTruncate(r.Error, 120))
		}
	}
}

func benchTruncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
