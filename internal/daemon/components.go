// Package daemon provides the main daemon lifecycle management.
package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/calendar"
	"github.com/caimlas/meept/internal/code/ast"
	"github.com/caimlas/meept/internal/code/lsp"
	codetools "github.com/caimlas/meept/internal/code/tools"
	"github.com/caimlas/meept/internal/comm/telegram"
	"github.com/caimlas/meept/internal/comm/web"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/memvid"
	memsync "github.com/caimlas/meept/internal/memory/sync"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/scheduler"
	intsecurity "github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/internal/selfimprove"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/shadow"
	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/templates"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
	"github.com/caimlas/meept/internal/tools/mcp"
	"github.com/caimlas/meept/internal/worker"
	"github.com/caimlas/meept/pkg/models"
	"github.com/caimlas/meept/pkg/security"
)

// Components holds all the daemon components.
type Components struct {
	Config               *config.Config
	ModelsConfig         *config.ModelsConfig
	LLMClient            *llm.Client
	ClassifierClient     *llm.Client // Separate client for intent classification (nil = use LLMClient)
	SummarizerClient     *llm.Client // Separate client for session summarization (nil = use LLMClient)
	LLMResolver          *llm.Resolver
	ToolRegistry         *tools.Registry
	SecurityChecker      *security.PermissionChecker
	SecurityOrchestrator *intsecurity.Orchestrator
	AgentLoop            *agent.AgentLoop
	ChatHandler          *agent.ChatHandler
	StatusHandler        *StatusHandler
	SessionStore         session.Store
	SessionHandler       *session.Handler

	// Multi-agent orchestration components
	Queue         queue.Queue
	QueueHandler  *queue.Handler
	TaskRegistry  *task.Registry
	TaskHandler   *task.Handler
	AmendmentMgr  *task.AmendmentManager
	WorkerPool    *worker.Pool
	WorkerHandler *worker.Handler
	JobProcessor  worker.JobProcessor
	Orchestrator  *agent.Orchestrator
	ReviewManager *agent.ReviewManager

	// Agent validation watchdog
	Watchdog              *agent.Watchdog
	HallucinationDetector *agent.HallucinationDetector
	EscalationManager     *agent.EscalationManager
	ArtifactManager       *agent.ArtifactManager

	// Memory
	MemoryManager *memory.Manager
	MemoryHandler *memory.Handler

	// Memvid and multi-agent
	MemvidClient  *memvid.Client
	AgentRegistry *agent.AgentRegistry
	Dispatcher    *agent.Dispatcher

	// Shadow training
	ShadowManager *shadow.Manager

	// Learning pipeline
	LearningPipeline *selfimprove.LearningPipeline

	// Self-improvement controller (full 5-phase cycle)
	SelfImproveCtrl  *selfimprove.Controller
	SelfImproveSched *selfimprove.Scheduler

	// LLM provider manager (for multi-provider failover)
	LLMProvider llm.Chatter

	// Token cache for LLM responses
	TokenCache *llm.TokenCacheCoordinator

	// MCP integration
	MCPManager *mcp.Manager

	// Scheduler with job dependencies
	Scheduler *scheduler.Scheduler

	// Skills
	SkillRegistry   *skills.Registry
	SkillExecutor   *skills.Executor
	SkillIndex      *skills.SkillIndex
	SkillLoader     *skills.LazySkillLoader
	CapabilityIndex *skills.CapabilityIndex

	// Agent capabilities
	CapabilitiesMap *agent.CapabilitiesMap

	// Templates
	TemplateDiscovery *templates.Discovery
	TemplateRegistry  *templates.Registry

	// Distributed memory sync
	SyncManager *memsync.SyncManager
	SyncHandler *memsync.Handler

	// Result cache for tool outputs
	ResultCache *agent.ResultCache

	// Web API server
	WebServer *web.Server

	// Telegram bot
	TelegramBot     *telegram.Bot
	TelegramHandler *telegram.AgentHandler

	// Code intelligence
	ASTParser  *ast.ParserManager
	LSPManager *lsp.Manager

	// Calendar integration
	CalendarClient   *calendar.Client
	CalendarReminder *calendar.ReminderWatcher

	Logger *slog.Logger
}

// NewComponents creates all daemon components from configuration.
// NewComponents creates agent components. If modelsCfg is non-nil, it uses the
// injected config instead of loading from disk.
func NewComponents(cfg *config.Config, msgBus *bus.MessageBus, logger *slog.Logger, modelsCfg ...*config.ModelsConfig) (*Components, error) {
	c := &Components{
		Config: cfg,
		Logger: logger,
	}

	// Load models configuration - fail explicitly if not found
	var configPath string
	var err error
	if len(modelsCfg) > 0 && modelsCfg[0] != nil {
		c.ModelsConfig = modelsCfg[0]
		configPath = "<injected>"
	} else {
		c.ModelsConfig, configPath, err = loadModelsConfigWithPath(logger)
		if err != nil {
			logger.Error("FATAL: Failed to load models configuration",
				"error", err,
				"searched_paths", []string{"config/models.json5", "~/.meept/models.json5"},
				"hint", "Copy config/models.json5 to ~/.meept/models.json5 or run daemon from project directory",
			)
			return nil, fmt.Errorf("models configuration required: %w", err)
		}
	}
	logger.Info("Loaded models configuration",
		"path", configPath,
		"default_model", c.ModelsConfig.Model,
		"small_model", c.ModelsConfig.SmallModel,
		"providers", getProviderNames(c.ModelsConfig),
	)

	// Create security checker
	secCfg := security.Config{
		AllowedPaths:                cfg.Security.AllowedPaths,
		BlockedPaths:                cfg.Security.BlockedPaths,
		BlockFinancial:              cfg.Security.BlockFinancial,
		RequireConfirmationHigh:     cfg.Security.RequireConfirmationHigh,
		RequireConfirmationCritical: cfg.Security.RequireConfirmationCritical,
	}
	c.SecurityChecker = security.NewPermissionChecker(secCfg)

	// Create security orchestrator for input sanitization, output monitoring, and shell scanning
	c.SecurityOrchestrator = createSecurityOrchestrator(cfg, logger)

	// Create LLM client with budget tracking
	llmCfg := createLLMConfig(c.ModelsConfig, logger)
	var budgetTracker *llm.Budget
	if llmCfg != nil {
		// Create budget tracker from config
		budgetTracker = llm.NewBudget(llm.BudgetConfig{
			HourlyLimit:    cfg.LLM.Budget.HourlyTokenLimit,
			DailyLimit:     cfg.LLM.Budget.DailyTokenLimit,
			RateLimitRPM:   cfg.LLM.Budget.RateLimitRPM,
			Aggressiveness: cfg.LLM.Budget.Aggressiveness,
		}, logger.With("component", "budget"))

		// Create token cache if enabled
		var tokenCache *llm.TokenCacheCoordinator
		if cfg.LLM.Cache.Enabled {
			cacheCfg := llm.DefaultCacheConfig()
			cacheCfg.L2DBPath = filepath.Join(cfg.Daemon.DataDir, "token_cache.db")
			// Apply config from TOML if specified
			if cfg.LLM.Cache.L1MaxEntries > 0 {
				cacheCfg.L1MaxEntries = cfg.LLM.Cache.L1MaxEntries
			}
			if cfg.LLM.Cache.L2Enabled {
				cacheCfg.L2Enabled = true
			}
			if cfg.LLM.Cache.DefaultTTLMin > 0 {
				cacheCfg.DefaultTTL = time.Duration(cfg.LLM.Cache.DefaultTTLMin) * time.Minute
			}
			var err error
			tokenCache, err = llm.NewTokenCacheCoordinatorWithMetrics(cacheCfg, nil)
			if err != nil {
				logger.Warn("Failed to create token cache", "error", err)
			} else {
				logger.Info("Token cache initialized",
					"l1_max_entries", cacheCfg.L1MaxEntries,
					"l2_enabled", cacheCfg.L2Enabled,
					"db_path", cacheCfg.L2DBPath,
					"default_ttl", cacheCfg.DefaultTTL,
				)
			}
		}

		// Build client options
		clientOpts := []llm.ClientOption{
			llm.WithLogger(logger),
			llm.WithBudget(budgetTracker),
		}
		if tokenCache != nil {
			clientOpts = append(clientOpts, llm.WithTokenCache(tokenCache))
		}
		c.LLMClient = llm.NewClient(llmCfg, clientOpts...)
		c.TokenCache = tokenCache
		logger.Info("LLM client initialized successfully",
			"provider", llmCfg.ProviderID,
			"model", llmCfg.ModelID,
			"base_url", llmCfg.BaseURL,
			"budget_hourly_limit", cfg.LLM.Budget.HourlyTokenLimit,
			"budget_daily_limit", cfg.LLM.Budget.DailyTokenLimit,
		)

		// Create auxiliary LLM clients for classifier and summarizer
		classifierRef := c.ModelsConfig.ClassifierModel
		if classifierRef == "" {
			classifierRef = c.ModelsConfig.SmallModel
		}
		c.ClassifierClient = createAuxiliaryLLMClient(c.ModelsConfig, classifierRef, logger.With("component", "classifier-llm"), budgetTracker)
		if c.ClassifierClient != nil {
			logger.Info("Classifier LLM client initialized", "model", classifierRef)
		} else {
			logger.Info("Classifier will use main LLM client", "reason", "no classifier_model or small_model configured")
		}

		summarizerRef := c.ModelsConfig.SummarizerModel
		if summarizerRef == "" {
			summarizerRef = c.ModelsConfig.SmallModel
		}
		c.SummarizerClient = createAuxiliaryLLMClient(c.ModelsConfig, summarizerRef, logger.With("component", "summarizer-llm"), budgetTracker)
		if c.SummarizerClient != nil {
			logger.Info("Summarizer LLM client initialized", "model", summarizerRef)
		} else {
			logger.Info("Summarizer will use main LLM client", "reason", "no summarizer_model or small_model configured")
		}
	} else {
		logger.Error("FATAL: No LLM configured - chat will not work",
			"hint", "Check models.json5 configuration and ensure model exists",
		)
		return nil, fmt.Errorf("LLM configuration required but model resolution failed")
	}

	// Create tool registry (builtin tools registered after all dependencies are available)
	c.ToolRegistry = tools.NewRegistry(logger)

	// Create LLM resolver for skill model resolution
	providersCfg, err := llm.LoadProvidersConfigDefault()
	if err != nil {
		logger.Warn("Failed to load providers config for resolver", "error", err)
	} else {
		c.LLMResolver = llm.NewResolver(providersCfg, logger.With("component", "resolver"))
		logger.Debug("LLM resolver initialized")
	}

	// Initialize skills system
	if cfg.Skills.Enabled {
		c.initializeSkills(cfg, logger)
	}

	// Initialize templates system (shares the skills enabled flag since
	// templates use the same discovery pattern).
	c.initializeTemplates(cfg, logger)

	// Create shadow training manager early (before agent loop) so it can be injected
	if cfg.Shadow.Enabled {
		shadowCfg := convertShadowConfig(cfg.Shadow)
		shadowMgr, err := shadow.NewManager(shadow.ManagerConfig{
			Config:     shadowCfg,
			PrimaryLLM: c.LLMClient,
			Logger:     logger.With("component", "shadow"),
		})
		if err != nil {
			logger.Error("Failed to create shadow manager", "error", err)
		} else {
			c.ShadowManager = shadowMgr
			logger.Info("Shadow training manager initialized",
				"data_dir", shadowCfg.DataDir,
				"teacher_model", shadowCfg.Teacher.Model,
			)
		}
	}

	// Create multi-provider LLM manager if multiple providers configured
	providerCount := len(c.ModelsConfig.Providers)
	if providerCount > 1 {
		providerConfigs := buildProviderConfigs(c.ModelsConfig, logger)
		if len(providerConfigs) > 1 {
			pmCfg := llm.ProviderManagerConfig{
				Providers: providerConfigs,
				Logger:    logger.With("component", "provider-manager"),
			}
			c.LLMProvider = llm.NewProviderManager(pmCfg)
			logger.Info("Multi-provider LLM manager initialized",
				"providers", len(providerConfigs),
			)
		}
	}
	// Fall back to single client if no provider manager
	if c.LLMProvider == nil && c.LLMClient != nil {
		c.LLMProvider = c.LLMClient
	}

	// Create learning pipeline
	if cfg.SelfImprove.Enabled {
		lpCfg := selfimprove.DefaultLearningConfig()
		dataDir := cfg.SelfImprove.DataDir
		if dataDir == "" {
			dataDir = filepath.Join(cfg.Daemon.DataDir, "learning")
		}
		c.LearningPipeline = selfimprove.NewLearningPipeline(lpCfg, c.LLMClient, dataDir, logger.With("component", "learning"))
		if err := c.LearningPipeline.Initialize(context.Background()); err != nil {
			logger.Error("Failed to initialize learning pipeline", "error", err)
			c.LearningPipeline = nil
		} else {
			logger.Info("Learning pipeline initialized", "data_dir", dataDir)
		}

		// Create self-improve controller for full 5-phase cycles
		siDataPath := cfg.SelfImprove.DataDir
		if siDataPath == "" {
			siDataPath = filepath.Join(cfg.Daemon.DataDir, "selfimprove")
		}
		siCfg := selfimprove.DefaultConfig()
		siCfg.Enabled = true
		siCfg.DataPath = siDataPath
		siCfg.MaxIterationsPerCycle = cfg.SelfImprove.MaxIterationsPerCycle
		siCfg.MaxFixesPerCycle = cfg.SelfImprove.MaxFixesPerCycle
		siCfg.Safety.RequireHumanApproval = cfg.SelfImprove.Safety.RequireHumanApproval

		c.SelfImproveCtrl = selfimprove.NewController(
			siCfg,
			msgBus,
			c.LLMClient,
			"",
			logger.With("component", "selfimprove"),
		)
		if err := c.SelfImproveCtrl.Initialize(context.Background()); err != nil {
			logger.Error("Failed to initialize self-improve controller", "error", err)
			c.SelfImproveCtrl = nil
		} else {
			logger.Info("Self-improve controller initialized", "data_path", siDataPath)
		}

		// Start scheduler if interval is configured
		if cfg.SelfImprove.AutoRunIntervalHours > 0 && c.SelfImproveCtrl != nil {
			interval := time.Duration(cfg.SelfImprove.AutoRunIntervalHours) * time.Hour
			c.SelfImproveSched = selfimprove.NewScheduler(
				c.SelfImproveCtrl,
				interval,
				logger.With("component", "selfimprove.scheduler"),
			)
			// Note: Scheduler.Start() is called in Components.Start(ctx) with proper context
		}
	}

	// Create result cache if enabled in config
	if cfg.Agent.Cache.Enabled {
		cacheConfig := agent.DefaultCacheConfig()

		// Override with config values if specified
		if cfg.Agent.Cache.MaxEntries > 0 {
			cacheConfig.MaxEntries = cfg.Agent.Cache.MaxEntries
		}
		if cfg.Agent.Cache.DefaultTTLSeconds > 0 {
			cacheConfig.DefaultTTL = time.Duration(cfg.Agent.Cache.DefaultTTLSeconds) * time.Second
		}
		if cfg.Agent.Cache.CleanupFreqSeconds > 0 {
			cacheConfig.CleanupFreq = time.Duration(cfg.Agent.Cache.CleanupFreqSeconds) * time.Second
		}
		if len(cfg.Agent.Cache.EnabledTools) > 0 {
			cacheConfig.EnabledTools = cfg.Agent.Cache.EnabledTools
		}

		c.ResultCache = agent.NewResultCache(cacheConfig, logger.With("component", "cache"))
		logger.Info("Result cache enabled", "max_entries", cacheConfig.MaxEntries)
	} else {
		logger.Info("Result cache disabled")
	}

	// Create agent validation components
	c.Watchdog = agent.NewWatchdog(cfg.Agent.Watchdog, logger.With("component", "watchdog"))
	logger.Info("Watchdog initialized",
		"enabled", cfg.Agent.Watchdog.Enabled,
		"timeout_min", cfg.Agent.Watchdog.TimeoutMinutes,
		"heartbeat_interval", cfg.Agent.Watchdog.HeartbeatIntervalSec,
	)

	c.HallucinationDetector = agent.NewHallucinationDetector(
		agent.DefaultHallucinationConfig(),
		logger.With("component", "hallucination-detector"),
	)
	logger.Info("Hallucination detector initialized")

	c.ArtifactManager = agent.NewArtifactManager(logger.With("component", "artifact-manager"))
	logger.Info("Artifact manager initialized")

	// Create agent loop
	agentOpts := []agent.LoopOption{
		agent.WithMessageBus(msgBus),
		agent.WithLoopLogger(logger),
	}
	// Use provider manager if available, otherwise use client directly
	if c.LLMProvider != nil {
		agentOpts = append(agentOpts, agent.WithLLMChatter(c.LLMProvider))
	} else if c.LLMClient != nil {
		agentOpts = append(agentOpts, agent.WithLLMClient(c.LLMClient))
	}
	if c.SecurityChecker != nil {
		agentOpts = append(agentOpts, agent.WithSecurityChecker(c.SecurityChecker))
	}
	if c.SecurityOrchestrator != nil {
		agentOpts = append(agentOpts, agent.WithSecurityOrchestrator(c.SecurityOrchestrator))
		logger.Info("Agent loop configured with security orchestrator")
	}
	if c.ToolRegistry != nil {
		agentOpts = append(agentOpts, agent.WithToolRegistry(c.ToolRegistry))
	}
	if c.ShadowManager != nil {
		agentOpts = append(agentOpts, agent.WithShadowManager(c.ShadowManager))
		logger.Info("Agent loop configured with shadow training")
	}
	// Wire learning pipeline for pattern extraction
	if c.LearningPipeline != nil {
		lpAdapter := &learningPipelineAdapter{pipeline: c.LearningPipeline}
		agentOpts = append(agentOpts, agent.WithLearningPipeline(lpAdapter))
		logger.Info("Agent loop configured with learning pipeline")
	}
	// Wire result cache
	if c.ResultCache != nil {
		agentOpts = append(agentOpts, agent.WithResultCache(c.ResultCache))
		logger.Info("Agent loop configured with result cache")
	}
	// Wire progress tracking
	if cfg.Agent.ProgressEnabled {
		agentOpts = append(agentOpts, agent.WithProgressEnabled(true))
		logger.Info("Agent loop configured with progress tracking")
	}
	// Wire watchdog for stuck/timeout monitoring
	if c.Watchdog != nil {
		agentOpts = append(agentOpts, agent.WithWatchdog(c.Watchdog))
		logger.Info("Agent loop configured with watchdog")
	}
	// Wire hallucination detection
	if c.HallucinationDetector != nil {
		agentOpts = append(agentOpts, agent.WithHallucinationDetector(c.HallucinationDetector))
		logger.Info("Agent loop configured with hallucination detection")
	}
	// Wire artifact manager for CLAUDE.md context injection
	if c.ArtifactManager != nil {
		agentOpts = append(agentOpts, agent.WithArtifactManager(c.ArtifactManager))
		logger.Info("Agent loop configured with artifact manager")
	}
	// Always set an agent ID for security checks - use "default" when multi-agent is disabled
	agentOpts = append(agentOpts, agent.WithAgentID("default"))

	// Wire typed event system and hooks
	hookRegistry := agent.NewHookRegistry(logger.With("component", "hook-registry"))
	emitter := agent.NewEventEmitter("default", msgBus, logger.With("component", "event-emitter"))

	// Register security hooks if security orchestrator is available
	if c.SecurityOrchestrator != nil {
		if beforeTC := agent.NewSecurityBeforeToolCall(c.SecurityOrchestrator); beforeTC != nil {
			hookRegistry.RegisterBeforeToolCall("security-before-tool", agent.HookPriorityCritical, beforeTC)
			logger.Info("Registered security BeforeToolCall hook")
		}
		if transformCtx := agent.NewSecurityTransformContext(c.SecurityOrchestrator); transformCtx != nil {
			hookRegistry.RegisterTransformContext("security-transform", agent.HookPriorityCritical, transformCtx)
			logger.Info("Registered security TransformContext hook")
		}
	}

	agentOpts = append(agentOpts, agent.WithEventEmitter(emitter), agent.WithHookRegistry(hookRegistry))

	// Note: memvid and taskStore are wired AFTER their initialization below
	c.AgentLoop = agent.NewAgentLoop(agentOpts...)
	// Wire context firewall settings from LLM config
	c.AgentLoop.SetContextFirewallConfig(cfg.LLM.ContextFirewall)

	// Start progress synthesizer for tiered agent activity summaries.
	// Subscribes to all agent events via wildcard and republishes condensed
	// SynthesizedProgressEvent messages on the "agent.progress.synthesized" topic.
	progressSynthesizer := agent.NewProgressSynthesizer(msgBus, c.LLMClient, logger.With("component", "progress-synthesizer"))
	progressSub := msgBus.Subscribe("progress-synthesizer", "agent.event.*")
	go func() {
		for msg := range progressSub.Channel {
			var event agent.AgentEvent
			if err := json.Unmarshal(msg.Payload, &event); err != nil {
				continue
			}
			synthesized := progressSynthesizer.Synthesize(event)
			if synthesized == nil {
				continue
			}
			payload, err := json.Marshal(synthesized)
			if err != nil {
				continue
			}
			synthMsg := &models.BusMessage{
				ID:        msg.ID + "-synth",
				Type:      models.MessageTypeEvent,
				Source:    "progress-synthesizer",
				Timestamp: synthesized.Timestamp,
				Payload:   payload,
			}
			msgBus.Publish("agent.progress.synthesized", synthMsg)
		}
	}()
	logger.Info("Progress synthesizer started, subscribed to agent.event.*")

	// Chat handler created later after dispatcher (if multi-agent enabled)

	// Create status handler with budget tracking
	statusOpts := []StatusHandlerOption{}
	if budgetTracker != nil {
		statusOpts = append(statusOpts, WithBudgetTracker(budgetTracker))
	}
	c.StatusHandler = NewStatusHandler(msgBus, logger, statusOpts...)

	// Create memory manager
	c.MemoryManager = memory.NewManager(memory.ManagerConfig{
		Config:            cfg.Memory,
		MemvidConfig:      cfg.Memvid,
		DistributedConfig: cfg.DistributedMemory,
		Logger:            logger.With("component", "memory"),
		Sanitizer:         c.SecurityOrchestrator.InputSanitizer(),
		SecurityConfig:    cfg.Memory.Security,
		LLM:               c.LLMProvider,
	})
	if err := c.MemoryManager.Initialize(context.Background()); err != nil {
		logger.Error("Failed to initialize memory manager", "error", err)
		// Non-fatal: daemon can run without memory
	} else {
		logger.Info("Memory manager initialized",
			"backend", c.MemoryManager.Backend(),
			"distributed", c.MemoryManager.IsDistributed(),
		)
		// Create memory handler to respond to memory.query and memory.recent bus messages
		c.MemoryHandler = memory.NewHandler(c.MemoryManager, msgBus, logger.With("component", "memory-handler"))

		// Wire prefetch callback to agent loop (Hermes pattern)
		// This enables background prefetching of memory context at turn completion
		prefetchCallback := func(query string, maxItems int) {
			c.MemoryManager.QueuePrefetch(query, maxItems)
		}
		c.AgentLoop.SetPrefetchCallback(prefetchCallback)
		logger.Info("Prefetch callback wired to agent loop")

		// Start prefetch service
		c.MemoryManager.StartPrefetchService(context.Background())
		logger.Info("Memory prefetch service started")
	}

	// Store the memvid client from memory manager if active, or create standalone
	if c.MemoryManager.IsMemvidActive() {
		c.MemvidClient = c.MemoryManager.MemvidClient()
		logger.Info("Using memvid client from memory manager", "endpoint", cfg.Memvid.Endpoint)
	} else if cfg.Memvid.Enabled {
		c.MemvidClient = memvid.NewClient(memvid.ClientConfig{
			Endpoint: cfg.Memvid.Endpoint,
			Zone:     "default",
			Timeout:  time.Duration(cfg.Memvid.Timeout) * time.Second,
		})
		logger.Info("Standalone memvid client initialized", "endpoint", cfg.Memvid.Endpoint)
	}

	// Create distributed memory sync manager if enabled
	if c.MemoryManager.IsDistributed() && c.MemvidClient != nil {
		syncMgr, err := memsync.NewSyncManager(memsync.SyncManagerConfig{
			Config:       cfg.DistributedMemory,
			LocalManager: c.MemoryManager,
			MemvidClient: c.MemvidClient,
			MessageBus:   msgBus,
			Logger:       logger.With("component", "sync"),
		})
		if err != nil {
			logger.Error("Failed to create sync manager", "error", err)
		} else {
			c.SyncManager = syncMgr
			c.SyncHandler = memsync.NewHandler(syncMgr, msgBus, logger.With("component", "sync-handler"))
			logger.Info("Distributed memory sync enabled",
				"mode", cfg.DistributedMemory.Mode,
				"hydrate_on_claim", cfg.DistributedMemory.Sync.HydrateOnClaim,
				"distill_on_complete", cfg.DistributedMemory.Sync.DistillOnComplete,
			)
		}
	}

	// Create session store (SQLite-backed for persistence)
	sessionsDB := filepath.Join(cfg.Daemon.DataDir, "sessions.db")
	sessionStore, err := session.NewSQLiteStore(sessionsDB, logger)
	if err != nil {
		// Fall back to in-memory store if SQLite fails
		logger.Warn("Failed to create SQLite session store, using in-memory", "error", err)
		c.SessionStore = session.NewMemoryStore(logger)
	} else {
		c.SessionStore = sessionStore
	}

	// Create session handler with summarizer if LLM is available
	sessionOpts := []session.HandlerOption{}
	var summarizer *session.Summarizer
	if c.LLMClient != nil {
		summarizerLLM := c.SummarizerClient
		if summarizerLLM == nil {
			summarizerLLM = c.LLMClient
		}
		summarizer = session.NewSummarizer(summarizerLLM, logger.With("component", "summarizer"))
		sessionOpts = append(sessionOpts, session.WithSummarizer(summarizer))
		logger.Info("Session summarizer enabled",
			"model_source", func() string {
				if c.SummarizerClient != nil {
					return "summarizer_model"
				}
				return "main_model"
			}(),
		)
	} else {
		logger.Warn("Session summarizer disabled - no LLM client available")
	}

	// Create branch manager if branching is enabled
	var branchMgr *session.BranchManager
	if cfg.Session.Branching {
		// Guard against typed-nil interface: only pass summarizer if non-nil.
		var branchSummarizer session.BranchSummarizer
		if summarizer != nil {
			branchSummarizer = summarizer
		}
		branchMgr = session.NewBranchManager(c.SessionStore, branchSummarizer, cfg.Session, logger.With("component", "branch-manager"))
		sessionOpts = append(sessionOpts, session.WithBranchManager(branchMgr))
		logger.Info("Branch manager enabled",
			"summary_threshold", cfg.Session.BranchSummaryThreshold,
		)
	}

	c.SessionHandler = session.NewHandler(c.SessionStore, msgBus, logger.With("component", "session"), sessionOpts...)

	// Wire session store to agent loop for persistence
	if c.AgentLoop != nil && c.SessionStore != nil && cfg.Session.Persistence {
		c.AgentLoop.SetSessionStore(c.SessionStore, cfg.Session)
		logger.Info("Session persistence wired to agent loop",
			"restore_message_limit", cfg.Session.RestoreMessageLimit,
		)
	}

	// Wire branch manager to agent loop for in-memory cache coordination
	// Reuse the same BranchManager instance created above.
	if c.AgentLoop != nil && cfg.Session.Persistence && cfg.Session.Branching && branchMgr != nil {
		c.AgentLoop.SetBranchManager(branchMgr)
		logger.Info("Branch navigation wired to agent loop")
	}

	// Create job queue
	queueDB := cfg.Queue.DBPath
	if queueDB == "" {
		queueDB = filepath.Join(cfg.Daemon.DataDir, "queue.db")
	}
	jobQueue, err := queue.NewPersistentQueue(queueDB, msgBus, logger)
	if err != nil {
		logger.Warn("Failed to create job queue", "error", err)
	} else {
		c.Queue = jobQueue
		c.QueueHandler = queue.NewHandler(jobQueue, msgBus, logger)
	}

	// Create task registry (before agent registry so task store can be shared)
	tasksDB := filepath.Join(cfg.Daemon.DataDir, "tasks.db")
	taskRegistry, err := task.NewRegistry(tasksDB, msgBus, logger)
	if err != nil {
		logger.Warn("Failed to create task registry", "error", err)
	} else {
		c.TaskRegistry = taskRegistry
		c.TaskHandler = task.NewHandler(taskRegistry, msgBus, logger)
		c.AmendmentMgr = taskRegistry.AmendmentManager()

		// Wire up queue with task cancellation callback for interrupt-aware job claiming
		if c.Queue != nil && taskRegistry.InterruptManager() != nil {
			c.Queue.(*queue.PersistentQueue).SetTaskCancelledCallback(func(taskID string) (bool, string) {
				token, exists := taskRegistry.InterruptManager().Get(taskID)
				if !exists {
					return false, ""
				}
				if token.IsTriggered() {
					return true, string(token.Reason())
				}
				return false, ""
			})
			logger.Info("Queue interrupt-aware claiming enabled")
		}
	}

	// Initialize MCP manager and register MCP tools
	if cfg.MCP.Enabled {
		c.MCPManager = mcp.NewManager(logger.With("component", "mcp"))

		// Load MCP servers config
		mcpCfg, err := config.LoadMCPConfig(cfg.MCP.ConfigFile)
		switch {
		case err != nil:
			logger.Warn("Failed to load MCP config", "error", err, "path", cfg.MCP.ConfigFile)
		case len(mcpCfg.Servers) > 0:
			logger.Info("Starting MCP servers", "count", len(mcpCfg.Servers))
			for _, serverCfg := range mcpCfg.Servers {
				if err := c.MCPManager.StartServer(context.Background(), serverCfg); err != nil {
					logger.Error("Failed to start MCP server",
						"name", serverCfg.Name,
						"error", err,
					)
					continue
				}
			}

			// Register MCP tools with the tool registry
			registerMCPTools(c.ToolRegistry, c.MCPManager, logger)
		default:
			logger.Info("MCP enabled but no servers configured")
		}
	}

	// Register builtin tools now that all dependencies are available
	var taskStore *task.Store
	if c.TaskRegistry != nil {
		taskStore = c.TaskRegistry.Store()
	}
	registerBuiltinTools(c.ToolRegistry, c.SecurityChecker, c.SecurityOrchestrator, c.MemoryManager, taskStore, c.Scheduler, logger)

	// Initialize code intelligence if enabled
	if cfg.CodeIntel.Enabled {
		c.initializeCodeIntel(cfg, logger)
	}

	// Initialize calendar integration if enabled
	if cfg.Calendar.Enabled {
		c.initializeCalendar(cfg, msgBus, logger)
	}

	// Wire memvid client and task store to the main agent loop now that they're available
	if c.MemvidClient != nil {
		c.AgentLoop.SetMemvidClient(c.MemvidClient)
		logger.Debug("Wired memvid client to main agent loop")
	}
	if taskStore != nil {
		c.AgentLoop.SetTaskStore(taskStore)
		logger.Debug("Wired task store to main agent loop")
	}
	// Wire skill discovery to the main agent loop
	if c.CapabilityIndex != nil {
		c.AgentLoop.SetCapabilityIndex(c.CapabilityIndex)
		logger.Info("Agent loop configured with capability index",
			"keywords", c.CapabilityIndex.KeywordCount(),
		)
	}
	if c.SkillLoader != nil {
		c.AgentLoop.SetSkillLoader(c.SkillLoader)
		logger.Info("Agent loop configured with skill loader",
			"cache_size", c.SkillLoader.CacheSize(),
		)
	}

	// Create agent registry if multi-agent is enabled
	if cfg.MultiAgent.Enabled {

		var taskStore *task.Store
		if c.TaskRegistry != nil {
			taskStore = c.TaskRegistry.Store()
		}

		c.AgentRegistry = agent.NewAgentRegistry(agent.RegistryConfig{
			MemvidClient:          c.MemvidClient,
			TaskStore:             taskStore,
			LLMClient:             c.LLMClient,
			Resolver:              c.LLMResolver,
			MessageBus:            msgBus,
			SecurityChecker:       c.SecurityChecker,
			ToolRegistry:          c.ToolRegistry,
			ShadowManager:         c.ShadowManager,
			Logger:                logger,
			BundledAgentsPath:     "config/agents",
			Watchdog:              c.Watchdog,
			HallucinationDetector: c.HallucinationDetector,
			ArtifactManager:       c.ArtifactManager,
			Queues:                cfg.Agent.Queues,
			DB:                    getQueueDB(c),
		})
		logger.Info("Agent registry initialized", "specs", len(c.AgentRegistry.ListSpecs()))

		// Wire skill discovery to registry so all specialist agents get it
		if c.CapabilityIndex != nil {
			c.AgentRegistry.SetCapabilityIndex(c.CapabilityIndex)
		}
		if c.SkillLoader != nil {
			c.AgentRegistry.SetSkillLoader(c.SkillLoader)
		}

		// Build capabilities map from agent specs and skill metadata
		capBuilder := agent.NewCapabilitiesBuilder(c.SkillIndex, logger.With("component", "capabilities-builder"))
		capMap, err := capBuilder.Build(c.AgentRegistry.ListSpecs())
		if err != nil {
			logger.Warn("Failed to build capabilities map", "error", err)
		} else {
			c.CapabilitiesMap = capMap
			c.AgentRegistry.SetCapabilitiesMap(capMap)
			logger.Info("Capabilities map built",
				"agents", capMap.Count(),
				"intent_types", len(capMap.AllIntentTypes()),
				"keywords", len(capMap.AllKeywords()),
			)
		}

		// Create capability matcher for fast routing
		var capMatcher *agent.CapabilityMatcher
		if c.CapabilitiesMap != nil {
			capMatcher = agent.NewCapabilityMatcher(agent.CapabilityMatcherConfig{
				CapabilitiesMap: c.CapabilitiesMap,
				CapabilityIndex: c.CapabilityIndex,
				Logger:          logger.With("component", "capability-matcher"),
			})
			if c.CapabilityIndex != nil {
				logger.Debug("Capability matcher initialized with capability index",
					"keywords", c.CapabilityIndex.KeywordCount(),
				)
			} else {
				logger.Debug("Capability matcher initialized without capability index")
			}
		}

		// Create dispatcher with capability matcher
		c.Dispatcher = agent.NewDispatcher(agent.DispatcherConfig{
			Registry:          c.AgentRegistry,
			MemvidClient:      c.MemvidClient,
			MemoryMgr:         c.MemoryManager,
			TaskStore:         taskStore,
			TaskRegistry:      c.TaskRegistry,
			AmendmentManager:  c.AmendmentMgr,
			SkillRegistry:     c.SkillRegistry,
			SkillExecutor:     c.SkillExecutor,
			TemplateRegistry:  c.TemplateRegistry,
			Logger:            logger.With("component", "dispatcher"),
			CapabilityMatcher: capMatcher,
			LLMClient:         c.LLMClient,
			ClassifierClient:  c.ClassifierClient,
			ClassifierModel:   c.Config.MultiAgent.ClassifierModel,
			ClassifierTimeout: 15 * time.Second, // Generous timeout for classifier; avoids cascade to weak keyword fallback.
			SessionMaxAge:     30 * time.Minute,
		})
		logger.Info("Dispatcher initialized", "has_capability_matcher", capMatcher != nil)

		// Register platform tools now that agent registry is available
		registerPlatformTools(c.ToolRegistry, c.AgentRegistry, c.StatusHandler, logger)

		// Register template tools if template registry is available
		registerTemplateTools(c.ToolRegistry, c.TemplateRegistry, logger)

		// Create chat handler with dispatcher for multi-agent routing
		c.ChatHandler = agent.NewChatHandler(c.AgentLoop, c.Dispatcher, msgBus, logger)

		// Wire step store for fetching step summaries in ACK and completion messages
		if c.TaskRegistry != nil {
			c.ChatHandler.SetStepStore(c.TaskRegistry.StepStore())
			c.ChatHandler.SetTaskStore(c.TaskRegistry.Store())
		}

		logger.Info("ChatHandler initialized with dispatcher")

		// Subscribe to dispatcher.stats requests
		statsSub := msgBus.Subscribe("dispatcher-stats-handler", "dispatcher.stats")
		go func() {
			for msg := range statsSub.Channel {
				stats := c.Dispatcher.GetStats()
				payload, _ := json.Marshal(&stats)
				resp := &models.BusMessage{
					ID:        msg.ID + "-response",
					Type:      models.MessageTypeResponse,
					Topic:     "dispatcher.stats.result",
					Source:    "dispatcher-stats-handler",
					Timestamp: time.Now().UTC(),
					Payload:   payload,
					ReplyTo:   msg.ID,
				}
				msgBus.Publish("dispatcher.stats.result", resp)
			}
		}()

		// Create orchestrator components if task registry and queue are available
		if c.TaskRegistry != nil && c.Queue != nil {
			stepStore := c.TaskRegistry.StepStore()
			orchTaskStore := c.TaskRegistry.Store()

			strategicPlanner := agent.NewStrategicPlanner(agent.StrategicPlannerConfig{
				Registry:       c.AgentRegistry,
				TaskStore:      orchTaskStore,
				StepStore:      stepStore,
				Bus:            msgBus,
				Logger:         logger.With("component", "strategic"),
				MaxPlanSteps:   cfg.Orchestrator.MaxPlanSteps,
				PlannerTimeout: time.Duration(cfg.Orchestrator.PlannerTimeout) * time.Second,
			})

			reviewManager := agent.NewReviewManager(agent.ReviewManagerConfig{
				Registry:  c.AgentRegistry,
				StepStore: stepStore,
				TaskStore: orchTaskStore,
				Bus:       msgBus,
				Logger:    logger.With("component", "review"),
			})
			c.ReviewManager = reviewManager

			// Create escalation manager for automatic re-planning on failures
			c.EscalationManager = agent.NewEscalationManager(agent.EscalationManagerConfig{
				Config:    agent.DefaultEscalationConfig(),
				Planner:   strategicPlanner,
				TaskStore: orchTaskStore,
				Bus:       msgBus,
				Logger:    logger.With("component", "escalation"),
			})
			logger.Info("Escalation manager initialized")

			tacticalScheduler := agent.NewTacticalScheduler(agent.TacticalSchedulerConfig{
				StepStore:         stepStore,
				TaskStore:         orchTaskStore,
				Queue:             c.Queue,
				Registry:          c.AgentRegistry,
				Bus:               msgBus,
				Logger:            logger.With("component", "tactical"),
				ReviewManager:     reviewManager,
				EscalationManager: c.EscalationManager,
			})

			c.Orchestrator = agent.NewOrchestrator(agent.OrchestratorDeps{
				Strategic: strategicPlanner,
				Tactical:  tacticalScheduler,
				Bus:       msgBus,
				Logger:    logger.With("component", "orchestrator"),
			})

			logger.Info("Orchestrator initialized with strategic and tactical layers")
		}
	} else {
		// Create chat handler without dispatcher (single-agent mode)
		c.ChatHandler = agent.NewChatHandler(c.AgentLoop, nil, msgBus, logger)

		// Wire step store for fetching step summaries in ACK and completion messages
		if c.TaskRegistry != nil {
			c.ChatHandler.SetStepStore(c.TaskRegistry.StepStore())
			c.ChatHandler.SetTaskStore(c.TaskRegistry.Store())
		}
	}

	// Create job processor that uses the agent loop (with optional multi-agent registry)
	jobProc := NewAgentJobProcessor(c.AgentLoop, logger)
	if c.AgentRegistry != nil {
		jobProc.WithRegistry(c.AgentRegistry)
	}
	c.JobProcessor = jobProc

	// Create worker pool
	if c.Queue != nil && c.JobProcessor != nil {
		workerPool, err := worker.NewPool(worker.PoolConfig{
			Queue:       c.Queue,
			Processor:   c.JobProcessor,
			MessageBus:  msgBus,
			Logger:      logger,
			DefaultCaps: cfg.Workers.DefaultCaps,
			IdleTimeout: time.Duration(cfg.Workers.IdleTimeoutSeconds) * time.Second,
		})
		if err != nil {
			logger.Warn("Failed to create worker pool", "error", err)
		} else {
			c.WorkerPool = workerPool
			c.WorkerHandler = worker.NewHandler(workerPool, msgBus, logger)
		}
	}

	// Create scheduler with job dependencies for extended job types
	if cfg.Scheduler.Enabled {
		schedOpts := make([]scheduler.Option, 0, 3)
		schedOpts = append(schedOpts,
			scheduler.WithDataDir(cfg.Daemon.DataDir),
			scheduler.WithLogger(logger.With("component", "scheduler")),
		)

		// Build job dependencies for optimization, security, and learning jobs
		jobDeps := &scheduler.JobDependencies{
			Bus: msgBus,
		}
		if c.MemoryManager != nil {
			jobDeps.MemoryManager = &scheduler.MemoryOptimizerAdapter{
				UpdateMetricsFn: c.MemoryManager.UpdateGraphMetrics,
				ConsolidateFn: func(ctx context.Context) error {
					_, err := c.MemoryManager.Consolidate(ctx)
					return err
				},
			}
		}
		if c.LearningPipeline != nil {
			jobDeps.LearningPipeline = &scheduler.LearningConsolidatorAdapter{
				ConsolidateFn: func(ctx context.Context) error {
					_, err := c.LearningPipeline.Consolidate(ctx)
					return err
				},
			}
		}
		schedOpts = append(schedOpts, scheduler.WithJobDependencies(jobDeps))

		sched, err := scheduler.NewScheduler(cfg.Scheduler, msgBus, schedOpts...)
		if err != nil {
			logger.Warn("Failed to create scheduler", "error", err)
		} else {
			c.Scheduler = sched
			logger.Info("Scheduler initialized",
				"timezone", cfg.Scheduler.Timezone,
			)
		}
	}

	// Create web server if enabled
	if cfg.Web.Enabled {
		webCfg := web.ServerConfig{
			Addr:         fmt.Sprintf("%s:%d", cfg.Web.Host, cfg.Web.Port),
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			EnableCORS:   true,
		}

		// Create web handler adapter (implements web.Handler interface)
		webHandler := &webHandlerAdapter{
			agentLoop:     c.AgentLoop,
			statusHandler: c.StatusHandler,
		}

		// Create authenticator
		var auth web.Authenticator
		if cfg.Web.SecretKey != "" {
			auth = web.NewBearerAuth(cfg.Web.SecretKey)
		}

		// Collect server options with adapters
		webOpts := []web.ServerOption{}

		// Wire memory searcher if available
		if c.MemoryManager != nil {
			webOpts = append(webOpts, web.WithMemorySearcher(&memorySearcherAdapter{mgr: c.MemoryManager}))
		}

		// Wire skills lister if available
		if c.SkillRegistry != nil {
			webOpts = append(webOpts, web.WithSkillsLister(&skillsListerAdapter{registry: c.SkillRegistry}))
		}

		// Wire jobs lister if available
		if c.Scheduler != nil {
			webOpts = append(webOpts, web.WithJobsLister(&jobsListerAdapter{scheduler: c.Scheduler}))
		}

		c.WebServer = web.NewServer(webCfg, webHandler, auth, logger.With("component", "web"), webOpts...)
		logger.Info("Web server configured",
			"addr", webCfg.Addr,
			"has_memory", c.MemoryManager != nil,
			"has_skills", c.SkillRegistry != nil,
			"has_jobs", c.Scheduler != nil,
		)
	}

	// Create Telegram bot if enabled
	if cfg.Telegram.Enabled {
		tgDataDir := filepath.Join(cfg.Daemon.DataDir, "telegram")
		tgHandler := telegram.NewAgentHandler(c.SessionStore, c.AgentLoop, tgDataDir, logger.With("component", "telegram-handler"))

		// Resolve token from environment variable if it starts with ${
		tgToken := cfg.Telegram.Token
		if strings.HasPrefix(tgToken, "${") && strings.HasSuffix(tgToken, "}") {
			envVar := tgToken[2 : len(tgToken)-1]
			tgToken = os.Getenv(envVar)
		}

		botCfg := telegram.BotConfig{
			Token:        tgToken,
			AllowedUsers: cfg.Telegram.AllowedUsers,
			AllowedChats: cfg.Telegram.AllowedChats,
			PollTimeout:  cfg.Telegram.PollTimeout,
		}

		bot, err := telegram.NewBot(botCfg, tgHandler.Handle, logger.With("component", "telegram"))
		if err != nil {
			logger.Error("failed to create telegram bot", "error", err)
		} else {
			bot.SetResetter(tgHandler)
			c.TelegramBot = bot
			c.TelegramHandler = tgHandler
			logger.Info("Telegram bot configured",
				"allowed_users", len(cfg.Telegram.AllowedUsers),
				"allowed_chats", len(cfg.Telegram.AllowedChats),
			)
		}
	}

	return c, nil
}

// Start starts all components that need background processing.
func (c *Components) Start(ctx context.Context) error {
	// Start chat handler
	if err := c.ChatHandler.Start(ctx); err != nil {
		return err
	}

	// Start status handler
	if err := c.StatusHandler.Start(ctx); err != nil {
		return err
	}

	// Start session handler
	if err := c.SessionHandler.Start(ctx); err != nil {
		return err
	}

	// Start memory handler
	if c.MemoryHandler != nil {
		if err := c.MemoryHandler.Start(ctx); err != nil {
			c.Logger.Error("Failed to start memory handler", "error", err)
		}
	}

	// Start result cache cleanup goroutine
	if c.ResultCache != nil {
		c.ResultCache.Start()
		c.Logger.Debug("Result cache cleanup started")
	}

	// Start queue handler
	if c.QueueHandler != nil {
		if err := c.QueueHandler.Start(ctx); err != nil {
			c.Logger.Error("Failed to start queue handler", "error", err)
		}
	}

	// Start task handler
	if c.TaskHandler != nil {
		if err := c.TaskHandler.Start(ctx); err != nil {
			c.Logger.Error("Failed to start task handler", "error", err)
		}
	}

	// Start worker handler
	if c.WorkerHandler != nil {
		if err := c.WorkerHandler.Start(ctx); err != nil {
			c.Logger.Error("Failed to start worker handler", "error", err)
		}
	}

	// Start worker pool
	if c.WorkerPool != nil {
		poolSize := c.Config.Workers.PoolSize
		if poolSize <= 0 {
			poolSize = 4
		}
		if err := c.WorkerPool.Start(ctx, poolSize); err != nil {
			c.Logger.Error("Failed to start worker pool", "error", err)
		}
	}

	// Start scheduler
	if c.Scheduler != nil {
		if err := c.Scheduler.Start(ctx); err != nil {
			c.Logger.Error("Failed to start scheduler", "error", err)
		}
	}

	// Start orchestrator
	if c.Orchestrator != nil {
		if err := c.Orchestrator.Start(ctx); err != nil {
			c.Logger.Error("Failed to start orchestrator", "error", err)
		}
	}

	// Start watchdog monitor for agent stuck/timeout detection
	if c.Watchdog != nil {
		c.Watchdog.Start(ctx)
	}

	// Start self-improve scheduler (if configured)
	if c.SelfImproveSched != nil {
		go c.SelfImproveSched.Start(ctx)
		c.Logger.Info("Self-improve scheduler started")
	}

	// Start calendar reminder watcher (if configured)
	if c.CalendarReminder != nil {
		go c.CalendarReminder.Start(ctx)
		c.Logger.Info("Calendar reminder watcher started")
	}

	// Start sync manager and handler
	if c.SyncManager != nil {
		if err := c.SyncManager.Start(ctx); err != nil {
			c.Logger.Error("Failed to start sync manager", "error", err)
		}
	}
	if c.SyncHandler != nil {
		if err := c.SyncHandler.Start(ctx); err != nil {
			c.Logger.Error("Failed to start sync handler", "error", err)
		}
	}

	// Start web server (in background goroutine - it blocks)
	if c.WebServer != nil {
		go func() {
			if err := c.WebServer.Start(ctx); err != nil {
				c.Logger.Error("Web server error", "error", err)
			}
		}()
		c.Logger.Info("Web server started")
	}

	// Start Telegram bot
	if c.TelegramBot != nil {
		go func() {
			c.Logger.Info("Starting Telegram bot")
			if err := c.TelegramBot.Start(ctx); err != nil && ctx.Err() == nil {
				c.Logger.Error("Telegram bot error", "error", err)
			}
		}()
	}

	return nil
}

// Stop stops all components.
func (c *Components) Stop(ctx context.Context) error {
	var lastErr error

	// Stop web server first (external API)
	if c.WebServer != nil {
		if err := c.WebServer.Shutdown(ctx); err != nil {
			c.Logger.Error("Failed to stop web server", "error", err)
			lastErr = err
		}
	}

	// Stop Telegram bot
	if c.TelegramBot != nil {
		c.TelegramBot.Stop()
		c.Logger.Info("Telegram bot stopped")
	}

	// Stop sync handler and manager first (depends on queue events)
	if c.SyncHandler != nil {
		if err := c.SyncHandler.Stop(ctx); err != nil {
			c.Logger.Error("Failed to stop sync handler", "error", err)
			lastErr = err
		}
	}
	if c.SyncManager != nil {
		if err := c.SyncManager.Stop(); err != nil {
			c.Logger.Error("Failed to stop sync manager", "error", err)
			lastErr = err
		}
	}

	// Stop orchestrator before scheduler and queue
	if c.Orchestrator != nil {
		if err := c.Orchestrator.Stop(ctx); err != nil {
			c.Logger.Error("Failed to stop orchestrator", "error", err)
			lastErr = err
		}
	}

	// Stop watchdog monitor
	if c.Watchdog != nil {
		c.Watchdog.Stop()
	}

	// Stop scheduler first to prevent new job executions
	if c.Scheduler != nil {
		if err := c.Scheduler.Stop(ctx); err != nil {
			c.Logger.Error("Failed to stop scheduler", "error", err)
			lastErr = err
		}
	}

	// Stop worker pool to prevent new work
	if c.WorkerPool != nil {
		if err := c.WorkerPool.Stop(ctx); err != nil {
			c.Logger.Error("Failed to stop worker pool", "error", err)
			lastErr = err
		}
	}

	// Stop handlers
	if c.WorkerHandler != nil {
		if err := c.WorkerHandler.Stop(ctx); err != nil {
			lastErr = err
		}
	}

	if c.TaskHandler != nil {
		if err := c.TaskHandler.Stop(ctx); err != nil {
			lastErr = err
		}
	}

	if c.QueueHandler != nil {
		if err := c.QueueHandler.Stop(ctx); err != nil {
			lastErr = err
		}
	}

	if c.ChatHandler != nil {
		if err := c.ChatHandler.Stop(ctx); err != nil {
			lastErr = err
		}
	}

	if c.StatusHandler != nil {
		if err := c.StatusHandler.Stop(ctx); err != nil {
			lastErr = err
		}
	}

	if c.SessionHandler != nil {
		if err := c.SessionHandler.Stop(ctx); err != nil {
			lastErr = err
		}
	}

	if c.MemoryHandler != nil {
		if err := c.MemoryHandler.Stop(ctx); err != nil {
			lastErr = err
		}
	}

	// Close stores
	if c.TaskRegistry != nil {
		if err := c.TaskRegistry.Close(); err != nil {
			lastErr = err
		}
	}

	if c.Queue != nil {
		if err := c.Queue.Close(); err != nil {
			lastErr = err
		}
	}

	if c.SessionStore != nil {
		if err := c.SessionStore.Close(); err != nil {
			lastErr = err
		}
	}

	if c.MemoryManager != nil {
		if err := c.MemoryManager.Close(); err != nil {
			c.Logger.Error("Failed to close memory manager", "error", err)
			lastErr = err
		}
	}

	if c.LLMClient != nil {
		c.LLMClient.Close()
	}

	if c.AgentRegistry != nil {
		c.AgentRegistry.Close()
	}

	if c.ShadowManager != nil {
		if err := c.ShadowManager.Close(); err != nil {
			c.Logger.Error("Failed to close shadow manager", "error", err)
			lastErr = err
		}
	}

	// Close learning pipeline
	if c.LearningPipeline != nil {
		if err := c.LearningPipeline.Close(); err != nil {
			c.Logger.Error("Failed to close learning pipeline", "error", err)
			lastErr = err
		}
	}

	// Stop self-improve scheduler and controller
	if c.SelfImproveSched != nil {
		c.SelfImproveSched.Stop()
		c.Logger.Info("Self-improve scheduler stopped")
	}
	if c.SelfImproveCtrl != nil {
		if err := c.SelfImproveCtrl.Stop(); err != nil {
			c.Logger.Error("Failed to stop self-improve controller", "error", err)
			lastErr = err
		}
	}

	// Stop all MCP server connections
	if c.MCPManager != nil {
		c.MCPManager.StopAll()
	}

	// Stop all LSP server connections
	if c.LSPManager != nil {
		if err := c.LSPManager.StopAll(ctx); err != nil {
			c.Logger.Error("Failed to stop LSP servers", "error", err)
			lastErr = err
		}
	}

	// Stop calendar reminder watcher
	if c.CalendarReminder != nil {
		c.CalendarReminder.Stop()
		c.Logger.Debug("Calendar reminder watcher stopped")
	}

	// Stop result cache cleanup goroutine
	if c.ResultCache != nil {
		c.ResultCache.Stop()
		c.Logger.Debug("Result cache cleanup stopped")
	}

	return lastErr
}

// loadModelsConfigWithPath loads models config and returns the path it was loaded from.
func loadModelsConfigWithPath(logger *slog.Logger) (*config.ModelsConfig, string, error) {
	// Try project-local first
	localPath := "config/models.json5"
	if _, err := os.Stat(localPath); err == nil {
		logger.Debug("Found models config", "path", localPath)
		cfg, err := config.LoadModelsConfig(localPath)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load %s: %w", localPath, err)
		}
		return cfg, localPath, nil
	}

	// Try user home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get home directory: %w", err)
	}

	homePath := filepath.Join(homeDir, ".meept", "models.json5")
	if _, err := os.Stat(homePath); err == nil {
		logger.Debug("Found models config", "path", homePath)
		cfg, err := config.LoadModelsConfig(homePath)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load %s: %w", homePath, err)
		}
		return cfg, homePath, nil
	}

	return nil, "", fmt.Errorf("models.json5 not found in config/ or ~/.meept/")
}

// getProviderNames returns a list of configured provider names.
func getProviderNames(cfg *config.ModelsConfig) []string {
	names := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		names = append(names, name)
	}
	return names
}

// getQueueDB extracts the *sql.DB from the job queue for queue persistence.
// Returns nil if the queue is not a PersistentQueue or has no DB.
func getQueueDB(c *Components) *sql.DB {
	if c.Queue == nil {
		return nil
	}
	if pq, ok := c.Queue.(*queue.PersistentQueue); ok {
		return pq.DB()
	}
	return nil
}

// createLLMConfig creates an LLM model config from the models configuration.
// Returns the config and logs detailed information about the resolution.
func createLLMConfig(cfg *config.ModelsConfig, logger *slog.Logger) *llm.ModelConfig {
	if cfg == nil {
		logger.Error("Cannot create LLM config: models configuration is nil")
		return nil
	}

	// Find the default model
	modelRef := cfg.Model
	if modelRef == "" {
		logger.Error("Cannot create LLM config: no default model specified in config")
		return nil
	}

	return resolveModelRef(cfg, modelRef, logger)
}

// createAuxiliaryLLMClient creates an LLM client for an auxiliary role (classifier,
// summarizer). If modelRef is empty it returns nil so the caller can fall back to
// the main client. The returned client shares no state with the main client.
func createAuxiliaryLLMClient(cfg *config.ModelsConfig, modelRef string, logger *slog.Logger, budget *llm.Budget) *llm.Client {
	if modelRef == "" {
		return nil
	}
	llmCfg := resolveModelRef(cfg, modelRef, logger)
	if llmCfg == nil {
		return nil
	}
	opts := []llm.ClientOption{llm.WithLogger(logger)}
	if budget != nil {
		opts = append(opts, llm.WithBudget(budget))
	}
	return llm.NewClient(llmCfg, opts...)
}

// resolveModelRef resolves a "provider/model-id" reference into an LLM config.
func resolveModelRef(cfg *config.ModelsConfig, modelRef string, logger *slog.Logger) *llm.ModelConfig {
	logger.Info("Resolving model", "model_ref", modelRef)

	// Parse provider/model format
	var targetProvider, targetModel string
	if parts := splitModelRef(modelRef); len(parts) == 2 {
		targetProvider = parts[0]
		targetModel = parts[1]
		logger.Debug("Parsed model reference", "provider", targetProvider, "model", targetModel)
	} else {
		targetModel = modelRef
		logger.Debug("Model reference has no provider prefix, searching all providers", "model", targetModel)
	}

	// Search for the model in providers
	for providerID, provider := range cfg.Providers {
		// If provider is specified, only check that provider
		if targetProvider != "" && providerID != targetProvider {
			continue
		}

		for id, model := range provider.Models {
			if id != targetModel && model.Name != targetModel {
				continue
			}
			caps := make(map[string]bool)
			for _, capName := range model.Capabilities {
				caps[capName] = true
			}

			apiKey := provider.Options.APIKey
			//nolint:gosec // field name, not a secret
			hasKey := apiKey != "" && apiKey != "${GALA_API_KEY}" // Check for unexpanded env var

			logger.Info("Resolved model configuration",
				"provider", providerID,
				"model_id", id,
				"model_name", model.Name,
				"base_url", provider.Options.BaseURL,
				"has_api_key", hasKey,
				"capabilities", model.Capabilities,
				"context_limit", model.ContextLimit,
				"max_output", model.MaxOutput,
			)

			if !hasKey {
				logger.Warn("API key not set or not expanded",
					"expected_env", "GALA_API_KEY",
					"hint", "Set GALA_API_KEY environment variable",
				)
			}

			return &llm.ModelConfig{
				BaseURL:              provider.Options.BaseURL,
				ModelID:              model.Name, // Use the actual model name, not the config key
				APIKey:               apiKey,
				CostPerMillionInput:  model.InputCost,
				CostPerMillionOutput: model.OutputCost,
				MaxTokens:            model.MaxOutput,
				Temperature:          model.Temperature,
				ContextLimit:         model.ContextLimit,
				Capabilities:         caps,
				ProviderID:           providerID,
			}
		}
	}

	// Model not found - log all available models
	var available []string
	for providerID, provider := range cfg.Providers {
		for id := range provider.Models {
			available = append(available, providerID+"/"+id)
		}
	}
	logger.Error("Model not found in any provider",
		"requested", modelRef,
		"available_models", available,
	)

	return nil
}

// splitModelRef splits "provider/model" into parts.
func splitModelRef(ref string) []string {
	for i, c := range ref {
		if c == '/' {
			return []string{ref[:i], ref[i+1:]}
		}
	}
	return []string{ref}
}

// createSecurityOrchestrator creates a security orchestrator from configuration.
func createSecurityOrchestrator(cfg *config.Config, logger *slog.Logger) *intsecurity.Orchestrator {
	orchCfg := intsecurity.OrchestratorConfig{
		SanitizeInputs:     cfg.Security.SanitizeInputs,
		SanitizeStrictness: intsecurity.ParseStrictnessLevel(cfg.Security.SanitizeStrictness),
		MonitorOutput:      cfg.Security.MonitorOutput,
		RedactOutput:       cfg.Security.RedactOutput,
		ScanShellCommands:  cfg.Security.ScanShellCommands,
		TirithBinary:       cfg.Security.TirithBinary,
		EnableAuditLog:     cfg.Security.EnableAuditLog,
		AuditDBPath:        cfg.Security.AuditDBPath,
	}

	return intsecurity.NewOrchestrator(orchCfg, logger)
}

// convertShadowConfig converts config.ShadowConfig to shadow.Config.
func convertShadowConfig(cfg config.ShadowConfig) *shadow.Config {
	return &shadow.Config{
		Enabled: cfg.Enabled,
		DataDir: cfg.DataDir,
		Shadowing: shadow.ShadowingConfig{
			Mode:          shadow.ShadowMode(cfg.Shadowing.Mode),
			MinComplexity: shadow.Complexity(cfg.Shadowing.MinComplexity),
			Domains:       cfg.Shadowing.Domains,
			TaskTypes:     cfg.Shadowing.TaskTypes,
			SampleRate:    cfg.Shadowing.SampleRate,
			QueueSize:     cfg.Shadowing.QueueSize,
			WorkerCount:   cfg.Shadowing.WorkerCount,
		},
		Teacher: shadow.TeacherConfig{
			Model:             cfg.Teacher.Model,
			FallbackModel:     cfg.Teacher.FallbackModel,
			Temperature:       cfg.Teacher.Temperature,
			MaxTokens:         cfg.Teacher.MaxTokens,
			TimeoutSeconds:    cfg.Teacher.TimeoutSeconds,
			MaxDailyQueries:   cfg.Teacher.MaxDailyQueries,
			MaxDailyCost:      cfg.Teacher.MaxDailyCost,
			RequestsPerMinute: cfg.Teacher.RequestsPerMinute,
		},
		Quality: shadow.QualityConfig{
			Method:               shadow.QualityMethod(cfg.Quality.Method),
			HighQualityThreshold: cfg.Quality.HighQualityThreshold,
			TrainableThreshold:   cfg.Quality.TrainableThreshold,
			PreferenceMargin:     cfg.Quality.PreferenceMargin,
			HeuristicWeights: shadow.HeuristicWeights{
				Relevance:    cfg.Quality.HeuristicWeights.Relevance,
				Completeness: cfg.Quality.HeuristicWeights.Completeness,
				Correctness:  cfg.Quality.HeuristicWeights.Correctness,
				Style:        cfg.Quality.HeuristicWeights.Style,
			},
			EvalPromptTemplate: cfg.Quality.EvalPromptTemplate,
		},
		Examples: shadow.ExamplesConfig{
			Enabled:          cfg.Examples.Enabled,
			MaxPerCategory:   cfg.Examples.MaxPerCategory,
			MinQuality:       cfg.Examples.MinQuality,
			DefaultCount:     cfg.Examples.DefaultCount,
			MaxCount:         cfg.Examples.MaxCount,
			SimilarityWeight: cfg.Examples.SimilarityWeight,
			RecencyWeight:    cfg.Examples.RecencyWeight,
			QualityWeight:    cfg.Examples.QualityWeight,
			MaxContextTokens: cfg.Examples.MaxContextTokens,
		},
		Export: shadow.ExportConfig{
			OutputDir:                cfg.Export.OutputDir,
			Formats:                  cfg.Export.Formats,
			MinRecords:               cfg.Export.MinRecords,
			IncludeLowQuality:        cfg.Export.IncludeLowQuality,
			Deduplicate:              cfg.Export.Deduplicate,
			DedupSimilarityThreshold: cfg.Export.DedupSimilarityThreshold,
		},
		Adapters: shadow.AdaptersConfig{
			Enabled:        cfg.Adapters.Enabled,
			OllamaEndpoint: cfg.Adapters.OllamaEndpoint,
			AutoTrain:      cfg.Adapters.AutoTrain,
			TrainThreshold: cfg.Adapters.TrainThreshold,
			TrainSchedule:  cfg.Adapters.TrainSchedule,
			AdapterDir:     cfg.Adapters.AdapterDir,
			LoRA: shadow.LoRAConfig{
				Rank:                 cfg.Adapters.LoRA.Rank,
				Alpha:                cfg.Adapters.LoRA.Alpha,
				Dropout:              cfg.Adapters.LoRA.Dropout,
				TargetModules:        cfg.Adapters.LoRA.TargetModules,
				LearningRate:         cfg.Adapters.LoRA.LearningRate,
				Epochs:               cfg.Adapters.LoRA.Epochs,
				BatchSize:            cfg.Adapters.LoRA.BatchSize,
				GradientAccumulation: cfg.Adapters.LoRA.GradientAccumulation,
				WarmupRatio:          cfg.Adapters.LoRA.WarmupRatio,
				MaxGradNorm:          cfg.Adapters.LoRA.MaxGradNorm,
			},
			DPO: shadow.DPOConfig{
				Beta:     cfg.Adapters.DPO.Beta,
				LossType: cfg.Adapters.DPO.LossType,
			},
		},
	}
}

// registerBuiltinTools registers all builtin tools with the registry.
func registerBuiltinTools(
	registry *tools.Registry,
	checker *security.PermissionChecker,
	secOrch *intsecurity.Orchestrator,
	memoryMgr *memory.Manager,
	taskStore *task.Store,
	sched *scheduler.Scheduler,
	logger *slog.Logger,
) {
	// Filesystem tools
	registry.Register(builtin.NewReadFileTool(checker))
	registry.Register(builtin.NewWriteFileTool(checker))
	registry.Register(builtin.NewDeleteFileTool(checker))
	registry.Register(builtin.NewListDirectoryTool(checker))

	// Shell tool with security orchestrator for Tirith scanning
	wd, _ := os.Getwd()
	shellTool := builtin.NewShellExecuteTool(wd, 60*time.Second)
	if secOrch != nil {
		shellTool.SetSecurityOrchestrator(secOrch)
		logger.Debug("Shell tool configured with security orchestrator")
	}
	registry.Register(shellTool)

	// Web fetch tool
	registry.Register(builtin.NewWebFetchTool(30*time.Second, 100000))

	// Web search tool (DuckDuckGo)
	registry.Register(builtin.NewWebSearchTool(15 * time.Second))

	// Memory tools (only if memory manager is available AND successfully initialized)
	if memoryMgr != nil && memoryMgr.IsInitialized() {
		registry.Register(builtin.NewMemoryStoreTool(memoryMgr))
		registry.Register(builtin.NewMemorySearchTool(memoryMgr))
		registry.Register(builtin.NewMemoryGetContextTool(memoryMgr))
		registry.Register(builtin.NewMemoryGetVersionTool(memoryMgr))
		registry.Register(builtin.NewMemoryGetVersionHistoryTool(memoryMgr))
		logger.Debug("Registered memory tools")
	} else if memoryMgr != nil {
		logger.Warn("Memory tools not registered: memory manager not initialized")
	}

	// Task tools (only if task store is available)
	if taskStore != nil {
		registry.Register(builtin.NewTaskCreateTool(taskStore))
		registry.Register(builtin.NewTaskGetTool(taskStore))
		registry.Register(builtin.NewTaskListTool(taskStore))
		registry.Register(builtin.NewTaskUpdateTool(taskStore))
		logger.Debug("Registered task tools")
	}

	// Scheduler tools (only if scheduler is available)
	if sched != nil {
		registry.Register(builtin.NewScheduleCreateTool(sched))
		registry.Register(builtin.NewScheduleListTool(sched))
		registry.Register(builtin.NewScheduleGetTool(sched))
		registry.Register(builtin.NewScheduleDeleteTool(sched))
		registry.Register(builtin.NewSchedulePauseTool(sched))
		registry.Register(builtin.NewScheduleResumeTool(sched))
		registry.Register(builtin.NewScheduleRunNowTool(sched))
		registry.Register(builtin.NewCronCreateTool(sched))
		logger.Debug("Registered scheduler tools")
	}

	logger.Info("Registered builtin tools", "count", registry.Count())
}

// registerPlatformTools registers platform introspection tools.
// Called after AgentRegistry is created since platform_agents needs it.
func registerPlatformTools(
	registry *tools.Registry,
	agentRegistry *agent.AgentRegistry,
	statusHandler *StatusHandler,
	logger *slog.Logger,
) {
	// Platform status tool - uses StatusHandler.getStatus
	var statusFunc func() map[string]any
	if statusHandler != nil {
		statusFunc = func() map[string]any {
			return map[string]any{
				KeyStatus:         StateRunning,
				"uptime_seconds": time.Since(statusHandler.startTime).Seconds(),
				"version":        "0.2.0-go",
			}
		}
	}
	registry.Register(builtin.NewPlatformStatusTool(statusFunc))

	// Platform agents tool
	registry.Register(builtin.NewPlatformAgentsTool(agentRegistry))

	// Platform tools tool
	registry.Register(builtin.NewPlatformToolsTool(registry))

	// Delegate task tool (for multi-agent routing)
	registry.Register(builtin.NewDelegateTaskTool(agentRegistry))

	logger.Debug("Registered platform tools")
}

// registerTemplateTools registers template-related tools if the template
// registry is available. These tools allow agents to discover, invoke, and
// manage prompt templates at runtime.
func registerTemplateTools(
	registry *tools.Registry,
	templateRegistry *templates.Registry,
	logger *slog.Logger,
) {
	if templateRegistry == nil {
		logger.Debug("Template tools not registered: no template registry")
		return
	}

	registry.Register(builtin.NewTemplateInvokeTool(templateRegistry))
	registry.Register(builtin.NewTemplateListTool(templateRegistry))
	registry.Register(builtin.NewTemplateClearTool(templateRegistry))

	logger.Debug("Registered template tools", "template_count", templateRegistry.Count())
}

// registerMCPTools registers all tools from MCP servers with the tool registry.
func registerMCPTools(
	registry *tools.Registry,
	mcpManager *mcp.Manager,
	logger *slog.Logger,
) {
	if mcpManager == nil {
		return
	}

	// Get all LLM tool definitions from MCP servers
	defs := mcpManager.AllLLMDefinitions()
	if len(defs) == 0 {
		logger.Debug("No MCP tools to register")
		return
	}

	// Register each tool
	for _, def := range defs {
		// Extract server name from the prefixed tool name
		serverName := ""
		if idx := findDot(def.Function.Name); idx > 0 {
			serverName = def.Function.Name[:idx]
		}

		tool := mcp.NewMCPTool(def, mcpManager, serverName)
		registry.Register(tool)
	}

	logger.Info("Registered MCP tools", "count", len(defs))
}

// findDot finds the index of the first dot in a string.
func findDot(s string) int {
	for i, c := range s {
		if c == '.' {
			return i
		}
	}
	return -1
}

// buildProviderConfigs converts ModelsConfig providers to LLM ModelConfig slice.
func buildProviderConfigs(cfg *config.ModelsConfig, logger *slog.Logger) []*llm.ModelConfig {
	if cfg == nil || len(cfg.Providers) == 0 {
		return nil
	}

	// Build a set of disabled providers
	disabled := make(map[string]bool)
	for _, p := range cfg.DisabledProviders {
		disabled[p] = true
	}

	var configs []*llm.ModelConfig
	priority := 0

	for providerID, provider := range cfg.Providers {
		if disabled[providerID] {
			logger.Debug("Skipping disabled provider", "provider", providerID)
			continue
		}

		// Skip if no API key
		if provider.Options.APIKey == "" {
			logger.Debug("Skipping provider without API key", "provider", providerID)
			continue
		}

		// Get the first model from this provider (or the default model)
		for modelID, model := range provider.Models {
			caps := make(map[string]bool)
			for _, capName := range model.Capabilities {
				caps[capName] = true
			}

			configs = append(configs, &llm.ModelConfig{
				ProviderID:           providerID,
				BaseURL:              provider.Options.BaseURL,
				ModelID:              model.Name,
				APIKey:               provider.Options.APIKey,
				CostPerMillionInput:  model.InputCost,
				CostPerMillionOutput: model.OutputCost,
				MaxTokens:            model.MaxOutput,
				Temperature:          model.Temperature,
				ContextLimit:         model.ContextLimit,
				Capabilities:         caps,
			})

			logger.Debug("Added provider config",
				"provider", providerID,
				"model", modelID,
				"priority", priority,
			)
			priority++
			break // Only use first model per provider for failover
		}
	}

	return configs
}

// learningPipelineAdapter wraps selfimprove.LearningPipeline to implement agent.LearningPipeline.
type learningPipelineAdapter struct {
	pipeline *selfimprove.LearningPipeline
}

func (a *learningPipelineAdapter) Judge(ctx context.Context, trajectory agent.Trajectory) (*agent.JudgmentResult, error) {
	// Convert agent.Trajectory to selfimprove.Trajectory
	steps := make([]selfimprove.TrajectoryStep, len(trajectory.Steps))
	for i, s := range trajectory.Steps {
		steps[i] = selfimprove.TrajectoryStep{
			Action:  s.Action,
			Input:   s.Input,
			Output:  s.Output,
			Success: s.Success,
		}
	}

	siTrajectory := selfimprove.Trajectory{
		ID:        trajectory.ID,
		SessionID: trajectory.SessionID,
		Domain:    trajectory.Domain,
		Steps:     steps,
		Outcome: selfimprove.TrajectoryOutcome{
			Success:       trajectory.Outcome.Success,
			Quality:       trajectory.Outcome.Quality,
			Feedback:      trajectory.Outcome.Feedback,
			TaskCompleted: trajectory.Outcome.TaskCompleted,
		},
	}

	result, err := a.pipeline.Judge(ctx, siTrajectory)
	if err != nil {
		return nil, err
	}

	return &agent.JudgmentResult{
		Quality:     result.Quality,
		ShouldLearn: result.ShouldStore,
		Reason:      result.Reason,
	}, nil
}

func (a *learningPipelineAdapter) Distill(ctx context.Context, trajectory agent.Trajectory, judgment *agent.JudgmentResult) ([]*agent.LearnedPattern, error) {
	// Convert agent.Trajectory to selfimprove.Trajectory
	steps := make([]selfimprove.TrajectoryStep, len(trajectory.Steps))
	for i, s := range trajectory.Steps {
		steps[i] = selfimprove.TrajectoryStep{
			Action:  s.Action,
			Input:   s.Input,
			Output:  s.Output,
			Success: s.Success,
		}
	}

	siTrajectory := selfimprove.Trajectory{
		ID:        trajectory.ID,
		SessionID: trajectory.SessionID,
		Domain:    trajectory.Domain,
		Steps:     steps,
		Outcome: selfimprove.TrajectoryOutcome{
			Success:       trajectory.Outcome.Success,
			Quality:       trajectory.Outcome.Quality,
			Feedback:      trajectory.Outcome.Feedback,
			TaskCompleted: trajectory.Outcome.TaskCompleted,
		},
	}

	siJudgment := &selfimprove.JudgmentResult{
		Quality:          judgment.Quality,
		Correctness:      judgment.Quality, // Approximate from Quality
		Efficiency:       judgment.Quality, // Approximate from Quality
		Generalizability: 0.7,              // Default: assume moderate generalizability
		ShouldStore:      judgment.ShouldLearn,
		Reason:           judgment.Reason,
	}

	patterns, err := a.pipeline.Distill(ctx, siTrajectory, siJudgment)
	if err != nil {
		return nil, err
	}

	result := make([]*agent.LearnedPattern, len(patterns))
	for i, p := range patterns {
		result[i] = &agent.LearnedPattern{
			ID:          p.ID,
			Type:        string(p.Type),
			Domain:      p.Domain,
			Description: p.Description,
			Pattern:     p.Pattern,
			Confidence:  p.Confidence,
		}
	}

	return result, nil
}

func (a *learningPipelineAdapter) StorePattern(ctx context.Context, pattern *agent.LearnedPattern) error {
	siPattern := &selfimprove.LearnedPattern{
		ID:          pattern.ID,
		Type:        selfimprove.PatternType(pattern.Type),
		Domain:      pattern.Domain,
		Description: pattern.Description,
		Pattern:     pattern.Pattern,
		Confidence:  pattern.Confidence,
	}
	return a.pipeline.StorePattern(ctx, siPattern)
}

func (a *learningPipelineAdapter) Retrieve(ctx context.Context, query, domain string, k int) ([]*agent.LearnedPattern, error) {
	patterns, err := a.pipeline.Retrieve(ctx, query, domain, k)
	if err != nil {
		return nil, err
	}

	result := make([]*agent.LearnedPattern, len(patterns))
	for i, p := range patterns {
		result[i] = &agent.LearnedPattern{
			ID:          p.ID,
			Type:        string(p.Type),
			Domain:      p.Domain,
			Description: p.Description,
			Pattern:     p.Pattern,
			Confidence:  p.Confidence,
		}
	}

	return result, nil
}

// initializeCodeIntel sets up code intelligence (AST and LSP).
func (c *Components) initializeCodeIntel(cfg *config.Config, logger *slog.Logger) {
	logger.Info("Initializing code intelligence")

	// Initialize AST parser manager
	astConfig := ast.ParserConfig{
		CacheEnabled: cfg.CodeIntel.AST.CacheEnabled,
		CacheMaxSize: cfg.CodeIntel.AST.CacheMaxSize,
		CacheTTL:     time.Duration(cfg.CodeIntel.AST.CacheTTLMinutes) * time.Minute,
	}

	c.ASTParser = ast.NewParserManager(astConfig)
	logger.Info("AST parser manager initialized",
		"cache_enabled", astConfig.CacheEnabled,
		"cache_max_size", astConfig.CacheMaxSize,
	)

	// Register AST tools
	if tool, err := codetools.NewASTParseTool(c.ASTParser); err != nil {
		logger.Error("Failed to initialize AST parse tool", "error", err)
	} else {
		c.ToolRegistry.Register(tool)
	}
	if tool, err := codetools.NewASTSymbolsTool(c.ASTParser); err != nil {
		logger.Error("Failed to initialize AST symbols tool", "error", err)
	} else {
		c.ToolRegistry.Register(tool)
	}
	if tool, err := codetools.NewASTQueryTool(c.ASTParser); err != nil {
		logger.Error("Failed to initialize AST query tool", "error", err)
	} else {
		c.ToolRegistry.Register(tool)
	}
	logger.Debug("Registered AST tools")

	// Initialize LSP manager if servers are configured
	if len(cfg.CodeIntel.LSP.Servers) > 0 {
		// Get workspace root
		rootURI := ""
		if wd, err := os.Getwd(); err == nil {
			rootURI = lsp.PathToURI(wd)
		}

		lspOpts := []lsp.ManagerOption{
			lsp.WithManagerLogger(logger.With("component", "lsp")),
			lsp.WithRootURI(rootURI),
		}

		c.LSPManager = lsp.NewManager(cfg.CodeIntel.LSP, lspOpts...)
		logger.Info("LSP manager initialized",
			"configured_servers", len(cfg.CodeIntel.LSP.Servers),
			"auto_start", cfg.CodeIntel.LSP.AutoStartServers,
		)

		// Register LSP tools
		if tool, err := codetools.NewLSPDefinitionTool(c.LSPManager); err != nil {
			logger.Error("Failed to initialize LSP definition tool", "error", err)
		} else {
			c.ToolRegistry.Register(tool)
		}
		if tool, err := codetools.NewLSPReferencesTool(c.LSPManager); err != nil {
			logger.Error("Failed to initialize LSP references tool", "error", err)
		} else {
			c.ToolRegistry.Register(tool)
		}
		if tool, err := codetools.NewLSPHoverTool(c.LSPManager); err != nil {
			logger.Error("Failed to initialize LSP hover tool", "error", err)
		} else {
			c.ToolRegistry.Register(tool)
		}
		if tool, err := codetools.NewLSPSymbolsTool(c.LSPManager); err != nil {
			logger.Error("Failed to initialize LSP symbols tool", "error", err)
		} else {
			c.ToolRegistry.Register(tool)
		}
		if tool, err := codetools.NewLSPDiagnosticsTool(c.LSPManager); err != nil {
			logger.Error("Failed to initialize LSP diagnostics tool", "error", err)
		} else {
			c.ToolRegistry.Register(tool)
		}
		logger.Debug("Registered LSP tools")
	} else {
		logger.Info("LSP tools not registered (no servers configured)")
	}

	logger.Info("Code intelligence initialized")
}

// initializeCalendar sets up Google Calendar integration including OAuth token management,
// calendar tools, and optional reminder watcher.
func (c *Components) initializeCalendar(cfg *config.Config, msgBus *bus.MessageBus, logger *slog.Logger) {
	calLogger := logger.With("component", "calendar")

	// Expand environment variables in credentials
	clientID := os.ExpandEnv(cfg.Calendar.ClientID)
	clientSecret := os.ExpandEnv(cfg.Calendar.ClientSecret)

	if clientID == "" || clientSecret == "" {
		calLogger.Warn("calendar enabled but client_id or client_secret not configured")
		return
	}

	redirectURI := cfg.Calendar.RedirectURI
	if redirectURI == "" {
		redirectURI = "http://localhost:8888/callback"
	}

	oauthCfg := calendar.DefaultOAuth2Config(clientID, clientSecret, redirectURI)
	tokenPath := filepath.Join(cfg.Daemon.DataDir, "calendar_token.json")
	auth := calendar.NewOAuth2Authenticator(oauthCfg, tokenPath)

	// Try to load existing token
	token, err := auth.GetValidToken(context.Background())
	if err != nil {
		calLogger.Warn("no valid calendar token found; run 'meept calendar auth' to authenticate", "error", err)
		return
	}

	calendarID := cfg.Calendar.CalendarID
	if calendarID == "" {
		calendarID = "primary"
	}

	calClient, err := calendar.NewClient(calendar.ClientConfig{
		AccessToken: token.AccessToken,
		CalendarID:  calendarID,
	}, calLogger)
	if err != nil {
		calLogger.Error("failed to create calendar client", "error", err)
		return
	}
	c.CalendarClient = calClient

	// Register calendar tools
	c.ToolRegistry.Register(builtin.NewCalendarListTool(calClient))
	c.ToolRegistry.Register(builtin.NewCalendarCreateTool(calClient))
	c.ToolRegistry.Register(builtin.NewCalendarQuickAddTool(calClient))
	c.ToolRegistry.Register(builtin.NewCalendarTodayTool(calClient))
	calLogger.Info("calendar tools registered", "calendar_id", calendarID)

	// Start reminder watcher if enabled
	if cfg.Calendar.ReminderEnabled {
		checkInterval, _ := time.ParseDuration(cfg.Calendar.ReminderCheckInterval)
		if checkInterval <= 0 {
			checkInterval = 5 * time.Minute
		}
		advanceMinutes := cfg.Calendar.ReminderAdvanceMinutes
		if advanceMinutes <= 0 {
			advanceMinutes = 10
		}

		var publish func(string, map[string]any)
		if msgBus != nil {
			publish = func(topic string, data map[string]any) {
				msg, _ := models.NewBusMessage(models.MessageTypeEvent, "calendar", data)
				msgBus.Publish(topic, msg)
			}
		}

		watcher := calendar.NewReminderWatcher(calClient, publish, calendar.ReminderWatcherConfig{
			Interval:       checkInterval,
			AdvanceMinutes: advanceMinutes,
		}, calLogger)

		c.CalendarReminder = watcher
		// Note: CalendarReminder.Start() is called in Components.Start(ctx) with proper context
		calLogger.Info("calendar reminder watcher configured",
			"check_interval", checkInterval,
			"advance_minutes", advanceMinutes,
		)
	}
}

// initializeSkills sets up the skills system with lazy loading.
func (c *Components) initializeSkills(cfg *config.Config, logger *slog.Logger) {
	// Build discovery options
	discoveryOpts := []skills.DiscoveryOption{
		skills.WithDiscoveryLogger(logger.With("component", "skills-discovery")),
	}

	// Add custom search paths if configured
	if len(cfg.Skills.SearchPaths) > 0 {
		customTiers := make([]skills.DiscoveryTier, len(cfg.Skills.SearchPaths))
		for i, path := range cfg.Skills.SearchPaths {
			customTiers[i] = skills.DiscoveryTier{
				Path:     path,
				Priority: skills.PriorityUser, // Same priority as user-global
			}
		}
		discoveryOpts = append(discoveryOpts, skills.WithTiers(
			append(skills.DefaultTiers(), customTiers...),
		))
	}

	// Create discovery
	discovery := skills.NewDiscovery(discoveryOpts...)

	// Discover metadata only (lightweight index, no bodies)
	indexEntries, err := discovery.DiscoverMetadataOnly()
	if err != nil {
		logger.Warn("Skill metadata discovery failed", "error", err)
	} else {
		// Create skill index from metadata
		c.SkillIndex = skills.NewSkillIndex()
		c.SkillIndex.IndexAll(indexEntries)
		logger.Info("Skill index built",
			"count", c.SkillIndex.Count(),
			"tags", len(c.SkillIndex.AllTags()),
			"capabilities", len(c.SkillIndex.AllCapabilities()),
		)

		// Build capability index for metadata-driven skill matching
		c.CapabilityIndex = skills.BuildCapabilityIndex(
			c.SkillIndex,
			skills.WithCapabilityLogger(logger.With("component", "capability-index")),
		)
		logger.Info("Capability index built",
			"skills", c.CapabilityIndex.SkillCount(),
			"keywords", c.CapabilityIndex.KeywordCount(),
		)

		// Create lazy loader with LRU cache
		cacheSize := 50 // Default cache size
		if cfg.Skills.CacheSize > 0 {
			cacheSize = cfg.Skills.CacheSize
		}
		c.SkillLoader = skills.NewLazySkillLoader(
			c.SkillIndex,
			skills.WithLoaderLogger(logger.With("component", "skills-loader")),
			skills.WithCacheSize(cacheSize),
		)
		logger.Debug("Skills lazy loader initialized", "cache_size", cacheSize)
	}

	// Create registry (for backwards compatibility)
	// Register skills from metadata (bodies will be loaded on-demand)
	c.SkillRegistry = skills.NewRegistry(
		skills.WithRegistryLogger(logger.With("component", "skills-registry")),
	)

	// Load full skills for registry (for backwards compatibility with existing
	// code that walks the registry eagerly at startup). A fully lazy registry
	// would require reworking every RegisterAll consumer — the SkillIndex
	// above is already metadata-only, and SkillLoader already provides
	// on-demand loading for new consumers, which covers the common hot path.
	// Full lazy conversion of the registry is tracked as a separate concern
	// in docs/bugs-and-gaps.md (issue #33, scope-deferred).
	discovered, err := discovery.Discover()
	if err != nil {
		logger.Warn("Skill discovery failed", "error", err)
	} else {
		c.SkillRegistry.RegisterAll(discovered)
		logger.Info("Skills loaded into registry", "count", len(discovered))
	}

	// Create executor if we have a resolver
	if c.LLMResolver != nil {
		executorOpts := []skills.ExecutorOption{
			skills.WithExecutorLogger(logger.With("component", "skills-executor")),
		}
		if c.LLMClient != nil {
			executorOpts = append(executorOpts, skills.WithClient(c.LLMClient))
		}

		// Add lazy loader to executor
		if c.SkillLoader != nil {
			executorOpts = append(executorOpts, skills.WithLazyLoader(c.SkillLoader))
		}

		c.SkillExecutor = skills.NewExecutor(c.LLMResolver, executorOpts...)
		logger.Debug("Skills executor initialized")
	} else {
		logger.Warn("Skills executor not created - no LLM resolver available")
	}

}

// initializeTemplates sets up the templates system with discovery and registry.
func (c *Components) initializeTemplates(_ *config.Config, logger *slog.Logger) {
	// Templates share the skills enabled flag since they use the same
	// discovery mechanism. Templates can still be useful even without skills.
	discoveryOpts := []templates.DiscoveryOption{
		templates.WithDiscoveryLogger(logger.With("component", "templates-discovery")),
	}

	// Create discovery
	c.TemplateDiscovery = templates.NewDiscovery(discoveryOpts...)

	// Create registry
	c.TemplateRegistry = templates.NewRegistry(
		templates.WithRegistryLogger(logger.With("component", "templates-registry")),
	)

	// Discover and load templates
	if err := c.TemplateRegistry.LoadFromDiscovery(c.TemplateDiscovery); err != nil {
		logger.Warn("Template discovery failed", "error", err)
	} else {
		logger.Info("Templates loaded into registry",
			"count", c.TemplateRegistry.Count(),
		)
	}
}

// StatusHandler handles status.request messages on the bus.
type StatusHandler struct {
	bus           *bus.MessageBus
	logger        *slog.Logger
	startTime     time.Time
	cancel        context.CancelFunc
	budgetTracker *llm.Budget
}

// StatusHandlerOption is a functional option for configuring StatusHandler.
type StatusHandlerOption func(*StatusHandler)

// WithBudgetTracker sets the budget tracker for status reporting.
func WithBudgetTracker(budget *llm.Budget) StatusHandlerOption {
	return func(h *StatusHandler) {
		h.budgetTracker = budget
	}
}

// NewStatusHandler creates a new status handler.
func NewStatusHandler(msgBus *bus.MessageBus, logger *slog.Logger, opts ...StatusHandlerOption) *StatusHandler {
	h := &StatusHandler{
		bus:       msgBus,
		logger:    logger,
		startTime: time.Now(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Start begins listening for status requests.
func (h *StatusHandler) Start(ctx context.Context) error {
	ctx, h.cancel = context.WithCancel(ctx)
	sub := h.bus.Subscribe("status-handler", "status.request")

	go func() {
		for {
			select {
			case <-ctx.Done():
				h.bus.Unsubscribe(sub)
				return
			case msg, ok := <-sub.Channel:
				if !ok {
					return
				}
				h.handleStatusRequest(msg)
			}
		}
	}()

	return nil
}

// Stop stops the handler.
func (h *StatusHandler) Stop(ctx context.Context) error {
	if h.cancel != nil {
		h.cancel()
	}
	return nil
}

// handleStatusRequest responds to a status request.
func (h *StatusHandler) handleStatusRequest(msg *models.BusMessage) {
	uptime := time.Since(h.startTime).Seconds()

	response := map[string]any{
		KeyStatus:          StateRunning,
		"uptime_seconds":  uptime,
		"version":         "0.2.0-go",
		"bus_subscribers": len(h.bus.Stats()),
	}

	// Include token usage from budget tracker if available
	if h.budgetTracker != nil {
		budgetStatus := h.budgetTracker.GetStatus()
		response["tokens_used"] = budgetStatus.HourlyUsed
		response["budget"] = map[string]any{
			"hourly_used":      budgetStatus.HourlyUsed,
			"hourly_limit":     budgetStatus.HourlyLimit,
			"hourly_remaining": budgetStatus.HourlyRemaining,
			"daily_used":       budgetStatus.DailyUsed,
			"daily_limit":      budgetStatus.DailyLimit,
			"daily_remaining":  budgetStatus.DailyRemaining,
			"rpm_current":      budgetStatus.RPMCurrent,
			"rpm_limit":        budgetStatus.RPMLimit,
			"within_budget":    budgetStatus.WithinBudget,
		}
	} else {
		response["tokens_used"] = 0
	}

	payload, _ := json.Marshal(response)

	respMsg := &models.BusMessage{
		ID:        time.Now().Format("20060102150405.000000000"),
		Type:      models.MessageTypeResponse,
		Topic:     "status.response",
		Source:    "status-handler",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
		ReplyTo:   msg.ID,
	}

	h.bus.Publish("status.response", respMsg)
}

// AgentJobProcessor processes jobs using the agent loop.
// AgentJobProcessor processes jobs using the agent loop, with optional
// multi-agent dispatch via the agent registry.
type AgentJobProcessor struct {
	agentLoop *agent.AgentLoop
	registry  *agent.AgentRegistry
	logger    *slog.Logger
}

// NewAgentJobProcessor creates a new agent job processor.
func NewAgentJobProcessor(agentLoop *agent.AgentLoop, logger *slog.Logger) *AgentJobProcessor {
	return &AgentJobProcessor{
		agentLoop: agentLoop,
		logger:    logger,
	}
}

// WithRegistry sets the agent registry for multi-agent job dispatch.
func (p *AgentJobProcessor) WithRegistry(registry *agent.AgentRegistry) *AgentJobProcessor {
	p.registry = registry
	return p
}

// Process executes a job using the appropriate agent loop.
// If the job has an AgentID and a registry is configured, it dispatches to
// the agent-specific loop. Otherwise it falls back to the main loop.
func (p *AgentJobProcessor) Process(ctx context.Context, job *queue.Job) (any, error) {
	// Try step-based payload first (from orchestrator)
	var stepPayload struct {
		StepID             string   `json:"step_id"`
		TaskID             string   `json:"task_id"`
		Description        string   `json:"description"`
		ToolHint           string   `json:"tool_hint,omitempty"`
		MemoryRefs         []string `json:"memory_refs,omitempty"`
		AccumulatedContext string   `json:"accumulated_context,omitempty"`
	}

	// Try legacy payload format
	var legacyPayload struct {
		Prompt    string `json:"prompt"`
		SessionID string `json:"session_id,omitempty"`
	}

	isStepJob := false
	if err := json.Unmarshal(job.Payload, &stepPayload); err == nil && stepPayload.StepID != "" {
		isStepJob = true
	}

	if !isStepJob {
		if err := json.Unmarshal(job.Payload, &legacyPayload); err != nil {
			return nil, fmt.Errorf("failed to parse job payload: %w", err)
		}
	}

	// Determine which agent loop to use
	var agentLoop *agent.AgentLoop
	if job.AgentID != "" && p.registry != nil {
		loop, err := p.registry.Get(job.AgentID)
		if err != nil {
			p.logger.Warn("Agent not found, falling back to main loop",
				"agent_id", job.AgentID,
				"job_id", job.ID,
				"error", err,
			)
			agentLoop = p.agentLoop
		} else {
			agentLoop = loop
		}
	} else {
		agentLoop = p.agentLoop
	}

	if agentLoop == nil {
		return nil, fmt.Errorf("no agent loop available")
	}

	// Build prompt and conversation ID with context
	var prompt, conversationID string
	if isStepJob {
		// Build context section from step's MemoryRefs and AccumulatedContext
		var contextSection string
		if len(stepPayload.MemoryRefs) > 0 || stepPayload.AccumulatedContext != "" {
			var sb strings.Builder
			sb.WriteString("## Context for this Step\n\n")
			if len(stepPayload.MemoryRefs) > 0 {
				sb.WriteString("**Available Memories:**\n")
				for i, ref := range stepPayload.MemoryRefs {
					fmt.Fprintf(&sb, "%d. Memory: `%s`\n", i+1, ref)
				}
				sb.WriteString("\n")
			}
			if stepPayload.AccumulatedContext != "" {
				sb.WriteString("**Results from Prior Steps:**\n\n")
				sb.WriteString(stepPayload.AccumulatedContext)
				sb.WriteString("\n\n")
			}
			contextSection = sb.String()
		}

		// Prepend context to the step description
		if contextSection != "" {
			prompt = contextSection + "\n## Your Task\n\n" + stepPayload.Description
		} else {
			prompt = stepPayload.Description
		}

		conversationID = fmt.Sprintf("step-%s-%s", stepPayload.TaskID, stepPayload.StepID)
		p.logger.Info("Processing step job",
			"job_id", job.ID,
			"step_id", stepPayload.StepID,
			"task_id", stepPayload.TaskID,
			"agent_id", job.AgentID,
			"memory_refs", len(stepPayload.MemoryRefs),
			"has_context", stepPayload.AccumulatedContext != "",
		)
	} else {
		prompt = legacyPayload.Prompt
		if prompt == "" {
			return nil, fmt.Errorf("job payload missing prompt")
		}
		conversationID = legacyPayload.SessionID
		if conversationID == "" {
			conversationID = job.ID
		}
		p.logger.Info("Processing job",
			"job_id", job.ID,
			"prompt_len", len(prompt),
			"agent_id", job.AgentID,
		)
	}

	// Execute
	response, err := agentLoop.RunOnce(ctx, prompt, conversationID)
	if err != nil {
		p.logger.Error("Agent execution failed",
			"job_id", job.ID,
			"agent_id", job.AgentID,
			"error", err,
		)
		return nil, fmt.Errorf("agent execution failed: %w", err)
	}

	result := map[string]any{
		"job_id":   job.ID,
		"response": response,
		KeyStatus:   "completed",
	}
	if isStepJob {
		result["step_id"] = stepPayload.StepID
		result["task_id"] = stepPayload.TaskID
	}

	return result, nil
}

// Ensure AgentJobProcessor implements worker.JobProcessor
var _ worker.JobProcessor = (*AgentJobProcessor)(nil)

// webHandlerAdapter adapts AgentLoop and StatusHandler to the web.Handler interface.
type webHandlerAdapter struct {
	agentLoop     *agent.AgentLoop
	statusHandler *StatusHandler
}

// Chat handles a chat request via the web handler.
func (h *webHandlerAdapter) Chat(ctx context.Context, message string) (string, error) {
	if h.agentLoop == nil {
		return "", fmt.Errorf("agent loop not available")
	}
	// Use AgentLoop.RunOnce for synchronous chat
	conversationID := fmt.Sprintf("web-%d", time.Now().UnixNano())
	return h.agentLoop.RunOnce(ctx, message, conversationID)
}

// Status returns the daemon status via the web handler.
func (h *webHandlerAdapter) Status(ctx context.Context) (map[string]any, error) {
	status := map[string]any{
		KeyStatus:  StateRunning,
		"version": "0.3.0-go",
	}

	if h.statusHandler != nil {
		uptime := time.Since(h.statusHandler.startTime).Seconds()
		status["uptime_seconds"] = uptime
		status["bus_subscribers"] = len(h.statusHandler.bus.Stats())

		if h.statusHandler.budgetTracker != nil {
			budgetStatus := h.statusHandler.budgetTracker.GetStatus()
			status["tokens_used"] = budgetStatus.HourlyUsed
			status["budget"] = map[string]any{
				"hourly_used":      budgetStatus.HourlyUsed,
				"hourly_limit":     budgetStatus.HourlyLimit,
				"hourly_remaining": budgetStatus.HourlyRemaining,
				"daily_used":       budgetStatus.DailyUsed,
				"daily_limit":      budgetStatus.DailyLimit,
				"daily_remaining":  budgetStatus.DailyRemaining,
				"rpm_current":      budgetStatus.RPMCurrent,
				"rpm_limit":        budgetStatus.RPMLimit,
				"within_budget":    budgetStatus.WithinBudget,
			}
		}
	}

	return status, nil
}

// memorySearcherAdapter wraps memory.Manager to implement web.MemorySearcher.
type memorySearcherAdapter struct {
	mgr *memory.Manager
}

// Search implements web.MemorySearcher.
func (a *memorySearcherAdapter) Search(ctx context.Context, query string, limit int) ([]web.MemorySearchResult, error) {
	results, err := a.mgr.Search(ctx, memory.MemoryQuery{
		Query: query,
		Limit: limit,
	})
	if err != nil {
		return nil, err
	}

	webResults := make([]web.MemorySearchResult, len(results))
	for i, r := range results {
		createdAt := ""
		if !r.Memory.CreatedAt.IsZero() {
			createdAt = r.Memory.CreatedAt.Format(time.RFC3339)
		}

		webResults[i] = web.MemorySearchResult{
			ID:        r.Memory.ID,
			Content:   r.Memory.Content,
			Type:      string(r.Memory.Type),
			Category:  r.Memory.Category,
			CreatedAt: createdAt,
			Score:     r.RelevanceScore,
			Metadata:  r.Memory.Metadata,
		}
	}

	return webResults, nil
}

// skillsListerAdapter wraps skills.Registry to implement web.SkillsLister.
type skillsListerAdapter struct {
	registry *skills.Registry
}

// List implements web.SkillsLister.
func (a *skillsListerAdapter) List() []web.SkillInfo {
	skillList := a.registry.List()
	webSkills := make([]web.SkillInfo, len(skillList))

	for i, s := range skillList {
		webSkills[i] = web.SkillInfo{
			Name:        s.Name,
			Description: s.Description,
			Tags:        s.Tags,
			Priority:    s.Priority,
		}
	}

	return webSkills
}

// jobsListerAdapter wraps scheduler.Scheduler to implement web.JobsLister.
type jobsListerAdapter struct {
	scheduler *scheduler.Scheduler
}

// ListJobs implements web.JobsLister.
func (a *jobsListerAdapter) ListJobs() ([]web.JobInfo, error) {
	jobs := a.scheduler.ListJobs()
	webJobs := make([]web.JobInfo, len(jobs))

	for i, j := range jobs {
		nextRun := ""
		if j.NextRun != nil {
			nextRun = j.NextRun.Format(time.RFC3339)
		}

		lastRun := ""
		if j.LastRun != nil {
			lastRun = j.LastRun.Format(time.RFC3339)
		}

		status := "active"
		if !j.Enabled {
			status = "paused"
		} else if j.IsRunning {
			status = StateRunning
		}

		webJobs[i] = web.JobInfo{
			ID:       j.ID,
			Name:     j.Name,
			Schedule: j.Schedule,
			NextRun:  nextRun,
			LastRun:  lastRun,
			Status:   status,
			Paused:   !j.Enabled,
		}
	}

	return webJobs, nil
}

// Ensure adapters implement their interfaces.
var (
	_ web.MemorySearcher = (*memorySearcherAdapter)(nil)
	_ web.SkillsLister   = (*skillsListerAdapter)(nil)
	_ web.JobsLister     = (*jobsListerAdapter)(nil)
	_ web.Handler        = (*webHandlerAdapter)(nil)
)
