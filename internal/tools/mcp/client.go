package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/mcp/transport"
)

// Client is an MCP client that communicates with a single MCP server.
type Client struct {
	name      string
	transport transport.Transport
	logger    *slog.Logger

	mu        sync.RWMutex
	tools     []ToolInfo
	requestID atomic.Int64
	connected atomic.Bool

	// Server capabilities from initialize
	serverInfo   ImplementationInfo
	capabilities ServerCapabilities
}

// NewClient creates a new MCP client.
func NewClient(name string, transport transport.Transport, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		name:      name,
		transport: transport,
		logger:    logger,
	}
}

// Name returns the server name.
func (c *Client) Name() string {
	return c.name
}

// Connect starts the transport and performs the MCP handshake.
func (c *Client) Connect(ctx context.Context) error {
	c.logger.Info("connecting to MCP server", "name", c.name)

	// Start the transport
	if err := c.transport.Start(ctx); err != nil {
		return fmt.Errorf("failed to start transport: %w", err)
	}

	// Perform initialize handshake
	initResult, err := c.initialize(ctx)
	if err != nil {
		c.transport.Close()
		return fmt.Errorf("initialize failed: %w", err)
	}

	c.mu.Lock()
	c.serverInfo = initResult.ServerInfo
	c.capabilities = initResult.Capabilities
	c.mu.Unlock()

	// Send initialized notification
	if err := c.sendNotification(ctx, "notifications/initialized", nil); err != nil {
		c.logger.Warn("failed to send initialized notification", "error", err)
	}

	// Discover tools
	if err := c.refreshTools(ctx); err != nil {
		c.logger.Warn("failed to list tools", "error", err)
	}

	c.connected.Store(true)
	c.logger.Info("connected to MCP server",
		"name", c.name,
		"server", initResult.ServerInfo.Name,
		"version", initResult.ServerInfo.Version,
		"tools", len(c.tools),
	)

	return nil
}

// initialize performs the MCP initialize handshake.
func (c *Client) initialize(ctx context.Context) (*InitializeResult, error) {
	params := InitializeParams{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    ClientCapabilities{},
		ClientInfo: ImplementationInfo{
			Name:    "meept",
			Version: "0.2.0",
		},
	}

	resp, err := c.request(ctx, "initialize", params)
	if err != nil {
		return nil, err
	}

	return ExtractResult[*InitializeResult](resp)
}

// refreshTools fetches the tool list from the server.
func (c *Client) refreshTools(ctx context.Context) error {
	resp, err := c.request(ctx, "tools/list", nil)
	if err != nil {
		return err
	}

	result, err := ExtractResult[*ListToolsResult](resp)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.tools = result.Tools
	c.mu.Unlock()

	return nil
}

// ListTools returns the available tools.
func (c *Client) ListTools() []ToolInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]ToolInfo{}, c.tools...)
}

// ToLLMDefinitions converts the server's tools to LLM tool definitions.
// Tool names are prefixed with the server name (e.g., "servername.toolname").
func (c *Client) ToLLMDefinitions() []llm.ToolDefinition {
	c.mu.RLock()
	defer c.mu.RUnlock()

	definitions := make([]llm.ToolDefinition, 0, len(c.tools))
	for _, tool := range c.tools {
		prefixedName := fmt.Sprintf("%s.%s", c.name, tool.Name)

		// Convert input schema to FunctionParameters
		params := llm.FunctionParameters{
			Type:       "object",
			Properties: make(map[string]llm.ParameterProperty),
		}

		if tool.InputSchema != nil {
			// Extract type
			if t, ok := tool.InputSchema["type"].(string); ok {
				params.Type = t
			}

			// Extract properties
			if props, ok := tool.InputSchema["properties"].(map[string]any); ok {
				for name, propRaw := range props {
					prop, ok := propRaw.(map[string]any)
					if !ok {
						continue
					}

					paramProp := llm.ParameterProperty{}
					if t, ok := prop["type"].(string); ok {
						paramProp.Type = t
					}
					if d, ok := prop["description"].(string); ok {
						paramProp.Description = d
					}
					if enum, ok := prop["enum"].([]any); ok {
						for _, e := range enum {
							if s, ok := e.(string); ok {
								paramProp.Enum = append(paramProp.Enum, s)
							}
						}
					}

					params.Properties[name] = paramProp
				}
			}

			// Extract required
			if req, ok := tool.InputSchema["required"].([]any); ok {
				for _, r := range req {
					if s, ok := r.(string); ok {
						params.Required = append(params.Required, s)
					}
				}
			}
		}

		def := llm.NewToolDefinition(prefixedName, tool.Description, params)
		definitions = append(definitions, def)
	}

	return definitions
}

// CallTool invokes a tool on the MCP server.
func (c *Client) CallTool(ctx context.Context, toolName string, arguments map[string]any) (*tools.ToolResult, error) {
	if !c.connected.Load() {
		return nil, fmt.Errorf("client not connected")
	}

	params := CallToolParams{
		Name:      toolName,
		Arguments: arguments,
	}

	resp, err := c.request(ctx, "tools/call", params)
	if err != nil {
		return tools.NewErrorResult(err.Error()), err
	}

	result, err := ExtractResult[*CallToolResult](resp)
	if err != nil {
		return tools.NewErrorResult(err.Error()), err
	}

	// Convert content blocks to text
	var text string
	for _, block := range result.Content {
		switch block.Type {
		case "text":
			if text != "" {
				text += "\n"
			}
			text += block.Text
		}
	}

	if result.IsError {
		return tools.NewErrorResult(text), nil
	}

	return tools.NewSuccessResult(text), nil
}

// Close disconnects from the MCP server.
func (c *Client) Close() error {
	if !c.connected.Load() {
		return nil
	}

	c.connected.Store(false)
	c.logger.Info("disconnecting from MCP server", "name", c.name)

	return c.transport.Close()
}

// IsConnected returns true if the client is connected.
func (c *Client) IsConnected() bool {
	return c.connected.Load() && c.transport.IsRunning()
}

// ServerInfo returns information about the connected server.
func (c *Client) ServerInfo() ImplementationInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.serverInfo
}

// ServerCapabilities returns the server's capabilities.
func (c *Client) ServerCapabilities() ServerCapabilities {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.capabilities
}

// request sends a JSON-RPC request and waits for the response.
func (c *Client) request(ctx context.Context, method string, params any) (*Response, error) {
	id := c.requestID.Add(1)

	req := NewRequest(id, method, params)
	data, err := Serialize(req)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize request: %w", err)
	}

	respData, err := c.transport.Send(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	resp, err := ParseResponse(respData)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// sendNotification sends a JSON-RPC notification (no response expected).
func (c *Client) sendNotification(ctx context.Context, method string, params any) error {
	notif := NewNotification(method, params)
	data, err := Serialize(notif)
	if err != nil {
		return fmt.Errorf("failed to serialize notification: %w", err)
	}

	// For notifications, we don't wait for a response
	// Some transports might still return data, but we ignore it
	_, _ = c.transport.Send(ctx, data)
	return nil
}
