package runtime

import (
	"context"
	"fmt"
	"time"
)

// TestHarnessConfig holds test harness configuration.
type TestHarnessConfig struct {
	InstallCommand string
	TestCommand    string
	Timeout        time.Duration
}

// TestHarness validates changes by running a test suite.
type TestHarness struct {
	config  TestHarnessConfig
	backend ExecutionBackend
}

// ValidationResult holds test results.
type ValidationResult struct {
	Passed   bool
	Output   string
	Duration time.Duration
}

// NewTestHarness creates a new test harness.
func NewTestHarness(cfg TestHarnessConfig, backend ExecutionBackend) *TestHarness {
	return &TestHarness{
		config:  cfg,
		backend: backend,
	}
}

// Validate runs the test harness and returns results.
func (h *TestHarness) Validate(ctx context.Context, workdir string) (*ValidationResult, error) {
	result := &ValidationResult{Passed: false}
	start := time.Now()

	// Run install command if configured
	if h.config.InstallCommand != "" && h.backend != nil {
		installCmd := Command{
			Cmd:     h.config.InstallCommand,
			Dir:     workdir,
			Timeout: h.config.Timeout,
		}

		installResult, err := h.backend.Execute(ctx, installCmd)
		if err != nil {
			return nil, fmt.Errorf("install failed: %w", err)
		}
		if installResult.ExitCode != 0 {
			result.Output = installResult.Output
			result.Duration = time.Since(start)
			return result, nil
		}
	}

	// Run test command
	if h.backend == nil {
		result.Passed = true  // No backend means no tests to fail
		result.Duration = time.Since(start)
		return result, nil
	}

	testResult, err := h.backend.Execute(ctx, Command{
		Cmd:     h.config.TestCommand,
		Dir:     workdir,
		Timeout: h.config.Timeout,
	})

	result.Duration = time.Since(start)

	if err != nil {
		// testResult may be nil when the backend returns an error
		// (e.g., local backend: nil, fmt.Errorf(...)), so defer
		//encing testResult before the err check would panic.
		return nil, err
	}

	result.Output = testResult.Output
	result.Passed = testResult.ExitCode == 0
	return result, nil
}
