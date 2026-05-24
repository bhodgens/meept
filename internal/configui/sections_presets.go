// internal/configui/sections_presets.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildPresetsFields() []Field {
	pc, _ := config.LoadPresetsConfigDefault()
	count := 0
	if pc != nil && pc.Presets != nil {
		count = len(pc.Presets)
	}
	return []Field{
		NewDrilldownField("presets", "presets", count),
	}
}
