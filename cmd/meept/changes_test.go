package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestChangesCommandStructure verifies the CLI command hierarchy exists and
// exposes list/revert subcommands with the expected flags.
func TestChangesCommandStructure(t *testing.T) {
	cmd := newChangesCmd()

	if cmd == nil {
		t.Fatal("expected non-nil changes command")
	}
	if cmd.Use != "changes" {
		t.Errorf("changes command use: got %q, want %q", cmd.Use, "changes")
	}
	if cmd.Short == "" {
		t.Error("changes command should have short description")
	}

	subs := map[string]bool{}
	for _, sub := range cmd.Commands() {
		subs[sub.Name()] = true
	}
	if !subs["list"] {
		t.Error("changes command should have a 'list' subcommand")
	}
	if !subs["revert"] {
		t.Error("changes command should have a 'revert' subcommand")
	}

	listCmd := findSubcommand(t, cmd, "list")
	for _, flag := range []string{"session", "limit", "json"} {
		if listCmd.Flags().Lookup(flag) == nil {
			t.Errorf("changes list missing --%s flag", flag)
		}
	}

	revertCmd := findSubcommand(t, cmd, "revert")
	if revertCmd.Flags().Lookup("json") == nil {
		t.Error("changes revert missing --json flag")
	}
}

func TestChangesCommandRegisteredOnRoot(t *testing.T) {
	cmd := newChangesCmd()
	root := &cobra.Command{Use: "meept"}
	root.AddCommand(cmd)
	found := false
	for _, sub := range root.Commands() {
		if sub.Name() == "changes" {
			found = true
		}
	}
	if !found {
		t.Fatal("changes command not attached to root command tree")
	}
}

// TestRunChangesListEmptyDB runs list against an empty journal DB in a temp
// state dir; it must not panic and must report no entries.
func TestRunChangesListEmptyDB(t *testing.T) {
	oldStateDir := stateDir
	stateDir = t.TempDir()
	defer func() { stateDir = oldStateDir }()

	// Override stdout to capture the command output.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w

	runErr := runChangesList("", 10, false)

	w.Close()
	os.Stdout = origStdout
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}

	if runErr != nil {
		t.Fatalf("runChangesList on empty db: %v", runErr)
	}
	if !strings.Contains(buf.String(), "no applied changes journaled") {
		t.Errorf("empty-db output = %q, want 'no applied changes journaled'", buf.String())
	}
}

func findSubcommand(t *testing.T, parent *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, sub := range parent.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	t.Fatalf("subcommand %q not found", name)
	return nil
}
