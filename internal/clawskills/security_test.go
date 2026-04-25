package clawskills

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestIsToolBlocked(t *testing.T) {
	tests := []struct {
		tool     string
		expected bool
	}{
		// Exact matches
		{"shell_execute", true},
		{"file_write", true},
		{"file_delete", true},
		{"security_bypass", true},
		{"daemon_restart", true},
		{"config_write", true},
		{"credential_set", true},
		{"sdk_install", true},

		// Pattern matches
		{"shell_bash", true},       // matches shell_*
		{"file_write_raw", true},   // matches file_write*
		{"security_scan", true},    // matches security_*
		{"daemon_status", true},    // matches daemon_*
		{"config_read_env", true},  // matches config_*
		{"credential_check", true}, // matches credential_*
		{"admin_users", true},      // matches admin_*

		// Allowed tools
		{"file_read", false},
		{"memory_search", false},
		{"web_search", false},
		{"chat", false},
		{"code_analyze", false},
		{"read_file", false},
		{"list_directory", false},
		{"platform_status", false},
		{"", false},
	}

	for _, tt := range tests {
		got := IsToolBlocked(tt.tool)
		if got != tt.expected {
			t.Errorf("IsToolBlocked(%q) = %v, want %v", tt.tool, got, tt.expected)
		}
	}
}

func TestIsToolBlockedCaseInsensitive(t *testing.T) {
	if !IsToolBlocked("SHELL_EXECUTE") {
		t.Error("expected SHELL_EXECUTE to be blocked")
	}
	if !IsToolBlocked("Shell_Execute") {
		t.Error("expected Shell_Execute to be blocked")
	}
	if !IsToolBlocked("FILE_WRITE") {
		t.Error("expected FILE_WRITE to be blocked")
	}
}

func TestFilterTools(t *testing.T) {
	tools := []string{
		"file_read",
		"shell_execute",
		"memory_search",
		"file_write",
		"code_analyze",
		"security_bypass",
		"chat",
	}

	filtered := FilterTools(tools)

	expected := []string{"file_read", "memory_search", "code_analyze", "chat"}
	if len(filtered) != len(expected) {
		t.Fatalf("expected %d tools, got %d: %v", len(expected), len(filtered), filtered)
	}

	for i, exp := range expected {
		if filtered[i] != exp {
			t.Errorf("filtered[%d] = %q, want %q", i, filtered[i], exp)
		}
	}
}

func TestFilterToolsEmpty(t *testing.T) {
	filtered := FilterTools(nil)
	if len(filtered) != 0 {
		t.Errorf("expected empty result for nil input, got %v", filtered)
	}

	filtered = FilterTools([]string{})
	if len(filtered) != 0 {
		t.Errorf("expected empty slice for empty input, got %v", filtered)
	}
}

func TestFilterToolsAllBlocked(t *testing.T) {
	tools := []string{"shell_execute", "file_write", "daemon_restart"}
	filtered := FilterTools(tools)
	if len(filtered) != 0 {
		t.Errorf("expected empty result, got %v", filtered)
	}
}

func TestEnforceRiskLevel(t *testing.T) {
	tests := []struct {
		requested string
		expected  string
	}{
		{"low", "high"},
		{"medium", "high"},
		{"high", "high"},
		{"", "high"},
		{"critical", "high"},
		{"LOW", "high"},
		{"Medium", "high"},
	}

	for _, tt := range tests {
		got := EnforceRiskLevel(tt.requested)
		if got != tt.expected {
			t.Errorf("EnforceRiskLevel(%q) = %q, want %q", tt.requested, got, tt.expected)
		}
	}
}

func TestVerifyDownloadSHA256Match(t *testing.T) {
	checker := NewSecurityChecker()
	data := []byte("hello world")

	// Compute actual hash
	hasher := sha256.Sum256(data)
	expectedHash := hex.EncodeToString(hasher[:])

	result := checker.VerifyDownload(data, expectedHash, true)
	if !result.Valid {
		t.Errorf("expected valid, got errors: %v", result.Errors)
	}
	if !result.SHA256Match {
		t.Error("expected SHA256Match=true")
	}
	if !result.Signed {
		t.Error("expected Signed=true when verified=true")
	}
}

func TestVerifyDownloadSHA256Mismatch(t *testing.T) {
	checker := NewSecurityChecker()

	result := checker.VerifyDownload([]byte("data"), "wronghash", false)
	if result.Valid {
		t.Error("expected invalid for SHA mismatch")
	}
	if result.SHA256Match {
		t.Error("expected SHA256Match=false")
	}
	if len(result.Errors) == 0 {
		t.Error("expected errors")
	}
	if len(result.Warnings) == 0 {
		t.Error("expected warning for unverified skill")
	}
}

func TestVerifyExtractedSuccess(t *testing.T) {
	dir := t.TempDir()

	// Create valid skill structure
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: test\n---\nbody"), 0644)

	checker := NewSecurityChecker()
	if err := checker.VerifyExtracted(dir); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestVerifyExtractedMissingSkillMd(t *testing.T) {
	dir := t.TempDir()

	checker := NewSecurityChecker()
	err := checker.VerifyExtracted(dir)
	if err == nil {
		t.Fatal("expected error for missing SKILL.md")
	}
}

func TestVerifyExtractedForbiddenExtension(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: test\n---\nbody"), 0644)
	os.WriteFile(filepath.Join(dir, "malware.exe"), []byte("binary"), 0644)

	checker := NewSecurityChecker()
	err := checker.VerifyExtracted(dir)
	if err == nil {
		t.Fatal("expected error for forbidden extension")
	}
}

func TestVerifyExtractedOversizedFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: test\n---\nbody"), 0644)

	// Create a file larger than 1MB
	bigFile := filepath.Join(dir, "big.txt")
	f, _ := os.Create(bigFile)
	f.Write(make([]byte, 1024*1024+1))
	f.Close()

	checker := NewSecurityChecker()
	err := checker.VerifyExtracted(dir)
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
}

func TestVerifyExtractedDangerousPattern(t *testing.T) {
	dir := t.TempDir()
	// SKILL.md with a dangerous pattern
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: test\n---\nos.system('rm -rf /')"), 0644)

	checker := NewSecurityChecker()
	err := checker.VerifyExtracted(dir)
	if err == nil {
		t.Fatal("expected error for dangerous pattern")
	}
}

func TestScanFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.py")
	os.WriteFile(f, []byte("import subprocess\nsubprocess.call(['rm', '-rf', '/'])"), 0644)

	checker := NewSecurityChecker()
	result := checker.ScanFile(f)
	if result.Valid {
		t.Error("expected invalid for dangerous file")
	}
}

func TestScanFileClean(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "clean.md")
	os.WriteFile(f, []byte("# Clean Skill\n\nThis is a safe skill."), 0644)

	checker := NewSecurityChecker()
	result := checker.ScanFile(f)
	if !result.Valid {
		t.Errorf("expected valid for clean file, got errors: %v", result.Errors)
	}
}

func TestIsTextFile(t *testing.T) {
	tests := []struct {
		ext      string
		expected bool
	}{
		{".md", true},
		{".py", true},
		{".go", true},
		{".js", true},
		{".json", true},
		{".yaml", true},
		{".sh", true},
		{".exe", false},
		{".bin", false},
		{".png", false},
		{"", false},
	}

	for _, tt := range tests {
		got := isTextFile(tt.ext)
		if got != tt.expected {
			t.Errorf("isTextFile(%q) = %v, want %v", tt.ext, got, tt.expected)
		}
	}
}

func TestMatchToolPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		match   bool
	}{
		{"shell_execute", "shell_*", true},
		{"shell_bash", "shell_*", true},
		{"file_write_raw", "file_write*", true},
		{"file_read", "shell_*", false},
		{"config_read", "config_*", true},
		{"exact_match", "exact_match", true},
		{"no_match", "exact_match", false},
	}

	for _, tt := range tests {
		got := matchToolPattern(tt.name, tt.pattern)
		if got != tt.match {
			t.Errorf("matchToolPattern(%q, %q) = %v, want %v", tt.name, tt.pattern, got, tt.match)
		}
	}
}
