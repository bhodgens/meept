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
	"github.com/caimlas/meept/internal/registry"
	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/pkg/models"
)

// Daemon manages the meept daemon lifecycle.
type Daemon struct {
	config   *Config
	bus      *bus.MessageBus
	registry *registry.Registry
	rpc      *rpc.Server
	logger   *slog.Logger

	status    models.DaemonStatus
	startTime time.Time
	pidFile   string
}

// Config holds daemon configuration.
type Config struct {
	SocketPath     string
	PIDFile        string
	StateDir       string
	ShutdownTimeout time.Duration
	LogLevel       slog.Level
}

// DefaultConfig returns the default daemon configuration.
func DefaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	stateDir := filepath.Join(homeDir, ".meept")
	return &Config{
		SocketPath:     filepath.Join(stateDir, "meept.sock"),
		PIDFile:        filepath.Join(stateDir, "meept.pid"),
		StateDir:       stateDir,
		ShutdownTimeout: 10 * time.Second,
		LogLevel:       slog.LevelInfo,
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

	// Create message bus
	msgBus := bus.New(nil, logger)

	// Create registry
	reg := registry.New(logger)

	// Create RPC server
	rpcServer := rpc.New(&rpc.Config{
		SocketPath: cfg.SocketPath,
	}, msgBus, logger)

	// Register proxy handlers for Python agent integration
	proxy := rpc.NewProxyHandler(msgBus)
	proxy.RegisterProxyMethods(rpcServer)

	// Register RPC server as a component
	reg.Register(rpcServer)

	return &Daemon{
		config:   cfg,
		bus:      msgBus,
		registry: reg,
		rpc:      rpcServer,
		logger:   logger,
		status:   models.StatusStopped,
		pidFile:  cfg.PIDFile,
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

	// Start all components
	if err := d.registry.StartAll(ctx); err != nil {
		return fmt.Errorf("failed to start components: %w", err)
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

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		d.logger.Info("daemon: received signal", "signal", sig)
	case <-ctx.Done():
		d.logger.Info("daemon: context cancelled")
	}

	// Graceful shutdown
	return d.shutdown()
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

	// Stop all components
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
