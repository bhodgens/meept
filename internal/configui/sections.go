// internal/configui/sections.go
package configui

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
	default:
		return []Field{
			NewTextField("_stub", "(section not yet implemented)", ""),
		}
	}
}
