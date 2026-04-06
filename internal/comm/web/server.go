// Package web provides the HTTP API server for meept.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ServerConfig holds configuration for the HTTP server.
type ServerConfig struct {
	Addr           string        // Listen address (default: :8080)
	ReadTimeout    time.Duration // Read timeout
	WriteTimeout   time.Duration // Write timeout
	MaxHeaderBytes int           // Max header size
	EnableCORS     bool          // Enable CORS headers
}

// DefaultServerConfig returns sensible defaults.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Addr:           ":8080",
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1 MB
		EnableCORS:     false,
	}
}

// Handler is the interface for request handlers.
type Handler interface {
	// Chat handles a chat request.
	Chat(ctx context.Context, message string) (string, error)
	// Status returns the daemon status.
	Status(ctx context.Context) (map[string]any, error)
}

// MemorySearcher provides memory search functionality.
type MemorySearcher interface {
	Search(ctx context.Context, query string, limit int) ([]MemorySearchResult, error)
}

// MemorySearchResult is a simplified memory search result.
type MemorySearchResult struct {
	ID        string         `json:"id"`
	Content   string         `json:"content"`
	Type      string         `json:"type"`
	Category  string         `json:"category"`
	CreatedAt string         `json:"created_at"`
	Score     float64        `json:"score"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// SkillsLister provides skills listing functionality.
type SkillsLister interface {
	List() []SkillInfo
}

// SkillInfo is a simplified skill information.
type SkillInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Priority    int      `json:"priority"`
}

// JobsLister provides scheduled jobs listing functionality.
type JobsLister interface {
	ListJobs() ([]JobInfo, error)
}

// JobInfo is a simplified job information.
type JobInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Schedule    string `json:"schedule"`
	NextRun     string `json:"next_run,omitempty"`
	LastRun     string `json:"last_run,omitempty"`
	Status      string `json:"status"`
	Paused      bool   `json:"paused"`
}

// Server is the HTTP API server.
type Server struct {
	mu sync.RWMutex

	config   ServerConfig
	handler  Handler
	auth     Authenticator
	logger   *slog.Logger
	server   *http.Server
	running  bool

	// Optional service dependencies for API endpoints
	memorySearcher MemorySearcher
	skillsLister   SkillsLister
	jobsLister     JobsLister
}

// ServerOption is a functional option for configuring a Server.
type ServerOption func(*Server)

// WithMemorySearcher sets the memory searcher for the server.
func WithMemorySearcher(ms MemorySearcher) ServerOption {
	return func(s *Server) {
		s.memorySearcher = ms
	}
}

// WithSkillsLister sets the skills lister for the server.
func WithSkillsLister(sl SkillsLister) ServerOption {
	return func(s *Server) {
		s.skillsLister = sl
	}
}

// WithJobsLister sets the jobs lister for the server.
func WithJobsLister(jl JobsLister) ServerOption {
	return func(s *Server) {
		s.jobsLister = jl
	}
}

// NewServer creates a new HTTP API server.
func NewServer(cfg ServerConfig, handler Handler, auth Authenticator, logger *slog.Logger, opts ...ServerOption) *Server {
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}
	if logger == nil {
		logger = slog.Default()
	}

	s := &Server{
		config:  cfg,
		handler: handler,
		auth:    auth,
		logger:  logger,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
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

	s.logger.Info("HTTP server starting", "addr", s.config.Addr)

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

	s.logger.Info("HTTP server shutting down")

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

	// API routes
	mux.HandleFunc("GET /api/v1/status", s.handleStatus)
	mux.HandleFunc("POST /api/v1/chat", s.handleChat)
	mux.HandleFunc("POST /api/v1/query", s.handleChat) // Alias

	// Memory
	mux.HandleFunc("GET /api/v1/memory/search", s.handleMemorySearch)

	// Skills
	mux.HandleFunc("GET /api/v1/skills", s.handleSkillsList)

	// Jobs
	mux.HandleFunc("GET /api/v1/jobs", s.handleJobsList)
}

// middleware applies common middleware.
func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// CORS headers
		if s.config.EnableCORS {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}
		}

		// Authentication
		if s.auth != nil && !s.auth.Authenticate(r) {
			s.writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		// Wrap response writer to capture status code
		lrw := &loggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(lrw, r)

		s.logger.Info("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", lrw.statusCode,
			"duration", time.Since(start))
	})
}

// handleHealth handles health check requests.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleStatus handles status requests.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if s.handler == nil {
		s.writeError(w, http.StatusServiceUnavailable, "handler not available")
		return
	}

	status, err := s.handler.Status(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, status)
}

// handleChat handles chat requests.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if s.handler == nil {
		s.writeError(w, http.StatusServiceUnavailable, "handler not available")
		return
	}

	var req struct {
		Message string `json:"message"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Message == "" {
		s.writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	response, err := s.handler.Chat(r.Context(), req.Message)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"response": response})
}

// handleMemorySearch handles memory search requests.
func (s *Server) handleMemorySearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		s.writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}

	limit := 20 // Default limit
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if n, err := parseLimit(limitStr, 100); err == nil {
			limit = n
		}
	}

	if s.memorySearcher == nil {
		s.writeJSON(w, http.StatusOK, map[string]any{
			"results": []any{},
			"query":   query,
			"message": "memory search not configured",
		})
		return
	}

	results, err := s.memorySearcher.Search(r.Context(), query, limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "memory search failed: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"results": results,
		"query":   query,
		"count":   len(results),
	})
}

// handleSkillsList handles skills list requests.
func (s *Server) handleSkillsList(w http.ResponseWriter, r *http.Request) {
	if s.skillsLister == nil {
		s.writeJSON(w, http.StatusOK, map[string]any{
			"skills":  []any{},
			"message": "skills listing not configured",
		})
		return
	}

	skills := s.skillsLister.List()

	// Filter by tags if requested
	if tagsParam := r.URL.Query().Get("tags"); tagsParam != "" {
		requestedTags := strings.Split(tagsParam, ",")
		for i := range requestedTags {
			requestedTags[i] = strings.TrimSpace(requestedTags[i])
		}

		var filtered []SkillInfo
		for _, skill := range skills {
			if hasAnyTag(skill.Tags, requestedTags) {
				filtered = append(filtered, skill)
			}
		}
		skills = filtered
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"skills": skills,
		"count":  len(skills),
	})
}

// handleJobsList handles jobs list requests.
func (s *Server) handleJobsList(w http.ResponseWriter, r *http.Request) {
	if s.jobsLister == nil {
		s.writeJSON(w, http.StatusOK, map[string]any{
			"jobs":    []any{},
			"message": "jobs listing not configured",
		})
		return
	}

	jobs, err := s.jobsLister.ListJobs()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list jobs: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"jobs":  jobs,
		"count": len(jobs),
	})
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

// loggingResponseWriter wraps http.ResponseWriter to capture the status code.
type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

// hasAnyTag checks if skill tags contain any of the requested tags.
func hasAnyTag(skillTags, requestedTags []string) bool {
	for _, requested := range requestedTags {
		for _, tag := range skillTags {
			if strings.EqualFold(tag, requested) {
				return true
			}
		}
	}
	return false
}

// parseLimit parses a limit string and clamps it to the given max.
func parseLimit(s string, max int) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if n < 1 {
		n = 1
	}
	if n > max {
		n = max
	}
	return n, nil
}
