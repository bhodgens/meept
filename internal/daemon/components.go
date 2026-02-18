// Package daemon provides the main daemon lifecycle management.
package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
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
	SessionStore    *session.Store
	SessionHandler  *session.Handler
	Logger          *slog.Logger
}

// NewComponents creates all daemon components from configuration.
func NewComponents(cfg *config.Config, msgBus *bus.MessageBus, logger *slog.Logger) (*Components, error) {
	c := &Components{
		Config: cfg,
		Logger: logger,
	}

	// Load models configuration
	modelsCfg, err := config.LoadModelsConfigDefault()
	if err != nil {
		// If no models config, create a default with environment variables
		logger.Warn("Failed to load models config, using defaults", "error", err)
		modelsCfg = createDefaultModelsConfig()
	}
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
	llmCfg := createLLMConfig(modelsCfg)
	if llmCfg != nil {
		c.LLMClient = llm.NewClient(llmCfg, llm.WithLogger(logger))
		logger.Info("LLM client created",
			"model", llmCfg.ModelID,
			"base_url", llmCfg.BaseURL,
		)
	} else {
		logger.Warn("No LLM configured - chat will not work")
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

	// Create session store and handler
	c.SessionStore = session.NewStore(logger)
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

	if c.LLMClient != nil {
		c.LLMClient.Close()
	}

	return lastErr
}

// createDefaultModelsConfig creates a minimal models config from environment.
func createDefaultModelsConfig() *config.ModelsConfig {
	return &config.ModelsConfig{
		Model:      "gpt-4o",
		SmallModel: "gpt-4o-mini",
		Providers: map[string]config.Provider{
			"openai": {
				API: "openai",
				Options: config.ProviderOptions{
					BaseURL: "https://api.openai.com",
					APIKey:  os.Getenv("OPENAI_API_KEY"),
				},
				Models: map[string]config.Model{
					"gpt-4o": {
						Name:         "gpt-4o",
						Capabilities: []string{"chat", "code", "tool_use"},
						ContextLimit: 128000,
						MaxOutput:    4096,
						Temperature:  0.7,
					},
				},
			},
		},
	}
}

// createLLMConfig creates an LLM model config from the models configuration.
func createLLMConfig(cfg *config.ModelsConfig) *llm.ModelConfig {
	if cfg == nil {
		return nil
	}

	// Find the default model
	modelID := cfg.Model
	if modelID == "" {
		return nil
	}

	// Search for the model in providers
	for providerID, provider := range cfg.Providers {
		for id, model := range provider.Models {
			if id == modelID || model.Name == modelID {
				caps := make(map[string]bool)
				for _, cap := range model.Capabilities {
					caps[cap] = true
				}

				return &llm.ModelConfig{
					BaseURL:              provider.Options.BaseURL,
					ModelID:              id,
					APIKey:               provider.Options.APIKey,
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

	return nil
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
