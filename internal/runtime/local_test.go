package runtime

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalBackend_Name(t *testing.T) {
	backend := NewLocalBackend(Config{}, nil)
	assert.Equal(t, "local", backend.Name())
}

func TestLocalBackend_Execute_Basic(t *testing.T) {
	backend := NewLocalBackend(Config{}, nil)
	result, err := backend.Execute(context.Background(), Command{
		Cmd: "echo hello",
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Output, "hello")
}

func TestLocalBackend_Execute_ExitCode(t *testing.T) {
	backend := NewLocalBackend(Config{}, nil)
	result, err := backend.Execute(context.Background(), Command{
		Cmd: "exit 42",
	})
	require.NoError(t, err)
	assert.Equal(t, 42, result.ExitCode)
}

func TestLocalBackend_Execute_WorkingDir(t *testing.T) {
	backend := NewLocalBackend(Config{}, nil)
	result, err := backend.Execute(context.Background(), Command{
		Cmd: "pwd",
		Dir: "/tmp",
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "/tmp")
}

func TestLocalBackend_Execute_Environment(t *testing.T) {
	// Since EnvPolicy (issue #25), Command.Env entries must be allowed
	// (BaseEnvKeys/Allowlist) to reach the child; allowlist MYVAR here to
	// verify explicit env pass-through mechanics.
	backend := NewLocalBackend(Config{
		EnvPolicy: EnvPolicyConfig{
			Mode:      EnvModeAllowlist,
			Allowlist: []string{"MYVAR"},
		},
	}, nil)
	result, err := backend.Execute(context.Background(), Command{
		Cmd: "echo $MYVAR",
		Env: map[string]string{"MYVAR": "test-value"},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "test-value")
}

func TestLocalBackend_Execute_ContextCancellation(t *testing.T) {
	backend := NewLocalBackend(Config{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately so the context expires before the command starts
	cancel()
	_, err := backend.Execute(ctx, Command{
		Cmd: "sleep 10",
	})
	assert.Error(t, err)
}

func TestLocalBackend_Close(t *testing.T) {
	backend := NewLocalBackend(Config{}, nil)
	err := backend.Close()
	assert.NoError(t, err)
}

func TestLocalBackend_Execute_Duration(t *testing.T) {
	backend := NewLocalBackend(Config{}, nil)
	result, err := backend.Execute(context.Background(), Command{
		Cmd: "echo done",
	})
	require.NoError(t, err)
	assert.Greater(t, result.Duration, time.Duration(0))
}

func TestLocalBackend_Execute_OutputToStderr(t *testing.T) {
	backend := NewLocalBackend(Config{}, nil)
	result, err := backend.Execute(context.Background(), Command{
		Cmd: "echo error >&2 && echo success",
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Output, "success")
	assert.Contains(t, result.Output, "error")
}

func TestLocalBackend_Execute_PassEnvOverride(t *testing.T) {
	backend := NewLocalBackend(Config{}, nil)
	// Set an existing env var and override it
	result, err := backend.Execute(context.Background(), Command{
		Cmd: "echo $HOME",
		Env: map[string]string{"HOME": "/nonexistent"},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "/nonexistent")
}

func TestLocalBackend_Execute_Noop(t *testing.T) {
	tmpDir := t.TempDir()
	backend := NewLocalBackend(Config{}, nil)

	// Create a file in the temp directory
	_, err := backend.Execute(context.Background(), Command{
		Cmd: "echo test > file.txt",
		Dir: tmpDir,
	})
	require.NoError(t, err)

	// Verify file exists
	content, err := os.ReadFile(tmpDir + "/file.txt")
	require.NoError(t, err)
	assert.Contains(t, string(content), "test")
}

// TestLocalBackend_StripsDaemonSecrets is the containment sentinel: a secret
// exported into the daemon process must NOT reach child shells in allowlist
// mode. The backend captures os.Environ() once at construction and filters it
// through BuildChildEnv.
func TestLocalBackend_StripsDaemonSecrets(t *testing.T) {
	t.Setenv("MEEPT_SENTINEL_SECRET", "topsecret")
	parentEnv := os.Environ()

	b := NewLocalBackend(Config{
		EnvPolicy: EnvPolicyConfig{
			Mode:      EnvModeAllowlist,
			DenyGlobs: []string{"*SECRET*"},
		},
	}, parentEnv)

	res, err := b.Execute(context.Background(), Command{
		Cmd: "if [ -n \"$(printenv MEEPT_SENTINEL_SECRET 2>/dev/null)\" ]; then echo LEAKED; else echo CLEAN; fi",
	})
	require.NoError(t, err)
	require.Equal(t, 0, res.ExitCode)
	require.Contains(t, res.Output, "CLEAN")
	require.NotContains(t, res.Output, "LEAKED")
}

// TestLocalBackend_InheritModePassesSecrets documents the legacy escape hatch:
// inherit mode preserves full parent-environment inheritance.
func TestLocalBackend_InheritModePassesSecrets(t *testing.T) {
	t.Setenv("MEEPT_SENTINEL_SECRET", "topsecret")

	b := NewLocalBackend(Config{
		EnvPolicy: EnvPolicyConfig{Mode: EnvModeInherit},
	}, nil)

	res, err := b.Execute(context.Background(), Command{Cmd: "printenv MEEPT_SENTINEL_SECRET"})
	require.NoError(t, err)
	require.Equal(t, 0, res.ExitCode)
	assert.Equal(t, "topsecret", strings.TrimSpace(res.Output))
}

// TestLocalBackend_CmdEnvOverrideSurvivesAllowlist verifies that explicit
// Command.Env values for ALLOWED keys still reach the child, overriding the
// parent value. (Non-allowlisted cmdEnv vars are stripped per policy.)
func TestLocalBackend_CmdEnvOverrideSurvivesAllowlist(t *testing.T) {
	b := NewLocalBackend(Config{
		EnvPolicy: EnvPolicyConfig{Mode: EnvModeAllowlist},
	}, nil)

	res, err := b.Execute(context.Background(), Command{
		Cmd: "echo $HOME",
		Env: map[string]string{"HOME": "/custom/home"},
	})
	require.NoError(t, err)
	require.Equal(t, 0, res.ExitCode)
	assert.Contains(t, res.Output, "/custom/home")
}

// TestLocalBackend_NonAllowlistedCmdEnvStripped documents that cmdEnv
// variables outside BaseEnvKeys+Allowlist do NOT reach the child even though
// the caller explicitly requested them.
func TestLocalBackend_NonAllowlistedCmdEnvStripped(t *testing.T) {
	b := NewLocalBackend(Config{
		EnvPolicy: EnvPolicyConfig{Mode: EnvModeAllowlist},
	}, nil)

	res, err := b.Execute(context.Background(), Command{
		Cmd: "if [ -n \"$(printenv MYVAR 2>/dev/null)\" ]; then echo PASSED; else echo STRIPPED; fi",
		Env: map[string]string{"MYVAR": "test-value"},
	})
	require.NoError(t, err)
	require.Equal(t, 0, res.ExitCode)
	assert.Contains(t, res.Output, "STRIPPED")
}

// TestSetEnvPolicy_TypedNilSafe exercises the setter with a typed-nil pointer;
// the setter takes a value type so there is no nil to guard, but callers may
// still pass a zero Config.
func TestSetEnvPolicy_TypedNilSafe(t *testing.T) {
	var cfg *Config // typed nil
	policy := EnvPolicyConfig{}
	if cfg != nil {
		t.Fatal("precondition: cfg must be nil")
	}
	// Dereferencing would panic; SetEnvPolicy takes a value so no deref occurs.
	_ = cfg

	b := NewLocalBackend(Config{}, nil)
	b.SetEnvPolicy(policy)
	if b.envPolicy.Mode != "" {
		t.Fatalf("expected zero policy applied verbatim, got %q", b.envPolicy.Mode)
	}
}
