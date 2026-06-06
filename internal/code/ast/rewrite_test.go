package ast

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewriteEngine_GenerateProposal_Rename(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "test.go")
	content := `package main

func Hello() string {
	return "world"
}
`
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	pm := NewParserManager(DefaultParserConfig())
	engine := NewRewriteEngine(pm)

	proposal, err := engine.GenerateProposal(context.Background(), f, LangGo,
		"(function_declaration name: (identifier) @name)", OpRename, "Goodbye")
	if err != nil {
		t.Fatalf("GenerateProposal failed: %v", err)
	}

	if proposal.MatchCount != 1 {
		t.Fatalf("expected 1 match, got %d", proposal.MatchCount)
	}

	edit := proposal.Edits[0]
	if edit.NewText != "Goodbye" {
		t.Errorf("expected new_text 'Goodbye', got %q", edit.NewText)
	}
	if !strings.Contains(proposal.PreviewText[0], "rename") {
		t.Errorf("expected preview to mention rename, got %q", proposal.PreviewText[0])
	}
}

func TestRewriteEngine_GenerateProposal_Replace(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "test.go")
	content := `package main

func Hello() string {
	return "world"
}
`
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	pm := NewParserManager(DefaultParserConfig())
	engine := NewRewriteEngine(pm)

	// Replace function declaration with a comment
	proposal, err := engine.GenerateProposal(context.Background(), f, LangGo,
		"(function_declaration) @func", OpReplace, "// removed: @func")
	if err != nil {
		t.Fatalf("GenerateProposal failed: %v", err)
	}

	if proposal.MatchCount != 1 {
		t.Fatalf("expected 1 match, got %d", proposal.MatchCount)
	}

	edit := proposal.Edits[0]
	if !strings.Contains(edit.NewText, "removed") {
		t.Errorf("expected new_text to contain 'removed', got %q", edit.NewText)
	}
}

func TestApplyEdits_Basic(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "test.go")
	content := "package main\n\nfunc Hello() {}\n"
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	edits := []EditProposal{
		{
			FilePath:  f,
			StartLine: 2,
			StartCol:  5,
			EndLine:   2,
			EndCol:    10,
			NewText:   "Goodbye",
		},
	}

	if err := ApplyEdits(f, edits); err != nil {
		t.Fatalf("ApplyEdits failed: %v", err)
	}

	got, _ := os.ReadFile(f)
	want := "package main\n\nfunc Goodbye() {}\n"
	if string(got) != want {
		t.Errorf("content mismatch\ngot:  %q\nwant: %q", string(got), want)
	}
}
