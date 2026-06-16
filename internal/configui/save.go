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
	loadMainConfig      = config.LoadDefault
	loadClientConfig    = tui.LoadClientConfig
	loadProvidersConfig = llm.LoadProvidersConfigDefault
	loadMCPConfig       = config.LoadMCPConfigDefault
	loadAgentsConfig    = config.LoadAgentDefinitionsDefault
	loadPresetsConfig   = config.LoadPresetsConfigDefault
)

// SaveSection writes modified fields back to the correct config file.
// For the "oauth" section, save is a no-op because actions (connect/disconnect)
// execute immediately when activated and don't produce dirty fields.
func SaveSection(sm *SectionModel) error {
	switch sm.ConfigFile() {
	case "oauth":
		return nil // actions are immediate; no config file to persist
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
// For string-slice drilldowns (e.g. security.allowed_paths), it reconstructs
// the full []string from all drilldown item field values.
func saveMainConfigSection(sm *SectionModel) error {
	cfg, err := loadMainConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if sm.IsStringSliceDrilldown() {
		// Reconstruct the []string from all drilldown items.
		// Each item has exactly one text field with the string value.
		sliceKeypath := sm.StringSliceKeypath()
		items := sm.StringSliceItems()
		slice := make([]string, 0, len(items))
		for _, item := range items {
			if len(item.Fields) > 0 {
				slice = append(slice, item.Fields[0].Get())
			}
		}
		if err := SetKeypath(cfg, sliceKeypath, slice); err != nil {
			return fmt.Errorf("set %s: %w", sliceKeypath, err)
		}
	} else if sm.IsDrilldown() {
		prefix := sm.DrilldownPrefix()
		// Try to save as a map-key drilldown (e.g., lsp.servers.golang)
		// If the prefix maps to a map[string]Struct entry, apply fields to that struct.
		// Prepend section key to form full config path for map resolution.
		fullMapPrefix := sm.SectionKey() + "." + prefix
		if err := applyMapDrilldownFields(cfg, fullMapPrefix, sm.Fields()); err == nil {
			// Successfully handled as map drilldown
		} else {
			// Fall back to standard keypath resolution
			sectionPrefix := sm.SectionKey()
			for _, f := range sm.Fields() {
				if f.IsDirty() {
					fullPath := resolveFullPath(sectionPrefix, f.Key())
					if err := SetKeypath(cfg, fullPath, f.Get()); err != nil {
						return fmt.Errorf("set %s: %w", fullPath, err)
					}
				}
			}
		}
	} else {
		prefix := sm.SectionKey()
		for _, f := range sm.Fields() {
			if f.IsDirty() {
				fullPath := resolveFullPath(prefix, f.Key())
				if err := SetKeypath(cfg, fullPath, f.Get()); err != nil {
					return fmt.Errorf("set %s: %w", fullPath, err)
				}
			}
		}
	}
	return SaveMainConfig(cfg)
}

// applyMapDrilldownFields attempts to apply field changes to a map[string]Struct
// entry identified by the drilldown prefix. The prefix format is "mapPath.mapKey"
// where mapPath resolves to a map[string]T and mapKey is the key within that map.
// Returns an error if the prefix doesn't map to a map entry.
func applyMapDrilldownFields(cfg *config.Config, prefix string, fields []Field) error {
	// Split prefix into "parentPath" and "key" — the last segment is the map key
	lastDot := strings.LastIndex(prefix, ".")
	if lastDot < 0 {
		return fmt.Errorf("prefix %q has no dot separator", prefix)
	}
	mapPath := prefix[:lastDot]
	mapKey := prefix[lastDot+1:]

	// Resolve the map parent
	v, err := resolvePath(reflect.ValueOf(cfg), strings.Split(mapPath, "."))
	if err != nil {
		return err
	}
	for v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Map {
		return fmt.Errorf("path %q is not a map (got %s)", mapPath, v.Kind())
	}

	// Get or create the map entry
	elemType := v.Type().Elem()
	entry := v.MapIndex(reflect.ValueOf(mapKey))
	var entryVal reflect.Value
	if entry.IsValid() {
		if entry.Kind() == reflect.Ptr {
			entryVal = entry.Elem()
		} else {
			// Map returns unaddressable values for struct-valued maps.
			// Make an addressable copy so we can modify fields.
			entryVal = reflect.New(elemType).Elem()
			entryVal.Set(entry)
		}
	} else {
		entryVal = reflect.New(elemType).Elem()
	}

	// Apply field changes to the entry struct
	for _, f := range fields {
		if !f.IsDirty() {
			continue
		}
		fieldName := f.Key()
		found := false
		for i := 0; i < entryVal.NumField(); i++ {
			field := entryVal.Type().Field(i)
			tag := field.Tag.Get("json")
			tagName := strings.Split(tag, ",")[0]
			if tagName == fieldName {
				fv := entryVal.Field(i)
				switch fv.Kind() {
				case reflect.String:
					fv.SetString(f.Get())
				case reflect.Bool:
					b, _ := strconv.ParseBool(f.Get())
					fv.SetBool(b)
				case reflect.Int, reflect.Int64:
					n, _ := strconv.Atoi(f.Get())
					fv.SetInt(int64(n))
				case reflect.Float64:
					fl, _ := strconv.ParseFloat(f.Get(), 64)
					fv.SetFloat(fl)
				case reflect.Slice:
					if fv.Type().Elem().Kind() == reflect.String {
						val := f.Get()
						if val == "" {
							fv.Set(reflect.MakeSlice(fv.Type(), 0, 0))
						} else {
							fv.Set(reflect.ValueOf(strings.Split(val, ", ")))
						}
					}
				}
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("field %q not found in map entry type", fieldName)
		}
	}

	// Write the modified entry back to the map
	if elemType.Kind() == reflect.Ptr {
		v.SetMapIndex(reflect.ValueOf(mapKey), entryVal.Addr())
	} else {
		v.SetMapIndex(reflect.ValueOf(mapKey), entryVal)
	}
	return nil
}

// resolveFullPath constructs the full keypath for a field. If the field key
// already starts with the section prefix (e.g. "llm.budget.limit" for section
// "llm"), the key is used as-is. Otherwise the prefix is prepended.
func resolveFullPath(prefix, fieldKey string) string {
	if strings.HasPrefix(fieldKey, prefix+".") {
		return fieldKey
	}
	return prefix + "." + fieldKey
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

	if sm.IsMapStringStringDrilldown() {
		// Rebuild the map[string]string from all drilldown items.
		// Each item.Name is the map key, and the single text field holds the value.
		mapKey := sm.MapStringStringKey()
		m, ok := resolveMapStringString(cfg, mapKey)
		if !ok {
			return fmt.Errorf("path %q does not resolve to a map[string]string", mapKey)
		}
		allItems := sm.MapStringStringItems()
		for _, item := range allItems {
			if len(item.Fields) > 0 {
				m[item.Name] = item.Fields[0].Get()
			}
		}
	} else if sm.IsDrilldown() {
		prefix := sm.DrilldownPrefix()
		// Standard struct field drilldown
		for _, f := range sm.Fields() {
			if f.IsDirty() {
				if err := setStructField(cfg, prefix+"."+f.Key(), f.Get()); err != nil {
					return fmt.Errorf("set %s.%s: %w", prefix, f.Key(), err)
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
	return WriteConfigFile(ConfigFilePath("client.json5"), cfg)
}

// resolveMapStringString resolves a dot-notation path on a struct to a
// map[string]string field. Returns the map and true if the path resolves to
// a map[string]string, or nil and false otherwise.
func resolveMapStringString(target any, path string) (map[string]string, bool) {
	v := reflect.ValueOf(target)
	parts := strings.Split(path, ".")
	fieldName := parts[len(parts)-1]
	parentParts := parts[:len(parts)-1]

	parent, err := resolvePath(v, parentParts)
	if err != nil {
		return nil, false
	}
	if parent.Kind() == reflect.Ptr {
		parent = parent.Elem()
	}
	for i := 0; i < parent.NumField(); i++ {
		field := parent.Type().Field(i)
		tagName := strings.Split(field.Tag.Get("json"), ",")[0]
		if tagName == fieldName {
			fv := parent.Field(i)
			if fv.Kind() == reflect.Map && fv.Type().Key().Kind() == reflect.String && fv.Type().Elem().Kind() == reflect.String {
				if fv.IsNil() {
					fv.Set(reflect.MakeMap(fv.Type()))
				}
				return fv.Interface().(map[string]string), true
			}
			return nil, false
		}
	}
	return nil, false
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
	} else if sm.IsMapStringStringDrilldown() {
		// map[string]string drilldown for server env/headers.
		// The section key is "mcp_servers", the map key path is like "servers.my_server.env"
		// and we need to resolve it on the loaded config.
		mapKey := sm.MapStringStringKey()
		parts := strings.SplitN(mapKey, ".", 3)
		if len(parts) >= 3 && parts[0] == "servers" {
			serverName := parts[1]
			fieldName := parts[2] // "env" or "headers"
			idx := -1
			for i, s := range cfg.Servers {
				if s.Name == serverName {
					idx = i
					break
				}
			}
			if idx == -1 {
				return fmt.Errorf("server %q not found for map drilldown %q", serverName, mapKey)
			}
			// Rebuild the map from all drilldown items
			allItems := sm.MapStringStringItems()
			m := make(map[string]string, len(allItems))
			for _, item := range allItems {
				if len(item.Fields) > 0 {
					m[item.Name] = item.Fields[0].Get()
				}
			}
			switch fieldName {
			case "env":
				cfg.Servers[idx].Env = m
			case "headers":
				cfg.Servers[idx].Headers = m
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
				switch f.Key() {
				case "additional_tools":
					val := f.Get()
					if val == "" {
						agent.AdditionalTools = nil
					} else {
						agent.AdditionalTools = strings.Split(val, ", ")
					}
				case "capabilities":
					val := f.Get()
					if val == "" {
						agent.Capabilities = nil
					} else {
						agent.Capabilities = strings.Split(val, ", ")
					}
				case "prompt_components":
					val := f.Get()
					if val == "" {
						agent.PromptComponents = nil
					} else {
						agent.PromptComponents = strings.Split(val, ", ")
					}
				default:
					if err := setStructField(agent, f.Key(), f.Get()); err != nil {
						return fmt.Errorf("set agents.%s.%s: %w", agentID, f.Key(), err)
					}
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
