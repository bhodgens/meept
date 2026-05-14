// Package lsp provides LSP client functionality.
package lsp

import "encoding/json"

// Position represents a position in a text document (0-indexed).
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range represents a range in a text document.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location represents a location inside a resource.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// LocationLink represents a link between a source and a target location.
type LocationLink struct {
	OriginSelectionRange *Range `json:"originSelectionRange,omitempty"`
	TargetURI            string `json:"targetUri"`
	TargetRange          Range  `json:"targetRange"`
	TargetSelectionRange Range  `json:"targetSelectionRange"`
}

// TextDocumentIdentifier identifies a text document.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// TextDocumentPositionParams is a parameter for position-based requests.
type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// TextDocumentItem represents an item to transfer a text document from the client to the server.
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// VersionedTextDocumentIdentifier identifies a versioned text document.
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

// TextDocumentContentChangeEvent describes changes to a text document.
type TextDocumentContentChangeEvent struct {
	Range       *Range `json:"range,omitempty"`
	RangeLength *int   `json:"rangeLength,omitempty"`
	Text        string `json:"text"`
}

// DidOpenTextDocumentParams is the parameter for textDocument/didOpen.
type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// DidCloseTextDocumentParams is the parameter for textDocument/didClose.
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// DidChangeTextDocumentParams is the parameter for textDocument/didChange.
type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// DiagnosticSeverity represents the severity of a diagnostic.
type DiagnosticSeverity int

const (
	DiagnosticSeverityError       DiagnosticSeverity = 1
	DiagnosticSeverityWarning     DiagnosticSeverity = 2
	DiagnosticSeverityInformation DiagnosticSeverity = 3
	DiagnosticSeverityHint        DiagnosticSeverity = 4
)

func (s DiagnosticSeverity) String() string {
	switch s {
	case DiagnosticSeverityError:
		return "error"
	case DiagnosticSeverityWarning:
		return "warning"
	case DiagnosticSeverityInformation:
		return "information"
	case DiagnosticSeverityHint:
		return "hint"
	default:
		return "unknown"
	}
}

// Diagnostic represents a diagnostic, such as a compiler error.
type Diagnostic struct {
	Range    Range              `json:"range"`
	Severity DiagnosticSeverity `json:"severity,omitempty"`
	Code     string             `json:"code,omitempty"`
	Source   string             `json:"source,omitempty"`
	Message  string             `json:"message"`
}

// PublishDiagnosticsParams is the parameter for textDocument/publishDiagnostics.
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     *int         `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// SymbolKind represents the kind of a symbol.
type SymbolKind int

const (
	SymbolKindFile          SymbolKind = 1
	SymbolKindModule        SymbolKind = 2
	SymbolKindNamespace     SymbolKind = 3
	SymbolKindPackage       SymbolKind = 4
	SymbolKindClass         SymbolKind = 5
	SymbolKindMethod        SymbolKind = 6
	SymbolKindProperty      SymbolKind = 7
	SymbolKindField         SymbolKind = 8
	SymbolKindConstructor   SymbolKind = 9
	SymbolKindEnum          SymbolKind = 10
	SymbolKindInterface     SymbolKind = 11
	SymbolKindFunction      SymbolKind = 12
	SymbolKindVariable      SymbolKind = 13
	SymbolKindConstant      SymbolKind = 14
	SymbolKindString        SymbolKind = 15
	SymbolKindNumber        SymbolKind = 16
	SymbolKindBoolean       SymbolKind = 17
	SymbolKindArray         SymbolKind = 18
	SymbolKindObject        SymbolKind = 19
	SymbolKindKey           SymbolKind = 20
	SymbolKindNull          SymbolKind = 21
	SymbolKindEnumMember    SymbolKind = 22
	SymbolKindStruct        SymbolKind = 23
	SymbolKindEvent         SymbolKind = 24
	SymbolKindOperator      SymbolKind = 25
	SymbolKindTypeParameter SymbolKind = 26
)

func (k SymbolKind) String() string {
	names := map[SymbolKind]string{
		SymbolKindFile: "file", SymbolKindModule: "module", SymbolKindNamespace: "namespace",
		SymbolKindPackage: "package", SymbolKindClass: "class", SymbolKindMethod: "method",
		SymbolKindProperty: "property", SymbolKindField: "field", SymbolKindConstructor: "constructor",
		SymbolKindEnum: "enum", SymbolKindInterface: "interface", SymbolKindFunction: "function",
		SymbolKindVariable: "variable", SymbolKindConstant: "constant", SymbolKindString: "string",
		SymbolKindNumber: "number", SymbolKindBoolean: "boolean", SymbolKindArray: "array",
		SymbolKindObject: "object", SymbolKindKey: "key", SymbolKindNull: "null",
		SymbolKindEnumMember: "enum_member", SymbolKindStruct: "struct", SymbolKindEvent: "event",
		SymbolKindOperator: "operator", SymbolKindTypeParameter: "type_parameter",
	}
	if name, ok := names[k]; ok {
		return name
	}
	return "unknown"
}

// SymbolInformation represents information about a symbol.
type SymbolInformation struct {
	Name          string     `json:"name"`
	Kind          SymbolKind `json:"kind"`
	Deprecated    bool       `json:"deprecated,omitempty"`
	Location      Location   `json:"location"`
	ContainerName string     `json:"containerName,omitempty"`
}

// DocumentSymbol represents a document symbol with hierarchy.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           SymbolKind       `json:"kind"`
	Deprecated     bool             `json:"deprecated,omitempty"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// WorkspaceSymbolParams is the parameter for workspace/symbol.
type WorkspaceSymbolParams struct {
	Query string `json:"query"`
}

// Hover represents the result of a hover request.
type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// MarkupKind describes the content type for markup.
type MarkupKind string

const (
	MarkupKindPlainText MarkupKind = "plaintext"
	MarkupKindMarkdown  MarkupKind = "markdown"
)

// MarkupContent represents a string value with markup.
type MarkupContent struct {
	Kind  MarkupKind `json:"kind"`
	Value string     `json:"value"`
}

// ReferenceParams is the parameter for textDocument/references.
type ReferenceParams struct {
	TextDocumentPositionParams
	Context ReferenceContext `json:"context"`
}

// ReferenceContext determines how references are resolved.
type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

// InitializeParams is the parameter for initialize.
type InitializeParams struct {
	ProcessID             int                `json:"processId"`
	RootURI               string             `json:"rootUri"`
	Capabilities          ClientCapabilities `json:"capabilities"`
	InitializationOptions any                `json:"initializationOptions,omitempty"`
}

// ClientCapabilities represents the client's capabilities.
type ClientCapabilities struct {
	TextDocument TextDocumentClientCapabilities `json:"textDocument,omitempty,omitzero"` //nolint:modernize // keep omitempty for backward compat
	Workspace    WorkspaceClientCapabilities    `json:"workspace,omitempty,omitzero"`    //nolint:modernize // keep omitempty for backward compat
}

// TextDocumentClientCapabilities represents text document capabilities.
type TextDocumentClientCapabilities struct {
	Synchronization TextDocumentSyncClientCapabilities `json:"synchronization,omitempty,omitzero"` //nolint:modernize // keep omitempty for backward compat
}

// TextDocumentSyncClientCapabilities represents sync capabilities.
type TextDocumentSyncClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	WillSave            bool `json:"willSave,omitempty"`
	WillSaveWaitUntil   bool `json:"willSaveWaitUntil,omitempty"`
	DidSave             bool `json:"didSave,omitempty"`
}

// WorkspaceClientCapabilities represents workspace capabilities.
type WorkspaceClientCapabilities struct {
	WorkspaceFolders bool `json:"workspaceFolders,omitempty"`
}

// InitializeResult is the result of initialize.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
}

// ServerCapabilities represents the server's capabilities.
type ServerCapabilities struct {
	TextDocumentSync           any  `json:"textDocumentSync,omitempty"`
	HoverProvider              bool `json:"hoverProvider,omitempty"`
	DefinitionProvider         bool `json:"definitionProvider,omitempty"`
	ReferencesProvider         bool `json:"referencesProvider,omitempty"`
	DocumentSymbolProvider     bool `json:"documentSymbolProvider,omitempty"`
	WorkspaceSymbolProvider    bool `json:"workspaceSymbolProvider,omitempty"`
	CodeActionProvider         bool `json:"codeActionProvider,omitempty"`
	DocumentFormattingProvider bool `json:"documentFormattingProvider,omitempty"`
	RenameProvider             bool `json:"renameProvider,omitempty"`
	DiagnosticProvider         any  `json:"diagnosticProvider,omitempty"`
}

// TextDocumentSyncKind defines how text documents are synced.
type TextDocumentSyncKind int

const (
	TextDocumentSyncKindNone        TextDocumentSyncKind = 0
	TextDocumentSyncKindFull        TextDocumentSyncKind = 1
	TextDocumentSyncKindIncremental TextDocumentSyncKind = 2
)

// JSON-RPC types

// JSONRPCRequest represents a JSON-RPC request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC error.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *JSONRPCError) Error() string {
	return e.Message
}

// JSON-RPC error codes
const (
	ErrorCodeParseError     = -32700
	ErrorCodeInvalidRequest = -32600
	ErrorCodeMethodNotFound = -32601
	ErrorCodeInvalidParams  = -32602
	ErrorCodeInternalError  = -32603
	ErrorCodeServerNotInit  = -32002
	ErrorCodeUnknownError   = -32001
	ErrorCodeRequestFailed  = -32803
)
