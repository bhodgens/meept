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
