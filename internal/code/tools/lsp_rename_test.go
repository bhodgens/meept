package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/code/lsp"
)

func TestApplyTextEdits_ModifiesFile(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "test.go")
	content := "package main\n\nfunc Old() {}\n"
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	edits := []lsp.TextEdit{
		{
			Range: lsp.Range{
				Start: lsp.Position{Line: 2, Character: 5},
				End:   lsp.Position{Line: 2, Character: 8},
			},
			NewText: "New",
		},
	}

	if err := applyTextEdits(f, edits); err != nil {
		t.Fatalf("applyTextEdits failed: %v", err)
	}

	got, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	want := "package main\n\nfunc New() {}\n"
	if string(got) != want {
		t.Errorf("file content mismatch\ngot:  %q\nwant: %q", string(got), want)
	}
}

func TestApplyTextEdits_MultipleEditsSameFile(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "test.go")
	content := "aaa bbb ccc\n"
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	edits := []lsp.TextEdit{
		{
			Range:   lsp.Range{Start: lsp.Position{Line: 0, Character: 8}, End: lsp.Position{Line: 0, Character: 11}},
			NewText: "xxx",
		},
		{
			Range:   lsp.Range{Start: lsp.Position{Line: 0, Character: 0}, End: lsp.Position{Line: 0, Character: 3}},
			NewText: "yyy",
		},
	}

	if err := applyTextEdits(f, edits); err != nil {
		t.Fatalf("applyTextEdits failed: %v", err)
	}

	got, _ := os.ReadFile(f)
	want := "yyy bbb xxx\n"
	if string(got) != want {
		t.Errorf("file content mismatch\ngot:  %q\nwant: %q", string(got), want)
	}
}
