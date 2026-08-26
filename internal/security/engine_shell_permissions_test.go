package security

import (
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/stretchr/testify/require"
)

func newPermissionTableTestEngine(t *testing.T) *Engine {
	t.Helper()
	engine, err := NewEngine(filepath.Join(t.TempDir(), "security.db"), &config.SecurityConfig{
		RequireConfirmationHigh:     true,
		RequireConfirmationCritical: true,
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { engine.Close() })
	return engine
}

func TestEnginePermissionTableDeny(t *testing.T) {
	engine := newPermissionTableTestEngine(t)
	engine.SetPermissionTable(NewPermissionTable(map[string]ShellRule{
		"rm -rf": {Action: ShellActionDeny},
	}))

	d := engine.Check(ActionShellExecute, "shell_execute", map[string]string{"command": "rm -rf /"}, "")
	require.False(t, d.Allowed)
	require.False(t, d.RequiresConfirmation)
	require.Equal(t, "shell_permission_table", d.RuleSource)
}

func TestEnginePermissionTableAsk(t *testing.T) {
	engine := newPermissionTableTestEngine(t)
	engine.SetPermissionTable(NewPermissionTable(map[string]ShellRule{
		"git push": {Action: ShellActionAsk},
	}))

	d := engine.Check(ActionShellExecute, "shell_execute", map[string]string{"command": "git push origin main"}, "")
	require.False(t, d.Allowed)
	require.True(t, d.RequiresConfirmation, "ask must route to confirmation flow")
	require.Equal(t, "shell_permission_table", d.RuleSource)
}

func TestEnginePermissionTableAllowStillScanned(t *testing.T) {
	engine := newPermissionTableTestEngine(t)
	engine.SetPermissionTable(NewPermissionTable(map[string]ShellRule{
		"*": {Action: ShellActionAllow},
	}))

	// Allow falls through to the normal path — no short-circuit. The
	// decision comes from the base rule / command pattern machinery, not the
	// table (RuleSource is not shell_permission_table), proving downstream
	// evaluation (tirith in the orchestrator; command patterns here) still runs.
	d := engine.Check(ActionShellExecute, "shell_execute", map[string]string{"command": "mytool deploy"}, "")
	require.True(t, d.Allowed)
	require.NotEqual(t, "shell_permission_table", d.RuleSource)
}

func TestEnginePermissionTableNoMatchUnchanged(t *testing.T) {
	engine := newPermissionTableTestEngine(t)
	engine.SetPermissionTable(NewPermissionTable(map[string]ShellRule{
		"rm -rf": {Action: ShellActionDeny},
	}))

	// Unmatched command behaves exactly as before the table existed.
	d := engine.Check(ActionShellExecute, "shell_execute", map[string]string{"command": "ls -la"}, "")
	require.True(t, d.Allowed)
}

func TestEnginePermissionTableNilIsNoop(t *testing.T) {
	engine := newPermissionTableTestEngine(t)
	engine.SetPermissionTable(nil)

	d := engine.Check(ActionShellExecute, "shell_execute", map[string]string{"command": "sudo rm -rf /tmp/x"}, "")
	// Without a table, existing path decides (not an immediate table deny).
	require.NotEqual(t, "shell_permission_table", d.RuleSource)
}
