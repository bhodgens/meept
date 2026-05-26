package builtin

import (
	"testing"
)

func TestValidateJSONSchema_TypeChecking(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		schema  map[string]any
		wantErr bool
	}{
		{
			name:  "string type valid",
			value: "hello",
			schema: map[string]any{
				"type": "string",
			},
		},
		{
			name:  "string type invalid",
			value: 42,
			schema: map[string]any{
				"type": "string",
			},
			wantErr: true,
		},
		{
			name:  "number type valid with float64",
			value: float64(3.14),
			schema: map[string]any{
				"type": "number",
			},
		},
		{
			name:  "number type valid with int",
			value: 42,
			schema: map[string]any{
				"type": "number",
			},
		},
		{
			name:  "number type invalid",
			value: "not a number",
			schema: map[string]any{
				"type": "number",
			},
			wantErr: true,
		},
		{
			name:  "boolean type valid",
			value: true,
			schema: map[string]any{
				"type": "boolean",
			},
		},
		{
			name:  "boolean type invalid",
			value: "true",
			schema: map[string]any{
				"type": "boolean",
			},
			wantErr: true,
		},
		{
			name:  "object type valid",
			value: map[string]any{"key": "value"},
			schema: map[string]any{
				"type": "object",
			},
		},
		{
			name:  "object type invalid",
			value: "not an object",
			schema: map[string]any{
				"type": "object",
			},
			wantErr: true,
		},
		{
			name:  "array type valid",
			value: []any{"a", "b"},
			schema: map[string]any{
				"type": "array",
			},
		},
		{
			name:  "array type invalid",
			value: "not an array",
			schema: map[string]any{
				"type": "array",
			},
			wantErr: true,
		},
		{
			name:  "integer type valid",
			value: float64(42),
			schema: map[string]any{
				"type": "integer",
			},
		},
		{
			name:  "integer type invalid - fractional",
			value: float64(3.14),
			schema: map[string]any{
				"type": "integer",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateJSONSchema(tt.value, tt.schema)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateJSONSchema() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateJSONSchema_RequiredFields(t *testing.T) {
	schema := map[string]any{
		"type":     "object",
		"required": []any{"name", "age"},
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"age":  map[string]any{"type": "integer"},
		},
	}

	t.Run("all required present", func(t *testing.T) {
		value := map[string]any{"name": "Alice", "age": float64(30)}
		if err := ValidateJSONSchema(value, schema); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("missing required field", func(t *testing.T) {
		value := map[string]any{"name": "Alice"}
		if err := ValidateJSONSchema(value, schema); err == nil {
			t.Error("expected error for missing required field")
		}
	})

	t.Run("all required missing", func(t *testing.T) {
		value := map[string]any{}
		if err := ValidateJSONSchema(value, schema); err == nil {
			t.Error("expected error for missing required fields")
		}
	})
}

func TestValidateJSONSchema_NestedProperties(t *testing.T) {
	schema := map[string]any{
		"type":     "object",
		"required": []any{"user"},
		"properties": map[string]any{
			"user": map[string]any{
				"type":     "object",
				"required": []any{"name", "email"},
				"properties": map[string]any{
					"name":  map[string]any{"type": "string"},
					"email": map[string]any{"type": "string"},
					"address": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"city": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
	}

	t.Run("valid nested object", func(t *testing.T) {
		value := map[string]any{
			"user": map[string]any{
				"name":  "Alice",
				"email": "alice@example.com",
			},
		}
		if err := ValidateJSONSchema(value, schema); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("nested missing required", func(t *testing.T) {
		value := map[string]any{
			"user": map[string]any{
				"name": "Alice",
			},
		}
		if err := ValidateJSONSchema(value, schema); err == nil {
			t.Error("expected error for missing nested required field")
		}
	})

	t.Run("nested wrong type", func(t *testing.T) {
		value := map[string]any{
			"user": map[string]any{
				"name":  float64(42),
				"email": "alice@example.com",
			},
		}
		if err := ValidateJSONSchema(value, schema); err == nil {
			t.Error("expected error for wrong type in nested property")
		}
	})

	t.Run("deep nested valid", func(t *testing.T) {
		value := map[string]any{
			"user": map[string]any{
				"name":  "Alice",
				"email": "alice@example.com",
				"address": map[string]any{
					"city": "Wonderland",
				},
			},
		}
		if err := ValidateJSONSchema(value, schema); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})
}

func TestValidateJSONSchema_ArrayItems(t *testing.T) {
	schema := map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "string",
		},
	}

	t.Run("valid string array", func(t *testing.T) {
		value := []any{"a", "b", "c"}
		if err := ValidateJSONSchema(value, schema); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("invalid item type in array", func(t *testing.T) {
		value := []any{"a", float64(42), "c"}
		if err := ValidateJSONSchema(value, schema); err == nil {
			t.Error("expected error for invalid array item type")
		}
	})

	t.Run("empty array valid", func(t *testing.T) {
		value := []any{}
		if err := ValidateJSONSchema(value, schema); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("array of objects", func(t *testing.T) {
		objSchema := map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":     "object",
				"required": []any{"id"},
				"properties": map[string]any{
					"id":   map[string]any{"type": "integer"},
					"name": map[string]any{"type": "string"},
				},
			},
		}
		value := []any{
			map[string]any{"id": float64(1), "name": "first"},
			map[string]any{"id": float64(2), "name": "second"},
		}
		if err := ValidateJSONSchema(value, objSchema); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})
}

func TestValidateJSONSchema_Enum(t *testing.T) {
	schema := map[string]any{
		"type": "string",
		"enum": []any{"red", "green", "blue"},
	}

	t.Run("valid enum value", func(t *testing.T) {
		if err := ValidateJSONSchema("red", schema); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("invalid enum value", func(t *testing.T) {
		if err := ValidateJSONSchema("yellow", schema); err == nil {
			t.Error("expected error for invalid enum value")
		}
	})

	t.Run("enum on object property", func(t *testing.T) {
		objSchema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status": map[string]any{
					"type": "string",
					"enum": []any{"active", "inactive", "pending"},
				},
			},
		}
		value := map[string]any{"status": "active"}
		if err := ValidateJSONSchema(value, objSchema); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}

		badValue := map[string]any{"status": "unknown"}
		if err := ValidateJSONSchema(badValue, objSchema); err == nil {
			t.Error("expected error for invalid enum value in property")
		}
	})
}

func TestValidateJSONSchema_NilValue(t *testing.T) {
	schema := map[string]any{"type": "string"}
	if err := ValidateJSONSchema(nil, schema); err == nil {
		t.Error("expected error for nil value with type constraint")
	}
}

func TestExtractJSONFromText_JSONCodeBlock(t *testing.T) {
	text := "Here is the result:\n```json\n{\"name\": \"Alice\", \"age\": 30}\n```\nThat's all."
	result, err := ExtractJSONFromText(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["name"] != "Alice" {
		t.Errorf("expected name=Alice, got %v", result["name"])
	}
	if result["age"] != float64(30) {
		t.Errorf("expected age=30, got %v", result["age"])
	}
}

func TestExtractJSONFromText_RawJSON(t *testing.T) {
	text := `{"name": "Bob", "age": 25}`
	result, err := ExtractJSONFromText(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["name"] != "Bob" {
		t.Errorf("expected name=Bob, got %v", result["name"])
	}
}

func TestExtractJSONFromText_CodeBlockWithoutLanguage(t *testing.T) {
	text := "```\n{\"name\": \"Charlie\"}\n```"
	result, err := ExtractJSONFromText(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["name"] != "Charlie" {
		t.Errorf("expected name=Charlie, got %v", result["name"])
	}
}

func TestExtractJSONFromText_NoJSON(t *testing.T) {
	text := "This is just plain text with no JSON at all."
	_, err := ExtractJSONFromText(text)
	if err == nil {
		t.Error("expected error for text with no JSON")
	}
}

func TestExtractJSONFromText_WhitespacePaddedJSON(t *testing.T) {
	text := "  \n  {\"key\": \"value\"}  \n  "
	result, err := ExtractJSONFromText(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["key"] != "value" {
		t.Errorf("expected key=value, got %v", result["key"])
	}
}
