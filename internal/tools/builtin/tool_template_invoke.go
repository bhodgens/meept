package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/templates"
	"github.com/caimlas/meept/internal/tools"
)

// Limits for template invocation.
const (
	// MaxTemplateBodySize is the maximum allowed character count for a template
	// body after substitution. Templates exceeding this size are rejected.
	MaxTemplateBodySize = 4096

	// MaxTemplatesPerTurn is the maximum number of turn-scoped template
	// injections allowed in a single tool call sequence.
	MaxTemplatesPerTurn = 3

	// MaxInjectedCharsTotal is the maximum total character count for injected
	// template content across all templates in a single invocation.
	MaxInjectedCharsTotal = 6000
)

// TemplateInvokeTool allows agents to invoke prompt templates at runtime.
// The tool supports two modes: invoke (default) returns the substituted
// template as result text, while inject mode activates session-scoped
// templates or returns content for turn injection.
type TemplateInvokeTool struct {
	registry *templates.Registry
}

// NewTemplateInvokeTool creates a new template invoke tool.
func NewTemplateInvokeTool(registry *templates.Registry) *TemplateInvokeTool {
	return &TemplateInvokeTool{registry: registry}
}

func (t *TemplateInvokeTool) Name() string { return "template_invoke" }

func (t *TemplateInvokeTool) Description() string {
	return "Invoke a prompt template by name with optional arguments. " +
		"Use template_list to discover available templates. " +
		"Set inject=true to activate the template as session-scoped context " +
		"(persists for the entire conversation) instead of returning the result as text."
}

func (t *TemplateInvokeTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"name": {
				Type:        "string",
				Description: "The name of the template to invoke.",
			},
			"args": {
				Type:        "array",
				Description: "Positional arguments for template substitution ($1, $2, $@, etc.).",
			},
			"inject": {
				Type:        "boolean",
				Description: "If true, inject the template as context rather than returning as text. Session-scoped templates persist until cleared.",
			},
			"conversation_id": {
				Type:        "string",
				Description: "Required when inject=true with a session-scoped template. The conversation ID to activate the template for.",
			},
		},
		Required: []string{"name"},
	}
}

// TemplateInvokeResult is the result of invoking a template.
type TemplateInvokeResult struct {
	Success       bool   `json:"success"`
	Name          string `json:"name"`
	Body          string `json:"body,omitempty"`
	Scope         string `json:"scope,omitempty"`
	CharCount     int    `json:"char_count"`
	Injected      bool   `json:"injected"`
	SessionActive bool   `json:"session_active"`
	Error         string `json:"error,omitempty"`
}

func (t *TemplateInvokeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.registry == nil {
		return TemplateInvokeResult{
			Success: false,
			Error:   "template registry not available",
		}, nil
	}

	name, _ := args["name"].(string)
	if name == "" {
		return TemplateInvokeResult{
			Success: false,
			Error:   "name is required",
		}, nil
	}

	// Parse positional arguments
	var templateArgs []string
	if rawArgs, ok := args["args"]; ok {
		switch v := rawArgs.(type) {
		case []any:
			for _, a := range v {
				if s, ok := a.(string); ok {
					templateArgs = append(templateArgs, s)
				}
			}
		case []string:
			templateArgs = v
		case string:
			if v != "" {
				templateArgs = strings.Fields(v)
			}
		}
	}

	inject, _ := args["inject"].(bool)
	conversationID, _ := args["conversation_id"].(string)

	// Look up the template
	tmpl := t.registry.Get(name)
	if tmpl == nil {
		return TemplateInvokeResult{
			Success: false,
			Name:    name,
			Error:   fmt.Sprintf("template not found: %s", name),
		}, nil
	}

	// Perform substitution
	substituted := templates.Substitute(tmpl.Body, templateArgs)

	// Validate body size
	if len(substituted) > MaxTemplateBodySize {
		return TemplateInvokeResult{
			Success: false,
			Name:    name,
			Error:   fmt.Sprintf("template body exceeds maximum size of %d characters (got %d)", MaxTemplateBodySize, len(substituted)),
		}, nil
	}

	if inject {
		// Inject mode: activate as session-scoped if template scope is session
		if tmpl.Scope == templates.ScopeSession {
			if conversationID == "" {
				return TemplateInvokeResult{
					Success: false,
					Name:    name,
					Error:   "conversation_id is required when injecting a session-scoped template",
				}, nil
			}

			if err := t.registry.ActivateSessionTemplate(conversationID, name, templateArgs); err != nil {
				return TemplateInvokeResult{
					Success: false,
					Name:    name,
					Error:   fmt.Sprintf("failed to activate session template: %s", err),
				}, nil
			}

			return TemplateInvokeResult{
				Success:       true,
				Name:          name,
				Body:          substituted,
				Scope:         string(tmpl.Scope),
				CharCount:     len(substituted),
				Injected:      true,
				SessionActive: true,
			}, nil
		}

		// Turn-scoped injection: return the substituted body with injection flag
		return TemplateInvokeResult{
			Success:   true,
			Name:      name,
			Body:      substituted,
			Scope:     string(tmpl.Scope),
			CharCount: len(substituted),
			Injected:  true,
		}, nil
	}

	// Default mode: return substituted text as result
	return TemplateInvokeResult{
		Success:   true,
		Name:      name,
		Body:      substituted,
		Scope:     string(tmpl.Scope),
		CharCount: len(substituted),
		Injected:  false,
	}, nil
}

// Ensure interface compliance
var _ tools.Tool = (*TemplateInvokeTool)(nil)
