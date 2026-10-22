package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// The 2026-10-09 config destruction, verbatim from ~/.meept/meept.log:19827:
//
//	time=2026-10-09T14:28:39.437-06:00 level=INFO msg="Executing tool"
//	agent=coder tool=file_write
//	args_summary="{\"append\":false,\"content\":\"probed.\",\"direct\":true,
//	              \"path\":\"/Users/caimlas/.meept/meept.json5\"}"
//
// That replaced a 30 KB operator config with 7 bytes and the daemon stopped
// booting. These tests pin the refusal for both the write and the delete path.

func withMeeptHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(config.EnvMeeptHome, dir)
	// MeeptHome() itself must exist for EvalSymlinks to resolve it.
	if err := os.MkdirAll(filepath.Join(dir, ".meept-placeholder"), 0o700); err != nil {
		t.Fatalf("seed home: %v", err)
	}
	return dir
}

func TestGuardRefusesOverwriteOfMeeptConfig(t *testing.T) {
	home := withMeeptHome(t)
	target := filepath.Join(home, "meept.json5")

	err := guardProtectedConfigPath(target, false)
	if err == nil {
		t.Fatal("guard allowed a write to $MEEPT_HOME/meept.json5; this is the exact " +
			"call that destroyed the operator config on 2026-10-09")
	}
	if !strings.Contains(err.Error(), "meept config set") {
		t.Fatalf("error should point at the safe alternative, got: %v", err)
	}
}

func TestGuardRefusesAppendToMeeptConfig(t *testing.T) {
	home := withMeeptHome(t)
	target := filepath.Join(home, "meept.json5")

	// Appending JSON5 text makes the file unparseable, so it must be refused too.
	if err := guardProtectedConfigPath(target, true); err == nil {
		t.Fatal("guard allowed an append to the daemon config; appended JSON5 does not parse")
	}
}

func TestGuardRefusesTomlVariant(t *testing.T) {
	home := withMeeptHome(t)
	// AGENTS.md documents meept.toml as the alternative config path; it must be
	// protected too, not just the json5 name.
	if err := guardProtectedConfigPath(filepath.Join(home, "meept.toml"), false); err == nil {
		t.Fatal("guard allowed a write to $MEEPT_HOME/meept.toml")
	}
}

func TestGuardRefusesProtectedDirs(t *testing.T) {
	home := withMeeptHome(t)
	for _, rel := range []string{
		filepath.Join("skills", "my-skill.md"),
		filepath.Join("agents", "coder.toml"),
		filepath.Join("keys", "id_ed25519"),
		filepath.Join("secrets", "token"),
	} {
		target := filepath.Join(home, rel)
		if err := guardProtectedConfigPath(target, false); err == nil {
			t.Fatalf("guard allowed a write to protected path %s", target)
		}
	}
}

func TestGuardRefusesDeleteOfMeeptConfig(t *testing.T) {
	home := withMeeptHome(t)
	target := filepath.Join(home, "meept.json5")

	// The delete path calls the same guard with appendMode=false, so a delete is
	// refused identically to an overwrite.
	if err := guardProtectedConfigPath(target, false); err == nil {
		t.Fatal("guard allowed a delete of the daemon config")
	}
}

// TestGuardRefusesNonexistentTargetUnderHome proves the guard still fires for a
// CREATE (the file does not exist yet), which is the case where EvalSymlinks on
// the target itself fails and the guard must fall back to the lexical path.
func TestGuardRefusesNonexistentTargetUnderHome(t *testing.T) {
	home := withMeeptHome(t)
	target := filepath.Join(home, "meept.json5")
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Skip("target unexpectedly exists; cannot test the create path")
	}
	if err := guardProtectedConfigPath(target, false); err == nil {
		t.Fatal("guard allowed creating $MEEPT_HOME/meept.json5")
	}
}

// TestGuardRefusesMCPServersConfig pins the MCP persistent-RCE gap: a file_write
// to mcp_servers.json5 lets an agent register an MCP subprocess the daemon execs
// at startup and hot-reloads on change, so it must be refused like the other
// daemon-control files.
func TestGuardRefusesMCPServersConfig(t *testing.T) {
	home := withMeeptHome(t)
	target := filepath.Join(home, "mcp_servers.json5")

	// Create variant: the file does not exist yet.
	if err := guardProtectedConfigPath(target, false); err == nil {
		t.Fatal("guard allowed creating $MEEPT_HOME/mcp_servers.json5 (persistent RCE primitive)")
	}
	// Overwrite variant: the file already exists (the realistic agent path).
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatalf("seed mcp config: %v", err)
	}
	if err := guardProtectedConfigPath(target, false); err == nil {
		t.Fatal("guard allowed overwriting $MEEPT_HOME/mcp_servers.json5")
	}
}

// TestGuardRefusesDevKey pins that the per-installation pairing credential is
// protected: an agent that rewrites it re-keys the whole install.
func TestGuardRefusesDevKey(t *testing.T) {
	home := withMeeptHome(t)
	target := filepath.Join(home, "dev_key")

	if err := os.WriteFile(target, []byte("seed-key"), 0o600); err != nil {
		t.Fatalf("seed dev_key: %v", err)
	}
	if err := guardProtectedConfigPath(target, false); err == nil {
		t.Fatal("guard allowed overwriting $MEEPT_HOME/dev_key")
	}
	if err := guardProtectedConfigPath(target, true); err == nil {
		t.Fatal("guard allowed appending to $MEEPT_HOME/dev_key")
	}
}

func TestGuardAllowsNormalPaths(t *testing.T) {
	home := withMeeptHome(t)
	work := t.TempDir()

	allowed := []string{
		filepath.Join(work, "README.md"),
		filepath.Join(work, "src", "main.go"),
		// The daemon's own DATA (not config) stays writable: tasks.db, plans.db
		// and meept.log are the daemon's to manage, and refusing them would
		// break legitimate operation.
		filepath.Join(home, "tasks.db"),
		filepath.Join(home, "plans.db"),
		filepath.Join(home, "meept.log"),
	}
	for _, p := range allowed {
		if err := guardProtectedConfigPath(p, false); err != nil {
			t.Fatalf("guard blocked a legitimate path %s: %v", p, err)
		}
	}
}

// TestGuardIsIndependentOfTheFence is the point of the whole change: the
// incident happened with the fence DISABLED per session (--nofence), and the
// permission checker was already consulted separately. The guard keys only off
// MEEPT_HOME plus the destination, so nothing that can be toggled at runtime
// disables it.
func TestGuardIsIndependentOfTheFence(t *testing.T) {
	home := withMeeptHome(t)
	target := filepath.Join(home, "meept.json5")

	// No fence checker, no permission checker, nothing wired — the guard is a
	// pure function of the path.
	for i := 0; i < 3; i++ {
		if err := guardProtectedConfigPath(target, false); err == nil {
			t.Fatalf("attempt %d: guard was bypassable", i)
		}
	}
}

// TestFileWriteToolRefusesConfigOverwrite exercises the real tool end to end,
// which is what actually failed on 2026-10-09. direct:true reproduces the exact
// headless/benchmark configuration from the incident.
func TestFileWriteToolRefusesConfigOverwrite(t *testing.T) {
	home := withMeeptHome(t)
	configPath := filepath.Join(home, "meept.json5")
	original := `{"daemon": {"log_level": "INFO"}}`
	if err := os.WriteFile(configPath, []byte(original), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	tool := &WriteFileTool{}
	res, err := tool.Execute(context.Background(), map[string]any{
		"path":    configPath,
		"content": "probed.",
		"direct":  true, // the incident's exact args
	})
	if err == nil {
		t.Fatalf("file_write overwrote the daemon config; result=%v", res)
	}

	// The original content must survive untouched.
	got, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read back: %v", readErr)
	}
	if string(got) != original {
		t.Fatalf("config was modified: got %q, want %q", got, original)
	}
}

// TestFileWriteToolRefusesConfigAppend covers the append variant through the tool.
func TestFileWriteToolRefusesConfigAppend(t *testing.T) {
	home := withMeeptHome(t)
	configPath := filepath.Join(home, "meept.json5")
	if err := os.WriteFile(configPath, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tool := &WriteFileTool{}
	if _, err := tool.Execute(context.Background(), map[string]any{
		"path":    configPath,
		"content": "probed.",
		"append":  true,
		"direct":  true,
	}); err == nil {
		t.Fatal("file_write appended to the daemon config")
	}
	got, _ := os.ReadFile(configPath)
	if string(got) != `{"a":1}` {
		t.Fatalf("config was modified by the append attempt: %q", got)
	}
}

// TestDeleteFileToolRefusesConfigDelete proves the delete path is fenced too.
func TestDeleteFileToolRefusesConfigDelete(t *testing.T) {
	home := withMeeptHome(t)
	configPath := filepath.Join(home, "meept.json5")
	if err := os.WriteFile(configPath, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tool := &DeleteFileTool{}
	if _, err := tool.Execute(context.Background(), map[string]any{
		"path": configPath,
	}); err == nil {
		t.Fatal("file_delete removed the daemon config")
	}
	if _, statErr := os.Stat(configPath); statErr != nil {
		t.Fatalf("config is gone after the delete attempt: %v", statErr)
	}
}

// TestGuardRefusesDevKeyRead pins the read-side guard (audit M5 Phase 2c):
// an agent that reads dev_key holds the API credential for the whole
// authenticated HTTP surface.
func TestGuardRefusesDevKeyRead(t *testing.T) {
	home := withMeeptHome(t)
	target := filepath.Join(home, "dev_key")
	if err := os.WriteFile(target, []byte("seed-key"), 0o600); err != nil {
		t.Fatalf("seed dev_key: %v", err)
	}
	if err := guardSecretReadPath(target); err == nil {
		t.Fatal("read guard allowed $MEEPT_HOME/dev_key")
	}
}

// TestGuardRefusesCredentialDirReads pins the keys/ and secrets/ read denial.
func TestGuardRefusesCredentialDirReads(t *testing.T) {
	home := withMeeptHome(t)
	for _, dir := range []string{"keys", "secrets"} {
		d := filepath.Join(home, dir)
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		f := filepath.Join(d, "cred.txt")
		if err := os.WriteFile(f, []byte("s3cret"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", f, err)
		}
		if err := guardSecretReadPath(f); err == nil {
			t.Fatalf("read guard allowed %s", f)
		}
	}
}

// TestGuardAllowsNonSecretReads pins the deliberate narrowness: daemon DATA
// and non-secret config stay readable (meept.json5, meept.log, tasks.db,
// skills/, agents/) — the read guard protects CREDENTIALS only.
func TestGuardAllowsNonSecretReads(t *testing.T) {
	home := withMeeptHome(t)
	work := t.TempDir()

	allowed := []string{
		filepath.Join(work, "README.md"),
		filepath.Join(home, "meept.json5"),
		filepath.Join(home, "meept.log"),
		filepath.Join(home, "tasks.db"),
		filepath.Join(home, "skills", "x", "SKILL.md"),
		filepath.Join(home, "agents", "chat", "AGENT.md"),
	}
	for _, p := range allowed {
		if err := guardSecretReadPath(p); err != nil {
			t.Fatalf("read guard blocked a legitimate path %s: %v", p, err)
		}
	}
}

// TestGuardReadFollowsSymlinks pins the symlink discipline: a symlink
// elsewhere pointing AT dev_key is still refused (EvalSymlinks on the
// target is the point of the check).
func TestGuardReadFollowsSymlinks(t *testing.T) {
	home := withMeeptHome(t)
	real := filepath.Join(home, "dev_key")
	if err := os.WriteFile(real, []byte("seed-key"), 0o600); err != nil {
		t.Fatalf("seed dev_key: %v", err)
	}
	link := filepath.Join(t.TempDir(), "innocent.txt")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := guardSecretReadPath(link); err == nil {
		t.Fatal("read guard followed a symlink away from dev_key")
	}
}

// TestReadFileToolRefusesDevKey exercises the real tool end to end: no
// fence, no permission checker wired (nil, nil) — the guard must fire on
// path alone, exactly like the write guard in the 2026-10-09 incident.
func TestReadFileToolRefusesDevKey(t *testing.T) {
	home := withMeeptHome(t)
	keyPath := filepath.Join(home, "dev_key")
	if err := os.WriteFile(keyPath, []byte("super-secret-api-key"), 0o600); err != nil {
		t.Fatalf("seed dev_key: %v", err)
	}

	tool := NewReadFileTool(nil, nil)
	res, err := tool.Execute(context.Background(), map[string]any{"path": keyPath})
	if err == nil {
		t.Fatalf("file_read returned the dev_key: %v", res)
	}
	if !strings.Contains(err.Error(), "credential material") {
		t.Fatalf("error = %v, want a credential-material refusal", err)
	}
}
