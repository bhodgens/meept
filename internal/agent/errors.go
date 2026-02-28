package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ToolErrorCode represents the type of error that occurred during tool execution
type ToolErrorCode string

const (
	ErrCodeJSONSyntax      ToolErrorCode = "json_syntax"
	ErrCodeJSONStructure   ToolErrorCode = "json_structure"
	ErrCodeMissingRequired ToolErrorCode = "missing_required"
	ErrCodeInvalidType     ToolErrorCode = "invalid_type"
	ErrCodeInvalidValue    ToolErrorCode = "invalid_value"
	ErrCodePermission      ToolErrorCode = "permission_denied"
	ErrCodeExecution       ToolErrorCode = "execution_failed"
	ErrCodeTimeout         ToolErrorCode = "timeout"
)

// ToolExecutionError provides detailed, actionable error information
type ToolExecutionError struct {
	Code       ToolErrorCode `json:"code"`
	ToolName   string        `json:"tool_name"`
	Message    string        `json:"message"`
	Suggestion string        `json:"suggestion,omitempty"`
	Example    string        `json:"example,omitempty"`
	InvalidArg string        `json:"invalid_arg,omitempty"`
	Expected   string        `json:"expected,omitempty"`
	Received   string        `json:"received,omitempty"`
}

func (e *ToolExecutionError) Error() string {
	return e.Message
}

// ToJSON converts the error to a JSON-serializable map
func (e *ToolExecutionError) ToJSON() map[string]any {
	result := map[string]any{
		"code":        string(e.Code),
		"error_code":  string(e.Code),
		"error_message": e.Message,
		"tool":        e.ToolName,
	}

	if e.Suggestion != "" {
		result["suggestion"] = e.Suggestion
	}
	if e.Example != "" {
		result["example"] = e.Example
	}
	if e.InvalidArg != "" {
		result["invalid_arg"] = e.InvalidArg
	}
	if e.Expected != "" {
		result["expected"] = e.Expected
	}
	if e.Received != "" {
		result["received"] = e.Received
	}

	return result
}

// ErrorBuilder helps construct detailed ToolExecutionError instances
type ErrorBuilder struct {
	toolName string
}

// NewErrorBuilder creates a new ErrorBuilder for the given tool name
func NewErrorBuilder(toolName string) *ErrorBuilder {
	return &ErrorBuilder{toolName: toolName}
}

// JSONSyntaxError creates an error for JSON parsing issues
func (b *ErrorBuilder) JSONSyntaxError(parseErr error, argsJSON string) *ToolExecutionError {
	// Extract a snippet of the JSON around the error location
	snippet := argsJSON
	if len(snippet) > 200 {
		snippet = snippet[:200] + "..."
	}

	// Provide specific guidance based on common errors
	suggestion := "Ensure all objects, arrays, and strings are properly closed. Check for trailing commas, missing quotes, or unescaped special characters."
	example := ""

	if strings.Contains(parseErr.Error(), "unexpected end") {
		suggestion = "The JSON appears to be incomplete. Ensure all objects, arrays, and strings are properly closed."
		example = fmt.Sprintf("Correct: {\"path\": \"/tmp/file.txt\", \"offset\": 10}\nIncorrect: {\"path\": \"/tmp/file.txt\", \"offset\":")
	} else if strings.Contains(parseErr.Error(), "invalid character") {
		suggestion = "Check for unquoted keys, missing commas between fields, or invalid escape sequences."
		example = fmt.Sprintf("Correct: {\"path\": \"/tmp/file.txt\"}\nIncorrect: {path: \"/tmp/file.txt\"}")
	}

	return &ToolExecutionError{
		Code:       ErrCodeJSONSyntax,
		ToolName:   b.toolName,
		Message:    fmt.Sprintf("Invalid JSON syntax: %s", parseErr.Error()),
		Suggestion: suggestion,
		Example:    example,
		Received:   snippet,
	}
}

// MissingRequiredError creates an error for missing required parameters
func (b *ErrorBuilder) MissingRequiredError(paramName string) *ToolExecutionError {
	return &ToolExecutionError{
		Code:       ErrCodeMissingRequired,
		ToolName:   b.toolName,
		Message:    fmt.Sprintf("Missing required parameter: %s", paramName),
		Suggestion: fmt.Sprintf("The '%s' parameter is required for this tool. Please provide it in the arguments.", paramName),
		InvalidArg: paramName,
		Example:    fmt.Sprintf("{\"%s\": \"value\"}", paramName),
	}
}

// InvalidTypeError creates an error for type mismatches
func (b *ErrorBuilder) InvalidTypeError(paramName string, expectedType string, receivedValue any) *ToolExecutionError {
	received := "null"
	if receivedValue != nil {
		switch v := receivedValue.(type) {
		case string:
			received = fmt.Sprintf("string (\"%s\")", v)
		case float64:
			received = fmt.Sprintf("number (%v)", v)
		case bool:
			received = fmt.Sprintf("boolean (%v)", v)
		case []any:
			received = fmt.Sprintf("array [%d items]", len(v))
		case map[string]any:
			received = fmt.Sprintf("object {%d keys}", len(v))
		default:
			received = fmt.Sprintf("%T", v)
		}
	}

	suggestion := fmt.Sprintf("The '%s' parameter must be a %s. Please check the value and try again.", paramName, expectedType)
	example := fmt.Sprintf("{\"%s\": <%s example>}", paramName, expectedType)

	return &ToolExecutionError{
		Code:       ErrCodeInvalidType,
		ToolName:   b.toolName,
		Message:    fmt.Sprintf("Invalid type for parameter '%s': expected %s", paramName, expectedType),
		Suggestion: suggestion,
		Example:    example,
		InvalidArg: paramName,
		Expected:   expectedType,
		Received:   received,
	}
}

// InvalidValueError creates an error for invalid values
func (b *ErrorBuilder) InvalidValueError(paramName string, reason string, validOptions []string) *ToolExecutionError {
	suggestion := fmt.Sprintf("The value provided for '%s' is invalid: %s", paramName, reason)
	example := ""

	if len(validOptions) > 0 {
		if len(validOptions) <= 5 {
			suggestion += fmt.Sprintf("\nValid options are: %s", strings.Join(validOptions, ", "))
		} else {
			suggestion += fmt.Sprintf("\nValid options include: %s", strings.Join(validOptions[:5], ", "))
		}
		example = fmt.Sprintf("{\"%s\": \"%s\"}", paramName, validOptions[0])
	}

	return &ToolExecutionError{
		Code:       ErrCodeInvalidValue,
		ToolName:   b.toolName,
		Message:    fmt.Sprintf("Invalid value for parameter '%s'", paramName),
		Suggestion: suggestion,
		Example:    example,
		InvalidArg: paramName,
		Received:   reason,
	}
}

// PermissionDeniedError creates an error for permission issues
func (b *ErrorBuilder) PermissionDeniedError(resource string, reason string) *ToolExecutionError {
	suggestion := "This action was blocked by the security engine. If you believe this is an error, contact your administrator."

	return &ToolExecutionError{
		Code:       ErrCodePermission,
		ToolName:   b.toolName,
		Message:    fmt.Sprintf("Permission denied: %s", reason),
		Suggestion: suggestion,
		Example:    fmt.Sprintf("Resource: %s\nReason: %s", resource, reason),
		InvalidArg: resource,
	}
}

// ExecutionError creates an error for execution failures
func (b *ErrorBuilder) ExecutionError(action string, err error) *ToolExecutionError {
	suggestion := fmt.Sprintf("The tool failed to %s. Check the error message for details.", action)

	return &ToolExecutionError{
		Code:       ErrCodeExecution,
		ToolName:   b.toolName,
		Message:    fmt.Sprintf("Execution failed: %s", err.Error()),
		Suggestion: suggestion,
		Received:   err.Error(),
	}
}

// TimeoutError creates an error for timeout scenarios
func (b *ErrorBuilder) TimeoutError(action string, timeoutSeconds int) *ToolExecutionError {
	suggestion := fmt.Sprintf("The operation timed out after %d seconds. Try breaking down the task into smaller steps or optimize the operation.", timeoutSeconds)

	return &ToolExecutionError{
		Code:       ErrCodeTimeout,
		ToolName:   b.toolName,
		Message:    fmt.Sprintf("Operation timed out: %s", action),
		Suggestion: suggestion,
		Received:   fmt.Sprintf("%d seconds", timeoutSeconds),
	}
}

// CreateErrorFromJSON creates a ToolExecutionError from a JSON error message (for backward compatibility)
func CreateErrorFromJSON(toolName string, errMsg string) *ToolExecutionError {
	builder := NewErrorBuilder(toolName)

	// Try to identify error type from message
	if strings.Contains(errMsg, "invalid JSON") || strings.Contains(errMsg, "JSON syntax") {
		return builder.JSONSyntaxError(fmt.Errorf("%s", errMsg), "{}")
	}

	if strings.Contains(errMsg, "missing required") || strings.Contains(errMsg, "required parameter") {
		// Extract parameter name if possible
		parts := strings.Split(errMsg, "'")
		if len(parts) >= 2 {
			return builder.MissingRequiredError(parts[1])
		}
		return builder.MissingRequiredError("unknown")
	}

	if strings.Contains(errMsg, "permission denied") || strings.Contains(errMsg, "unauthorized") {
		return builder.PermissionDeniedError("unknown", errMsg)
	}

	// Default to execution error
	return builder.ExecutionError("operation", fmt.Errorf("%s", errMsg))
}

// SerializeError converts a ToolExecutionError or standard error to a JSON map
func SerializeError(toolName string, err error) map[string]any {
	if toolErr, ok := err.(*ToolExecutionError); ok {
		return toolErr.ToJSON()
	}

	// For standard errors, wrap in a basic ToolExecutionError
	wrapped := NewErrorBuilder(toolName).ExecutionError("operation", err)
	return wrapped.ToJSON()
}

// ParseAndEnhanceError attempts to parse JSON args and provide detailed error information
func ParseAndEnhanceError(toolName string, argsJSON string, parseErr error) *ToolExecutionError {
	builder := NewErrorBuilder(toolName)

	// Basic syntax error
	result := builder.JSONSyntaxError(parseErr, argsJSON)

	// Try to provide more specific guidance
	errStr := parseErr.Error()

	// Check for common JSON patterns
	if strings.Contains(errStr, "unexpected end") {
		// Count opening and closing braces to identify what's missing
		openBraces := strings.Count(argsJSON, "{")
		closeBraces := strings.Count(argsJSON, "}")
		openBrackets := strings.Count(argsJSON, "[")
		closeBrackets := strings.Count(argsJSON, "]")

		if openBraces > closeBraces {
			result.Suggestion += fmt.Sprintf("\n\nDetected %d unclosed object(s) (missing %d '}')", openBraces-closeBraces, openBraces-closeBraces)
		}
		if openBrackets > closeBrackets {
			result.Suggestion += fmt.Sprintf("\n\nDetected %d unclosed array(s) (missing %d ']')", openBrackets-closeBrackets, openBrackets-closeBrackets)
		}
	}

	// Check for unquoted strings (common error)
	if strings.Contains(errStr, "invalid character") {
		// Look for patterns like: {key: value} instead of {"key": "value"}
		if strings.Contains(argsJSON, ": ") && !strings.Contains(argsJSON, "\",") {
			result.Suggestion += "\n\nKeys in JSON must be enclosed in double quotes. Example: {\"key\": \"value\"} not {key: \"value\"}"
		}
	}

	// Check for trailing commas (common error)
	if strings.Contains(errStr, "after object") || strings.Contains(errStr, "after array") {
		if strings.HasSuffix(strings.TrimSpace(argsJSON), ",") || strings.Contains(argsJSON, ", }") || strings.Contains(argsJSON, ",]") {
			result.Suggestion += "\n\nTrailing commas are not allowed in JSON. Remove the comma before the closing bracket."
		}
	}

	return result
}

// ValidateJSONParameters validates that required parameters are present and have correct types
func ValidateJSONParameters(toolName string, args map[string]any, requiredParams map[string]string) *ToolExecutionError {
	builder := NewErrorBuilder(toolName)

	// Check required parameters
	for paramName, paramType := range requiredParams {
		value, exists := args[paramName]
		if !exists {
			return builder.MissingRequiredError(paramName)
		}

		// Type validation
		if value != nil {
			switch paramType {
			case "string":
				if _, ok := value.(string); !ok {
					return builder.InvalidTypeError(paramName, "string", value)
				}
			case "number", "integer":
				if _, ok := value.(float64); !ok {
					return builder.InvalidTypeError(paramName, "number", value)
				}
			case "boolean":
				if _, ok := value.(bool); !ok {
					return builder.InvalidTypeError(paramName, "boolean", value)
				}
			case "array":
				if _, ok := value.([]any); !ok {
					return builder.InvalidTypeError(paramName, "array", value)
				}
			case "object":
				if _, ok := value.(map[string]any); !ok {
					return builder.InvalidTypeError(paramName, "object", value)
				}
			}
		}
	}

	return nil
}

// FormatExampleArgs formats example arguments for display in error messages
func FormatExampleArgs(args map[string]any) string {
	jsonBytes, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(jsonBytes)
}
