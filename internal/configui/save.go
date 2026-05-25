package configui

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools/mcp"
	"github.com/caimlas/meept/internal/tui"
)

// Loader functions are package-level variables so tests can override them
// to inject configs from temp directories without modifying global state.
var (
	loadMainConfig       = config.LoadDefault
	loadClientConfig     = tui.LoadClientConfig
	loadProvidersConfig  = llm.LoadProvidersConfigDefault
	loadMCPConfig        = config.LoadMCPConfigDefault
	loadAgentsConfig     = config.LoadAgentDefinitionsDefault
	loadPresetsConfig    = config.LoadPresetsConfigDefault
)

// SaveSection writes modified fields back to the correct config file.
func SaveSection(sm *SectionModel) error {
	switch sm.ConfigFile() {
	case "client.json5":
		return saveClientConfig(sm)
	case "models.json5":
		return saveModelsConfig(sm)
	case "mcp_servers.json5":
		return saveMCPServersConfig(sm)
	case "agents.json5":
		return saveAgentsConfig(sm)
	case "presets.json5":
		return savePresetsConfig(sm)
	default:
		return saveMainConfigSection(sm)
	}
}

// saveMainConfigSection loads the full main config, applies dirty field values
// via keypath, and writes the entire config back.
// For drilldown sub-sections, it uses drilldownPrefix instead of sectionKey
// as the keypath prefix.
func saveMainConfigSection(sm *SectionModel) error {
	cfg, err := loadMainConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	prefix := sm.SectionKey()
	if sm.IsDrilldown() {
		prefix = sm.DrilldownPrefix()
	}
	for _, f := range sm.Fields() {
		if f.IsDirty() {
			fullPath := prefix + "." + f.Key()
			if err := SetKeypath(cfg, fullPath, f.Get()); err != nil {
				return fmt.Errorf("set %s: %w", fullPath, err)
			}
		}
	}
	return SaveMainConfig(cfg)
}

// setStructField sets a dot-notation path value on any struct pointer using
// reflection. It resolves each path segment via the struct's json tag to find
// the target field, then assigns the string value with appropriate type
// conversion. Works identically to SetKeypath but is not tied to *config.Config.
func setStructField(target any, path string, value string) error {
	v := reflect.ValueOf(target)
	parts := strings.Split(path, ".")
	parent, err := resolvePath(v, parts[:len(parts)-1])
	if err != nil {
		return err
	}
	fieldName := parts[len(parts)-1]

	parentType := parent.Type()
	if parent.Kind() == reflect.Ptr {
		parent = parent.Elem()
		parentType = parent.Type()
	}
	for i := 0; i < parentType.NumField(); i++ {
		field := parentType.Field(i)
		tag := field.Tag.Get("json")
		tagName := strings.Split(tag, ",")[0]
		if tagName == fieldName {
			fv := parent.Field(i)
			switch fv.Kind() {
			case reflect.String:
				fv.SetString(value)
			case reflect.Bool:
				b, err := strconv.ParseBool(value)
				if err != nil {
					return fmt.Errorf("invalid bool %q: %w", value, err)
				}
				fv.SetBool(b)
			case reflect.Int, reflect.Int64:
				n, err := strconv.Atoi(value)
				if err != nil {
					return fmt.Errorf("invalid int %q: %w", value, err)
				}
				fv.SetInt(int64(n))
			case reflect.Float64:
				f, err := strconv.ParseFloat(value, 64)
				if err != nil {
					return fmt.Errorf("invalid float %q: %w", value, err)
				}
				fv.SetFloat(f)
			default:
				return fmt.Errorf("unsupported type %s for field %s", fv.Kind(), fieldName)
			}
			return nil
		}
	}
	return fmt.Errorf("field %q not found", fieldName)
}

// --- client.json5 ---

func saveClientConfig(sm *SectionModel) error {
	cfg, err := loadClientConfig()
	if err != nil {
		return fmt.Errorf("load client config: %w", err)
	}
	for _, f := range sm.Fields() {
		if f.IsDirty() {
			if err := setStructField(cfg, f.Key(), f.Get()); err != nil {
				return fmt.Errorf("set %s: %w", f.Key(), err)
			}
		}
	}
	return WriteConfigFile(ConfigFilePath("client.json5"), cfg)
}

// --- models.json5 ---

func saveModelsConfig(sm *SectionModel) error {
	cfg, err := loadProvidersConfig()
	if err != nil {
		return fmt.Errorf("load models config: %w", err)
	}

	if sm.IsDrilldown() {
		// Drilldown sub-section: prefix is like "providers.openai"
		// Fields have keys like "api", "options.base_url", etc.
		// We use the drilldown prefix to resolve the correct map entry.
		parts := strings.SplitN(sm.DrilldownPrefix(), ".", 2)
		if len(parts) == 2 && parts[0] == "providers" {
			providerName := parts[1]
			provider, ok := cfg.Providers[providerName]
			if !ok {
				// New provider: create it
				provider = llm.ProviderConfig{}
			}
			for _, f := range sm.Fields() {
				if !f.IsDirty() {
					continue
				}
				if err := setStructField(&provider, f.Key(), f.Get()); err != nil {
					return fmt.Errorf("set providers.%s.%s: %w", providerName, f.Key(), err)
				}
			}
			cfg.Providers[providerName] = provider
		}
	} else {
		for _, f := range sm.Fields() {
			if !f.IsDirty() {
				continue
			}
			// disabled_providers is a []string displayed as comma-separated text;
			// split it back into a slice rather than going through reflection.
			if f.Key() == "disabled_providers" {
				val := f.Get()
				if val == "" {
					cfg.DisabledProviders = nil
				} else {
					cfg.DisabledProviders = strings.Split(val, ", ")
				}
				continue
			}
			if err := setStructField(cfg, f.Key(), f.Get()); err != nil {
				return fmt.Errorf("set %s: %w", f.Key(), err)
			}
		}
	}
	return WriteConfigFile(ConfigFilePath("models.json5"), cfg)
}

// --- mcp_servers.json5 ---

func saveMCPServersConfig(sm *SectionModel) error {
	cfg, err := loadMCPConfig()
	if err != nil {
		return fmt.Errorf("load mcp servers config: %w", err)
	}

	if sm.IsDrilldown() {
		// Drilldown sub-section: prefix is like "servers.my_server"
		// MCP servers are a slice; find by name and apply changes.
		parts := strings.SplitN(sm.DrilldownPrefix(), ".", 2)
		if len(parts) == 2 && parts[0] == "servers" {
			serverName := parts[1]
			// Find the server in the slice by name
			idx := -1
			for i, s := range cfg.Servers {
				if s.Name == serverName {
					idx = i
					break
				}
			}
			if idx == -1 {
				// New server: append it
				cfg.Servers = append(cfg.Servers, mcp.ServerConfig{Name: serverName})
				idx = len(cfg.Servers) - 1
			}
			for _, f := range sm.Fields() {
				if !f.IsDirty() {
					continue
				}
				if f.Key() == "command" {
					// command field is displayed as comma-separated text; split back
					val := f.Get()
					if val == "" {
						cfg.Servers[idx].Command = nil
					} else {
						cfg.Servers[idx].Command = strings.Split(val, ", ")
					}
					continue
				}
				if err := setStructField(&cfg.Servers[idx], f.Key(), f.Get()); err != nil {
					return fmt.Errorf("set servers.%s.%s: %w", serverName, f.Key(), err)
				}
			}
		}
	} else {
		for _, f := range sm.Fields() {
			if f.IsDirty() {
				if err := setStructField(cfg, f.Key(), f.Get()); err != nil {
					return fmt.Errorf("set %s: %w", f.Key(), err)
				}
			}
		}
	}
	return WriteConfigFile(ConfigFilePath("mcp_servers.json5"), cfg)
}

// --- agents.json5 ---

// agentsFile is the JSON structure written to agents.json5.
type agentsFile struct {
	Agents []config.AgentDefinition `json:"agents"`
}

func saveAgentsConfig(sm *SectionModel) error {
	agents, err := loadAgentsConfig(nil)
	if err != nil {
		return fmt.Errorf("load agents config: %w", err)
	}

	if sm.IsDrilldown() {
		// Drilldown sub-section: prefix is like "agents.coder"
		// Find the agent by ID (the DrilldownItem.Name) and apply field changes.
		parts := strings.SplitN(sm.DrilldownPrefix(), ".", 2)
		if len(parts) == 2 && parts[0] == "agents" {
			agentID := parts[1]
			agent, ok := agents[agentID]
			if !ok {
				// New agent: create it
				agent = &config.AgentDefinition{ID: agentID}
			}
			for _, f := range sm.Fields() {
				if !f.IsDirty() {
					continue
				}
				if err := setStructField(agent, f.Key(), f.Get()); err != nil {
					return fmt.Errorf("set agents.%s.%s: %w", agentID, f.Key(), err)
				}
			}
			agents[agentID] = agent
		}
	}

	// Convert map to a deterministically ordered slice for stable output.
	ids := make([]string, 0, len(agents))
	for id := range agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	agentList := make([]config.AgentDefinition, 0, len(ids))
	for _, id := range ids {
		agentList = append(agentList, *agents[id])
	}
	return WriteConfigFile(ConfigFilePath("agents.json5"), &agentsFile{Agents: agentList})
}

// --- presets.json5 ---

func savePresetsConfig(sm *SectionModel) error {
	cfg, err := loadPresetsConfig()
	if err != nil {
		return fmt.Errorf("load presets config: %w", err)
	}

	if sm.IsDrilldown() {
		// Drilldown sub-section: prefix is like "presets.development"
		parts := strings.SplitN(sm.DrilldownPrefix(), ".", 2)
		if len(parts) == 2 && parts[0] == "presets" {
			presetName := parts[1]
			preset, ok := cfg.Presets[presetName]
			if !ok {
				// New preset: create it
				preset = &config.ModelPreset{}
			}
			for _, f := range sm.Fields() {
				if !f.IsDirty() {
					continue
				}
				if err := setStructField(preset, f.Key(), f.Get()); err != nil {
					return fmt.Errorf("set presets.%s.%s: %w", presetName, f.Key(), err)
				}
			}
			cfg.Presets[presetName] = preset
		}
	} else {
		for _, f := range sm.Fields() {
			if f.IsDirty() {
				if err := setStructField(cfg, f.Key(), f.Get()); err != nil {
					return fmt.Errorf("set %s: %w", f.Key(), err)
				}
			}
		}
	}
	return WriteConfigFile(ConfigFilePath("presets.json5"), cfg)
}
