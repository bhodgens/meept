package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/caimlas/meept/internal/agent/q"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory/memvid"
	"github.com/spf13/cobra"
)

func newQCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "q",
		Short: "Q Agent (meta-agent for agent optimization)",
		Long: `Q Agent analyzes session transcripts to identify opportunities for creating new specialized agents or improving existing ones.

Q Agent performs:
  - Session analysis for patterns (long duration, high iterations, divergent tasks)
  - Pattern detection across sessions (model misconfiguration, high error rates, wrong agent assignment)
  - Research reports with causal attribution
  - Agent specification generation based on findings
  - Impact estimation for recommendations

Examples:
  meept q analyze          # Run analysis on completed sessions
  meept q status           # Show Q Agent status
  meept q analyze --force  # Force analysis even if disabled`,
	}

	cmd.AddCommand(newQAnalyzeCmd())
	cmd.AddCommand(newQStatusCmd())

	return cmd
}

func newQStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   cmdStatus,
		Short: "Show Q Agent status and configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Load config
			cfg, err := config.LoadDefault()
			if err != nil {
				cfg = config.DefaultConfig()
			}

			// Check memvid availability
			memvidClient := createMemvidClient(cfg)
			if memvidClient == nil {
				fmt.Println("Q Agent status")
				fmt.Println("================")
				fmt.Println("memvid: not available (session analysis requires memvid)")
				return nil
			}

			// Create Q Agent and get status
			qAgent := q.NewQAgent(slog.Default(), cfg.QAgent, memvidClient)
			status, err := qAgent.GetStatus(ctx)
			if err != nil {
				return fmt.Errorf("failed to get status: %w", err)
			}

			fmt.Println("Q Agent status")
			fmt.Println("================")
			fmt.Printf("enabled:         %v\n", status.Enabled)
			fmt.Printf("memvid healthy:  %v\n", status.MemvidHealthy)
			fmt.Printf("sessions tracked: %d\n", status.SessionCount)
			fmt.Printf("analysis dir:    %s\n", status.AnalysisDir)
			fmt.Printf("outcomes log:    %s\n", status.OutcomesLog)
			fmt.Println()
			fmt.Println("configuration:")
			fmt.Printf("  session idle trigger:  %d hours\n", status.Config.SessionIdleTriggerHours)
			fmt.Printf("  analysis timeout:      %d minutes\n", status.Config.AnalysisTimeoutMinutes)
			fmt.Printf("  min sessions/pattern:  %d\n", status.Config.MinSessionsForPattern)
			fmt.Printf("  min confidence:        %.1f%%\n", status.Config.MinConfidenceScore*100)
			fmt.Printf("  high error threshold:  %.0f%%\n", status.Config.HighErrorRateThreshold*100)
			fmt.Printf("  high rejection threshold: %.0f%%\n", status.Config.HighRejectionRateThreshold*100)

			return nil
		},
	}
}

func newQAnalyzeCmd() *cobra.Command {
	var (
		force      bool
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Run Q Agent analysis on completed sessions",
		Long: `Analyze completed sessions to identify agent improvement opportunities.

By default, analysis runs automatically on sessions idle for the configured threshold (default: 12 hours).
Use this command to trigger analysis on-demand.

Examples:
  meept q analyze          # Analyze sessions idle for threshold
  meept q analyze --force  # Analyze regardless of config enabled flag
  meept q analyze --json   # Output as JSON`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Load config
			cfg, err := config.LoadDefault()
			if err != nil {
				cfg = config.DefaultConfig()
			}

			if !cfg.QAgent.Enabled && !force {
				return fmt.Errorf("q agent is disabled; enable in meept.toml or use --force")
			}

			// Check memvid availability
			memvidClient := createMemvidClient(cfg)
			if memvidClient == nil {
				return fmt.Errorf("memvid not available; Q Agent requires memvid for session storage")
			}

			fmt.Println("starting Q Agent analysis...")
			fmt.Printf("analyzing sessions idle for %d+ hours\n\n", cfg.QAgent.SessionIdleTriggerHours)

			// Create Q Agent and run analysis
			qAgent := q.NewQAgent(slog.Default(), cfg.QAgent, memvidClient)
			result, err := qAgent.RunAnalysis(ctx)
			if err != nil {
				return fmt.Errorf("analysis failed: %w", err)
			}

			if jsonOutput {
				data, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal result: %w", err)
				}
				fmt.Println(string(data))
				return nil
			}

			// Print results
			fmt.Println("Analysis Complete")
			fmt.Println("=================")
			fmt.Printf("sessions analyzed: %d\n", result.SessionsAnalyzed)
			fmt.Printf("status:          %s\n", result.Status)
			fmt.Printf("summary:         %s\n\n", result.Summary)

			if len(result.PatternsDetected) == 0 {
				fmt.Println("No significant patterns detected.")
				fmt.Println("All agents are operating within expected parameters.")
				return nil
			}

			fmt.Printf("patterns detected: %d\n\n", len(result.PatternsDetected))

			for i, pattern := range result.PatternsDetected {
				fmt.Printf("%d. %s (%.0f%% confidence)\n", i+1, pattern.PatternType, pattern.Confidence*100)
				affected := pattern.AffectedAgent
				if pattern.AffectedIntent != "" {
					if affected != "" {
						affected += " / "
					}
					affected += pattern.AffectedIntent
				}
				fmt.Printf("   affected: %s\n", affected)
				fmt.Printf("   sessions: %d\n", pattern.SessionCount)
				fmt.Printf("   action:   %s\n\n", pattern.RecommendedAction)
			}

			if len(result.Recommendations) > 0 {
				fmt.Printf("recommendations: %d\n\n", len(result.Recommendations))
				for i, rec := range result.Recommendations {
					fmt.Printf("%d. %s [%s priority]\n", i+1, rec.Title, rec.Priority)
					fmt.Printf("   %s\n", rec.Description)
					fmt.Printf("   expected impact: %s\n\n", rec.ExpectedImpact)
				}
			}

			if len(result.ImpactEstimates) > 0 {
				fmt.Println("impact estimates:")
				for _, est := range result.ImpactEstimates {
					fmt.Printf("  - %s: %s\n", est.MetricType, est.WeeklyImpact)
				}
			}

			fmt.Println("\nFull report saved to:", filepath.Join(mustExpandPath(cfg.QAgent.AnalysisDir), fmt.Sprintf("%s_analysis.json", result.ID)))

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Run analysis even if Q Agent is disabled")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output results as JSON")

	return cmd
}

// Helper functions

func createMemvidClient(cfg *config.Config) *memvid.Client {
	if cfg.Memvid.Endpoint == "" {
		return nil
	}

	return memvid.NewClient(memvid.ClientConfig{
		Endpoint: cfg.Memvid.Endpoint,
		Zone:     "sessions",
		Timeout:  30 * time.Second,
	})
}

func mustExpandPath(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fall back to the raw path; the caller will get a file-not-found
		// which is more actionable than a panic.
		return path
	}
	if path == "~" {
		return homeDir
	}
	return filepath.Join(homeDir, path[1:])
}
