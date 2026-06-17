// internal/configui/sections_multiagent.go
package configui

func buildMultiAgentFields() []Field {
	cfg := loadMainConfigOrFallback()
	s := &cfg.MultiAgent
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewTextField("dispatcher_model", "dispatcher model", s.DispatcherModel),
		NewTextField("default_model", "default model", s.DefaultModel),
		NewTextField("classifier_model", "classifier model", s.ClassifierModel),
		NewNumberField("max_memory_refs", "max memory refs", s.MaxMemoryRefs),
		NewNumberField("context_search_limit", "context search limit", s.ContextSearchLimit),
	}
}
