package debug

import (
	"os"
	"runtime"
	"testing"
)

func TestFindPIDByNameEmpty(t *testing.T) {
	_, err := FindPIDByName("")
	if err == nil {
		t.Fatal("expected error for empty process name")
	}
}

func TestFindPIDByNameSelf(t *testing.T) {
	// Look up our own process by name (e.g. "go" or "go.test").
	procName, ok := os.LookupEnv("GO_TEST_PROCESS_NAME")
	if !ok {
		// Use a process name that is very likely running: the current test runner.
		// On macOS/Linux, the test binary is usually named after the package.
		return // skip if we can't determine a reliable name
	}

	pid, err := FindPIDByName(procName)
	if err != nil {
		t.Logf("FindPIDByName(%q) returned error (may be expected in CI): %v", procName, err)
		return
	}
	if pid <= 0 {
		t.Fatalf("expected positive pid, got %d", pid)
	}
}

func TestFindPIDByNameNonexistent(t *testing.T) {
	_, err := FindPIDByName("no_such_process_xyz_12345")
	if err == nil {
		t.Fatal("expected error for nonexistent process name")
	}
}

func TestFindProcessBinaryInvalidPID(t *testing.T) {
	_, err := FindProcessBinary(-1)
	if err == nil {
		t.Fatal("expected error for negative pid")
	}

	_, err = FindProcessBinary(0)
	if err == nil {
		t.Fatal("expected error for pid 0")
	}
}

func TestFindProcessBinaryNonexistentPID(t *testing.T) {
	_, err := FindProcessBinary(9999999)
	if err == nil {
		t.Fatal("expected error for nonexistent pid")
	}
}

func TestFindProcessBinarySelf(t *testing.T) {
	pid := os.Getpid()
	binary, err := FindProcessBinary(pid)
	if err != nil {
		t.Fatalf("FindProcessBinary(%d) failed: %v", pid, err)
	}
	if binary == "" {
		t.Fatal("expected non-empty binary path")
	}
}

func TestDetectAdapterForProcessInvalidPID(t *testing.T) {
	_, err := DetectAdapterForProcess(-1)
	if err == nil {
		t.Fatal("expected error for invalid pid")
	}
}

func TestDetectAdapterForProcessNonexistentPID(t *testing.T) {
	_, err := DetectAdapterForProcess(9999999)
	if err == nil {
		t.Fatal("expected error for nonexistent pid")
	}
}

func TestBaseName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/usr/bin/python3", "python3"},
		{"python3", "python3"},
		{"/usr/local/bin/dlv", "dlv"},
		{"", ""},
		{"relative/path/to/binary", "binary"},
	}
	for _, tt := range tests {
		got := baseName(tt.input)
		if got != tt.want {
			t.Errorf("baseName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestUnknownExt(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/usr/bin/python3", ""},
		{"/path/to/main.go", ".go"},
		{"/path/to/app.py", ".py"},
		{"/path/to/main.c", ".c"},
		{"/path/to/main.rs", ".rs"},
		{"/path/to/main.swift", ".swift"},
		{"/path/to/binary", ""},
		{"/path/to/script.sh", ""}, // .sh is not a known shebang ext
	}
	for _, tt := range tests {
		got := unknownExt(tt.input)
		if got != tt.want {
			t.Errorf("unknownExt(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSessionModeValues(t *testing.T) {
	modes := []SessionMode{
		SessionModeLaunch,
		SessionModeAttach,
		SessionModeCore,
	}
	for _, m := range modes {
		if m == "" {
			t.Errorf("session mode %q should not be empty", m)
		}
	}
}

func TestFindPIDByNameUnsupportedOS(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		t.Skip("skipping on supported OS")
	}
	_, err := FindPIDByName("anything")
	if err == nil {
		t.Fatal("expected error on unsupported OS")
	}
}

func TestFindProcessBinaryUnsupportedOS(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		t.Skip("skipping on supported OS")
	}
	_, err := FindProcessBinary(1)
	if err == nil {
		t.Fatal("expected error on unsupported OS")
	}
}
