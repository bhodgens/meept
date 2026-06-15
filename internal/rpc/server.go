package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/errcls"
	"github.com/caimlas/meept/pkg/models"
)

// connectionDoneKey is the context value key for the connection's done channel.
// Handlers (e.g., bus.subscribe) use this to derive long-lived contexts that are
// cancelled when the client disconnects, preventing subscription leaks (Bug C8).
type connectionDoneKey struct{}

// Default timeouts.
const (
	DefaultShutdownTimeout = 30 * time.Second // Graceful shutdown deadline
	readIdleTimeout        = 5 * time.Minute  // Max time waiting for a request
	writeTimeout           = 5 * time.Minute  // Max time to write response (FIX #0056: was 30s, increased for long-running ops like selfimprove.cycle)
	operationTimeout       = 10 * time.Minute // Max time for a single RPC operation
)

// Handler is a function that handles an RPC method.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// Server implements a JSON-RPC 2.0 server over Unix sockets.
type Server struct {
	socketPath   string
	listener     net.Listener
	bus          *bus.MessageBus
	logger       *slog.Logger
	startTime    time.Time
	defaultModel string // Configured default model for status reporting
	shutdown     time.Duration

	mu       sync.RWMutex
	handlers map[string]Handler
	running  atomic.Bool

	// FirewallStatsGetter is an optional callback that returns firewall stats.
	// When set, stats are included in the status response.
	FirewallStatsGetter func() map[string]any

	// BudgetStatusGetter is an optional callback that returns budget status.
	// Used by the status handler to report actual token and cost usage (FIX #0031/#0035).
	BudgetStatusGetter func() (hourlyUsed int, hourlyRemaining int, dailyUsed int, dailyRemaining int, rpmCurrent int, rpmLimit int, dailyCostUsed float64, dailyCostLimit float64, hourlyCostUsed float64, hourlyCostLimit float64, perTaskCost float64, perSessionCost float64, perTaskBudget int, perSessionBudget int)

	// Connection tracking
	connMu   sync.Mutex
	conns    map[net.Conn]struct{}
	connWg   sync.WaitGroup
	closeCh  chan struct{}
	stopOnce sync.Once

	// Active request tracking for graceful shutdown
	activeReqs atomic.Int64

	// Per-request handlers
	requestHandlers []func()
}

// Config holds server configuration.
type Config struct {
	SocketPath     string
	Shutdown       time.Duration // Graceful shutdown deadline (default 30s)
	ShutdownNotify func()        // Called when a request completes during shutdown
}

// New creates a new RPC server.
func New(cfg *Config, msgBus *bus.MessageBus, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	shutdown := cfg.Shutdown
	if shutdown <= 0 {
		shutdown = DefaultShutdownTimeout
	}
	s := &Server{
		socketPath:      cfg.SocketPath,
		bus:             msgBus,
		logger:          logger,
		startTime:       time.Now(),
		shutdown:        shutdown,
		handlers:        make(map[string]Handler),
		conns:           make(map[net.Conn]struct{}),
		closeCh:         make(chan struct{}),
		requestHandlers: make([]func(), 0),
	}
	if cfg.ShutdownNotify != nil {
		s.requestHandlers = append(s.requestHandlers, cfg.ShutdownNotify)
	}
	return s
}

// Name implements registry.Component.
func (s *Server) Name() string {
	return "rpc.server"
}

// SetDefaultModel sets the default model name for status reporting.
func (s *Server) SetDefaultModel(model string) {
	s.defaultModel = model
}

// Running implements registry.Component.
func (s *Server) Running() bool {
	return s.running.Load()
}

// RegisterHandler registers a method handler.
func (s *Server) RegisterHandler(method string, handler Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = handler
	s.logger.Debug("rpc: registered handler", "method", method)
}

// CallMethod dispatches a method call through the handler registry.
// This is the exported entry point for HTTP-to-RPC bridging.
func (s *Server) CallMethod(ctx context.Context, method string, params json.RawMessage) (any, error) {
	s.mu.RLock()
	handler, ok := s.handlers[method]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("method not found: %s", method)
	}

	return handler(ctx, params)
}

// Start starts the RPC server.
func (s *Server) Start(ctx context.Context) error {
	// Remove existing socket
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	s.listener = listener

	// Set socket permissions
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		s.logger.Warn("rpc: failed to set socket permissions", "error", err)
	}

	s.running.Store(true)
	s.logger.Info("rpc: server started", "socket", s.socketPath)

	// Register built-in handlers
	s.registerBuiltinHandlers()

	// Accept connections
	go s.acceptLoop()

	return nil
}

// Stop stops the RPC server.
func (s *Server) Stop(ctx context.Context) error {
	if !s.running.Load() {
		return nil
	}

	s.running.Store(false)

	// CORE-2 FIX: Use sync.Once to prevent double-close panic
	s.stopOnce.Do(func() {
		close(s.closeCh)
	})

	// Close listener
	if s.listener != nil {
		s.listener.Close()
	}

	// Close all connections
	s.connMu.Lock()
	for conn := range s.conns {
		conn.Close()
	}
	s.connMu.Unlock()

	// Wait for active requests to drain with configurable timeout
	done := make(chan struct{})
	go func() {
		s.connWg.Wait()
		close(done)
	}()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdown)
	defer cancel()

	select {
	case <-done:
		// All requests drained
	case <-shutdownCtx.Done():
		n := s.activeReqs.Load()
		if n > 0 {
			s.logger.Warn("rpc: shutdown timed out with active requests",
				"active", n,
				"timeout", s.shutdown,
			)
		}
	case <-ctx.Done():
		s.logger.Warn("rpc: shutdown cancelled")
	}

	// Remove socket
	os.Remove(s.socketPath)
	s.logger.Info("rpc: server stopped")
	return nil
}

func (s *Server) acceptLoop() {
	for s.running.Load() {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.running.Load() {
				s.logger.Error("rpc: accept failed", "error", err)
			}
			continue
		}

		s.connMu.Lock()
		s.conns[conn] = struct{}{}
		s.connMu.Unlock()

		s.connWg.Add(1)
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	// Create connection-scoped context that cancels when we return
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel() // Signal all handlers to stop
		conn.Close()
		s.connMu.Lock()
		delete(s.conns, conn)
		s.connMu.Unlock()
		s.connWg.Done()
	}()

	reader := NewFrameReader(conn)
	writer := NewFrameWriter(conn)

	for s.running.Load() {
		// Set read deadline to detect client disconnects
		if err := conn.SetReadDeadline(time.Now().Add(readIdleTimeout)); err != nil {
			s.logger.Debug("rpc: failed to set read deadline", "error", err)
			return
		}

		req, err := reader.ReadRequest()
		if err != nil {
			if s.running.Load() {
				// Don't log timeout as error - it's expected idle behavior
				var netErr net.Error
				if errors.As(err, &netErr) {
					s.logger.Debug("rpc: client idle timeout, closing connection")
				} else {
					s.logger.Debug("rpc: read error", "error", err)
				}
			}
			return
		}

		// Clear read deadline during processing
		_ = conn.SetReadDeadline(time.Time{})

		// Process request with connection-scoped context
		resp := s.dispatch(ctx, cancel, req)

		// Check if context was cancelled (client disconnected) before writing
		if ctx.Err() != nil {
			s.logger.Debug("rpc: client disconnected during request processing",
				"method", req.Method,
				"id", req.ID)
			return
		}

		// Set write deadline before attempting to write response
		if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
			s.logger.Debug("rpc: failed to set write deadline", "error", err)
			return
		}

		// Write response
		if err := writer.WriteResponse(resp); err != nil {
			// Log at debug level for broken pipe - it's expected when client disconnects
			if s.running.Load() {
				s.logger.Debug("rpc: write error (client may have disconnected)",
					"error", err,
					"method", req.Method)
			}
			return
		}

		// Clear write deadline
		_ = conn.SetWriteDeadline(time.Time{})
	}
}

func (s *Server) dispatch(connCtx context.Context, connCancel context.CancelFunc, req *models.JSONRPCRequest) *models.JSONRPCResponse {
	s.mu.RLock()
	handler, ok := s.handlers[req.Method]
	s.mu.RUnlock()

	if !ok {
		return MakeErrorResponse(
			req.ID,
			models.ErrCodeMethodNotFound,
			fmt.Sprintf("method not found: %s", req.Method),
			nil,
		)
	}

	s.activeReqs.Add(1)
	defer func() {
		s.activeReqs.Add(-1)
		for _, fn := range s.requestHandlers {
			fn()
		}
	}()

	// Create a timeout context for this specific operation
	// This ensures handlers don't run forever even if connection stays open
	opCtx, cancel := context.WithTimeout(connCtx, operationTimeout)
	defer cancel()

	// Inject the connection-scoped done channel into context (Bug C8 so proxy
	// can derive long-lived subscription contexts that are cancelled on disconnect).
	opCtx = context.WithValue(opCtx, connectionDoneKey{}, connCtx.Done())

	result, err := handler(opCtx, req.Params)
	if err != nil {
		// Check if the error is due to context cancellation
		if connCtx.Err() != nil {
			return MakeErrorResponse(
				req.ID,
				models.ErrCodeInternal,
				"request cancelled: client disconnected",
				nil,
			)
		}
		// D18: Use appropriate JSON-RPC 2.0 error codes
		// -32602 (InvalidParams) for parameter/validation errors
		// -32603 (Internal) for other handler errors
		errStr := err.Error()
		code := models.ErrCodeInternal
		if isParameterError(err) {
			code = models.ErrCodeInvalidParams
		}
		return MakeErrorResponse(
			req.ID,
			code,
			errStr,
			nil,
		)
	}

	return MakeResponse(req.ID, result)
}

func (s *Server) registerBuiltinHandlers() {
	// Ping/pong for health checks
	s.RegisterHandler("ping", func(ctx context.Context, params json.RawMessage) (any, error) {
		return "pong", nil
	})

	// Get daemon status (both names for compatibility)
	statusHandler := func(ctx context.Context, params json.RawMessage) (any, error) {
		// Get bus stats for additional info
		busStats := s.bus.Stats()

		// Count registered handlers
		s.mu.RLock()
		methods := make([]string, 0, len(s.handlers))
		for method := range s.handlers {
			methods = append(methods, method)
		}
		s.mu.RUnlock()

		result := map[string]any{
			RPCKeyStatus:         "running",
			"version":            "0.2.0-go",
			"uptime_seconds":     time.Since(s.startTime).Seconds(),
			RPCKeyModel:          s.defaultModel,
			"default_model":      s.defaultModel,
			"tokens_used":        0,
			"tokens_remaining":   100000,
			"budget_used":        0.0,
			"budget_remaining":   10.0,
			"registered_methods": methods,
			"bus_subscribers":    busStats["_total"],
			"rpm_current":        0,
			"rpm_limit":          0,
			"hourly_used":        0,
			"hourly_remaining":   0,
			"daily_used":         0,
			"daily_remaining":    0,
		}

		// Include firewall stats if a getter is configured
		if s.FirewallStatsGetter != nil {
			if fwStats := s.FirewallStatsGetter(); fwStats != nil {
				result["firewall"] = fwStats
			}
		}

		// Include budget stats if a getter is configured (FIX #0031/#0035)
		if s.BudgetStatusGetter != nil {
			hu, hr, du, dr, rc, rl, dcu, dcl, hcu, hcl, ptc, psc, ptb, psb := s.BudgetStatusGetter()
			result["tokens_used"] = hu
			result["tokens_remaining"] = hr
			result["daily_used"] = du
			result["daily_remaining"] = dr
			result["rpm_current"] = rc
			result["rpm_limit"] = rl
			result["daily_cost_used"] = dcu
			result["daily_cost_limit"] = dcl
			result["daily_cost_remaining"] = max(dcl-dcu, 0)
			result["hourly_cost_used"] = hcu
			result["hourly_cost_limit"] = hcl
			result["hourly_cost_remaining"] = max(hcl-hcu, 0)
			result["budget_used"] = dcu
			result["budget_remaining"] = max(dcl-dcu, 0)
			result["within_cost_budget"] = dcu < dcl && hcu < hcl
			result["per_task_cost"] = ptc
			result["per_session_cost"] = psc
			result["per_task_budget"] = ptb
			result["per_session_budget"] = psb
		}

		return result, nil
	}
	s.RegisterHandler("status", statusHandler)
	s.RegisterHandler("daemon.status", statusHandler)

	// Get firewall stats (standalone endpoint)
	s.RegisterHandler("firewall.stats", func(ctx context.Context, params json.RawMessage) (any, error) {
		if s.FirewallStatsGetter == nil {
			return map[string]any{}, nil
		}
		return s.FirewallStatsGetter(), nil
	})

	// Bus publish
	s.RegisterHandler("bus.publish", func(ctx context.Context, params json.RawMessage) (any, error) {
		var p struct {
			Topic   string          `json:"topic"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}

		msg := &models.BusMessage{
			ID:      fmt.Sprintf("rpc-%d", atomicCounter()),
			Type:    models.MessageTypeEvent,
			Topic:   p.Topic,
			Source:  "rpc.client",
			Payload: p.Payload,
		}
		delivered := s.bus.Publish(p.Topic, msg)
		return map[string]int{"delivered": delivered}, nil
	})

	// Bus stats
	s.RegisterHandler("bus.stats", func(ctx context.Context, params json.RawMessage) (any, error) {
		return s.bus.Stats(), nil
	})

	// Task amendment submission - publishes to task.amend.request bus topic
	s.RegisterHandler("task.amend.submit", func(ctx context.Context, params json.RawMessage) (any, error) {
		var req struct {
			TaskID  string `json:"task_id"`
			Type    string `json:"type"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid amendment request: %w", err)
		}
		if req.TaskID == "" || req.Type == "" {
			return nil, fmt.Errorf("task_id and type are required")
		}

		amendmentID := fmt.Sprintf("amend-%d", atomicCounter())

		// Publish the amendment request on the bus for the orchestrator to handle
		payload, err := json.Marshal(map[string]any{
			"id":          amendmentID,
			"task_id":     req.TaskID,
			"type":        req.Type,
			RPCKeyContent: req.Content,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal amendment payload: %w", err)
		}

		msg := &models.BusMessage{
			ID:      fmt.Sprintf("rpc-%d", atomicCounter()),
			Type:    models.MessageTypeRequest,
			Topic:   "task.amend.request",
			Source:  "rpc.client",
			Payload: payload,
		}
		s.bus.Publish("task.amend.request", msg)

		return map[string]string{
			"id":          amendmentID,
			RPCKeyStatus:  "submitted",
			RPCKeyMessage: fmt.Sprintf("amendment %s submitted for task %s", req.Type, req.TaskID),
		}, nil
	})
}

var counter atomic.Int64

func atomicCounter() int64 {
	return counter.Add(1)
}

// isParameterError returns true for parameter-validation errors that should
// map to JSON-RPC -32602 InvalidParams. Uses structured detection via
// errcls.IsParameterError (errors.Is / errors.As) instead of the old
// substring heuristic which false-positive'd on common words like "type",
// "expected", "parse", etc.
func isParameterError(err error) bool {
	return errcls.IsParameterError(err)
}

