// internal/configui/sections.go
package configui

import "github.com/caimlas/meept/internal/config"

// loadMainConfigOrFallback loads the main config via the (possibly test-overridden)
// loadMainConfig loader, falling back to DefaultConfig if the result is nil.
// This prevents nil-pointer panics in section builders when the user's config
// file has parse errors.
func loadMainConfigOrFallback() *config.Config {
	cfg, err := loadMainConfig()
	if err != nil || cfg == nil {
		return config.DefaultConfig()
	}
	return cfg
}

// BuildSectionFields creates the fields for a given section key path.
// Sections are defined in separate files (sections_*.go) but this function
// dispatches to them.
func BuildSectionFields(keyPath string) []Field {
	switch keyPath {
	case "daemon":
		return buildDaemonFields()
	case "scheduler":
		return buildSchedulerFields()
	case "transport":
		return buildTransportFields()
	case "llm":
		return buildLLMFields()
	case "memory":
		return buildMemoryFields()
	case "security":
		return buildSecurityFields()
	case "client":
		return buildClientFields()
	case "agents":
		return buildAgentsFields()
	case "mcp_servers":
		return buildMCPServersFields()
	case "models":
		return buildModelsFields()
	case "multiagent":
		return buildMultiAgentFields()
	case "agent":
		return buildAgentLoopFields()
	case "queue":
		return buildQueueFields()
	case "workers":
		return buildWorkersFields()
	case "isolation":
		return buildIsolationFields()
	case "workspace":
		return buildWorkspaceFields()
	case "skills":
		return buildSkillsFields()
	case "orchestrator":
		return buildOrchestratorFields()
	case "compaction":
		return buildCompactionFields()
	case "session":
		return buildSessionFields()
	case "code_intel":
		return buildCodeIntelFields()
	case "telegram":
		return buildTelegramFields()
	case "web":
		return buildWebFields()
	case "mcp":
		return buildMCPFields()
	case "plugins":
		return buildPluginsFields()
	case "selfimprove":
		return buildSelfImproveFields()
	case "shadow":
		return buildShadowFields()
	case "distributed_memory":
		return buildDistributedMemoryFields()
	case "q_agent":
		return buildQAgentFields()
	case "tooling":
		return buildToolingFields()
	case "calendar":
		return buildCalendarFields()
	case "memvid":
		return buildMemvidFields()
	case "presets":
		return buildPresetsFields()
	case "projects":
		return buildProjectsFields()
	case "tts":
		return buildTTSFields()
	case "stt":
		return buildSTTFields()
	case "oauth":
		return buildOAuthFields()
	default:
		return []Field{
			NewTextField("_stub", "(section not yet implemented)", ""),
		}
	}
}
