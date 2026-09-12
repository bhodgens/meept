package main

// meept soul — inspect the user-authored persona file (SOUL.md).
//
// The daemon owns hot reload; these subcommands are read-only views of the
// same file so users can verify what the daemon accepted (show) and where
// the file lives under the active MEEPT_HOME (path).

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
)

func newSoulCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "soul",
		Short: "Inspect the SOUL.md persona file",
		Long: `Inspect the user-authored persona file (~/.meept/SOUL.md).

Examples:
  meept soul show    # Print the file plus its sha256 and size
  meept soul path    # Print the resolved path (honors MEEPT_HOME)`,
	}
	cmd.AddCommand(soulShowCmd(), soulPathCmd())
	return cmd
}

func soulShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the current SOUL.md content and its hash",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := agent.SoulPath()
			content, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			sum := sha256.Sum256(content)
			if verr := agent.ValidateSoul(content); verr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: file is invalid and would be rejected by the daemon: %v\n", verr)
			}
			fmt.Printf("path: %s\nsha256: %s\nbytes: %d\n\n%s\n",
				path, hex.EncodeToString(sum[:]), len(content), string(content))
			return nil
		},
	}
}

func soulPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the resolved SOUL.md path",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println(config.MeeptPath(agent.SoulFileName))
			return nil
		},
	}
}
