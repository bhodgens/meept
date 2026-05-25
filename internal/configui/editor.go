// internal/configui/editor.go
package configui

import "fmt"

// FieldEditor handles inline editing of a single Field value.
// It stores the original value at creation time so Cancel can revert.
type FieldEditor struct {
	field            Field
	input            string
	selectIdx        int
	multiSelect      map[int]bool
	multiSelectCursor int
	origValue        string
}

// NewFieldEditor creates a FieldEditor initialized from the field's current value.
func NewFieldEditor(f Field) *FieldEditor {
	ed := &FieldEditor{
		field:     f,
		origValue: f.Get(),
	}

	switch f.Type() {
	case FieldSelect:
		sf := f.(*SelectField)
		ed.selectIdx = findStringIndex(sf.Options, sf.Get())
		ed.input = sf.Get()

	case FieldMultiSelect:
		msf := f.(*MultiSelectField)
		ed.multiSelect = buildMultiSelectState(msf.GetStrings(), msf.Options)
		ed.multiSelectCursor = 0
		ed.input = msf.Get()

	default:
		ed.input = f.Get()
	}

	return ed
}

// Toggle flips the value of a ToggleField between true and false.
func (ed *FieldEditor) Toggle() {
	if ed.field.Type() != FieldToggle {
		return
	}
	cur := ed.field.Get()
	if cur == "true" {
		ed.field.Set("false")
	} else {
		ed.field.Set("true")
	}
}

// SelectUp moves the select cursor up one option.
func (ed *FieldEditor) SelectUp() {
	if ed.selectIdx > 0 {
		ed.selectIdx--
	}
}

// SelectDown moves the select cursor down one option.
func (ed *FieldEditor) SelectDown() {
	if ed.field.Type() != FieldSelect {
		return
	}
	sf := ed.field.(*SelectField)
	if ed.selectIdx < len(sf.Options)-1 {
		ed.selectIdx++
	}
}

// ConfirmSelect applies the currently selected option to the field.
func (ed *FieldEditor) ConfirmSelect() {
	if ed.field.Type() != FieldSelect {
		return
	}
	sf := ed.field.(*SelectField)
	if ed.selectIdx >= 0 && ed.selectIdx < len(sf.Options) {
		ed.field.Set(sf.Options[ed.selectIdx])
	}
}

// ToggleMultiSelectOption toggles the selection state of option at index i
// and updates the field via SetStrings.
func (ed *FieldEditor) ToggleMultiSelectOption(i int) {
	if ed.field.Type() != FieldMultiSelect {
		return
	}
	msf := ed.field.(*MultiSelectField)
	if i < 0 || i >= len(msf.Options) {
		return
	}
	ed.multiSelect[i] = !ed.multiSelect[i]

	// Build the new selected list preserving option order.
	var selected []string
	for idx, opt := range msf.Options {
		if ed.multiSelect[idx] {
			selected = append(selected, opt)
		}
	}
	msf.SetStrings(selected)
}

// SetInput sets the text buffer for Text, Number, and Masked fields.
func (ed *FieldEditor) SetInput(v string) error {
	switch ed.field.Type() {
	case FieldText, FieldNumber, FieldFloat, FieldMasked:
		ed.input = v
		return nil
	default:
		return fmt.Errorf("SetInput not supported for field type %v", ed.field.Type())
	}
}

// ConfirmInput applies the current input buffer to the field via Set().
func (ed *FieldEditor) ConfirmInput() error {
	return ed.field.Set(ed.input)
}

// Cancel reverts the field to its original value.
func (ed *FieldEditor) Cancel() error {
	return ed.field.Set(ed.origValue)
}

// SelectCursor returns the current select cursor index.
func (ed *FieldEditor) SelectCursor() int {
	return ed.selectIdx
}

// InputValue returns the current input buffer.
func (ed *FieldEditor) InputValue() string {
	return ed.input
}

// MultiSelectState returns a copy of the current multi-select toggle states.
func (ed *FieldEditor) MultiSelectState() map[int]bool {
	out := make(map[int]bool, len(ed.multiSelect))
	for k, v := range ed.multiSelect {
		out[k] = v
	}
	return out
}

// MultiSelectCursor returns the current multi-select cursor index.
func (ed *FieldEditor) MultiSelectCursor() int {
	return ed.multiSelectCursor
}

// MultiSelectUp moves the multi-select cursor up one option.
func (ed *FieldEditor) MultiSelectUp() {
	if ed.multiSelectCursor > 0 {
		ed.multiSelectCursor--
	}
}

// MultiSelectDown moves the multi-select cursor down one option.
func (ed *FieldEditor) MultiSelectDown() {
	if ed.field.Type() != FieldMultiSelect {
		return
	}
	msf := ed.field.(*MultiSelectField)
	if ed.multiSelectCursor < len(msf.Options)-1 {
		ed.multiSelectCursor++
	}
}

// --- helpers ---

func findStringIndex(slice []string, target string) int {
	for i, s := range slice {
		if s == target {
			return i
		}
	}
	return 0
}

func buildMultiSelectState(selected []string, options []string) map[int]bool {
	active := make(map[int]bool, len(options))
	for i := range options {
		active[i] = false
	}
	selectedSet := make(map[string]bool, len(selected))
	for _, s := range selected {
		selectedSet[s] = true
	}
	for i, opt := range options {
		if selectedSet[opt] {
			active[i] = true
		}
	}
	return active
}
