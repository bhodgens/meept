package lsp

// HasCapability checks if a server capability is enabled.
type HasCapability interface {
	HasDefinition() bool
	HasReferences() bool
	HasHover() bool
	HasDocumentSymbols() bool
	HasWorkspaceSymbols() bool
	HasDiagnostics() bool
}

// Capabilities wraps ServerCapabilities with convenience methods.
type Capabilities struct {
	ServerCapabilities
}

// NewCapabilities wraps server capabilities.
func NewCapabilities(caps ServerCapabilities) Capabilities {
	return Capabilities{ServerCapabilities: caps}
}

// HasDefinition returns true if go-to-definition is supported.
func (c Capabilities) HasDefinition() bool {
	return c.DefinitionProvider
}

// HasReferences returns true if find-references is supported.
func (c Capabilities) HasReferences() bool {
	return c.ReferencesProvider
}

// HasHover returns true if hover information is supported.
func (c Capabilities) HasHover() bool {
	return c.HoverProvider
}

// HasDocumentSymbols returns true if document symbols are supported.
func (c Capabilities) HasDocumentSymbols() bool {
	return c.DocumentSymbolProvider
}

// HasWorkspaceSymbols returns true if workspace symbols are supported.
func (c Capabilities) HasWorkspaceSymbols() bool {
	return c.WorkspaceSymbolProvider
}

// HasDiagnostics returns true if diagnostics are supported.
func (c Capabilities) HasDiagnostics() bool {
	return c.DiagnosticProvider != nil
}

// HasCodeActions returns true if code actions are supported.
func (c Capabilities) HasCodeActions() bool {
	return c.CodeActionProvider
}

// HasFormatting returns true if document formatting is supported.
func (c Capabilities) HasFormatting() bool {
	return c.DocumentFormattingProvider
}

// HasRename returns true if rename is supported.
func (c Capabilities) HasRename() bool {
	return c.RenameProvider
}

// TextDocumentSyncKindFromCapabilities extracts sync kind from capabilities.
func TextDocumentSyncKindFromCapabilities(caps ServerCapabilities) TextDocumentSyncKind {
	if caps.TextDocumentSync == nil {
		return TextDocumentSyncKindNone
	}

	switch v := caps.TextDocumentSync.(type) {
	case float64:
		return TextDocumentSyncKind(int(v))
	case int:
		return TextDocumentSyncKind(v)
	case map[string]any:
		// TextDocumentSyncOptions
		if change, ok := v["change"].(float64); ok {
			return TextDocumentSyncKind(int(change))
		}
	}

	return TextDocumentSyncKindNone
}

// RequiredCapabilities lists capabilities needed for specific features.
var RequiredCapabilities = map[string][]string{
	"definition":        {"textDocument/definition"},
	"references":        {"textDocument/references"},
	"hover":             {"textDocument/hover"},
	"document_symbols":  {"textDocument/documentSymbol"},
	"workspace_symbols": {"workspace/symbol"},
	"diagnostics":       {"textDocument/publishDiagnostics"},
}

// CheckCapabilities checks if a server supports required capabilities.
func CheckCapabilities(caps ServerCapabilities, feature string) bool {
	switch feature {
	case "definition":
		return caps.DefinitionProvider
	case "references":
		return caps.ReferencesProvider
	case "hover":
		return caps.HoverProvider
	case "document_symbols":
		return caps.DocumentSymbolProvider
	case "workspace_symbols":
		return caps.WorkspaceSymbolProvider
	case "diagnostics":
		return caps.DiagnosticProvider != nil
	default:
		return false
	}
}
