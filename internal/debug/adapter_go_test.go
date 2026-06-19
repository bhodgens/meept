package debug

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseGoroutineStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected GoroutineStatus
	}{
		{"", GoroutineUnknown},
		{"running", GoroutineRunning},
		{"Running", GoroutineRunning},
		{"runnable", GoroutineRunning},
		{"sleeping", GoroutineWaiting},
		{"waiting", GoroutineWaiting},
		{"Waiting", GoroutineWaiting},
		{"syscall", GoroutineSyscall},
		{"Syscall", GoroutineSyscall},
		{"idle", GoroutineIdle},
		{"chan receive", GoroutineIdle},
		{"chan send", GoroutineIdle},
		{"select", GoroutineIdle},
		{"io wait", GoroutineUnknown},
		{"some random state", GoroutineUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseGoroutineStatus(tt.input)
			if got != tt.expected {
				t.Errorf("parseGoroutineStatus(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFindStringInBinary(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		query  string
		expect int
	}{
		{
			name:   "found at start",
			data:   []byte("hello world"),
			query:  "hello",
			expect: 0,
		},
		{
			name:   "found at middle",
			data:   []byte("hello world"),
			query:  "world",
			expect: 6,
		},
		{
			name:   "not found",
			data:   []byte("hello world"),
			query:  "xyz",
			expect: -1,
		},
		{
			name:   "empty query",
			data:   []byte("hello"),
			query:  "",
			expect: -1,
		},
		{
			name:   "empty data",
			data:   []byte{},
			query:  "hello",
			expect: -1,
		},
		{
			name:   "exact match",
			data:   []byte("a"),
			query:  "a",
			expect: 0,
		},
		{
			name:   "found with null bytes",
			data:   []byte{0x00, 0x00, 'r', 'u', 'n', 't', 'i', 'm', 'e', '.', 'm', 'a', 'i', 'n'},
			query:  "runtime.main",
			expect: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findStringInBinary(tt.data, tt.query)
			if got != tt.expect {
				t.Errorf("findStringInBinary() = %d, want %d", got, tt.expect)
			}
		})
	}
}

func TestMatchAt(t *testing.T) {
	data := []byte("hello world")

	if !matchAt(data, 0, []byte("hello")) {
		t.Error("matchAt should match 'hello' at position 0")
	}
	if !matchAt(data, 6, []byte("world")) {
		t.Error("matchAt should match 'world' at position 6")
	}
	if matchAt(data, 1, []byte("world")) {
		t.Error("matchAt should not match 'world' at position 1")
	}
	if matchAt(data, 0, []byte("hello world!")) {
		t.Error("matchAt should not match longer string")
	}
}

func TestIsGoBinary(t *testing.T) {
	// Test with the current binary (should be a Go binary).
	self, err := os.Executable()
	if err != nil {
		t.Skipf("cannot determine executable path: %v", err)
	}

	isGo, err := IsGoBinary(self)
	if err != nil {
		t.Fatalf("IsGoBinary(%q) error: %v", self, err)
	}
	if !isGo {
		t.Errorf("IsGoBinary(%q) = false, expected true (this test binary is compiled Go)", self)
	}

	// Test with a non-Go file (this test file itself).
	tmpDir := t.TempDir()
	nonGoPath := filepath.Join(tmpDir, "not_go.txt")
	if err := os.WriteFile(nonGoPath, []byte("this is not a go binary"), 0644); err != nil {
		t.Fatal(err)
	}

	isGo, err = IsGoBinary(nonGoPath)
	if err != nil {
		t.Fatalf("IsGoBinary(%q) error: %v", nonGoPath, err)
	}
	if isGo {
		t.Errorf("IsGoBinary(%q) = true, expected false", nonGoPath)
	}

	// Test with non-existent file.
	_, err = IsGoBinary("/nonexistent/binary")
	if err == nil {
		t.Error("IsGoBinary should return error for non-existent file")
	}
}

func TestDetectGoBinary(t *testing.T) {
	// Test with a .go source file — should return false.
	tmpDir := t.TempDir()
	goSource := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(goSource, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	isGo, dlvPath, err := DetectGoBinary(goSource)
	if err != nil {
		t.Fatalf("DetectGoBinary(%q) error: %v", goSource, err)
	}
	if isGo {
		t.Error("DetectGoBinary should return false for .go source files")
	}
	if dlvPath != "" {
		t.Error("dlvPath should be empty for non-Go binary")
	}

	// Test with empty path.
	_, _, err = DetectGoBinary("")
	if err == nil {
		t.Error("DetectGoBinary should return error for empty path")
	}

	// Test with directory.
	isGo, _, err = DetectGoBinary(tmpDir)
	if err != nil {
		t.Fatalf("DetectGoBinary(%q) error: %v", tmpDir, err)
	}
	if isGo {
		t.Error("DetectGoBinary should return false for directories")
	}

	// Test with non-existent file.
	isGo, _, err = DetectGoBinary(filepath.Join(tmpDir, "nonexistent"))
	if err != nil {
		t.Fatal(err)
	}
	if isGo {
		t.Error("DetectGoBinary should return false for non-existent file")
	}
}

func TestDetectGoBinaryWithSelf(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skipf("cannot determine executable path: %v", err)
	}

	isGo, dlvPath, err := DetectGoBinary(self)
	if err != nil {
		t.Fatalf("DetectGoBinary(%q) error: %v", self, err)
	}
	if !isGo {
		t.Errorf("DetectGoBinary(%q) should detect Go binary", self)
	}
	// dlvPath may or may not be set depending on whether dlv is installed.
	_ = dlvPath
}

func TestGoDebugHint(t *testing.T) {
	// Non-existent file should return empty hint.
	hint := GoDebugHint("/nonexistent/binary")
	if hint != "" {
		t.Errorf("GoDebugHint should return empty for non-existent file, got %q", hint)
	}

	// .go source file should return empty hint.
	hint = GoDebugHint("main.go")
	if hint != "" {
		t.Errorf("GoDebugHint should return empty for .go source file, got %q", hint)
	}

	// Empty path should return empty hint.
	hint = GoDebugHint("")
	if hint != "" {
		t.Errorf("GoDebugHint should return empty for empty path, got %q", hint)
	}
}

func TestGoroutineInfoFields(t *testing.T) {
	// Verify that GoroutineInfo fields serialize correctly.
	info := GoroutineInfo{
		ID:        42,
		Status:    GoroutineRunning,
		Function:  "main.main",
		File:      "/home/user/main.go",
		Line:      15,
		UserState: "running",
		Args:      []GoroutineArg{{Name: "ctx", Value: "context.Context"}},
		Labels:    map[string]string{"pkey": "pval"},
	}

	if info.ID != 42 {
		t.Errorf("GoroutineInfo.ID = %d, want 42", info.ID)
	}
	if info.Status != GoroutineRunning {
		t.Errorf("GoroutineInfo.Status = %q, want %q", info.Status, GoroutineRunning)
	}
	if len(info.Args) != 1 || info.Args[0].Name != "ctx" {
		t.Errorf("GoroutineInfo.Args not set correctly")
	}
	if info.Labels["pkey"] != "pval" {
		t.Errorf("GoroutineInfo.Labels not set correctly")
	}
}

func TestGoroutinesResultFields(t *testing.T) {
	result := GoroutinesResult{
		Total: 100,
		List:  []GoroutineInfo{{ID: 1, Status: GoroutineIdle}},
	}

	if result.Total != 100 {
		t.Errorf("GoroutinesResult.Total = %d, want 100", result.Total)
	}
	if len(result.List) != 1 || result.List[0].ID != 1 {
		t.Errorf("GoroutinesResult.List not set correctly")
	}
}

func TestIsGoBinaryRuntime(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("IsGoBinary test only runs on darwin/linux")
	}

	// The test binary itself should be a Go binary.
	self, err := os.Executable()
	if err != nil {
		t.Skipf("cannot determine executable path: %v", err)
	}

	isGo, err := IsGoBinary(self)
	if err != nil {
		t.Fatalf("IsGoBinary(%q) error: %v", self, err)
	}
	if !isGo {
		t.Errorf("IsGoBinary(%q) = false, expected true for the test binary (compiled Go)", self)
	}
}
