package llm

// Schema mode resolution (loop-economics leaf 02 — indexed tool schemas).
//
// The effective mode is a pure function of configuration, resolved in the
// loop at construction time:
//
//	per-model schema_mode (models.json5 model entry)
//		> provider schema_mode (models.json5 provider block)
//		> global [agent.tools].schema_mode
//		> "indexed" (leaf verdict: indexed is default-on; "full" restores
//		  legacy full-schema payloads)
//
// Per-model values are pre-resolved into ModelConfig.SchemaMode by
// modelConfigFrom (warn+ignore on unknown strings, mirroring tool_constraint).

// SchemaModeValid reports whether s is a recognized schema-mode string:
// "" (unset), "full", or "indexed". Unknown strings are rejected at
// [agent.tools] config-load validation and warn-ignored at models.json5
// resolve time.
func SchemaModeValid(s string) bool {
	switch s {
	case "", "full", "indexed":
		return true
	}
	return false
}

// EffectiveSchemaMode resolves the tool-schema mode for the given provider
// and model, honoring the full precedence chain described above. It is a
// pure function of configuration (thread-safe): it reads the resolver's
// ProvidersConfig and the globalMode argument supplied by the caller
// (typically config.Agent.Tools.SchemaMode, where "" means the indexed
// default). Unknown provider/model references simply fall through to the
// provider/global path.
func (r *Resolver) EffectiveSchemaMode(providerID, modelID, globalMode string) string {
	// Final fallback: indexed is default-on per the leaf 02 verdict.
	const fallback = "indexed"

	global := globalMode
	if global == "" {
		global = fallback
	}
	// Defensive: an unknown global value behaves as the default rather than
	// disabling resolution entirely (config-level validation already rejects
	// it with a pointer at agent.tools.schema_mode).
	if !SchemaModeValid(global) {
		global = fallback
	}

	// Snapshot the config pointer once; ProvidersConfig is treated as
	// immutable after load (same pattern as modelConfigFrom/GetAllModels).
	r.mu.Lock()
	cfg := r.config
	r.mu.Unlock()
	if cfg == nil {
		return global
	}

	provider, ok := cfg.Providers[providerID]
	if !ok {
		return global
	}

	// Model entry wins. ModelConfig.SchemaMode is the already-resolved
	// per-model-over-provider value; a non-empty value here means an
	// explicit override at one of those two levels.
	mc := ResolveModelRef(providerID+"/"+modelID, cfg)
	if mc != nil && mc.SchemaMode != "" {
		return mc.SchemaMode
	}

	// Provider block next. Guard with SchemaModeValid so this function can
	// never return an unrecognized string (invalid provider-level values are
	// also warn+ignored at modelConfigFrom resolve time).
	if psm := provider.Options.SchemaMode; psm != "" && SchemaModeValid(psm) {
		return psm
	}

	return global
}
