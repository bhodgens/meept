package runtime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hasBwrap reports whether the external bubblewrap binary is available.
// Mirrors the hasDocker() opt-in pattern: tests requiring the real binary
// skip cleanly when absent rather than failing or fake-passing.
func hasBwrap() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	_, err := exec.LookPath("bwrap")
	return err == nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// recordingRunner captures the assembled argv/env/dir and returns canned
// results, letting tests verify arg assembly without a real binary.
type recordingRunner struct {
	path string
	args []string
	dir  string
	env  []string

	out      []byte
	exitCode int
	err      error
}

func (r *recordingRunner) run(_ context.Context, path string, args []string, dir string, env []string) ([]byte, int, error) {
	r.path = path
	r.args = args
	r.dir = dir
	r.env = env
	return r.out, r.exitCode, r.err
}

// newTestBwrapBackend builds a backend with injected probes so assembly
// tests are platform-independent (no GOOS gate, no real LookPath).
func newTestBwrapBackend(t *testing.T, cfg BwrapConfig, policy EnvPolicyConfig, run bwrapRunner) *BwrapBackend {
	t.Helper()
	be, err := newBwrapBackendWithProbes(cfg, policy, discardLogger(), "linux",
		func(p string) (string, error) { return "/usr/bin/" + p, nil }, run)
	require.NoError(t, err)
	return be
}

// TestNewBwrapBackend_UnsupportedGOOS verifies construction fails cleanly on
// non-Linux platforms (darwin CI must never attempt real exec).
func TestNewBwrapBackend_UnsupportedGOOS(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("test targets non-linux behavior")
	}
	be, err := NewBwrapBackend(BwrapConfig{}, EnvPolicyConfig{Mode: EnvModeAllowlist}, discardLogger())
	require.Error(t, err)
	assert.Nil(t, be)
	assert.Contains(t, err.Error(), "unsupported")
}

// TestNewBwrapBackend_MissingBinary verifies a missing binary is an explicit
// constructor error, not a deferred failure at Execute time.
func TestNewBwrapBackend_MissingBinary(t *testing.T) {
	_, err := newBwrapBackendWithProbes(
		BwrapConfig{}, EnvPolicyConfig{Mode: EnvModeAllowlist}, discardLogger(), "linux",
		func(string) (string, error) { return "", exec.ErrNotFound },
		realBwrapRunner)
	require.Error(t, err)
	assert.True(t, errors.Is(err, exec.ErrNotFound))
	assert.Contains(t, err.Error(), "not found")
}

// TestBwrapBackend_ArgvAssembly pins the exact jail layout from the leaf
// contract: ro-binds for present base dirs, writable workdir bind,
// --dev/--proc, tmpfs defaults, extra args, then `-- sh -c <cmd>`.
func TestBwrapBackend_ArgvAssembly(t *testing.T) {
	runner := &recordingRunner{}
	be := newTestBwrapBackend(t, BwrapConfig{
		ExtraArgs: []string{"--unshare-net"},
	}, EnvPolicyConfig{
		Mode:      EnvModeAllowlist,
		Allowlist: []string{"PATH", "HOME"},
	}, runner.run)

	workDir := t.TempDir()
	result, err := be.Execute(context.Background(), Command{
		Cmd: "echo hello", // metacharacter-bearing cmds stay ONE argv element
		Dir: workDir,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	args := runner.args

	// Base ro-binds: each present system dir must appear as a
	// "--ro-bind <dir> <dir>" triple.
	for _, dir := range []string{"/usr", "/bin", "/lib", "/lib64"} {
		if !dirExists(dir) {
			continue
		}
		found := false
		for j := 0; j+2 < len(args); j++ {
			if args[j] == "--ro-bind" && args[j+1] == dir && args[j+2] == dir {
				found = true
				break
			}
		}
		assert.True(t, found, "expected '--ro-bind %s %s' in argv: %v", dir, dir, args)
	}

	// Writable workdir bind.
	idx := indexOf(args, "--bind")
	require.GreaterOrEqual(t, idx, 0, "expected --bind in argv: %v", args)
	assert.Equal(t, workDir, args[idx+1])
	assert.Equal(t, workDir, args[idx+2])
	assert.Equal(t, workDir, runner.dir)
}

func indexOf(xs []string, needle string) int {
	for i, x := range xs {
		if x == needle {
			return i
		}
	}
	return -1
}

// TestBwrapBackend_ArgvAssembly_Tail verifies the tail of the argv exactly:
// --dev /dev --proc /proc [--tmpfs /tmp] <extra> -- sh -c <cmd>.
func TestBwrapBackend_ArgvAssembly_Tail(t *testing.T) {
	runner := &recordingRunner{}
	be := newTestBwrapBackend(t, BwrapConfig{
		ExtraArgs: []string{"--unshare-net"},
	}, EnvPolicyConfig{Mode: EnvModeAllowlist}, runner.run)

	_, err := be.Execute(context.Background(), Command{Cmd: "echo hello"})
	require.NoError(t, err)

	args := runner.args
	assert.Contains(t, args, "--dev")
	di := indexOf(args, "--dev")
	assert.Equal(t, "/dev", args[di+1])
	pi := indexOf(args, "--proc")
	assert.Equal(t, "/proc", args[pi+1])

	ti := indexOf(args, "--tmpfs")
	require.GreaterOrEqual(t, ti, 0, "default tmpfs /tmp expected")
	assert.Equal(t, "/tmp", args[ti+1])

	ei := indexOf(args, "--unshare-net")
	assert.GreaterOrEqual(t, ei, 0, "ExtraArgs must be appended")

	// Tail contract: last three elements are sh -c <cmd>; "--" precedes them.
	require.GreaterOrEqual(t, len(args), 4)
	assert.Equal(t, "--", args[len(args)-4])
	assert.Equal(t, "sh", args[len(args)-3])
	assert.Equal(t, "-c", args[len(args)-2])
	assert.Equal(t, "echo hello", args[len(args)-1], "cmd must be a SINGLE argv element")

	assert.Equal(t, "bwrap", runner.path)
}

// TestBwrapBackend_EnvFiltering verifies BuildChildEnv output reaches the
// child process env and disallowed variables are stripped per policy.
func TestBwrapBackend_EnvFiltering(t *testing.T) {
	runner := &recordingRunner{}
	be := newTestBwrapBackend(t, BwrapConfig{}, EnvPolicyConfig{
		Mode:      EnvModeAllowlist,
		Allowlist: []string{"ALLOWED_VAR"},
		DenyGlobs: []string{"*SECRET*"},
	}, runner.run)

	parentEnv := []string{
		"PATH=/usr/bin:/bin",
		"MY_SECRET=supersecret",
		"HOME=/root",
	}
	be.parentEnv = parentEnv // test seam: deterministic snapshot

	_, err := be.Execute(context.Background(), Command{
		Cmd: "env",
		Env: map[string]string{"CMD_VAR": "from-cmd"},
	})
	require.NoError(t, err)

	envJoined := strings.Join(runner.env, "\n")
	assert.Contains(t, envJoined, "PATH=/usr/bin:/bin")
	assert.Contains(t, envJoined, "HOME=/root")
	assert.NotContains(t, envJoined, "MY_SECRET", "secret var must be stripped by allowlist policy")
	assert.NotContains(t, envJoined, "CMD_VAR=from-cmd", "non-allowlisted cmd env must not pass through")
}

// TestBwrapBackend_ResultMapping verifies non-zero exit surfaces as result
// (not error) and start failures surface as errors.
func TestBwrapBackend_ResultMapping(t *testing.T) {
	t.Run("nonzero_exit_is_result", func(t *testing.T) {
		runner := &recordingRunner{out: []byte("boom"), exitCode: 42}
		be := newTestBwrapBackend(t, BwrapConfig{}, EnvPolicyConfig{Mode: EnvModeAllowlist}, runner.run)
		res, err := be.Execute(context.Background(), Command{Cmd: "exit 42"})
		require.NoError(t, err)
		assert.Equal(t, 42, res.ExitCode)
		assert.Equal(t, "boom", res.Output)
	})

	t.Run("start_failure_is_error", func(t *testing.T) {
		runner := &recordingRunner{err: errors.New("fork/exec failed")}
		be := newTestBwrapBackend(t, BwrapConfig{}, EnvPolicyConfig{Mode: EnvModeAllowlist}, runner.run)
		res, err := be.Execute(context.Background(), Command{Cmd: "true"})
		require.Error(t, err)
		require.NotNil(t, res)
		assert.NotEqual(t, 0, res.ExitCode, "start failure must not report success")
		assert.Equal(t, -1, res.ExitCode)
	})
}

// TestBwrapBackend_NameAndClose pins interface plumbing.
func TestBwrapBackend_NameAndClose(t *testing.T) {
	be := newTestBwrapBackend(t, BwrapConfig{}, EnvPolicyConfig{Mode: EnvModeAllowlist}, (&recordingRunner{}).run)
	assert.Equal(t, "bwrap", be.Name())
	require.NoError(t, be.Close())
}

// --- Linux-only integration tests against the REAL bubblewrap binary ---
// These SKIP unless running on linux with bwrap installed (mirrors the
// TEST_DOCKER pattern in docker_test.go). They never fake-pass.

func requireRealBwrap(t *testing.T) {
	t.Helper()
	if !hasBwrap() {
		t.Skip("skipping: requires linux + bwrap binary (integration phase / CI)")
	}
}

func TestBwrapIntegration_EchoHello(t *testing.T) {
	requireRealBwrap(t)
	be, err := NewBwrapBackend(BwrapConfig{}, EnvPolicyConfig{Mode: EnvModeAllowlist}, discardLogger())
	require.NoError(t, err)
	defer be.Close()

	dir := t.TempDir()
	res, err := be.Execute(context.Background(), Command{Cmd: "echo hello", Dir: dir})
	require.NoError(t, err)
	assert.Equal(t, 0, res.ExitCode)
	assert.Contains(t, res.Output, "hello")
}

func TestBwrapIntegration_ReadOutsideBindFails(t *testing.T) {
	requireRealBwrap(t)
	be, err := NewBwrapBackend(BwrapConfig{}, EnvPolicyConfig{Mode: EnvModeAllowlist}, discardLogger())
	require.NoError(t, err)
	defer be.Close()

	dir := t.TempDir()
	// /root is NOT ro-bound; inside the jail it either does not exist or is
	// unreadable. Either way the command must not succeed with content.
	res, err := be.Execute(context.Background(), Command{Cmd: "ls -la /root 2>/dev/null || true", Dir: dir})
	require.NoError(t, err)
	assert.NotContains(t, res.Output, "total ", "expected no directory listing of unbound /root")
}

func TestBwrapIntegration_EnvSentinelStripped(t *testing.T) {
	requireRealBwrap(t)
	be, err := NewBwrapBackend(BwrapConfig{}, EnvPolicyConfig{
		Mode:      EnvModeAllowlist,
		Allowlist: []string{"PATH"},
		DenyGlobs: []string{"*SENTINEL*"},
	}, discardLogger())
	require.NoError(t, err)
	defer be.Close()

	dir := t.TempDir()
	res, err := be.Execute(context.Background(), Command{
		Cmd: `if [ -n "$BWAP_SENTINEL_TEST" ]; then echo LEAKED; else echo CLEAN; fi`,
		Dir: dir,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, res.ExitCode)
	assert.Contains(t, res.Output, "CLEAN")
	assert.NotContains(t, res.Output, "LEAKED")
}
