package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/project"
)

func newInitCmd() *cobra.Command {
	var (
		maxDepth     int
		minFiles     int
		maxFiles     int
		includeTests bool
		format       string
		rootDir      string
		outputJSON   bool
	)

	cmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Initialize AGENTS.md files for a project",
		Long: `Deep-initialize a project by generating hierarchical AGENTS.md files.

Scans the project tree, extracts code symbols using AST parsing, and generates
concise AGENTS.md files at the root, domain, and component levels. These files
help AI agents understand project structure and conventions.

If no path is given, the current directory is used.

Examples:
  meept init                           # Initialize current directory
  meept init ./my-project              # Initialize specific directory
  meept init --max-depth 4 --json      # Limit depth, output as JSON
  meept init --include-tests           # Include test files in symbol extraction`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootDir
			if len(args) > 0 {
				root = args[0]
			}
			if root == "" {
				wd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getwd: %w", err)
				}
				root = wd
			}

			root, err := filepath.Abs(root)
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}

			// Verify directory exists
			info, err := os.Stat(root)
			if err != nil {
				return fmt.Errorf("stat %s: %w", root, err)
			}
			if !info.IsDir() {
				return fmt.Errorf("%s is not a directory", root)
			}

			opts := project.DefaultDeepInitOptions()
			opts.RootDir = root
			opts.MaxDepth = maxDepth
			opts.MinFileCount = minFiles
			opts.MaxFileCount = maxFiles
			opts.IncludeTests = includeTests
			opts.OutputFormat = format

			di := project.NewDeepInitializer(opts, nil)
			result, err := di.Run(context.Background())
			if err != nil {
				return fmt.Errorf("init failed: %w", err)
			}

			if outputJSON {
				out, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					return fmt.Errorf("marshal JSON: %w", err)
				}
				fmt.Println(string(out))
				return nil
			}

			// Human-readable output
			fmt.Printf("initialized %s\n", root)
			fmt.Printf("generated %d AGENTS.md file(s) in %s\n",
				len(result.AgentsFiles), result.Duration)
			if len(result.Errors) > 0 {
				fmt.Printf("warnings: %d\n", len(result.Errors))
				for _, e := range result.Errors {
					fmt.Fprintf(os.Stderr, "  - %s\n", e)
				}
			}
			fmt.Println()
			for _, af := range result.AgentsFiles {
				rel, _ := filepath.Rel(root, af.Path)
				fmt.Printf("  %-40s  %s  (%d files, %d symbols)\n",
					rel, strings.ToLower(af.Level), af.FileCount, af.SymbolCount)
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&maxDepth, "max-depth", 6, "Maximum directory depth to scan")
	cmd.Flags().IntVar(&minFiles, "min-files", 3, "Minimum source files for a directory to get AGENTS.md")
	cmd.Flags().IntVar(&maxFiles, "max-files", 50, "Maximum files scanned per directory")
	cmd.Flags().BoolVar(&includeTests, "include-tests", false, "Include test files in symbol extraction")
	cmd.Flags().StringVarP(&format, "format", "f", "concise", "Output format: concise or detailed")
	cmd.Flags().StringVarP(&rootDir, "root", "r", "", "Project root (default: current directory)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output results as JSON")

	return cmd
}
