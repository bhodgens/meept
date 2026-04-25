package clawskills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewIndex(t *testing.T) {
	idx := NewIndex()
	if idx == nil {
		t.Fatal("NewIndex returned nil")
	}
	if idx.Count() != 0 {
		t.Errorf("expected empty index, got %d skills", idx.Count())
	}
	if idx.Version != indexVersion {
		t.Errorf("expected version %q, got %q", indexVersion, idx.Version)
	}
}

func TestIndexSetGet(t *testing.T) {
	idx := NewIndex()

	skill := &InstalledSkill{
		Slug:      "test-skill",
		Name:      "claw:test-skill",
		Version:   "1.0.0",
		RiskLevel: "low", // intentionally low, should be enforced to high
	}
	idx.Set("test-skill", skill)

	got := idx.Get("test-skill")
	if got == nil {
		t.Fatal("expected to find test-skill")
	}

	// Risk level must be enforced to high
	if got.RiskLevel != "high" {
		t.Errorf("expected risk_level 'high', got %q", got.RiskLevel)
	}

	if got.Slug != "test-skill" {
		t.Errorf("expected slug 'test-skill', got %q", got.Slug)
	}
}

func TestIndexRiskEnforcement(t *testing.T) {
	tests := []struct {
		name       string
		riskLevel  string
		expected   string
	}{
		{"low enforced to high", "low", "high"},
		{"medium enforced to high", "medium", "high"},
		{"high stays high", "high", "high"},
		{"empty enforced to high", "", "high"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := NewIndex()
			skill := &InstalledSkill{
				Slug:      "test",
				RiskLevel: tt.riskLevel,
			}
			idx.Set("test", skill)

			got := idx.Get("test")
			if got.RiskLevel != tt.expected {
				t.Errorf("expected risk %q, got %q", tt.expected, got.RiskLevel)
			}
		})
	}
}

func TestIndexMaxIterationsCap(t *testing.T) {
	tests := []struct {
		name         string
		iterations   int
		expected     int
	}{
		{"zero capped to default", 0, DefaultMaxIterations},
		{"negative capped to default", -1, DefaultMaxIterations},
		{"too high capped to default", 100, DefaultMaxIterations},
		{"within range kept", 5, 5},
		{"at boundary kept", DefaultMaxIterations, DefaultMaxIterations},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := NewIndex()
			skill := &InstalledSkill{
				Slug:          "test",
				MaxIterations: tt.iterations,
			}
			idx.Set("test", skill)

			got := idx.Get("test")
			if got.MaxIterations != tt.expected {
				t.Errorf("expected max_iterations %d, got %d", tt.expected, got.MaxIterations)
			}
		})
	}
}

func TestIndexDelete(t *testing.T) {
	idx := NewIndex()
	idx.Set("test", &InstalledSkill{Slug: "test"})
	if idx.Count() != 1 {
		t.Fatal("expected 1 skill")
	}

	idx.Delete("test")
	if idx.Count() != 0 {
		t.Error("expected 0 skills after delete")
	}

	if got := idx.Get("test"); got != nil {
		t.Error("expected nil after delete")
	}
}

func TestIndexList(t *testing.T) {
	idx := NewIndex()
	idx.Set("skill-a", &InstalledSkill{Slug: "skill-a"})
	idx.Set("skill-b", &InstalledSkill{Slug: "skill-b"})

	list := idx.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(list))
	}
}

func TestIndexHasUpdates(t *testing.T) {
	idx := NewIndex()
	if idx.HasUpdates() {
		t.Error("expected no updates for empty index")
	}

	idx.Set("auto", &InstalledSkill{Slug: "auto", AutoUpdate: true})
	if !idx.HasUpdates() {
		t.Error("expected updates after adding auto-update skill")
	}

	idx.Set("manual", &InstalledSkill{Slug: "manual", AutoUpdate: false})
	if !idx.HasUpdates() {
		t.Error("still expected updates (one auto skill)")
	}
}

func TestLoadSaveIndex(t *testing.T) {
	dir := t.TempDir()

	idx := NewIndex()
	idx.Set("test-skill", &InstalledSkill{
		Slug:        "test-skill",
		Name:        "claw:test-skill",
		Version:     "1.0.0",
		InstalledAt: time.Now(),
		Path:        filepath.Join(dir, "test-skill"),
		SHA256:      "abc123",
	})

	if err := idx.Save(dir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex failed: %v", err)
	}

	got := loaded.Get("test-skill")
	if got == nil {
		t.Fatal("expected to find test-skill after reload")
	}
	if got.Slug != "test-skill" {
		t.Errorf("expected slug 'test-skill', got %q", got.Slug)
	}
	if got.RiskLevel != "high" {
		t.Errorf("expected risk 'high' after reload, got %q", got.RiskLevel)
	}
}

func TestLoadIndexMissingFile(t *testing.T) {
	_, err := LoadIndex(t.TempDir())
	if err == nil {
		t.Error("expected error for missing index file")
	}
}

func TestLoadIndexCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, indexFileName), []byte("not json"), 0644)

	_, err := LoadIndex(dir)
	if err == nil {
		t.Error("expected error for corrupt JSON")
	}
}

func TestLoadIndexNullSkills(t *testing.T) {
	dir := t.TempDir()
	// Write JSON with null skills field
	data, _ := json.Marshal(map[string]any{
		"version":    "1.0",
		"updated_at": time.Now(),
		"skills":     nil,
	})
	os.WriteFile(filepath.Join(dir, indexFileName), data, 0644)

	idx, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex failed: %v", err)
	}
	if idx.Count() != 0 {
		t.Error("expected 0 skills from null skills field")
	}
}

func TestClawPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"my-skill", "claw:my-skill"},
		{"claw:already", "claw:already"},
		{"", "claw:"},
	}

	for _, tt := range tests {
		got := PrefixedName(tt.input)
		if got != tt.expected {
			t.Errorf("PrefixedName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestStripPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"claw:my-skill", "my-skill"},
		{"my-skill", "my-skill"},
		{"claw:", ""},
	}

	for _, tt := range tests {
		got := StripPrefix(tt.input)
		if got != tt.expected {
			t.Errorf("StripPrefix(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestIsBlocked(t *testing.T) {
	blocked := []string{"malware-skill", "bad-actor", "exploit-kit"}

	tests := []struct {
		slug     string
		expected bool
	}{
		{"malware-skill", true},
		{"MALWARE-SKILL", true}, // case-insensitive
		{"good-skill", false},
		{"bad-actor", true},
		{"", false},
	}

	for _, tt := range tests {
		got := IsBlocked(tt.slug, blocked)
		if got != tt.expected {
			t.Errorf("IsBlocked(%q) = %v, want %v", tt.slug, got, tt.expected)
		}
	}
}

func TestIsBlockedEmptyBlocklist(t *testing.T) {
	if IsBlocked("anything", nil) {
		t.Error("expected false with nil blocklist")
	}
	if IsBlocked("anything", []string{}) {
		t.Error("expected false with empty blocklist")
	}
}

func TestScanAndLoadEmptyDir(t *testing.T) {
	dir := t.TempDir()
	// Non-existent subdirectory should return empty index, no error
	idx, warnings, err := ScanAndLoad(filepath.Join(dir, "no-such-dir"), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx.Count() != 0 {
		t.Error("expected empty index for non-existent dir")
	}
	if len(warnings) != 0 {
		t.Error("expected no warnings")
	}
}

func TestScanAndLoadWithSkills(t *testing.T) {
	dir := t.TempDir()

	// Create a skill directory with .origin.json
	skillDir := filepath.Join(dir, "test-skill")
	os.MkdirAll(skillDir, 0755)

	origin := map[string]any{
		"name":     "Test Skill",
		"version":  "1.2.3",
		"sha256":   "deadbeef",
		"verified": true,
	}
	originData, _ := json.Marshal(origin)
	os.WriteFile(filepath.Join(skillDir, ".origin.json"), originData, 0644)

	idx, warnings, err := ScanAndLoad(dir, nil, nil)
	if err != nil {
		t.Fatalf("ScanAndLoad failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if idx.Count() != 1 {
		t.Fatalf("expected 1 skill, got %d", idx.Count())
	}

	got := idx.Get("test-skill")
	if got == nil {
		t.Fatal("expected to find test-skill")
	}
	if got.RiskLevel != "high" {
		t.Errorf("expected risk 'high', got %q", got.RiskLevel)
	}
	if got.Version != "1.2.3" {
		t.Errorf("expected version '1.2.3', got %q", got.Version)
	}
	if got.Name != "claw:Test Skill" {
		t.Errorf("expected claw-prefixed name, got %q", got.Name)
	}
	if !got.Verified {
		t.Error("expected verified=true")
	}
}

func TestScanAndLoadBlockedSlug(t *testing.T) {
	dir := t.TempDir()

	// Create a skill directory
	skillDir := filepath.Join(dir, "malware")
	os.MkdirAll(skillDir, 0755)

	idx, warnings, err := ScanAndLoad(dir, []string{"malware"}, nil)
	if err != nil {
		t.Fatalf("ScanAndLoad failed: %v", err)
	}
	if idx.Count() != 0 {
		t.Error("expected 0 skills (blocked)")
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
}

func TestScanAndLoadCorruptOrigin(t *testing.T) {
	dir := t.TempDir()

	skillDir := filepath.Join(dir, "corrupt-skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, ".origin.json"), []byte("not json"), 0644)

	idx, warnings, err := ScanAndLoad(dir, nil, nil)
	if err != nil {
		t.Fatalf("ScanAndLoad failed: %v", err)
	}
	// Corrupt origin should be skipped
	if idx.Count() != 0 {
		t.Errorf("expected 0 skills, got %d", idx.Count())
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
}

func TestScanAndLoadNoOrigin(t *testing.T) {
	dir := t.TempDir()

	// Skill directory with no .origin.json - should still be indexed with defaults
	skillDir := filepath.Join(dir, "simple-skill")
	os.MkdirAll(skillDir, 0755)

	idx, _, err := ScanAndLoad(dir, nil, nil)
	if err != nil {
		t.Fatalf("ScanAndLoad failed: %v", err)
	}
	if idx.Count() != 1 {
		t.Fatalf("expected 1 skill, got %d", idx.Count())
	}

	got := idx.Get("simple-skill")
	if got.Name != "claw:simple-skill" {
		t.Errorf("expected claw-prefixed slug as name, got %q", got.Name)
	}
	if got.RiskLevel != "high" {
		t.Errorf("expected risk 'high', got %q", got.RiskLevel)
	}
	if got.MaxIterations != DefaultMaxIterations {
		t.Errorf("expected max_iterations %d, got %d", DefaultMaxIterations, got.MaxIterations)
	}
}

func TestScanAndLoadSkipsFiles(t *testing.T) {
	dir := t.TempDir()

	// Create a regular file (not a directory) -- should be skipped
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0644)

	idx, _, err := ScanAndLoad(dir, nil, nil)
	if err != nil {
		t.Fatalf("ScanAndLoad failed: %v", err)
	}
	if idx.Count() != 0 {
		t.Error("expected 0 skills (no directories)")
	}
}
