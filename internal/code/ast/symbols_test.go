package ast

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// Symbol extraction from Go code
// ---------------------------------------------------------------------------

func TestExtractSymbols_GoFunctions(t *testing.T) {
	skipIfNoGrammar(t, LangGo)

	pm := NewParserManager(ParserConfig{CacheEnabled: false})
	extractor := NewSymbolExtractor(pm)
	ctx := context.Background()

	source := []byte(`package main

import "fmt"

func Hello() {
	fmt.Println("hello")
}

func Add(a int, b int) int {
	return a + b
}

func privateHelper() {}
`)

	filter := DefaultSymbolFilter()
	symbols, err := extractor.ExtractFromSource(ctx, source, LangGo, filter)
	if err != nil {
		t.Fatalf("ExtractFromSource failed: %v", err)
	}

	// Find function symbols
	funcs := filterByKind(symbols, SymbolKindFunction)
	if len(funcs) < 2 {
		t.Errorf("expected at least 2 function symbols, got %d", len(funcs))
	}

	names := symbolNames(funcs)
	hasHello := false
	hasAdd := false
	hasPrivate := false
	for _, name := range names {
		switch name {
		case "Hello":
			hasHello = true
		case "Add":
			hasAdd = true
		case "privateHelper":
			hasPrivate = true
		}
	}

	if !hasHello {
		t.Error("expected to find function 'Hello'")
	}
	if !hasAdd {
		t.Error("expected to find function 'Add'")
	}
	if !hasPrivate {
		t.Error("expected to find function 'privateHelper' (default filter includes private)")
	}
}

func TestExtractSymbols_GoStructs(t *testing.T) {
	skipIfNoGrammar(t, LangGo)

	pm := NewParserManager(ParserConfig{CacheEnabled: false})
	extractor := NewSymbolExtractor(pm)
	ctx := context.Background()

	source := []byte(`package main

type Server struct {
	Name string
	Port int
}

type Handler interface {
	Handle() error
	Close() error
}
`)

	filter := DefaultSymbolFilter()
	symbols, err := extractor.ExtractFromSource(ctx, source, LangGo, filter)
	if err != nil {
		t.Fatalf("ExtractFromSource failed: %v", err)
	}

	structs := filterByKind(symbols, SymbolKindStruct)
	if len(structs) != 1 {
		t.Errorf("expected 1 struct symbol, got %d", len(structs))
	}
	if len(structs) > 0 && structs[0].Name != "Server" {
		t.Errorf("expected struct 'Server', got %q", structs[0].Name)
	}

	ifaces := filterByKind(symbols, SymbolKindInterface)
	if len(ifaces) != 1 {
		t.Errorf("expected 1 interface symbol, got %d", len(ifaces))
	}
	if len(ifaces) > 0 && ifaces[0].Name != "Handler" {
		t.Errorf("expected interface 'Handler', got %q", ifaces[0].Name)
	}
}

func TestExtractSymbols_GoMethods(t *testing.T) {
	skipIfNoGrammar(t, LangGo)

	pm := NewParserManager(ParserConfig{CacheEnabled: false})
	extractor := NewSymbolExtractor(pm)
	ctx := context.Background()

	source := []byte(`package main

type Counter struct {
	count int
}

func (c *Counter) Increment() {
	c.count++
}

func (c Counter) Value() int {
	return c.count
}
`)

	filter := DefaultSymbolFilter()
	symbols, err := extractor.ExtractFromSource(ctx, source, LangGo, filter)
	if err != nil {
		t.Fatalf("ExtractFromSource failed: %v", err)
	}

	methods := filterByKind(symbols, SymbolKindMethod)
	if len(methods) < 2 {
		t.Errorf("expected at least 2 method symbols, got %d", len(methods))
	}

	names := symbolNames(methods)
	hasIncrement := false
	hasValue := false
	for _, name := range names {
		switch name {
		case "Increment":
			hasIncrement = true
		case "Value":
			hasValue = true
		}
	}

	if !hasIncrement {
		t.Error("expected to find method 'Increment'")
	}
	if !hasValue {
		t.Error("expected to find method 'Value'")
	}
}

func TestExtractSymbols_GoConstants(t *testing.T) {
	skipIfNoGrammar(t, LangGo)

	pm := NewParserManager(ParserConfig{CacheEnabled: false})
	extractor := NewSymbolExtractor(pm)
	ctx := context.Background()

	source := []byte(`package main

const Version = "1.0.0"
const (
	MaxRetries = 3
	Timeout    = 30
)
`)

	filter := DefaultSymbolFilter()
	symbols, err := extractor.ExtractFromSource(ctx, source, LangGo, filter)
	if err != nil {
		t.Fatalf("ExtractFromSource failed: %v", err)
	}

	constants := filterByKind(symbols, SymbolKindConstant)
	if len(constants) < 1 {
		t.Errorf("expected at least 1 constant symbol, got %d", len(constants))
	}

	names := symbolNames(constants)
	hasVersion := false
	for _, name := range names {
		if name == "Version" {
			hasVersion = true
		}
	}
	if !hasVersion {
		t.Error("expected to find constant 'Version'")
	}
}

// ---------------------------------------------------------------------------
// Filter by symbol kind
// ---------------------------------------------------------------------------

func TestExtractSymbols_FilterByKind(t *testing.T) {
	skipIfNoGrammar(t, LangGo)

	pm := NewParserManager(ParserConfig{CacheEnabled: false})
	extractor := NewSymbolExtractor(pm)
	ctx := context.Background()

	source := []byte(`package main

type Server struct{}
func NewServer() *Server { return nil }
func (s *Server) Start() {}
`)

	// Filter to only functions
	filter := SymbolFilter{
		Kinds:          []SymbolKind{SymbolKindFunction},
		IncludePrivate: true,
	}
	symbols, err := extractor.ExtractFromSource(ctx, source, LangGo, filter)
	if err != nil {
		t.Fatalf("ExtractFromSource failed: %v", err)
	}

	for _, sym := range symbols {
		if sym.Kind != SymbolKindFunction {
			t.Errorf("expected only Function symbols, got %s (%s)", sym.Kind, sym.Name)
		}
	}

	// Filter to only structs
	filter = SymbolFilter{
		Kinds:          []SymbolKind{SymbolKindStruct},
		IncludePrivate: true,
	}
	symbols, err = extractor.ExtractFromSource(ctx, source, LangGo, filter)
	if err != nil {
		t.Fatalf("ExtractFromSource failed: %v", err)
	}

	for _, sym := range symbols {
		if sym.Kind != SymbolKindStruct {
			t.Errorf("expected only Struct symbols, got %s (%s)", sym.Kind, sym.Name)
		}
	}

	// Filter to only methods
	filter = SymbolFilter{
		Kinds:          []SymbolKind{SymbolKindMethod},
		IncludePrivate: true,
	}
	symbols, err = extractor.ExtractFromSource(ctx, source, LangGo, filter)
	if err != nil {
		t.Fatalf("ExtractFromSource failed: %v", err)
	}

	for _, sym := range symbols {
		if sym.Kind != SymbolKindMethod {
			t.Errorf("expected only Method symbols, got %s (%s)", sym.Kind, sym.Name)
		}
	}
}

func TestExtractSymbols_FilterExcludePrivate(t *testing.T) {
	skipIfNoGrammar(t, LangGo)

	pm := NewParserManager(ParserConfig{CacheEnabled: false})
	extractor := NewSymbolExtractor(pm)
	ctx := context.Background()

	source := []byte(`package main

func PublicFunc() {}
func privateFunc() {}
`)

	filter := SymbolFilter{
		Kinds:          []SymbolKind{SymbolKindFunction},
		IncludePrivate: false,
	}
	symbols, err := extractor.ExtractFromSource(ctx, source, LangGo, filter)
	if err != nil {
		t.Fatalf("ExtractFromSource failed: %v", err)
	}

	for _, sym := range symbols {
		if len(sym.Name) > 0 {
			first := sym.Name[0]
			if first >= 'a' && first <= 'z' {
				t.Errorf("expected no private symbols, got %q", sym.Name)
			}
		}
	}
}

func TestExtractSymbols_EmptyFilter(t *testing.T) {
	skipIfNoGrammar(t, LangGo)

	pm := NewParserManager(ParserConfig{CacheEnabled: false})
	extractor := NewSymbolExtractor(pm)
	ctx := context.Background()

	source := []byte(`package main

func Hello() {}
`)

	// Empty kinds filter should match everything
	filter := SymbolFilter{
		Kinds:          []SymbolKind{},
		IncludePrivate: true,
	}
	symbols, err := extractor.ExtractFromSource(ctx, source, LangGo, filter)
	if err != nil {
		t.Fatalf("ExtractFromSource failed: %v", err)
	}

	if len(symbols) == 0 {
		t.Error("expected symbols with empty kinds filter")
	}
}

func TestExtractSymbols_EmptySource(t *testing.T) {
	skipIfNoGrammar(t, LangGo)

	pm := NewParserManager(ParserConfig{CacheEnabled: false})
	extractor := NewSymbolExtractor(pm)
	ctx := context.Background()

	filter := DefaultSymbolFilter()
	symbols, err := extractor.ExtractFromSource(ctx, []byte(""), LangGo, filter)
	if err != nil {
		t.Fatalf("ExtractFromSource failed: %v", err)
	}

	if len(symbols) != 0 {
		t.Errorf("expected 0 symbols from empty source, got %d", len(symbols))
	}
}

// ---------------------------------------------------------------------------
// Symbol type helpers
// ---------------------------------------------------------------------------

func TestSymbolKind_String(t *testing.T) {
	tests := []struct {
		kind SymbolKind
		want string
	}{
		{SymbolKindFunction, "function"},
		{SymbolKindMethod, "method"},
		{SymbolKindStruct, "struct"},
		{SymbolKindInterface, "interface"},
		{SymbolKindClass, "class"},
		{SymbolKindVariable, "variable"},
		{SymbolKindConstant, "constant"},
		{SymbolKindModule, "module"},
		{SymbolKindEnum, "enum"},
		{SymbolKindField, "field"},
		{SymbolKindProperty, "property"},
		{SymbolKindConstructor, "constructor"},
		{SymbolKindEnumMember, "enum_member"},
		{SymbolKind(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.kind.String()
			if got != tt.want {
				t.Errorf("SymbolKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestRange_Contains(t *testing.T) {
	r := Range{StartLine: 2, StartColumn: 5, EndLine: 4, EndColumn: 10}

	if !r.Contains(2, 5) {
		t.Error("should contain start position")
	}
	if !r.Contains(3, 0) {
		t.Error("should contain middle position")
	}
	if !r.Contains(4, 10) {
		t.Error("should contain end position")
	}
	if r.Contains(1, 0) {
		t.Error("should not contain position before range")
	}
	if r.Contains(5, 0) {
		t.Error("should not contain position after range")
	}
	if r.Contains(2, 4) {
		t.Error("should not contain position before start column on start line")
	}
	if r.Contains(4, 11) {
		t.Error("should not contain position after end column on end line")
	}
}

// ---------------------------------------------------------------------------
// DefaultSymbolFilter
// ---------------------------------------------------------------------------

func TestDefaultSymbolFilter(t *testing.T) {
	filter := DefaultSymbolFilter()
	if len(filter.Kinds) == 0 {
		t.Error("default filter should have kinds")
	}
	if !filter.IncludePrivate {
		t.Error("default filter should include private symbols")
	}
	if filter.MaxDepth != 0 {
		t.Error("default filter should have unlimited depth")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func filterByKind(symbols []Symbol, kind SymbolKind) []Symbol {
	var filtered []Symbol
	for _, s := range symbols {
		if s.Kind == kind {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

func symbolNames(symbols []Symbol) []string {
	names := make([]string, len(symbols))
	for i, s := range symbols {
		names[i] = s.Name
	}
	return names
}
