package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"syscall"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/pathutil"
	"github.com/spf13/cobra"
)

func newRuntimeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runtime <status|start|stop|restart> [provider]",
		Short: "Manage local LLM runtime processes",
		Long: `Manage local LLM runtime processes (llama.cpp, MLX).

Examples:
  meept runtime status          # Show runtime status for default provider
  meept runtime status local    # Show runtime status for specific provider
  meept runtime start           # Start the runtime for default provider
  meept runtime stop            # Stop the runtime for default provider
  meept runtime restart         # Restart the runtime for default provider
  meept runtime start local     # Start runtime for specific provider`,
	}

	cmd.AddCommand(newRuntimeStatusCmd())
	cmd.AddCommand(newRuntimeStartCmd())
	cmd.AddCommand(newRuntimeStopCmd())
	cmd.AddCommand(newRuntimeRestartCmd())

	return cmd
}

func newRuntimeStatusCmd() *cobra.Command {
	var provider string

	cmd := &cobra.Command{
		Use:   "status [provider]",
		Short: "Show local LLM runtime status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				provider = args[0]
			}
			return runRuntimeStatus(cmd.Context(), provider)
		},
	}

	return cmd
}

func newRuntimeStartCmd() *cobra.Command {
	var provider string

	cmd := &cobra.Command{
		Use:   "start [provider]",
		Short: "Start the local LLM runtime",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				provider = args[0]
			}
			return runRuntimeStart(cmd.Context(), provider)
		},
	}

	return cmd
}

func newRuntimeStopCmd() *cobra.Command {
	var provider string

	cmd := &cobra.Command{
		Use:   "stop [provider]",
		Short: "Stop the local LLM runtime",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				provider = args[0]
			}
			return runRuntimeStop(cmd.Context(), provider)
		},
	}

	return cmd
}

func newRuntimeRestartCmd() *cobra.Command {
	var provider string

	cmd := &cobra.Command{
		Use:   "restart [provider]",
		Short: "Restart the local LLM runtime",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				provider = args[0]
			}
			return runRuntimeRestart(cmd.Context(), provider)
		},
	}

	return cmd
}

// loadRuntimeConfig loads the provider config and returns the configured provider.
func loadRuntimeConfig(provider string) (*llm.ProvidersConfig, *llm.ProviderConfig, error) {
	cfg, err := llm.LoadProvidersConfigDefault()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	if provider == "" {
		provider = "local"
	}

	pc, ok := cfg.Providers[provider]
	if !ok {
		return nil, nil, fmt.Errorf("provider not found: %s", provider)
	}

	if pc.Lifecycle == nil {
		return nil, nil, fmt.Errorf("provider %s has no lifecycle config", provider)
	}

	return cfg, &pc, nil
}

// pidFileFromConfig resolves the expanded PID file path from lifecycle config.
func pidFileFromConfig(lc *llm.RuntimeLifecycleConfig) string {
	return pathutil.ExpandPath(lc.PIDFile)
}

// checkProcessAlive checks if a PID is alive via signal 0.
func checkProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// readPID reads a PID from the given file path.
func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}

// runRuntimeStatus shows the current runtime status.
func runRuntimeStatus(ctx context.Context, provider string) error {
	_, pc, err := loadRuntimeConfig(provider)
	if err != nil {
		return err
	}

	pidFile := pidFileFromConfig(pc.Lifecycle)

	// Check if PID file exists
	data, err := os.ReadFile(pidFile)
	if os.IsNotExist(err) {
		fmt.Printf("Runtime %s: not running (no PID file)\n", provider)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return fmt.Errorf("invalid PID in file: %w", err)
	}

	if !checkProcessAlive(pid) {
		fmt.Printf("Runtime %s: not running (process dead, PID: %d)\n", provider, pid)
		os.Remove(pidFile)
		return nil
	}

	// Process is running
	fmt.Printf("Runtime %s: running (PID: %d)\n", provider, pid)

	baseURL := pc.Options.BaseURL
	healthEndpoint := pc.Lifecycle.HealthCheck.Endpoint
	if healthEndpoint == "" {
		healthEndpoint = "/health"
	}
	fmt.Printf("  Health endpoint:  %s%s\n", baseURL, healthEndpoint)
	fmt.Printf("  PID file:         %s\n", pidFile)

	return nil
}

// runRuntimeStart starts the runtime.
func runRuntimeStart(ctx context.Context, provider string) error {
	_, pc, err := loadRuntimeConfig(provider)
	if err != nil {
		return err
	}

	rtCfg, err := llm.ValidateAndNormalize(*pc.Lifecycle)
	if err != nil {
		return fmt.Errorf("invalid lifecycle config: %w", err)
	}

	pidFile := rtCfg.PIDFile

	// Check if already running
	if data, err := os.ReadFile(pidFile); err == nil {
		if pid, err := strconv.Atoi(string(data)); err == nil {
			if checkProcessAlive(pid) {
				return fmt.Errorf("runtime %s is already running (PID: %d)", provider, pid)
			}
		}
		// Stale PID file
		os.Remove(pidFile)
	}

	// Spawn the process
	runtimeProc := llm.NewRuntimeProcess(rtCfg)
	if err := runtimeProc.Start(ctx); err != nil {
		return fmt.Errorf("failed to start runtime: %w", err)
	}

	fmt.Printf("Runtime %s started (PID: %d)\n", provider, runtimeProc.PID())
	return nil
}

// runRuntimeStop stops the runtime.
func runRuntimeStop(ctx context.Context, provider string) error {
	_, pc, err := loadRuntimeConfig(provider)
	if err != nil {
		return err
	}

	pidFile := pidFileFromConfig(pc.Lifecycle)

	// Check if running
	data, err := os.ReadFile(pidFile)
	if os.IsNotExist(err) {
		fmt.Printf("Runtime %s: not running (no PID file)\n", provider)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return fmt.Errorf("invalid PID in file: %w", err)
	}

	if !checkProcessAlive(pid) {
		fmt.Printf("Runtime %s: not running (process dead, PID: %d)\n", provider, pid)
		os.Remove(pidFile)
		return nil
	}

	// Create a minimal RuntimeConfig so RuntimeProcess can stop via Stop()
	runtimeProc := llm.NewRuntimeProcess(runtimePIDConfig(pc, pidFile))
	if err := runtimeProc.Stop(ctx); err != nil {
		// The error from Stop() is usually about the process not responding,
		// but it still tried to stop it. Consider it stopped if the process is dead.
		if pid, rerr := readPID(pidFile); rerr == nil && checkProcessAlive(pid) {
			return fmt.Errorf("failed to stop runtime (process %d still running): %w", pid, err)
		}
	}

	fmt.Printf("Runtime %s stopped\n", provider)
	return nil
}

// runRuntimeRestart restarts the runtime.
func runRuntimeRestart(ctx context.Context, provider string) error {
	if err := runRuntimeStop(ctx, provider); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: stop failed: %v\n", err)
	}
	time.Sleep(1 * time.Second)
	return runRuntimeStart(ctx, provider)
}

// runtimePIDConfig builds a minimal RuntimeConfig from a provider,
// suitable for Stop() calls without needing full validation (no model check).
func runtimePIDConfig(pc *llm.ProviderConfig, pidFile string) *llm.RuntimeConfig {
	return &llm.RuntimeConfig{
		PIDFile: pidFile,
	}
}
