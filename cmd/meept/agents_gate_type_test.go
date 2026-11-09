package main

// Tests for the --gate-type flag (e25f2512 follow-up): set-gate and
// goal-create both accept command|approval (default command), validate
// the value client-side, and refuse approval+command conflicts without
// dialing the daemon.

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// flagLookup fetches a flag's current string value by name.
func flagLookup(t *testing.T, cmd *cobra.Command, name string) string {
	t.Helper()
	f := cmd.Flags().Lookup(name)
	if f == nil {
		t.Fatalf("flag %q not found on %s", name, cmd.Name())
	}
	return f.Value.String()
}

func TestAgentsSetGateGateTypeFlag(t *testing.T) {
	agents := newAgentsCmd()
	cmd, _, err := agents.Find([]string{"set-gate"})
	if err != nil || cmd == nil || cmd.Name() != "set-gate" {
		t.Fatalf("set-gate lookup failed: %v", err)
	}
	if got := flagLookup(t, cmd, "gate-type"); got != gateTypeCommand {
		t.Errorf("set-gate --gate-type default = %q, want %q", got, gateTypeCommand)
	}
	// The flag accepts the approval value.
	if err := cmd.Flags().Set("gate-type", gateTypeApproval); err != nil {
		t.Fatalf("set --gate-type=approval: %v", err)
	}
	if got := flagLookup(t, cmd, "gate-type"); got != gateTypeApproval {
		t.Errorf("set-gate --gate-type = %q, want %q", got, gateTypeApproval)
	}
}

func TestAgentsGoalCreateGateTypeFlag(t *testing.T) {
	agents := newAgentsCmd()
	cmd, _, err := agents.Find([]string{"goal-create"})
	if err != nil || cmd == nil || cmd.Name() != "goal-create" {
		t.Fatalf("goal-create lookup failed: %v", err)
	}
	if got := flagLookup(t, cmd, "gate-type"); got != gateTypeCommand {
		t.Errorf("goal-create --gate-type default = %q, want %q", got, gateTypeCommand)
	}
	if err := cmd.Flags().Set("gate-type", gateTypeApproval); err != nil {
		t.Fatalf("set --gate-type=approval: %v", err)
	}
	if got := flagLookup(t, cmd, "gate-type"); got != gateTypeApproval {
		t.Errorf("goal-create --gate-type = %q, want %q", got, gateTypeApproval)
	}
}

// setPersistentFlag sets a persistent flag living on the CLI root (e.g.
// --transport). Root flags only exist after newRootCmd-style
// construction in main(), which tests do not run — so this seeds the
// package-level flag VARIABLE the flag binding writes into (the same
// seam getTransportConfig reads), leaving the absent flag untouched.
func setPersistentFlag(t *testing.T, cmd *cobra.Command, name, value string) {
	t.Helper()
	root := cmd.Root()
	var f *pflag.Flag
	if root != nil {
		f = root.PersistentFlags().Lookup(name)
		if f == nil {
			f = root.Flags().Lookup(name)
		}
	}
	if f != nil {
		if err := f.Value.Set(value); err != nil {
			t.Fatalf("set %s=%s: %v", name, value, err)
		}
		return
	}
	// Flag not registered (no root built in tests): fall back to the
	// package variable the persistent flag binds to.
	switch name {
	case "transport":
		transportFlag = value
	default:
		t.Fatalf("no handling for unregistered persistent flag %q", name)
	}
}

// Validation must run client-side and fail before any daemon connection.
// Cobra's Execute() on a subcommand delegates to the root (which would
// re-parse os.Args — test flags), so these tests set flags and invoke
// RunE directly: the process has no daemon to connect to, so reaching
// the connect path would surface as a connection error, not the
// expected validation message.
func TestAgentsSetGateGateTypeValidation(t *testing.T) {
	agents := newAgentsCmd()
	cmd, _, err := agents.Find([]string{"set-gate"})
	if err != nil {
		t.Fatalf("set-gate lookup failed: %v", err)
	}
	if err := cmd.Flags().Set("gate-type", "bogus"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if err := cmd.Flags().Set("command", "true"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	setPersistentFlag(t, cmd, "transport", "rpc")
	err = cmd.RunE(cmd, []string{"goal_x"})
	if err == nil {
		t.Fatal("set-gate with --gate-type=bogus must fail")
	}
	if want := `--gate-type must be "command" or "approval"`; err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func TestAgentsGoalCreateGateTypeValidation(t *testing.T) {
	agents := newAgentsCmd()
	cmd, _, err := agents.Find([]string{"goal-create"})
	if err != nil {
		t.Fatalf("goal-create lookup failed: %v", err)
	}
	if err := cmd.Flags().Set("gate-type", "bogus"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	for _, f := range [][2]string{{"title", "t"}, {"mandate", "m"}} {
		if err := cmd.Flags().Set(f[0], f[1]); err != nil {
			t.Fatalf("set flag %s: %v", f[0], err)
		}
	}
	err = cmd.RunE(cmd, []string{"emp_x"})
	if err == nil {
		t.Fatal("goal-create with --gate-type=bogus must fail")
	}
	if want := `--gate-type must be "command" or "approval"`; err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

// Approval + a command is a contradiction (approval gates never shell
// out): refused client-side before the dial.
func TestAgentsGoalCreateApprovalCommandConflict(t *testing.T) {
	agents := newAgentsCmd()
	cmd, _, err := agents.Find([]string{"goal-create"})
	if err != nil {
		t.Fatalf("goal-create lookup failed: %v", err)
	}
	if err := cmd.Flags().Set("gate-type", gateTypeApproval); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if err := cmd.Flags().Set("gate-command", "go test ./..."); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	for _, f := range [][2]string{{"title", "t"}, {"mandate", "m"}} {
		if err := cmd.Flags().Set(f[0], f[1]); err != nil {
			t.Fatalf("set flag %s: %v", f[0], err)
		}
	}
	err = cmd.RunE(cmd, []string{"emp_x"})
	if err == nil {
		t.Fatal("goal-create approval+gate-command must fail")
	}
	if want := "--gate-command conflicts with --gate-type=approval (approval gates never run a command)"; err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func TestAgentsSetGateApprovalCommandConflict(t *testing.T) {
	agents := newAgentsCmd()
	cmd, _, err := agents.Find([]string{"set-gate"})
	if err != nil {
		t.Fatalf("set-gate lookup failed: %v", err)
	}
	if err := cmd.Flags().Set("gate-type", gateTypeApproval); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if err := cmd.Flags().Set("command", "true"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	setPersistentFlag(t, cmd, "transport", "rpc")
	err = cmd.RunE(cmd, []string{"goal_x"})
	if err == nil {
		t.Fatal("set-gate approval+command must fail")
	}
	if want := "--command conflicts with --gate-type=approval (approval gates never run a command)"; err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}
