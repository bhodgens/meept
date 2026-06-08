package bot

type MemoryNamespace struct {
	BotID string
}

func NewMemoryNamespace(botID string) *MemoryNamespace {
	return &MemoryNamespace{BotID: botID}
}

func (n *MemoryNamespace) Prefix() string {
	return "bot:" + n.BotID
}

func (n *MemoryNamespace) ScopeQuery(scope MemoryScope, query string) string {
	switch scope {
	case MemoryScopePrivate, MemoryScopeReadOnly:
		return n.Prefix() + " " + query
	case MemoryScopeShared:
		return query
	default:
		return n.Prefix() + " " + query
	}
}

func (n *MemoryNamespace) TagMemory(meta map[string]any) map[string]any {
	if meta == nil {
		meta = make(map[string]any)
	}
	meta["bot_id"] = n.BotID
	return meta
}

// FilterBotMemories filters a slice of memory results to only include
// memories belonging to this bot (for private scope enforcement).
func (n *MemoryNamespace) FilterBotMemories(scope MemoryScope, results []map[string]any) []map[string]any {
	if scope == MemoryScopeShared {
		return results
	}
	var filtered []map[string]any
	for _, r := range results {
		if botID, ok := r["bot_id"].(string); ok && botID == n.BotID {
			filtered = append(filtered, r)
		}
	}
	return filtered
}
