// internal/configui/sections_memory.go
package configui

import (
	"strings"

)

func buildMemoryFields() []Field {
	cfg := loadMainConfigOrFallback()
	mem := &cfg.Memory
	ep := &mem.Episodic
	tk := &mem.Task
	pers := &mem.Personality
	emb := &mem.Embeddings
	sec := &mem.Security
	cac := &mem.Caching
	lim := &mem.Limits
	exp := &mem.Expiration
	ver := &mem.Versioning
	return []Field{
		NewSelectField("memory.backend", "backend", string(mem.Backend), []string{"memvid", "sqlite"}),
		NewTextField("memory.data_dir", "data dir", mem.DataDir),
		NewNumberField("memory.consolidation_interval_hours", "consolidation interval hours", mem.ConsolidationIntervalHours),
		NewToggleField("memory.episodic.enabled", "episodic enabled", ep.Enabled),
		NewNumberField("memory.episodic.max_context_items", "episodic max context items", ep.MaxContextItems),
		NewToggleField("memory.task.enabled", "task enabled", tk.Enabled),
		NewTextField("memory.task.domains", "task domains", strings.Join(tk.Domains, ",")),
		NewToggleField("memory.personality.enabled", "personality enabled", pers.Enabled),
		NewNumberField("memory.personality.update_interval_conversations", "personality update interval", pers.UpdateIntervalConversations),
		NewToggleField("memory.embeddings.enabled", "embeddings enabled", emb.Enabled),
		NewSelectField("memory.embeddings.provider", "embeddings provider", emb.Provider, []string{"openai", "ollama"}),
		NewMaskedField("memory.embeddings.api_key", "embeddings api key", emb.APIKey),
		NewTextField("memory.embeddings.base_url", "embeddings base url", emb.BaseURL),
		NewTextField("memory.embeddings.model", "embeddings model", emb.Model),
		NewNumberField("memory.embeddings.dimension", "embeddings dimension", emb.Dimension),
		NewToggleField("memory.security.enabled", "security enabled", sec.Enabled),
		NewToggleField("memory.security.fail_closed", "security fail closed", sec.FailClosed),
		NewToggleField("memory.security.log_blocked", "security log blocked", sec.LogBlocked),
		NewToggleField("memory.caching.enabled", "caching enabled", cac.Enabled),
		NewToggleField("memory.caching.refresh_on_session_end", "caching refresh on session end", cac.RefreshOnSessionEnd),
		NewDrilldownField("memory.limits", "limits", []DrilldownItem{
			{Name: "limits", Fields: []Field{
				NewToggleField("memory.limits.episodic.enabled", "episodic enabled", lim.Episodic.Enabled),
				NewNumberField("memory.limits.episodic.character_limit", "episodic character limit", lim.Episodic.CharacterLimit),
				NewToggleField("memory.limits.task_code.enabled", "task code enabled", lim.TaskCode.Enabled),
				NewNumberField("memory.limits.task_code.character_limit", "task code character limit", lim.TaskCode.CharacterLimit),
				NewToggleField("memory.limits.task_general.enabled", "task general enabled", lim.TaskGeneral.Enabled),
				NewNumberField("memory.limits.task_general.character_limit", "task general character limit", lim.TaskGeneral.CharacterLimit),
				NewToggleField("memory.limits.task_commands.enabled", "task commands enabled", lim.TaskCommands.Enabled),
				NewNumberField("memory.limits.task_commands.character_limit", "task commands character limit", lim.TaskCommands.CharacterLimit),
				NewToggleField("memory.limits.personality.enabled", "personality enabled", lim.Personality.Enabled),
				NewNumberField("memory.limits.personality.character_limit", "personality character limit", lim.Personality.CharacterLimit),
			}},
		}),
		NewToggleField("memory.expiration.enabled", "expiration enabled", exp.Enabled),
		NewNumberField("memory.expiration.access_expiration_days", "expiration access days", exp.AccessExpirationDays),
		NewToggleField("memory.expiration.summarize_before_delete", "summarize before delete", exp.SummarizeBeforeDelete),
		NewTextField("memory.expiration.summary_category", "summary category", exp.SummaryCategory),
		NewToggleField("memory.versioning.enabled", "versioning enabled", ver.Enabled),
		NewNumberField("memory.versioning.max_versions", "versioning max versions", ver.MaxVersions),
	}
}
