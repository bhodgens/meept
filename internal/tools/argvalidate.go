package tools

import (
	"fmt"
	"reflect"
	"strings"
)

// ArgValidationError reports that tool arguments failed validation against
// the tool's own declared schema (Tool.Parameters().Required) at the
// registry boundary — before the tool's Execute runs.
type ArgValidationError struct {
	Tool    string // tool name
	Arg     string // schema name of the offending argument
	Problem string // "missing" | "empty" | "wrong type: want string, got number"
}

// Error returns a message that names the argument, the problem, and the one
// action that unblocks the call (pass the argument exactly as the schema
// names it). The e2e offender this gate targets repeated an identical bad
// call because nothing structural said WHY it failed.
func (e *ArgValidationError) Error() string {
	return fmt.Sprintf("%s: %s is %s (required by the tool schema; pass it exactly as named)",
		e.Tool, e.Arg, e.Problem)
}

// ValidateToolArgs validates args against the tool's declared schema: every
// name in Required must be present, non-nil (including typed nils), and —
// for strings — non-empty after TrimSpace; and every present required value
// must match the declared property type. Only required args are type-checked:
// optional args stay the tool's own business (forward/backward compatibility).
//
// This is the single boundary the registry enforces; tools may keep their own
// hand-rolled checks as defense-in-depth but the schema is now binding.
func ValidateToolArgs(tool Tool, args map[string]any) error {
	if tool == nil {
		return nil
	}
	params := tool.Parameters()
	if len(params.Required) == 0 {
		return nil
	}

	props := params.Properties
	for _, name := range params.Required {
		val, present := args[name]

		// Declared type; an undeclared required name is still enforced
		// (schema-drift defense) but has no type to check against.
		var wantType string
		if prop, ok := props[name]; ok {
			wantType = prop.Type
		}

		if !present || isMissingValue(val) {
			return &ArgValidationError{Tool: tool.Name(), Arg: name, Problem: "missing"}
		}
		// Type check first: a non-string for a string prop is a wrong
		// type, not an "empty" string.
		if wantType != "" {
			if problem := typeMismatch(wantType, val); problem != "" {
				return &ArgValidationError{Tool: tool.Name(), Arg: name, Problem: problem}
			}
		}
		if wantType == "string" {
			s, _ := val.(string)
			if strings.TrimSpace(s) == "" {
				return &ArgValidationError{Tool: tool.Name(), Arg: name, Problem: "empty"}
			}
		}
	}
	return nil
}

// isMissingValue reports whether val counts as absent for a required
// argument: a bare nil, or a typed nil (interface holding a nil pointer /
// nil map / nil slice / etc.) — the val == nil check alone does not catch
// those, the same trap that panicked json_extract (2026-09-10). Basic kinds
// (string, number, bool) can never be typed-nil, so they skip reflect.
func isMissingValue(val any) bool {
	if val == nil {
		return true
	}
	rv := reflect.ValueOf(val)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface,
		reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return rv.IsNil()
	default:
		return false
	}
}

// typeMismatch returns a Problem string when val does not match the declared
// schema type, or "" when it matches (or the type is unknown/relaxed).
// JSON decoding produces float64 for numbers, []any for arrays, and
// map[string]any for objects; integer is folded into number.
func typeMismatch(wantType string, val any) string {
	switch wantType {
	case "string":
		if _, ok := val.(string); !ok {
			return "wrong type: want string, got " + jsonTypeName(val)
		}
	case "number", "integer":
		if _, ok := val.(float64); !ok {
			return "wrong type: want " + wantType + ", got " + jsonTypeName(val)
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			return "wrong type: want boolean, got " + jsonTypeName(val)
		}
	case "array":
		if _, ok := val.([]any); !ok {
			return "wrong type: want array, got " + jsonTypeName(val)
		}
	case "object":
		if _, ok := val.(map[string]any); !ok {
			return "wrong type: want object, got " + jsonTypeName(val)
		}
	}
	// Unknown declared types (or enum-only props) are not enforced here:
	// the schema contract this gate owns is required+type.
	return ""
}

// jsonTypeName names val in JSON type vocabulary for Problem strings.
func jsonTypeName(val any) string {
	switch val.(type) {
	case string:
		return "string"
	case float64, int, int64:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%T", val)
	}
}
