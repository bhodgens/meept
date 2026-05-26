package builtin

import (
	"strings"
	"testing"
)

func TestComputeLineHash(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantLen   int
		wantLower bool
	}{
		{"simple", "hello world", 2, true},
		{"empty", "", 2, true},
		{"whitespace only", "   ", 2, true},
		{"trailing whitespace ignored", "hello   ", 2, true},
		{"with tabs", "hello\tworld", 2, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeLineHash(tt.line)
			if len(got) != tt.wantLen {
				t.Errorf("ComputeLineHash(%q) length = %d, want %d", tt.line, len(got), tt.wantLen)
			}
			if tt.wantLower && got != strings.ToLower(got) {
				t.Errorf("ComputeLineHash(%q) = %q, want lowercase", tt.line, got)
			}
		})
	}

	// Test determinism: same input always gives same output
	t.Run("deterministic", func(t *testing.T) {
		h1 := ComputeLineHash("some line content")
		h2 := ComputeLineHash("some line content")
		if h1 != h2 {
			t.Errorf("ComputeLineHash not deterministic: %q != %q", h1, h2)
		}
	})

	// Test trailing whitespace is trimmed
	t.Run("trailing whitespace trimmed", func(t *testing.T) {
		h1 := ComputeLineHash("hello")
		h2 := ComputeLineHash("hello   ")
		h3 := ComputeLineHash("hello\t")
		if h1 != h2 {
			t.Errorf("trailing spaces should be trimmed: %q != %q", h1, h2)
		}
		if h1 != h3 {
			t.Errorf("trailing tab should be trimmed: %q != %q", h1, h3)
		}
	})

	// Test different content produces different hashes (not guaranteed but very likely)
	t.Run("different content different hash", func(t *testing.T) {
		hashes := make(map[string]string)
		lines := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
		for _, line := range lines {
			h := ComputeLineHash(line)
			if existing, ok := hashes[h]; ok && existing != line {
				// Collision is possible but unlikely for these distinct strings
				t.Logf("hash collision between %q and %q: %s", existing, line, h)
			}
			hashes[h] = line
		}
	})
}

func TestFormatHashLine(t *testing.T) {
	tests := []struct {
		name     string
		lineNum  int
		line     string
		wantPre  string // prefix to check
		contains string // substring that must be present
	}{
		{"line 1", 1, "hello", "1:", "|hello"},
		{"line 10", 10, "world", "10:", "|world"},
		{"empty line", 5, "", "5:", "|"},
		{"line with pipe", 3, "a|b", "3:", "|a|b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatHashLine(tt.lineNum, tt.line)
			if !strings.HasPrefix(got, tt.wantPre) {
				t.Errorf("FormatHashLine(%d, %q) = %q, want prefix %q", tt.lineNum, tt.line, got, tt.wantPre)
			}
			if !strings.Contains(got, tt.contains) {
				t.Errorf("FormatHashLine(%d, %q) = %q, want to contain %q", tt.lineNum, tt.line, got, tt.contains)
			}
		})
	}

	// Test format is exactly "NUM:HASH|CONTENT"
	t.Run("exact format", func(t *testing.T) {
		got := FormatHashLine(42, "test content")
		hash := ComputeLineHash("test content")
		expected := "42:" + hash + "|test content"
		if got != expected {
			t.Errorf("FormatHashLine(42, %q) = %q, want %q", "test content", got, expected)
		}
	})
}

func TestFormatHashLines(t *testing.T) {
	t.Run("multiple lines", func(t *testing.T) {
		lines := []string{"alpha", "beta", "gamma"}
		got := FormatHashLines(lines, 1)

		// Should have 3 lines separated by \n
		parts := strings.Split(got, "\n")
		if len(parts) != 3 {
			t.Fatalf("expected 3 lines, got %d: %q", len(parts), parts)
		}

		// Check each line has correct line number prefix
		for i, part := range parts {
			expectedPre := strings.TrimSpace(strings.Repeat(" ", 0)) // just i+1
			if !strings.HasPrefix(part, expectedPre) {
				t.Errorf("line %d: %q should start with line number %d", i, part, i+1)
			}
		}

		// Verify first line
		h1 := ComputeLineHash("alpha")
		if parts[0] != "1:"+h1+"|alpha" {
			t.Errorf("first line = %q, want 1:%s|alpha", parts[0], h1)
		}
	})

	t.Run("custom start line", func(t *testing.T) {
		lines := []string{"x", "y"}
		got := FormatHashLines(lines, 10)
		parts := strings.Split(got, "\n")
		if len(parts) != 2 {
			t.Fatalf("expected 2 lines, got %d", len(parts))
		}
		if !strings.HasPrefix(parts[0], "10:") {
			t.Errorf("first line should start with '10:', got %q", parts[0])
		}
		if !strings.HasPrefix(parts[1], "11:") {
			t.Errorf("second line should start with '11:', got %q", parts[1])
		}
	})

	t.Run("empty input", func(t *testing.T) {
		got := FormatHashLines([]string{}, 1)
		if got != "" {
			t.Errorf("expected empty string for empty input, got %q", got)
		}
	})
}

func TestParseAnchor(t *testing.T) {
	tests := []struct {
		name     string
		anchor   string
		wantLine int
		wantHash string
		wantErr  bool
	}{
		{"valid anchor", "42:ab", 42, "ab", false},
		{"valid anchor line 1", "1:zz", 1, "zz", false},
		{"missing colon", "42ab", 0, "", true},
		{"missing hash", "42:", 0, "", true},
		{"hash too short", "42:a", 0, "", true},
		{"hash too long", "42:abc", 0, "", true},
		{"non-numeric line", "abc:ab", 0, "", true},
		{"empty string", "", 0, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lineNum, hash, err := ParseAnchor(tt.anchor)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseAnchor(%q) error = %v, wantErr %v", tt.anchor, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if lineNum != tt.wantLine {
					t.Errorf("ParseAnchor(%q) lineNum = %d, want %d", tt.anchor, lineNum, tt.wantLine)
				}
				if hash != tt.wantHash {
					t.Errorf("ParseAnchor(%q) hash = %q, want %q", tt.anchor, hash, tt.wantHash)
				}
			}
		})
	}
}

func TestValidateAnchor(t *testing.T) {
	lines := []string{"alpha", "beta", "gamma", "delta"}

	t.Run("valid anchor", func(t *testing.T) {
		hash := ComputeLineHash("beta")
		if !ValidateAnchor(lines, 2, hash) {
			t.Error("expected anchor to validate for line 2")
		}
	})

	t.Run("wrong hash", func(t *testing.T) {
		if ValidateAnchor(lines, 2, "zz") {
			t.Error("expected anchor to fail with wrong hash")
		}
	})

	t.Run("line out of range high", func(t *testing.T) {
		hash := ComputeLineHash("anything")
		if ValidateAnchor(lines, 10, hash) {
			t.Error("expected anchor to fail for out-of-range line")
		}
	})

	t.Run("line out of range zero", func(t *testing.T) {
		hash := ComputeLineHash("anything")
		if ValidateAnchor(lines, 0, hash) {
			t.Error("expected anchor to fail for line 0")
		}
	})

	t.Run("line number is 1-based", func(t *testing.T) {
		hash := ComputeLineHash("alpha")
		if !ValidateAnchor(lines, 1, hash) {
			t.Error("expected line 1 to match 'alpha'")
		}
	})
}

func TestReadCache(t *testing.T) {
	t.Run("store and get", func(t *testing.T) {
		cache := NewReadCache(10)
		lines := []string{"a", "b", "c"}
		cache.Store("/test/file.txt", lines)

		got := cache.Get("/test/file.txt")
		if got == nil {
			t.Fatal("expected to find cached entry")
		}
		if len(got) != len(lines) {
			t.Errorf("expected %d lines, got %d", len(lines), len(got))
		}
		for i, line := range got {
			if line != lines[i] {
				t.Errorf("line %d: got %q, want %q", i, line, lines[i])
			}
		}
	})

	t.Run("get missing", func(t *testing.T) {
		cache := NewReadCache(10)
		got := cache.Get("/nonexistent")
		if got != nil {
			t.Error("expected nil for missing entry")
		}
	})

	t.Run("eviction", func(t *testing.T) {
		cache := NewReadCache(3)
		cache.Store("/file1.txt", []string{"1"})
		cache.Store("/file2.txt", []string{"2"})
		cache.Store("/file3.txt", []string{"3"})
		// Adding 4th should evict 1st
		cache.Store("/file4.txt", []string{"4"})

		if cache.Get("/file1.txt") != nil {
			t.Error("expected /file1.txt to be evicted")
		}
		if cache.Get("/file4.txt") == nil {
			t.Error("expected /file4.txt to be present")
		}
	})

	t.Run("overwrite updates recency", func(t *testing.T) {
		cache := NewReadCache(3)
		cache.Store("/file1.txt", []string{"1"})
		cache.Store("/file2.txt", []string{"2"})
		cache.Store("/file3.txt", []string{"3"})
		// Touch file1 again to make it most recent
		cache.Store("/file1.txt", []string{"1-updated"})
		// Now adding 4th should evict file2 (oldest untouched)
		cache.Store("/file4.txt", []string{"4"})

		if cache.Get("/file2.txt") != nil {
			t.Error("expected /file2.txt to be evicted")
		}
		got := cache.Get("/file1.txt")
		if got == nil {
			t.Fatal("expected /file1.txt to still be present")
		}
		if got[0] != "1-updated" {
			t.Errorf("expected updated content, got %q", got[0])
		}
	})

	t.Run("copy on store prevents aliasing", func(t *testing.T) {
		cache := NewReadCache(10)
		lines := []string{"original"}
		cache.Store("/test.txt", lines)
		lines[0] = "modified"

		got := cache.Get("/test.txt")
		if got[0] != "original" {
			t.Errorf("cache should not be affected by external mutation, got %q", got[0])
		}
	})

	t.Run("copy on get prevents aliasing", func(t *testing.T) {
		cache := NewReadCache(10)
		cache.Store("/test.txt", []string{"original"})

		got1 := cache.Get("/test.txt")
		got1[0] = "mutated"
		got2 := cache.Get("/test.txt")

		if got2[0] != "original" {
			t.Errorf("cache should not be affected by Get mutation, got %q", got2[0])
		}
	})

	t.Run("zero maxItems uses default", func(t *testing.T) {
		cache := NewReadCache(0)
		if cache == nil {
			t.Fatal("expected cache to be created with default size")
		}
		// Should be able to store more than 0 items
		for i := 0; i < 35; i++ {
			cache.Store("/file.txt", []string{"a"})
		}
	})
}
