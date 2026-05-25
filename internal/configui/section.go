// internal/configui/section.go
package configui

// SectionModel manages a scrollable list of config fields for one section.
// It is a pure data model used by the App, not a bubbletea tea.Model.
type SectionModel struct {
	title      string
	sectionKey string // e.g. "daemon", "transport", "llm"
	keyPath    string // config file name, e.g. "meept.json5"
	fields     []Field
	cursor     int

	// drilldownPrefix is set when this SectionModel represents a sub-section
	// created from a DrilldownItem. It holds the config keypath prefix that
	// should be used instead of sectionKey when persisting field changes.
	// For example, for provider "openai" in the models section, this would be
	// "providers.openai", so that field "api" becomes "providers.openai.api".
	// Empty string means this is a top-level section (normal save flow).
	drilldownPrefix string

	// stringSliceKeypath is set when this drilldown sub-section was created
	// from a []string drilldown. It holds the config keypath where the full
	// []string should be written (e.g. "security.allowed_paths").
	// When non-empty, stringSliceItems holds all DrilldownItems so the slice
	// can be reconstructed from their field values during save.
	stringSliceKeypath string
	stringSliceItems   []DrilldownItem

	// mapStringStringKey is set when this drilldown sub-section was created
	// from a map[string]string drilldown. It holds the config keypath where
	// the full map should be written (e.g. "vim.normal"). When non-empty,
	// mapStringStringItems holds all DrilldownItems so the map can be
	// reconstructed from their Name (map key) and field values during save.
	mapStringStringKey   string
	mapStringStringItems []DrilldownItem
}

// NewSectionModel creates a SectionModel with the given title, section key
// (e.g. "daemon"), config file name, and field slice. The cursor starts at 0.
func NewSectionModel(title, sectionKey, configFile string, fields []Field) *SectionModel {
	return &SectionModel{
		title:      title,
		sectionKey: sectionKey,
		keyPath:    configFile,
		fields:     fields,
		cursor:     0,
	}
}

// NewDrilldownSectionModel creates a SectionModel for a drilldown sub-section.
// The drilldownPrefix is the config keypath prefix (e.g. "providers.openai")
// used to persist field changes back to the correct nested location.
func NewDrilldownSectionModel(title, sectionKey, configFile, drilldownPrefix string, fields []Field) *SectionModel {
	return &SectionModel{
		title:           title,
		sectionKey:      sectionKey,
		keyPath:         configFile,
		fields:          fields,
		cursor:          0,
		drilldownPrefix: drilldownPrefix,
	}
}

// NewStringSliceDrilldownSectionModel creates a SectionModel for a drilldown
// sub-section that originated from a []string. The sliceKeypath is the full
// config keypath (e.g. "security.allowed_paths") where the reconstructed slice
// will be written. allItems contains all DrilldownItems so the full slice can
// be rebuilt from their field values.
func NewStringSliceDrilldownSectionModel(title, sectionKey, configFile, drilldownPrefix, sliceKeypath string, fields []Field, allItems []DrilldownItem) *SectionModel {
	return &SectionModel{
		title:              title,
		sectionKey:         sectionKey,
		keyPath:            configFile,
		fields:             fields,
		cursor:             0,
		drilldownPrefix:    drilldownPrefix,
		stringSliceKeypath: sliceKeypath,
		stringSliceItems:   allItems,
	}
}

// NewMapStringStringDrilldownSectionModel creates a SectionModel for a
// drilldown sub-section that originated from a map[string]string. The
// mapKeypath is the config keypath (e.g. "vim.normal") where the reconstructed
// map will be written. allItems contains all DrilldownItems so the full map
// can be rebuilt from their Names (map keys) and field values.
func NewMapStringStringDrilldownSectionModel(title, sectionKey, configFile, drilldownPrefix, mapKeypath string, fields []Field, allItems []DrilldownItem) *SectionModel {
	return &SectionModel{
		title:                title,
		sectionKey:           sectionKey,
		keyPath:              configFile,
		fields:               fields,
		cursor:               0,
		drilldownPrefix:      drilldownPrefix,
		mapStringStringKey:   mapKeypath,
		mapStringStringItems: allItems,
	}
}

// Title returns the display name of the section.
func (s *SectionModel) Title() string { return s.title }

// KeyPath returns the config file name this section edits.
func (s *SectionModel) KeyPath() string { return s.keyPath }

// ConfigFile returns the config file name this section edits (alias for KeyPath).
func (s *SectionModel) ConfigFile() string { return s.keyPath }

// SectionKey returns the section prefix used for keypath construction (e.g. "daemon").
func (s *SectionModel) SectionKey() string { return s.sectionKey }

// DrilldownPrefix returns the config keypath prefix for drilldown sub-sections.
// Returns empty string for top-level sections.
func (s *SectionModel) DrilldownPrefix() string { return s.drilldownPrefix }

// IsDrilldown returns true if this SectionModel represents a drilldown sub-section.
func (s *SectionModel) IsDrilldown() bool { return s.drilldownPrefix != "" }

// IsStringSliceDrilldown returns true if this drilldown originated from a []string.
func (s *SectionModel) IsStringSliceDrilldown() bool { return s.stringSliceKeypath != "" }

// StringSliceKeypath returns the config keypath where the reconstructed []string
// should be written. Empty for non-string-slice drilldowns.
func (s *SectionModel) StringSliceKeypath() string { return s.stringSliceKeypath }

// StringSliceItems returns all DrilldownItems for reconstructing the full []string.
func (s *SectionModel) StringSliceItems() []DrilldownItem { return s.stringSliceItems }

// IsMapStringStringDrilldown returns true if this drilldown originated from a map[string]string.
func (s *SectionModel) IsMapStringStringDrilldown() bool { return s.mapStringStringKey != "" }

// MapStringStringKey returns the config keypath where the reconstructed map[string]string
// should be written. Empty for non-map drilldowns.
func (s *SectionModel) MapStringStringKey() string { return s.mapStringStringKey }

// MapStringStringItems returns all DrilldownItems for reconstructing the full map[string]string.
func (s *SectionModel) MapStringStringItems() []DrilldownItem { return s.mapStringStringItems }

// Cursor returns the current cursor position (0-indexed).
func (s *SectionModel) Cursor() int { return s.cursor }

// FieldCount returns the number of fields in this section.
func (s *SectionModel) FieldCount() int { return len(s.fields) }

// Fields returns the slice of all fields in this section.
func (s *SectionModel) Fields() []Field { return s.fields }

// CurrentField returns the field at the current cursor position, or nil if
// the section has no fields.
func (s *SectionModel) CurrentField() Field {
	if len(s.fields) == 0 {
		return nil
	}
	return s.fields[s.cursor]
}

// MoveDown increments the cursor, clamping at the last field index.
func (s *SectionModel) MoveDown() {
	if len(s.fields) == 0 {
		return
	}
	if s.cursor < len(s.fields)-1 {
		s.cursor++
	}
}

// MoveUp decrements the cursor, clamping at 0.
func (s *SectionModel) MoveUp() {
	if s.cursor > 0 {
		s.cursor--
	}
}

// IsDirty returns true if any field in this section has been modified.
func (s *SectionModel) IsDirty() bool {
	for _, f := range s.fields {
		if f.IsDirty() {
			return true
		}
	}
	return false
}
