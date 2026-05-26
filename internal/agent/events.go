// Package agent provides the agent loop for LLM reasoning interleaved with tool execution.
//
// # Typed Agent Events
//
// The typed event system provides type-safe lifecycle events for the agent loop.
// It coexists with the message bus (internal/bus): typed events serve agent-internal
// lifecycle concerns (turn boundaries, tool execution, context transforms) while
// the bus continues to serve system-wide pub/sub (daemon, scheduler, RPC, TUI).
//
// Convention:
//   - Bus topics: "agent.progress", "agent.action", etc. (system-wide)
//   - Agent events: AgentEventType enum (agent-internal, bridged to bus via EventEmitter)
package agent

import (
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// AgentEventType identifies the type of agent lifecycle event.
type AgentEventType string //nolint:revive // stutter with package name is intentional for API clarity

const (
	// Session lifecycle
	AgentEventSessionStart AgentEventType = "session_start"
	AgentEventSessionEnd   AgentEventType = "session_end"

	// Agent lifecycle
	AgentEventAgentStart AgentEventType = "agent_start"
	AgentEventAgentEnd   AgentEventType = "agent_end"

	// Turn lifecycle
	AgentEventTurnStart AgentEventType = "turn_start"
	AgentEventTurnEnd   AgentEventType = "turn_end"

	// Message lifecycle
	AgentEventMessageStart  AgentEventType = "message_start"
	AgentEventMessageUpdate AgentEventType = "message_update"
	AgentEventMessageEnd    AgentEventType = "message_end"

	// Tool execution
	AgentEventToolExecutionStart  AgentEventType = "tool_execution_start"
	AgentEventToolExecutionUpdate AgentEventType = "tool_execution_update"
	AgentEventToolExecutionEnd    AgentEventType = "tool_execution_end"

	// Context management
	AgentEventSessionBeforeCompact AgentEventType = "session_before_compact"
	AgentEventSessionCompact       AgentEventType = "session_compact"
	AgentEventSessionBeforeTree    AgentEventType = "session_before_tree"
	AgentEventSessionTree          AgentEventType = "session_tree"

	// Provider interaction
	AgentEventBeforeProviderRequest AgentEventType = "before_provider_request"
	AgentEventBeforeProviderPayload AgentEventType = "before_provider_payload"
	AgentEventAfterProviderResponse AgentEventType = "after_provider_response"

	// Model selection
	AgentEventModelSelect         AgentEventType = "model_select"
	AgentEventThinkingLevelSelect AgentEventType = "thinking_level_select"

	// Resource updates
	AgentEventResourcesUpdate AgentEventType = "resources_update"
	AgentEventQueueUpdate     AgentEventType = "queue_update"

	// Checkpointing
	AgentEventSavePoint AgentEventType = "save_point"
	AgentEventAbort     AgentEventType = "abort"
	AgentEventSettled   AgentEventType = "settled"

	// Chat visibility events
	AgentEventChatMessageReceived    AgentEventType = "chat_message_received"
	AgentEventChatClientDisconnected AgentEventType = "chat_client_disconnected"
)

// AgentEvent is the envelope for all typed agent events.
// Type is the discriminating field. Data holds the event-specific payload.
type AgentEvent struct { //nolint:revive // stutter with package name is intentional for API clarity
	Type           AgentEventType `json:"type"`
	Timestamp      time.Time      `json:"timestamp"`
	AgentID        string         `json:"agent_id"`
	ConversationID string         `json:"conversation_id"`
	Iteration      int            `json:"iteration"`
	Data           AgentEventData `json:"data"`
}

// AgentEventData is the interface all event payloads implement.
type AgentEventData interface { //nolint:revive // stutter with package name is intentional for API clarity
	agentEventData()
}

// --- Session Events ---

// SessionStartData is emitted when an agent session begins.
type SessionStartData struct {
	SessionID string `json:"session_id"`
	Input     string `json:"input"`
	AgentSpec string `json:"agent_spec"`
}

func (SessionStartData) agentEventData() {}

// SessionEndData is emitted when an agent session ends.
type SessionEndData struct {
	SessionID   string        `json:"session_id"`
	Outcome     string        `json:"outcome"`
	Duration    time.Duration `json:"duration"`
	TotalTokens int           `json:"total_tokens"`
	TotalIter   int           `json:"total_iter"`
	Error       string        `json:"error,omitempty"`
}

func (SessionEndData) agentEventData() {}

// --- Agent Lifecycle Events ---

// AgentStartData is emitted when the agent loop starts.
type AgentStartData struct { //nolint:revive // stutter with package name is intentional for API clarity
	AgentID   string `json:"agent_id"`
	AgentType string `json:"agent_type"`
	ModelRef  string `json:"model_ref"`
}

func (AgentStartData) agentEventData() {}

// AgentEndData is emitted when the agent loop ends.
type AgentEndData struct { //nolint:revive // stutter with package name is intentional for API clarity
	AgentID  string        `json:"agent_id"`
	Reason   string        `json:"reason"`
	Duration time.Duration `json:"duration"`
}

func (AgentEndData) agentEventData() {}

// --- Turn Events ---

// TurnStartData is emitted at the beginning of each loop iteration.
type TurnStartData struct {
	TurnNumber       int `json:"turn_number"`
	TotalTokensSoFar int `json:"total_tokens_so_far"`
	MessagesCount    int `json:"messages_count"`
	ToolCount        int `json:"tool_count"`
}

func (TurnStartData) agentEventData() {}

// TurnEndData is emitted at the end of each loop iteration.
type TurnEndData struct {
	TurnNumber     int    `json:"turn_number"`
	HadToolCalls   bool   `json:"had_tool_calls"`
	ToolCallCount  int    `json:"tool_call_count"`
	ResponseTokens int    `json:"response_tokens"`
	CachedTokens   int    `json:"cached_tokens"`
	StoppedBy      string `json:"stopped_by"`
}

func (TurnEndData) agentEventData() {}

// --- Message Events ---

// MessageStartData is emitted when a message begins being formed.
type MessageStartData struct {
	Role       string `json:"role"`
	TokenCount int    `json:"token_count"`
}

func (MessageStartData) agentEventData() {}

// MessageUpdateData is emitted during streaming message updates.
type MessageUpdateData struct {
	Role       string `json:"role"`
	Delta      string `json:"delta"`
	TokenCount int    `json:"token_count"`
}

func (MessageUpdateData) agentEventData() {}

// MessageEndData is emitted when a message is finalized.
type MessageEndData struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	TokenCount int    `json:"token_count"`
	ToolCalls  int    `json:"tool_calls"`
}

func (MessageEndData) agentEventData() {}

// --- Tool Execution Events ---

// ToolExecutionStartData is emitted before a tool is executed.
type ToolExecutionStartData struct {
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	Arguments  string `json:"arguments"`
}

func (ToolExecutionStartData) agentEventData() {}

// ToolExecutionUpdateData is emitted during tool execution progress.
type ToolExecutionUpdateData struct {
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	Status     string `json:"status"`
	Detail     string `json:"detail"`
}

func (ToolExecutionUpdateData) agentEventData() {}

// ToolExecutionEndData is emitted after a tool execution completes or is blocked.
type ToolExecutionEndData struct {
	ToolCallID  string        `json:"tool_call_id"`
	ToolName    string        `json:"tool_name"`
	Success     bool          `json:"success"`
	Result      string        `json:"result"`
	Error       string        `json:"error,omitempty"`
	Cached      bool          `json:"cached"`
	Duration    time.Duration `json:"duration"`
	Blocked     bool          `json:"blocked"`
	BlockReason string        `json:"block_reason,omitempty"`
}

func (ToolExecutionEndData) agentEventData() {}

// --- Context Management Events ---

// SessionBeforeCompactData is emitted before context compaction.
type SessionBeforeCompactData struct {
	MessageCount int    `json:"message_count"`
	TokenCount   int    `json:"token_count"`
	Reason       string `json:"reason"`
}

func (SessionBeforeCompactData) agentEventData() {}

// SessionCompactData is emitted after context compaction.
type SessionCompactData struct {
	MessageCountBefore int    `json:"message_count_before"`
	MessageCountAfter  int    `json:"message_count_after"`
	TokensSaved        int    `json:"tokens_saved"`
	Method             string `json:"method"`
}

func (SessionCompactData) agentEventData() {}

// SessionBeforeTreeData is emitted before tree restructuring.
type SessionBeforeTreeData struct {
	NodeCount int `json:"node_count"`
	Depth     int `json:"depth"`
}

func (SessionBeforeTreeData) agentEventData() {}

// SessionTreeData is emitted after tree restructuring.
type SessionTreeData struct {
	Nodes     int `json:"nodes"`
	Depth     int `json:"depth"`
	TokenSize int `json:"token_size"`
}

func (SessionTreeData) agentEventData() {}

// --- Provider Interaction Events ---

// BeforeProviderRequestData is emitted before making an LLM provider request.
type BeforeProviderRequestData struct {
	ModelID    string               `json:"model_id"`
	Messages   []llm.ChatMessage    `json:"messages"`
	Tools      []llm.ToolDefinition `json:"tools,omitempty"`
	TokenCount int                  `json:"token_count"`
}

func (BeforeProviderRequestData) agentEventData() {}

// BeforeProviderPayloadData is emitted with the serialized provider payload.
type BeforeProviderPayloadData struct {
	ModelID  string `json:"model_id"`
	Payload  string `json:"payload"`
	Endpoint string `json:"endpoint"`
}

func (BeforeProviderPayloadData) agentEventData() {}

// AfterProviderResponseData is emitted after receiving an LLM provider response.
type AfterProviderResponseData struct {
	ModelID        string        `json:"model_id"`
	StatusCode     int           `json:"status_code"`
	ResponseTokens int           `json:"response_tokens"`
	CachedTokens   int           `json:"cached_tokens"`
	Latency        time.Duration `json:"latency"`
	Cached         bool          `json:"cached"`
	Error          string        `json:"error,omitempty"`
}

func (AfterProviderResponseData) agentEventData() {}

// --- Model Selection Events ---

// ModelSelectData is emitted when a model is selected.
type ModelSelectData struct {
	Alias    string `json:"alias"`
	ModelID  string `json:"model_id"`
	Provider string `json:"provider"`
	Reason   string `json:"reason"`
}

func (ModelSelectData) agentEventData() {}

// ThinkingLevelSelectData is emitted when a thinking level is chosen.
type ThinkingLevelSelectData struct {
	Level  string `json:"level"`
	Reason string `json:"reason"`
}

func (ThinkingLevelSelectData) agentEventData() {}

// --- Resource Events ---

// ResourcesUpdateData is emitted when resource usage changes.
type ResourcesUpdateData struct {
	TokensUsed     int `json:"tokens_used"`
	TokensBudget   int `json:"tokens_budget"`
	IterationsUsed int `json:"iterations_used"`
	IterationsMax  int `json:"iterations_max"`
}

func (ResourcesUpdateData) agentEventData() {}

// QueueUpdateData is emitted when the message queue state changes.
type QueueUpdateData struct {
	QueueDepth    int `json:"queue_depth"`
	ActiveJobs    int `json:"active_jobs"`
	CompletedJobs int `json:"completed_jobs"`
}

func (QueueUpdateData) agentEventData() {}

// --- Checkpoint Events ---

// SavePointData is emitted when a save point is created.
type SavePointData struct {
	Reason string         `json:"reason"`
	State  map[string]any `json:"state"`
}

func (SavePointData) agentEventData() {}

// AbortData is emitted when an abort occurs.
type AbortData struct {
	Reason    string `json:"reason"`
	Iteration int    `json:"iteration"`
}

func (AbortData) agentEventData() {}

// SettledData is emitted after all async listeners have settled.
type SettledData struct {
	ListenerCount int           `json:"listener_count"`
	Duration      time.Duration `json:"duration"`
}

func (SettledData) agentEventData() {}

// --- Chat Message Visibility Events ---

// ChatMessageReceivedData is emitted when a client sends a message to a session.
// Broadcast to all session participants for bilateral visibility.
type ChatMessageReceivedData struct {
	SessionID    string `json:"session_id"`
	SourceClient string `json:"source_client"`
	Content      string `json:"content"`
}

func (ChatMessageReceivedData) agentEventData() {}

// ChatClientDisconnectedData is emitted when a client disconnects from a session.
type ChatClientDisconnectedData struct {
	SessionID    string `json:"session_id"`
	SourceClient string `json:"source_client"`
}

func (ChatClientDisconnectedData) agentEventData() {}
