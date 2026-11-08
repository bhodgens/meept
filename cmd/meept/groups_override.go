// groups_override.go — `meept groups override add|list|prune` (agent-groups
// plan tree leaf 07, contract C8): operator management of the time-boxed
// allow overrides file ($MEEPT_HOME/group_overrides.json5).
//
// Merge point (executed): this file is the `override` SUBTREE attached by
// cmd/meept/groups.go's newGroupsCmd (leaf 06-diagnosis owns the `groups`
// root: list/explain/check). This file deliberately defines NO root —
// only the subtree constructors the parent attaches:
//
//	cmd.AddCommand(newGroupsOverrideCmd()) // in groups.go newGroupsCmd
//
// All file access is direct (users_cmd.go precedent: the CLI runs on the
// same host as the file), writes are atomic (groups.SaveOverrides
// tmp+rename), and the expiry clock is the wall clock for operator
// commands (an operator listing/pruning wants the real now; tests inject
// through the internal/groups reader API, not the CLI).
package main

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/groups"
	"github.com/spf13/cobra"
)

// groupsOverridesFilePath resolves the overrides file for CLI commands:
// an explicit --file flag wins, else $MEEPT_HOME/group_overrides.json5
// (config.MeeptPath is THE resolution point).
func groupsOverridesFilePath(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	return config.MeeptPath(groups.OverridesFileName)
}

// loadOverridesForCLI loads the file at path with the wall clock, mapping
// a missing file to an empty set (first use out of the box). Malformed
// files/entries return the named reader error — never a silent skip.
func loadOverridesForCLI(path string) (*groups.OverrideSet, error) {
	return groups.LoadOverrides(path, groups.OverrideLoadOptions{Now: time.Now().UTC})
}

// newGroupsOverrideCmd builds the `override` subtree, attached under the
// leaf-06 `groups` root (groups.go newGroupsCmd).
func newGroupsOverrideCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "override",
		Short: "manage time-boxed tool allow overrides",
		Long: `Time-boxed tool allow overrides ($MEEPT_HOME/group_overrides.json5).

Overrides ADD allows only — never deny, never widen ceilings. Entries:
  {subject: "agent:x", allow: ["git_push"], reason: "deploy window",
   expires: "2026-11-05T17:00:00Z"}

Expired entries are skipped at daemon load with one warning; malformed
entries fail daemon boot (fix with this CLI, or prune).`,
	}
	cmd.AddCommand(newGroupsOverrideAddCmd())
	cmd.AddCommand(newGroupsOverrideListCmd())
	cmd.AddCommand(newGroupsOverridePruneCmd())
	return cmd
}

// newGroupsOverrideAddCmd implements `groups override add`.
func newGroupsOverrideAddCmd() *cobra.Command {
	var (
		fileFlag  string
		allowFlag string
		reasonFl  string
		expiresFl string
	)
	cmd := &cobra.Command{
		Use:   "add <subject>",
		Short: "add a time-boxed allow override",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			subject := args[0]
			entry := groups.Override{
				Subject: subject,
				Reason:  reasonFl,
			}
			entry.Allow = append(entry.Allow, splitCommaList(allowFlag)...)
			if expiresFl == "" {
				return fmt.Errorf("--expires is required (RFC3339, e.g. 2026-11-05T17:00:00Z); overrides are time-boxed only")
			}
			exp, err := time.Parse(time.RFC3339, expiresFl)
			if err != nil {
				return fmt.Errorf("--expires must be RFC3339 (e.g. 2026-11-05T17:00:00Z): %w", err)
			}
			entry.Expires = exp

			if err := entry.Validate(); err != nil {
				return err
			}

			path := groupsOverridesFilePath(fileFlag)
			set, err := loadOverridesForCLI(path)
			if err != nil {
				return err
			}
			// Replace any existing entry with the same subject+reason so
			// re-adding a window updates it instead of duplicating.
			entries := make([]groups.Override, 0, len(set.Active)+len(set.Expired)+1)
			for _, o := range set.Active {
				if o.Subject == entry.Subject && o.Reason == entry.Reason {
					continue
				}
				entries = append(entries, o)
			}
			for _, o := range set.Expired {
				if o.Subject == entry.Subject && o.Reason == entry.Reason {
					continue
				}
				entries = append(entries, o)
			}
			entries = append(entries, entry)
			if err := groups.SaveOverrides(path, entries); err != nil {
				return err
			}
			fmt.Printf("override added: %s allow=%v expires=%s (%s)\n",
				subject, entry.Allow, exp.Format(time.RFC3339), path)
			return nil
		},
	}
	cmd.Flags().StringVar(&fileFlag, "file", "", "overrides file path (default $MEEPT_HOME/group_overrides.json5)")
	cmd.Flags().StringVar(&allowFlag, "allow", "", "comma-separated tool names to grant (required)")
	cmd.Flags().StringVar(&reasonFl, "reason", "", "human justification (required — the file is a reviewable artifact)")
	cmd.Flags().StringVar(&expiresFl, "expires", "", "expiry as RFC3339, e.g. 2026-11-05T17:00:00Z (required)")
	_ = cmd.MarkFlagRequired("allow")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

// newGroupsOverrideListCmd implements `groups override list`. Default
// shows ACTIVE entries only; --all includes expired (marked).
func newGroupsOverrideListCmd() *cobra.Command {
	var (
		fileFlag string
		allFlag  bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "list active overrides (--all includes expired)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := groupsOverridesFilePath(fileFlag)
			set, err := loadOverridesForCLI(path)
			if err != nil {
				return err
			}
			if set.ActiveCount() == 0 && set.ExpiredCount() == 0 {
				fmt.Printf("no overrides (%s)\n", path)
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "STATE\tSUBJECT\tALLOW\tEXPIRES\tREASON")
			for _, o := range set.Active {
				fmt.Fprintf(w, "active\t%s\t%s\t%s\t%s\n",
					o.Subject, commaJoin(o.Allow), o.Expires.Format(time.RFC3339), o.Reason)
			}
			if allFlag {
				for _, o := range set.Expired {
					fmt.Fprintf(w, "expired\t%s\t%s\t%s\t%s\n",
						o.Subject, commaJoin(o.Allow), o.Expires.Format(time.RFC3339), o.Reason)
				}
			}
			w.Flush()
			fmt.Printf("\n%d active, %d expired (%s)\n", set.ActiveCount(), set.ExpiredCount(), path)
			return nil
		},
	}
	cmd.Flags().StringVar(&fileFlag, "file", "", "overrides file path (default $MEEPT_HOME/group_overrides.json5)")
	cmd.Flags().BoolVar(&allFlag, "all", false, "include expired entries")
	return cmd
}

// newGroupsOverridePruneCmd implements `groups override prune`: remove
// every expired entry (atomic rewrite; a no-op rewrite never touches the
// file).
func newGroupsOverridePruneCmd() *cobra.Command {
	var fileFlag string
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "remove expired overrides from the file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := groupsOverridesFilePath(fileFlag)
			removed, err := groups.PruneOverrides(path, time.Now().UTC)
			if err != nil {
				return err
			}
			if removed == 0 {
				fmt.Printf("nothing to prune (%s)\n", path)
				return nil
			}
			fmt.Printf("pruned %d expired override(s) (%s)\n", removed, path)
			return nil
		},
	}
	cmd.Flags().StringVar(&fileFlag, "file", "", "overrides file path (default $MEEPT_HOME/group_overrides.json5)")
	return cmd
}

// splitCommaList splits a comma-separated flag value, trimming spaces and
// dropping empty items.
func splitCommaList(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			item := trimSpaces(s[start:i])
			if item != "" {
				out = append(out, item)
			}
			start = i + 1
		}
	}
	return out
}

// commaJoin renders a list for the list table (empty => "-").
func commaJoin(in []string) string {
	if len(in) == 0 {
		return "-"
	}
	out := ""
	for i, v := range in {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}

// trimSpaces is strings.TrimSpace without the import (single use pair).
func trimSpaces(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
