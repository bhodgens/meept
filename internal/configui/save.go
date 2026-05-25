package configui

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tui"
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
func saveMainConfigSection(sm *SectionModel) error {
	cfg, err := config.LoadDefault()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	sectionPrefix := sm.SectionKey()
	for _, f := range sm.Fields() {
		if f.IsDirty() {
			fullPath := sectionPrefix + "." + f.Key()
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
	cfg, err := tui.LoadClientConfig()
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
	cfg, err := llm.LoadProvidersConfigDefault()
	if err != nil {
		return fmt.Errorf("load models config: %w", err)
	}
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
	return WriteConfigFile(ConfigFilePath("models.json5"), cfg)
}

// --- mcp_servers.json5 ---

func saveMCPServersConfig(sm *SectionModel) error {
	cfg, err := config.LoadMCPConfigDefault()
	if err != nil {
		return fmt.Errorf("load mcp servers config: %w", err)
	}
	for _, f := range sm.Fields() {
		if f.IsDirty() {
			if err := setStructField(cfg, f.Key(), f.Get()); err != nil {
				return fmt.Errorf("set %s: %w", f.Key(), err)
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
	agents, err := config.LoadAgentDefinitionsDefault(nil)
	if err != nil {
		return fmt.Errorf("load agents config: %w", err)
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
	cfg, err := config.LoadPresetsConfigDefault()
	if err != nil {
		return fmt.Errorf("load presets config: %w", err)
	}
	for _, f := range sm.Fields() {
		if f.IsDirty() {
			if err := setStructField(cfg, f.Key(), f.Get()); err != nil {
				return fmt.Errorf("set %s: %w", f.Key(), err)
			}
		}
	}
	return WriteConfigFile(ConfigFilePath("presets.json5"), cfg)
}
