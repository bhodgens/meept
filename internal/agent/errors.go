package agent

import (


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



// ErrorBuilder helps construct detailed ToolExecutionError instances
type ErrorBuilder struct {
}



func (e *ToolExecutionError) Error() string {
	return e.Message
}
