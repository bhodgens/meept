package security

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/stretchr/testify/require"
)

// Tests for SecurityConfig.BlockedPaths wiring in the engine's file-path
// stage (docs/plans/20261101-security-gate-followups/01-blocked-paths.md).
// Matching mirrors the AllowedPaths/DB allow-rule logic inverted: glob
// (filepath.Match), directory containment (isPathUnderDir), and ~ expansion
// (pathutil.ExpandPath) - with deny winning over allow.

func newBlockedPathsEngine(t *testing.T, blocked []string, allowed []string) *Engine {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")
	cfg := &config.SecurityConfig{
		BlockedPaths: blocked,
		AllowedPaths: allowed,
	}
	engine, err := NewEngine(dbPath, cfg, nil)
	require.NoError(t, err, "NewEngine failed")
	t.Cleanup(func() { _ = engine.Close() })
	return engine
}

// T2b: recursive '**'-suffix patterns must match nested paths. The e2e
// harness seeds allowed_paths as "<work>/**"; filepath.Match does not let
// '*' cross '/', so nested work files were denied once the gate ran engine
// checks on the live path (dispatcher-05 failure, 2026-11-01).
func TestEnginePathPatternRecursiveGlobSuffix(t *testing.T) {
	engine := newBlockedPathsEngine(t, []string{"/opt/locked/**"}, nil)

	decision := engine.Check(ActionFileWrite, "file_write", map[string]string{"path": "/opt/locked/nested/secret.txt"}, "")
	require.False(t, decision.Allowed, "nested path under a '**' blocked entry must be denied, got: %+v", decision)
	require.Contains(t, decision.Reason, "/opt/locked/**")

	// Sibling pattern shapes keep working: direct child and the dir itself.
	d := engine.Check(ActionFileWrite, "file_write", map[string]string{"path": "/opt/locked/top.txt"}, "")
	require.False(t, d.Allowed, "direct child under '**' must be denied")
}

// T1: a file-category tool call whose target path matches a BlockedPaths
// entry is denied, with the rule named in the Reason.
func TestBlockedPathsDeniesFileWrite(t *testing.T) {
	engine := newBlockedPathsEngine(t, []string{"~/.ssh/*"}, nil)

	decision := engine.Check(ActionFileWrite, "file_write", map[string]string{"path": "~/.ssh/id_rsa"}, "")
	require.False(t, decision.Allowed, "blocked path must be denied, got: %+v", decision)
	require.Contains(t, decision.Reason, "blocked_paths", "reason must name the blocked-path rule: %s", decision.Reason)
	require.Contains(t, decision.Reason, "~/.ssh/*", "reason must name the matching rule: %s", decision.Reason)
	require.Equal(t, RiskHigh, decision.RiskLevel, "blocked-path deny is high risk")
}

func TestBlockedPathsDeniesFileReadAndDelete(t *testing.T) {
	engine := newBlockedPathsEngine(t, []string{"~/private/*"}, nil)

	read := engine.Check(ActionFileRead, "file_read", map[string]string{"path": "~/private/notes.txt"}, "")
	require.False(t, read.Allowed, "blocked path must deny file_read, got: %+v", read)

	del := engine.Check(ActionFileDelete, "file_delete", map[string]string{"path": "~/private/notes.txt"}, "")
	require.False(t, del.Allowed, "blocked path must deny file_delete, got: %+v", del)
}

// A path outside every BlockedPaths entry must stay unaffected.
func TestBlockedPathsNonMatchUnaffected(t *testing.T) {
	engine := newBlockedPathsEngine(t, []string{"~/.ssh/*"}, nil)

	decision := engine.Check(ActionFileWrite, "file_write", map[string]string{"path": "~/projects/notes.txt"}, "")
	if decision.Allowed == false && strings.Contains(decision.Reason, "blocked path") {
		t.Fatalf("non-matching path was denied by blocked-path stage: %+v", decision)
	}
}

// T1 deny-wins pin: a path both allowed (AllowedPaths / allow rule) and
// matched by BlockedPaths must be DENIED.
func TestBlockedPathsDenyWinsOverAllow(t *testing.T) {
	engine := newBlockedPathsEngine(t, []string{"~/.ssh/*"}, []string{"~/*"})

	// Seed an allow rule covering the same area via the DB (the engine's
	// existing allow stage reads path_rules, rule_type='allow'). The seeded
	// defaults already carry '~/*' allow, so INSERT OR IGNORE keeps this a
	// no-op when the seed already landed it.
	var err error
	_, err = engine.db.Exec(`
		INSERT OR IGNORE INTO path_rules (pattern, rule_type, risk_level, description, immutable, enabled)
		VALUES ('~/*', 'allow', 0, 'Allow home', 0, 1)`)
	require.NoError(t, err)

	decision := engine.Check(ActionFileWrite, "file_write", map[string]string{"path": "~/.ssh/id_rsa"}, "")
	require.False(t, decision.Allowed, "deny must win over allow, got: %+v", decision)
	require.Contains(t, decision.Reason, "~/.ssh/*", "deny reason must name the blocked rule: %s", decision.Reason)
}

// T2: glob, exact, and ~-expanded home shapes all deny.
func TestBlockedPathsPatternShapes(t *testing.T) {
	engine := newBlockedPathsEngine(t, []string{"~/secret-key.pem", "~/vault/*", "/opt/secrets"}, nil)

	glob := engine.Check(ActionFileRead, "file_read", map[string]string{"path": "~/vault/tokens.json"}, "")
	require.False(t, glob.Allowed, "glob blocked entry must deny, got: %+v", glob)

	exact := engine.Check(ActionFileRead, "file_read", map[string]string{"path": "~/secret-key.pem"}, "")
	require.False(t, exact.Allowed, "exact blocked entry must deny, got: %+v", exact)

	abs := engine.Check(ActionFileRead, "file_read", map[string]string{"path": "/opt/secrets/creds"}, "")
	require.False(t, abs.Allowed, "absolute blocked entry must deny subdirectory (containment), got: %+v", abs)
}

// Directory containment parity: a subdirectory of a blocked directory entry
// denies even when the glob itself would not match.
func TestBlockedPathsDirectoryContainment(t *testing.T) {
	engine := newBlockedPathsEngine(t, []string{"~/.gnupg"}, nil)

	decision := engine.Check(ActionFileRead, "file_read", map[string]string{"path": "~/.gnupg/pubring.kbx"}, "")
	require.False(t, decision.Allowed, "path under a blocked directory must deny, got: %+v", decision)
}

// Paths that do not touch blocked entries keep working end-to-end through
// Check (not just checkPath) with the seeded defaults active.
func TestBlockedPathsNoInterferenceWithNormalFlow(t *testing.T) {
	engine := newBlockedPathsEngine(t, []string{"~/.ssh/*", "~/.gnupg/*"}, nil)

	decision := engine.Check(ActionFileWrite, "file_write", map[string]string{"path": "~/work/file.txt"}, "")
	if decision.Reason != "" && strings.Contains(strings.ToLower(decision.Reason), "blocked path") {
		t.Fatalf("ordinary path hit the blocked-path stage: %+v", decision)
	}
}

// ---------------------------------------------------------------------------
// SecurityConfig.AllowedPaths wiring in the allow stage
// ---------------------------------------------------------------------------

// T-A: a config allow entry grants a path no DB rule covers. This is the
// dispatcher-05 shape: the e2e harness seeds allowed_paths as "<work>/**",
// while the seeded DB allow rules are only ~/* and /tmp/*, so a scratch
// daemon's project dir matched nothing and file_write was denied. Before this
// wiring the config field was validated, expanded, and shown in the config UI
// while granting nothing (its BlockedPaths twin landed in 2331d457).
func TestAllowedPathsConfigEntryGrantsNestedPath(t *testing.T) {
	engine := newBlockedPathsEngine(t, nil, []string{"/opt/e2e-work/**"})

	decision := engine.Check(ActionFileWrite, "file_write", map[string]string{"path": "/opt/e2e-work/project/quickplan.txt"}, "")
	require.True(t, decision.Allowed, "nested path under a config allow entry must be granted, got: %+v", decision)
}

// T-B: deny still wins. A config allow must never open a path a block rule
// covers, so the precedence order (blocked config, DB block rules, then allow)
// is unchanged by the wiring.
func TestAllowedPathsConfigCannotOverrideBlock(t *testing.T) {
	engine := newBlockedPathsEngine(t,
		[]string{"/opt/e2e-work/private/**"},
		[]string{"/opt/e2e-work/**"})

	decision := engine.Check(ActionFileWrite, "file_write", map[string]string{"path": "/opt/e2e-work/private/token.txt"}, "")
	require.False(t, decision.Allowed, "a config allow must not override a blocked entry, got: %+v", decision)
	require.Contains(t, decision.Reason, "/opt/e2e-work/private/**")
}

// T-C: fail-closed is preserved. Wiring config allows must not turn the allow
// stage into a no-op: a path matching neither a DB rule nor a config entry
// still denies.
func TestAllowedPathsConfigKeepsFailClosed(t *testing.T) {
	engine := newBlockedPathsEngine(t, nil, []string{"/opt/e2e-work/**"})

	decision := engine.Check(ActionFileWrite, "file_write", map[string]string{"path": "/opt/somewhere-else/file.txt"}, "")
	require.False(t, decision.Allowed, "path outside every allow rule must still deny, got: %+v", decision)
	require.Contains(t, decision.Reason, "allowed pattern")
}
