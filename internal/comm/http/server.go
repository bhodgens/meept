// Package http provides the unified HTTP server for the Meept daemon (REST API, WebSocket, MCP).
package http

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/mcp"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/services"
	"github.com/caimlas/meept/pkg/constants"
	"github.com/caimlas/meept/pkg/models"

	"golang.org/x/net/websocket"
)

const maxRequestBodySize = 1 << 20 // 1 MB

var defaultWSOrigins = []string{"localhost", "127.0.0.1", "::1", "null", ""}

// ServerConfig holds configuration for the HTTP server.
// TLS is always enabled; there is no option to disable HTTPS.
type ServerConfig struct {
	Addr                    string        // Listen address (default: :8081)
	ReadTimeout             time.Duration // Read timeout
	WriteTimeout            time.Duration // Write timeout
	MaxHeaderBytes          int           // Max header size
	EnableCORS              bool          // Enable CORS headers
	APIKeys                 []string      // Valid API keys for authentication
	RequireAuth             bool          // Require API key authentication (default: true)
	TLSCertFile             string        // TLS certificate file path
	TLSKeyFile              string        // TLS key file path
	RESTEnabled             bool                  // Enable REST API at /api/v1/* (default: true)
	WebSocketAllowedOrigins []string              // Allowed origins for WebSocket (default: localhost, 127.0.0.1, ::1, null, "")
	SecurityHeaders         SecurityHeadersConfig // HSTS, CSP, X-Frame-Options, etc.
	TLSMinVersion           uint16                // Default: tls.VersionTLS12
	TLSClientAuth           tls.ClientAuthType    // Default: tls.NoClientCert
	FingerprintFile         string                // Path to write cert fingerprint for client discovery
}

// DefaultServerConfig returns sensible defaults for the unified HTTP server.
// TLS is always enabled; a self-signed cert is auto-generated if needed.
func DefaultServerConfig() ServerConfig {
	homeDir, _ := os.UserHomeDir()
	defaultCertFile := filepath.Join(homeDir, ".meept", "tls", "cert.pem")
	defaultKeyFile := filepath.Join(homeDir, ".meept", "tls", "key.pem")

	return ServerConfig{
		Addr:           ":8081", // Different from existing web server (:8080)
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1 MB
		EnableCORS:     true,    // Enable CORS for local HTTP clients
		RequireAuth:    true,    // Enabled by default for security
		APIKeys:        []string{},
		TLSCertFile:    defaultCertFile,
		TLSKeyFile:     defaultKeyFile,
		RESTEnabled:    true, // REST API enabled by default
		SecurityHeaders: DefaultSecurityHeaders(),
		TLSMinVersion:   tls.VersionTLS12,
		TLSClientAuth:   tls.NoClientCert,
		FingerprintFile: filepath.Join(homeDir, ".meept", "tls", "fingerprint.txt"),
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

// Server is the unified HTTP API server for the Meept daemon.
type Server struct {
	mu sync.RWMutex

	config         ServerConfig
	configService  *ConfigService
	daemonCtrl     DaemonController
	metricsService MetricsService
	services       *services.ServiceRegistry
	logger         *slog.Logger
	server         *http.Server
	listener       net.Listener
	running        bool
	// FirewallStatsGetter is an optional callback that returns firewall stats.
	FirewallStatsGetter func() map[string]any

	// RateLimitSummaryGetter is an optional callback that returns rate limit summary.
	RateLimitSummaryGetter func(ctx context.Context, recentLimit int) (map[string]any, error)

	// BudgetStatusGetter is an optional callback that returns budget stats.
	// Used by the status handler to report actual token and cost usage.
	BudgetStatusGetter func() (hourlyUsed int, hourlyRemaining int, dailyUsed int, dailyRemaining int, rpmCurrent int, rpmLimit int, dailyCostUsed float64, dailyCostLimit float64, hourlyCostUsed float64, hourlyCostLimit float64)

	wsHub *WebSocketHub

	// MCP over HTTP+SSE support
	mcpServices *services.ServiceRegistry
	mcpSessions sync.Map // map[string]*MCPSession
	mcpPath     string
	wsPath      string
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
func NewServer(cfg ServerConfig, configSvc *ConfigService, daemonCtrl DaemonController, metricsSvc MetricsService, svcRegistry *services.ServiceRegistry, logger *slog.Logger, opts ...ServerOption) *Server {
	if cfg.Addr == "" {
		cfg.Addr = ":8081"
	}
	if logger == nil {
		logger = slog.Default()
	}

	if cfg.RequireAuth && len(cfg.APIKeys) == 0 {
		cfg.APIKeys = []string{constants.DefaultDevAPIKey}
		logger.Warn("using default development API key",
			"hint", "replace with a generated key via `meept token generate --save` for production",
			"default_key_visible", false) // key is never logged
	}

	s := &Server{
		config:         cfg,
		configService:  configSvc,
		daemonCtrl:     daemonCtrl,
		metricsService: metricsSvc,
		services:       svcRegistry,
		logger:         logger,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// WebSocketHub manages WebSocket client connections and broadcasts messages.
type WebSocketHub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]struct{}
	logger  *slog.Logger
}

// NewWebSocketHub creates a new WebSocket hub.
func NewWebSocketHub(logger *slog.Logger) *WebSocketHub {
	if logger == nil {
		logger = slog.Default()
	}
	return &WebSocketHub{
		clients: make(map[*websocket.Conn]struct{}),
		logger:  logger,
	}
}

// Register adds a WebSocket client connection.
func (h *WebSocketHub) Register(conn *websocket.Conn) {
	h.mu.Lock()
	h.clients[conn] = struct{}{}
	h.mu.Unlock()
	h.logger.Debug("ws client registered", "remote", conn.RemoteAddr())
}

// Unregister removes a WebSocket client connection and closes it.
func (h *WebSocketHub) Unregister(conn *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, conn)
	h.mu.Unlock()
	conn.Close()
	h.logger.Debug("ws client unregistered", "remote", conn.RemoteAddr())
}

// ClientCount returns the number of connected clients.
func (h *WebSocketHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Broadcast sends a typed message to all connected WebSocket clients.
func (h *WebSocketHub) Broadcast(msgType string, data any) {
	payload, err := json.Marshal(map[string]any{
		"type": msgType,
		"data": data,
	})
	if err != nil {
		h.logger.Error("ws broadcast marshal error", "error", err)
		return
	}

	var failedConns []*websocket.Conn

	h.mu.RLock()
	conns := make([]*websocket.Conn, 0, len(h.clients))
	for conn := range h.clients {
		conns = append(conns, conn)
	}
	h.mu.RUnlock()

	for _, conn := range conns {
		if _, err := conn.Write(payload); err != nil {
			h.logger.Warn("ws write error, will remove client", "error", err)
			failedConns = append(failedConns, conn)
		}
	}

	for _, conn := range failedConns {
		h.Unregister(conn)
	}
}

// WithWebSocket enables WebSocket support.
func WithWebSocket(msgBus *bus.MessageBus, wsPath string) ServerOption {
	return func(s *Server) {
		if msgBus == nil {
			return
		}
		if wsPath == "" {
			wsPath = "/ws"
		}
		s.wsPath = wsPath
		s.wsHub = NewWebSocketHub(s.logger)

		// Subscribe to all bus topic patterns that produce frontend events.
		// The bus wildcard "*" only matches single-segment topics, so we
		// subscribe to multiple prefixes used by the agent system.
		topics := []string{"*", "agent.*", "agent.*.*", "task.*", "task.*.*", "step.*", "step.*.*", "orchestrator.*",
			"chat.*", "chat.*.*", "tool.*", "llm.*", "review.*"}
		for _, topic := range topics {
			sub := msgBus.Subscribe("http-ws-"+topic, topic)
			go func(sub *bus.Subscriber) {
				for msg := range sub.Channel {
					s.handleWSEvent(msg)
				}
			}(sub)
		}
	}
}

// handleWSEvent transforms a bus message into a frontend-friendly WebSocket event
// and broadcasts it to all subscribed clients.
func (s *Server) handleWSEvent(msg *models.BusMessage) {
	if s.wsHub == nil || msg == nil {
		return
	}

	frontendData := transformBusEventToWS(msg)
	if frontendData == nil {
		return // unrecognized topic, skip
	}
	eventType, ok := frontendData["type"].(string)
	if !ok || eventType == "" {
		return
	}
	s.wsHub.Broadcast(eventType, frontendData)
}

// transformBusEventToWS converts a bus event into a frontend-compatible flat map.
// Returns nil if the event should not be sent to the frontend.
func transformBusEventToWS(msg *models.BusMessage) map[string]any {
	topic := msg.Topic
	if topic == "" {
		return nil
	}

	// Unmarshal the payload once for inspection
	var payload map[string]any
	if msg.Payload != nil && len(msg.Payload) > 0 {
		_ = json.Unmarshal(msg.Payload, &payload)
	}

	var eventType string
	switch {
	case strings.HasPrefix(topic, "chat.") || topic == "chat_message":
		// All chat-related events → chat_message
		eventType = "chat_message"
	case strings.HasPrefix(topic, "metrics."):
		eventType = "metrics_update"
	case strings.HasPrefix(topic, "task.") || strings.HasPrefix(topic, "step.") || strings.HasPrefix(topic, "job."):
		eventType = "job_update"
	default:
		// Generic fallback instead of mislabeling as job_update
		eventType = "event"
	}

	if payload == nil {
		payload = make(map[string]any)
	}

	// Normalize chat response fields so Flutter's WebSocketService can
	// route by session_id and display content as a chat message.
	if eventType == "chat_message" {
		if convID, ok := payload["conversation_id"].(string); ok && convID != "" {
			payload["session_id"] = convID
		}
		if reply, ok := payload["reply"].(string); ok {
			payload["content"] = reply
		}
		if _, ok := payload["role"]; !ok {
			payload["role"] = "assistant"
		}
		if _, ok := payload["id"]; !ok {
			payload["id"] = msg.ID
		}
	}

	// Add the source topic as metadata
	payload["source_topic"] = topic

	// If there's no timestamp, add one
	if _, hasTS := payload["timestamp"]; !hasTS && !msg.Timestamp.IsZero() {
		payload["timestamp"] = msg.Timestamp.Format(time.RFC3339)
	}

	payload["type"] = eventType
	return payload
}

// mcpEventRecord stores a buffered bus event for MCP polling.
type mcpEventRecord struct {
	Topic     string    `json:"topic"`
	Type      string    `json:"type"`
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`
	Payload   any       `json:"payload"`
}

// MCPSession represents an MCP over HTTP+SSE client session.
type MCPSession struct {
	mu        sync.RWMutex
	sessionID string
	eventChan chan *SSEEvent
	done      chan struct{}
	events    []mcpEventRecord // buffered events for meept_events polling
}

// SSEEvent represents a Server-Sent Event.
type SSEEvent struct {
	ID   string
	Type string
	Data []byte
}

// WithMCP enables MCP over HTTP+SSE support.
func WithMCP(services *services.ServiceRegistry, mcpPath string) ServerOption {
	return func(s *Server) {
		if services == nil {
			return
		}
		s.mcpServices = services
		if mcpPath == "" {
			mcpPath = "/mcp"
		}
		s.mcpPath = mcpPath
	}
}

// ServerOption is a functional option for configuring a Server.
type ServerOption func(*Server)

// Addr returns the actual address the server is listening on. Useful when
// binding to :0 to discover the kernel-assigned port.
func (s *Server) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.config.Addr
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

	// Chain middleware: security headers first (always applied), then auth/CORS/logging
	handler := s.middleware(SecurityHeadersMiddleware(s.config.SecurityHeaders)(mux))

	// Generate TLS cert if needed — TLS is mandatory, fail hard if we can't create one
	if err := s.ensureTLSCert(); err != nil {
		return fmt.Errorf("failed to ensure TLS certificate: %w", err)
	}

	// Compute and persist fingerprint so clients can pin this certificate
	if err := s.ensureFingerprint(); err != nil {
		s.logger.Warn("failed to write certificate fingerprint", "error", err)
	}

	// Build hardened TLS config
	tlsConfig := BuildTLSConfig(s.config.TLSMinVersion, s.config.TLSClientAuth)

	s.server = &http.Server{
		Addr:           s.config.Addr,
		Handler:        handler,
		ReadTimeout:    s.config.ReadTimeout,
		WriteTimeout:   s.config.WriteTimeout,
		MaxHeaderBytes: s.config.MaxHeaderBytes,
		TLSConfig:      tlsConfig,
	}
	// Disable HTTP/2 — the golang.org/x/net/websocket handler does not
	// support HTTP/2 and Flutter's dart:io WebSocket expects HTTP/1.1
	// upgrade.  Without this, TLS-enabled servers get PROTOCOL_ERROR on
	// every request because Go enables HTTP/2 automatically with TLS.
	s.server.TLSNextProto = make(map[string]func(*http.Server, *tls.Conn, http.Handler))

	s.logger.Info("unified HTTP server starting with TLS",
		"addr", s.config.Addr,
		"cert_file", s.config.TLSCertFile,
		"tls_min_version", s.config.TLSMinVersion,
		"client_auth", s.config.TLSClientAuth,
	)

	errCh := make(chan error, 1)
	go func() {
		ln, listenErr := net.Listen("tcp", s.config.Addr)
		if listenErr != nil {
			s.logger.Error("failed to listen on TCP", "addr", s.config.Addr, "error", listenErr)
			errCh <- listenErr
			return
		}
		s.mu.Lock()
		// Wrap the listener to detect plain HTTP and return 426
		s.listener = &tlsDetectListener{Listener: ln, logger: s.logger}
		s.mu.Unlock()

		if err := s.server.ServeTLS(s.listener, s.config.TLSCertFile, s.config.TLSKeyFile); err != nil && err != http.ErrServerClosed {
			s.logger.Error("HTTP server TLS error", "error", err, "addr", s.config.Addr, "cert_file", s.config.TLSCertFile)
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

// tlsDetectListener wraps a net.Listener to detect plain HTTP connections.
// When a connection starts with an ASCII letter (likely an HTTP method name),
// it returns 426 Upgrade Required and closes the connection.
type tlsDetectListener struct {
	net.Listener
	logger *slog.Logger
}

func (l *tlsDetectListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		// Peek at the first byte with a short timeout
		if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			conn.Close()
			continue
		}
		var first [1]byte
		n, readErr := conn.Read(first[:])
		if err := conn.SetReadDeadline(time.Time{}); err != nil {
			conn.Close()
			continue
		}
		if readErr != nil || n == 0 {
			conn.Close()
			continue
		}
		// HTTP methods start with a letter (G, P, D, H, O, T, C, ...).
		// TLS ClientHello starts with 0x16; SSL 2.0 starts with 0x80.
		b := first[0]
		if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') {
			l.logger.Warn("plain HTTP detected on TLS port",
				"remote", conn.RemoteAddr(),
				"first_byte", string(rune(b)),
				"hint", "client must use HTTPS")
			resp := []byte("HTTP/1.1 426 Upgrade Required\r\nContent-Type: application/json\r\nConnection: close\r\nContent-Length: 77\r\n\r\n{\"error\":\"upgrade required\",\"message\":\"use HTTPS for this endpoint\"}")
			conn.Write(resp)
			conn.Close()
			continue
		}
		// Restore the peeked byte so the TLS stack sees the full ClientHello.
		return &peekConn{Conn: conn, peeked: first[:n]}, nil
	}
}

// peekConn wraps a net.Conn, prepending peeked bytes before normal reads.
type peekConn struct {
	net.Conn
	peeked []byte
}

func (c *peekConn) Read(b []byte) (int, error) {
	if len(c.peeked) > 0 {
		n := copy(b, c.peeked)
		c.peeked = c.peeked[n:]
		if n == len(b) || len(c.peeked) > 0 {
			return n, nil
		}
		m, err := c.Conn.Read(b[n:])
		return n + m, err
	}
	return c.Conn.Read(b)
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	srv := s.server
	s.mu.Unlock()

	s.logger.Info("unified HTTP server shutting down")
	if srv != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
	return nil
}

// setupRoutes configures the HTTP routes.
func (s *Server) setupRoutes(mux *http.ServeMux) {
	// Health check (always available)
	mux.HandleFunc("GET /health", s.handleHealth)

	if s.config.RESTEnabled {
		s.setupRESTRoutes(mux)
	}

	// WebSocket endpoint (if enabled)
	if s.wsHub != nil {
		mux.HandleFunc(fmt.Sprintf("GET %s", s.wsPath), s.handleWebSocket)
	}

	// MCP over HTTP+SSE endpoints (if enabled)
	if s.mcpServices != nil {
		mux.HandleFunc(fmt.Sprintf("POST %s", s.mcpPath), s.handleMCPPost)
		mux.HandleFunc(fmt.Sprintf("GET %s/sse", s.mcpPath), s.handleMCPSSE)
	}
}

// setupRESTRoutes registers all /api/v1/* REST API endpoints.
func (s *Server) setupRESTRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)

	// Config endpoints
	mux.HandleFunc("GET /api/v1/config/client", s.handleGetClientConfig)
	mux.HandleFunc("POST /api/v1/config/client", s.handleSaveClientConfig)
	mux.HandleFunc("GET /api/v1/config/models", s.handleGetModelsConfig)
	mux.HandleFunc("POST /api/v1/config/models", s.handleSaveModelsConfig)
	mux.HandleFunc("GET /api/v1/config/menubar", s.handleGetMenubarConfig)
	mux.HandleFunc("POST /api/v1/config/menubar", s.handleSaveMenubarConfig)
	mux.HandleFunc("POST /api/v1/config/normalize", s.handleNormalizeConfig)
	mux.HandleFunc("GET /api/v1/config/agents", s.handleListAgents)
	mux.HandleFunc("GET /api/v1/config/agents/{id}", s.handleGetAgent)
	mux.HandleFunc("POST /api/v1/config/agents/{id}", s.handleSaveAgent)
	mux.HandleFunc("DELETE /api/v1/config/agents/{id}", s.handleDeleteAgent)

	// Daemon control
	// Daemon control
	mux.HandleFunc("GET /api/v1/daemon/status", s.handleDaemonStatus)
	mux.HandleFunc("POST /api/v1/daemon/restart", s.handleDaemonRestart)
	mux.HandleFunc("POST /api/v1/daemon/start", s.handleDaemonStart)
	mux.HandleFunc("POST /api/v1/daemon/stop", s.handleDaemonStop)

	// Model endpoints
	mux.HandleFunc("GET /api/v1/models", s.handleModelsList)
	mux.HandleFunc("GET /api/v1/models/providers", s.handleModelsProviders)
	mux.HandleFunc("GET /api/v1/models/default", s.handleModelsGetDefault)
	mux.HandleFunc("POST /api/v1/models/default", s.handleModelsSetDefault)
	mux.HandleFunc("DELETE /api/v1/models/{provider}/{model}", s.handleModelsRemove)
	mux.HandleFunc("GET /api/v1/models/credentials/{provider}", s.handleModelsGetCredential)
	mux.HandleFunc("POST /api/v1/models/credentials/{provider}", s.handleModelsSetCredential)
	mux.HandleFunc("DELETE /api/v1/models/credentials/{provider}", s.handleModelsDeleteCredential)

	// Metrics
	mux.HandleFunc("GET /api/v1/metrics/live", s.handleLiveMetrics)
	mux.HandleFunc("GET /api/v1/metrics/historical", s.handleHistoricalMetrics)
	mux.HandleFunc("GET /api/v1/metrics/stream", s.handleMetricsStream)
	mux.HandleFunc("GET /api/v1/metrics/rate-limits", s.handleRateLimitSummary)

	// Runtime management endpoints
	mux.HandleFunc("GET /api/v1/runtime/status", s.handleRuntimeStatus)
	mux.HandleFunc("GET /api/v1/runtime/status/{provider}", s.handleRuntimeStatusProvider)
	mux.HandleFunc("POST /api/v1/runtime/start/{provider}", s.handleRuntimeStart)
	mux.HandleFunc("POST /api/v1/runtime/stop/{provider}", s.handleRuntimeStop)
	mux.HandleFunc("POST /api/v1/runtime/restart/{provider}", s.handleRuntimeRestart)

	// Chat endpoints
	mux.HandleFunc("POST /api/v1/chat", s.handleChat)
	mux.HandleFunc("GET /api/v1/chat/stream", s.handleChatStream)
	mux.HandleFunc("GET /api/v1/chat/queue/{id}", s.handleChatQueueStatus)
	mux.HandleFunc("POST /api/v1/chat/with-agent", s.handleChatWithAgent)

	// Memory endpoints
	mux.HandleFunc("POST /api/v1/memory/query", s.handleMemoryQuery)
	mux.HandleFunc("GET /api/v1/memory/recent", s.handleMemoryRecent)
	mux.HandleFunc("POST /api/v1/memory/export", s.handleMemoryExport)

	// Memory Vector endpoints
	mux.HandleFunc("POST /api/v1/memory/vector/search", s.handleMemoryVectorSearch)
	mux.HandleFunc("POST /api/v1/memory/vector/store", s.handleMemoryVectorStore)
	mux.HandleFunc("DELETE /api/v1/memory/vector/{id}", s.handleMemoryVectorDelete)
	mux.HandleFunc("GET /api/v1/memory/vector/stats", s.handleMemoryVectorStats)

	// Queue endpoints
	mux.HandleFunc("POST /api/v1/queue/jobs", s.handleQueueEnqueue)
	mux.HandleFunc("GET /api/v1/queue/jobs", s.handleQueueList)
	mux.HandleFunc("GET /api/v1/queue/jobs/{id}", s.handleQueueGet)
	mux.HandleFunc("POST /api/v1/queue/jobs/{id}/claim", s.handleQueueClaim)
	mux.HandleFunc("POST /api/v1/queue/jobs/{id}/complete", s.handleQueueComplete)
	mux.HandleFunc("POST /api/v1/queue/jobs/{id}/fail", s.handleQueueFail)
	mux.HandleFunc("POST /api/v1/queue/jobs/{id}/retry", s.handleQueueRetry)
	mux.HandleFunc("GET /api/v1/queue/stats", s.handleQueueStats)
	// Queue steering/follow-up convenience aliases
	mux.HandleFunc("POST /api/v1/queue/steer", s.handleQueueSteerRoute)
	mux.HandleFunc("POST /api/v1/queue/followup", s.handleQueueFollowUpRoute)
	mux.HandleFunc("GET /api/v1/queue/status/{id}", s.handleQueueStatusRoute)

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
	mux.HandleFunc("POST /api/v1/sessions/{id}/resume", s.handleSessionResume)
	mux.HandleFunc("POST /api/v1/sessions/{id}/branch", s.handleSessionBranch)
	mux.HandleFunc("GET /api/v1/sessions/{id}/branches", s.handleSessionBranches)
	mux.HandleFunc("POST /api/v1/sessions/{id}/fork", s.handleSessionFork)
	mux.HandleFunc("GET /api/v1/sessions/{id}/tree", s.handleSessionTree)
	mux.HandleFunc("GET /api/v1/sessions/{id}/messages", s.handleSessionMessages)
	mux.HandleFunc("POST /api/v1/sessions/{id}/compact", s.handleSessionCompact)

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

	// Scheduler endpoints
	mux.HandleFunc("GET /api/v1/scheduler/jobs", s.handleSchedulerListJobs)
	mux.HandleFunc("POST /api/v1/scheduler/jobs", s.handleSchedulerAddJob)
	mux.HandleFunc("DELETE /api/v1/scheduler/jobs/{id}", s.handleSchedulerRemoveJob)
	mux.HandleFunc("POST /api/v1/scheduler/jobs/{id}/enable", s.handleSchedulerEnableJob)
	mux.HandleFunc("POST /api/v1/scheduler/jobs/{id}/pause", s.handleSchedulerPauseJob)
	mux.HandleFunc("POST /api/v1/scheduler/jobs/{id}/resume", s.handleSchedulerResumeJob)

	// Calendar endpoints
	mux.HandleFunc("GET /api/v1/calendar/events", s.handleCalendarList)
	mux.HandleFunc("GET /api/v1/calendar/events/{id}", s.handleCalendarGet)
	mux.HandleFunc("POST /api/v1/calendar/events", s.handleCalendarCreate)
	mux.HandleFunc("PUT /api/v1/calendar/events/{id}", s.handleCalendarUpdate)
	mux.HandleFunc("DELETE /api/v1/calendar/events/{id}", s.handleCalendarDelete)
	mux.HandleFunc("GET /api/v1/calendar/today", s.handleCalendarToday)
	mux.HandleFunc("GET /api/v1/calendar/upcoming", s.handleCalendarUpcoming)
	mux.HandleFunc("POST /api/v1/calendar/quickadd", s.handleCalendarQuickAdd)

	// Terminal endpoints
	mux.HandleFunc("GET /api/v1/terminal/history", s.handleTerminalHistory)
	mux.HandleFunc("POST /api/v1/terminal/exec", s.handleTerminalExec)
	mux.HandleFunc("GET /api/v1/terminal/sessions", s.handleTerminalSessions)
	mux.HandleFunc("POST /api/v1/terminal/clear", s.handleTerminalClear)

	// Bus endpoints
	mux.HandleFunc("POST /api/v1/bus/publish", s.handleBusPublish)
	mux.HandleFunc("GET /api/v1/bus/stats", s.handleBusStats)

	// Firewall stats endpoint
	mux.HandleFunc("GET /api/v1/metrics/firewall", s.handleFirewallStats)

	// Project endpoints
	mux.HandleFunc("GET /api/v1/projects", s.handleProjectList)
	mux.HandleFunc("GET /api/v1/projects/{id}", s.handleProjectGet)
	mux.HandleFunc("POST /api/v1/projects", s.handleProjectRegister)
	mux.HandleFunc("DELETE /api/v1/projects/{id}", s.handleProjectUnregister)
	mux.HandleFunc("POST /api/v1/projects/{id}/sync", s.handleProjectSync)
	mux.HandleFunc("GET /api/v1/projects/{id}/status", s.handleProjectStatus)
	mux.HandleFunc("GET /api/v1/projects/{id}/branches", s.handleProjectBranches)
	mux.HandleFunc("POST /api/v1/projects/{id}/checkout", s.handleProjectCheckout)
	mux.HandleFunc("POST /api/v1/projects/detect", s.handleProjectDetect)

	// Plan endpoints
	mux.HandleFunc("GET /api/v1/plans", s.handlePlanList)
	mux.HandleFunc("POST /api/v1/plans", s.handlePlanCreate)
	mux.HandleFunc("GET /api/v1/plans/{id}", s.handlePlanGet)
	mux.HandleFunc("POST /api/v1/plans/{id}/approve", s.handlePlanApprove)
	mux.HandleFunc("POST /api/v1/plans/{id}/reject", s.handlePlanReject)
	mux.HandleFunc("POST /api/v1/plans/{id}/confirm", s.handlePlanConfirm)
	mux.HandleFunc("POST /api/v1/plans/{id}/revise", s.handlePlanRevise)
	mux.HandleFunc("GET /api/v1/sessions/{id}/plans", s.handleSessionPlans)

	// Search endpoint
	mux.HandleFunc("POST /api/v1/search", s.handleSearch)

	// Project branch endpoints
	mux.HandleFunc("GET /api/v1/projects/{id}/branches", s.handleProjectBranches)
	mux.HandleFunc("POST /api/v1/projects/{id}/checkout", s.handleProjectCheckout)

	// Skill UI endpoint
	mux.HandleFunc("GET /api/v1/skills/{slug}/ui", s.handleSkillUI)
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
			origin := r.Header.Get("Origin")
			if s.config.RequireAuth {
				// Authenticated endpoints: never wildcard. Echo localhost origins only.
				if origin == "" || isLocalOrigin(origin) {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					if origin != "" {
						w.Header().Set("Access-Control-Allow-Credentials", "true")
					}
				}
			} else {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
			w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")

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
// It delegates http.Hijacker and http.Flusher to the underlying writer so
// that WebSocket upgrade and SSE streaming work through the logging middleware.
type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *loggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := lrw.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

func (lrw *loggingResponseWriter) Flush() {
	if fl, ok := lrw.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
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

// isLocalOrigin checks whether an Origin header is a safe local origin.
func isLocalOrigin(origin string) bool {
	if origin == "" || origin == "null" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	for _, allowed := range defaultWSOrigins {
		if host == allowed {
			return true
		}
	}
	return false
}

// readJSON reads and decodes a JSON request body with a size limit.
func (s *Server) readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

// handleHealth handles health check requests.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "ok"})
}

// handleGetClientConfig handles GET /api/v1/config/client.
func (s *Server) handleGetClientConfig(w http.ResponseWriter, _ *http.Request) {
	if s.configService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "config service not available")
		return
	}

	content, err := s.configService.LoadClientConfig()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"content": content})
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
	if !s.readJSON(w, r, &body) {
		return
	}

	if err := s.configService.SaveClientConfig(body.Content); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: KeySaved})
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

	s.writeJSON(w, http.StatusOK, map[string]string{"content": content})
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
	if !s.readJSON(w, r, &body) {
		return
	}

	if err := s.configService.SaveModelsConfig(body.Content); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: KeySaved})
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
		KeyCount: len(agents),
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
	if !s.readJSON(w, r, &agent) {
		return
	}

	if err := s.configService.SaveAgent(id, &agent); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: KeySaved})
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

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "deleted"})
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
			if liveMetrics, err := s.metricsService.GetLiveMetrics(); err == nil {
				if liveMetrics.ActiveAgents > 0 || liveMetrics.QueueDepth > 0 {
					state = "working"
				} else if liveMetrics.ModelFailovers > 0 {
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

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "restarted"})
}

// handleDaemonStart handles POST /api/v1/daemon/start.
func (s *Server) handleDaemonStart(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Daemon == nil {
		s.writeError(w, http.StatusServiceUnavailable, "daemon service not available")
		return
	}

	if err := s.services.Daemon.Start(r.Context()); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

// handleDaemonStop handles POST /api/v1/daemon/stop.
func (s *Server) handleDaemonStop(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Daemon == nil {
		s.writeError(w, http.StatusServiceUnavailable, "daemon service not available")
		return
	}

	if err := s.services.Daemon.Stop(r.Context()); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

// handleLiveMetrics handles GET /api/v1/metrics/live.
func (s *Server) handleLiveMetrics(w http.ResponseWriter, r *http.Request) {
	if s.metricsService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "metrics service not available")
		return
	}

	liveMetrics, err := s.metricsService.GetLiveMetrics()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, liveMetrics)
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
		KeyCount: len(points),
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
		KeyStatus: "websocket_not_implemented",
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
	s.writeJSON(w, http.StatusOK, map[string]string{"content": content})
}

// handleSaveMenubarConfig handles POST /api/v1/config/menubar.
func (s *Server) handleSaveMenubarConfig(w http.ResponseWriter, r *http.Request) {
	if s.configService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "config service not available")
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if !s.readJSON(w, r, &body) {
		return
	}
	if err := s.configService.SaveMenubarConfig(body.Content); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: KeySaved})
}

// handleNormalizeConfig handles POST /api/v1/config/normalize.
func (s *Server) handleNormalizeConfig(w http.ResponseWriter, r *http.Request) {
	if s.configService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "config service not available")
		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if !s.readJSON(w, r, &body) {
		return
	}

	normalized, err := s.configService.NormalizeJSON5(body.Content)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"normalized": normalized})
}

// ensureTLSCert ensures TLS certificate and key files exist, generating self-signed if needed.
func (s *Server) ensureTLSCert() error {
	certExists := fileExists(s.config.TLSCertFile)
	keyExists := fileExists(s.config.TLSKeyFile)

	if certExists && keyExists {
		s.logger.Debug("TLS certificate files already exist")
		return nil
	}

	s.logger.Info("Generating self-signed TLS certificate...",
		"cert", s.config.TLSCertFile,
		"key", s.config.TLSKeyFile)

	// Ensure directory exists
	certDir := filepath.Dir(s.config.TLSCertFile)
	if err := os.MkdirAll(certDir, 0o700); err != nil {
		return fmt.Errorf("failed to create cert directory: %w", err)
	}

	// Generate private key
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	// Generate certificate template
	notBefore := time.Now()
	notAfter := notBefore.Add(365 * 24 * time.Hour) // Valid for 1 year

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Meept Development"},
			CommonName:   "localhost",
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		IPAddresses: []net.IP{
			net.ParseIP("127.0.0.1"),
			net.ParseIP("::1"),
		},
		DNSNames: []string{
			"localhost",
			"*.localhost",
		},
	}

	// Generate certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// Write certificate file
	certOut, err := os.Create(s.config.TLSCertFile)
	if err != nil {
		return fmt.Errorf("failed to create cert file: %w", err)
	}
	defer certOut.Close()

	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return fmt.Errorf("failed to write cert: %w", err)
	}

	// Write key file
	keyOut, err := os.Create(s.config.TLSKeyFile)
	if err != nil {
		return fmt.Errorf("failed to create key file: %w", err)
	}
	defer keyOut.Close()

	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return fmt.Errorf("failed to marshal key: %w", err)
	}

	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes}); err != nil {
		return fmt.Errorf("failed to write key: %w", err)
	}

	// Set restrictive permissions
	if err := os.Chmod(s.config.TLSCertFile, 0o600); err != nil {
		s.logger.Warn("Failed to set cert permissions", "error", err)
	}
	if err := os.Chmod(s.config.TLSKeyFile, 0o600); err != nil {
		s.logger.Warn("Failed to set key permissions", "error", err)
	}

	s.logger.Info("Self-signed TLS certificate generated",
		"cert", s.config.TLSCertFile,
		"key", s.config.TLSKeyFile,
		"valid_until", notAfter.Format(time.RFC3339))

	return nil
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ensureFingerprint computes the server certificate fingerprint and writes it
// to disk so clients can discover and pin it.
func (s *Server) ensureFingerprint() error {
	if s.config.FingerprintFile == "" {
		return nil
	}
	if !fileExists(s.config.TLSCertFile) {
		return nil
	}
	certFP, spkiFP, err := LoadCertFingerprint(s.config.TLSCertFile)
	if err != nil {
		return err
	}
	return SaveFingerprint(s.config.FingerprintFile, certFP, spkiFP)
}

// handleWebSocket handles GET /ws WebSocket connections.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.wsHub == nil {
		s.writeError(w, http.StatusServiceUnavailable, "WebSocket not enabled")
		return
	}

	// Validate API token if auth is required
	if s.config.RequireAuth {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			// Also check query param for web clients that can't set headers
			token := r.URL.Query().Get("token")
			if token == "" {
				s.writeError(w, http.StatusUnauthorized, "unauthorized: missing API token")
				return
			}
			authHeader = "Bearer " + token
		} else {
			authHeader = strings.TrimPrefix(authHeader, "Bearer ")
		}

		// Validate token against configured API keys using constant-time compare
		valid := false
		for _, key := range s.config.APIKeys {
			if subtle.ConstantTimeCompare([]byte(authHeader), []byte(key)) == 1 {
				valid = true
				break
			}
		}
		if !valid {
			s.writeError(w, http.StatusUnauthorized, "unauthorized: invalid API token")
			return
		}
	}

	allowedOrigins := s.config.WebSocketAllowedOrigins
	if len(allowedOrigins) == 0 {
		allowedOrigins = defaultWSOrigins
	}

	wsServer := &websocket.Server{
		Handler: websocket.Handler(func(conn *websocket.Conn) {
			s.wsHub.Register(conn)
			welcome := WSMessage{Type: "status", Data: []byte(`{"connected":true}`)}
			_ = websocket.JSON.Send(conn, welcome)
			defer s.wsHub.Unregister(conn)

			for {
				var msg WSMessage
				if err := websocket.JSON.Receive(conn, &msg); err != nil {
					return
				}
				s.handleWSMessage(conn, &msg)
			}
		}),
		Handshake: func(config *websocket.Config, request *http.Request) error {
			if !isLocalOrigin(request.Header.Get("Origin")) {
				return fmt.Errorf("origin not allowed: %s", request.Header.Get("Origin"))
			}
			return nil
		},
	}

	wsServer.ServeHTTP(w, r)
}

// WSMessage represents a message sent/received over WebSocket.
type WSMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// handleWSMessage processes incoming WebSocket messages.
func (s *Server) handleWSMessage(conn *websocket.Conn, msg *WSMessage) {
	switch msg.Type {
	case "ping":
		_ = websocket.JSON.Send(conn, WSMessage{Type: "pong"})
	case "subscribe":
		s.handleWSSubscribe(conn, msg)
	default:
		s.logger.Debug("ws unknown message type", "type", msg.Type)
	}
}

// handleWSSubscribe handles subscribe messages from WebSocket clients.
// Clients can subscribe to channels: chat, jobs, metrics.
func (s *Server) handleWSSubscribe(conn *websocket.Conn, msg *WSMessage) {
	if s.wsHub == nil {
		_ = websocket.JSON.Send(conn, WSMessage{Type: "error", Data: json.RawMessage(`{"message":"WebSocket not enabled"}`)})
		return
	}

	// Extract channel from msg.Data
	var channel string
	var sessionID string
	if msg.Data != nil {
		var parsed map[string]any
		if err := json.Unmarshal(msg.Data, &parsed); err == nil {
			if ch, ok := parsed["channel"].(string); ok {
				channel = ch
			}
			if sid, ok := parsed["session_id"].(string); ok {
				sessionID = sid
			}
		}
	}
	if channel == "" {
		// Default channel
		channel = "all"
	}

	subscribeData, _ := json.Marshal(map[string]string{"channel": channel})
	_ = websocket.JSON.Send(conn, WSMessage{
		Type: "subscribed",
		Data: subscribeData,
	})

	s.logger.Debug("ws client subscribed", "remote", conn.RemoteAddr(), "channel", channel, "session", sessionID)
}

// handleMCPPost handles POST /mcp - JSON-RPC requests over HTTP.
func (s *Server) handleMCPPost(w http.ResponseWriter, r *http.Request) {
	if s.mcpServices == nil {
		s.writeError(w, http.StatusServiceUnavailable, "MCP not enabled")
		return
	}

	// Verify Content-Type
	ct := r.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		s.writeError(w, http.StatusBadRequest, "Content-Type must be application/json")
		return
	}

	// Read body with limit
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
	if err != nil {
		s.logger.Error("MCP POST: read body", "error", err)
		s.writeError(w, http.StatusInternalServerError, "failed to read body")
		return
	}

	// Parse JSON-RPC request
	var req mcp.JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		s.logger.Error("MCP POST: parse JSON-RPC", "error", err)
		s.writeError(w, http.StatusBadRequest, "invalid JSON-RPC request")
		return
	}

	// Process JSON-RPC request
	resp := s.processMCPRequest(r.Context(), &req)

	// Notifications (e.g., notifications/initialized) return nil — respond with 204 No Content
	if resp == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("MCP POST: encode response", "error", err)
	}
}

// processMCPRequest routes and processes MCP JSON-RPC requests.
func (s *Server) processMCPRequest(ctx context.Context, req *mcp.JSONRPCRequest) *mcp.JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleMCPInitialize(req)
	case "notifications/initialized":
		return nil
	case "tools/list":
		return s.handleMCPToolsList(req)
	case "tools/call":
		return s.handleMCPToolsCall(req)
	default:
		return &mcp.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &mcp.JSONRPCError{
				Code:    -32601,
				Message: fmt.Sprintf("method not found: %s", req.Method),
			},
		}
	}
}

// handleMCPInitialize handles MCP initialize request.
func (s *Server) handleMCPInitialize(req *mcp.JSONRPCRequest) *mcp.JSONRPCResponse {
	result, _ := json.Marshal(map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"serverInfo": map[string]any{
			"name":    "meept",
			"version": "0.2.0",
		},
	})
	return &mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handleMCPToolsList handles MCP tools/list request.
func (s *Server) handleMCPToolsList(req *mcp.JSONRPCRequest) *mcp.JSONRPCResponse {
	tools := mcp.ToolDefinitions()
	result, _ := json.Marshal(map[string]any{
		"tools": tools,
	})
	return &mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handleMCPToolsCall handles MCP tools/call request.
func (s *Server) handleMCPToolsCall(req *mcp.JSONRPCRequest) *mcp.JSONRPCResponse {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return &mcp.JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &mcp.JSONRPCError{Code: -32602, Message: "invalid params"},
			}
		}
	}

	// Validate tool name
	switch params.Name {
	case "meept_sessions", "meept_send", "meept_events", "meept_status", "meept_session_history":
		// known tools
	default:
		if params.Name == "" {
			return &mcp.JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &mcp.JSONRPCError{Code: -32602, Message: "missing tool name"},
			}
		}
		return &mcp.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &mcp.JSONRPCError{Code: -32601, Message: fmt.Sprintf("unknown tool: %s", params.Name)},
		}
	}

	if s.mcpServices == nil {
		return &mcp.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &mcp.JSONRPCError{Code: -32000, Message: "services not available"},
		}
	}

	var result any
	var err error

	switch params.Name {
	case "meept_sessions":
		result, err = s.mcpToolSessions(params.Arguments)
	case "meept_send":
		result, err = s.mcpToolSend(params.Arguments)
	case "meept_events":
		result, err = s.mcpToolEvents(params.Arguments)
	case "meept_status":
		result, err = s.mcpToolStatus(params.Arguments)
	case "meept_session_history":
		result, err = s.mcpToolSessionHistory(params.Arguments)
	}

	if err != nil {
		return &mcp.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mustMarshalMCP(map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("error: %v", err)}}}),
		}
	}

	var text string
	switch v := result.(type) {
	case string:
		text = v
	case json.RawMessage:
		text = string(v)
	default:
		data, err := json.Marshal(result)
		if err != nil {
			text = fmt.Sprintf("error marshaling result: %v", err)
		} else {
			text = string(data)
		}
	}

	return &mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  mustMarshalMCP(map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}),
	}
}

// handleMCPSSE handles GET /mcp/sse - Server-Sent Events for async MCP notifications.
func (s *Server) handleMCPSSE(w http.ResponseWriter, r *http.Request) {
	if s.mcpServices == nil || s.mcpServices.Bus == nil {
		s.writeError(w, http.StatusServiceUnavailable, "MCP or bus service not enabled")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, "SSE not supported")
		return
	}

	// Create session for this client
	session := &MCPSession{
		sessionID: fmt.Sprintf("http-%d", time.Now().UnixNano()),
		eventChan: make(chan *SSEEvent, 100),
		done:      make(chan struct{}),
	}
	s.mcpSessions.Store(session.sessionID, session)
	defer s.mcpSessions.Delete(session.sessionID)
	defer close(session.done)

	// Subscribe to bus events for MCP clients
	// Topics: chat messages, agent events, worker events
	sub, cleanup := s.mcpServices.Bus.Subscribe(session.sessionID, "*")
	defer cleanup()

	// Forward bus events to SSE channel and buffer for polling
	go func() {
		defer close(session.eventChan)
		for msg := range sub.Channel {
			event := &SSEEvent{
				ID:   fmt.Sprintf("%d", time.Now().UnixNano()),
				Type: msg.Topic,
				Data: msg.Payload,
			}
			// Buffer for meept_events polling
			var payload any
			if len(msg.Payload) > 0 {
				_ = json.Unmarshal(msg.Payload, &payload)
			}
			session.mu.Lock()
			session.events = append(session.events, mcpEventRecord{
				Topic:     msg.Topic,
				Type:      string(msg.Type),
				Source:    msg.Source,
				Timestamp: msg.Timestamp,
				Payload:   payload,
			})
			// Cap buffer at 200 events
			if len(session.events) > 200 {
				session.events = session.events[len(session.events)-200:]
			}
			session.mu.Unlock()

			select {
			case session.eventChan <- event:
			case <-session.done:
				return
			}
		}
	}()

	// Send initial session id
	sessionData, _ := json.Marshal(map[string]string{"session_id": session.sessionID})
	fmt.Fprintf(w, "event: session\n")
	fmt.Fprintf(w, "data: %s\n", sessionData)
	fmt.Fprintf(w, "\n")
	flusher.Flush()

	// Stream events until client disconnects
	for {
		select {
		case <-r.Context().Done():
			return
		case <-session.done:
			return
		case event, ok := <-session.eventChan:
			if !ok {
				return
			}
			fmt.Fprintf(w, "id: %s\n", event.ID)
			fmt.Fprintf(w, "event: %s\n", event.Type)
			fmt.Fprintf(w, "data: %s\n", string(event.Data))
			fmt.Fprintf(w, "\n")
			flusher.Flush()
		}
	}
}

// mustMarshalMCP marshals a value to json.RawMessage for MCP responses.
func mustMarshalMCP(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

// mcpToolSessions handles MCP meept_sessions tool.
func (s *Server) mcpToolSessions(args map[string]any) (any, error) {
	if s.mcpServices.SessionStore == nil {
		return nil, fmt.Errorf("session store not available")
	}

	action, _ := args["action"].(string)
	switch action {
	case "list":
		sessions, err := s.mcpServices.SessionStore.List()
		if err != nil {
			return nil, err
		}
		return sessions, nil
	case "create":
		name, _ := args["name"].(string)
		if name == "" {
			name = "mcp-session"
		}
		sess, err := s.mcpServices.SessionStore.Create(name)
		if err != nil {
			return nil, err
		}
		return sess, nil
	case "attach":
		sessionID, _ := args["session_id"].(string)
		sess := s.mcpServices.SessionStore.Get(sessionID)
		if sess == nil {
			return nil, fmt.Errorf("session not found: %s", sessionID)
		}
		messages, _ := s.mcpServices.SessionStore.GetMessages(sessionID, 0, 50)
		return map[string]any{
			"status":     "attached",
			"session_id": sessionID,
			"history":    messages,
		}, nil
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// mcpToolSend handles MCP meept_send tool.
func (s *Server) mcpToolSend(args map[string]any) (any, error) {
	sessionID, _ := args["session_id"].(string)
	message, _ := args["message"].(string)
	if sessionID == "" || message == "" {
		return nil, fmt.Errorf("session_id and message are required")
	}

	if s.mcpServices.Bus == nil {
		return nil, fmt.Errorf("bus service not available")
	}

	err := s.mcpServices.Bus.Publish(context.Background(), services.PublishRequest{
		Topic:  "chat.request",
		Type:   "request",
		Source: "mcp-http",
		Payload: map[string]any{
			"message":         message,
			"conversation_id": sessionID,
			"source_client":   "mcp-http",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to publish message: %w", err)
	}

	return map[string]any{
		"response": fmt.Sprintf("Message queued for session %s", sessionID),
	}, nil
}

// mcpToolEvents handles MCP meept_events tool.
func (s *Server) mcpToolEvents(args map[string]any) (any, error) {
	subID, _ := args["subscription_id"].(string)
	if subID == "" {
		return nil, fmt.Errorf("subscription_id is required")
	}

	sessVal, ok := s.mcpSessions.Load(subID)
	if !ok {
		return nil, fmt.Errorf("subscription not found: %s", subID)
	}
	sess := sessVal.(*MCPSession)

	since, _ := args["since"].(string)
	sess.mu.RLock()
	defer sess.mu.RUnlock()

	var events []mcpEventRecord
	if since != "" {
		sinceTime, err := time.Parse(time.RFC3339Nano, since)
		if err != nil {
			// Try RFC3339 as fallback
			sinceTime, _ = time.Parse(time.RFC3339, since)
		}
		if !sinceTime.IsZero() {
			for _, e := range sess.events {
				if e.Timestamp.After(sinceTime) {
					events = append(events, e)
				}
			}
		} else {
			events = sess.events
		}
	} else {
		events = sess.events
	}

	if events == nil {
		events = []mcpEventRecord{}
	}
	return map[string]any{"events": events}, nil
}

// mcpToolStatus handles MCP meept_status tool.
func (s *Server) mcpToolStatus(args map[string]any) (any, error) {
	if s.mcpServices.Daemon == nil {
		return nil, fmt.Errorf("daemon service not available")
	}
	return s.mcpServices.Daemon.Status(context.Background())
}

// mcpToolSessionHistory handles MCP meept_session_history tool.
func (s *Server) mcpToolSessionHistory(args map[string]any) (any, error) {
	if s.mcpServices.SessionStore == nil {
		return nil, fmt.Errorf("session store not available")
	}

	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	limit := 50
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	return s.mcpServices.SessionStore.GetMessages(sessionID, 0, limit)
}

// handleRuntimeStatus handles GET /api/v1/runtime/status.
func (s *Server) handleRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Runtime == nil {
		s.writeError(w, http.StatusServiceUnavailable, "runtime service not available")
		return
	}
	resp, err := s.services.Runtime.Status(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// handleRuntimeStatusProvider handles GET /api/v1/runtime/status/{provider}.
func (s *Server) handleRuntimeStatusProvider(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Runtime == nil {
		s.writeError(w, http.StatusServiceUnavailable, "runtime service not available")
		return
	}
	provider := r.PathValue("provider")
	if provider == "" {
		provider = "local"
	}
	resp, err := s.services.Runtime.StatusForProvider(r.Context(), provider)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// handleRuntimeStart handles POST /api/v1/runtime/start/{provider}.
func (s *Server) handleRuntimeStart(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Runtime == nil {
		s.writeError(w, http.StatusServiceUnavailable, "runtime service not available")
		return
	}
	provider := r.PathValue("provider")
	if provider == "" {
		provider = "local"
	}
	if err := s.services.Runtime.StartProvider(r.Context(), provider); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

// handleRuntimeStop handles POST /api/v1/runtime/stop/{provider}.
func (s *Server) handleRuntimeStop(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Runtime == nil {
		s.writeError(w, http.StatusServiceUnavailable, "runtime service not available")
		return
	}
	provider := r.PathValue("provider")
	if provider == "" {
		provider = "local"
	}
	if err := s.services.Runtime.StopProvider(r.Context(), provider); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

// handleRuntimeRestart handles POST /api/v1/runtime/restart/{provider}.
func (s *Server) handleRuntimeRestart(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Runtime == nil {
		s.writeError(w, http.StatusServiceUnavailable, "runtime service not available")
		return
	}
	provider := r.PathValue("provider")
	if provider == "" {
		provider = "local"
	}
	if err := s.services.Runtime.RestartProvider(r.Context(), provider); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "restarted"})
}
