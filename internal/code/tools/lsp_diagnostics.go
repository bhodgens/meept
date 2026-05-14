package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/code/lsp"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// LSPDiagnosticsTool gets diagnostics (errors, warnings) for a file.
type LSPDiagnosticsTool struct {
	manager     *lsp.Manager
	mu          sync.RWMutex
	diagnostics map[string][]lsp.Diagnostic // URI -> diagnostics
}

// NewLSPDiagnosticsTool creates a new LSP diagnostics tool.
func NewLSPDiagnosticsTool(manager *lsp.Manager) (*LSPDiagnosticsTool, error) {
	if manager == nil {
		return nil, fmt.Errorf("lsp.Manager cannot be nil")
	}
	return &LSPDiagnosticsTool{
		manager:     manager,
		diagnostics: make(map[string][]lsp.Diagnostic),
	}, nil
}

func (t *LSPDiagnosticsTool) Name() string { return "lsp_diagnostics" }

func (t *LSPDiagnosticsTool) Description() string {
	return `Get diagnostics (errors, warnings, hints) for a source file.
Opens the file in the LSP server and waits for diagnostic results.
Requires an LSP server for the file's language to be configured and running.`
}

func (t *LSPDiagnosticsTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: SchemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			SchemaPropFilePath: {
				Type:        SchemaTypeString,
				Description: "Path to the source file to analyze.",
			},
			"severity": {
				Type:        SchemaTypeString,
				Description: "Minimum severity to include: 'error', 'warning', 'information', 'hint'. Default: 'warning'.",
			},
			"wait_ms": {
				Type:        SchemaTypeInteger,
				Description: "Milliseconds to wait for diagnostics after opening file (default: 1000).",
			},
		},
		Required: []string{SchemaPropFilePath},
	}
}

func (t *LSPDiagnosticsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	filePath, _ := args[SchemaPropFilePath].(string)
	if filePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	minSeverity := lsp.DiagnosticSeverityWarning
	if sev, ok := args["severity"].(string); ok {
		minSeverity = parseSeverity(sev)
	}

	waitMs := 1000
	if w, ok := args["wait_ms"].(float64); ok {
		waitMs = int(w)
	}

	// Get absolute path
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	// Detect language and get server
	languageID := lsp.DetectLanguageID(absPath)
	srv, err := t.manager.GetServerForLanguage(ctx, languageID)
	if err != nil {
		return nil, fmt.Errorf("no LSP server for language %s: %w", languageID, err)
	}
	if srv.DocMgr == nil || srv.Client == nil {
		return nil, fmt.Errorf("LSP server for %s is not fully initialized", languageID)
	}

	uri := lsp.PathToURI(absPath)

	// Set up diagnostics handler
	diagChan := make(chan struct{}, 1)
	srv.Client.OnNotification("textDocument/publishDiagnostics", func(method string, params json.RawMessage) {
		var diagParams lsp.PublishDiagnosticsParams
		if err := json.Unmarshal(params, &diagParams); err != nil {
			return
		}
		if diagParams.URI == uri {
			t.mu.Lock()
			t.diagnostics[uri] = diagParams.Diagnostics
			t.mu.Unlock()
			select {
			case diagChan <- struct{}{}:
			default:
			}
		}
	})

	// Open the document
	if _, err := srv.DocMgr.OpenFile(ctx, absPath); err != nil {
		return nil, fmt.Errorf("failed to open document: %w", err)
	}

	// Wait for diagnostics
	select {
	case <-diagChan:
		// Received diagnostics
	case <-time.After(time.Duration(waitMs) * time.Millisecond):
		// Timeout - continue with whatever we have
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Get diagnostics
	t.mu.RLock()
	diags := t.diagnostics[uri]
	t.mu.RUnlock()

	// Filter by severity
	filtered := filterBySeverity(diags, minSeverity)

	// Convert to result format
	result := make([]map[string]any, 0, len(filtered))
	for _, d := range filtered {
		item := map[string]any{
			SchemaPropMessage:    d.Message,
			"severity":   severityToString(d.Severity),
			SchemaPropStartLine: d.Range.Start.Line,
			SchemaPropStartChar: d.Range.Start.Character,
			SchemaPropEndLine:   d.Range.End.Line,
			SchemaPropEndChar:   d.Range.End.Character,
		}
		if d.Code != "" {
			item["code"] = d.Code
		}
		if d.Source != "" {
			item["source"] = d.Source
		}
		result = append(result, item)
	}

	// Count by severity
	counts := countBySeverity(filtered)

	return map[string]any{
		SchemaPropFilePath:   filePath,
		"diagnostics": result,
		"total_count": len(result),
		"counts":      counts,
	}, nil
}

func parseSeverity(s string) lsp.DiagnosticSeverity {
	switch s {
	case "error":
		return lsp.DiagnosticSeverityError
	case "warning":
		return lsp.DiagnosticSeverityWarning
	case "information":
		return lsp.DiagnosticSeverityInformation
	case "hint":
		return lsp.DiagnosticSeverityHint
	default:
		return lsp.DiagnosticSeverityWarning
	}
}

func severityToString(s lsp.DiagnosticSeverity) string {
	switch s {
	case lsp.DiagnosticSeverityError:
		return "error"
	case lsp.DiagnosticSeverityWarning:
		return "warning"
	case lsp.DiagnosticSeverityInformation:
		return "information"
	case lsp.DiagnosticSeverityHint:
		return "hint"
	default:
		return "unknown"
	}
}

func filterBySeverity(diags []lsp.Diagnostic, minSeverity lsp.DiagnosticSeverity) []lsp.Diagnostic {
	result := make([]lsp.Diagnostic, 0, len(diags))
	for _, d := range diags {
		if d.Severity <= minSeverity {
			result = append(result, d)
		}
	}
	return result
}

func countBySeverity(diags []lsp.Diagnostic) map[string]int {
	counts := map[string]int{
		"error":       0,
		"warning":     0,
		"information": 0,
		"hint":        0,
	}
	for _, d := range diags {
		key := severityToString(d.Severity)
		counts[key]++
	}
	return counts
}

// Ensure tool implements the Tool interface
var _ tools.Tool = (*LSPDiagnosticsTool)(nil)
