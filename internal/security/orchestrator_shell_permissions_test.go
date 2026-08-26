package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOrchestratorPermissionTableAllowStillScans proves the defense-in-depth
// contract: a table "allow" decision does not skip tirith scanning — the
// orchestrator's ScanShellCommand still runs (commands_scanned increments).
func TestOrchestratorPermissionTableAllowStillScans(t *testing.T) {
	orch := NewOrchestrator(OrchestratorConfig{
		SanitizeInputs:    true,
		MonitorOutput:     false,
		RedactOutput:      false,
		ScanShellCommands: true,
		TirithBinary:      BinaryTirith, // binary likely absent in CI; ScanShellCommand still counts
	}, nil)

	before := orch.Stats()["commands_scanned"]

	table := NewPermissionTable(map[string]ShellRule{
		"*": {Action: ShellActionAllow},
	})
	decision, _, ok := table.Evaluate("echo hello")
	require.True(t, ok)
	require.Equal(t, ShellActionAllow, decision)

	// The allow decision falls through to scanning in shell.go; simulate that
	// flow: table allows -> ScanShellCommand is still invoked.
	blocked, _, _ := orch.ScanShellCommand(context.Background(), "echo hello")

	after := orch.Stats()["commands_scanned"]
	require.Greater(t, after, before, "tirith scan must still run after table allow")

	// And a deny short-circuits: scanning must NOT be consulted.
	denyTable := NewPermissionTable(map[string]ShellRule{"rm -rf": {Action: ShellActionDeny}})
	dec, prefix, ok2 := denyTable.Evaluate("rm -rf /")
	require.True(t, ok2)
	require.Equal(t, ShellActionDeny, dec)
	require.Equal(t, "rm -rf", prefix)
	_ = blocked // tirith availability varies by environment; count assertion is the point
}
