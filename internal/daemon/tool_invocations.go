package daemon

// toolInvocation records execution identity independently of artifact evidence.
// No model reply, argument text, or tool output is stored in this record.
type toolInvocation struct {
	ToolCallID     string `json:"tool_call_id"`
	ToolName       string `json:"tool_name"`
	ConversationID string `json:"conversation_id"`
	AgentID        string `json:"agent_id"`
	Success        bool   `json:"success"`
	Cached         bool   `json:"cached"`
	Conflict       bool   `json:"conflict,omitempty"`
}

func (c *toolEvidenceCollector) recordInvocation(payload toolInvocation) {
	if payload.ToolCallID == "" || payload.ToolName == "" || payload.ConversationID == "" || payload.AgentID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.invocations == nil {
		c.invocations = make(map[string]map[string]toolInvocation)
	}
	calls := c.invocations[payload.ConversationID]
	if calls == nil {
		calls = make(map[string]toolInvocation)
		c.invocations[payload.ConversationID] = calls
	}
	if previous, exists := calls[payload.ToolCallID]; exists && previous != payload {
		payload.Conflict = true
	}
	calls[payload.ToolCallID] = payload
}

func (c *toolEvidenceCollector) drainInvocations(conversationID string) []toolInvocation {
	c.mu.Lock()
	defer c.mu.Unlock()
	calls := c.invocations[conversationID]
	delete(c.invocations, conversationID)
	result := make([]toolInvocation, 0, len(calls))
	for _, call := range calls {
		result = append(result, call)
	}
	return result
}
