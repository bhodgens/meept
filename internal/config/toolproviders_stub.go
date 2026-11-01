// stub: implementation lands in 04-config.md
//
// Compile-only stub for contract C3. Contains declarations only - zero
// logic. EnabledNames and the Config.ToolProviders field are
// INTENTIONALLY NOT DECLARED here: 04-config.md implements them, and the
// gate tests must fail RED naming exactly those missing symbols, never a
// compilation error on the struct shape itself. Leaf 04 replaces this
// file with the real schema in schema.go and deletes this file.
package config

// ToolProvidersConfig is the [tool_providers] block in meept.json5:
// per-provider enable/disable. Absent map or absent entry = enabled
// (default-true, so migrated native groups keep working on existing
// installs).
type ToolProvidersConfig struct {
	Providers map[string]ToolProviderEntry `json:"providers" toml:"providers"`
}

// ToolProviderEntry is one provider's configuration.
type ToolProviderEntry struct {
	Enabled *bool `json:"enabled,omitempty" toml:"enabled,omitempty"`
}
