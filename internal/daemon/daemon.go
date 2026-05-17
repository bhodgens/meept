// Package daemon provides the main daemon lifecycle management.
package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/registry"
	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/scheduler"
	"github.com/caimlas/meept/internal/selfimprove"
	"github.com/caimlas/meept/internal/services"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/templates"
	"github.com/caimlas/meept/internal/worker"
	"github.com/caimlas/meept/pkg/models"
	"github.com/caimlas/meept/pkg/security"
)

// Daemon manages the meept daemon lifecycle.
//nolint:revive // stutter with package name is intentional for API clarity
type Daemon struct {
	config       *Config
	fullConfig   *config.Config // Full configuration loaded from file
	bus          *bus.MessageBus
	registry     *registry.Registry
	rpc          *rpc.Server
	httpServer   *http.Server
	components   *Components // Agent, tools, LLM, etc.
	metricsStore    *metrics.Store
	metricsCollector *metrics.Collector
	logger       *slog.Logger

	status    atomic.Value // stores models.DaemonStatus
	startTime time.Time
	pidFile   string
}

// Config holds daemon configuration.
type Config struct {
	SocketPath      string
	PIDFile         string
	StateDir        string
	ShutdownTimeout time.Duration
	LogLevel        slog.Level

	// Optional pre-loaded models config (used by tests to avoid loading real config)
	ModelsConfig *config.ModelsConfig

	// Security settings
	AllowedPaths                []string
	BlockedPaths                []string
	BlockFinancial              bool
	RequireConfirmationHigh     bool
	RequireConfirmationCritical bool
}

// DefaultConfig returns the default daemon configuration.
func DefaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	stateDir := filepath.Join(homeDir, ".meept")
	return &Config{
		SocketPath:      filepath.Join(stateDir, "meept.sock"),
		PIDFile:         filepath.Join(stateDir, "meept.pid"),
		StateDir:        stateDir,
		ShutdownTimeout: 10 * time.Second,
		LogLevel:        slog.LevelInfo,
	}
}

// New creates a new Daemon instance.
func New(cfg *Config) (daemon *Daemon, err error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Ensure state directory exists
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	// Set up logging
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	logger := slog.New(slog.NewTextHandler(os.Stderr, opts))

	// Load full configuration
	fullCfg, err := config.LoadDefault()
	if err != nil {
		logger.Warn("Failed to load config, using defaults", "error", err)
		fullCfg = config.DefaultConfig()
	}

	// Create message bus
	msgBus := bus.New(nil, logger)

	// Create registry
	reg := registry.New(logger)

	// Create RPC server (if enabled)
	var rpcServer *rpc.Server
	if fullCfg.Transport.RPC.Enabled {
		rpcServer = rpc.New(&rpc.Config{
			SocketPath: cfg.SocketPath,
		}, msgBus, logger)

		// Register proxy handlers that forward to bus subscribers
		proxy := rpc.NewProxyHandler(msgBus)
		proxy.RegisterProxyMethods(rpcServer)

		// Register security handlers (Go-native, high-performance)
		securityCfg := security.Config{
			AllowedPaths:                cfg.AllowedPaths,
			BlockedPaths:                cfg.BlockedPaths,
			BlockFinancial:              cfg.BlockFinancial,
			RequireConfirmationHigh:     cfg.RequireConfirmationHigh,
			RequireConfirmationCritical: cfg.RequireConfirmationCritical,
		}
		securityHandler := rpc.NewSecurityHandler(securityCfg)
		securityHandler.RegisterSecurityMethods(rpcServer)

		// Register dev handlers (model switching, testing, etc.)
		devHandler := rpc.NewDevHandler()
		devHandler.RegisterDevMethods(rpcServer)

		// Register RPC server as a component
		if err := reg.Register(rpcServer); err != nil {
			return nil, fmt.Errorf("failed to register RPC server: %w", err)
		}

		logger.Info("RPC transport enabled", "socket", cfg.SocketPath)
	} else {
		logger.Info("RPC transport disabled")
	}

	// Create agent components (LLM, tools, agent loop, chat handler)
	components, err := NewComponents(fullCfg, msgBus, logger, cfg.ModelsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create components: %w", err)
	}

	// Set default model info on RPC server for status reporting
	if rpcServer != nil && components.ModelsConfig != nil {
		rpcServer.SetDefaultModel(components.ModelsConfig.Model)
	}

	// Register skills handlers (both direct and bus-based)
	if rpcServer != nil && fullCfg.Skills.Enabled && components.SkillRegistry != nil {
		rpc.RegisterSkillsHandlers(rpcServer, components.SkillRegistry, components.SkillExecutor)
		logger.Info("Skills RPC handlers registered",
			"skill_count", components.SkillRegistry.Count(),
			"executor_available", components.SkillExecutor != nil,
		)
	}

	// Register template handlers
	if rpcServer != nil && components.TemplateRegistry != nil {
		rpc.RegisterTemplateHandlers(rpcServer, components.TemplateRegistry, components.SkillExecutor)
		logger.Info("Template RPC handlers registered",
			"template_count", components.TemplateRegistry.Count(),
		)
	}

	// Register self-improve handlers (native Go, calling Controller directly)
	if rpcServer != nil {
		siHandler := rpc.NewSelfImproveHandler(components.SelfImproveCtrl)
		siHandler.RegisterSelfImproveMethods(rpcServer)
		if components.SelfImproveCtrl != nil {
			logger.Info("Self-improve RPC handlers registered")
		}

		// Register cache handler (native Go)
		cacheHandler := rpc.NewCacheHandler(components.TokenCache, logger)
		cacheHandler.RegisterCacheMethods(rpcServer)
		if components.TokenCache != nil {
			logger.Info("Token cache RPC handlers registered")
		}

		// Register queue (steer/follow-up) handlers (native Go, calling AgentRegistry directly)
		queueHandler := rpc.NewQueueHandler(components.AgentRegistry)
		queueHandler.RegisterQueueMethods(rpcServer)
		if components.AgentRegistry != nil {
			logger.Info("Queue RPC handlers registered")
		}

		// Register scheduler RPC handlers (direct Go handlers override bus proxy)
		if components.Scheduler != nil {
			scheduler.RegisterRPCHandlers(rpcServer, components.Scheduler)
			logger.Info("Scheduler RPC handlers registered")
		}

		// Wire firewall stats getter (exposes context firewall metrics via RPC)
		if rpcServer != nil && components.AgentLoop != nil {
			rpcServer.FirewallStatsGetter = func() map[string]any {
				return components.AgentLoop.FirewallStats()
			}
			logger.Info("Firewall stats RPC getter registered")
		}
	}

	// Create config service for HTTP server
	configService, err := http.NewConfigService()
	if err != nil {
		logger.Warn("Failed to create config service", "error", err)
	}

	// Create daemon control for HTTP server
	daemonControl, err := NewDaemonControl()
	if err != nil {
		logger.Warn("Failed to create daemon control", "error", err)
	}

	// Create metrics store
	metricsStore, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  filepath.Join(cfg.StateDir, "metrics.db"),
		BatchSize:     100,
		FlushInterval: 10 * time.Second,
	})
	if err != nil {
		logger.Warn("Failed to create metrics store", "error", err)
	}

	// Wire metrics store to cache coordinator (created in NewComponents before metrics store existed)
	if metricsStore != nil && components != nil && components.TokenCache != nil {
		components.TokenCache.SetMetricsStore(metricsStore)
	}
	if metricsStore != nil && components != nil && components.ChatHandler != nil {
		components.ChatHandler.SetMetricsStore(metricsStore)
	}

	// Create metrics collector with getter functions for actual values
	var coll *metrics.Collector
	if metricsStore != nil && components != nil {
		coll = metrics.NewCollector(metricsStore, msgBus, &metrics.CollectorConfig{
			GetQueueDepth: func() int {
				ctx := context.Background()
				stats, err := components.Queue.Stats(ctx)
				if err != nil || stats.ByState == nil {
					return 0
				}
				return stats.ByState[queue.StatePending] + stats.ByState[queue.StateClaimed]
			},
			GetActiveAgents: func() int {
				stats := components.WorkerPool.GetStats()
				return stats.BusyWorkers
			},
		})
	} else if metricsStore != nil {
		// metricsStore exists but components is nil - create collector without getters
		coll = metrics.NewCollector(metricsStore, msgBus, nil)
	}

	// Create service registry with dependencies
	svcRegistry, err := services.NewRegistry(services.Config{
		Bus:             msgBus,
		AgentRegistry:   nilSafeAgentRegistry(components),
		Queue:           nilSafeQueue(components),
		MemoryManager:   nilSafeMemoryManager(components),
		TaskRegistry:    nilSafeTaskRegistry(components),
		SessionStore:    nilSafeSessionStore(components),
		WorkerPool:      nilSafeWorkerPool(components),
		SkillRegistry:   nilSafeSkillRegistry(components),
		SkillExecutor:   nilSafeSkillExecutor(components),
		TemplateRegistry: nilSafeTemplateRegistry(components),
		SelfImprove:     nilSafeSelfImprove(components),
		TokenCache:      nilSafeTokenCache(components),
		SecurityChecker: nilSafeSecurityChecker(components),
		Scheduler:        nilSafeScheduler(components),
		DaemonController: daemonControl,
		PidFile:          cfg.PIDFile,
		StateDir:         cfg.StateDir,
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create service registry: %w", err)
	}

	// Register daemon and model RPC handlers (after service registry is created)
	if rpcServer != nil {
		if svcRegistry.Daemon != nil {
			daemonHandler := NewDaemonRPCHandler(svcRegistry.Daemon)
			daemonHandler.RegisterDaemonMethods(rpcServer)
			logger.Info("Daemon RPC handlers registered")
		}
		if svcRegistry.Model != nil {
			modelHandler := services.NewModelRPCHandler(svcRegistry.Model)
			modelHandler.RegisterModelMethods(rpcServer)
			logger.Info("Model RPC handlers registered")
		}
	}

	// Create HTTP server (if enabled)
	var httpSrv *http.Server
	if fullCfg.Transport.HTTP.Enabled {
		if configService != nil && daemonControl != nil && metricsStore != nil {
			httpCfg := http.DefaultServerConfig()
			httpCfg.Addr = fullCfg.Transport.HTTP.Addr
			httpSrv = http.NewServer(httpCfg, configService, daemonControl, &metricsStoreWrapper{store: metricsStore}, svcRegistry, logger)
			// Wire firewall stats getter for HTTP endpoint
			if components.AgentLoop != nil {
				httpSrv.FirewallStatsGetter = func() map[string]any {
					return components.AgentLoop.FirewallStats()
				}
				logger.Info("Firewall stats HTTP getter registered")
			}
			logger.Info("HTTP server created", "addr", httpCfg.Addr)
		}
	} else {
		logger.Info("HTTP transport disabled")
	}

	// Ensure at least one transport is enabled
	if rpcServer == nil && httpSrv == nil {
		logger.Error("No transports enabled. Daemon cannot accept connections.")
		return nil, fmt.Errorf("at least one transport (rpc or http) must be enabled")
	}

	d := &Daemon{
		config:           cfg,
		fullConfig:       fullCfg,
		bus:              msgBus,
		registry:         reg,
		rpc:              rpcServer,
		httpServer:       httpSrv,
		components:       components,
		metricsStore:     metricsStore,
		metricsCollector: coll,
		logger:           logger,
		pidFile:          cfg.PIDFile,
	}
	d.status.Store(models.StatusStopped)
	return d, nil
}

// Run starts the daemon and blocks until shutdown.
func (d *Daemon) Run(ctx context.Context) error {
	d.logger.Info("daemon: starting", "pid", os.Getpid())
	d.status.Store(models.StatusStarting)
	d.startTime = time.Now()

	// Check for existing daemon
	if err := d.checkExisting(); err != nil {
		return err
	}

	// Write PID file
	if err := d.writePIDFile(); err != nil {
		return err
	}
	defer d.removePIDFile()

	// Start all registry components (RPC server, etc.)
	if err := d.registry.StartAll(ctx); err != nil {
		return fmt.Errorf("failed to start components: %w", err)
	}

	// Start agent components (chat handler, status handler, etc.)
	if d.components != nil {
		if err := d.components.Start(ctx); err != nil {
			return fmt.Errorf("failed to start agent components: %w", err)
		}
		d.logger.Info("daemon: agent components started",
			"tools", d.components.ToolRegistry.Count(),
			"llm_configured", d.components.LLMClient != nil,
		)
	}

	// Metrics collector is started automatically by NewCollector

	// Recover pending follow-ups from queue persistence.
	// This scans queued_followups for conversations that had messages pending
	// when the daemon last shut down, loads them, and publishes restore events
	// so that any connected TUI clients are notified.
	if d.components != nil && d.components.Queue != nil {
		if pq, ok := d.components.Queue.(*queue.PersistentQueue); ok {
			db := pq.DB()
			if db != nil {
				agent.RecoverPendingFollowUps(db, d.bus, d.logger)
			}
		}
	}

	// Recover stale tasks left in non-terminal states from a previous daemon run.
	// Marks them and their pending steps as failed so they don't accumulate forever.
	if d.components != nil && d.components.TaskRegistry != nil {
		if store := d.components.TaskRegistry.Store(); store != nil {
			count, err := store.RecoverStaleTasks()
			if err != nil {
				d.logger.Error("daemon: failed to recover stale tasks", "error", err)
			} else if count > 0 {
				d.logger.Info("daemon: recovered stale tasks from previous run", "count", count)
			}
		}
	}

	// Start HTTP server for menubar app
	if d.httpServer != nil {
		go func() {
			if err := d.httpServer.Start(ctx); err != nil {
				d.logger.Error("HTTP server error", "error", err)
			}
		}()
		d.logger.Info("HTTP server starting", "addr", ":8081")
	}

	d.status.Store(models.StatusRunning)
	d.logger.Info("daemon: running",
		"socket", d.config.SocketPath,
		"pid", os.Getpid(),
	)

	// Publish startup event
	msg, _ := models.NewBusMessage(models.MessageTypeEvent, "daemon", map[string]any{
		"event": "started",
		"pid":   os.Getpid(),
	})
	d.bus.Publish("daemon.started", msg)

	// Wait for shutdown signal or SIGHUP for reload
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	for {
		select {
		case sig := <-sigCh:
			if sig == syscall.SIGHUP {
				d.logger.Info("daemon: received SIGHUP, reloading configuration")
				if err := d.reloadConfig(ctx); err != nil {
					d.logger.Error("daemon: reload failed", "error", err)
				}
				continue // Continue waiting for signals
			}
			d.logger.Info("daemon: received signal", "signal", sig)
			// Graceful shutdown
			return d.shutdown()
		case <-ctx.Done():
			d.logger.Info("daemon: context cancelled")
			// Graceful shutdown
			return d.shutdown()
		}
	}
}

func (d *Daemon) shutdown() error {
	d.logger.Info("daemon: shutting down")
	d.status.Store(models.StatusStopping)

	// Publish shutdown event
	msg, _ := models.NewBusMessage(models.MessageTypeEvent, "daemon", map[string]any{
		"event": "stopping",
	})
	d.bus.Publish("daemon.stopping", msg)

	// Create shutdown context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), d.config.ShutdownTimeout)
	defer cancel()

	// Stop HTTP server
	if d.httpServer != nil {
		if err := d.httpServer.Shutdown(ctx); err != nil {
			d.logger.Error("HTTP server shutdown error", "error", err)
		}
	}

	// Stop metrics collector
	if d.metricsCollector != nil {
		d.metricsCollector.Shutdown()
	}
	if d.metricsStore != nil {
		d.metricsStore.Close()
	}

	// Stop agent components first
	if d.components != nil {
		if err := d.components.Stop(ctx); err != nil {
			d.logger.Error("daemon: agent component shutdown errors", "error", err)
		}
	}

	// Stop registry components (RPC server, etc.)
	if err := d.registry.StopAll(ctx); err != nil {
		d.logger.Error("daemon: shutdown errors", "error", err)
	}

	// Close message bus
	d.bus.Close()

	d.status.Store(models.StatusStopped)
	d.logger.Info("daemon: stopped")
	return nil
}

func (d *Daemon) checkExisting() error {
	data, err := os.ReadFile(d.pidFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		// Invalid PID file, remove it
		os.Remove(d.pidFile)
		return nil //nolint:nilerr // intentional: stale PID cleanup returns nil to allow startup
	}

	// Check if process is running
	proc, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(d.pidFile)
		return nil //nolint:nilerr // intentional: stale PID cleanup returns nil to allow startup
	}

	// Send signal 0 to check if process exists
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		os.Remove(d.pidFile)
		return nil //nolint:nilerr // intentional: stale PID cleanup returns nil to allow startup
	}

	return fmt.Errorf("daemon already running (PID %d)", pid)
}

func (d *Daemon) writePIDFile() error {
	return os.WriteFile(d.pidFile, []byte(strconv.Itoa(os.Getpid())), 0o600)
}

func (d *Daemon) removePIDFile() {
	os.Remove(d.pidFile)
}

// reloadConfig reloads configuration from disk and applies changes.
// Currently supports reloading MCP server configuration.
func (d *Daemon) reloadConfig(ctx context.Context) error {
	d.logger.Info("daemon: reloading configuration")

	// Reload full configuration from disk
	newCfg, err := config.LoadDefault()
	if err != nil {
		d.logger.Warn("daemon: failed to reload config, keeping existing", "error", err)
		// Continue with MCP reload using existing config
		newCfg = d.fullConfig
	} else {
		d.fullConfig = newCfg
	}

	// Reload MCP configuration if MCP is enabled
	if d.components != nil && d.components.MCPManager != nil && newCfg.MCP.Enabled {
		mcpCfg, err := config.LoadMCPConfig(newCfg.MCP.ConfigFile)
		if err != nil {
			d.logger.Error("daemon: failed to load MCP config during reload", "error", err)
			return err
		}

		if err := d.components.MCPManager.Reload(ctx, mcpCfg.Servers); err != nil {
			d.logger.Error("daemon: MCP reload failed", "error", err)
			return err
		}

		// Re-register MCP tools with the tool registry
		// First, unregister old MCP tools (those with "." in name indicating server.tool format)
		for _, name := range d.components.ToolRegistry.Names() {
			if hasDot(name) {
				if err := d.components.ToolRegistry.Unregister(name); err != nil {
					d.logger.Debug("daemon: failed to unregister old MCP tool", "name", name, "error", err)
				}
			}
		}

		// Register new MCP tools
		registerMCPTools(d.components.ToolRegistry, d.components.MCPManager, d.logger)
	}

	// Publish reload event
	msg, _ := models.NewBusMessage(models.MessageTypeEvent, "daemon", map[string]any{
		"event": "reloaded",
	})
	d.bus.Publish("daemon.reloaded", msg)

	d.logger.Info("daemon: configuration reloaded successfully")
	return nil
}

// hasDot checks if a string contains a dot.
func hasDot(s string) bool {
	for _, c := range s {
		if c == '.' {
			return true
		}
	}
	return false
}

// Status returns the current daemon status.
func (d *Daemon) Status() models.DaemonStatus {
	if v := d.status.Load(); v != nil {
		return v.(models.DaemonStatus)
	}
	return models.StatusStopped
}

// Info returns daemon information.
func (d *Daemon) Info() *models.DaemonInfo {
	return &models.DaemonInfo{
		PID:       os.Getpid(),
		Status:    d.Status(),
		StartTime: d.startTime,
		Version:   "0.2.0-go",
		Socket:    d.config.SocketPath,
	}
}

// Bus returns the message bus.
func (d *Daemon) Bus() *bus.MessageBus {
	return d.bus
}

// Registry returns the component registry.
func (d *Daemon) Registry() *registry.Registry {
	return d.registry
}

// RPC returns the RPC server.
func (d *Daemon) RPC() *rpc.Server {
	return d.rpc
}

// Components returns the agent components.
func (d *Daemon) Components() *Components {
	return d.components
}

// metricsStoreWrapper adapts *metrics.Store to implement the MetricsService interface.
type metricsStoreWrapper struct {
	store *metrics.Store
}

func (w *metricsStoreWrapper) GetLiveMetrics() (*metrics.LiveMetricsSnapshot, error) {
	return w.store.GetLiveMetrics()
}

func (w *metricsStoreWrapper) GetHistoricalMetrics(ctx context.Context, from, to time.Time, resolution string) ([]metrics.MetricPoint, error) {
	return w.store.GetHistoricalMetrics(from, to, resolution)
}

func (w *metricsStoreWrapper) SubscribeMetrics() (_ <-chan *metrics.LiveMetricsSnapshot, _ func()) {
	return w.store.SubscribeMetrics()
}

// nil-safe accessors extract fields from *Components, returning nil when
// components itself is nil.  This prevents typed-nil interface values from
// reaching service constructors (see CLAUDE.md typed-nil guard pattern).

func nilSafeAgentRegistry(c *Components) *agent.AgentRegistry {
	if c == nil {
		return nil
	}
	return c.AgentRegistry
}

func nilSafeQueue(c *Components) queue.Queue {
	if c == nil {
		return nil
	}
	return c.Queue
}

func nilSafeMemoryManager(c *Components) *memory.Manager {
	if c == nil {
		return nil
	}
	return c.MemoryManager
}

func nilSafeTaskRegistry(c *Components) *task.Registry {
	if c == nil {
		return nil
	}
	return c.TaskRegistry
}

func nilSafeSessionStore(c *Components) session.Store {
	if c == nil {
		return nil
	}
	return c.SessionStore
}

func nilSafeWorkerPool(c *Components) *worker.Pool {
	if c == nil {
		return nil
	}
	return c.WorkerPool
}

func nilSafeSkillRegistry(c *Components) *skills.Registry {
	if c == nil {
		return nil
	}
	return c.SkillRegistry
}

func nilSafeSkillExecutor(c *Components) *skills.Executor {
	if c == nil {
		return nil
	}
	return c.SkillExecutor
}

func nilSafeSelfImprove(c *Components) *selfimprove.Controller {
	if c == nil {
		return nil
	}
	return c.SelfImproveCtrl
}

func nilSafeTokenCache(c *Components) *llm.TokenCacheCoordinator {
	if c == nil {
		return nil
	}
	return c.TokenCache
}

func nilSafeSecurityChecker(c *Components) *security.PermissionChecker {
	if c == nil {
		return nil
	}
	return c.SecurityChecker
}

func nilSafeScheduler(c *Components) *scheduler.Scheduler {
	if c == nil {
		return nil
	}
	return c.Scheduler
}

func nilSafeTemplateRegistry(c *Components) *templates.Registry {
	if c == nil {
		return nil
	}
	return c.TemplateRegistry
}
