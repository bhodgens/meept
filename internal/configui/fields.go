// internal/configui/fields.go
package configui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// FieldType identifies the kind of editor a field uses.
type FieldType int

const (
	FieldText       FieldType = iota
	FieldToggle
	FieldSelect
	FieldMultiSelect
	FieldMasked
	FieldNumber
	FieldDrilldown // opens a sub-screen (list of structs)
)

// Field is the interface for all editable config fields.
type Field interface {
	Key() string
	Label() string
	Get() string            // current value as string
	Set(string) error       // set value from string; may validate
	Display() string        // value shown in the field list (may mask)
	IsDirty() bool          // value changed since load?
	Reset()                 // restore original value
	Type() FieldType
	Help() string
	SetHelp(string)
}

// baseField holds shared state for all field types.
type baseField struct {
	key     string
	label   string
	orig    string
	current string
	dirty   bool
	help    string
}

func (b *baseField) Key() string      { return b.key }
func (b *baseField) Label() string    { return b.label }
func (b *baseField) Get() string      { return b.current }
func (b *baseField) Display() string  { return b.current }
func (b *baseField) IsDirty() bool    { return b.dirty }
func (b *baseField) Type() FieldType  { return FieldText }
func (b *baseField) Help() string     { return b.help }
func (b *baseField) SetHelp(h string) { b.help = h }

// --- TextField ---

type TextField struct{ baseField }

func NewTextField(key, label, value string) *TextField {
	return &TextField{baseField{key: key, label: label, orig: value, current: value}}
}

func (f *TextField) Set(v string) error {
	f.current = v
	f.dirty = f.current != f.orig
	return nil
}

func (f *TextField) Reset() {
	f.current = f.orig
	f.dirty = false
}

// --- ToggleField ---

type ToggleField struct{ baseField }

func NewToggleField(key, label string, value bool) *ToggleField {
	s := formatBool(value)
	return &ToggleField{baseField{key: key, label: label, orig: s, current: s}}
}

func (f *ToggleField) Type() FieldType { return FieldToggle }

func (f *ToggleField) Set(v string) error {
	if v != "true" && v != "false" {
		return fmt.Errorf("toggle field %q: value must be \"true\" or \"false\"", f.key)
	}
	f.current = v
	f.dirty = f.current != f.orig
	return nil
}

func (f *ToggleField) Reset() {
	f.current = f.orig
	f.dirty = false
}

// --- SelectField ---

type SelectField struct {
	baseField
	Options []string
}

func NewSelectField(key, label, value string, options []string) *SelectField {
	return &SelectField{
		baseField: baseField{key: key, label: label, orig: value, current: value},
		Options:   options,
	}
}

func (f *SelectField) Type() FieldType { return FieldSelect }

func (f *SelectField) Set(v string) error {
	for _, o := range f.Options {
		if o == v {
			f.current = v
			f.dirty = f.current != f.orig
			return nil
		}
	}
	return fmt.Errorf("select field %q: %q is not a valid option", f.key, v)
}

func (f *SelectField) Reset() {
	f.current = f.orig
	f.dirty = false
}

// --- MultiSelectField ---

type MultiSelectField struct {
	baseField
	Options []string
}

func NewMultiSelectField(key, label string, selected []string, options []string) *MultiSelectField {
	orig := strings.Join(selected, ",")
	return &MultiSelectField{
		baseField: baseField{key: key, label: label, orig: orig, current: orig},
		Options:   options,
	}
}

func (f *MultiSelectField) Type() FieldType { return FieldMultiSelect }

func (f *MultiSelectField) GetStrings() []string {
	if f.current == "" {
		return nil
	}
	return strings.Split(f.current, ",")
}

func (f *MultiSelectField) SetStrings(selected []string) {
	f.current = strings.Join(selected, ",")
	f.dirty = f.current != f.orig
}

func (f *MultiSelectField) Set(v string) error {
	f.current = v
	f.dirty = f.current != f.orig
	return nil
}

func (f *MultiSelectField) Display() string {
	vals := f.GetStrings()
	if len(vals) == 0 {
		return "[]"
	}
	return "[" + strings.Join(vals, ", ") + "]"
}

func (f *MultiSelectField) Reset() {
	f.current = f.orig
	f.dirty = false
}

// --- MaskedField ---

type MaskedField struct{ baseField }

func NewMaskedField(key, label, value string) *MaskedField {
	return &MaskedField{baseField{key: key, label: label, orig: value, current: value}}
}

func (f *MaskedField) Type() FieldType { return FieldMasked }

func (f *MaskedField) Display() string {
	if f.current == "" {
		return "(not set)"
	}
	return strings.Repeat("\u2022", len(f.current))
}

func (f *MaskedField) Set(v string) error {
	f.current = v
	f.dirty = f.current != f.orig
	return nil
}

func (f *MaskedField) Reset() {
	f.current = f.orig
	f.dirty = false
}

// --- NumberField ---

type NumberField struct{ baseField }

func NewNumberField(key, label string, value int) *NumberField {
	s := strconv.Itoa(value)
	return &NumberField{baseField{key: key, label: label, orig: s, current: s}}
}

func (f *NumberField) Type() FieldType { return FieldNumber }

func (f *NumberField) Set(v string) error {
	if _, err := strconv.Atoi(v); err != nil {
		return fmt.Errorf("number field %q: %q is not a valid integer", f.key, v)
	}
	f.current = v
	f.dirty = f.current != f.orig
	return nil
}

func (f *NumberField) Reset() {
	f.current = f.orig
	f.dirty = false
}

// --- DrilldownField ---

type DrilldownField struct {
	baseField
	ItemCount int
}

func NewDrilldownField(key, label string, itemCount int) *DrilldownField {
	return &DrilldownField{
		baseField: baseField{key: key, label: label},
		ItemCount: itemCount,
	}
}

func (f *DrilldownField) Type() FieldType { return FieldDrilldown }

func (f *DrilldownField) Display() string {
	return fmt.Sprintf("[%d items]", f.ItemCount)
}

func (f *DrilldownField) Set(v string) error {
	return errors.New("drilldown fields cannot be set directly")
}

func (f *DrilldownField) Reset() {}

// --- helpers ---

func formatBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
