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

// Title returns the display name of the section.
func (s *SectionModel) Title() string { return s.title }

// KeyPath returns the config file name this section edits.
func (s *SectionModel) KeyPath() string { return s.keyPath }

// ConfigFile returns the config file name this section edits (alias for KeyPath).
func (s *SectionModel) ConfigFile() string { return s.keyPath }

// SectionKey returns the section prefix used for keypath construction (e.g. "daemon").
func (s *SectionModel) SectionKey() string { return s.sectionKey }

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
