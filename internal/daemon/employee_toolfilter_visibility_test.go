package daemon

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/tools"
)

// Bug 3 pins: an explicitly-EMPTY tools_allowed (declared, non-nil)
// shows the model NOTHING; only an explicit "*" inherits the parent
// list. A constitution that never declares tool lists (nil slices, e.g.
// the zero-value Constraints struct) still inherits the parent, which is
// the no-Constitution shape applyEmployeeDefinition skips anyway.

func toolfilterParent() *nameRegistry {
	return &nameRegistry{tools: []tools.Tool{
		nameTool{name: "inbox"},
		nameTool{name: "shell"},
	}}
}

func TestEmployeeToolfilter_VisibleToolsEmptyAllowlistHidesShell(t *testing.T) {
	parent := toolfilterParent()
	c := &employee.Constitution{Constraints: employee.ConstitutionalConstraints{
		ToolsAllowed: []string{},
	}}
	visible := employeeVisibleTools(parent, c)
	if got := defNames(visible); len(got) != 0 {
		t.Fatalf("empty declared allowlist tools = %v, want none", got)
	}
	if visible.Get("shell") != nil {
		t.Fatal("shell is reachable on a declared-empty-allowlist registry")
	}
	if parent.Get("shell") == nil {
		t.Fatal("filter mutated the parent registry")
	}
}

func TestEmployeeToolfilter_VisibleToolsStarInheritsMinusForbidden(t *testing.T) {
	parent := toolfilterParent()

	star := &employee.Constitution{Constraints: employee.ConstitutionalConstraints{
		ToolsAllowed: []string{"*"},
	}}
	visible := employeeVisibleTools(parent, star)
	if got := defNames(visible); len(got) != 2 {
		t.Fatalf("star allowlist tools = %v, want both", got)
	}
	if visible.Get("shell") == nil {
		t.Fatal("explicit star must inherit shell")
	}

	starForbid := &employee.Constitution{Constraints: employee.ConstitutionalConstraints{
		ToolsAllowed:   []string{" * "},
		ToolsForbidden: []string{"SHELL"},
	}}
	visible = employeeVisibleTools(parent, starForbid)
	got := defNames(visible)
	if len(got) != 1 || got[0] != "inbox" {
		t.Fatalf("star-minus-forbidden tools = %v, want [inbox]", got)
	}
	if visible.Get("shell") != nil {
		t.Fatal("forbidden shell reachable through the star lane")
	}
}

func TestEmployeeToolfilter_VisibleToolsEmptyForbiddenOnlyStillInherits(t *testing.T) {
	// Declared-empty forbidden list with NO allowlist declaration is not
	// a tool policy: the parent (inherit-everything) stands.
	parent := toolfilterParent()
	c := &employee.Constitution{Constraints: employee.ConstitutionalConstraints{
		ToolsForbidden: []string{},
	}}
	if got := employeeVisibleTools(parent, c); got != agent.ToolRegistry(parent) {
		t.Fatal("empty forbidden-only constraint replaced the parent registry")
	}
}

func TestEmployeeToolfilter_VisibleToolsNilConstitutionReturnsParent(t *testing.T) {
	parent := toolfilterParent()
	if got := employeeVisibleTools(parent, nil); got != agent.ToolRegistry(parent) {
		t.Fatal("nil constitution must return parent unchanged")
	}
	// No Constraints at all (zero-value struct): parent stands.
	c := &employee.Constitution{}
	if got := employeeVisibleTools(parent, c); got != agent.ToolRegistry(parent) {
		t.Fatal("no-Constraints constitution replaced the parent registry")
	}
}

// --- Bug 2: failed employee lookup must deny-all, not inherit ---

type errEmployeeResolver struct{ err error }

func (s errEmployeeResolver) GetEmployee(context.Context, string) (*employee.Employee, error) {
	return nil, s.err
}

func TestApplyEmployeeDefinition_LookupErrorDeniesAll(t *testing.T) {
	parent := toolfilterParent()
	template := agent.NewAgentLoop("template-lookup-err", "", agent.WithToolRegistry(parent))
	clone := agent.NewAgentLoop("clone-lookup-err", "", agent.WithToolRegistry(parent))
	logger := slog.New(slog.DiscardHandler)

	wantErr := errors.New("employee store unreachable")
	override := applyEmployeeDefinition(context.Background(), clone, "ghost",
		errEmployeeResolver{err: wantErr}, templateRegistry(template), logger)
	if override != "" {
		t.Fatalf("override = %q, want empty", override)
	}

	// The clone runs deny-all: an unresolvable employee gets NO tools.
	if got := defNames(clone.ToolRegistry()); len(got) != 0 {
		t.Fatalf("clone tools after lookup failure = %v, want none", got)
	}
	if clone.ToolRegistry().Get("shell") != nil {
		t.Fatal("clone kept shell after an unresolvable employee lookup")
	}
	// The shared template registry is untouched.
	if got := defNames(template.ToolRegistry()); len(got) != 2 {
		t.Fatalf("template tools = %v, want both", got)
	}
}

func TestApplyEmployeeDefinition_LookupNilDefDeniesAll(t *testing.T) {
	parent := toolfilterParent()
	template := agent.NewAgentLoop("template-nildef", "", agent.WithToolRegistry(parent))
	clone := agent.NewAgentLoop("clone-nildef", "", agent.WithToolRegistry(parent))
	logger := slog.New(slog.DiscardHandler)

	override := applyEmployeeDefinition(context.Background(), clone, "ghost",
		stubEmployeeResolver{emp: nil}, templateRegistry(template), logger)
	if override != "" {
		t.Fatalf("override = %q, want empty", override)
	}
	if clone.ToolRegistry().Get("inbox") != nil {
		t.Fatal("clone kept tools after a nil-definition lookup")
	}
}

// --- Bug 3 through applyEmployeeDefinition: declared-empty allowlist ---

func TestApplyEmployeeDefinition_EmptyAllowlistCloneSeesNoTools(t *testing.T) {
	parent := toolfilterParent()
	template := agent.NewAgentLoop("template-emptyallow", "", agent.WithToolRegistry(parent))
	clone := agent.NewAgentLoop("clone-emptyallow", "", agent.WithToolRegistry(parent))
	def := &employee.Employee{}
	def.Prompt = "speak"
	def.Constitution = employee.Constitution{
		Purpose: "answer calls",
		Constraints: employee.ConstitutionalConstraints{
			ToolsAllowed: []string{},
		},
	}

	applyEmployeeDefinition(context.Background(), clone, "secretary",
		stubEmployeeResolver{emp: def}, templateRegistry(template), slog.New(slog.DiscardHandler))

	if got := defNames(clone.ToolRegistry()); len(got) != 0 {
		t.Fatalf("clone tools = %v, want none (declared-empty allowlist)", got)
	}
	if got := defNames(template.ToolRegistry()); len(got) != 2 {
		t.Fatalf("template tools = %v, want both", got)
	}
}
