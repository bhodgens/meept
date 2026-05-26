package debug

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdapterDetectByExtension(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		program    string
		wantName   string // expected adapter name (may be one of several valid options)
		wantNotNil bool
	}{
		{"main.go", "dlv", true},
		{"app.py", "debugpy", true},
		// C/C++ can be gdb, lldb-dap, or codelldb - just check not nil
		{"main.c", "", true},
		{"main.cpp", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.program, func(t *testing.T) {
			cfg, err := DetectAdapter(tt.program, tmpDir)
			if !tt.wantNotNil {
				if err == nil {
					t.Fatalf("expected error for %q, got adapter %q", tt.program, cfg.Name)
				}
				return
			}
			if err != nil {
				t.Fatalf("DetectAdapter(%q, %q) failed: %v", tt.program, tmpDir, err)
			}
			if tt.wantName != "" && cfg.Name != tt.wantName {
				t.Fatalf("expected adapter %q, got %q", tt.wantName, cfg.Name)
			}
		})
	}
}

func TestAdapterDetectByRootMarker(t *testing.T) {
	tmpDir := t.TempDir()

	// Create go.mod marker.
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := DetectAdapter("main.go", tmpDir)
	if err != nil {
		t.Fatalf("DetectAdapter failed: %v", err)
	}
	if cfg.Name != "dlv" {
		t.Fatalf("expected adapter 'dlv', got %q", cfg.Name)
	}
}

func TestAdapterDetectUnknownExtension(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := DetectAdapter("main.xyz", tmpDir)
	if err == nil {
		t.Fatal("expected error for unknown extension")
	}
}

func TestAdapterDetectNoMatch(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := DetectAdapter("readme.txt", tmpDir)
	if err == nil {
		t.Fatal("expected error for .txt extension")
	}
}

func TestFindAdapterByName(t *testing.T) {
	tests := []struct {
		name    string
		wantNil bool
	}{
		{"dlv", false},
		{"gdb", false},
		{"lldb-dap", false},
		{"debugpy", false},
		{"codelldb", false},
		{"nonexistent", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := FindAdapterByName(tt.name)
			if tt.wantNil {
				if cfg != nil {
					t.Fatalf("expected nil for %q, got %+v", tt.name, cfg)
				}
			} else {
				if cfg == nil {
					t.Fatalf("expected non-nil for %q", tt.name)
				}
				if cfg.Name != tt.name {
					t.Fatalf("expected name %q, got %q", tt.name, cfg.Name)
				}
			}
		})
	}
}

func TestAdapterDefaultConfigurations(t *testing.T) {
	if len(defaultAdapters) == 0 {
		t.Fatal("expected at least one default adapter")
	}

	// Verify each adapter has required fields.
	for _, a := range defaultAdapters {
		if a.Name == "" {
			t.Error("adapter missing name")
		}
		if a.Command == "" {
			t.Errorf("adapter %q missing command", a.Name)
		}
		if len(a.FileTypes) == 0 {
			t.Errorf("adapter %q has no file types", a.Name)
		}
		if a.Transport == "" {
			t.Errorf("adapter %q missing transport", a.Name)
		}
	}
}

func TestAdapterDetectPrefersRootMarker(t *testing.T) {
	tmpDir := t.TempDir()

	// Create Cargo.toml (Rust marker) in tmpDir.
	if err := os.WriteFile(filepath.Join(tmpDir, "Cargo.toml"), []byte("[package]\nname = \"test\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// .rs files match lldb-dap and codelldb. Root marker gives a scoring boost.
	cfg, err := DetectAdapter("main.rs", tmpDir)
	if err != nil {
		t.Fatalf("DetectAdapter failed: %v", err)
	}
	// Should be one of the Rust-capable adapters.
	if cfg.Name != "lldb-dap" && cfg.Name != "codelldb" {
		t.Fatalf("expected lldb-dap or codelldb, got %q", cfg.Name)
	}
}
