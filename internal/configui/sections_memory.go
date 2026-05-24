// internal/configui/sections_memory.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildMemoryFields() []Field {
	cfg, _ := config.LoadDefault()
	mem := &cfg.Memory
	ep := &mem.Episodic
	tk := &mem.Task
	pers := &mem.Personality
	emb := &mem.Embeddings
	sec := &mem.Security
	cac := &mem.Caching
	exp := &mem.Expiration
	ver := &mem.Versioning
	return []Field{
		NewSelectField("memory.backend", "backend", string(mem.Backend), []string{"memvid", "sqlite"}),
		NewTextField("memory.data_dir", "data dir", mem.DataDir),
		NewNumberField("memory.consolidation_interval_hours", "consolidation interval hours", mem.ConsolidationIntervalHours),
		NewToggleField("memory.episodic.enabled", "episodic enabled", ep.Enabled),
		NewNumberField("memory.episodic.max_context_items", "episodic max context items", ep.MaxContextItems),
		NewToggleField("memory.task.enabled", "task enabled", tk.Enabled),
		NewToggleField("memory.personality.enabled", "personality enabled", pers.Enabled),
		NewNumberField("memory.personality.update_interval_conversations", "personality update interval", pers.UpdateIntervalConversations),
		NewToggleField("memory.embeddings.enabled", "embeddings enabled", emb.Enabled),
		NewSelectField("memory.embeddings.provider", "embeddings provider", emb.Provider, []string{"openai", "ollama"}),
		NewMaskedField("memory.embeddings.api_key", "embeddings api key", emb.APIKey),
		NewTextField("memory.embeddings.base_url", "embeddings base url", emb.BaseURL),
		NewTextField("memory.embeddings.model", "embeddings model", emb.Model),
		NewToggleField("memory.security.enabled", "security enabled", sec.Enabled),
		NewToggleField("memory.security.fail_closed", "security fail closed", sec.FailClosed),
		NewToggleField("memory.caching.enabled", "caching enabled", cac.Enabled),
		NewToggleField("memory.expiration.enabled", "expiration enabled", exp.Enabled),
		NewNumberField("memory.expiration.access_expiration_days", "expiration access days", exp.AccessExpirationDays),
		NewToggleField("memory.versioning.enabled", "versioning enabled", ver.Enabled),
		NewNumberField("memory.versioning.max_versions", "versioning max versions", ver.MaxVersions),
	}
}
