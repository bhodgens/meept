package builtin

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// ValidateJSONSchema performs basic JSON Schema validation.
// This is a simplified implementation covering: type, required, properties, items, enum.
// For full validation, use a dedicated library.
func ValidateJSONSchema(value any, schema map[string]any) error {
	// Check "type" constraint
	if typeVal, ok := schema["type"].(string); ok {
		if err := validateType(value, typeVal); err != nil {
			return err
		}
	}

	// Check "enum" constraint
	if enumVal, ok := schema["enum"]; ok {
		if err := validateEnum(value, enumVal); err != nil {
			return err
		}
	}

	// For objects: check "required" and "properties"
	if m, ok := value.(map[string]any); ok {
		if req, ok := schema["required"].([]any); ok {
			for _, r := range req {
				key, ok := r.(string)
				if !ok {
					continue
				}
				if _, exists := m[key]; !exists {
					return fmt.Errorf("missing required property: %s", key)
				}
			}
		}

		if props, ok := schema["properties"].(map[string]any); ok {
			for propName, propSchema := range props {
				propSchemaMap, ok := propSchema.(map[string]any)
				if !ok {
					continue
				}
				propValue, exists := m[propName]
				if !exists {
					// Only validate present properties; "required" handles missing ones
					continue
				}
				if err := ValidateJSONSchema(propValue, propSchemaMap); err != nil {
					return fmt.Errorf("property %q: %w", propName, err)
				}
			}
		}
	}

	// For arrays: check "items"
	if arr, ok := value.([]any); ok {
		if itemsSchema, ok := schema["items"].(map[string]any); ok {
			for i, item := range arr {
				if err := ValidateJSONSchema(item, itemsSchema); err != nil {
					return fmt.Errorf("item[%d]: %w", i, err)
				}
			}
		}
	}

	return nil
}

// validateType checks that a value matches the expected JSON Schema type.
func validateType(value any, expected string) error {
	if value == nil {
		// null is not a valid value for any of the standard types we check
		return fmt.Errorf("expected type %s, got null", expected)
	}

	switch expected {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected type string, got %s", reflect.TypeOf(value).Kind())
		}
	case "number":
		switch value.(type) {
		case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			// ok
		default:
			return fmt.Errorf("expected type number, got %s", reflect.TypeOf(value).Kind())
		}
	case "integer":
		switch value.(type) {
		case float64:
			// JSON numbers parsed via encoding/json are always float64;
			// accept float64 if it has no fractional part.
			if value.(float64) != float64(int64(value.(float64))) {
				return fmt.Errorf("expected type integer, got float with fractional part")
			}
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			// ok
		default:
			return fmt.Errorf("expected type integer, got %s", reflect.TypeOf(value).Kind())
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected type boolean, got %s", reflect.TypeOf(value).Kind())
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("expected type object, got %s", reflect.TypeOf(value).Kind())
		}
	case "array":
		if _, ok := value.([]any); !ok {
			return fmt.Errorf("expected type array, got %s", reflect.TypeOf(value).Kind())
		}
	default:
		// Unknown type, skip validation
	}

	return nil
}

// validateEnum checks that a value is one of the allowed enum values.
func validateEnum(value any, enumSpec any) error {
	enumValues, ok := enumSpec.([]any)
	if !ok {
		return nil
	}

	for _, allowed := range enumValues {
		if reflect.DeepEqual(value, allowed) {
			return nil
		}
	}

	// Build a readable list of allowed values
	strValues := make([]string, len(enumValues))
	for i, v := range enumValues {
		strValues[i] = fmt.Sprintf("%v", v)
	}
	return fmt.Errorf("value %v not in enum [%s]", value, strings.Join(strValues, ", "))
}

// ExtractJSONFromText attempts to extract a JSON object from text.
// It tries multiple strategies in order, continuing to the next if a
// strategy finds candidate content that fails to parse:
//  1. ```json ... ``` fenced code blocks
//  2. Any ``` ... ``` fenced code block (with optional language tag)
//  3. The entire text as raw JSON
//
// A strategy only "wins" if it both finds candidate content AND the
// content parses as valid JSON.
func ExtractJSONFromText(text string) (map[string]any, error) {
	var parseErrs []error

	// Strategy 1: extract from ```json code blocks.
	// Try every occurrence, not just the first.
	searchFrom := 0
	for {
		idx := strings.Index(text[searchFrom:], "```json")
		if idx == -1 {
			break
		}
		idx += searchFrom
		start := idx + len("```json")
		end := strings.Index(text[start:], "```")
		if end == -1 {
			searchFrom = start
			continue
		}
		jsonStr := strings.TrimSpace(text[start : start+end])
		searchFrom = start + end + 3
		var result map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
			parseErrs = append(parseErrs, fmt.Errorf("```json block: %w", err))
			continue
		}
		return result, nil
	}

	// Strategy 2: extract from any ```-delimited block.
	searchFrom = 0
	for {
		idx := strings.Index(text[searchFrom:], "```")
		if idx == -1 {
			break
		}
		idx += searchFrom
		start := idx + len("```")
		// Skip optional language tag on the same line.
		if nl := strings.Index(text[start:], "\n"); nl != -1 {
			start += nl + 1
		}
		end := strings.Index(text[start:], "```")
		if end == -1 {
			searchFrom = start
			continue
		}
		jsonStr := strings.TrimSpace(text[start : start+end])
		searchFrom = start + end + 3
		var result map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
			parseErrs = append(parseErrs, fmt.Errorf("``` block: %w", err))
			continue
		}
		return result, nil
	}

	// Strategy 3: parse the entire text as JSON.
	var result map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &result); err != nil {
		parseErrs = append(parseErrs, fmt.Errorf("raw text: %w", err))
		return nil, fmt.Errorf("no valid JSON found in text (tried %d strategies): %w", len(parseErrs), errors.Join(parseErrs...))
	}

	return result, nil
}
