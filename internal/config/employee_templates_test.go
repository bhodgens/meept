package config_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/employee"
)

// employeesTemplateDir resolves the repo's config/employees directory
// relative to THIS test source file (internal/config/), never $MEEPT_HOME:
// a live install's swapped-in definitions must not change what the shipped
// templates are graded on.
func employeesTemplateDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", "config", "employees")
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("resolve template dir: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("template dir %s unreadable (test must run from the repo checkout): %v", abs, err)
	}
	return abs
}

// companionReadOnlyTools is the pinned read-only reporting subset of the
// builtin tool registry (verified against Name() strings in
// internal/tools/builtin/*.go). The companion answers "what have I been
// working on / what is running" with exactly these tools and nothing else.
var companionReadOnlyTools = []string{
	"memory_search",
	"memory_recall",
	"memory_get_context",
	"platform_status",
	"platform_agents",
	"task_list",
}

// mutatingToolPattern matches any tool name whose prefix is mutating or
// out-of-charter for the companion: file I/O, shell, web, scheduling,
// dialing, delegation, memory/task writes, and the whole github/git/
// browser/office/template/debug surface. Used on the companion's tools
// and tools_allowed lists - a single hit means the read-only contract
// broke (live defect 2026-11-09: a voice companion executed schedule_list
// mid-call and its raw JSON was spoken aloud).
var mutatingToolPattern = regexp.MustCompile(`^(file_|shell|web_|schedule_|voice_call|github_|delegate|memory_store|memory_reflect|memory_retain|task_create|task_update|cron_|contact|inbox|resolve|ask|debug|template_|lsp_|git_|browser_|office_|pdf_|spreadsheet_)`)

// employeeTemplateShape is the minimal parse of a shipped employee
// template: enough to grade the governance invariants without importing
// internal/employee (which imports internal/config - a cycle).
type employeeTemplateShape struct {
	ID           string   `json:"id"`
	Prompt       string   `json:"prompt"`
	Tools        []string `json:"tools"`
	Enabled      *bool    `json:"enabled"`
	Constitution struct {
		Purpose     string `json:"purpose"`
		Charter     string `json:"charter"`
		Constraints struct {
			ToolsAllowed   []string `json:"tools_allowed"`
			ToolsForbidden []string `json:"tools_forbidden"`
		} `json:"constraints"`
		AmendmentPolicy struct {
			RequiresApproval bool `json:"requires_approval"`
		} `json:"amendment_policy"`
	} `json:"constitution"`
}

// TestEmployeeTemplates_Shape walks every shipped template in
// config/employees, parses it through the same JSON5 preprocessor the
// daemon uses (UnmarshalJSON5), and pins the governance invariants: a
// non-empty constitution and amendment_policy.requires_approval true.
func TestEmployeeTemplates_Shape(t *testing.T) {
	dir := employeesTemplateDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read template dir: %v", err)
	}
	var seen int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json5") {
			continue
		}
		seen++
		t.Run(strings.TrimSuffix(e.Name(), ".json5"), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("read %s: %v", e.Name(), err)
			}
			var shape employeeTemplateShape
			if err := config.UnmarshalJSON5(data, &shape); err != nil {
				t.Fatalf("parse %s: %v", e.Name(), err)
			}
			if shape.Constitution.Purpose == "" {
				t.Errorf("%s: constitution.purpose is empty - every shipped employee needs a constitution", e.Name())
			}
			if !shape.Constitution.AmendmentPolicy.RequiresApproval {
				t.Errorf("%s: amendment_policy.requires_approval must be true in a shipped template", e.Name())
			}
		})
	}
	if seen == 0 {
		t.Fatalf("no .json5 templates found in %s", dir)
	}
}

// TestEmployeeTemplates_CompanionReadOnly pins the leaf-04 companion
// contract (docs/plans/20261109-voice-comms-fixes/04-companion-refactor.md):
// template ships disabled, the tools array is EXACTLY the read-only
// reporting set, the constitution allow list matches it, no mutating-tool
// name appears in either, the forbidden list is the explicit mutating set
// (NOT the "*" wildcard - the enforcement engine checks tools_forbidden
// before tools_allowed, so a wildcard would deny the reporting tools too),
// and the prompt carries the "how can I help" opener directive.
func TestEmployeeTemplates_CompanionReadOnly(t *testing.T) {
	dir := employeesTemplateDir(t)
	data, err := os.ReadFile(filepath.Join(dir, "companion.json5"))
	if err != nil {
		t.Fatalf("read companion template: %v", err)
	}
	var shape employeeTemplateShape
	if err := config.UnmarshalJSON5(data, &shape); err != nil {
		t.Fatalf("parse companion template: %v", err)
	}

	if shape.Enabled == nil || *shape.Enabled {
		t.Error("companion template must ship enabled:false (hire is an operator opt-in, like secretary)")
	}

	if !slices.Equal(shape.Tools, companionReadOnlyTools) {
		t.Errorf("companion tools = %v, want exactly %v", shape.Tools, companionReadOnlyTools)
	}

	allowed := shape.Constitution.Constraints.ToolsAllowed
	if !slices.Equal(allowed, companionReadOnlyTools) {
		t.Errorf("companion tools_allowed = %v, want exactly %v", allowed, companionReadOnlyTools)
	}

	for _, list := range [][]string{shape.Tools, allowed} {
		for _, name := range list {
			if mutatingToolPattern.MatchString(name) {
				t.Errorf("mutating tool %q in companion read-only list %v", name, list)
			}
		}
	}

	forbidden := shape.Constitution.Constraints.ToolsForbidden
	if slices.Contains(forbidden, "*") {
		t.Error("companion tools_forbidden must not use the \"*\" wildcard: step 1a of the " +
			"pre-exec checker (internal/employee/enforcement.go) denies EVERY tool when the " +
			"wildcard is present, which would strip the companion of its read-only reporting tools")
	}
	if len(forbidden) == 0 {
		t.Error("companion tools_forbidden must name the mutating set explicitly")
	}
	var overlap []string
	for _, name := range companionReadOnlyTools {
		if slices.Contains(forbidden, name) {
			overlap = append(overlap, name)
		}
	}
	if len(overlap) > 0 {
		t.Errorf("read-only tools also listed in tools_forbidden: %v", overlap)
	}

	lower := strings.ToLower(shape.Prompt)
	if !strings.Contains(lower, "how can i help") {
		t.Error("companion prompt must contain the \"how can I help\" opener directive")
	}
	if strings.TrimSpace(shape.Constitution.Charter) == "" {
		t.Error("companion constitution.charter is empty")
	}
	if !strings.Contains(strings.ToLower(shape.Constitution.Charter), "never a worker") {
		t.Error("companion charter must state reporter + conversationalist, never a worker")
	}
}

// TestEmployeeTemplates_CompanionConstitutionValid runs the shipped
// companion template through the SAME decode the daemon performs at hire
// time. The constitution struct lives in internal/employee, which imports
// internal/config, so decoding into the shared JSON5 map and re-marshalling
// mirrors employee.decodeConstitution without an import cycle.
func TestEmployeeTemplates_CompanionConstitutionValid(t *testing.T) {
	dir := employeesTemplateDir(t)
	data, err := os.ReadFile(filepath.Join(dir, "companion.json5"))
	if err != nil {
		t.Fatalf("read companion template: %v", err)
	}
	var shape employeeTemplateShape
	if err := config.UnmarshalJSON5(data, &shape); err != nil {
		t.Fatalf("parse companion template: %v", err)
	}
	// Full decode requires the constitution block: re-parse generically.
	var full struct {
		Constitution employee.Constitution `json:"constitution"`
	}
	if err := config.UnmarshalJSON5(data, &full); err != nil {
		t.Fatalf("decode constitution block: %v", err)
	}
	if err := full.Constitution.Validate("companion"); err != nil {
		t.Fatalf("companion constitution invalid: %v", err)
	}

	// Behavioral pin: the SHIPPED constitution run through the real
	// pre-exec enforcement gate must ALLOW each read-only reporting tool
	// and DENY representative mutating ones. This is the leaf's whole
	// point - the old talk-only shape (tools_forbidden ["*"]) denied the
	// reporting tools too, and the pre-refactor shape (empty allow list)
	// let schedule_list through.
	p := employee.NewPreExecChecker("companion", &full.Constitution)
	for _, tool := range companionReadOnlyTools {
		if d := p.Check("execute", tool, nil); !d.Allowed {
			t.Errorf("read-only tool %q denied by shipped constitution: %s", tool, d.Reason)
		}
	}
	for _, tool := range []string{"file_write", "shell", "schedule_list", "web_fetch", "memory_store", "voice_call", "delegate_task"} {
		if d := p.Check("execute", tool, nil); d.Allowed {
			t.Errorf("mutating tool %q ALLOWED by shipped constitution", tool)
		}
	}
}
