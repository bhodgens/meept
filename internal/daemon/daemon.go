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
	"syscall"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/registry"
	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/pkg/models"
	"github.com/caimlas/meept/pkg/security"
)

// Daemon manages the meept daemon lifecycle.
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

	status    models.DaemonStatus
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

	// Security settings
	AllowedPaths              []string
	BlockedPaths              []string
	BlockFinancial            bool
	RequireConfirmationHigh   bool
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
func New(cfg *Config) (*Daemon, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Ensure state directory exists
	if err := os.MkdirAll(cfg.StateDir, 0700); err != nil {
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

	// Create RPC server
	rpcServer := rpc.New(&rpc.Config{
		SocketPath: cfg.SocketPath,
	}, msgBus, logger)

	// Register proxy handlers that forward to bus subscribers
	proxy := rpc.NewProxyHandler(msgBus)
	proxy.RegisterProxyMethods(rpcServer)

	// Register security handlers (Go-native, high-performance)
	securityCfg := security.Config{
		AllowedPaths:              cfg.AllowedPaths,
		BlockedPaths:              cfg.BlockedPaths,
		BlockFinancial:            cfg.BlockFinancial,
		RequireConfirmationHigh:   cfg.RequireConfirmationHigh,
		RequireConfirmationCritical: cfg.RequireConfirmationCritical,
	}
	securityHandler := rpc.NewSecurityHandler(securityCfg)
	securityHandler.RegisterSecurityMethods(rpcServer)

	// Register dev handlers (model switching, testing, etc.)
	devHandler := rpc.NewDevHandler()
	devHandler.RegisterDevMethods(rpcServer)

	// Register RPC server as a component
	reg.Register(rpcServer)

	// Create agent components (LLM, tools, agent loop, chat handler)
	components, err := NewComponents(fullCfg, msgBus, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create components: %w", err)
	}

	// Register skills handlers (both direct and bus-based)
	if fullCfg.Skills.Enabled && components.SkillRegistry != nil {
		rpc.RegisterSkillsHandlers(rpcServer, components.SkillRegistry, components.SkillExecutor)
		logger.Info("Skills RPC handlers registered",
			"skill_count", components.SkillRegistry.Count(),
			"executor_available", components.SkillExecutor != nil,
		)
	}

	// Register self-improve handlers (native Go, calling Controller directly)
	siHandler := rpc.NewSelfImproveHandler(components.SelfImproveCtrl)
	siHandler.RegisterSelfImproveMethods(rpcServer)
	if components.SelfImproveCtrl != nil {
		logger.Info("Self-improve RPC handlers registered")
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

	// Create HTTP server - use metricsStore as the MetricsService
	var httpSrv *http.Server
	if configService != nil && daemonControl != nil && metricsStore != nil {
		httpCfg := http.DefaultServerConfig()
		httpSrv = http.NewServer(httpCfg, configService, daemonControl, &metricsStoreWrapper{store: metricsStore}, logger)
		logger.Info("HTTP server created", "addr", httpCfg.Addr)
	}

	return &Daemon{
		config:         cfg,
		fullConfig:     fullCfg,
		bus:            msgBus,
		registry:       reg,
		rpc:            rpcServer,
		httpServer:     httpSrv,
		components:     components,
		metricsStore:   metricsStore,
		metricsCollector: coll,
		logger:         logger,
		status:         models.StatusStopped,
		pidFile:        cfg.PIDFile,
	}, nil
}

// Run starts the daemon and blocks until shutdown.
func (d *Daemon) Run(ctx context.Context) error {
	d.logger.Info("daemon: starting", "pid", os.Getpid())
	d.status = models.StatusStarting
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

	// Start HTTP server for menubar app
	if d.httpServer != nil {
		go func() {
			if err := d.httpServer.Start(ctx); err != nil {
				d.logger.Error("HTTP server error", "error", err)
			}
		}()
		d.logger.Info("HTTP server starting", "addr", ":8081")
	}

	d.status = models.StatusRunning
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
	d.status = models.StatusStopping

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

	d.status = models.StatusStopped
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
		return nil
	}

	// Check if process is running
	proc, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(d.pidFile)
		return nil
	}

	// Send signal 0 to check if process exists
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		os.Remove(d.pidFile)
		return nil
	}

	return fmt.Errorf("daemon already running (PID %d)", pid)
}

func (d *Daemon) writePIDFile() error {
	return os.WriteFile(d.pidFile, []byte(strconv.Itoa(os.Getpid())), 0600)
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
	return d.status
}

// Info returns daemon information.
func (d *Daemon) Info() *models.DaemonInfo {
	return &models.DaemonInfo{
		PID:       os.Getpid(),
		Status:    d.status,
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

func (w *metricsStoreWrapper) SubscribeMetrics() (<-chan *metrics.LiveMetricsSnapshot, func()) {
	return w.store.SubscribeMetrics()
}
