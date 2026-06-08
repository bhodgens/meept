// Package ast provides AST parsing capabilities using tree-sitter.
package ast

import (
	"encoding/json"
)

// Tree-sitter node type constants.
const (
	NodeFunctionDeclaration = "function_declaration"
	NodeFunctionDefinition  = "function_definition"
	NodeClassDeclaration    = "class_declaration"
	NodeMethodDeclaration   = "method_declaration"
)

// Tree-sitter query constants.
const (
	QueryComment = "(comment) @comment"
)

// Language represents a supported programming language.
type Language string

const (
	LangGo         Language = "go"
	LangPython     Language = "python"
	LangTypeScript Language = "typescript"
	LangJavaScript Language = "javascript"
	LangRust       Language = "rust"
	LangC          Language = "c"
	LangCpp        Language = "cpp"
	LangJava       Language = "java"
	LangRuby       Language = "ruby"
	LangYAML       Language = "yaml"
	LangTOML       Language = "toml"
	LangBash       Language = "bash"
	LangMarkdown   Language = "markdown"
	LangHTML       Language = "html"
	LangCSS        Language = "css"
	LangSQL        Language = "sql"
	LangUnknown    Language = ""
)

// SymbolKind matches LSP SymbolKind values for compatibility.
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

// String returns the human-readable name for a SymbolKind.
func (k SymbolKind) String() string {
	names := map[SymbolKind]string{
		SymbolKindFile:          "file",
		SymbolKindModule:        "module",
		SymbolKindNamespace:     "namespace",
		SymbolKindPackage:       "package",
		SymbolKindClass:         "class",
		SymbolKindMethod:        "method",
		SymbolKindProperty:      "property",
		SymbolKindField:         "field",
		SymbolKindConstructor:   "constructor",
		SymbolKindEnum:          "enum",
		SymbolKindInterface:     "interface",
		SymbolKindFunction:      "function",
		SymbolKindVariable:      "variable",
		SymbolKindConstant:      "constant",
		SymbolKindString:        "string",
		SymbolKindNumber:        "number",
		SymbolKindBoolean:       "boolean",
		SymbolKindArray:         "array",
		SymbolKindObject:        "object",
		SymbolKindKey:           "key",
		SymbolKindNull:          "null",
		SymbolKindEnumMember:    "enum_member",
		SymbolKindStruct:        "struct",
		SymbolKindEvent:         "event",
		SymbolKindOperator:      "operator",
		SymbolKindTypeParameter: "type_parameter",
	}
	if name, ok := names[k]; ok {
		return name
	}
	return "unknown"
}

// Range represents a source code range with line and column positions.
// Lines and columns are 0-indexed.
type Range struct {
	StartLine   int `json:"start_line"`
	StartColumn int `json:"start_column"`
	EndLine     int `json:"end_line"`
	EndColumn   int `json:"end_column"`
}

// Contains checks if this range contains a position.
func (r Range) Contains(line, column int) bool {
	if line < r.StartLine || line > r.EndLine {
		return false
	}
	if line == r.StartLine && column < r.StartColumn {
		return false
	}
	if line == r.EndLine && column > r.EndColumn {
		return false
	}
	return true
}

// Symbol represents a code symbol (function, class, variable, etc).
type Symbol struct {
	Name       string     `json:"name"`
	Kind       SymbolKind `json:"kind"`
	KindName   string     `json:"kind_name,omitempty"`
	Language   Language   `json:"language,omitempty"`
	FilePath   string     `json:"file_path,omitempty"`
	Range      Range      `json:"range"`
	Signature  string     `json:"signature,omitempty"`
	DocComment string     `json:"doc_comment,omitempty"`
	Children   []Symbol   `json:"children,omitempty"`
}

// MarshalJSON implements json.Marshaler with kind_name populated.
func (s Symbol) MarshalJSON() ([]byte, error) {
	type Alias Symbol
	return json.Marshal(&struct {
		KindName string `json:"kind_name"`
		*Alias
	}{
		KindName: s.Kind.String(),
		Alias:    (*Alias)(&s),
	})
}

// Node represents a parsed AST node.
type Node struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Range    Range  `json:"range"`
	IsNamed  bool   `json:"is_named"`
	Children []Node `json:"children,omitempty"`
}

// ParseResult holds the result of parsing source code.
type ParseResult struct {
	Language Language `json:"language"`
	FilePath string   `json:"file_path,omitempty"`
	RootNode Node     `json:"root"`
	Errors   []string `json:"errors,omitempty"`
}

// QueryMatch represents a match from a tree-sitter query.
type QueryMatch struct {
	PatternIndex int            `json:"pattern_index"`
	Captures     []QueryCapture `json:"captures"`
}

// QueryCapture represents a captured node in a query match.
type QueryCapture struct {
	Name string `json:"name"`
	Node Node   `json:"node"`
}

// QueryResult holds the result of running a tree-sitter query.
type QueryResult struct {
	Query   string       `json:"query"`
	Matches []QueryMatch `json:"matches"`
	Count   int          `json:"count"`
	RuleID  string       `json:"rule_id,omitempty"` // Set when using YAML rules
}

// ContextMatch wraps a QueryMatch with surrounding source context.
type ContextMatch struct {
	Match          QueryMatch `json:"match"`
	BeforeContext  []string   `json:"before_context,omitempty"`
	AfterContext   []string   `json:"after_context,omitempty"`
	MatchedLines   []string   `json:"matched_lines,omitempty"`
}

// SymbolFilter specifies which symbol kinds to include.
type SymbolFilter struct {
	// Kinds lists the symbol kinds to include. Empty means all kinds.
	Kinds []SymbolKind
	// IncludePrivate includes private/unexported symbols.
	IncludePrivate bool
	// MaxDepth limits the nesting depth (0 = unlimited).
	MaxDepth int
}

// DefaultSymbolFilter returns a filter that includes all common symbol types.
func DefaultSymbolFilter() SymbolFilter {
	return SymbolFilter{
		Kinds: []SymbolKind{
			SymbolKindFunction,
			SymbolKindMethod,
			SymbolKindClass,
			SymbolKindInterface,
			SymbolKindStruct,
			SymbolKindEnum,
			SymbolKindConstant,
			SymbolKindVariable,
			SymbolKindModule,
		},
		IncludePrivate: true,
		MaxDepth:       0,
	}
}
