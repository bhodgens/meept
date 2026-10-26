package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/config"
)

// githubToolNames is the C2 tool-name set the [github] block registers
// (same set the gate test pins: internal/tools/builtin/
// github_register_test.go). Statically reported: there is no daemon
// tool-listing RPC surface, so the CLI reports what WOULD be registered
// when the block is enabled, not what the daemon actually registered.
var githubToolNames = []string{
	"github_pr_create",
	"github_pr_status",
	"github_ci_wait",
	"github_pr_comment",
	"github_pr_review",
	"github_pr_merge",
	"github_issue_get",
	"github_issue_comment",
}

// newGithubCmd builds the `meept github` command tree.
func newGithubCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "github",
		Short: "GitHub PR/CI tool set diagnostics (report-only)",
		Long:  "Inspect the [github] config block the daemon uses to register the github_* agent tools.\nAll subcommands are report-only (doctor convention): nothing is mutated.",
	}
	cmd.AddCommand(newGithubStatusCmd())
	return cmd
}

// newGithubStatusCmd builds `meept github status`: enabled?, token
// resolvable? (presence/length only — the token itself is NEVER printed),
// default owner/repo, base_url, and the github tool count.
func newGithubStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report the [github] config state and token source (report-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGithubStatus()
		},
	}
}

// runGithubStatus prints the report. Always exit 0: a disabled or
// missing-token state is a normal (default) state, not a failure — the
// same report-only posture as meept doctor.
func runGithubStatus() error {
	cfg, err := config.LoadDefault()
	if err != nil {
		// A config load failure still yields a useful report: the block
		// is effectively disabled.
		fmt.Printf("meept github status\n-------------------\n")
		fmt.Printf("  config:      %s\n", err)
		fmt.Printf("  enabled:     false (config unavailable; tools will not register)\n")
		return nil
	}

	g := cfg.GitHub
	fmt.Printf("meept github status\n-------------------\n")
	fmt.Printf("  enabled:     %v\n", g.Enabled)

	// Token source: mirror the daemon's precedence, presence/length only.
	tokenDetail := describeGitHubTokenSource(g.TokenSecret)
	fmt.Printf("  token:       %s\n", tokenDetail)

	// Default target.
	if g.DefaultOwner != "" || g.DefaultRepo != "" {
		fmt.Printf("  default:     %s/%s\n", valueOrDash(g.DefaultOwner), valueOrDash(g.DefaultRepo))
	} else {
		fmt.Printf("  default:     (unset — tools require an explicit owner/repo argument)\n")
	}

	// Base URL (GitHub Enterprise).
	if g.BaseURL != "" {
		fmt.Printf("  base_url:    %s\n", g.BaseURL)
	}

	// Tool count: static report (no daemon tool-listing RPC exists; the
	// count reflects what the daemon WOULD register when enabled).
	if g.Enabled && g.TokenSecret != "" {
		fmt.Printf("  tools:       %d github_* tools will register when the daemon boots with this config\n", len(githubToolNames))
		for _, n := range githubToolNames {
			fmt.Printf("    - %s\n", n)
		}
	} else if g.Enabled {
		fmt.Printf("  tools:       0 — github enabled but token_secret is empty; the daemon logs and registers nothing\n")
	} else {
		fmt.Printf("  tools:       0 — github disabled (default); no github_* tools register\n")
	}
	return nil
}

// describeGitHubTokenSource mirrors the daemon's token-source precedence
// (internal/daemon/github_wiring.go) WITHOUT ever reading or printing the
// token value. It reports only presence, length, and the source kind.
func describeGitHubTokenSource(tokenSecret string) string {
	if tokenSecret == "" {
		return "not configured"
	}
	// 1. Token file: an existing regular path (after ~ / MEEPT_HOME
	//    expansion).
	path := config.ExpandMeeptPath(tokenSecret)
	if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() {
		if fi.Mode().Perm()&0o077 != 0 {
			return fmt.Sprintf("file %s present but has group/other access bits — the daemon refuses it (chmod it to 0600, or use a [secrets] broker name)", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Sprintf("file %s unreadable: %v", path, err)
		}
		token := strings.TrimSpace(string(data))
		if token == "" {
			return fmt.Sprintf("file %s is empty — the daemon fails closed (ErrNoToken)", path)
		}
		return fmt.Sprintf("file %s present, token length %d (value not shown)", path, len(token))
	}
	// 2. Named secret: resolved at daemon startup against the [secrets]
	//    broker; the CLI has no live broker, so report the configured name
	//    only.
	return fmt.Sprintf("named secret %q — resolved via the [secrets] broker at daemon startup (no value check here)", tokenSecret)
}

// valueOrDash renders "" as "-" for the default-target line.
func valueOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
