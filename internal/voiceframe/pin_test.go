package voiceframe

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoJargoImport walks every non-test .go file in this package directory
// and asserts no import path contains "jargo". Companion to the internal/
// voice import pin (gate test T9, master.md coding conventions: jargo is
// imported ONLY by cmd/meept-voice and internal/voicechild).
func TestNoJargoImport(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	goFiles := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		goFiles++
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("parse %s: %v", name, err)
			continue
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(path, "jargo") {
				t.Errorf("%s imports %q: jargo is imported ONLY by cmd/meept-voice and internal/voicechild, never by internal/voiceframe", name, path)
			}
		}
	}
	if goFiles == 0 {
		t.Skip("no non-test .go files in internal/voiceframe")
	}
}

// TestStdlibOnly asserts the codec imports nothing outside the stdlib: both
// socket sides share it and it must stay dependency-free.
func TestStdlibOnly(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(dir, "frame.go"), nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse frame.go: %v", err)
	}
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if strings.Contains(path, ".") && !strings.HasPrefix(path, "std/") {
			t.Errorf("frame.go imports %q: the shared codec must stay stdlib-only", path)
		}
	}
}
