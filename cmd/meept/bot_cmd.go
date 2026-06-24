// Package main — bot_cmd.go contains the legacy `meept bots` command stub.
//
// Per AI Employee Design spec line 490, the `meept bots` command has been
// removed in the hard cutover to `meept agents`. The stub remains so existing
// scripts get a clear error message pointing to the replacement instead of a
// confusing "unknown command" from cobra.
//
// See docs/workflows/employees.md for the new `meept agents` commands.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// newBotsCmd returns a stub cobra command that always exits with an error
// explaining that `meept bots` was removed and pointing callers at the
// replacement `meept agents` command. This matches the spec directive at
// line 490.
func newBotsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bots",
		Short: "removed (use agents)",
		Long:  "meept bots was removed in favor of meept agents. Run `meept agents --help` for the new commands.",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.ErrOrStderr(),
				"meept bots was removed; see `meept agents --help` and docs/workflows/employees.md")
			os.Exit(1)
		},
	}
}
