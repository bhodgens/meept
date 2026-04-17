package tools

import (
	"testing"
)

// Tests for constructors: confirm nil dependency returns a non-nil error
// rather than panicking.

func TestNewASTParseTool_ErrorOnNil(t *testing.T) {
	if _, err := NewASTParseTool(nil); err == nil {
		t.Fatal("expected error for nil parser, got nil")
	}
}

func TestNewASTSymbolsTool_ErrorOnNil(t *testing.T) {
	if _, err := NewASTSymbolsTool(nil); err == nil {
		t.Fatal("expected error for nil parser, got nil")
	}
}

func TestNewASTQueryTool_ErrorOnNil(t *testing.T) {
	if _, err := NewASTQueryTool(nil); err == nil {
		t.Fatal("expected error for nil parser, got nil")
	}
}

func TestNewLSPDefinitionTool_ErrorOnNil(t *testing.T) {
	if _, err := NewLSPDefinitionTool(nil); err == nil {
		t.Fatal("expected error for nil manager, got nil")
	}
}

func TestNewLSPReferencesTool_ErrorOnNil(t *testing.T) {
	if _, err := NewLSPReferencesTool(nil); err == nil {
		t.Fatal("expected error for nil manager, got nil")
	}
}

func TestNewLSPHoverTool_ErrorOnNil(t *testing.T) {
	if _, err := NewLSPHoverTool(nil); err == nil {
		t.Fatal("expected error for nil manager, got nil")
	}
}

func TestNewLSPSymbolsTool_ErrorOnNil(t *testing.T) {
	if _, err := NewLSPSymbolsTool(nil); err == nil {
		t.Fatal("expected error for nil manager, got nil")
	}
}

func TestNewLSPDiagnosticsTool_ErrorOnNil(t *testing.T) {
	if _, err := NewLSPDiagnosticsTool(nil); err == nil {
		t.Fatal("expected error for nil manager, got nil")
	}
}
