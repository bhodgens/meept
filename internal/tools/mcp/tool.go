package mcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/security/taint"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
)

// Sanitizer is the subset of the security.InputSanitizer behaviour required
// by MCPTool. Defining it locally avoids an import cycle
// (internal/tools/mcp is imported by internal/config, which is imported by
// internal/security, so mcp cannot import security directly). Callers that
// can import internal/security pass a *SecuritySanitizer adapter; see
// daemon/components.go registerMCPTools.
type Sanitizer interface {
	// Sanitize scans text for prompt-injection and similar threats and
	// returns the cleaned text plus metadata about what was detected.
	Sanitize(text string) SanitizeResult
}

// SanitizeResult is the decoupled mirror of security.SanitizationResult.
// Only the fields MCPTool actually consumes are represented.
type SanitizeResult struct {
	CleanText       string
	WasModified     bool
	ThreatsDetected int
}

// SecuritySanitizer adapts a security.InputSanitizer to the local Sanitizer
// interface. It is defined here (rather than importing internal/security)
// to keep the mcp package free of the internal/security import cycle.
//
// Callers construct this from packages that can import internal/security;
// see daemon/components.go registerMCPTools.
type SecuritySanitizer struct {
	sanitize func(string) SanitizeResult
}

// NewSecuritySanitizer builds a SecuritySanitizer from a sanitize function.
// Callers typically pass a closure over *intsecurity.InputSanitizer.Sanitize.
func NewSecuritySanitizer(sanitize func(string) SanitizeResult) *SecuritySanitizer {
	return &SecuritySanitizer{sanitize: sanitize}
}

// Sanitize delegates to the wrapped function.
func (s *SecuritySanitizer) Sanitize(text string) SanitizeResult {
	if s == nil || s.sanitize == nil {
		return SanitizeResult{CleanText: text}
	}
	return s.sanitize(text)
}

// MCPTool wraps an MCP tool to expose it as a local tool.
// It implements the tools.Tool interface.
//
// Security: MCP servers are external processes that may return arbitrary
// (potentially hostile) content. When a Sanitizer is attached via
// SetSanitizer, Execute() sanitizes the result content for injection
// patterns. The returned *tools.ToolResult always carries
// taint.TaintExternal so downstream policy checks (shell_exec sink, agent
// message sink, etc.) can apply stricter rules. The agent loop wraps the
// output with boundary markers via Orchestrator.WrapToolOutput().
//
//nolint:revive // stutter with package name is intentional for API clarity
type MCPTool struct {
	def       llm.ToolDefinition
	manager   *Manager
	server    string // Server name for routing
	sanitizer Sanitizer
	logger    *slog.Logger
}

// NewMCPTool creates a new MCPTool wrapper.
func NewMCPTool(def llm.ToolDefinition, manager *Manager, server string) *MCPTool {
	return &MCPTool{
		def:     def,
		manager: manager,
		server:  server,
		logger:  slog.Default(),
	}
}

// SetSanitizer attaches a Sanitizer used to scrub MCP result content for
// injection patterns. Follows the nil-guard pattern mandated by CLAUDE.md.
func (t *MCPTool) SetSanitizer(s Sanitizer) {
	if s != nil {
		t.sanitizer = s
	}
}

// SetLogger sets the logger used for audit-style messages. Falls back to
// slog.Default() when unset. Follows the nil-guard pattern.
func (t *MCPTool) SetLogger(l *slog.Logger) {
	if l != nil {
		t.logger = l
	}
}

// Name returns the fully qualified tool name (server.tool).
func (t *MCPTool) Name() string {
	return t.def.Function.Name
}

// Description returns the tool description.
func (t *MCPTool) Description() string {
	return t.def.Function.Description
}

// Parameters returns the tool parameters as FunctionParameters.
func (t *MCPTool) Parameters() llm.FunctionParameters {
	return t.def.Function.Parameters
}

// Execute invokes the MCP tool via the manager and returns a *tools.ToolResult
// carrying taint.TaintExternal. The result content is sanitized for injection
// patterns when a Sanitizer is attached.
//
// Note: the agent loop (internal/agent/loop.go) is responsible for wrapping
// the stringified output with boundary markers via
// Orchestrator.WrapToolOutput(name, output). This method does not double-wrap.
func (t *MCPTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	result, err := t.manager.CallTool(ctx, t.Name(), args)
	if err != nil {
		return nil, err
	}
	if result == nil {
		// Defensive: should not happen, but avoid typed-nil/nil-deref.
		return nil, fmt.Errorf("mcp tool %q returned nil result", t.Name())
	}

	// On error results from the MCP server, propagate the error string with
	// the external taint label so downstream sinks still treat it as untrusted.
	if !result.Success {
		return &tools.ToolResult{
			Success:    false,
			Error:      result.Error,
			TaintLabel: taint.TaintExternal,
		}, nil
	}

	// Stringify the result content for sanitization. CallTool returns the
	// text content directly (built in client.CallTool from content blocks).
	content, ok := result.Result.(string)
	if !ok {
		// Non-string result: pass through unchanged but still taint-marked.
		return &tools.ToolResult{
			Success:    true,
			Result:     result.Result,
			TaintLabel: taint.TaintExternal,
		}, nil
	}

	// Sanitize result content for injection patterns. The Sanitizer detects
	// prompt-injection-style payloads (e.g., fake <system-reminder> blocks)
	// and strips/redacts them before the content reaches the LLM.
	if t.sanitizer != nil {
		sanitizeResult := t.sanitizer.Sanitize(content)
		if sanitizeResult.WasModified || sanitizeResult.ThreatsDetected > 0 {
			t.logger.Info("MCP result sanitized",
				"server", t.server,
				"tool", t.Name(),
				"threats", sanitizeResult.ThreatsDetected,
				"modified", sanitizeResult.WasModified,
			)
		}
		content = sanitizeResult.CleanText
	}

	// Build evidence: record that an MCP tool returned data.
	evidence := []models.Evidence{
		models.NewEvidence(
			models.EvidenceAPIResponse,
			fmt.Sprintf("mcp://%s/%s", t.server, t.def.Function.Name),
			fmt.Sprintf("success=%v,len=%d", result.Success, len(content)),
			t.Name(),
		),
	}

	return &tools.ToolResult{
		Success:    true,
		Result:     content,
		TaintLabel: taint.TaintExternal, // MCP servers are external processes
		Evidence:   evidence,
	}, nil
}

// ToLLMDefinition returns the LLM tool definition.
func (t *MCPTool) ToLLMDefinition() llm.ToolDefinition {
	return t.def
}

// Server returns the MCP server name this tool belongs to.
func (t *MCPTool) Server() string {
	return t.server
}

// Category returns the tool category. MCP tools are categorized as "mcp".
func (t *MCPTool) Category() string { return "mcp" }

// Compile-time assertion that MCPTool implements the tools.Tool interface.
var _ tools.Tool = (*MCPTool)(nil)
