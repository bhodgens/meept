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
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/process"
	"github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
)

// connectionDoneKey is the context value key for the connection's done channel.
// Handlers (e.g., bus.subscribe) use this to derive long-lived contexts that are
// cancelled when the client disconnects, preventing subscription leaks (Bug C8).
type connectionDoneKey struct{}

// PeerIdentity is the kernel-verified identity of a Unix-socket RPC peer
// (audit M5). PID/UID come from SO_PEERCRED (Linux) / LOCAL_PEERPID +
// LOCAL_PEERCRED (macOS) and cannot be forged by the connecting process.
// HasPID/HasUID are false on platforms without peer-credential support,
// where identity-dependent gates must fail closed.
type PeerIdentity struct {
	PID    int
	UID    int
	HasPID bool
	HasUID bool
	// SpawnedByDaemon is true when the peer PID is the daemon itself or a
	// descendant of it (walked ONCE at accept time, while the peer is
	// provably alive — a resolve-time walk would race PID reuse). Agent-
	// controlled processes (shell-tool children, PTY sessions, MCP stdio
	// servers) are daemon descendants; human surfaces (TUI, menubar, GUI)
	// are not. Used by the ACP approver gate (audit M5).
	SpawnedByDaemon bool
}

// peerIdentityKey is the context value key for the connection's PeerIdentity.
// Handlers that need caller identity (ACP approver gate) read it from the
// per-connection context built in handleConnection.
type peerIdentityKey struct{}

// PeerIdentityFromContext extracts the socket peer identity attached to a
// connection-scoped context. ok is false when the context carries none
// (in-process CallMethod paths — HTTP dispatch — attach no peer; they
// identify themselves separately via acp.WithApprover).
func PeerIdentityFromContext(ctx context.Context) (PeerIdentity, bool) {
	if ctx == nil {
		return PeerIdentity{}, false
	}
	p, ok := ctx.Value(peerIdentityKey{}).(PeerIdentity)
	return p, ok
}

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

	// BudgetStatusSnapshotGetter is the preferred budget callback: it
	// returns the authoritative snapshot (scalars + WithinBudget /
	// WithinCostBudget) straight from Budget.GetStatus(). When set, the
	// status handler passes its booleans through untouched and
	// BudgetStatusGetter is ignored. Wire handlers must NEVER recompute
	// the budget booleans from raw counters — a `used < limit` recompute
	// reads false when a limit is disabled (0) and reports an unlimited
	// daemon as over budget (budget-status-consistency Contract 1).
	BudgetStatusSnapshotGetter func() llm.BudgetStatusSnapshot

	// TokenLimitGetter is an optional callback returning the configured
	// hourly and daily TOKEN limits (0 = disabled/unlimited). When set, the
	// limits are reported in the status response as "hourly_token_limit" and
	// "daily_token_limit" so clients never have to infer a cap from
	// used+remaining (which is 0 remaining while a disabled budget keeps
	// counting usage, the source of the bogus 100% display).
	TokenLimitGetter func() (hourlyLimit int, dailyLimit int)

	// SecretsProxyStatusGetter is an optional callback returning the egress
	// secrets-proxy status map {"enabled", "addr", "leak_attempts"}; nil or
	// nil-result means the proxy is disabled/unwired and the "secrets_proxy"
	// key is omitted from status responses.
	SecretsProxyStatusGetter func() map[string]any

	// DispatcherStatsGetter is an optional callback returning the dispatcher's
	// per-classification-method dispatch counts (ByMethod map). When set, the
	// counts are included in the status response under
	// "by_classification_method" for regression tracking; nil result means
	// the key is omitted.
	DispatcherStatsGetter func() map[string]int

	// DeterministicToolsGetter is an optional callback disclosing whether
	// the deterministic (cached-fetch) tool gate is live (phase-2-3 P2.3).
	// When non-nil, the status response carries "deterministic_tools" so
	// meept-bench can preflight suites that require tools.cached_fetch.
	DeterministicToolsGetter func() bool

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

	// Peer-credential policy (leaf 06). allowedUIDs is a set keyed by UID;
	// nil or empty means no rejection (log-only mode). peerCredLog enables
	// the per-connection Debug log of the peer UID. Stored as an immutable
	// map so reads in acceptLoop need no synchronization.
	allowedUIDs map[int]bool
	peerCredLog bool
}

// Config holds server configuration.
type Config struct {
	SocketPath     string
	Shutdown       time.Duration // Graceful shutdown deadline (default 30s)
	ShutdownNotify func()        // Called when a request completes during shutdown

	// AllowedUIDs optionally restricts Unix-socket RPC connections to these
	// kernel-verified peer UIDs. Empty/nil preserves single-user behavior
	// (socket is still 0600); connections are logged but never rejected on
	// this basis.
	AllowedUIDs []int

	// PeerCredLog controls per-connection Debug logging of the kernel-verified
	// peer UID. Default true; irrelevant on platforms without peer-credential
	// support, where no lookup ever succeeds.
	PeerCredLog *bool
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

	// Peer-credential policy: default to log-only with logging enabled. The
	// *bool lets tests opt out of per-connection Debug logs without logging
	// infrastructure; a nil field means the default (true).
	peerCredLog := true
	if cfg.PeerCredLog != nil {
		peerCredLog = *cfg.PeerCredLog
	}
	s.peerCredLog = peerCredLog
	if len(cfg.AllowedUIDs) > 0 {
		uidSet := make(map[int]bool, len(cfg.AllowedUIDs))
		for _, uid := range cfg.AllowedUIDs {
			uidSet[uid] = true
		}
		s.allowedUIDs = uidSet
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
		s.logger.Warn("rpc: method not found", "method", method)
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

	// Set socket permissions. A failed chmod on some filesystems (NFS, a
	// strict-umask mount) silently leaves the socket group/other-readable,
	// exposing the full owner-trusted JSON-RPC registry to other local users.
	// Verify the resulting mode via stat and escalate to Error (not Warn) so
	// a weakened socket is visible in logs. The UID-allowlist at the accept
	// loop (allowedUIDs) is the enforcement; this is the detection layer.
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		s.logger.Error("rpc: failed to set socket permissions 0600; socket may be over-permissive",
			"socket", s.socketPath, "error", err)
	}
	if st, err := os.Stat(s.socketPath); err == nil {
		if perm := st.Mode().Perm() & 0o077; perm != 0 {
			s.logger.Error("rpc: socket has group/other access bits; not owner-only",
				"socket", s.socketPath, "perm", fmt.Sprintf("0%o", st.Mode().Perm()))
		}
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

	// Close all connections. Snapshot under lock, then close outside to
	// avoid holding connMu during network teardown (CLAUDE.md mutex scope).
	s.connMu.Lock()
	conns := make([]net.Conn, 0, len(s.conns))
	for conn := range s.conns {
		conns = append(conns, conn)
	}
	s.connMu.Unlock()
	for _, conn := range conns {
		conn.Close()
	}

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
			// If the listener is closed, the server is shutting down.
			if errors.Is(err, net.ErrClosed) {
				return
			}
			if s.running.Load() {
				s.logger.Error("rpc: accept failed", "error", err)
			}
			// Brief backoff to avoid a tight loop on transient errors.
			time.Sleep(50 * time.Millisecond)
			continue
		}

		// Kernel-enforced peer identity: extract the peer UID before any
		// state is registered for the connection so rejected peers leave no
		// trace behind a closed socket. The peer PID rides along for the
		// ACP approver gate (audit M5): handlers read it from the
		// connection context, never from the request payload.
		uid, uidOK := peerCredential(conn)
		pid, pidOK := peerProcessID(conn)
		// Walk the ancestry ONCE, at accept time, while the peer is alive
		// (audit M5). os.Getpid() is the daemon: every process an agent can
		// control was spawned by it, so descendant-of-daemon == agent-
		// controlled. Unverifiable PID (platform stub / lookup failure) is
		// NOT flagged here; identity-dependent gates fail closed on their
		// own when HasPID is false.
		spawnedByDaemon := pidOK && (pid == os.Getpid() || process.IsDescendantOf(pid, os.Getpid()))
		if uidOK {
			if s.peerCredLog {
				s.logger.Debug("rpc: connection accepted", "peer_uid", uid, "peer_pid", pid)
			}
			if len(s.allowedUIDs) > 0 && !s.allowedUIDs[uid] {
				s.logger.Warn("rpc: connection rejected by uid allowlist",
					"peer_uid", uid,
					"allowlist_size", len(s.allowedUIDs))
				conn.Close()
				continue
			}
		}

		s.connMu.Lock()
		s.conns[conn] = struct{}{}
		s.connMu.Unlock()

		s.connWg.Add(1)
		go s.handleConnection(conn, PeerIdentity{
			PID:             pid,
			UID:             uid,
			HasPID:          pidOK,
			HasUID:          uidOK,
			SpawnedByDaemon: spawnedByDaemon,
		})
	}
}

func (s *Server) handleConnection(conn net.Conn, peer PeerIdentity) {
	// Create connection-scoped context that cancels when we return
	ctx, cancel := context.WithCancel(context.Background())
	// Attach the kernel-verified peer identity so handlers can gate on
	// caller identity (ACP approver check, audit M5). Value-only context
	// key; handlers read it via PeerIdentityFromContext.
	ctx = context.WithValue(ctx, peerIdentityKey{}, peer)
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
				if _, isNet := errors.AsType[net.Error](err); isNet {
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
		s.logger.Warn("rpc: method not found", "method", req.Method)
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
		s.logger.Warn("rpc: handler error",
			"method", req.Method,
			"error_code", code,
			"error", err)
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
			"hourly_token_limit": 0,
			"daily_token_limit":  0,
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

		// Include egress secrets-proxy info if a getter is configured
		if s.SecretsProxyStatusGetter != nil {
			if sp := s.SecretsProxyStatusGetter(); sp != nil {
				result["secrets_proxy"] = sp
			}
		}

		// Include per-classification-method dispatch counts if a getter is
		// configured (classifier regression tracking).
		if s.DispatcherStatsGetter != nil {
			if byMethod := s.DispatcherStatsGetter(); byMethod != nil {
				result["by_classification_method"] = byMethod
			}
		}

		// Include the deterministic (cached-fetch) gate disclosure when a
		// getter is configured (meept-bench suite preflight, P2.3).
		if s.DeterministicToolsGetter != nil {
			result["deterministic_tools"] = s.DeterministicToolsGetter()
		}

		// Include budget stats if a getter is configured (FIX #0031/#0035).
		// The snapshot getter is the preferred pass-through of the
		// authoritative Budget.GetStatus(); the legacy 14-value tuple is
		// bridged through llm.LegacyTupleBudgetSnapshot so the booleans
		// keep the authoritative disabled-limits semantics (limit 0 =
		// unconstrained) without any recompute in this handler.
		if s.BudgetStatusSnapshotGetter != nil || s.BudgetStatusGetter != nil {
			var snap llm.BudgetStatusSnapshot
			if s.BudgetStatusSnapshotGetter != nil {
				snap = s.BudgetStatusSnapshotGetter()
			} else {
				hu, hr, du, dr, rc, rl, dcu, dcl, hcu, hcl, ptc, psc, ptb, psb := s.BudgetStatusGetter()
				var hl, dl int
				if s.TokenLimitGetter != nil {
					hl, dl = s.TokenLimitGetter()
				}
				snap = llm.LegacyTupleBudgetSnapshot(hu, hr, du, dr, rc, rl, dcu, dcl, hcu, hcl, ptc, psc, ptb, psb, hl, dl)
			}
			result["tokens_used"] = snap.HourlyUsed
			result["tokens_remaining"] = snap.HourlyRemaining
			result["daily_used"] = snap.DailyUsed
			result["daily_remaining"] = snap.DailyRemaining
			result["rpm_current"] = snap.RPMCurrent
			result["rpm_limit"] = snap.RPMLimit
			result["daily_cost_used"] = snap.DailyCostUsed
			result["daily_cost_limit"] = snap.DailyCostLimit
			result["daily_cost_remaining"] = max(snap.DailyCostLimit-snap.DailyCostUsed, 0)
			result["hourly_cost_used"] = snap.HourlyCostUsed
			result["hourly_cost_limit"] = snap.HourlyCostLimit
			result["hourly_cost_remaining"] = max(snap.HourlyCostLimit-snap.HourlyCostUsed, 0)
			result["budget_used"] = snap.DailyCostUsed
			result["budget_remaining"] = max(snap.DailyCostLimit-snap.DailyCostUsed, 0)
			result["within_cost_budget"] = snap.WithinCostBudget
			result["within_budget"] = snap.WithinBudget
			result["per_task_cost"] = snap.PerTaskCost
			result["per_session_cost"] = snap.PerSessionCost
			result["per_task_budget"] = snap.PerTaskBudget
			result["per_session_budget"] = snap.PerSessionBudget
		}

		// Include the configured token limits when a getter is wired so
		// clients print usage against the LIMIT instead of inferring a total
		// from used+remaining (which reads 0 remaining, and therefore a bogus
		// 100%, whenever the limit is disabled).
		if s.TokenLimitGetter != nil {
			hl, dl := s.TokenLimitGetter()
			result["hourly_token_limit"] = hl
			result["daily_token_limit"] = dl
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
			ID:      id.Generate("rpc-bus-"),
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

		amendmentID := id.Generate("rpc-amend-")

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
			ID:      id.Generate("rpc-amend-"),
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

// isParameterError returns true for parameter-validation errors that should
// map to JSON-RPC -32602 InvalidParams. Uses structured detection via
// errcls.IsParameterError (errors.Is / errors.As) instead of the old
// substring heuristic which false-positive'd on common words like "type",
// "expected", "parse", etc.
func isParameterError(err error) bool {
	return errcls.IsParameterError(err)
}
