package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "absolute path",
			path:    "/tmp/test",
			wantErr: false,
		},
		{
			name:    "relative path",
			path:    "./test",
			wantErr: false,
		},
		{
			name:    "path with .",
			path:    ".",
			wantErr: false,
		},
		{
			name:    "path with ..",
			path:    "../test",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NormalizePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("NormalizePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result == "" {
				t.Error("NormalizePath() returned empty string")
			}
		})
	}
}

func TestFileExists(t *testing.T) {
	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "testdir")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "existing file",
			path: tmpFile.Name(),
			want: true,
		},
		{
			name: "existing directory",
			path: tmpDir,
			want: false,
		},
		{
			name: "non-existent path",
			path: "/nonexistent/path/12345",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FileExists(tt.path); got != tt.want {
				t.Errorf("FileExists() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDirExists(t *testing.T) {
	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "testdir")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "existing directory",
			path: tmpDir,
			want: true,
		},
		{
			name: "existing file",
			path: tmpFile.Name(),
			want: false,
		},
		{
			name: "non-existent path",
			path: "/nonexistent/path/12345",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DirExists(tt.path); got != tt.want {
				t.Errorf("DirExists() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestArtifactCache(t *testing.T) {
	cache := NewArtifactCache(1 * time.Second)

	// Create test artifacts
	artifacts := &Artifacts{
		WorkingDir:  "/test/dir",
		Available:   true,
		LastScanned: time.Now(),
	}

	// Test Put and Get
	cache.Put("/test/dir", artifacts)

	got, found := cache.Get("/test/dir")
	if !found {
		t.Error("Cache entry not found after Put")
	}
	if got.WorkingDir != artifacts.WorkingDir {
		t.Errorf("Got working dir %s, want %s", got.WorkingDir, artifacts.WorkingDir)
	}

	// Test cache miss
	_, found = cache.Get("/other/dir")
	if found {
		t.Error("Cache entry found for non-existent key")
	}

	// Test Invalidate
	cache.Invalidate("/test/dir")
	_, found = cache.Get("/test/dir")
	if found {
		t.Error("Cache entry found after Invalidate")
	}

	// Test Clear
	cache.Put("/test/dir", artifacts)
	cache.Clear()
	_, found = cache.Get("/test/dir")
	if found {
		t.Error("Cache entry found after Clear")
	}

	// Test TTL expiration
	cache.Put("/test/dir", artifacts)
	time.Sleep(1100 * time.Millisecond)
	_, found = cache.Get("/test/dir")
	if found {
		t.Error("Cache entry found after TTL expiration")
	}
}

func TestNewArtifacts(t *testing.T) {
	workingDir := "/test/directory"
	artifacts := NewArtifacts(workingDir)

	if artifacts.WorkingDir != workingDir {
		t.Errorf("WorkingDir = %v, want %v", artifacts.WorkingDir, workingDir)
	}
	if artifacts.Available {
		t.Error("Available should be false for new artifacts")
	}
	if artifacts.CLAUDEMD != nil {
		t.Error("CLAUDEMD should be nil for new artifacts")
	}
	if artifacts.ClaudeDir != nil {
		t.Error("ClaudeDir should be nil for new artifacts")
	}
}

func TestArtifacts_Helpers(t *testing.T) {
	artifacts := &Artifacts{
		WorkingDir: "/test/dir",
		Available:  false,
	}

	// Test with no artifacts
	if artifacts.HasCLAUDEMD() {
		t.Error("HasCLAUDEMD() should return false when CLAUDEMD is nil")
	}
	if artifacts.HasClaudeDir() {
		t.Error("HasClaudeDir() should return false when ClaudeDir is nil")
	}
	if artifacts.HasSkills() {
		t.Error("HasSkills() should return false when no skills")
	}

	// Test with CLAUDEMD
	artifacts.CLAUDEMD = &CLAUDEDocument{
		Path: "/test/dir/CLAUDE.md",
		BuildCommands: []BuildCommand{
			{Command: "go build", Category: "build"},
			{Command: "go test", Category: "test"},
		},
	}

	if !artifacts.HasCLAUDEMD() {
		t.Error("HasCLAUDEMD() should return true when CLAUDEMD is set")
	}

	// Test GetCommandsForCategory
	buildCmds := artifacts.GetCommandsForCategory("build")
	if len(buildCmds) != 1 {
		t.Errorf("GetCommandsForCategory(build) = %v, want 1 command", len(buildCmds))
	}
	if buildCmds[0].Command != "go build" {
		t.Errorf("Command = %v, want 'go build'", buildCmds[0].Command)
	}

	testCmds := artifacts.GetCommandsForCategory("test")
	if len(testCmds) != 1 {
		t.Errorf("GetCommandsForCategory(test) = %v, want 1 command", len(testCmds))
	}

	emptyCmds := artifacts.GetCommandsForCategory("deploy")
	if len(emptyCmds) != 0 {
		t.Errorf("GetCommandsForCategory(deploy) = %v, want 0 commands", len(emptyCmds))
	}

	// Test with ClaudeDir and skills
	artifacts.ClaudeDir = &ClaudeDirectory{
		Path: "/test/dir/.claude",
		Skills: []*Skill{
			{Name: "test-skill", Slug: "test-skill"},
		},
	}

	if !artifacts.HasClaudeDir() {
		t.Error("HasClaudeDir() should return true when ClaudeDir is set")
	}
	if !artifacts.HasSkills() {
		t.Error("HasSkills() should return true when skills exist")
	}
}

func TestArtifactManager(t *testing.T) {
	// Create a temporary directory structure
	tmpDir, err := os.MkdirTemp("", "test-artifacts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewArtifactManager(5 * time.Minute)

	// Test scanning directory without artifacts
	artifacts, err := manager.ScanDirectory(tmpDir)
	if err != nil {
		t.Fatalf("ScanDirectory() error = %v", err)
	}
	if artifacts.Available {
		t.Error("Artifacts should not be available in empty directory")
	}

	// Create CLAUDE.md
	claudeMDPath := filepath.Join(tmpDir, "CLAUDE.md")
	claudeMDContent := strings.Join([]string{
		"# CLAUDE.md",
		"",
		"## Build Commands",
		"",
		"```bash",
		"go build -o bin/app ./cmd/app",
		"```",
		"",
		"## Architecture",
		"",
		"This is a simple architecture.",
	}, "\n")
	if err := os.WriteFile(claudeMDPath, []byte(claudeMDContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Invalidate cache and rescan
	manager.Invalidate(tmpDir)
	artifacts, err = manager.ScanDirectory(tmpDir)
	if err != nil {
		t.Fatalf("ScanDirectory() error = %v", err)
	}
	if !artifacts.Available {
		t.Error("Artifacts should be available after creating CLAUDE.md")
	}
	if !artifacts.HasCLAUDEMD() {
		t.Error("HasCLAUDEMD() should return true")
	}

	// Test GetArtifacts (should use cache)
	cachedArtifacts, err := manager.GetArtifacts(tmpDir)
	if err != nil {
		t.Fatalf("GetArtifacts() error = %v", err)
	}
	if cachedArtifacts.WorkingDir != artifacts.WorkingDir {
		t.Error("GetArtifacts() should return cached artifacts")
	}

	// Test HasArtifacts
	hasArtifacts, err := manager.HasArtifacts(tmpDir)
	if err != nil {
		t.Fatalf("HasArtifacts() error = %v", err)
	}
	if !hasArtifacts {
		t.Error("HasArtifacts() should return true")
	}

	// Test GetCacheStats
	stats := manager.GetCacheStats()
	if scanners, ok := stats["scanners"].(int); !ok || scanners == 0 {
		t.Error("Cache stats should show scanners")
	}

	// Test InvalidateAll
	manager.InvalidateAll()
	artifacts, err = manager.ScanDirectory(tmpDir)
	if err != nil {
		t.Fatalf("ScanDirectory() after InvalidateAll() error = %v", err)
	}
	// Should work fine, just clears cache
}

func TestArtifactScanner(t *testing.T) {
	// Create a temporary directory structure
	tmpDir, err := os.MkdirTemp("", "test-scanner")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cache := NewArtifactCache(5 * time.Minute)
	scanner := NewArtifactScanner(tmpDir, cache)

	// Test scanning empty directory
	artifacts, err := scanner.Scan()
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if artifacts.Available {
		t.Error("Artifacts should not be available in empty directory")
	}

	// Create CLAUDE.md
	claudeMDPath := filepath.Join(tmpDir, "CLAUDE.md")
	claudeMDContent := strings.Join([]string{
		"# CLAUDE.md",
		"",
		"## Build Commands",
		"",
		"```bash",
		"go test ./...",
		"```",
	}, "\n")
	if err := os.WriteFile(claudeMDPath, []byte(claudeMDContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Invalidate cache and rescan
	scanner.InvalidateCache(tmpDir)
	artifacts, err = scanner.Scan()
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if !artifacts.Available {
		t.Error("Artifacts should be available after creating CLAUDE.md")
	}

	// Test cache
	scanner.InvalidateCache(tmpDir)
	artifacts2, err := scanner.Scan()
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if artifacts2.LastScanned != artifacts.LastScanned {
		t.Error("Subsequent scans should use cache and have same timestamp")
	}

	// Test SetWorkingDir
	newDir := "/other/dir"
	scanner.SetWorkingDir(newDir)
	if scanner.GetWorkingDir() != newDir {
		t.Error("SetWorkingDir() should update working directory")
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		substr   string
		expected bool
	}{
		{"exact match", "hello world", "hello world", true},
		{"substring", "hello world", "world", true},
		{"not found", "hello", "world", false},
		{"empty substring", "hello", "", true},
		{"empty string", "", "test", false},
		{"both empty", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contains(tt.s, tt.substr); got != tt.expected {
				t.Errorf("contains() = %v, want %v", got, tt.expected)
			}
		})
	}
}
