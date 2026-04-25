package clawskills

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultInstallerConfig(t *testing.T) {
	cfg := DefaultInstallerConfig()
	if cfg.SkillsDir == "" {
		t.Error("expected non-empty SkillsDir")
	}
	if cfg.Verify != true {
		t.Error("expected Verify=true by default")
	}
}

func TestNewInstaller(t *testing.T) {
	dir := t.TempDir()
	cfg := InstallerConfig{
		SkillsDir:  dir,
		AutoUpdate: false,
		Verify:     true,
	}

	installer, err := NewInstaller(nil, cfg, nil)
	if err != nil {
		t.Fatalf("NewInstaller failed: %v", err)
	}
	defer installer.Close()

	if installer.index == nil {
		t.Error("expected non-nil index")
	}
	if installer.security == nil {
		t.Error("expected non-nil security checker")
	}
}

func TestInstallerInstall(t *testing.T) {
	// Create a mock server that handles resolve, detail, and download
	skill := RemoteSkill{
		Slug: "test-skill", Name: "Test Skill", Version: "1.0.0",
		Description: "A test skill", Verified: true,
	}
	skillBody, _ := json.Marshal(skill)

	resolveResult := ResolveResult{
		Slug: "test-skill", Version: "1.0.0", SHA256: "abc123",
	}
	resolveBody, _ := json.Marshal(resolveResult)

	// Create a valid zip with SKILL.md
	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	w, _ := zipWriter.Create("SKILL.md")
	w.Write([]byte("---\nname: Test Skill\n---\n# Test\nHello world"))
	zipWriter.Close()

	downloadData := zipBuf.Bytes()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/resolve":
			w.Header().Set("Content-Type", "application/json")
			w.Write(resolveBody)
		case "/api/v1/skills/test-skill":
			w.Header().Set("Content-Type", "application/json")
			w.Write(skillBody)
		case "/api/v1/download":
			w.Write(downloadData)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	cfg := InstallerConfig{
		SkillsDir: dir,
		Verify:    false, // Skip SHA verification for mock
	}

	client := NewClient(WithBaseURL(server.URL))
	defer client.Close()

	installer, err := NewInstaller(client, cfg, nil)
	if err != nil {
		t.Fatalf("NewInstaller failed: %v", err)
	}
	defer installer.Close()

	installed, err := installer.Install(context.Background(), "test-skill", "")
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify installed skill properties
	if installed.Slug != "test-skill" {
		t.Errorf("expected slug 'test-skill', got %q", installed.Slug)
	}
	if installed.Name != "claw:Test Skill" {
		t.Errorf("expected claw-prefixed name, got %q", installed.Name)
	}
	if installed.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", installed.Version)
	}
	if installed.RiskLevel != "high" {
		t.Errorf("expected risk 'high', got %q", installed.RiskLevel)
	}
	if installed.MaxIterations != DefaultMaxIterations {
		t.Errorf("expected max_iterations %d, got %d", DefaultMaxIterations, installed.MaxIterations)
	}

	// Verify files were extracted
	if _, err := os.Stat(filepath.Join(dir, "test-skill", "SKILL.md")); err != nil {
		t.Errorf("SKILL.md not extracted: %v", err)
	}
}

func TestInstallerUninstall(t *testing.T) {
	dir := t.TempDir()
	cfg := InstallerConfig{SkillsDir: dir, Verify: false}

	installer, _ := NewInstaller(nil, cfg, nil)
	defer installer.Close()

	// Manually add a skill to the index
	skillDir := filepath.Join(dir, "test-skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("test"), 0644)

	installer.index.Set("test-skill", &InstalledSkill{
		Slug: "test-skill",
		Path: skillDir,
	})

	if err := installer.Uninstall("test-skill"); err != nil {
		t.Fatalf("Uninstall failed: %v", err)
	}

	if installer.Get("test-skill") != nil {
		t.Error("expected skill to be removed from index")
	}
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Error("expected skill directory to be removed")
	}
}

func TestInstallerUninstallNotInstalled(t *testing.T) {
	dir := t.TempDir()
	cfg := InstallerConfig{SkillsDir: dir, Verify: false}

	installer, _ := NewInstaller(nil, cfg, nil)
	defer installer.Close()

	err := installer.Uninstall("nonexistent")
	if err == nil {
		t.Error("expected error for uninstalling non-existent skill")
	}
}

func TestInstallerList(t *testing.T) {
	dir := t.TempDir()
	cfg := InstallerConfig{SkillsDir: dir, Verify: false}

	installer, _ := NewInstaller(nil, cfg, nil)
	defer installer.Close()

	installer.index.Set("skill-a", &InstalledSkill{Slug: "skill-a"})
	installer.index.Set("skill-b", &InstalledSkill{Slug: "skill-b"})

	list := installer.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(list))
	}
}

func TestInstallerGet(t *testing.T) {
	dir := t.TempDir()
	cfg := InstallerConfig{SkillsDir: dir, Verify: false}

	installer, _ := NewInstaller(nil, cfg, nil)
	defer installer.Close()

	installer.index.Set("test", &InstalledSkill{Slug: "test", Version: "1.0"})

	got := installer.Get("test")
	if got == nil || got.Version != "1.0" {
		t.Error("expected to find test skill")
	}

	if got := installer.Get("missing"); got != nil {
		t.Error("expected nil for missing skill")
	}
}

func TestExtractZipPathTraversal(t *testing.T) {
	dir := t.TempDir()
	cfg := InstallerConfig{SkillsDir: dir, Verify: false}

	installer, _ := NewInstaller(nil, cfg, nil)
	defer installer.Close()

	// Create a zip with path traversal
	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	w, _ := zipWriter.Create("../../../etc/passwd")
	w.Write([]byte("malicious"))
	zipWriter.Close()

	targetDir := filepath.Join(dir, "traversal-test")
	err := installer.extractZip(zipBuf.Bytes(), targetDir)
	if err != nil {
		// extractZip may succeed but skip the file; check the file was not created
	}

	// The traversal file should not exist outside targetDir
	traversalPath := filepath.Join(dir, "..", "..", "..", "etc", "passwd")
	if _, err := os.Stat(traversalPath); err == nil {
		t.Error("path traversal file should not exist")
	}
}

func TestExtractZipNormal(t *testing.T) {
	dir := t.TempDir()
	cfg := InstallerConfig{SkillsDir: dir, Verify: false}

	installer, _ := NewInstaller(nil, cfg, nil)
	defer installer.Close()

	// Create a valid zip
	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	w, _ := zipWriter.Create("SKILL.md")
	w.Write([]byte("---\nname: test\n---\nbody"))
	w, _ = zipWriter.Create("subdir/file.txt")
	w.Write([]byte("content"))
	zipWriter.Close()

	targetDir := filepath.Join(dir, "normal-test")
	if err := installer.extractZip(zipBuf.Bytes(), targetDir); err != nil {
		t.Fatalf("extractZip failed: %v", err)
	}

	// Verify files were extracted
	if _, err := os.Stat(filepath.Join(targetDir, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md not extracted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "subdir", "file.txt")); err != nil {
		t.Errorf("subdir/file.txt not extracted: %v", err)
	}
}

func TestExtractZipEmpty(t *testing.T) {
	dir := t.TempDir()
	cfg := InstallerConfig{SkillsDir: dir, Verify: false}

	installer, _ := NewInstaller(nil, cfg, nil)
	defer installer.Close()

	// Create an empty zip
	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	zipWriter.Close()

	targetDir := filepath.Join(dir, "empty-test")
	if err := installer.extractZip(zipBuf.Bytes(), targetDir); err != nil {
		t.Fatalf("extractZip failed for empty zip: %v", err)
	}
}

func TestInstallerCloseSavesIndex(t *testing.T) {
	dir := t.TempDir()
	cfg := InstallerConfig{SkillsDir: dir, Verify: false}

	installer, _ := NewInstaller(nil, cfg, nil)
	installer.index.Set("test", &InstalledSkill{
		Slug:    "test",
		Version: "1.0",
	})

	if err := installer.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify index was saved
	if _, err := os.Stat(filepath.Join(dir, indexFileName)); os.IsNotExist(err) {
		t.Error("index file should have been saved")
	}
}

func TestInstallerCreateDirFailure(t *testing.T) {
	// Use a path that cannot be created
	cfg := InstallerConfig{
		SkillsDir: "/dev/null/impossible",
		Verify:    false,
	}

	_, err := NewInstaller(nil, cfg, nil)
	if err == nil {
		t.Error("expected error for impossible directory")
	}
}

func TestInstallerInstallAlreadyInstalled(t *testing.T) {
	dir := t.TempDir()
	cfg := InstallerConfig{SkillsDir: dir, Verify: false}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should not be called
		w.WriteHeader(500)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	defer client.Close()

	installer, _ := NewInstaller(client, cfg, nil)
	defer installer.Close()

	// Pre-install the skill
	installer.index.Set("test-skill", &InstalledSkill{
		Slug:    "test-skill",
		Version: "1.0.0",
	})

	// Installing same version should return existing without making API calls
	installed, err := installer.Install(context.Background(), "test-skill", "1.0.0")
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	if installed.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %q", installed.Version)
	}
}

func TestInstallerUpdateNotInstalled(t *testing.T) {
	dir := t.TempDir()
	cfg := InstallerConfig{SkillsDir: dir, Verify: false}

	installer, _ := NewInstaller(nil, cfg, nil)
	defer installer.Close()

	_, err := installer.Update(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for updating non-installed skill")
	}
}

func TestInstallerUpdateAllEmpty(t *testing.T) {
	dir := t.TempDir()
	cfg := InstallerConfig{SkillsDir: dir, Verify: false}

	installer, _ := NewInstaller(nil, cfg, nil)
	defer installer.Close()

	updated, errors := installer.UpdateAll(context.Background())
	if len(updated) != 0 {
		t.Errorf("expected no updates, got %d", len(updated))
	}
	if len(errors) != 0 {
		t.Errorf("expected no errors, got %d", len(errors))
	}
}
