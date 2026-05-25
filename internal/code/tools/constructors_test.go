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

func TestNewLSPRenameTool_ErrorOnNil(t *testing.T) {
	if _, err := NewLSPRenameTool(nil); err == nil {
		t.Fatal("expected error for nil manager, got nil")
	}
}

func TestNewLSPCodeActionsTool_ErrorOnNil(t *testing.T) {
	if _, err := NewLSPCodeActionsTool(nil); err == nil {
		t.Fatal("expected error for nil manager, got nil")
	}
}

func TestNewLSPTypeDefinitionTool_ErrorOnNil(t *testing.T) {
	if _, err := NewLSPTypeDefinitionTool(nil); err == nil {
		t.Fatal("expected error for nil manager, got nil")
	}
}

func TestNewLSPImplementationTool_ErrorOnNil(t *testing.T) {
	if _, err := NewLSPImplementationTool(nil); err == nil {
		t.Fatal("expected error for nil manager, got nil")
	}
}

func TestNewLSPDocumentSymbolsTool_ErrorOnNil(t *testing.T) {
	if _, err := NewLSPDocumentSymbolsTool(nil); err == nil {
		t.Fatal("expected error for nil manager, got nil")
	}
}

func TestNewLSPFormatTool_ErrorOnNil(t *testing.T) {
	if _, err := NewLSPFormatTool(nil); err == nil {
		t.Fatal("expected error for nil manager, got nil")
	}
}
