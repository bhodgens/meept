package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/agent"
	"github.com/spf13/cobra"
)

// newHaloCmd returns the HALO analysis command group.
func newHaloCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "halo",
		Short: "HALO-style trace analysis tools",
		Long: `HALO (Heuristic Analysis & Learning Operations) commands for trace analysis.

Available Commands:
  compact    Analyze and compact agent turns from a session
  report     Generate analysis report from failure modes`,
	}

	cmd.AddCommand(newHaloCompactCmd())
	cmd.AddCommand(newHaloReportCmd())

	return cmd
}

// newHaloCompactCmd analyzes and compacts agent turns.
func newHaloCompactCmd() *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "compact [session-file]",
		Short: "Analyze and compact agent turns for efficient context",
		Long: `Compact agent turns by merging tool_call + tool_response pairs
and collapsing redundant thinking turns.

Reads turns from a JSON session file and outputs compacted representation.
Reduces context size by ~40% for typical RLM analysis runs.

Examples:
  meept halo compact session.json
  meept halo compact session.json --format=json
  meept halo compact session.json --format=summary`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inputFile := args[0]

			// Read session file
			data, err := os.ReadFile(inputFile)
			if err != nil {
				return fmt.Errorf("read session file: %w", err)
			}

			// Parse turns from session
			turns, err := parseTurnsFromJSON(data)
			if err != nil {
				return fmt.Errorf("parse turns: %w", err)
			}

			// Compact turns
			tc := agent.NewTurnCompactor()
			compacted := tc.CompactTurns(turns)

			// Output results
			switch outputFormat {
			case "summary":
				originalTokens := sumTurnTokens(turns)
				compactedTokens := sumCompactTurnTokens(compacted)
				reduction := float64(originalTokens-compactedTokens) / float64(originalTokens) * 100
				fmt.Printf("Turn compaction summary:\n")
				fmt.Printf("  Original turns:    %d (%d tokens)\n", len(turns), originalTokens)
				fmt.Printf("  Compacted turns:   %d (%d tokens)\n", len(compacted), compactedTokens)
				fmt.Printf("  Reduction:         %.1f%%\n", reduction)

				// Count by type
				typeCounts := make(map[string]int)
				for _, ct := range compacted {
					typeCounts[ct.Type]++
				}
				fmt.Printf("  By type:           ")
				parts := []string{}
				for t, c := range typeCounts {
					parts = append(parts, fmt.Sprintf("%s=%d", t, c))
				}
				fmt.Printf("%s\n", strings.Join(parts, ", "))

			case "json":
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(compacted); err != nil {
					return fmt.Errorf("encode JSON: %w", err)
				}

			default:
				// Human-readable format
				for i, ct := range compacted {
					fmt.Printf("--- Turn %d (%s, %d tokens) ---\n", i, ct.Type, ct.TokenCount)
					switch ct.Type {
					case "tool":
						fmt.Printf("Tool: %s\n", ct.ToolName)
						if len(ct.ToolInput) > 0 {
							inputJSON, _ := json.MarshalIndent(ct.ToolInput, "  ", "  ")
							fmt.Printf("Input:  %s\n", string(inputJSON))
						}
						if ct.ToolOutput != "" {
							output := ct.ToolOutput
							if len(output) > 200 {
								output = output[:200] + "..."
							}
							fmt.Printf("Output: %s\n", output)
						}
					case "thinking":
						thinking := ct.Thinking
						if len(thinking) > 300 {
							thinking = thinking[:300] + "..."
						}
						fmt.Printf("Thinking: %s\n", thinking)
					case "final":
						content := ct.Content
						if len(content) > 300 {
							content = content[:300] + "..."
						}
						fmt.Printf("Content: %s\n", content)
					}
					fmt.Println()
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "format", "f", "text", "Output format: text, json, summary")

	return cmd
}

// newHaloReportCmd generates analysis reports.
func newHaloReportCmd() *cobra.Command {
	var outputDir string
	var runID string

	cmd := &cobra.Command{
		Use:   "report [failure-modes.json]",
		Short: "Generate analysis report from failure modes",
		Long: `Generate a markdown report from HALO analysis failure modes.

Examples:
  meept halo report failure-modes.json
  meept halo report failure-modes.json --output=/tmp/reports`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inputFile := args[0]

			// Read failure modes
			data, err := os.ReadFile(inputFile)
			if err != nil {
				return fmt.Errorf("read input file: %w", err)
			}

			var failureModes []agent.FailureMode
			if err := json.Unmarshal(data, &failureModes); err != nil {
				return fmt.Errorf("parse failure modes: %w", err)
			}

			// Generate report
			report := generateReport(failureModes)

			// Output
			if outputDir != "" {
				if runID == "" {
					runID = "analysis"
				}
				outputPath := filepath.Join(outputDir, runID, "report.md")
				if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
					return fmt.Errorf("create output dir: %w", err)
				}
				if err := os.WriteFile(outputPath, []byte(report), 0o644); err != nil {
					return fmt.Errorf("write report: %w", err)
				}
				fmt.Printf("Report written to: %s\n", outputPath)
			} else {
				fmt.Println(report)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputDir, "output", "o", "", "Output directory (default: stdout)")
	cmd.Flags().StringVarP(&runID, "run-id", "r", "", "Run ID for output path")

	return cmd
}

// parseTurnsFromJSON parses agent turns from a JSON session file.
func parseTurnsFromJSON(data []byte) ([]agent.Turn, error) {
	// Try parsing as array of turns
	var turns []agent.Turn
	if err := json.Unmarshal(data, &turns); err == nil {
		return turns, nil
	}

	// Try parsing as session object with turns field
	var session struct {
		Turns []agent.Turn `json:"turns"`
	}
	if err := json.Unmarshal(data, &session); err == nil && len(session.Turns) > 0 {
		return session.Turns, nil
	}

	return nil, fmt.Errorf("parse turns: could not parse JSON - expected array of turns or object with 'turns' field")
}

func sumTurnTokens(turns []agent.Turn) int {
	total := 0
	for _, t := range turns {
		total += t.TokenCount
	}
	return total
}

func sumCompactTurnTokens(turns []agent.CompactTurn) int {
	total := 0
	for _, t := range turns {
		total += t.TokenCount
	}
	return total
}

func generateReport(modes []agent.FailureMode) string {
	var sb strings.Builder
	sb.WriteString("# HALO Trace Analysis Report\n\n")
	sb.WriteString(fmt.Sprintf("## Summary\n\n"))
	sb.WriteString(fmt.Sprintf("Failure modes found: **%d**\n\n", len(modes)))

	if len(modes) == 0 {
		sb.WriteString("No failure modes detected.\n")
		return sb.String()
	}

	// Group by severity
	severityGroups := map[string][]agent.FailureMode{
		"critical": {},
		"high":     {},
		"medium":   {},
		"low":      {},
	}
	for _, fm := range modes {
		severityGroups[fm.Severity] = append(severityGroups[fm.Severity], fm)
	}

	// Output by severity
	for _, severity := range []string{"critical", "high", "medium", "low"} {
		group := severityGroups[severity]
		if len(group) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("## %s Severity Issues (%d)\n\n", strings.ToUpper(severity), len(group)))
		for i, fm := range group {
			sb.WriteString(fmt.Sprintf("### %d. %s\n\n", i+1, fm.ID))
			sb.WriteString(fmt.Sprintf("**Category:** %s\n\n", fm.Category))
			sb.WriteString(fmt.Sprintf("**Description:** %s\n\n", fm.Description))
			if len(fm.TraceIDs) > 0 {
				sb.WriteString(fmt.Sprintf("**Affected traces:** %s\n\n", strings.Join(fm.TraceIDs, ", ")))
			}
		}
	}

	sb.WriteString("\n---\n*Report generated by meept halo*\n")
	return sb.String()
}
