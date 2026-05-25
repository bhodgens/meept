// internal/configui/sections_presets.go
package configui

import (
	"sort"

	"github.com/caimlas/meept/internal/config"
)

func buildPresetsFields() []Field {
	pc, _ := config.LoadPresetsConfigDefault()
	var items []DrilldownItem
	if pc != nil && pc.Presets != nil {
		items = buildPresetItems(pc.Presets)
	}
	return []Field{
		NewDrilldownField("presets", "presets", items),
	}
}

func buildPresetItems(presets map[string]*config.ModelPreset) []DrilldownItem {
	names := make([]string, 0, len(presets))
	for name := range presets {
		names = append(names, name)
	}
	sort.Strings(names)

	items := make([]DrilldownItem, 0, len(names))
	for _, name := range names {
		p := presets[name]
		if p == nil {
			continue
		}
		fields := []Field{
			NewTextField("label", "label", p.Label),
			NewTextField("description", "description", p.Description),
			NewFloatField("params.temperature", "temperature", p.Params.Temperature),
			NewFloatField("params.top_p", "top p", p.Params.TopP),
		}
		items = append(items, DrilldownItem{Name: name, Fields: fields})
	}
	return items
}
