package configui

import (
	"fmt"

	"github.com/caimlas/meept/internal/config"
)

// SaveSection writes modified fields back to the correct config file.
// It uses the SectionModel's KeyPath (which stores the config file name)
// to determine which file to save.
func SaveSection(sm *SectionModel) error {
	switch sm.KeyPath() {
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
	for _, f := range sm.Fields() {
		if f.IsDirty() {
			if err := SetKeypath(cfg, f.Key(), f.Get()); err != nil {
				return fmt.Errorf("set %s: %w", f.Key(), err)
			}
		}
	}
	return SaveMainConfig(cfg)
}

func saveClientConfig(sm *SectionModel) error {
	return fmt.Errorf("save not yet implemented for %s", sm.KeyPath())
}

func saveModelsConfig(sm *SectionModel) error {
	return fmt.Errorf("save not yet implemented for %s", sm.KeyPath())
}

func saveMCPServersConfig(sm *SectionModel) error {
	return fmt.Errorf("save not yet implemented for %s", sm.KeyPath())
}

func saveAgentsConfig(sm *SectionModel) error {
	return fmt.Errorf("save not yet implemented for %s", sm.KeyPath())
}

func savePresetsConfig(sm *SectionModel) error {
	return fmt.Errorf("save not yet implemented for %s", sm.KeyPath())
}
