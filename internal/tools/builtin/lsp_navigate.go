package builtin

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/code/lsp"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// Result caps and budget for lsp_navigate, ported from the dsh tool surface
// (packages/lsp/tool-lsp/src/index.ts defaults) and the 04-tool-surface
// contract C8/B1/B2. dsh convergence leaf 02-lsp-navigation.
const (
	defaultMaxLocations   = 100
	defaultMaxResultChars = 16000
	lspTimeout            = 60 * time.Second
)

// lspNavigateOperation enumerates the read-only navigation operations the tool
// supports, mirroring the dsh `lsp` operation enum.
type lspNavigateOperation string

const (
	opGoToDefinition     lspNavigateOperation = "goToDefinition"
	opFindReferences     lspNavigateOperation = "findReferences"
	opGoToImplementation lspNavigateOperation = "goToImplementation"
	opHover              lspNavigateOperation = "hover"
)

// Parameter keys (kept local to this tool; does not disturb shared constants).
const (
	navPropOperation = "operation"
	navPropFilePath  = "file_path"
	navPropLine      = "line"
	navPropChar      = "character"
	navPropWorkspace = "workspace"
	navPropFound     = "found"
	navPropMessage   = "message"
	navPropCount     = "count"
	navPropTruncated = "truncated"
	navPropOmitted   = "omitted_count"
	navPropLocations = "locations"
	navPropRender    = "result"
	navPropContent   = "content"
	navPropKind      = "kind"
	navPropPath      = "file_path"
	navPropStartLine = "start_line"
	navPropStartChar = "start_char"
	navPropEndLine   = "end_line"
	navPropEndChar   = "end_char"
)

// LSPNavigateTool exposes read-only code navigation (go-to definition,
// find references, go-to implementation, hover) to the model. It reuses the
// existing internal/code/lsp client server that writethrough already spawns
// via the shared lsp.Manager — it never spawns its own LSP server.
type LSPNavigateTool struct {
	tools.ToolDefaults
	manager *lsp.Manager
}

// NewLSPNavigateTool creates a new LSP navigation tool. The manager is the
// SAME seam the writethrough notifier drives (internal/daemon/components.go),
// so a server spawned for writethrough is reused, not duplicated.
func NewLSPNavigateTool(manager *lsp.Manager) (*LSPNavigateTool, error) {
	if manager == nil {
		return nil, fmt.Errorf("lsp.Manager cannot be nil")
	}
	return &LSPNavigateTool{manager: manager}, nil
}

func (t *LSPNavigateTool) Name() string { return "lsp_navigate" }

func (t *LSPNavigateTool) Category() string { return "code" }

// TimeoutMs declares the cooperative budget (contract B1/leaf B2): a hung LSP
// query expires into a structured TOOL_TIMEOUT instead of hanging the turn.
func (t *LSPNavigateTool) TimeoutMs() time.Duration { return lspTimeout }

// IsReadOnly reports the tool as a pure read: it never mutates files or server
// state.
func (t *LSPNavigateTool) IsReadOnly(map[string]any) bool { return true }

func (t *LSPNavigateTool) Description() string {
	return `Navigate a symbol at a position in a source file using the language server.
Supports goToDefinition, findReferences (includes the declaration),
goToImplementation, and hover. Read-only. Requires an LSP server for the
file's language to be configured and running. Positions are 1-based; the
workspace defaults to the session working directory.`
}

func (t *LSPNavigateTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			navPropOperation: {
				Type:        "string",
				Description: "The navigation operation to perform.",
				Enum: []string{
					string(opGoToDefinition),
					string(opFindReferences),
					string(opGoToImplementation),
					string(opHover),
				},
			},
			navPropFilePath: {
				Type:        "string",
				Description: "Path to the source file containing the symbol (absolute, or relative to the workspace).",
			},
			navPropLine: {
				Type:        "integer",
				Description: "Line number of the symbol (1-based).",
			},
			navPropChar: {
				Type:        "integer",
				Description: "Character offset of the symbol within the line (1-based, UTF-16).",
			},
			navPropWorkspace: {
				Type:        "string",
				Description: "Optional workspace root directory. Defaults to the session working directory.",
			},
		},
		Required: []string{navPropOperation, navPropFilePath, navPropLine, navPropChar},
	}
}

func (t *LSPNavigateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	opRaw, _ := args[navPropOperation].(string)
	op := lspNavigateOperation(opRaw)
	switch op {
	case opGoToDefinition, opFindReferences, opGoToImplementation, opHover:
	default:
		return nil, fmt.Errorf("lsp_navigate: unknown operation %q (expected goToDefinition|findReferences|goToImplementation|hover)", opRaw)
	}

	filePath, _ := args[navPropFilePath].(string)
	if filePath == "" {
		return nil, fmt.Errorf("lsp_navigate: %s is required", navPropFilePath)
	}
	lineRaw, ok := args[navPropLine].(float64)
	if !ok {
		return nil, fmt.Errorf("lsp_navigate: %s is required", navPropLine)
	}
	charRaw, ok := args[navPropChar].(float64)
	if !ok {
		return nil, fmt.Errorf("lsp_navigate: %s is required", navPropChar)
	}

	// Resolve the workspace root: explicit argument wins, else the session
	// working directory injected by the agent loop. Unresolvable -> a
	// detectable error (errors.Is-routeable to tools.ErrNoWorkingDir) naming
	// the required "workspace cwd", never a panic or an empty-guess path.
	workspace, _ := args[navPropWorkspace].(string)
	if workspace == "" {
		workspace = tools.WorkingDirFromContext(ctx)
	}
	if workspace == "" {
		return nil, fmt.Errorf("lsp_navigate: session workspace cwd required; pass a workspace argument or bind the session to a working directory: %w", tools.ErrNoWorkingDir)
	}

	absPath := filePath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(workspace, absPath)
	}
	absPath, err := filepath.Abs(absPath)
	if err != nil {
		return nil, fmt.Errorf("lsp_navigate: failed to resolve path: %w", err)
	}

	languageID := lsp.DetectLanguageID(absPath)
	srv, err := t.manager.GetServerForLanguage(ctx, languageID)
	if err != nil {
		return nil, fmt.Errorf("lsp_navigate: no LSP server for language %s: %w", languageID, err)
	}
	if srv.DocMgr == nil || srv.Client == nil {
		return nil, fmt.Errorf("lsp_navigate: LSP server for %s is not fully initialized", languageID)
	}

	// Capability guard: never hang on an operation the server lacks. A
	// goToImplementation against a server that does not advertise
	// implementationProvider returns an explicit result rather than a stall.
	if op == opGoToImplementation && !srv.Client.Capabilities().ImplementationProvider {
		return map[string]any{
			navPropFound:   false,
			navPropMessage: "goToImplementation not supported by server (server does not advertise implementationProvider)",
		}, nil
	}

	// Open the document so the server has it in sync (navigation needs the
	// file's content indexed).
	if _, err := srv.DocMgr.OpenFile(ctx, absPath); err != nil {
		return nil, fmt.Errorf("lsp_navigate: failed to open document: %w", err)
	}

	uri := lsp.PathToURI(absPath)
	// Convert the dsh one-based (UTF-16) positions to the client's zero-based
	// positions. Off-symbol positions are allowed to return no results.
	line := int(lineRaw) - 1
	char := int(charRaw) - 1

	if op == opHover {
		return t.hoverResult(ctx, srv, uri, line, char)
	}
	return t.locationsResult(ctx, srv, uri, line, char, op)
}

// locationsResult runs goToDefinition / findReferences / goToImplementation
// and renders the capped location list (contract T3: 100 locations, 16000
// rendered chars, truncation metadata).
func (t *LSPNavigateTool) locationsResult(ctx context.Context, srv *lsp.ServerInstance, uri string, line, char int, op lspNavigateOperation) (any, error) {
	var (
		locations []lsp.Location
		err       error
	)
	switch op {
	case opGoToDefinition:
		locations, err = srv.Client.GotoDefinition(ctx, uri, line, char)
	case opFindReferences:
		// dsh semantics: findReferences always includes the declaration.
		locations, err = srv.Client.FindReferences(ctx, uri, line, char, true)
	case opGoToImplementation:
		locations, err = srv.Client.GotoImplementation(ctx, uri, line, char)
	}
	if err != nil {
		return nil, fmt.Errorf("lsp_navigate: %s failed: %w", op, err)
	}

	total := len(locations)
	truncated := total > defaultMaxLocations
	if truncated {
		locations = locations[:defaultMaxLocations]
	}

	if len(locations) == 0 {
		return map[string]any{
			navPropFound:   false,
			navPropMessage: "no " + string(op) + " results found at this location",
		}, nil
	}

	items := make([]map[string]any, 0, len(locations))
	var sb strings.Builder
	for _, loc := range locations {
		path := lsp.URIToPath(loc.URI)
		items = append(items, map[string]any{
			navPropPath:      path,
			navPropStartLine: loc.Range.Start.Line + 1,
			navPropStartChar: loc.Range.Start.Character + 1,
			navPropEndLine:   loc.Range.End.Line + 1,
			navPropEndChar:   loc.Range.End.Character + 1,
		})
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "%s:%d:%d", path, loc.Range.Start.Line+1, loc.Range.Start.Character+1)
	}

	render := sb.String()
	if len(render) > defaultMaxResultChars {
		render = render[:defaultMaxResultChars]
		truncated = true
	}

	result := map[string]any{
		navPropFound:     true,
		navPropCount:     len(items),
		navPropLocations: items,
		navPropRender:    render,
		navPropTruncated: truncated,
	}
	if total > len(items) {
		result[navPropOmitted] = total - len(items)
	}
	return result, nil
}

// hoverResult runs the hover operation and returns the capped hover content.
func (t *LSPNavigateTool) hoverResult(ctx context.Context, srv *lsp.ServerInstance, uri string, line, char int) (any, error) {
	hover, err := srv.Client.Hover(ctx, uri, line, char)
	if err != nil {
		if errors.Is(err, lsp.ErrNotFound) {
			return map[string]any{
				navPropFound:   false,
				navPropMessage: "no hover information at this location",
			}, nil
		}
		return nil, fmt.Errorf("lsp_navigate: hover failed: %w", err)
	}

	content := hover.Contents.Value
	truncated := false
	if len(content) > defaultMaxResultChars {
		content = content[:defaultMaxResultChars]
		truncated = true
	}

	return map[string]any{
		navPropFound:     true,
		navPropContent:   content,
		navPropKind:      string(hover.Contents.Kind),
		navPropTruncated: truncated,
	}, nil
}

// Ensure the tool implements the Tool interface.
var _ tools.Tool = (*LSPNavigateTool)(nil)
