package acp

import (
	"encoding/json"
	"fmt"
)

// ProtocolVersion is ACP major protocol version 1 (JSON integer, not a string).
const ProtocolVersion = 1

const jsonRPCVersion = "2.0"

// JSON-RPC method names. Later leaves must use these constants, never literals.
const (
	MethodInitialize               = "initialize"
	MethodSessionNew               = "session/new"
	MethodSessionPrompt            = "session/prompt"
	MethodSessionCancel            = "session/cancel"
	MethodSessionUpdate            = "session/update"
	MethodSessionRequestPermission = "session/requestPermission"
)

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// Notification is a JSON-RPC 2.0 notification, or an inbound request
// (method + id) such as session/requestPermission. ID is nil for true
// notifications and set for inbound requests that need Reply.
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *RPCError) Error() string {
	if e == nil {
		return "rpc error"
	}
	if e.Data != nil {
		return fmt.Sprintf("rpc error %d: %s (data: %v)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// ImplementationInfo is clientInfo / agentInfo (name, optional title, version).
type ImplementationInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
}

// FSCapabilities advertises fs/readTextFile and fs/writeTextFile support.
type FSCapabilities struct {
	ReadTextFile  bool `json:"readTextFile,omitempty"`
	WriteTextFile bool `json:"writeTextFile,omitempty"`
}

// ClientCapabilities is the optional clientCapabilities object on initialize.
type ClientCapabilities struct {
	FS       *FSCapabilities `json:"fs,omitempty"`
	Terminal bool            `json:"terminal,omitempty"`
}

// PromptCapabilities lists extra content types the agent accepts in session/prompt.
type PromptCapabilities struct {
	Image           bool `json:"image,omitempty"`
	Audio           bool `json:"audio,omitempty"`
	EmbeddedContext bool `json:"embeddedContext,omitempty"`
}

// MCPCapabilities lists optional MCP transports the agent supports.
type MCPCapabilities struct {
	HTTP bool `json:"http,omitempty"`
	SSE  bool `json:"sse,omitempty"`
}

// AgentCapabilities is the agentCapabilities object on initialize result.
type AgentCapabilities struct {
	LoadSession        bool                `json:"loadSession,omitempty"`
	PromptCapabilities *PromptCapabilities `json:"promptCapabilities,omitempty"`
	MCPCapabilities    *MCPCapabilities    `json:"mcpCapabilities,omitempty"`
}

// AuthMethod is one entry in initialize result authMethods.
type AuthMethod struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// InitializeParams is params for the initialize method.
type InitializeParams struct {
	ProtocolVersion    int                 `json:"protocolVersion"`
	ClientInfo         ImplementationInfo  `json:"clientInfo,omitempty"`
	ClientCapabilities *ClientCapabilities `json:"clientCapabilities,omitempty"`
}

// InitializeResult is the initialize method result.
type InitializeResult struct {
	ProtocolVersion   int                `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities  `json:"agentCapabilities,omitempty"`
	AgentInfo         ImplementationInfo `json:"agentInfo,omitempty"`
	AuthMethods       []AuthMethod       `json:"authMethods,omitempty"`
}

// EnvVariable is a name/value pair for MCP stdio server env.
type EnvVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// MCPServer is an MCP server the agent should connect to on session/new.
type MCPServer struct {
	Name    string        `json:"name"`
	Command string        `json:"command,omitempty"`
	Args    []string      `json:"args,omitempty"`
	Env     []EnvVariable `json:"env,omitempty"`
	Type    string        `json:"type,omitempty"`
	URL     string        `json:"url,omitempty"`
}

// SessionNewParams is params for session/new.
type SessionNewParams struct {
	Cwd        string      `json:"cwd"`
	MCPServers []MCPServer `json:"mcpServers"`
}

// SessionNewResult is the session/new result.
type SessionNewResult struct {
	SessionID string `json:"sessionId"`
}

// ContentBlock is one prompt content item (text, image, resource, …).
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// SessionPromptParams is params for session/prompt.
type SessionPromptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

// SessionPromptResult is the session/prompt result.
type SessionPromptResult struct {
	StopReason string `json:"stopReason"`
}

// SessionCancelParams is params for the session/cancel notification.
type SessionCancelParams struct {
	SessionID string `json:"sessionId"`
}

// SessionUpdate is one session/update variant (discriminated by sessionUpdate).
type SessionUpdate struct {
	SessionUpdate string          `json:"sessionUpdate"`
	MessageID     string          `json:"messageId,omitempty"`
	Content       json.RawMessage `json:"content,omitempty"`
	ToolCallID    string          `json:"toolCallId,omitempty"`
	Title         string          `json:"title,omitempty"`
	Kind          string          `json:"kind,omitempty"`
	Status        string          `json:"status,omitempty"`
}

// SessionUpdateParams is params for the session/update notification.
type SessionUpdateParams struct {
	SessionID string        `json:"sessionId"`
	Update    SessionUpdate `json:"update"`
}
