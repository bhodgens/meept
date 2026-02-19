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
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
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

	// Create tool registry with builtin tools
	c.ToolRegistry = tools.NewRegistry(logger)
	registerBuiltinTools(c.ToolRegistry, c.SecurityChecker, logger)

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
	c.AgentLoop = agent.NewAgentLoop(agentOpts...)

	// Create chat handler
	c.ChatHandler = agent.NewChatHandler(c.AgentLoop, msgBus, logger)

	// Create status handler
	c.StatusHandler = NewStatusHandler(msgBus, logger)

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
	c.SessionHandler = session.NewHandler(c.SessionStore, msgBus, logger)

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

	return nil
}

// Stop stops all components.
func (c *Components) Stop(ctx context.Context) error {
	var lastErr error

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

	if c.SessionStore != nil {
		if err := c.SessionStore.Close(); err != nil {
			lastErr = err
		}
	}

	if c.LLMClient != nil {
		c.LLMClient.Close()
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

// registerBuiltinTools registers all builtin tools with the registry.
func registerBuiltinTools(registry *tools.Registry, checker *security.PermissionChecker, logger *slog.Logger) {
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

	logger.Info("Registered builtin tools", "count", registry.Count())
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
