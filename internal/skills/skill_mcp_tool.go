package skills

import (
	"context"
	"fmt"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/mcp"
)

// SkillMCPTool wraps a tool from a skill-embedded MCP server as a tools.Tool.
// Unlike mcp.MCPTool which routes through the global Manager, this calls
// the MCP client directly for skill-scoped tool execution.
type SkillMCPTool struct {
	def     llm.ToolDefinition
	client  *mcp.Client
	rawName string // unprefixed tool name for the client call
}

// Verify SkillMCPTool implements tools.Tool at compile time.
var _ tools.Tool = (*SkillMCPTool)(nil)

// NewSkillMCPTool creates a SkillMCPTool from a ToolDefinition and MCP client.
func NewSkillMCPTool(def llm.ToolDefinition, client *mcp.Client) *SkillMCPTool {
	return &SkillMCPTool{
		def:     def,
		client:  client,
		rawName: stripServerPrefix(def.Function.Name),
	}
}

func (t *SkillMCPTool) Name() string                       { return t.def.Function.Name }
func (t *SkillMCPTool) Description() string                { return t.def.Function.Description }
func (t *SkillMCPTool) Parameters() llm.FunctionParameters { return t.def.Function.Parameters }

// Execute invokes the MCP tool via the client directly.
func (t *SkillMCPTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	result, err := t.client.CallTool(ctx, t.rawName, args)
	if err != nil {
		return nil, err
	}
	if result.Success {
		return result.Result, nil
	}
	return nil, fmt.Errorf("skill mcp tool %q error: %s", t.Name(), result.Error)
}

// Category returns the tool category.
func (t *SkillMCPTool) Category() string { return "mcp-skill" }

// ToLLMDefinition returns the LLM tool definition for prompt building.
func (t *SkillMCPTool) ToLLMDefinition() llm.ToolDefinition {
	return t.def
}

// stripServerPrefix removes the "servername." prefix from a tool name.
func stripServerPrefix(name string) string {
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			return name[i+1:]
		}
	}
	return name
}
