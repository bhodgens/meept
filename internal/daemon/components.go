// Package daemon provides the main daemon lifecycle management.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/memvid"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/shadow"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
	"github.com/caimlas/meept/internal/tools/mcp"
	"github.com/caimlas/meept/internal/worker"
	"github.com/caimlas/meept/pkg/models"
	"github.com/caimlas/meept/pkg/security"
)

// Components holds all the daemon components.
type Components struct {
	Config          *config.Config
	ModelsConfig    *config.ModelsConfig
	LLMClient       *llm.Client
	ToolRegistry    *tools.Registry
	SecurityChecker *security.PermissionChecker
	AgentLoop       *agent.AgentLoop
	ChatHandler     *agent.ChatHandler
	StatusHandler   *StatusHandler
	SessionStore    session.Store
	SessionHandler  *session.Handler

	// Multi-agent orchestration components
	Queue           queue.Queue
	QueueHandler    *queue.Handler
	TaskRegistry    *task.Registry
	TaskHandler     *task.Handler
	WorkerPool      *worker.Pool
	WorkerHandler   *worker.Handler
	JobProcessor    worker.JobProcessor

	// Memory
	MemoryManager   *memory.Manager

	// Memvid and multi-agent
	MemvidClient    *memvid.Client
	AgentRegistry   *agent.AgentRegistry
	Dispatcher      *agent.Dispatcher

	// Shadow training
	ShadowManager   *shadow.Manager

	// MCP integration
	MCPManager      *mcp.Manager

	Logger          *slog.Logger
}

// NewComponents creates all daemon components from configuration.
func NewComponents(cfg *config.Config, msgBus *bus.MessageBus, logger *slog.Logger) (*Components, error) {
	c := &Components{
		Config: cfg,
		Logger: logger,
	}

	// Load models configuration - fail explicitly if not found
	modelsCfg, configPath, err := loadModelsConfigWithPath(logger)
	if err != nil {
		logger.Error("FATAL: Failed to load models configuration",
			"error", err,
			"searched_paths", []string{"config/models.json5", "~/.meept/models.json5"},
			"hint", "Copy config/models.json5 to ~/.meept/models.json5 or run daemon from project directory",
		)
		return nil, fmt.Errorf("models configuration required: %w", err)
	}
	logger.Info("Loaded models configuration",
		"path", configPath,
		"default_model", modelsCfg.Model,
		"small_model", modelsCfg.SmallModel,
		"providers", getProviderNames(modelsCfg),
	)
	c.ModelsConfig = modelsCfg

	// Create security checker
	secCfg := security.Config{
		AllowedPaths:              cfg.Security.AllowedPaths,
		BlockedPaths:              cfg.Security.BlockedPaths,
		BlockFinancial:            cfg.Security.BlockFinancial,
		RequireConfirmationHigh:   cfg.Security.RequireConfirmationHigh,
		RequireConfirmationCritical: cfg.Security.RequireConfirmationCritical,
	}
	c.SecurityChecker = security.NewPermissionChecker(secCfg)

	// Create LLM client
	llmCfg := createLLMConfig(modelsCfg, logger)
	if llmCfg != nil {
		c.LLMClient = llm.NewClient(llmCfg, llm.WithLogger(logger))
		logger.Info("LLM client initialized successfully",
			"provider", llmCfg.ProviderID,
			"model", llmCfg.ModelID,
			"base_url", llmCfg.BaseURL,
		)
	} else {
		logger.Error("FATAL: No LLM configured - chat will not work",
			"hint", "Check models.json5 configuration and ensure model exists",
		)
		return nil, fmt.Errorf("LLM configuration required but model resolution failed")
	}

	// Create tool registry (builtin tools registered after all dependencies are available)
	c.ToolRegistry = tools.NewRegistry(logger)

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

	// Create agent loop
	agentOpts := []agent.LoopOption{
		agent.WithMessageBus(msgBus),
		agent.WithLoopLogger(logger),
	}
	if c.LLMClient != nil {
		agentOpts = append(agentOpts, agent.WithLLMClient(c.LLMClient))
	}
	if c.SecurityChecker != nil {
		agentOpts = append(agentOpts, agent.WithSecurityChecker(c.SecurityChecker))
	}
	if c.ToolRegistry != nil {
		adapter := agent.NewToolRegistryAdapter(c.ToolRegistry)
		agentOpts = append(agentOpts, agent.WithToolRegistry(adapter))
	}
	if c.ShadowManager != nil {
		agentOpts = append(agentOpts, agent.WithShadowManager(c.ShadowManager))
		logger.Info("Agent loop configured with shadow training")
	}
	// Always set an agent ID for security checks - use "default" when multi-agent is disabled
	agentOpts = append(agentOpts, agent.WithAgentID("default"))
	c.AgentLoop = agent.NewAgentLoop(agentOpts...)

	// Chat handler created later after dispatcher (if multi-agent enabled)

	// Create status handler
	c.StatusHandler = NewStatusHandler(msgBus, logger)

	// Create memory manager
	c.MemoryManager = memory.NewManager(memory.ManagerConfig{
		Config:       cfg.Memory,
		MemvidConfig: cfg.Memvid,
		Logger:       logger.With("component", "memory"),
	})
	if err := c.MemoryManager.Initialize(context.Background()); err != nil {
		logger.Error("Failed to initialize memory manager", "error", err)
		// Non-fatal: daemon can run without memory
	} else {
		logger.Info("Memory manager initialized",
			"backend", c.MemoryManager.Backend(),
		)
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
	if c.LLMClient != nil {
		summarizer := session.NewSummarizer(c.LLMClient, logger.With("component", "summarizer"))
		sessionOpts = append(sessionOpts, session.WithSummarizer(summarizer))
		logger.Info("Session summarizer enabled with LLM client")
	} else {
		logger.Warn("Session summarizer disabled - no LLM client available")
	}
	c.SessionHandler = session.NewHandler(c.SessionStore, msgBus, logger.With("component", "session"), sessionOpts...)

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
	}

	// Initialize MCP manager and register MCP tools
	if cfg.MCP.Enabled {
		c.MCPManager = mcp.NewManager(logger.With("component", "mcp"))

		// Load MCP servers config
		mcpCfg, err := config.LoadMCPConfig(cfg.MCP.ConfigFile)
		if err != nil {
			logger.Warn("Failed to load MCP config", "error", err, "path", cfg.MCP.ConfigFile)
		} else if len(mcpCfg.Servers) > 0 {
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
		} else {
			logger.Info("MCP enabled but no servers configured")
		}
	}

	// Register builtin tools now that all dependencies are available
	var taskStore *task.Store
	if c.TaskRegistry != nil {
		taskStore = c.TaskRegistry.Store()
	}
	registerBuiltinTools(c.ToolRegistry, c.SecurityChecker, c.MemoryManager, taskStore, logger)

	// Create agent registry if multi-agent is enabled
	if cfg.MultiAgent.Enabled {
		toolAdapter := agent.NewToolRegistryAdapter(c.ToolRegistry)

		var taskStore *task.Store
		if c.TaskRegistry != nil {
			taskStore = c.TaskRegistry.Store()
		}

		c.AgentRegistry = agent.NewAgentRegistry(agent.RegistryConfig{
			MemvidClient:    c.MemvidClient,
			TaskStore:       taskStore,
			LLMClient:       c.LLMClient,
			MessageBus:      msgBus,
			SecurityChecker: c.SecurityChecker,
			ToolRegistry:    toolAdapter,
			Logger:          logger,
		})
		logger.Info("Agent registry initialized", "specs", len(c.AgentRegistry.ListSpecs()))

		// Create dispatcher
		c.Dispatcher = agent.NewDispatcher(agent.DispatcherConfig{
			Registry:     c.AgentRegistry,
			MemvidClient: c.MemvidClient,
			MemoryMgr:    c.MemoryManager,
			TaskStore:    taskStore,
			Logger:       logger.With("component", "dispatcher"),
		})
		logger.Info("Dispatcher initialized")

		// Register platform tools now that agent registry is available
		registerPlatformTools(c.ToolRegistry, c.AgentRegistry, c.StatusHandler, logger)

		// Create chat handler with dispatcher for multi-agent routing
		c.ChatHandler = agent.NewChatHandler(c.AgentLoop, c.Dispatcher, msgBus, logger)
		logger.Info("ChatHandler initialized with dispatcher")
	} else {
		// Create chat handler without dispatcher (single-agent mode)
		c.ChatHandler = agent.NewChatHandler(c.AgentLoop, nil, msgBus, logger)
	}

	// Create job processor that uses the agent loop
	c.JobProcessor = NewAgentJobProcessor(c.AgentLoop, logger)

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

	return nil
}

// Stop stops all components.
func (c *Components) Stop(ctx context.Context) error {
	var lastErr error

	// Stop worker pool first to prevent new work
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

	// Stop all MCP server connections
	if c.MCPManager != nil {
		c.MCPManager.StopAll()
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
			if id == targetModel || model.Name == targetModel {
				caps := make(map[string]bool)
				for _, cap := range model.Capabilities {
					caps[cap] = true
				}

				apiKey := provider.Options.APIKey
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
	memoryMgr *memory.Manager,
	taskStore *task.Store,
	logger *slog.Logger,
) {
	// Filesystem tools
	registry.Register(builtin.NewReadFileTool(checker))
	registry.Register(builtin.NewWriteFileTool(checker))
	registry.Register(builtin.NewDeleteFileTool(checker))
	registry.Register(builtin.NewListDirectoryTool(checker))

	// Shell tool
	wd, _ := os.Getwd()
	registry.Register(builtin.NewShellExecuteTool(wd, 60*time.Second))

	// Web fetch tool
	registry.Register(builtin.NewWebFetchTool(30*time.Second, 100000))

	// Memory tools (only if memory manager is available)
	if memoryMgr != nil {
		registry.Register(builtin.NewMemoryStoreTool(memoryMgr))
		registry.Register(builtin.NewMemorySearchTool(memoryMgr))
		registry.Register(builtin.NewMemoryGetContextTool(memoryMgr))
		logger.Debug("Registered memory tools")
	}

	// Task tools (only if task store is available)
	if taskStore != nil {
		registry.Register(builtin.NewTaskCreateTool(taskStore))
		registry.Register(builtin.NewTaskGetTool(taskStore))
		registry.Register(builtin.NewTaskListTool(taskStore))
		registry.Register(builtin.NewTaskUpdateTool(taskStore))
		logger.Debug("Registered task tools")
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
				"status":         "running",
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

// StatusHandler handles status.request messages on the bus.
type StatusHandler struct {
	bus       *bus.MessageBus
	logger    *slog.Logger
	startTime time.Time
	cancel    context.CancelFunc
}

// NewStatusHandler creates a new status handler.
func NewStatusHandler(msgBus *bus.MessageBus, logger *slog.Logger) *StatusHandler {
	return &StatusHandler{
		bus:       msgBus,
		logger:    logger,
		startTime: time.Now(),
	}
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
		"status":         "running",
		"uptime_seconds": uptime,
		"version":        "0.2.0-go",
		"bus_subscribers": len(h.bus.Stats()),
		"tokens_used":    0, // TODO: Get from budget tracker
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
type AgentJobProcessor struct {
	agentLoop *agent.AgentLoop
	logger    *slog.Logger
}

// NewAgentJobProcessor creates a new agent job processor.
func NewAgentJobProcessor(agentLoop *agent.AgentLoop, logger *slog.Logger) *AgentJobProcessor {
	return &AgentJobProcessor{
		agentLoop: agentLoop,
		logger:    logger,
	}
}

// Process executes a job using the agent loop.
func (p *AgentJobProcessor) Process(ctx context.Context, job *queue.Job) (any, error) {
	// Parse the job payload
	var payload struct {
		Prompt    string `json:"prompt"`
		SessionID string `json:"session_id,omitempty"`
	}

	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse job payload: %w", err)
	}

	if payload.Prompt == "" {
		return nil, fmt.Errorf("job payload missing prompt")
	}

	p.logger.Info("Processing job", "job_id", job.ID, "prompt_len", len(payload.Prompt))

	if p.agentLoop == nil {
		return nil, fmt.Errorf("agent loop not configured")
	}

	// Use session ID as conversation ID if provided, otherwise use job ID
	conversationID := payload.SessionID
	if conversationID == "" {
		conversationID = job.ID
	}

	// Execute using agent loop
	response, err := p.agentLoop.RunOnce(ctx, payload.Prompt, conversationID)
	if err != nil {
		p.logger.Error("Agent loop execution failed", "job_id", job.ID, "error", err)
		return nil, fmt.Errorf("agent execution failed: %w", err)
	}

	result := map[string]any{
		"job_id":     job.ID,
		"response":   response,
		"status":     "completed",
		"session_id": payload.SessionID,
	}

	return result, nil
}

// Ensure AgentJobProcessor implements worker.JobProcessor
var _ worker.JobProcessor = (*AgentJobProcessor)(nil)
