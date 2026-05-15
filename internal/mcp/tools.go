package mcp

// ToolDefinition describes an MCP tool.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolDefinitions returns all MCP tools exposed by the meept server.
func ToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "meept_sessions",
			Description: "List, create, or attach to meept chat sessions. Use action 'list' to see sessions, 'create' to make a new one, 'attach' to join an existing session (auto-fetches history).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"enum":        []string{"list", "create", "attach"},
						"description": "Action to perform",
					},
					"session_id": map[string]any{
						"type":        "string",
						"description": "Session ID (required for attach)",
					},
					"name": map[string]any{
						"type":        "string",
						"description": "Name for new session (optional, for create)",
					},
					"client_id": map[string]any{
						"type":        "string",
						"description": "Client identifier (e.g. 'claude', used for attach)",
					},
				},
				"required": []string{"action"},
			},
		},
		{
			Name:        "meept_send",
			Description: "Send a message to an attached meept session. The message is processed by the agent system and the response is returned.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{
						"type":        "string",
						"description": "Session ID to send to",
					},
					"message": map[string]any{
						"type":        "string",
						"description": "Message text to send",
					},
					"source_client": map[string]any{
						"type":        "string",
						"description": "Client identifier (e.g. 'claude')",
					},
				},
				"required": []string{"session_id", "message"},
			},
		},
		{
			Name:        "meept_events",
			Description: "Poll events from a meept session since the last call. Returns agent progress events, chat messages from other participants, and agent responses.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"subscription_id": map[string]any{
						"type":        "string",
						"description": "Subscription ID from bus.subscribe",
					},
					"since": map[string]any{
						"type":        "string",
						"description": "RFC3339 timestamp to fetch events after",
					},
				},
				"required": []string{"subscription_id"},
			},
		},
		{
			Name:        "meept_status",
			Description: "Get meept daemon status including active agents, queue depth, and connected clients.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "meept_session_history",
			Description: "Get recent messages from a meept session for context.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{
						"type":        "string",
						"description": "Session ID",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum messages to return (default 50)",
					},
				},
				"required": []string{"session_id"},
			},
		},
	}
}
