package builtin

// F10 gate T2/T2b (04-tool-surface/00T-tests.md): lsp_navigate tool.
// Pins contract B2 (.hermes/contracts/04-tool-surface.md):
//   - Name() "lsp_navigate"; IsReadOnly -> true; TimeoutMs() -> 60s (B1)
//   - operations: goToDefinition | findReferences | goToImplementation | hover
//   - one-based UTF-16 line/character map to zero-based seam positions
//   - result caps defaultMaxLocations=100, defaultMaxResultChars=16000
//   - unresolvable workspace -> detectable error wrapping tools.ErrNoWorkingDir
//     (mirrors "workspace cwd" semantics), never a panic
//
// Activated from the parked gate file (lsp_navigate_test.go.staged, 762dc9d3).
// Drift fixed vs the staged draft: NewLSPNavigateTool returns (tool, error)
// and rejects a nil manager; there is no RegisterLSPNavigateTool or
// parseLspArgs seam — argument validation lives in Execute, so the arg-parse
// and rejection assertions drive Execute directly. The caps are package
// constants (defaultMaxLocations/defaultMaxResultChars).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/code/lsp"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/tools"
)

func TestLSPNavigateTool_Registration(t *testing.T) {
	tool, err := NewLSPNavigateTool(nil)
	if err == nil || tool != nil {
		t.Fatal("NewLSPNavigateTool(nil) must be rejected: registration requires a live manager seam")
	}

	// The real construction seam succeeds with a non-nil manager and the
	// tool carries the contract surface (read-only + declared 60s budget).
	tool, err = NewLSPNavigateTool(newStubNavManager())
	if err != nil {
		t.Fatalf("NewLSPNavigateTool: %v", err)
	}
	if tool.Name() != "lsp_navigate" {
		t.Errorf("Name() = %q, want lsp_navigate", tool.Name())
	}
	if !tool.IsReadOnly(map[string]any{"operation": "goToDefinition"}) {
		t.Error("lsp_navigate must be read-only")
	}
	if got := tool.TimeoutMs(); got != 60*time.Second {
		t.Errorf("TimeoutMs() = %v, want 60s (B1 consumer)", got)
	}
	var _ tools.Tool = tool
}

func TestLSPNavigateTool_RejectsUnknownOperation(t *testing.T) {
	tool := mustNavTool(t)
	_, err := tool.Execute(context.Background(), map[string]any{
		"operation": "renameSymbol",
		"file_path": "a.go",
		"line":      float64(1),
		"character": float64(1),
	})
	if err == nil {
		t.Fatal("unknown operation must be rejected")
	}
}

func TestLSPNavigateTool_RejectsNonPositiveCoords(t *testing.T) {
	tool := mustNavTool(t)
	_, err := tool.Execute(context.Background(), map[string]any{
		"operation": "hover",
		"file_path": "a.go",
		"line":      float64(0), // one-based: 0 is invalid
		"character": float64(1),
	})
	if err == nil {
		t.Fatal("non-positive coordinates must be rejected (coords are one-based)")
	}
}

func TestLSPNavigateTool_RejectsMissingRequiredArgs(t *testing.T) {
	tool := mustNavTool(t)
	// No file_path.
	if _, err := tool.Execute(context.Background(), map[string]any{
		"operation": "goToDefinition",
		"line":      float64(1),
		"character": float64(1),
	}); err == nil {
		t.Fatal("missing file_path must be rejected")
	}
	// Non-numeric line.
	if _, err := tool.Execute(context.Background(), map[string]any{
		"operation": "goToDefinition",
		"file_path": "a.go",
		"line":      "5",
		"character": float64(1),
	}); err == nil {
		t.Fatal("non-numeric line must be rejected")
	}
}

func TestLSPNavigateTool_WorkspaceSentinel(t *testing.T) {
	// T2b: an unresolvable workspace yields a detectable error naming the
	// required workspace cwd, wrapping tools.ErrNoWorkingDir (errors.Is
	// routeable), never a panic or an empty-guess path.
	tool := mustNavTool(t)
	_, err := tool.Execute(context.Background(), map[string]any{
		"operation": "goToDefinition",
		"file_path": "a.go",
		"line":      float64(1),
		"character": float64(1),
		// no workspace, no session working dir injected in ctx
	})
	if err == nil {
		t.Fatal("expected a workspace failure, got success")
	}
	if !errors.Is(err, tools.ErrNoWorkingDir) && !tools.IsNoWorkingDir(err) {
		t.Errorf("error %q must mirror the tools.ErrNoWorkingDir sentinel semantics", err)
	}
}

func TestLSPNavigateTool_ResultCaps(t *testing.T) {
	// The caps are contract constants: 100 locations, 16000 chars.
	if defaultMaxLocations != 100 {
		t.Errorf("defaultMaxLocations = %d, want 100", defaultMaxLocations)
	}
	if defaultMaxResultChars != 16000 {
		t.Errorf("defaultMaxResultChars = %d, want 16000", defaultMaxResultChars)
	}
}

// mustNavTool builds the tool against an empty lsp.Manager (never spawned:
// the workspace sentinel and arg-validation paths fail before any server
// call; GetServerForLanguage on an empty manager errors, which is exactly
// the "no LSP server" arm of the contract).
func mustNavTool(t *testing.T) *LSPNavigateTool {
	t.Helper()
	tool, err := NewLSPNavigateTool(lsp.NewManager(config.LSPConfig{}))
	if err != nil {
		t.Fatalf("NewLSPNavigateTool: %v", err)
	}
	return tool
}

// newStubNavManager is the bare manager for the registration-shape test.
func newStubNavManager() *lsp.Manager {
	return lsp.NewManager(config.LSPConfig{})
}
