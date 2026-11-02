package voice

// Import pin (master.md coding conventions): jargo is imported ONLY by
// cmd/meept-voice and internal/voicechild. internal/voice (the daemon side)
// must never import jargo. This test scans the package source with
// go/parser, so it compiles and PASSES trivially even before leaf 03/05/06
// land; it must SKIP (not fail) when the directory has no Go files yet.

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoJargoImport walks every .go file in this package directory and
// asserts no import path contains "jargo".
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
				t.Errorf("%s imports %q: jargo is imported ONLY by cmd/meept-voice and internal/voicechild, never by internal/voice", name, path)
			}
		}
	}

	if goFiles == 0 {
		t.Skip("no non-test .go files in internal/voice yet; the pin activates with leaf 03/05/06 source")
	}
	_ = fmt.Sprintf // keep fmt linked if the scan loop changes
}
