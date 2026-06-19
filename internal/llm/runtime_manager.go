package llm

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"sort"
	"sync"
	"time"
)

// MetricsRecorder records runtime-related metrics.
type MetricsRecorder interface {
	RecordRuntimeHealth(providerID string, healthy bool)
	RecordRuntimeSpawn(providerID string, duration time.Duration, success bool)
	RecordRuntimeRestart(providerID string, attempt int, success bool)
}

// restartState tracks auto-restart attempts for an endpoint.
type restartState struct {
	attempts    int
	lastRestart time.Time
	lastFailure time.Time
}

// endpointProcess bundles together the shared process and associated state for
// a single (runtime, host, port) endpoint.
type endpointProcess struct {
	cfg        *RuntimeConfig
	proc       *RuntimeProcess
	hc         *HealthChecker
	rs         *restartState
	procLogger *ProcessLogger
	// providerID -> set of modelKeys registered against this endpoint.
	providers map[string]map[string]struct{}
}

// RuntimeManager manages local LLM runtime lifecycle. Processes are shared by
// endpoint key (runtime:host:port); each registered provider contributes models
// and per-model loggers but the subprocess is spawned/stopped once per endpoint.
type RuntimeManager struct {
	mu               sync.Mutex
	configs          map[string]*RuntimeConfig
	endpoints        map[string]*endpointProcess
	providerEndpoint map[string]string                  // providerID -> endpointKey
	modelLoggers     map[string]map[string]*ModelLogger // endpointKey -> modelKey -> logger
	logger           *slog.Logger
	metrics          MetricsRecorder
	inUseModels      map[string]struct{}
	shutdown         bool
}

// NewRuntimeManager creates a new manager.
func NewRuntimeManager(logger *slog.Logger) *RuntimeManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &RuntimeManager{
		configs:          make(map[string]*RuntimeConfig),
		endpoints:        make(map[string]*endpointProcess),
		providerEndpoint: make(map[string]string),
		modelLoggers:     make(map[string]map[string]*ModelLogger),
		logger:           logger,
	}
}

// SetMetricsRecorder sets the metrics recorder for runtime events.
func (m *RuntimeManager) SetMetricsRecorder(rec MetricsRecorder) {
	if rec == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metrics = rec
}

// SetModelsInUse sets the in-use set used by StartAll to gate spawning.
func (m *RuntimeManager) SetModelsInUse(set map[string]struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inUseModels = set
}

// RegisterConfig registers a runtime configuration. If the endpoint key
// (cfg.EndpointKey or derived from baseURL) already exists, this provider's
// models are merged into the existing process; spawn_command on the first
// registration wins. Per-model loggers are opened and a `register` event is
// logged for each model key.
//
// Model identity resolution: the in-use gate and per-model loggers key on the
// provider's real model IDs (cfg.ModelKeys when populated by the caller).
// When cfg.ModelKeys is empty (legacy callers), it falls back to the
// cfg.ModelPaths map keys — for legacy single-model configs synthesized under
// the "default" key, this means the gate will look for "<provider>/default"
// unless the daemon pre-populates ModelKeys from the provider's models map.
func (m *RuntimeManager) RegisterConfig(providerID string, cfg *RuntimeConfig, baseURL string) error {
	if cfg == nil {
		return fmt.Errorf("nil runtime config for provider %s", providerID)
	}
	endpointKey := cfg.EndpointKey
	if endpointKey == "" {
		endpointKey = ComputeEndpointKey(string(cfg.Type), baseURL)
		cfg.EndpointKey = endpointKey
	}

	// Resolve the authoritative model-key list. Prefer cfg.ModelKeys (set by
	// the daemon from the provider's models map). Fall back to ModelPaths keys
	// for legacy callers. Without this, a legacy single-model config (whose
	// ModelPaths only has the synthetic "default" key) would never match the
	// in-use set built from the real "provider/<model-id>" references.
	modelKeys := cfg.ModelKeys
	if len(modelKeys) == 0 {
		modelKeys = make([]string, 0, len(cfg.ModelPaths))
		for k := range cfg.ModelPaths {
			modelKeys = append(modelKeys, k)
		}
		sort.Strings(modelKeys)
	}
	cfg.ModelKeys = modelKeys

	m.mu.Lock()
	// Always store the per-provider config and endpoint mapping.
	m.configs[providerID] = cfg
	m.providerEndpoint[providerID] = endpointKey

	// Resolve host/port for process logger naming.
	host, port := hostPortFromBaseURL(baseURL)

	// Open per-model loggers for this provider's declared models.
	loggerMap, ok := m.modelLoggers[endpointKey]
	if !ok {
		loggerMap = make(map[string]*ModelLogger)
		m.modelLoggers[endpointKey] = loggerMap
	}
	m.mu.Unlock()

	for _, modelKey := range modelKeys {
		// Don't re-open an existing logger for the same endpoint+model.
		m.mu.Lock()
		_, exists := loggerMap[modelKey]
		m.mu.Unlock()
		if exists {
			continue
		}
		ml, mlErr := OpenModelLogger(providerID, modelKey)
		if mlErr != nil {
			m.logger.Warn("Failed to open per-model log; falling back to stderr",
				"provider", providerID, "model", modelKey, "error", mlErr)
		}
		ml.Log("register", slog.String("model_path", cfg.ModelPaths[modelKey]))
		m.mu.Lock()
		loggerMap[modelKey] = ml
		m.mu.Unlock()
	}

	m.mu.Lock()
	ep, exists := m.endpoints[endpointKey]
	if !exists {
		// First registration for this endpoint: create process + health checker.
		proc := NewRuntimeProcess(cfg)
		hc := NewHealthChecker(cfg, baseURL)
		ep = &endpointProcess{
			cfg:       cfg,
			proc:      proc,
			hc:        hc,
			providers: map[string]map[string]struct{}{providerID: setOf(modelKeys)},
		}
		m.endpoints[endpointKey] = ep
		if cfg.RestartEnabled {
			ep.rs = &restartState{}
		}
		// Wire health callback for auto-restart + per-model fan-out.
		hc.OnHealthChange(m.makeHealthCallback(endpointKey))

		m.logger.Info("Registered runtime config",
			"provider", providerID,
			"endpoint_key", endpointKey,
			"runtime", cfg.Type,
			"auto_restart", cfg.RestartEnabled)
		m.mu.Unlock()
		return nil
	}

	// Merge into existing endpoint.
	m.mergeProviderLocked(ep, providerID, cfg, modelKeys)
	needProcessLogger := ep.procLogger == nil
	m.mu.Unlock()

	// Open process logger outside the lock (file I/O).
	if needProcessLogger {
		pl, plErr := OpenProcessLogger(host, port)
		if plErr != nil {
			m.logger.Warn("Failed to open per-process log; falling back to stderr",
				"endpoint_key", endpointKey, "error", plErr)
		}
		if pl != nil {
			var dupLogger *ProcessLogger
			m.mu.Lock()
			if ep.procLogger == nil {
				ep.procLogger = pl
			} else {
				dupLogger = pl
			}
			m.mu.Unlock()
			if dupLogger != nil {
				_ = dupLogger.Close()
			}
		}
	}

	m.logger.Info("Merged runtime config into existing endpoint",
		"provider", providerID,
		"endpoint_key", endpointKey,
		"runtime", cfg.Type)
	return nil
}

func (m *RuntimeManager) mergeProviderLocked(ep *endpointProcess, providerID string, cfg *RuntimeConfig, modelKeys []string) {
	// Warn if spawn_command differs from the stored one (first wins).
	if len(ep.cfg.SpawnCommand) > 0 && len(cfg.SpawnCommand) > 0 && !sliceEqual(ep.cfg.SpawnCommand, cfg.SpawnCommand) {
		m.logger.Warn("Conflicting spawn_command for shared endpoint; keeping the first",
			"provider", providerID,
			"existing", ep.cfg.SpawnCommand,
			"new", cfg.SpawnCommand)
	}
	// Debug when a later provider's pid_file differs from the first
	// registration's (the first one wins; this log helps operators diagnose
	// why their pid_file setting appears to be ignored).
	if ep.cfg.PIDFile != "" && cfg.PIDFile != "" && ep.cfg.PIDFile != cfg.PIDFile {
		m.logger.Debug("Conflicting pid_file for shared endpoint; keeping the first",
			"provider", providerID,
			"existing", ep.cfg.PIDFile,
			"new", cfg.PIDFile)
	}

	if _, ok := ep.providers[providerID]; !ok {
		ep.providers[providerID] = make(map[string]struct{})
	}
	for _, k := range modelKeys {
		ep.providers[providerID][k] = struct{}{}
	}
}

// makeHealthCallback returns a HealthChangeCallback that fans out health
// transitions to per-model loggers on the endpoint and triggers auto-restart
// when enabled.
func (m *RuntimeManager) makeHealthCallback(endpointKey string) HealthChangeCallback {
	return func(healthy bool) {
		m.logToEndpoint(endpointKey, "health_transition", slog.Bool("healthy", healthy))
		if !healthy {
			go m.attemptAutoRestart(endpointKey)
			return
		}
		// Reset failure count after sustained healthy period.
		m.mu.Lock()
		defer m.mu.Unlock()
		ep, ok := m.endpoints[endpointKey]
		if !ok || ep.rs == nil || ep.cfg == nil {
			return
		}
		if !ep.rs.lastFailure.IsZero() && time.Since(ep.rs.lastFailure) > ep.cfg.RestartResetAfter {
			ep.rs.attempts = 0
		}
	}
}

// logToEndpoint fans out an event to every per-model logger on the endpoint.
func (m *RuntimeManager) logToEndpoint(endpointKey, event string, kv ...any) {
	m.mu.Lock()
	loggerMap, ok := m.modelLoggers[endpointKey]
	if !ok {
		m.mu.Unlock()
		return
	}
	loggers := make([]*ModelLogger, 0, len(loggerMap))
	for _, ml := range loggerMap {
		loggers = append(loggers, ml)
	}
	m.mu.Unlock()
	for _, ml := range loggers {
		ml.Log(event, kv...)
	}
}

// StartAll starts all registered runtimes with auto_start=true whose endpoint
// has at least one model in the in-use set.
func (m *RuntimeManager) StartAll(ctx context.Context) error {
	type startItem struct {
		endpointKey string
		cfg         *RuntimeConfig
		proc        *RuntimeProcess
		hc          *HealthChecker
		ep          *endpointProcess
	}

	m.mu.Lock()
	var items []startItem
	for endpointKey, ep := range m.endpoints {
		cfg := ep.cfg
		if !cfg.AutoStart {
			continue
		}
		if !m.endpointHasInUseLocked(endpointKey) {
			m.logger.Debug("Skipping runtime start: no model in use", "endpoint_key", endpointKey)
			continue
		}
		items = append(items, startItem{
			endpointKey: endpointKey,
			cfg:         cfg,
			proc:        ep.proc,
			hc:          ep.hc,
			ep:          ep,
		})
	}
	m.mu.Unlock()

	for _, item := range items {
		m.logger.Info("Starting runtime", "endpoint_key", item.endpointKey)
		m.logToEndpoint(item.endpointKey, "spawn_attempt")

		var stdout, stderr io.Writer = nil, nil
		m.mu.Lock()
		procLogger := item.ep.procLogger
		m.mu.Unlock()
		if procLogger == nil {
			host, port := hostPortFromBaseURL(item.hc.baseURL)
			pl, plErr := OpenProcessLogger(host, port)
			if plErr != nil {
				m.logger.Warn("Failed to open per-process log; falling back to stderr",
					"endpoint_key", item.endpointKey, "error", plErr)
			}
			if pl != nil {
				var dupLogger *ProcessLogger
				m.mu.Lock()
				if item.ep.procLogger == nil {
					item.ep.procLogger = pl
				} else {
					dupLogger = pl
				}
				procLogger = item.ep.procLogger
				m.mu.Unlock()
				if dupLogger != nil {
					_ = dupLogger.Close()
				}
			}
		}
		if procLogger != nil {
			// Truncate only when we are about to spawn a fresh subprocess.
			// If the process is already running (PID file present + alive),
			// Start will no-op; in that case preserve the existing log.
			if !item.proc.AlreadyRunning() {
				procLogger.Truncate()
			}
			stdout = procLogger.Stdout()
			stderr = procLogger.Stderr()
		}

		start := time.Now()
		if err := item.proc.Start(ctx, stdout, stderr); err != nil {
			m.logToEndpoint(item.endpointKey, "spawn_failure", slog.String("error", err.Error()))
			m.recordSpawn(item.endpointKey, time.Since(start), false)
			return fmt.Errorf("failed to start runtime %s: %w", item.endpointKey, err)
		}
		m.recordSpawn(item.endpointKey, time.Since(start), true)
		m.logToEndpoint(item.endpointKey, "spawn_success", slog.Int("pid", item.proc.PID()))

		// Start health checker.
		item.hc.Start(ctx)

		// Wait for healthy.
		if err := item.hc.WaitForHealthy(ctx, item.cfg.SpawnTimeout); err != nil {
			item.proc.Stop(ctx)
			m.logger.Error("Runtime did not become healthy", "endpoint_key", item.endpointKey, "error", err)
			return fmt.Errorf("runtime %s did not become healthy: %w", item.endpointKey, err)
		}

		m.logger.Info("Runtime started and healthy", "endpoint_key", item.endpointKey)
	}

	return nil
}

// StopAll stops all running runtimes that have auto_stop_on_exit=true.
// Processes are keyed by endpoint so the shared subprocess is stopped once.
func (m *RuntimeManager) StopAll(ctx context.Context) error {
	type stopItem struct {
		endpointKey string
		autoStop    bool
		proc        *RuntimeProcess
		hc          *HealthChecker
		ep          *endpointProcess
	}

	m.mu.Lock()
	m.shutdown = true
	items := make([]stopItem, 0, len(m.endpoints))
	for endpointKey, ep := range m.endpoints {
		items = append(items, stopItem{
			endpointKey: endpointKey,
			autoStop:    ep.cfg.AutoStop,
			proc:        ep.proc,
			hc:          ep.hc,
			ep:          ep,
		})
	}
	m.mu.Unlock()

	for _, item := range items {
		if !item.autoStop {
			m.logger.Debug("Skipping runtime stop (auto_stop disabled)", "endpoint_key", item.endpointKey)
			continue
		}
		m.logger.Info("Stopping runtime", "endpoint_key", item.endpointKey)
		if err := item.proc.Stop(ctx); err != nil {
			m.logger.Error("Failed to stop runtime", "endpoint_key", item.endpointKey, "error", err)
		}
		m.logToEndpoint(item.endpointKey, "stop")
	}

	// Stop health checkers.
	for _, item := range items {
		if item.hc != nil {
			item.hc.Stop()
			m.logger.Debug("Health checker stopped", "endpoint_key", item.endpointKey)
		}
	}

	// Close per-endpoint process loggers and per-model loggers.
	// Collect under lock, close without the lock to satisfy the mutex-scope
	// rule (Close may perform I/O).
	m.mu.Lock()
	var procLoggers []*ProcessLogger
	for _, ep := range m.endpoints {
		if ep.procLogger != nil {
			procLoggers = append(procLoggers, ep.procLogger)
			ep.procLogger = nil
		}
	}
	modelLoggersToClose := m.modelLoggers
	m.modelLoggers = make(map[string]map[string]*ModelLogger)
	m.mu.Unlock()

	for _, pl := range procLoggers {
		_ = pl.Close()
	}
	for _, lm := range modelLoggersToClose {
		for _, ml := range lm {
			_ = ml.Close()
		}
	}

	return nil
}

// RuntimeStatus describes the current state of a managed runtime.
type RuntimeStatus struct {
	ProviderID   string   `json:"provider_id"`
	Runtime      string   `json:"runtime"`
	Healthy      bool     `json:"healthy"`
	Running      bool     `json:"running"`
	PID          int      `json:"pid,omitempty"`
	ModelPath    string   `json:"model_path"`
	ProcessGroup string   `json:"process_group,omitempty"`
	InUseModels  []string `json:"in_use_models,omitempty"`
	WouldStart   bool     `json:"would_start"`
}

// Status returns the status of all registered runtimes.
func (m *RuntimeManager) Status() []RuntimeStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	var statuses []RuntimeStatus
	for providerID, cfg := range m.configs {
		status := RuntimeStatus{
			ProviderID: providerID,
			Runtime:    string(cfg.Type),
			ModelPath:  cfg.ModelPath,
		}
		endpointKey := cfg.EndpointKey
		if endpointKey == "" {
			endpointKey = m.providerEndpoint[providerID]
		}
		status.ProcessGroup = endpointKey
		if ep, ok := m.endpoints[endpointKey]; ok {
			status.Running = ep.proc.IsRunning()
			status.PID = ep.proc.PID()
			if ep.hc != nil {
				status.Healthy = ep.hc.IsHealthy()
			}
		}
		status.InUseModels = m.providerInUseModelsLocked(providerID, cfg)
		status.WouldStart = len(status.InUseModels) > 0 && cfg.AutoStart
		statuses = append(statuses, status)
	}
	return statuses
}

// StatusForProvider returns the status of a specific provider.
func (m *RuntimeManager) StatusForProvider(providerID string) (RuntimeStatus, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, ok := m.configs[providerID]
	if !ok {
		return RuntimeStatus{}, false
	}

	status := RuntimeStatus{
		ProviderID: providerID,
		Runtime:    string(cfg.Type),
		ModelPath:  cfg.ModelPath,
	}
	endpointKey := cfg.EndpointKey
	if endpointKey == "" {
		endpointKey = m.providerEndpoint[providerID]
	}
	status.ProcessGroup = endpointKey
	if ep, ok := m.endpoints[endpointKey]; ok {
		status.Running = ep.proc.IsRunning()
		status.PID = ep.proc.PID()
		if ep.hc != nil {
			status.Healthy = ep.hc.IsHealthy()
		}
	}
	status.InUseModels = m.providerInUseModelsLocked(providerID, cfg)
	status.WouldStart = len(status.InUseModels) > 0 && cfg.AutoStart
	return status, true
}

// StartProvider starts a specific provider's runtime (and, since the process
// is shared, all models on the same endpoint).
func (m *RuntimeManager) StartProvider(ctx context.Context, providerID string) error {
	m.mu.Lock()
	cfg, ok := m.configs[providerID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("provider %s not registered", providerID)
	}
	endpointKey := cfg.EndpointKey
	if endpointKey == "" {
		endpointKey = m.providerEndpoint[providerID]
	}
	ep, ok := m.endpoints[endpointKey]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no endpoint process for provider %s", providerID)
	}

	m.logger.Info("Starting runtime", "provider", providerID, "endpoint_key", endpointKey)
	m.logToEndpoint(endpointKey, "spawn_attempt", slog.String("provider", providerID))
	start := time.Now()

	var stdout, stderr io.Writer = nil, nil
	m.mu.Lock()
	procLogger := ep.procLogger
	m.mu.Unlock()
	if procLogger == nil {
		host, port := hostPortFromBaseURL(ep.hc.baseURL)
		pl, plErr := OpenProcessLogger(host, port)
		if plErr != nil {
			m.logger.Warn("Failed to open per-process log; falling back to stderr",
				"endpoint_key", endpointKey, "error", plErr)
		}
		if pl != nil {
			var dupLogger *ProcessLogger
			m.mu.Lock()
			if ep.procLogger == nil {
				ep.procLogger = pl
			} else {
				dupLogger = pl
			}
			procLogger = ep.procLogger
			m.mu.Unlock()
			if dupLogger != nil {
				_ = dupLogger.Close()
			}
		}
	}
	if procLogger != nil {
		// Truncate only when actually spawning a fresh subprocess; preserve
		// the log of an already-running process.
		if !ep.proc.AlreadyRunning() {
			procLogger.Truncate()
		}
		stdout = procLogger.Stdout()
		stderr = procLogger.Stderr()
	}

	// Re-check shutdown under the lock right before spawning. attemptAutoRestart
	// checks m.shutdown at its top, but releases m.mu before calling
	// RestartProvider → StartProvider → ep.proc.Start; if StopAll ran in that
	// window it would set m.shutdown=true and we must not spawn a subprocess
	// that survives shutdown (TOCTOU found by TestShutdownBlocksAutoRestart).
	m.mu.Lock()
	if m.shutdown {
		m.mu.Unlock()
		m.logger.Info("Skipping spawn; manager is shutting down",
			"provider", providerID, "endpoint_key", endpointKey)
		return fmt.Errorf("runtime manager is shutting down")
	}
	m.mu.Unlock()

	if err := ep.proc.Start(ctx, stdout, stderr); err != nil {
		m.logToEndpoint(endpointKey, "spawn_failure", slog.String("provider", providerID), slog.String("error", err.Error()))
		m.recordSpawn(providerID, time.Since(start), false)
		return fmt.Errorf("failed to start runtime %s: %w", providerID, err)
	}
	m.recordSpawn(providerID, time.Since(start), true)
	m.logToEndpoint(endpointKey, "spawn_success", slog.String("provider", providerID), slog.Int("pid", ep.proc.PID()))

	ep.hc.Start(ctx)

	if err := ep.hc.WaitForHealthy(ctx, cfg.SpawnTimeout); err != nil {
		ep.proc.Stop(ctx)
		return fmt.Errorf("runtime %s did not become healthy: %w", providerID, err)
	}

	m.logger.Info("Runtime started and healthy", "provider", providerID)
	return nil
}

// StopProvider stops a specific provider's runtime (and the shared subprocess).
func (m *RuntimeManager) StopProvider(ctx context.Context, providerID string) error {
	m.mu.Lock()
	cfg, ok := m.configs[providerID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("provider %s not registered", providerID)
	}
	endpointKey := cfg.EndpointKey
	if endpointKey == "" {
		endpointKey = m.providerEndpoint[providerID]
	}
	ep, ok := m.endpoints[endpointKey]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no endpoint process for provider %s", providerID)
	}

	m.logger.Info("Stopping runtime", "provider", providerID, "endpoint_key", endpointKey)
	if err := ep.proc.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop runtime %s: %w", providerID, err)
	}
	m.logToEndpoint(endpointKey, "stop")

	if ep.hc != nil {
		ep.hc.Stop()
	}
	return nil
}

// RestartProvider restarts a specific provider's runtime.
func (m *RuntimeManager) RestartProvider(ctx context.Context, providerID string) error {
	if err := m.StopProvider(ctx, providerID); err != nil {
		m.logger.Warn("Stop failed during restart", "provider", providerID, "error", err)
	}
	return m.StartProvider(ctx, providerID)
}

// GetHealthChecker returns the health checker for a provider.
func (m *RuntimeManager) GetHealthChecker(providerID string) (*HealthChecker, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, ok := m.configs[providerID]
	if !ok {
		return nil, false
	}
	endpointKey := cfg.EndpointKey
	if endpointKey == "" {
		endpointKey = m.providerEndpoint[providerID]
	}
	ep, ok := m.endpoints[endpointKey]
	if !ok {
		return nil, false
	}
	return ep.hc, true
}

func (m *RuntimeManager) attemptAutoRestart(endpointKey string) {
	m.mu.Lock()
	if m.shutdown {
		m.mu.Unlock()
		return
	}
	ep, ok := m.endpoints[endpointKey]
	if !ok || ep.cfg == nil || !ep.cfg.RestartEnabled {
		m.mu.Unlock()
		return
	}
	rs := ep.rs
	if rs == nil {
		m.mu.Unlock()
		return
	}
	if rs.attempts >= ep.cfg.RestartMaxAttempts {
		m.logger.Error("Auto-restart max attempts reached, giving up",
			"endpoint_key", endpointKey, "attempts", rs.attempts)
		m.mu.Unlock()
		return
	}
	if !rs.lastRestart.IsZero() && time.Since(rs.lastRestart) < ep.cfg.RestartCooldown {
		m.mu.Unlock()
		return
	}
	rs.attempts++
	rs.lastRestart = time.Now()
	rs.lastFailure = time.Now()
	providerID := m.endpointProviderLocked(endpointKey)
	m.mu.Unlock()

	m.logger.Warn("Attempting auto-restart",
		"endpoint_key", endpointKey,
		"attempt", rs.attempts,
		"max", ep.cfg.RestartMaxAttempts)
	m.logToEndpoint(endpointKey, "restart_attempt", slog.Int("attempt", rs.attempts))

	ctx, cancel := context.WithTimeout(context.Background(), ep.cfg.SpawnTimeout)
	defer cancel()

	if err := m.RestartProvider(ctx, providerID); err != nil {
		m.logger.Error("Auto-restart failed", "endpoint_key", endpointKey, "error", err)
		m.logToEndpoint(endpointKey, "restart_failed", slog.Int("attempt", rs.attempts), slog.String("error", err.Error()))
		m.recordRestart(providerID, rs.attempts, false)
		return
	}
	m.logger.Info("Auto-restart succeeded", "endpoint_key", endpointKey, "attempt", rs.attempts)
	m.logToEndpoint(endpointKey, "restart_success", slog.Int("attempt", rs.attempts))
	m.recordRestart(providerID, rs.attempts, true)
}

// endpointProviderLocked returns one providerID registered against endpointKey
// (the first in sorted order, for deterministic logging/metrics).
// Caller must hold m.mu.
func (m *RuntimeManager) endpointProviderLocked(endpointKey string) string {
	ids := make([]string, 0)
	for pid, ek := range m.providerEndpoint {
		if ek == endpointKey {
			ids = append(ids, pid)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return endpointKey
	}
	return ids[0]
}

// endpointHasInUseLocked reports whether any (providerID/modelKey) for the
// given endpoint appears in the in-use set. Caller must hold m.mu.
func (m *RuntimeManager) endpointHasInUseLocked(endpointKey string) bool {
	if len(m.inUseModels) == 0 {
		return true // No gate configured; start everything.
	}
	for providerID, ek := range m.providerEndpoint {
		if ek != endpointKey {
			continue
		}
		cfg, ok := m.configs[providerID]
		if !ok {
			continue
		}
		for _, modelKey := range cfg.ModelKeys {
			if _, isInUse := m.inUseModels[providerID+"/"+modelKey]; isInUse {
				return true
			}
		}
	}
	return false
}

// providerInUseModelsLocked returns the subset of the provider's model keys
// that appear in the in-use set. Caller must hold m.mu.
func (m *RuntimeManager) providerInUseModelsLocked(providerID string, cfg *RuntimeConfig) []string {
	if len(m.inUseModels) == 0 || len(cfg.ModelKeys) == 0 {
		return nil
	}
	out := make([]string, 0, len(cfg.ModelKeys))
	// cfg.ModelKeys is already sorted by RegisterConfig.
	for _, k := range cfg.ModelKeys {
		if _, isInUse := m.inUseModels[providerID+"/"+k]; isInUse {
			out = append(out, k)
		}
	}
	return out
}

// recordSpawn records a spawn metric.
//
// Callers that already hold m.mu must use recordSpawnLocked instead to avoid
// a self-deadlock (Go's sync.Mutex is non-reentrant).
func (m *RuntimeManager) recordSpawn(providerID string, duration time.Duration, success bool) {
	m.mu.Lock()
	rec := m.metrics
	m.mu.Unlock()
	if rec != nil {
		rec.RecordRuntimeSpawn(providerID, duration, success)
	}
}

func (m *RuntimeManager) recordRestart(providerID string, attempt int, success bool) {
	m.mu.Lock()
	rec := m.metrics
	m.mu.Unlock()
	if rec != nil {
		rec.RecordRuntimeRestart(providerID, attempt, success)
	}
}

// hostPortFromBaseURL extracts host and port from baseURL; defaults to
// 127.0.0.1:8080 when missing.
func hostPortFromBaseURL(baseURL string) (string, string) {
	host := "127.0.0.1"
	port := "8080"
	if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
		if h := u.Hostname(); h != "" {
			host = h
		}
		if p := u.Port(); p != "" {
			port = p
		}
	}
	return host, port
}

func setOf(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, i := range items {
		out[i] = struct{}{}
	}
	return out
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
