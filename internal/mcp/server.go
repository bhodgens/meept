package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/caimlas/meept/internal/transport"
)

// Server implements an MCP server that talks to meept-daemon via RPC.
type Server struct {
	input   io.Reader
	bufRead *BufferedReader
	output  io.Writer
	client  transport.Client
	logger  *slog.Logger
}

// Version is the MCP implementation version advertised by both the server
// (internal/mcp) and client (internal/tools/mcp). Keep these in sync.
const Version = "0.2.0"

// NewServer creates a new MCP server reading from input and writing to output.
// client may be nil for testing (tools will return errors).
// A BufferedReader is created to persist the bufio.Reader across message loops.
func NewServer(input io.Reader, output io.Writer, client transport.Client) *Server {
	br := NewBufferedReader(input)
	return &Server{
		input:   input,
		bufRead: br,
		output:  output,
		client:  client,
		logger:  slog.Default().With("component", "mcp-server"),
	}
}

// Run starts the MCP server message loop. Blocks until EOF or error.
func (s *Server) Run() error {
	for {
		if err := s.processOne(); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			s.logger.Error("message processing error", "error", err)
			return err
		}
	}
}

// processOne reads and handles a single JSON-RPC message.
func (s *Server) processOne() error {
	req, err := ReadMessageBuffered(s.bufRead)
	if err != nil {
		return err
	}

	var resp *JSONRPCResponse
	switch req.Method {
	case "initialize":
		resp = s.handleInitialize(req)
	case "notifications/initialized":
		// No response needed for notifications
		return nil
	case "tools/list":
		resp = s.handleToolsList(req)
	case "tools/call":
		resp = s.handleToolsCall(req)
	default:
		resp = &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32601,
				Message: fmt.Sprintf("method not found: %s", req.Method),
			},
		}
	}

	if resp != nil {
		return WriteMessage(s.output, resp)
	}
	return nil
}

func (s *Server) handleInitialize(req *JSONRPCRequest) *JSONRPCResponse {
	result, _ := json.Marshal(map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "meept",
			"version": Version,
		},
	})
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func (s *Server) handleToolsList(req *JSONRPCRequest) *JSONRPCResponse {
	tools := ToolDefinitions()
	result, _ := json.Marshal(map[string]any{
		"tools": tools,
	})
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func (s *Server) handleToolsCall(req *JSONRPCRequest) *JSONRPCResponse {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &JSONRPCError{Code: -32602, Message: "invalid params"},
			}
		}
	}

	// Validate tool name before checking client connection
	switch params.Name {
	case "meept_sessions", "meept_send", "meept_events", "meept_status", "meept_session_history":
		// known tools
	default:
		if params.Name == "" {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &JSONRPCError{Code: -32602, Message: "missing tool name"},
			}
		}
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: -32601, Message: fmt.Sprintf("unknown tool: %s", params.Name)},
		}
	}

	if s.client == nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: -32000, Message: "not connected to daemon"},
		}
	}

	var result any
	var err error

	switch params.Name {
	case "meept_sessions":
		result, err = s.toolSessions(params.Arguments)
	case "meept_send":
		result, err = s.toolSend(params.Arguments)
	case "meept_events":
		result, err = s.toolEvents(params.Arguments)
	case "meept_status":
		result, err = s.toolStatus(params.Arguments)
	case "meept_session_history":
		result, err = s.toolSessionHistory(params.Arguments)
	}

	if err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mustMarshal(map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("error: %v", err)}}, "isError": true}),
		}
	}

	var text string
	switch v := result.(type) {
	case string:
		text = v
	case json.RawMessage:
		text = string(v)
	default:
		// Marshal the result as JSON for proper formatting
		data, err := json.Marshal(result)
		if err != nil {
			text = fmt.Sprintf("error marshaling result: %v", err)
		} else {
			text = string(data)
		}
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  mustMarshal(map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}),
	}
}

// Tool implementations delegate to the RPC client.

func (s *Server) toolSessions(args map[string]any) (any, error) {
	action, _ := args["action"].(string)
	switch action {
	case "list":
		return s.client.ListSessions()
	case "create":
		name, _ := args["name"].(string)
		if name == "" {
			name = "mcp-session"
		}
		return s.client.CreateSession(name)
	case "attach":
		sessionID, _ := args["session_id"].(string)
		clientID, _ := args["client_id"].(string)
		if clientID == "" {
			clientID = "mcp"
		}
		if err := s.client.AttachSession(sessionID, clientID); err != nil {
			return nil, err
		}
		// Auto-catchup: fetch recent history
		messages, err := s.client.GetSessionMessages(sessionID, 0, 50)
		if err != nil {
			return map[string]any{"status": "attached", "session_id": sessionID}, nil
		}
		return map[string]any{
			"status":     "attached",
			"session_id": sessionID,
			"history":    messages,
		}, nil
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

func (s *Server) toolSend(args map[string]any) (any, error) {
	sessionID, _ := args["session_id"].(string)
	message, _ := args["message"].(string)
	if sessionID == "" || message == "" {
		return nil, fmt.Errorf("session_id and message are required")
	}
	// Use the chat RPC method with source_client
	sourceClient, _ := args["source_client"].(string)
	if sourceClient == "" {
		sourceClient = "mcp"
	}
	// The Chat method on transport.Client sends to chat.request
	// We need to include source_client, so use the low-level Call method
	params := map[string]any{
		"message":         message,
		"conversation_id": sessionID,
		"source_client":   sourceClient,
	}
	result, err := s.client.Call("chat", params)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"response": string(result),
	}, nil
}

func (s *Server) toolEvents(args map[string]any) (any, error) {
	subID, _ := args["subscription_id"].(string)
	since, _ := args["since"].(string)
	if subID == "" {
		return nil, fmt.Errorf("subscription_id is required")
	}
	params := map[string]any{
		"subscription_id": subID,
		"since":           since,
	}
	result, err := s.client.Call("bus.poll", params)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(result), nil
}

func (s *Server) toolStatus(args map[string]any) (any, error) {
	status, err := s.client.Status()
	if err != nil {
		return nil, err
	}
	return status, nil
}

func (s *Server) toolSessionHistory(args map[string]any) (any, error) {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	limit := 50
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	return s.client.GetSessionMessages(sessionID, 0, limit)
}

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

// ConnectRPC connects to the daemon's Unix socket. The socket is trusted to
// provide authenticated RPC. If the socket file is world-accessible, a warning
// is logged so the operator can investigate unintended permission drift.
//
// The check is advisory: the connection still proceeds when broad permissions
// are detected, because the operator may have intentionally set them (for
// example, when multiple trusted users share a development machine).
func (s *Server) ConnectRPC(socketPath string) error {
	// Advisory permission check: warn if the socket is more permissive than
	// owner-only access. We check the group/other bits (0o077) so that any
	// non-owner read, write, or execute triggers the warning.
	if info, err := os.Stat(socketPath); err == nil {
		perm := info.Mode().Perm()
		if perm&0o077 != 0 {
			s.logger.Warn("rpc socket is accessible beyond the owner",
				"socket_path", socketPath,
				"mode", fmt.Sprintf("%04o", perm),
				"hint", "consider `chmod 0600 <socket>` if this is unintended",
			)
		}
	}

	cfg := transport.DefaultConfig()
	cfg.SocketPath = socketPath
	client, err := transport.New(cfg)
	if err != nil {
		return fmt.Errorf("create RPC client: %w", err)
	}
	if err := client.Connect(); err != nil {
		client.Close()
		return fmt.Errorf("connect to daemon: %w", err)
	}
	s.client = client
	return nil
}

// CloseRPC closes the RPC client connection if one was established.
func (s *Server) CloseRPC() {
	if s.client != nil {
		_ = s.client.Close()
	}
}

// ConnectAndSubscribe connects to the daemon and subscribes to event topics.
// Returns the subscription ID for use with meept_events.
func (s *Server) ConnectAndSubscribe(socketPath string) (string, error) {
	if err := s.ConnectRPC(socketPath); err != nil {
		return "", err
	}

	// Subscribe to relevant bus topics
	topics := []string{
		"chat.message.received",
		"chat.response",
		"agent.event.*",
		"worker.*",
	}
	result, err := s.client.Call("bus.subscribe", map[string]any{"topics": topics})
	if err != nil {
		return "", fmt.Errorf("subscribe: %w", err)
	}

	var resp struct {
		SubscriptionID string `json:"subscription_id"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("parse subscription response: %w", err)
	}
	return resp.SubscriptionID, nil
}
