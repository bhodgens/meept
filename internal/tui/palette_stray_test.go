package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoStrayColorLiterals is the Phase-4 regression guard for unified
// theming (issue #24): after the migration, no file in internal/tui may
// construct colors from raw hex strings or the deleted Color* string consts.
// Colors must come from the palette (Current() fields, paletteColor, or
// viz.SetPalette-derived vars).
//
// Exemptions:
//   - _test.go files
//   - the palette machinery itself (palette.go, styles.go)
//   - viz/colors.go (owns its own SetPalette-backed defaults)
//   - lines marked `// palette-exempt` (one-off accents with no role)
//   - lipgloss.Color("") — "no color", not a theme color
var strayLiteralRe = regexp.MustCompile(`lipgloss\.Color\("(#|Color)`)

var strayExemptFiles = map[string]bool{
	"palette.go":   true,
	"styles.go":    true,
	"constants.go": false, // migrated; literals here would fail the scan
}

func TestNoStrayColorLiterals(t *testing.T) {
	root := "."
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		base := filepath.Base(path)
		if strayExemptFiles[base] {
			return nil
		}
		if path == filepath.Join("viz", "colors.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			if !strayLiteralRe.MatchString(line) {
				continue
			}
			if strings.Contains(line, "palette-exempt") {
				continue
			}
			t.Errorf("%s:%d: stray color literal (use the palette):\n\t%s",
				filepath.ToSlash(path), i+1, strings.TrimSpace(line))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/tui: %v", err)
	}
}
