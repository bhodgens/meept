package daemon

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

type nameTool struct{ name string }

func (t nameTool) Name() string                       { return t.name }
func (t nameTool) Description() string                { return t.name }
func (t nameTool) Parameters() llm.FunctionParameters { return llm.FunctionParameters{} }
func (t nameTool) Execute(context.Context, map[string]any) (any, error) {
	return nil, nil
}
func (t nameTool) IsReadOnly(map[string]any) bool        { return true }
func (t nameTool) IsConcurrencySafe(map[string]any) bool { return true }
func (t nameTool) TimeoutMs() time.Duration              { return 0 }

type nameRegistry struct{ tools []tools.Tool }

func (r nameRegistry) Get(name string) tools.Tool {
	for _, tool := range r.tools {
		if tool != nil && tool.Name() == name {
			return tool
		}
	}
	return nil
}

func (r nameRegistry) List() []tools.Tool { return r.tools }

func (r nameRegistry) GetDefinitions() []llm.ToolDefinition {
	out := make([]llm.ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		if tool == nil {
			continue
		}
		out = append(out, llm.ToolDefinition{Function: llm.FunctionDef{Name: tool.Name()}})
	}
	return out
}

func defNames(reg agent.ToolRegistry) []string {
	if reg == nil {
		return nil
	}
	defs := reg.GetDefinitions()
	out := make([]string, 0, len(defs))
	for _, def := range defs {
		out = append(out, def.Function.Name)
	}
	return out
}

func TestEmployeeVisibleTools_AllowlistHidesUnlistedTools(t *testing.T) {
	parent := nameRegistry{tools: []tools.Tool{
		nameTool{name: "inbox"},
		nameTool{name: "shell"},
		nameTool{name: "ledger_append"},
	}}
	c := &employee.Constitution{Constraints: employee.ConstitutionalConstraints{
		ToolsAllowed:   []string{"Inbox", "ledger_append"},
		ToolsForbidden: []string{"shell"},
	}}

	visible := employeeVisibleTools(parent, c)
	got := defNames(visible)
	if len(got) != 2 || got[0] != "inbox" || got[1] != "ledger_append" {
		t.Fatalf("visible tools = %v, want [inbox ledger_append]", got)
	}
	if visible.Get("shell") != nil {
		t.Fatal("shell is callable on the filtered registry")
	}
	if parent.Get("shell") == nil {
		t.Fatal("filter mutated the parent registry")
	}
}

func TestEmployeeVisibleTools_EmptyConstraintKeepsParent(t *testing.T) {
	parent := &nameRegistry{tools: []tools.Tool{nameTool{name: "shell"}}}
	c := &employee.Constitution{}
	if got := employeeVisibleTools(parent, c); got != parent {
		t.Fatal("empty tool constraint replaced the parent registry")
	}
}

func TestEmployeeVisibleTools_ForbidStarDeniesAll(t *testing.T) {
	parent := nameRegistry{tools: []tools.Tool{nameTool{name: "inbox"}}}
	c := &employee.Constitution{Constraints: employee.ConstitutionalConstraints{ToolsForbidden: []string{"*"}}}
	visible := employeeVisibleTools(parent, c)
	if len(defNames(visible)) != 0 {
		t.Fatalf("forbid-star tools = %v, want none", defNames(visible))
	}
}

type stubEmployeeResolver struct{ emp *employee.Employee }

func (s stubEmployeeResolver) GetEmployee(context.Context, string) (*employee.Employee, error) {
	return s.emp, nil
}

func TestApplyEmployeeDefinition_FiltersCloneNotTemplate(t *testing.T) {
	parent := nameRegistry{tools: []tools.Tool{
		nameTool{name: "inbox"},
		nameTool{name: "shell"},
	}}
	template := agent.NewAgentLoop("template", "", agent.WithToolRegistry(parent))
	clone := agent.NewAgentLoop("clone", "", agent.WithToolRegistry(parent))
	emp := &employee.Employee{
		Prompt: "speak",
		Constitution: employee.Constitution{
			Purpose: "answer calls",
			Constraints: employee.ConstitutionalConstraints{
				ToolsAllowed: []string{"inbox"},
			},
		},
	}

	applyEmployeeDefinition(context.Background(), clone, "secretary", stubEmployeeResolver{emp}, templateRegistry(template), slog.Default())

	if got := defNames(clone.ToolRegistry()); len(got) != 1 || got[0] != "inbox" {
		t.Fatalf("clone tools = %v, want [inbox]", got)
	}
	if got := defNames(template.ToolRegistry()); len(got) != 2 {
		t.Fatalf("template tools = %v, want both", got)
	}
}
