package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// Handler is a function that handles an RPC method.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// Server implements a JSON-RPC 2.0 server over Unix sockets.
type Server struct {
	socketPath string
	listener   net.Listener
	bus        *bus.MessageBus
	logger     *slog.Logger

	mu       sync.RWMutex
	handlers map[string]Handler
	running  atomic.Bool

	// Connection tracking
	connMu  sync.Mutex
	conns   map[net.Conn]struct{}
	connWg  sync.WaitGroup
	closeCh chan struct{}
}

// Config holds server configuration.
type Config struct {
	SocketPath string
}

// New creates a new RPC server.
func New(cfg *Config, bus *bus.MessageBus, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		socketPath: cfg.SocketPath,
		bus:        bus,
		logger:     logger,
		handlers:   make(map[string]Handler),
		conns:      make(map[net.Conn]struct{}),
		closeCh:    make(chan struct{}),
	}
}

// Name implements registry.Component.
func (s *Server) Name() string {
	return "rpc.server"
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
	if err := os.Chmod(s.socketPath, 0600); err != nil {
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
	close(s.closeCh)

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

	// Wait for connections to finish
	done := make(chan struct{})
	go func() {
		s.connWg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		s.logger.Warn("rpc: shutdown timed out")
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
	defer func() {
		conn.Close()
		s.connMu.Lock()
		delete(s.conns, conn)
		s.connMu.Unlock()
		s.connWg.Done()
	}()

	reader := NewFrameReader(conn)
	writer := NewFrameWriter(conn)

	for s.running.Load() {
		req, err := reader.ReadRequest()
		if err != nil {
			if s.running.Load() {
				s.logger.Debug("rpc: read error", "error", err)
			}
			return
		}

		// Process request
		resp := s.dispatch(req)

		// Write response
		if err := writer.WriteResponse(resp); err != nil {
			s.logger.Error("rpc: write error", "error", err)
			return
		}
	}
}

func (s *Server) dispatch(req *models.JSONRPCRequest) *models.JSONRPCResponse {
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

	ctx := context.Background()
	result, err := handler(ctx, req.Params)
	if err != nil {
		return MakeErrorResponse(
			req.ID,
			models.ErrCodeInternal,
			err.Error(),
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

	// Get daemon status
	s.RegisterHandler("daemon.status", func(ctx context.Context, params json.RawMessage) (any, error) {
		return map[string]any{
			"status":  "running",
			"version": "0.2.0-go",
		}, nil
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
}

var counter atomic.Int64

func atomicCounter() int64 {
	return counter.Add(1)
}
