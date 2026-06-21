// Package daemon provides the main daemon lifecycle management.
package daemon

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/caimlas/meept/internal/agent"
	botpkg "github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/calendar"
	"github.com/caimlas/meept/internal/cluster"
	"github.com/caimlas/meept/internal/comm/http"
	comprpkg "github.com/caimlas/meept/internal/compress"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/metrics"
	mcp "github.com/caimlas/meept/internal/tools/mcp"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/project"
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
//
//nolint:revive // stutter with package name is intentional for API clarity
type Daemon struct {
	config           *Config
	fullConfig       *config.Config // Full configuration loaded from file
	bus              *bus.MessageBus
	registry         *registry.Registry
	rpc              *rpc.Server
	httpServer       *http.Server
	components       *Components // Agent, tools, LLM, etc.
	metricsStore     *metrics.Store
	metricsCollector    *metrics.Collector
	compressionStore    comprpkg.CCRStore
	compressionPipeline *comprpkg.Pipeline
	taskCollector       *metrics.TaskCollector
	logger              *slog.Logger

	// Plan system
	planStore   *plan.SQLiteStore
	planManager *plan.PlanManager
	planHandler *plan.PlanHandler

	status       atomic.Value // stores models.DaemonStatus
	startTime    time.Time
	pidFile      string
	shutdownOnce atomic.Bool // DAE-H1: per-instance guard (was package-level, broke test isolation)
}

// Config holds daemon configuration.
type Config struct {
	SocketPath      string
	PIDFile         string
	StateDir        string
	WorkingDir      string
	ShutdownTimeout time.Duration
	LogLevel        slog.Level

	// Optional pre-loaded full config (used when --config flag is provided)
	FullConfig *config.Config

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
	workingDir, _ := os.Getwd()
	return &Config{
		SocketPath:      filepath.Join(stateDir, "meept.sock"),
		PIDFile:         filepath.Join(stateDir, "meept.pid"),
		StateDir:        stateDir,
		WorkingDir:      workingDir,
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

	// Use pre-loaded config if provided (e.g. from --config flag), otherwise load defaults
	var fullCfg *config.Config
	if cfg.FullConfig != nil {
		fullCfg = cfg.FullConfig
	} else {
		var err error
		fullCfg, err = config.LoadDefault()
		if err != nil {
			logger.Warn("Failed to load config, using defaults", "error", err)
			fullCfg = config.DefaultConfig()
		}
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
	components, err := NewComponents(context.Background(), fullCfg, msgBus, logger, cfg.ModelsConfig)
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

		// Register MCP management handlers (mcp.list, mcp.set_enabled).
		// The handler reads on-disk config fresh, mutates Enabled, persists
		// atomically via SaveMCPConfig, then calls Manager.Reload so newly
		// enabled servers start and newly disabled servers stop. A
		// tool-registry refresher is attached so toggled-on servers' tools
		// become visible to agents without a daemon restart, and toggled-off
		// servers' tools are unregistered.
		if components.MCPManager != nil {
			mcpRPCHandler := rpc.NewMCPHandler(components.MCPManager, fullCfg.MCP.ConfigFile)
			if refresher := newMCPToolRefresher(components.ToolRegistry, components.MCPManager, logger); refresher != nil {
				mcpRPCHandler.SetToolRefresher(refresher)
			}
			mcpRPCHandler.RegisterMCPMethods(rpcServer)
			logger.Info("MCP RPC handlers registered")
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

		// Wire budget stats getter (FIX #0031/#0035 - exposes token budget via RPC)
		if rpcServer != nil && components.LLMClient != nil {
			budget := components.LLMClient.Budget()
			if budget != nil {
				rpcServer.BudgetStatusGetter = func() (int, int, int, int, int, int, float64, float64, float64, float64, float64, float64, int, int) {
					bs := budget.GetStatus()
					return bs.HourlyUsed, bs.HourlyRemaining, bs.DailyUsed, bs.DailyRemaining, bs.RPMCurrent, bs.RPMLimit, bs.DailyCostUsed, bs.DailyCostLimit, bs.HourlyCostUsed, bs.HourlyCostLimit, bs.PerTaskCost, bs.PerSessionCost, bs.PerTaskBudget, bs.PerSessionBudget
				}
			}
		}
	}

	// Initialize cluster components (if config exists)
	var clusterCfg *cluster.Config
	var clusterEngine *cluster.GossipEngine
	var clusterGitSync *cluster.GitSync
	var clusterWG *cluster.WireGuardManager
	var clusterMQ *queue.ClusterQueue
	var queueStore *queue.Store

	cfgPath := cluster.DefaultClusterConfigPath()
	if loadedCfg, err := cluster.LoadClusterConfig(cfgPath); err == nil && loadedCfg != nil {
		clusterCfg = loadedCfg
		logger.Info("cluster config loaded",
			"path", cfgPath,
			"cluster_id", clusterCfg.ClusterID,
			"node_id", clusterCfg.NodeID,
		)

		// Create gossip engine if we have a message bus
		if msgBus != nil {
			localNodeID := clusterCfg.NodeID
			if localNodeID == "" {
				localNodeID = "local"
			}
			clusterEngine = cluster.NewGossipEngine(clusterCfg, localNodeID, msgBus, logger)
		}

		// Create git sync for cluster membership registry
		gitRepoPath := filepath.Join(cfg.StateDir, "cluster")
		clusterGitSync = cluster.NewGitSync(clusterCfg, clusterCfg, gitRepoPath, logger)

		// Create WireGuard manager for mesh networking (best-effort; non-fatal on macOS)
		wgConfigPath := filepath.Join(gitRepoPath, "wireguard")
		wgIface := clusterCfg.Network.Interface
		if wgIface == "" {
			wgIface = "wg0"
		}
		wgMgr, wgErr := cluster.NewWireGuardManager(wgConfigPath, wgIface)
		if wgErr != nil {
			logger.Warn("failed to create WireGuard manager, continuing without it", "error", wgErr)
		} else {
			clusterWG = wgMgr
		}

		// Create cluster-aware queue wrapping the existing queue
		if components != nil && components.Queue != nil {
			localNodeID := clusterCfg.NodeID
			if localNodeID == "" {
				localNodeID = "local"
			}
			cqConfig := queue.ClusterQueueConfig{
				DefaultClaimTimeout:     clusterCfg.Gossip.HeartbeatInterval,
				NodeReachabilityTimeout: clusterCfg.Gossip.PeerTimeout,
				FullPayloadReplication:  false,
			}
			if pq, ok := components.Queue.(*queue.PersistentQueue); ok {
				queueStore = pq.Store()
			}
			clusterMQ = queue.NewClusterQueue(components.Queue, queueStore, localNodeID, logger, cqConfig)
		}
	} else if err != nil {
		logger.Debug("cluster config not found, cluster features disabled", "path", cfgPath, "error", err)
	}

	// Use the WireGuard manager created above (clusterWG)
	clusterWireGuard := clusterWG

	// Register cluster RPC handlers if RPC server is available
	if rpcServer != nil && (clusterEngine != nil || clusterCfg != nil) {
		clusterHandler := rpc.NewClusterHandler(clusterEngine, clusterGitSync, clusterCfg)
		clusterHandler.SetClusterQueue(clusterMQ)
		clusterHandler.SetStore(queueStore)
		clusterHandler.RegisterClusterMethods(rpcServer)
		logger.Info("cluster RPC handlers registered")
	}

	// Wire cluster queue into components
	if clusterMQ != nil {
		if components != nil {
			components.ClusterQueue = clusterMQ
		}
	}

	// Wire cluster engine into components
	if clusterEngine != nil {
		if components != nil {
			components.ClusterEngine = clusterEngine
		}
	}

	// Wire cluster config into components
	if clusterCfg != nil {
		if components != nil {
			components.ClusterConfig = clusterCfg
		}
	}

	// Wire WireGuard sync into components
	if clusterWireGuard != nil {
		if components != nil {
			components.ClusterWireGuard = clusterWireGuard
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
	if metricsStore != nil && components != nil && components.Dispatcher != nil {
		components.Dispatcher.SetMetricsStore(metricsStore)
	}

	// Wire metrics recorder to runtime manager for lifecycle metrics
	if metricsStore != nil && components != nil && components.ContainerManager != nil {
		components.ContainerManager.SetMetricsRecorder(&runtimeMetricsAdapter{store: metricsStore})
	}

	// Wire task collector and response analyzer to agent loop
	var taskColl *metrics.TaskCollector
	if metricsStore != nil && components != nil && components.AgentLoop != nil {
		// Share the same *sql.DB handle as the metrics store to avoid
		// SQLite locking conflicts when both Store and TaskCollector
		// write to the same metrics.db file concurrently.
		tc, err := metrics.NewTaskCollectorWithDB(metricsStore.DB(), logger.With("component", "task-collector"))
		if err != nil {
			logger.Warn("Failed to create task collector with shared DB, falling back to path-based", "error", err)
			// Fall back to opening a separate connection (less safe, but keeps the feature working).
			metricsDBPath := filepath.Join(cfg.StateDir, "metrics.db")
			tc, err = metrics.NewTaskCollector(metricsDBPath, logger.With("component", "task-collector"))
			if err != nil {
				logger.Warn("Failed to create task collector", "error", err)
			}
		}
		if tc != nil {
			taskColl = tc
			components.AgentLoop.SetTaskCollector(taskColl)
			logger.Info("Task collector wired into agent loop")
		}
		respAnalyzer := metrics.NewResponseAnalyzer()
		components.AgentLoop.SetResponseAnalyzer(respAnalyzer)
		logger.Info("Response analyzer wired into agent loop")
	}

	// Create metrics collector with getter functions for actual values
	var coll *metrics.Collector
	if metricsStore != nil && components != nil {
		coll = metrics.NewCollector(metricsStore, msgBus, &metrics.CollectorConfig{
			GetQueueDepth: func() int {
				if components.Queue == nil {
					return 0
				}
				ctx := context.Background()
				stats, err := components.Queue.Stats(ctx)
				if err != nil || stats.ByState == nil {
					return 0
				}
				return stats.ByState[queue.StatePending] + stats.ByState[queue.StateClaimed]
			},
			GetActiveAgents: func() int {
				if components.WorkerPool == nil {
					return 0
				}
				stats := components.WorkerPool.GetStats()
				return stats.BusyWorkers
			},
		})
	} else if metricsStore != nil {
		// metricsStore exists but components is nil - create collector without getters
		coll = metrics.NewCollector(metricsStore, msgBus, nil)
	}

	// Wire typed event listeners for rich metrics (token tracking, turn timing, etc.)
	if coll != nil && components != nil && components.AgentEventEmitter != nil {
		coll.RegisterEventListeners(agentEventAdapter{components.AgentEventEmitter})
		logger.Info("Metrics collector wired to agent event emitter")
	}

	// --- Plan system initialization ---

	// Create plan store (SQLite-backed)
	var planStoreInst *plan.SQLiteStore
	var planStoreIF plan.PlanStore
	var planManagerInst *plan.PlanManager
	var planHandlerInst *plan.PlanHandler

	planStoreInst, err = plan.NewSQLiteStore(filepath.Join(cfg.StateDir, "plans.db"), logger)
	if err != nil {
		logger.Warn("Failed to create plan store, plan system disabled", "error", err)
	} else {
		planStoreIF = planStoreInst

		// Create TaskCreator adapter wrapping task.Registry
		var taskCreator plan.TaskCreator
		if components != nil && components.TaskRegistry != nil {
			taskCreator = newTaskCreatorAdapter(components.TaskRegistry)
		}

		// Create PlanManager
		planManagerInst = plan.NewPlanManager(planStoreIF, msgBus, fullCfg.Plans, taskCreator, logger)

		// Wire PlanManager into components for RalphLoop access
		if components != nil {
			components.PlanManager = planManagerInst
		}

		// Create PlanHandler (subscribes to task events for progress tracking)
		planHandlerInst = plan.NewPlanHandler(planManagerInst, msgBus, logger)

		logger.Info("Plan system initialized",
			"mode", fullCfg.Plans.Mode,
			"task_creator_available", taskCreator != nil,
		)
	}

	// Register plan RPC handlers (direct Go handlers override bus proxy)
	if rpcServer != nil && planManagerInst != nil {
		planRPCHandler := rpc.NewPlanHandler(planManagerInst, planStoreIF)
		planRPCHandler.RegisterPlanMethods(rpcServer)
		logger.Info("Plan RPC handlers registered")
	}

	// Create service registry with dependencies
	uploadCfg := fullCfg.Daemon.Uploads
	uploadDataDir := filepath.Join(cfg.StateDir, "uploads")
	if fullCfg.Daemon.DataDir != "" {
		uploadDataDir = filepath.Join(fullCfg.Daemon.DataDir, "uploads")
	}
	// Respect the operator's `uploads.enabled = false` toggle — when disabled,
	// leave UploadsDir empty so services.NewRegistry skips UploadService creation.
	if !uploadCfg.Enabled {
		uploadDataDir = ""
		logger.Info("Upload service disabled by config (uploads.enabled=false)")
	}
	svcRegistry, err := services.NewRegistry(services.Config{
		Bus:              msgBus,
		AgentRegistry:    nilSafeAgentRegistry(components),
		Queue:            nilSafeQueue(components),
		MemoryManager:    nilSafeMemoryManager(components),
		TaskRegistry:     nilSafeTaskRegistry(components),
		SessionStore:     nilSafeSessionStore(components),
		WorkerPool:       nilSafeWorkerPool(components),
		SkillRegistry:    nilSafeSkillRegistry(components),
		SkillExecutor:    nilSafeSkillExecutor(components),
		TemplateRegistry: nilSafeTemplateRegistry(components),
		SelfImprove:      nilSafeSelfImprove(components),
		TokenCache:       nilSafeTokenCache(components),
		SecurityChecker:  nilSafeSecurityChecker(components),
		Scheduler:        nilSafeScheduler(components),
		CalendarClient:   nilSafeCalendarClient(components),
		RuntimeManager:   nilSafeRuntimeManager(components),
		WorkingDir:       cfg.WorkingDir,
		DaemonController: daemonControl,
		PidFile:          cfg.PIDFile,
		StateDir:         cfg.StateDir,
		ProjectManager:   nilSafeProjectManager(components),
		PlanManager:      planManagerInst,
		PlanStore:        planStoreIF,
		ChatTimeout:      fullCfg.ChatTimeout(),
		UploadsDir:       uploadDataDir,
		UploadsMaxMB:     uploadCfg.MaxSizeMB,
		UploadsTypes:     uploadCfg.AllowedTypes,
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create service registry: %w", err)
	}

	// Wire audit DB from security orchestrator into security service
	if components.SecurityOrchestrator != nil && svcRegistry.Security != nil {
		if auditDB := components.SecurityOrchestrator.AuditDB(); auditDB != nil {
			svcRegistry.Security.SetAuditDB(auditDB)
			logger.Info("Audit DB wired to security service")
		}
	}

	// Wire upload store into agent loop for vision pre-flight image resolution.
	// The UploadService satisfies llm.UploadStore via its Load method.
	if svcRegistry.Upload != nil && components.AgentLoop != nil {
		components.AgentLoop.SetUploadStore(svcRegistry.Upload)
		logger.Info("Upload service wired to agent loop for vision pre-flight")
	}

	// Register daemon and model RPC handlers (after service registry is created)
	if rpcServer != nil {
		if svcRegistry.Daemon != nil {
			daemonHandler := NewDaemonRPCHandler(svcRegistry.Daemon)
			daemonHandler.SetRuntimeService(svcRegistry.Runtime)
			daemonHandler.RegisterDaemonMethods(rpcServer)
			logger.Info("Daemon RPC handlers registered")
		}
		if svcRegistry.Model != nil {
			modelHandler := NewModelRPCHandler(svcRegistry.Model)
			modelHandler.RegisterModelMethods(rpcServer)
			logger.Info("Model RPC handlers registered")
		}

		// Runtime management handlers
		if svcRegistry.Runtime != nil {
			runtimeHandler := NewRuntimeRPCHandler(svcRegistry.Runtime)
			runtimeHandler.RegisterRuntimeMethods(rpcServer)
			logger.Info("Runtime RPC handlers registered")
		}

		// Search handlers (semantic + keyword)
		if svcRegistry.Search != nil {
			registerSearchRPCHandlers(rpcServer, svcRegistry.Search)
			logger.Info("Search RPC handlers registered")
		}

		// Upload handlers (image / file uploads for multimodal chat)
		if svcRegistry.Upload != nil {
			uploadHandler := NewUploadRPCHandler(svcRegistry.Upload)
			uploadHandler.RegisterUploadMethods(rpcServer)
			logger.Info("Upload RPC handlers registered")
		}

		// Project management handlers
		if components.ProjectManager != nil {
			projectHandler := rpc.NewProjectHandler(components.ProjectManager, nilSafeSessionStore(components))
			if components.ArtifactManager != nil {
				projectHandler.SetArtifactInvalidator(components.ArtifactManager)
			}
			projectHandler.RegisterProjectMethods(rpcServer)
			logger.Info("Project RPC handlers registered")
		}

		// Bot management handlers
		if components.BotManager != nil {
			botRPCHandler := botpkg.NewRPCHandler(components.BotManager)
			for method, handler := range botRPCHandler.Handlers() {
				rpcServer.RegisterHandler(method, handler)
			}
			logger.Info("Bot RPC handlers registered")
		}
	}

	// Create HTTP server (if enabled)
	var httpSrv *http.Server
	if fullCfg.Transport.HTTP.Enabled {
		if configService != nil && daemonControl != nil {
			httpCfg := http.DefaultServerConfig()
			httpCfg.Addr = fullCfg.Transport.HTTP.Addr
			httpCfg.RequireAuth = fullCfg.Transport.HTTP.RequireAuth
			httpCfg.APIKeys = fullCfg.Transport.HTTP.APIKeys
			httpCfg.TLSCertFile = fullCfg.Transport.HTTP.TLSCertFile
			httpCfg.TLSKeyFile = fullCfg.Transport.HTTP.TLSKeyFile
			httpCfg.RESTEnabled = fullCfg.Transport.HTTP.REST
			// Map TLS version string to Go constant
			switch fullCfg.Transport.HTTP.TLSMinVersion {
			case "tls1.3":
				httpCfg.TLSMinVersion = tls.VersionTLS13
			default:
				httpCfg.TLSMinVersion = tls.VersionTLS12
			}
			// Build functional options based on config
			var httpOpts []http.ServerOption

			// WebSocket support (if enabled)
			if fullCfg.Transport.HTTP.WebSocket && msgBus != nil {
				wsPath := fullCfg.Transport.HTTP.WSPath
				if wsPath == "" {
					wsPath = "/ws"
				}
				httpOpts = append(httpOpts, http.WithWebSocket(msgBus, wsPath))
				logger.Info("WebSocket endpoint enabled", "path", wsPath)
			}

			// MCP over HTTP+SSE support (if enabled)
			if fullCfg.Transport.HTTP.MCP && svcRegistry != nil {
				mcpPath := fullCfg.Transport.HTTP.MCPPath
				if mcpPath == "" {
					mcpPath = "/mcp"
				}
				httpOpts = append(httpOpts, http.WithMCP(svcRegistry, mcpPath))
				logger.Info("MCP over HTTP+SSE enabled", "path", mcpPath)
			}

			// Bot webhook support (if bot manager is available)
			if components.BotManager != nil {
				httpOpts = append(httpOpts, http.WithBotWebhook(botpkg.NewWebhookHandler(components.BotManager)))
				logger.Info("Bot webhook endpoint enabled", "path", "/api/v1/bot/{botID}/trigger")
			}

			// RPC call bridge: enables /api/v1/bus/call so HTTP clients can
			// dispatch any RPC method registered on the RPC server.
			if rpcServer != nil {
				httpOpts = append(httpOpts, http.WithRPCCall(rpcServer.CallMethod))
				logger.Info("RPC call bridge enabled", "endpoint", "/api/v1/bus/call")
			}

			// Notification event system for real-time push to clients
			if components.NotificationEmitter != nil {
				httpOpts = append(httpOpts, http.WithNotification(components.NotificationEmitter))
				logger.Info("Notification endpoint enabled")
			}

			// PTY session endpoints (if PTY manager is available)
			if components.PTYManager != nil {
				httpOpts = append(httpOpts, http.WithPTY(http.NewPTYHandler(components.PTYManager, logger)))
				logger.Info("PTY session endpoints enabled", "path", "/api/v1/pty/*")
			}

			var metricsService interface {
				GetLiveMetrics() (*metrics.LiveMetricsSnapshot, error)
				GetHistoricalMetrics(ctx context.Context, from, to time.Time, resolution string) ([]metrics.MetricPoint, error)
				SubscribeMetrics() (<-chan *metrics.LiveMetricsSnapshot, func())
			}
			if metricsStore != nil {
				metricsService = &metricsStoreWrapper{store: metricsStore}
			}
			httpSrv = http.NewServer(httpCfg, configService, daemonControl, metricsService, svcRegistry, logger, httpOpts...)
			// Wire firewall stats getter for HTTP endpoint
			if components.AgentLoop != nil {
				httpSrv.FirewallStatsGetter = func() map[string]any {
					return components.AgentLoop.FirewallStats()
				}
				logger.Info("Firewall stats HTTP getter registered")
			}

			// Wire budget stats getter for HTTP endpoint (FIX #0031/#0035)
			if components.LLMClient != nil {
				budget := components.LLMClient.Budget()
				if budget != nil {
					httpSrv.BudgetStatusGetter = func() (int, int, int, int, int, int, float64, float64, float64, float64, float64, float64, int, int) {
						bs := budget.GetStatus()
						return bs.HourlyUsed, bs.HourlyRemaining, bs.DailyUsed, bs.DailyRemaining, bs.RPMCurrent, bs.RPMLimit, bs.DailyCostUsed, bs.DailyCostLimit, bs.HourlyCostUsed, bs.HourlyCostLimit, bs.PerTaskCost, bs.PerSessionCost, bs.PerTaskBudget, bs.PerSessionBudget
					}
					logger.Info("Budget stats HTTP getter registered")
				}
			}
			logger.Info("HTTP server created", "addr", httpCfg.Addr, "tls", "mandatory")
			logger.Info("TLS always enabled for HTTP server", "cert", httpCfg.TLSCertFile)
			if httpCfg.RequireAuth {
				logger.Info("Authentication required for HTTP server", "api_keys_configured", len(httpCfg.APIKeys))
			} else {
				logger.Warn("Authentication disabled for HTTP server - no API key required")
			}
		}
	} else {
		logger.Info("HTTP transport disabled")
	}

	// Create compression CCR store and pipeline (if compression is enabled in config)
	var (
		compPipeline *comprpkg.Pipeline
		compStore    comprpkg.CCRStore
	)
	if fullCfg.Agent.Compression.Enabled {
		ccrPath := filepath.Join(cfg.StateDir, "compression.db")
		ccrStore, err := comprpkg.NewCCRStore(comprpkg.CCRStoreConfig{
			DatabasePath: ccrPath,
			DefaultTTL:   comprpkg.Duration{Duration: fullCfg.Agent.Compression.TTL},
		})
		if err != nil {
			logger.Warn("Failed to create compression store, compression disabled at runtime", "error", err)
		} else {
			compStore = ccrStore
			logger.Info("Compression CCR store created", "path", ccrPath)

			pipelineCfg := comprpkg.PipelineConfig{
				MinTokensToCompress:  fullCfg.Agent.Compression.MinTokensToCompress,
				TTL:                  fullCfg.Agent.Compression.TTL,
				EnableCCR:            true,
				CompressUserMessages: fullCfg.Agent.Compression.CompressUserMessages,
				TargetRatio:          fullCfg.Agent.Compression.TargetRatio,
			}
			compPipeline = comprpkg.NewPipelineWithConfig(ccrStore, pipelineCfg)

			// Wire pipeline into components' AgentLoop
			if components != nil && components.AgentLoop != nil {
				components.AgentLoop.SetCompressionPipeline(compPipeline)
				logger.Info("Compression pipeline wired into agent loop")
			}
			logger.Info("Compression pipeline created",
				"strategy", fullCfg.Agent.Compression.Strategy,
				"min_tokens", fullCfg.Agent.Compression.MinTokensToCompress,
			)
		}
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
		components:          components,
		metricsStore:        metricsStore,
		metricsCollector:    coll,
		compressionStore:    compStore,
		compressionPipeline: compPipeline,
		taskCollector:       taskColl,
		planStore:        planStoreInst,
		planManager:      planManagerInst,
		planHandler:      planHandlerInst,
		logger:           logger,
		pidFile:          cfg.PIDFile,
	}
	d.status.Store(models.StatusStopped)

	// Register compression tools with the MCP manager so they appear in the
	// tool registry (mcc_compress, mcc_retrieve, mcc_stats).
	if d.components != nil && d.components.MCPManager != nil {
		cfg := mcp.CompressionConfig{
			Enabled:             fullCfg.Agent.Compression.Enabled,
			MinTokensToCompress: fullCfg.Agent.Compression.MinTokensToCompress,
			TTL:                 int64(fullCfg.Agent.Compression.TTL.Seconds()),
		}
		handler := mcp.NewCompressionHandler(compPipeline, compStore, cfg)
		d.components.MCPManager.RegisterCompressionHandler(handler)
		if handler != nil {
			logger.Info("Compression tools registered with MCP manager")
		}
	}

	// Wire PlanManager into Orchestrator for plan system integration.
	// The PlanHandler subscribes to task events independently via the bus,
	// so event routing is already handled. This reference enables the
	// Orchestrator to make direct plan queries when needed.
	if components != nil && components.Orchestrator != nil && planManagerInst != nil {
		components.Orchestrator.SetPlanManager(planManagerInst)
	}

	// Wire PlanManager into RalphLoop. The RalphLoop is created in
	// NewComponents before the plan system is initialized, so its
	// planManager field is nil at construction time.
	if components != nil && components.RalphLoop != nil && planManagerInst != nil {
		components.RalphLoop.SetPlanManager(planManagerInst)
	}

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
		d.shutdown() // D5: Clean up any partially-initialized state
		return fmt.Errorf("failed to start components: %w", err)
	}

	// Start agent components (chat handler, status handler, etc.)
	if d.components != nil {
		if err := d.components.Start(ctx); err != nil {
			d.shutdown() // D5: Clean up registry components on agent start failure
			return fmt.Errorf("failed to start agent components: %w", err)
		}
		d.logger.Info("daemon: agent components started",
			"tools", d.components.ToolRegistry.Count(),
			"llm_configured", d.components.LLMClient != nil,
			"cluster_enabled", d.components.ClusterEngine != nil,
		)
	}

	// WireGuard manager is config-only (no background goroutines);
	// configuration is applied on-demand via syncWireGuardConfig.

	// Start local LLM runtimes in the background so the daemon reaches
	// "running" status without blocking on potentially slow model loading.
	// D6: Derive a cancellable context from the parent so the goroutine
	// can be stopped on shutdown/reload instead of running indefinitely.
	if d.components != nil && d.components.ContainerManager != nil {
		llmCtx, cancelLlm := context.WithCancel(ctx)
		go func() {
			defer cancelLlm() // Ensure cleanup on exit
			if err := d.components.ContainerManager.StartAll(llmCtx); err != nil {
				d.logger.Error("Failed to start LLM runtimes", "error", err)
			}
		}()
	}

	// Metrics collector is started automatically by NewCollector

	// Start plan handler (subscribes to task events for plan progress tracking)
	if d.planHandler != nil {
		if err := d.planHandler.Start(ctx); err != nil {
			d.logger.Error("Failed to start plan handler", "error", err)
		}
	}

	// Sync WireGuard configuration for cluster mesh (best-effort)
	if d.components != nil && d.components.ClusterWireGuard != nil && d.components.ClusterConfig != nil && d.components.ClusterGitSync != nil {
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := d.syncWireGuardConfig(); err != nil {
						d.logger.Warn("WireGuard config sync failed", "error", err)
					}
				}
			}
		}()
	}

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

// shutdownOnce ensures shutdown() is idempotent - safe to call multiple times
// or on error paths during startup when partial initialization occurred.
// NOTE: This is a per-Daemon field (d.shutdownOnce). A previous package-level
// var leaked shutdown state across Daemon instances in the same process, which
// broke test isolation and would prevent any future in-process restart.

func (d *Daemon) shutdown() error {
	// Idempotent guard: skip if already shutting down or stopped
	if !d.shutdownOnce.CompareAndSwap(false, true) {
		return nil
	}

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

	// Stop plan handler and close plan store
	if d.planHandler != nil {
		d.planHandler.Stop()
	}
	if d.planStore != nil {
		if err := d.planStore.Close(); err != nil {
			d.logger.Error("Plan store close error", "error", err)
		}
	}

	// Stop local LLM runtimes
	if d.components != nil && d.components.ContainerManager != nil {
		if err := d.components.ContainerManager.StopAll(ctx); err != nil {
			d.logger.Error("Failed to stop LLM runtimes", "error", err)
		}
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

	// Stop task collector last. Its db.Close() can block if another
	// connection to the same SQLite DB (metrics.db) is still open.
	// By this point, metricsStore is already closed and all components
	// are stopped, so the DB should be exclusively owned by the collector.
	if d.taskCollector != nil {
		d.taskCollector.Shutdown()
	}

	// Close message bus
	d.bus.Close()

	d.status.Store(models.StatusStopped)
	d.logger.Info("daemon: stopped")
	return nil
}

// syncWireGuardConfig generates and applies the WireGuard configuration
// for the cluster mesh based on the current member registry.
func (d *Daemon) syncWireGuardConfig() error {
	wgMgr := d.components.ClusterWireGuard
	cfg := d.components.ClusterConfig
	gitSync := d.components.ClusterGitSync

	if wgMgr == nil || cfg == nil || gitSync == nil {
		return nil
	}

	// Load WireGuard private key from keys directory
	keysDir := filepath.Join(d.config.StateDir, "cluster", "keys")
	privPath := filepath.Join(keysDir, "wg_private.key")
	privKey, err := os.ReadFile(privPath)
	if err != nil {
		return fmt.Errorf("failed to read WireGuard private key: %w", err)
	}

	// Get current members from git
	members, err := gitSync.GetMembers()
	if err != nil {
		return fmt.Errorf("failed to get cluster members: %w", err)
	}

	// Find self in members to get our ClusterIP
	var selfClusterIP string
	for _, m := range members {
		if m.NodeID == cfg.NodeID {
			selfClusterIP = m.ClusterIP
			break
		}
	}
	if selfClusterIP == "" {
		return fmt.Errorf("local node %q not found in cluster members", cfg.NodeID)
	}

	// Build WireGuard config
	wgConfig := &cluster.WireGuardConfig{
		PrivateKey:          string(privKey),
		ClusterIP:           selfClusterIP,
		ListenPort:          cfg.Network.WireGuardPort,
		DNS:                 "8.8.8.8",
		Peers:               make([]cluster.Member, 0, len(members)),
		PersistentKeepalive: "25",
	}

	// Add peers (skip self)
	for _, m := range members {
		if m.NodeID == cfg.NodeID {
			continue // Skip self
		}
		wgConfig.Peers = append(wgConfig.Peers, cluster.Member{
			NodeID:       m.NodeID,
			WireGuardPub: m.WireGuardPub,
			Endpoint:     m.Endpoint,
			ClusterIP:    m.ClusterIP,
		})
	}

	// Apply the config
	if err := wgMgr.ApplyConfig(wgConfig); err != nil {
		return fmt.Errorf("failed to apply WireGuard config: %w", err)
	}

	d.logger.Debug("WireGuard config synced", "peers", len(wgConfig.Peers))
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

// agentEventAdapter wraps an *agent.EventEmitter to satisfy the
// metrics.TypedEventEmitter interface, bridging the two packages' separate
// type definitions (both AgentEventType and AgentEvent are string-based
// mirrors defined independently to avoid import cycles).
type agentEventAdapter struct {
	inner *agent.EventEmitter
}

// convertEventData converts agent event data types to their metrics package
// mirrors so that the collector's type assertions work correctly.
func convertEventData(data agent.AgentEventData) any {
	switch d := data.(type) {
	case agent.AfterProviderResponseData:
		return metrics.AfterProviderResponseData{
			ModelID:        d.ModelID,
			StatusCode:     d.StatusCode,
			ResponseTokens: d.ResponseTokens,
			Latency:        d.Latency,
			Cached:         d.Cached,
			Error:          d.Error,
		}
	case agent.TurnEndData:
		return metrics.TurnEndData{
			TurnNumber:     d.TurnNumber,
			HadToolCalls:   d.HadToolCalls,
			ToolCallCount:  d.ToolCallCount,
			ResponseTokens: d.ResponseTokens,
			StoppedBy:      d.StoppedBy,
		}
	case agent.SessionEndData:
		return metrics.SessionEndData{
			SessionID:   d.SessionID,
			Outcome:     d.Outcome,
			Duration:    d.Duration,
			TotalTokens: d.TotalTokens,
			TotalIter:   d.TotalIter,
			Error:       d.Error,
		}
	case agent.ToolExecutionStartData:
		return metrics.ToolExecutionStartData{
			ToolCallID: d.ToolCallID,
			ToolName:   d.ToolName,
			Arguments:  d.Arguments,
		}
	case agent.ToolExecutionEndData:
		return metrics.ToolExecutionEndData{
			ToolCallID:  d.ToolCallID,
			ToolName:    d.ToolName,
			Success:     d.Success,
			Result:      d.Result,
			Error:       d.Error,
			Cached:      d.Cached,
			Duration:    d.Duration,
			Blocked:     d.Blocked,
			BlockReason: d.BlockReason,
		}
	default:
		return data
	}
}

func (a agentEventAdapter) On(eventType metrics.AgentEventType, name string, listener func(context.Context, metrics.AgentEvent)) {
	a.inner.On(agent.AgentEventType(eventType), name, func(ctx context.Context, event agent.AgentEvent) {
		listener(ctx, metrics.AgentEvent{
			Type:           metrics.AgentEventType(event.Type),
			Timestamp:      event.Timestamp,
			AgentID:        event.AgentID,
			ConversationID: event.ConversationID,
			Iteration:      event.Iteration,
			Data:           convertEventData(event.Data),
		})
	})
}

func (a agentEventAdapter) OnAsync(eventType metrics.AgentEventType, name string, listener func(context.Context, metrics.AgentEvent)) {
	a.inner.OnAsync(agent.AgentEventType(eventType), name, func(ctx context.Context, event agent.AgentEvent) {
		listener(ctx, metrics.AgentEvent{
			Type:           metrics.AgentEventType(event.Type),
			Timestamp:      event.Timestamp,
			AgentID:        event.AgentID,
			ConversationID: event.ConversationID,
			Iteration:      event.Iteration,
			Data:           convertEventData(event.Data),
		})
	})
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
			if strings.Contains(name, ".") {
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
	return w.store.GetHistoricalMetrics(ctx, from, to, resolution)
}

func (w *metricsStoreWrapper) SubscribeMetrics() (_ <-chan *metrics.LiveMetricsSnapshot, _ func()) {
	return w.store.SubscribeMetrics()
}

// runtimeMetricsAdapter adapts metrics.Store to implement llm.MetricsRecorder.
type runtimeMetricsAdapter struct {
	store *metrics.Store
}

func (a *runtimeMetricsAdapter) RecordRuntimeHealth(providerID string, healthy bool) {
	if a.store == nil {
		return
	}
	val := 0.0
	if healthy {
		val = 1.0
	}
	a.store.Record("runtime.healthy", val, map[string]string{
		"provider": providerID,
	})
}

func (a *runtimeMetricsAdapter) RecordRuntimeSpawn(providerID string, duration time.Duration, success bool) {
	if a.store == nil {
		return
	}
	tags := map[string]string{"provider": providerID}
	a.store.Record("runtime.spawn.duration", duration.Seconds(), tags)
	if success {
		a.store.Record("runtime.spawn.success", 1, tags)
	} else {
		a.store.Record("runtime.spawn.failure", 1, tags)
	}
}

func (a *runtimeMetricsAdapter) RecordRuntimeRestart(providerID string, attempt int, success bool) {
	if a.store == nil {
		return
	}
	tags := map[string]string{
		"provider": providerID,
	}
	a.store.Record("runtime.restart.attempts", float64(attempt), tags)
	if success {
		a.store.Record("runtime.restart.success", 1, tags)
	} else {
		a.store.Record("runtime.restart.failure", 1, tags)
	}
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

func nilSafeRuntimeManager(c *Components) *llm.RuntimeManager {
	if c == nil {
		return nil
	}
	return c.ContainerManager
}

func nilSafeProjectManager(c *Components) *project.ProjectManager {
	if c == nil {
		return nil
	}
	return c.ProjectManager
}

func nilSafeCalendarClient(c *Components) *calendar.Client {
	if c == nil {
		return nil
	}
	return c.CalendarClient
}

// taskCreatorAdapter adapts task.Registry to implement plan.TaskCreator.
// It bridges the plan system's task creation needs with the existing task
// subsystem, allowing plans to synthesize tasks during approval.
type taskCreatorAdapter struct {
	registry *task.Registry
}

func newTaskCreatorAdapter(registry *task.Registry) *taskCreatorAdapter {
	return &taskCreatorAdapter{registry: registry}
}

func (a *taskCreatorAdapter) CreateTask(ctx context.Context, name, description string) (*task.Task, error) {
	return a.registry.Create(ctx, name, description)
}

func (a *taskCreatorAdapter) CreateTaskStep(ctx context.Context, taskID, description string, sequence int) (*task.TaskStep, error) {
	step := task.NewTaskStep(taskID, description, sequence)
	if err := a.registry.StepStore().Create(step); err != nil {
		return nil, err
	}
	return step, nil
}

func (a *taskCreatorAdapter) UpdateTaskStep(_ context.Context, step *task.TaskStep) error {
	return a.registry.StepStore().Update(step)
}

func (a *taskCreatorAdapter) LinkSession(ctx context.Context, taskID, sessionID string) error {
	return a.registry.LinkSession(ctx, taskID, sessionID)
}
