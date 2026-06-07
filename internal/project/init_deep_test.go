package project

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/code/ast"
)

func TestDeepInitializer_ScanTree(t *testing.T) {
	ctx := context.Background()

	// Create a temporary project structure
	tmpDir := t.TempDir()
	createTestGoFile(t, filepath.Join(tmpDir, "main.go"), "package main\n\nfunc main() {}\n")
	createTestGoFile(t, filepath.Join(tmpDir, "internal", "svc", "service.go"),
		"package svc\n\nfunc Process() error { return nil }\n")
	createTestGoFile(t, filepath.Join(tmpDir, "internal", "api", "handler.go"),
		"package api\n\ntype Handler struct{}\nfunc (h *Handler) Serve() {}\n")

	opts := DefaultDeepInitOptions()
	opts.RootDir = tmpDir
	opts.MaxDepth = 3
	opts.MinFileCount = 1

	di := NewDeepInitializer(opts, nil)
	dirs, err := di.scanTree(ctx, tmpDir)
	if err != nil {
		t.Fatalf("scanTree: %v", err)
	}

	if len(dirs) == 0 {
		t.Fatal("expected directories, got none")
	}

	// Should find root, internal/svc, internal/api
	found := make(map[string]bool)
	for _, d := range dirs {
		found[d.Path] = true
	}

	if !found[tmpDir] {
		t.Error("expected root directory")
	}
	if !found[filepath.Join(tmpDir, "internal", "svc")] {
		t.Error("expected internal/svc directory")
	}
	if !found[filepath.Join(tmpDir, "internal", "api")] {
		t.Error("expected internal/api directory")
	}
}

func TestDeepInitializer_GenerateAgentsFile(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	createTestGoFile(t, filepath.Join(tmpDir, "handler.go"),
		"package api\n\ntype Handler struct{}\nfunc (h *Handler) Serve() {}\nfunc NewHandler() *Handler { return nil }\n")

	opts := DefaultDeepInitOptions()
	opts.RootDir = tmpDir
	opts.MinFileCount = 1

	di := NewDeepInitializer(opts, nil)
	dirs, err := di.scanTree(ctx, tmpDir)
	if err != nil {
		t.Fatalf("scanTree: %v", err)
	}
	if len(dirs) != 1 {
		t.Fatalf("expected 1 dir, got %d", len(dirs))
	}

	result := &DeepInitResult{RootDir: tmpDir}
	if err := di.generateAgentsFile(ctx, dirs[0], result); err != nil {
		t.Fatalf("generateAgentsFile: %v", err)
	}

	if len(result.AgentsFiles) != 1 {
		t.Fatalf("expected 1 agents file, got %d", len(result.AgentsFiles))
	}

	agentsPath := filepath.Join(tmpDir, "AGENTS.md")
	data, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "# Agent Context:") {
		t.Error("expected header in AGENTS.md")
	}
	if !strings.Contains(content, "Type Handler") {
		t.Error("expected Type Handler in symbols")
	}
	if !strings.Contains(content, "Meth Serve") {
		t.Error("expected Meth Serve in symbols")
	}
}

func TestDeepInitializer_Run(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	createTestGoFile(t, filepath.Join(tmpDir, "main.go"), "package main\n\nfunc main() {}\n")
	createTestGoFile(t, filepath.Join(tmpDir, "internal", "utils", "util.go"),
		"package utils\n\nfunc Helper() {}\n")

	opts := DefaultDeepInitOptions()
	opts.RootDir = tmpDir
	opts.MaxDepth = 3
	opts.MinFileCount = 1

	di := NewDeepInitializer(opts, nil)
	result, err := di.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.AgentsFiles) == 0 {
		t.Fatal("expected at least one agents file")
	}

	for _, af := range result.AgentsFiles {
		if _, err := os.Stat(af.Path); err != nil {
			t.Errorf("agents file not found: %s", af.Path)
		}
	}
}

func TestLoadAgentsForContext(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte("# Root Context\n"), 0o644)
	internalDir := filepath.Join(tmpDir, "internal")
	_ = os.MkdirAll(internalDir, 0o755)
	_ = os.WriteFile(filepath.Join(internalDir, "AGENTS.md"), []byte("# Internal Context\n"), 0o644)

	workingFile := filepath.Join(internalDir, "svc", "service.go")
	_ = os.MkdirAll(filepath.Dir(workingFile), 0o755)

	contents, err := LoadAgentsForContext(tmpDir, workingFile)
	if err != nil {
		t.Fatalf("LoadAgentsForContext: %v", err)
	}

	if len(contents) == 0 {
		t.Fatal("expected agents content")
	}

	foundInternal := false
	foundRoot := false
	for _, c := range contents {
		if strings.Contains(c, "Internal Context") {
			foundInternal = true
		}
		if strings.Contains(c, "Root Context") {
			foundRoot = true
		}
	}

	if !foundInternal {
		t.Error("expected internal context to be loaded")
	}
	if !foundRoot {
		t.Error("expected root context to be loaded")
	}
}

func TestIsSourceFile(t *testing.T) {
	di := NewDeepInitializer(DefaultDeepInitOptions(), nil)
	tests := []struct {
		name   string
		expect bool
	}{
		{"main.go", true},
		{"script.py", true},
		{"app.ts", true},
		{"lib.rs", true},
		{"README.md", false},
		{"config.json", false},
		{"Makefile", false},
	}
	for _, tc := range tests {
		got := di.isSourceFile(tc.name)
		if got != tc.expect {
			t.Errorf("isSourceFile(%q) = %v, want %v", tc.name, got, tc.expect)
		}
	}
}

func TestShorthandFor(t *testing.T) {
	di := NewDeepInitializer(DefaultDeepInitOptions(), nil)
	tests := []struct {
		sym  ast.Symbol
		want string
	}{
		{ast.Symbol{Name: "Foo", Kind: ast.SymbolKindFunction, Signature: "(ctx context.Context) error"}, "Fn Foo: (ctx context.Context) error"},
		{ast.Symbol{Name: "Bar", Kind: ast.SymbolKindStruct, Signature: "struct { X int }"}, "Type Bar struct { X int }"},
		{ast.Symbol{Name: "Baz", Kind: ast.SymbolKindInterface, Signature: "interface { Run() }"}, "Ifac Baz interface { Run() }"},
	}
	for _, tc := range tests {
		got := di.shorthandFor(tc.sym)
		if got != tc.want {
			t.Errorf("shorthandFor(%+v) = %q, want %q", tc.sym, got, tc.want)
		}
	}
}

func TestTruncateSignature(t *testing.T) {
	di := NewDeepInitializer(DefaultDeepInitOptions(), nil)
	long := strings.Repeat("a", 100)
	got := di.truncateSignature(long, 20)
	if len(got) != 20 {
		t.Errorf("truncateSignature length = %d, want 20", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncateSignature should end with ...: %q", got)
	}
}

func createTestGoFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
