package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/code/lsp"
	"github.com/caimlas/meept/internal/config"
)

// DiagnosticsSummary holds a summary of LSP diagnostics for a file.
type DiagnosticsSummary struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Info     int `json:"info"`
	Hints    int `json:"hints"`
}

// FormatResult holds the result of LSP formatting on write.
type FormatResult struct {
	EditsApplied int  `json:"edits_applied"`
	LinesChanged int  `json:"lines_changed"`
	Reformatted  bool `json:"reformatted"`
}

// WritethroughResult holds the combined result of LSP writethrough operations.
type WritethroughResult struct {
	Diagnostics *DiagnosticsSummary `json:"diagnostics,omitempty"`
	Formatting  *FormatResult       `json:"formatting,omitempty"`
}

// String returns a human-readable summary of the writethrough result.
func (r *WritethroughResult) String() string {
	var parts []string
	if r.Diagnostics != nil {
		parts = append(parts, fmt.Sprintf("diagnostics: %d errors, %d warnings, %d info",
			r.Diagnostics.Errors, r.Diagnostics.Warnings, r.Diagnostics.Info))
	}
	if r.Formatting != nil && r.Formatting.Reformatted {
		parts = append(parts, fmt.Sprintf("formatted: %d edits applied (%d lines changed)",
			r.Formatting.EditsApplied, r.Formatting.LinesChanged))
	}
	if len(parts) == 0 {
		return ""
	}
	return " [" + strings.Join(parts, "; ") + "]"
}

// LSPWriteNotifier is the interface that file tools use to notify LSP of writes.
// Implementations are optional; a nil-safe no-op is used when LSP is disabled.
type LSPWriteNotifier interface {
	// NotifyWrite is called after a file has been written to disk.
	// It handles LSP document sync, optional formatting, and diagnostics collection.
	NotifyWrite(ctx context.Context, filePath string, content string) *WritethroughResult
}

// lspWriteNotifier is the concrete implementation wrapping the LSP manager.
type lspWriteNotifier struct {
	manager            *lsp.Manager
	formatOnWrite      bool
	diagnosticsOnWrite bool
	diagnosticsTimeout time.Duration
	logger             *slog.Logger
}

// NewLSPWriteNotifier creates a new LSP write notifier.
// Returns nil if manager is nil, following the typed-nil guard pattern.
func NewLSPWriteNotifier(manager *lsp.Manager, cfg config.LSPConfig, logger *slog.Logger) LSPWriteNotifier {
	if manager == nil {
		return nil
	}

	timeout := time.Duration(cfg.DiagnosticsTimeout) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	return &lspWriteNotifier{
		manager:            manager,
		formatOnWrite:      cfg.FormatOnWrite,
		diagnosticsOnWrite: cfg.DiagnosticsOnWrite,
		diagnosticsTimeout: timeout,
		logger:             logger,
	}
}

// NotifyWrite notifies the LSP server about a file write, optionally formatting
// and collecting diagnostics.
func (n *lspWriteNotifier) NotifyWrite(ctx context.Context, filePath string, content string) *WritethroughResult {
	// Detect language and find a server
	languageID := lsp.DetectLanguageID(filePath)
	srv, err := n.manager.GetServerForLanguage(ctx, languageID)
	if err != nil {
		// No server for this language -- silently skip
		return nil
	}
	if srv.DocMgr == nil || srv.Client == nil {
		return nil
	}

	absPath, err := absPath(filePath)
	if err != nil {
		return nil
	}

	result := &WritethroughResult{}

	// Ensure the document is open with the LSP server
	if _, err := srv.DocMgr.OpenFile(ctx, absPath); err != nil {
		n.logger.Debug("LSP writethrough: failed to open document", "path", absPath, "error", err)
		return nil
	}

	// Sync content: send didChange
	if err := srv.DocMgr.UpdateFile(ctx, absPath, content); err != nil {
		n.logger.Debug("LSP writethrough: failed to update document", "path", absPath, "error", err)
	}

	// Format on write
	if n.formatOnWrite {
		fr := n.formatFile(ctx, srv, absPath)
		if fr != nil {
			result.Formatting = fr
			// If formatting changed the file, re-read the content for diagnostics
			if fr.Reformatted {
				if updated, err := os.ReadFile(absPath); err == nil {
					content = string(updated)
				}
			}
		}
	}

	// Diagnostics on write
	if n.diagnosticsOnWrite {
		ds := n.collectDiagnostics(ctx, srv, absPath, content)
		result.Diagnostics = ds
	}

	return result
}

// formatFile requests LSP formatting and applies edits.
func (n *lspWriteNotifier) formatFile(ctx context.Context, srv *lsp.ServerInstance, absPath string) *FormatResult {
	uri := lsp.PathToURI(absPath)
	edits, err := srv.Client.Formatting(ctx, uri)
	if err != nil {
		n.logger.Debug("LSP writethrough: formatting failed", "path", absPath, "error", err)
		return nil
	}

	if len(edits) == 0 {
		return &FormatResult{EditsApplied: 0, LinesChanged: 0, Reformatted: false}
	}

	// Apply formatting edits
	if err := applyFormattingEdits(absPath, edits); err != nil {
		n.logger.Debug("LSP writethrough: failed to apply formatting edits", "path", absPath, "error", err)
		return nil
	}

	// Notify LSP of the formatting change
	if updated, err := os.ReadFile(absPath); err == nil {
		_ = srv.DocMgr.UpdateFile(ctx, absPath, string(updated))
	}

	// Count lines changed
	linesChanged := make(map[int]bool)
	for _, edit := range edits {
		for line := edit.Range.Start.Line; line <= edit.Range.End.Line; line++ {
			linesChanged[line] = true
		}
	}

	return &FormatResult{
		EditsApplied: len(edits),
		LinesChanged: len(linesChanged),
		Reformatted:  true,
	}
}

// collectDiagnostics waits for and collects diagnostics for a file.
func (n *lspWriteNotifier) collectDiagnostics(ctx context.Context, srv *lsp.ServerInstance, absPath string, content string) *DiagnosticsSummary {
	uri := lsp.PathToURI(absPath)

	// Set up a one-shot diagnostics listener using sync.Once to guarantee
	// the handler fires at most once, then unregisters itself to prevent
	// listener accumulation across repeated calls.
	diagChan := make(chan []lsp.Diagnostic, 1)
	var once sync.Once

	srv.Client.OnNotification("textDocument/publishDiagnostics", func(method string, params json.RawMessage) {
		var diagParams lsp.PublishDiagnosticsParams
		if err := json.Unmarshal(params, &diagParams); err != nil {
			return
		}
		if diagParams.URI == uri {
			once.Do(func() {
				diagChan <- diagParams.Diagnostics
				// Replace ourselves with a no-op so subsequent notifications
				// for this URI don't accumulate dead closures.
				srv.Client.OnNotification("textDocument/publishDiagnostics", func(string, json.RawMessage) {})
			})
		}
	})

	// Send a didSave to trigger fresh diagnostics
	_ = srv.Client.Notify(ctx, "textDocument/didSave", map[string]any{
		"textDocument": map[string]any{
			"uri": uri,
		},
	})

	// Wait for diagnostics with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, n.diagnosticsTimeout)
	defer cancel()

	var diags []lsp.Diagnostic
	select {
	case diags = <-diagChan:
		// Got diagnostics
	case <-timeoutCtx.Done():
		// Timeout -- use whatever we have cached
		n.logger.Debug("LSP writethrough: diagnostics timeout", "path", absPath)
	}

	if diags == nil {
		return nil
	}

	summary := &DiagnosticsSummary{}
	for _, d := range diags {
		switch d.Severity {
		case lsp.DiagnosticSeverityError:
			summary.Errors++
		case lsp.DiagnosticSeverityWarning:
			summary.Warnings++
		case lsp.DiagnosticSeverityInformation:
			summary.Info++
		case lsp.DiagnosticSeverityHint:
			summary.Hints++
		default:
			summary.Warnings++ // Default unknown severity to warnings
		}
	}

	return summary
}

// applyFormattingEdits applies text edits to a file on disk.
func applyFormattingEdits(filePath string, edits []lsp.TextEdit) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(content), "\n")

	// Apply edits in reverse order to preserve positions
	for i := len(edits) - 1; i >= 0; i-- {
		edit := edits[i]
		startLine := edit.Range.Start.Line
		startChar := edit.Range.Start.Character
		endLine := edit.Range.End.Line
		endChar := edit.Range.End.Character

		if startLine >= len(lines) || endLine > len(lines) {
			continue // Skip out-of-bounds edits
		}

		var before, after strings.Builder
		for l := 0; l < startLine; l++ {
			if l > 0 {
				before.WriteString("\n")
			}
			before.WriteString(lines[l])
		}
		if startLine < len(lines) {
			before.WriteString(lines[startLine][:min(startChar, len(lines[startLine]))])
		}

		if endLine < len(lines) {
			after.WriteString(lines[endLine][min(endChar, len(lines[endLine])):])
		}
		for l := endLine + 1; l < len(lines); l++ {
			after.WriteString("\n")
			after.WriteString(lines[l])
		}

		newContent := before.String() + edit.NewText + after.String()
		lines = strings.Split(newContent, "\n")
	}

	return os.WriteFile(filePath, []byte(strings.Join(lines, "\n")), 0o644)
}

// absPath resolves a file path to absolute, expanding ~ and resolving relative paths.
func absPath(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = home + path[1:]
	}
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}
