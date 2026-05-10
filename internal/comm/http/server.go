// Package http provides the HTTP REST API server for the Meept menubar application.
package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/services"
)

// ServerConfig holds configuration for the HTTP server.
type ServerConfig struct {
	Addr           string        // Listen address (default: :8081)
	ReadTimeout    time.Duration // Read timeout
	WriteTimeout   time.Duration // Write timeout
	MaxHeaderBytes int           // Max header size
	EnableCORS     bool          // Enable CORS headers
	APIKeys        []string      // Valid API keys for authentication
	RequireAuth    bool          // Require API key authentication
}

// DefaultServerConfig returns sensible defaults for the menubar HTTP server.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Addr:           ":8081", // Different from existing web server (:8080)
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1 MB
		EnableCORS:     true,    // Enable CORS for local menubar app
		RequireAuth:    false,   // Disabled by default for local development
		APIKeys:        []string{},
	}
}

// DaemonController provides daemon lifecycle control.
type DaemonController interface {
	IsRunning() bool
	PID() int
	Uptime() time.Duration
	Restart(ctx context.Context) error
}

// MetricsService provides metrics access.
type MetricsService interface {
	GetLiveMetrics() (*metrics.LiveMetricsSnapshot, error)
	GetHistoricalMetrics(ctx context.Context, from, to time.Time, resolution string) ([]metrics.MetricPoint, error)
	SubscribeMetrics() (<-chan *metrics.LiveMetricsSnapshot, func())
}

// Server is the HTTP API server for the menubar app.
type Server struct {
	mu sync.RWMutex

	config         ServerConfig
	configService  *ConfigService
	daemonCtrl     DaemonController
	metricsService MetricsService
	services       *services.ServiceRegistry
	logger         *slog.Logger
	server         *http.Server
	running        bool
}

// AgentInfo describes an agent for listing.
type AgentInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

// Agent describes a full agent configuration.
type Agent struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Prompt      string         `json:"prompt"`
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
	Enabled     bool           `json:"enabled"`
}

// NewServer creates a new HTTP API server.
func NewServer(cfg ServerConfig, configSvc *ConfigService, daemonCtrl DaemonController, metricsSvc MetricsService, svcRegistry *services.ServiceRegistry, logger *slog.Logger) *Server {
	if cfg.Addr == "" {
		cfg.Addr = ":8081"
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Server{
		config:         cfg,
		configService:  configSvc,
		daemonCtrl:     daemonCtrl,
		metricsService: metricsSvc,
		services:       svcRegistry,
		logger:         logger,
	}
}

// Start starts the HTTP server.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("server is already running")
	}
	s.running = true
	s.mu.Unlock()

	mux := http.NewServeMux()
	s.setupRoutes(mux)

	s.server = &http.Server{
		Addr:           s.config.Addr,
		Handler:        s.middleware(mux),
		ReadTimeout:    s.config.ReadTimeout,
		WriteTimeout:   s.config.WriteTimeout,
		MaxHeaderBytes: s.config.MaxHeaderBytes,
	}

	s.logger.Info("menubar HTTP server starting", "addr", s.config.Addr)

	errCh := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return s.Shutdown(context.Background())
	}
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.logger.Info("menubar HTTP server shutting down")
	s.running = false

	if s.server != nil {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return s.server.Shutdown(ctx)
	}
	return nil
}

// setupRoutes configures the HTTP routes.
func (s *Server) setupRoutes(mux *http.ServeMux) {
	// Health check
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)

	// Config endpoints
	mux.HandleFunc("GET /api/v1/config/client", s.handleGetClientConfig)
	mux.HandleFunc("POST /api/v1/config/client", s.handleSaveClientConfig)
	mux.HandleFunc("GET /api/v1/config/models", s.handleGetModelsConfig)
	mux.HandleFunc("POST /api/v1/config/models", s.handleSaveModelsConfig)
	mux.HandleFunc("GET /api/v1/config/menubar", s.handleGetMenubarConfig)
	mux.HandleFunc("POST /api/v1/config/menubar", s.handleSaveMenubarConfig)
	mux.HandleFunc("GET /api/v1/config/agents", s.handleListAgents)
	mux.HandleFunc("GET /api/v1/config/agents/{id}", s.handleGetAgent)
	mux.HandleFunc("POST /api/v1/config/agents/{id}", s.handleSaveAgent)
	mux.HandleFunc("DELETE /api/v1/config/agents/{id}", s.handleDeleteAgent)

	// Daemon control
	mux.HandleFunc("GET /api/v1/daemon/status", s.handleDaemonStatus)
	mux.HandleFunc("POST /api/v1/daemon/restart", s.handleDaemonRestart)

	// Metrics
	mux.HandleFunc("GET /api/v1/metrics/live", s.handleLiveMetrics)
	mux.HandleFunc("GET /api/v1/metrics/historical", s.handleHistoricalMetrics)
	mux.HandleFunc("GET /api/v1/metrics/stream", s.handleMetricsStream)
	// Chat endpoints
	mux.HandleFunc("POST /api/v1/chat", s.handleChat)

	// Memory endpoints
	mux.HandleFunc("POST /api/v1/memory/query", s.handleMemoryQuery)
	mux.HandleFunc("GET /api/v1/memory/recent", s.handleMemoryRecent)
	mux.HandleFunc("POST /api/v1/memory/export", s.handleMemoryExport)

	// Queue endpoints
	mux.HandleFunc("POST /api/v1/queue/jobs", s.handleQueueEnqueue)
	mux.HandleFunc("GET /api/v1/queue/jobs", s.handleQueueList)
	mux.HandleFunc("GET /api/v1/queue/jobs/{id}", s.handleQueueGet)
	mux.HandleFunc("POST /api/v1/queue/jobs/{id}/claim", s.handleQueueClaim)
	mux.HandleFunc("POST /api/v1/queue/jobs/{id}/complete", s.handleQueueComplete)
	mux.HandleFunc("POST /api/v1/queue/jobs/{id}/fail", s.handleQueueFail)
	mux.HandleFunc("POST /api/v1/queue/jobs/{id}/retry", s.handleQueueRetry)
	mux.HandleFunc("GET /api/v1/queue/stats", s.handleQueueStats)

	// Task endpoints
	mux.HandleFunc("POST /api/v1/tasks", s.handleTaskCreate)
	mux.HandleFunc("GET /api/v1/tasks", s.handleTaskList)
	mux.HandleFunc("GET /api/v1/tasks/{id}", s.handleTaskGet)
	mux.HandleFunc("PUT /api/v1/tasks/{id}", s.handleTaskUpdate)
	mux.HandleFunc("DELETE /api/v1/tasks/{id}", s.handleTaskDelete)
	mux.HandleFunc("POST /api/v1/tasks/{id}/cancel", s.handleTaskCancel)
	mux.HandleFunc("GET /api/v1/tasks/{id}/steps", s.handleTaskSteps)

	// Session endpoints
	mux.HandleFunc("POST /api/v1/sessions", s.handleSessionCreate)
	mux.HandleFunc("GET /api/v1/sessions", s.handleSessionList)
	mux.HandleFunc("GET /api/v1/sessions/{id}", s.handleSessionGet)
	mux.HandleFunc("DELETE /api/v1/sessions/{id}", s.handleSessionDelete)
	mux.HandleFunc("POST /api/v1/sessions/{id}/attach", s.handleSessionAttach)
	mux.HandleFunc("POST /api/v1/sessions/{id}/detach", s.handleSessionDetach)

	// Worker endpoints
	mux.HandleFunc("GET /api/v1/workers/stats", s.handleWorkerStats)
	mux.HandleFunc("POST /api/v1/workers", s.handleWorkerAdd)
	mux.HandleFunc("DELETE /api/v1/workers/{id}", s.handleWorkerRemove)
	mux.HandleFunc("POST /api/v1/workers/scale", s.handleWorkerScale)

	// Skills endpoints
	mux.HandleFunc("GET /api/v1/skills", s.handleSkillsList)
	mux.HandleFunc("GET /api/v1/skills/{slug}", s.handleSkillsGet)
	mux.HandleFunc("POST /api/v1/skills/{slug}/execute", s.handleSkillsExecute)

	// Self-improve endpoints
	mux.HandleFunc("GET /api/v1/selfimprove/status", s.handleSelfImproveStatus)
	mux.HandleFunc("POST /api/v1/selfimprove/trigger", s.handleSelfImproveTrigger)
	mux.HandleFunc("POST /api/v1/selfimprove/analyze", s.handleSelfImproveAnalyze)
	mux.HandleFunc("POST /api/v1/selfimprove/generate", s.handleSelfImproveGenerate)
	mux.HandleFunc("POST /api/v1/selfimprove/validate", s.handleSelfImproveValidate)
	mux.HandleFunc("POST /api/v1/selfimprove/apply", s.handleSelfImproveApply)
	mux.HandleFunc("POST /api/v1/selfimprove/reject", s.handleSelfImproveReject)

	// Cache endpoints
	mux.HandleFunc("GET /api/v1/cache/stats", s.handleCacheStats)
	mux.HandleFunc("POST /api/v1/cache/clear", s.handleCacheClear)
	mux.HandleFunc("POST /api/v1/cache/invalidate", s.handleCacheInvalidate)
	mux.HandleFunc("GET /api/v1/cache/inspect", s.handleCacheInspect)

	// Security endpoints
	mux.HandleFunc("POST /api/v1/security/check", s.handleSecurityCheck)

	// Scheduler endpoints
	mux.HandleFunc("GET /api/v1/scheduler/jobs", s.handleSchedulerListJobs)
	mux.HandleFunc("POST /api/v1/scheduler/jobs", s.handleSchedulerAddJob)

	// Bus endpoints
	mux.HandleFunc("POST /api/v1/bus/publish", s.handleBusPublish)
	mux.HandleFunc("GET /api/v1/bus/stats", s.handleBusStats)

}

// middleware applies common middleware (CORS, logging, auth).
func (s *Server) middleware(next http.Handler) http.Handler {
	// Create auth middleware if API keys are configured
	var authMiddleware func(http.Handler) http.Handler
	if s.config.RequireAuth && len(s.config.APIKeys) > 0 {
		authMiddleware = NewAPIKeyAuth(s.config.APIKeys).Middleware
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// CORS headers
		if s.config.EnableCORS {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}
		}

		// Wrap response writer to capture status code
		lrw := &loggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		handler := next
		if authMiddleware != nil {
			handler = authMiddleware(next)
		}

		handler.ServeHTTP(lrw, r)

		s.logger.Debug("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", lrw.statusCode,
			"duration", time.Since(start))
	})
}

// loggingResponseWriter wraps http.ResponseWriter to capture the status code.
type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

// writeJSON writes a JSON response.
func (s *Server) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("failed to encode response", "error", err)
	}
}

// writeError writes an error response.
func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]string{"error": message})
}

// handleHealth handles health check requests.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleGetClientConfig handles GET /api/v1/config/client.
func (s *Server) handleGetClientConfig(w http.ResponseWriter, r *http.Request) {
	if s.configService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "config service not available")
		return
	}

	content, err := s.configService.LoadClientConfig()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json5")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(content))
}

// handleSaveClientConfig handles POST /api/v1/config/client.
func (s *Server) handleSaveClientConfig(w http.ResponseWriter, r *http.Request) {
	if s.configService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "config service not available")
		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.configService.SaveClientConfig(body.Content); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// handleGetModelsConfig handles GET /api/v1/config/models.
func (s *Server) handleGetModelsConfig(w http.ResponseWriter, r *http.Request) {
	if s.configService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "config service not available")
		return
	}

	content, err := s.configService.LoadModelsConfig()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json5")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(content))
}

// handleSaveModelsConfig handles POST /api/v1/config/models.
func (s *Server) handleSaveModelsConfig(w http.ResponseWriter, r *http.Request) {
	if s.configService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "config service not available")
		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.configService.SaveModelsConfig(body.Content); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// handleListAgents handles GET /api/v1/config/agents.
func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	if s.configService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "config service not available")
		return
	}

	agents, err := s.configService.ListAgents()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"agents": agents,
		"count":  len(agents),
	})
}

// handleGetAgent handles GET /api/v1/config/agents/{id}.
func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	if s.configService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "config service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "agent id is required")
		return
	}

	agent, err := s.configService.GetAgent(id)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "agent not found: "+id)
		return
	}

	s.writeJSON(w, http.StatusOK, agent)
}

// handleSaveAgent handles POST /api/v1/config/agents/{id}.
func (s *Server) handleSaveAgent(w http.ResponseWriter, r *http.Request) {
	if s.configService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "config service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "agent id is required")
		return
	}

	var agent Agent
	if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.configService.SaveAgent(id, &agent); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// handleDeleteAgent handles DELETE /api/v1/config/agents/{id}.
func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	if s.configService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "config service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "agent id is required")
		return
	}

	if err := s.configService.DeleteAgent(id); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleDaemonStatus handles GET /api/v1/daemon/status.
func (s *Server) handleDaemonStatus(w http.ResponseWriter, r *http.Request) {
	if s.daemonCtrl == nil {
		s.writeError(w, http.StatusServiceUnavailable, "daemon controller not available")
		return
	}

	running := s.daemonCtrl.IsRunning()
	state := "offline"
	if running {
		state = "idle"
		// Check for active work via metrics
		if s.metricsService != nil {
			if metrics, err := s.metricsService.GetLiveMetrics(); err == nil {
				if metrics.ActiveAgents > 0 || metrics.QueueDepth > 0 {
					state = "working"
				} else if metrics.ModelFailovers > 0 {
					state = "error"
				}
			}
		}
	}

	status := map[string]any{
		"running": running,
		"pid":     0,
		"uptime":  "",
		"state":   state,
	}

	if s.daemonCtrl.IsRunning() {
		status["pid"] = s.daemonCtrl.PID()
		status["uptime"] = s.daemonCtrl.Uptime().String()
	}

	s.writeJSON(w, http.StatusOK, status)
}

// handleDaemonRestart handles POST /api/v1/daemon/restart.
func (s *Server) handleDaemonRestart(w http.ResponseWriter, r *http.Request) {
	if s.daemonCtrl == nil {
		s.writeError(w, http.StatusServiceUnavailable, "daemon controller not available")
		return
	}

	if err := s.daemonCtrl.Restart(r.Context()); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "restarted"})
}

// handleLiveMetrics handles GET /api/v1/metrics/live.
func (s *Server) handleLiveMetrics(w http.ResponseWriter, r *http.Request) {
	if s.metricsService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "metrics service not available")
		return
	}

	metrics, err := s.metricsService.GetLiveMetrics()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, metrics)
}

// handleHistoricalMetrics handles GET /api/v1/metrics/historical.
func (s *Server) handleHistoricalMetrics(w http.ResponseWriter, r *http.Request) {
	if s.metricsService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "metrics service not available")
		return
	}

	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	resolution := r.URL.Query().Get("resolution")

	if fromStr == "" || toStr == "" {
		s.writeError(w, http.StatusBadRequest, "from and to parameters are required")
		return
	}

	from, err := time.Parse(time.RFC3339, fromStr)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid from parameter: "+err.Error())
		return
	}

	to, err := time.Parse(time.RFC3339, toStr)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid to parameter: "+err.Error())
		return
	}

	if resolution == "" {
		resolution = "hour"
	}

	points, err := s.metricsService.GetHistoricalMetrics(r.Context(), from, to, resolution)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"points": points,
		"count":  len(points),
	})
}

// handleMetricsStream handles GET /api/v1/metrics/stream (WebSocket).
func (s *Server) handleMetricsStream(w http.ResponseWriter, r *http.Request) {
	if s.metricsService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "metrics service not available")
		return
	}

	// WebSocket upgrade would be handled here
	// For now, return a simple SSE-style response
	s.writeJSON(w, http.StatusOK, map[string]string{
		"status":  "websocket_not_implemented",
		"message": "use polling as fallback",
	})
}

// handleGetMenubarConfig handles GET /api/v1/config/menubar.
func (s *Server) handleGetMenubarConfig(w http.ResponseWriter, r *http.Request) {
	if s.configService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "config service not available")
		return
	}
	content, err := s.configService.LoadMenubarConfig()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json5")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(content))
}

// handleSaveMenubarConfig handles POST /api/v1/config/menubar.
func (s *Server) handleSaveMenubarConfig(w http.ResponseWriter, r *http.Request) {
	if s.configService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "config service not available")
		return
	}
	var body struct{ Content string `json:"content"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.configService.SaveMenubarConfig(body.Content); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}
