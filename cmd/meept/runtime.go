package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/caimlas/meept/internal/agents"
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
	var format string

	cmd := &cobra.Command{
		Use:   "status [provider]",
		Short: "Show local LLM runtime status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				provider = args[0]
			}
			return runRuntimeStatusFormatted(cmd.Context(), provider, format)
		},
	}

	cmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")

	return cmd
}

func newRuntimeStartCmd() *cobra.Command {
	var provider string
	var wait bool

	cmd := &cobra.Command{
		Use:   "start [provider]",
		Short: "Start the local LLM runtime",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				provider = args[0]
			}
			return runRuntimeStart(cmd.Context(), provider, wait)
		},
	}

	cmd.Flags().BoolVar(&wait, "wait", true, "Wait for runtime to become healthy before returning")

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

// readPID reads a PID from the given file path. Accepts both the current
// JSON runtime-pidfile format and legacy bare-int files (delegates to
// llm.ParsePIDFile so CLI and daemon never drift on the format).
func readPID(path string) (int, error) {
	return llm.ParsePIDFile(path)
}

// runRuntimeStatusFormatted shows the current runtime status in the requested format.
func runRuntimeStatusFormatted(ctx context.Context, provider, format string) error {
	if provider == "" {
		provider = "local"
	}

	cfg, pc, err := loadRuntimeConfig(provider)
	if err != nil {
		return err
	}

	pidFile := pidFileFromConfig(pc.Lifecycle)

	// Compute lifecycle-derived metadata. Best-effort: on failure, fields are
	// zero-valued and the status command still succeeds.
	processGroup := computeProcessGroup(pc)
	inUseModels := computeInUseModels(cfg, pc, provider)
	wouldStart := len(inUseModels) > 0 && pc.Lifecycle.AutoStart

	pid, err := readPID(pidFile)
	if os.IsNotExist(err) {
		if format == "json" {
			return jsonOutput(map[string]any{
				"provider":      provider,
				"running":       false,
				"pid":           nil,
				"process_group": processGroup,
				"in_use_models": inUseModels,
				"would_start":   wouldStart,
			})
		}
		fmt.Printf("Runtime %s: not running (no PID file)\n", provider)
		printStatusExtras(processGroup, inUseModels, wouldStart)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	running := checkProcessAlive(pid)
	if !running {
		if format == "json" {
			return jsonOutput(map[string]any{
				"provider":      provider,
				"running":       false,
				"pid":           pid,
				"note":          "process dead, stale PID file",
				"process_group": processGroup,
				"in_use_models": inUseModels,
				"would_start":   wouldStart,
			})
		}
		fmt.Printf("Runtime %s: not running (process dead, PID: %d)\n", provider, pid)
		printStatusExtras(processGroup, inUseModels, wouldStart)
		return nil
	}

	baseURL := pc.Options.BaseURL
	healthEndpoint := pc.Lifecycle.HealthCheck.Endpoint
	if healthEndpoint == "" {
		healthEndpoint = "/health"
	}

	if format == "json" {
		return jsonOutput(map[string]any{
			"provider":        provider,
			"running":         true,
			"pid":             pid,
			"health_endpoint": baseURL + healthEndpoint,
			"pid_file":        pidFile,
			"process_group":   processGroup,
			"in_use_models":   inUseModels,
			"would_start":     wouldStart,
		})
	}

	fmt.Printf("Runtime %s: running (PID: %d)\n", provider, pid)
	fmt.Printf("  Health endpoint:  %s%s\n", baseURL, healthEndpoint)
	fmt.Printf("  PID file:         %s\n", pidFile)
	printStatusExtras(processGroup, inUseModels, wouldStart)
	return nil
}

// printStatusExtras prints process_group, in_use_models, and would_start in text mode.
func printStatusExtras(processGroup string, inUseModels []string, wouldStart bool) {
	if processGroup != "" {
		fmt.Printf("  Process group:    %s\n", processGroup)
	}
	if len(inUseModels) > 0 {
		fmt.Printf("  In-use models:    %s\n", strings.Join(inUseModels, ", "))
	} else {
		fmt.Printf("  In-use models:    (none)\n")
	}
	fmt.Printf("  Would start:      %v\n", wouldStart)
}

// computeProcessGroup returns the endpoint key for the provider's runtime.
func computeProcessGroup(pc *llm.ProviderConfig) string {
	if pc == nil || pc.Lifecycle == nil {
		return ""
	}
	return llm.ComputeEndpointKey(pc.Lifecycle.Runtime, pc.Options.BaseURL)
}

// computeInUseModels computes which of the provider's models appear in the
// daemon-wide in-use set. Best-effort: returns nil on any error.
func computeInUseModels(cfg *llm.ProvidersConfig, pc *llm.ProviderConfig, providerID string) []string {
	if cfg == nil || pc == nil {
		return nil
	}
	// Build AgentModelRef list from default-loaded agent definitions.
	agentRefs := loadAgentRefsCLI()
	slots := llm.ModelSlots{
		Model:           cfg.Model,
		SmallModel:      cfg.SmallModel,
		ClassifierModel: cfg.ClassifierModel,
		SummarizerModel: cfg.SummarizerModel,
	}
	inUse := llm.BuildModelsInUse(agentRefs, slots, cfg.ModelAliases, cfg.DisabledProviders)
	if len(inUse) == 0 {
		return nil
	}
	// Filter to just this provider's models.
	var out []string
	keys := make([]string, 0, len(pc.Models))
	for k := range pc.Models {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, isInUse := inUse[providerID+"/"+k]; isInUse {
			out = append(out, k)
		}
	}
	return out
}

// jsonOutput writes data as indented JSON to stdout.
func jsonOutput(data any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

// runRuntimeStart starts the runtime.
func runRuntimeStart(ctx context.Context, provider string, wait bool) error {
	if provider == "" {
		provider = "local"
	}
	_, pc, err := loadRuntimeConfig(provider)
	if err != nil {
		return err
	}

	rtCfg, err := llm.ValidateAndNormalize(*pc.Lifecycle)
	if err != nil {
		return fmt.Errorf("invalid lifecycle config: %w", err)
	}

	// Resolve the provider's base URL onto the runtime config. The runtime
	// process probes rtCfg.BaseURL before spawning to refuse a duplicate
	// spawn onto an occupied endpoint port (see RuntimeProcess.Start); an
	// empty BaseURL silently skips that pre-check.
	rtCfg.BaseURL = pc.Options.BaseURL

	pidFile := rtCfg.PIDFile

	// Check if already running. The pidfile is JSON ({"pid":N,"token":"..."}),
	// so parse it through llm.ParsePIDFile rather than a bare int parse: a
	// failed Atoi here previously misread every live pidfile as stale,
	// removed it, and spawned a duplicate onto the occupied port.
	if pid, err := readPID(pidFile); err == nil {
		if checkProcessAlive(pid) {
			return fmt.Errorf("runtime %s is already running (PID: %d)", provider, pid)
		}
		// Stale PID file
		os.Remove(pidFile)
	}

	// Spawn the process
	runtimeProc := llm.NewRuntimeProcess(rtCfg)
	if err := runtimeProc.Start(ctx, io.Discard, io.Discard); err != nil {
		return fmt.Errorf("failed to start runtime: %w", err)
	}

	fmt.Printf("Runtime %s started (PID: %d)\n", provider, runtimeProc.PID())

	if wait {
		baseURL := pc.Options.BaseURL
		hc := llm.NewHealthChecker(rtCfg, baseURL)
		hc.Start(ctx)
		defer hc.Stop()

		fmt.Printf("Waiting for runtime to become healthy")
		if err := hc.WaitForHealthy(ctx, rtCfg.SpawnTimeout); err != nil {
			fmt.Printf(" - timeout\n")
			return fmt.Errorf("runtime did not become healthy within %v: %w", rtCfg.SpawnTimeout, err)
		}
		fmt.Printf(" - healthy\n")
	}

	return nil
}

// runRuntimeStop stops the runtime.
func runRuntimeStop(ctx context.Context, provider string) error {
	if provider == "" {
		provider = "local"
	}
	_, pc, err := loadRuntimeConfig(provider)
	if err != nil {
		return err
	}

	pidFile := pidFileFromConfig(pc.Lifecycle)

	// Check if running. JSON pidfile: parse via llm.ParsePIDFile, never Atoi
	// (a bare int parse fails on the JSON format and the documented stop
	// command would then be unable to stop anything).
	pid, err := readPID(pidFile)
	if os.IsNotExist(err) {
		fmt.Printf("Runtime %s: not running (no PID file)\n", provider)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	if !checkProcessAlive(pid) {
		fmt.Printf("Runtime %s: not running (process dead, PID: %d)\n", provider, pid)
		os.Remove(pidFile)
		return nil
	}

	// StopAsOperator, not Stop: this CLI process never spawned the runtime
	// (the daemon did), so Stop would refuse with ErrRuntimeNotOwned and the
	// documented command could never stop anything. An explicit operator
	// request is exactly the StopAsOperator case.
	runtimeProc := llm.NewRuntimeProcess(runtimePIDConfig(pc, pidFile))
	stopErr := runtimeProc.StopAsOperator(ctx)

	// Report truthfully: claim "stopped" only once the pid is verifiably
	// gone. A SIGTERM that was accepted but did not take the process down
	// (unhandled signal, stuck in a syscall, wrong pid file) must surface as
	// an error naming the pid, never as a false success.
	if checkProcessAlive(pid) {
		if stopErr != nil {
			return fmt.Errorf("failed to stop runtime %s: process %d is still running: %w", provider, pid, stopErr)
		}
		return fmt.Errorf("failed to stop runtime %s: process %d is still running", provider, pid)
	}

	fmt.Printf("Runtime %s stopped (PID: %d)\n", provider, pid)
	return nil
}

// runRuntimeRestart restarts the runtime.
func runRuntimeRestart(ctx context.Context, provider string) error {
	if provider == "" {
		provider = "local"
	}
	if err := runRuntimeStop(ctx, provider); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: stop failed: %v\n", err)
	}
	time.Sleep(1 * time.Second)
	return runRuntimeStart(ctx, provider, true)
}

// runtimePIDConfig builds a minimal RuntimeConfig from a provider,
// suitable for Stop() calls without needing full validation (no model check).
func runtimePIDConfig(pc *llm.ProviderConfig, pidFile string) *llm.RuntimeConfig {
	return &llm.RuntimeConfig{
		PIDFile: pidFile,
	}
}

// loadAgentRefsCLI loads default agent definitions and converts them to the
// minimal AgentModelRef form used by BuildModelsInUse. Best-effort: returns
// nil on any error.
func loadAgentRefsCLI() []llm.AgentModelRef {
	disc := agents.NewDiscovery(agents.WithBundledPath("config/agents"))
	defs, err := disc.Discover()
	if err != nil || len(defs) == 0 {
		return nil
	}
	out := make([]llm.AgentModelRef, 0, len(defs))
	for _, def := range defs {
		if def == nil {
			continue
		}
		out = append(out, llm.AgentModelRef{Model: def.Model, Enabled: def.IsEnabled()})
	}
	return out
}
