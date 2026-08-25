package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/runtime"
	"github.com/caimlas/meept/internal/tools/builtin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildRuntimeConfig_SandboxMapping verifies [runtime].sandbox mapping
// into the runtime package's Config.
func TestBuildRuntimeConfig_SandboxMapping(t *testing.T) {
	logger := newCaptureLogger(&captureHandler{})

	rc := config.RuntimeConfig{
		Enabled: true,
		Sandbox: config.ResolverConfig{
			Order:          "bwrap",
			RequireSandbox: true,
		},
	}

	got := buildRuntimeConfig(rc, logger)

	assert.Equal(t, runtime.SandboxOrderBwrap, got.Sandbox.Order)
	assert.True(t, got.Sandbox.RequireSandbox)
}

// TestBuildRuntimeConfig_SandboxDefaultEmpty verifies an unset sandbox order
// maps to the zero value (resolver treats "" as auto).
func TestBuildRuntimeConfig_SandboxDefaultEmpty(t *testing.T) {
	logger := newCaptureLogger(&captureHandler{})

	got := buildRuntimeConfig(config.RuntimeConfig{Enabled: true}, logger)

	assert.Equal(t, runtime.SandboxOrder(""), got.Sandbox.Order)
	assert.False(t, got.Sandbox.RequireSandbox)
}

// shellBackendForTest reads the tool's actual wired backend through the
// exported accessor, letting tests assert on the real refusing/resolved
// backend rather than a stand-in.
func shellBackendForTest(t *testing.T, shellTool *builtin.ShellExecuteTool) runtime.ExecutionBackend {
	t.Helper()
	return shellTool.Backend()
}

// TestWireShellToolSandbox_RequireUnavailableRefuses is THE fail-closed
// proof: when ResolveBackend returns ErrSandboxRequired and [runtime] is
// enabled, the shell tool must receive a REFUSING backend whose Execute
// surfaces the ErrSandboxRequired text — never a nil-manager direct-exec
// fallback.
func TestWireShellToolSandbox_RequireUnavailableRefuses(t *testing.T) {
	h := &captureHandler{}
	logger := newCaptureLogger(h)

	shellTool := builtin.NewShellExecuteTool("", 0, nil)

	wireShellToolSandbox(
		shellTool,
		fmt.Errorf("resolving backend: %w", runtime.ErrSandboxRequired),
		true, // [runtime].enabled
		logger,
	)

	backend := shellBackendForTest(t, shellTool)
	require.NotNil(t, backend,
		"shell tool must carry a refusing backend, not fall back to direct exec")

	_, err := backend.Execute(context.Background(), runtime.Command{Cmd: "echo hi"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, runtime.ErrSandboxRequired),
		"refusal must wrap ErrSandboxRequired; got: %v", err)
	assert.Contains(t, err.Error(),
		"sandbox required but no qualifying backend available",
		"contract text must surface to callers")
	assert.True(t, h.contains("REFUSE"),
		"expected a startup log about refusing execution; got %v", h.records)
}

// TestWireShellToolSandbox_SuccessNoRefusal verifies that a nil resolve
// error installs nothing: no refusing backend, no refusal logs.
func TestWireShellToolSandbox_SuccessNoRefusal(t *testing.T) {
	h := &captureHandler{}
	logger := newCaptureLogger(h)

	shellTool := builtin.NewShellExecuteTool("", 0, nil)

	wireShellToolSandbox(shellTool, nil, true, logger)

	assert.False(t, shellTool.BackendWired(),
		"a healthy resolution must not install the refuser here "+
			"(resolved-backend wiring happens at tool registration)")
	assert.False(t, h.contains("REFUSE"), "no refusal logs expected")
}

// TestWireShellToolSandbox_RuntimeDisabledNoOp verifies the documented
// posture distinction: when [runtime].enabled=false entirely, existing
// behavior is unchanged — no refusing backend is installed even if a resolve
// error is somehow present.
func TestWireShellToolSandbox_RuntimeDisabledNoOp(t *testing.T) {
	h := &captureHandler{}
	logger := newCaptureLogger(h)

	shellTool := builtin.NewShellExecuteTool("", 0, nil)

	wireShellToolSandbox(
		shellTool,
		errors.New("runtime: sandbox required but no qualifying backend available"),
		false, // [runtime].disabled
		logger,
	)

	assert.Nil(t, shellBackendForTest(t, shellTool),
		"runtime disabled must leave the tool in its pre-existing direct-exec posture")
}

// TestRefusingBackend_Unit pins RefusingBackend semantics directly.
func TestRefusingBackend_Unit(t *testing.T) {
	rb := newRefusingBackend(fmt.Errorf("resolving backend: %w", runtime.ErrSandboxRequired))
	assert.Equal(t, "refusing", rb.Name())
	require.NoError(t, rb.Close())

	res, err := rb.Execute(context.Background(), runtime.Command{Cmd: "ls"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, runtime.ErrSandboxRequired))
	assert.Nil(t, res)
	assert.True(t, strings.Contains(err.Error(), "sandbox"),
		"error text should mention sandbox")
}
