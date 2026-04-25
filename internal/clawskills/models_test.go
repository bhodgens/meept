package clawskills

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRemoteSkillJSON(t *testing.T) {
	skill := RemoteSkill{
		Slug:         "test",
		Name:         "Test Skill",
		Description:  "A test",
		Author:       "tester",
		Version:      "1.0.0",
		Downloads:    42,
		Stars:        5,
		Tags:         []string{"test", "example"},
		Requirements: []string{"code"},
		Capabilities: []string{"reasoning"},
		Verified:     true,
	}

	data, err := json.Marshal(skill)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded RemoteSkill
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Slug != skill.Slug {
		t.Errorf("slug mismatch: %q != %q", decoded.Slug, skill.Slug)
	}
	if decoded.Downloads != skill.Downloads {
		t.Errorf("downloads mismatch: %d != %d", decoded.Downloads, skill.Downloads)
	}
	if len(decoded.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(decoded.Tags))
	}
	if !decoded.Verified {
		t.Error("expected verified=true")
	}
}

func TestInstalledSkillJSON(t *testing.T) {
	now := time.Now()
	skill := InstalledSkill{
		Slug:          "my-skill",
		Name:          "claw:My Skill",
		Version:       "2.0.0",
		InstalledAt:   now,
		Path:          "/tmp/skills/my-skill",
		SHA256:        "deadbeef",
		AutoUpdate:    true,
		Verified:      false,
		RiskLevel:     "high",
		MaxIterations: 10,
	}

	data, err := json.Marshal(skill)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded InstalledSkill
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Slug != skill.Slug {
		t.Errorf("slug mismatch: %q != %q", decoded.Slug, skill.Slug)
	}
	if decoded.RiskLevel != "high" {
		t.Errorf("expected risk 'high', got %q", decoded.RiskLevel)
	}
	if decoded.MaxIterations != 10 {
		t.Errorf("expected max_iterations 10, got %d", decoded.MaxIterations)
	}
	if decoded.AutoUpdate != true {
		t.Error("expected auto_update=true")
	}
}

func TestSearchResultJSON(t *testing.T) {
	result := SearchResult{
		Slug:        "search-hit",
		Name:        "Search Hit",
		Description: "A search result",
		Author:      "author",
		Version:     "3.0.0",
		Downloads:   100,
		Stars:       10,
		Tags:        []string{"search"},
		Verified:    true,
		Score:       0.95,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded SearchResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Score != 0.95 {
		t.Errorf("score mismatch: %f != %f", decoded.Score, 0.95)
	}
}

func TestVerificationResult(t *testing.T) {
	result := &VerificationResult{
		Valid:       true,
		SHA256Match: true,
		Signed:      false,
		Warnings:    []string{"not verified"},
		Errors:      nil,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded VerificationResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if !decoded.Valid {
		t.Error("expected valid=true")
	}
	if !decoded.SHA256Match {
		t.Error("expected sha256_match=true")
	}
	if decoded.Signed {
		t.Error("expected signed=false")
	}
	if len(decoded.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(decoded.Warnings))
	}
}

func TestDownloadResult(t *testing.T) {
	result := &DownloadResult{
		Data:   []byte("test data"),
		SHA256: "abc123",
		Size:   9,
	}

	if result.Size != int64(len(result.Data)) {
		t.Errorf("size mismatch: %d != %d", result.Size, len(result.Data))
	}
}

func TestResolveResultJSON(t *testing.T) {
	result := ResolveResult{
		Slug:         "resolved",
		Version:      "4.0.0",
		SHA256:       "feedbeef",
		DownloadURL:  "https://example.com/download",
		Dependencies: []string{"dep1", "dep2"},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ResolveResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Slug != "resolved" {
		t.Errorf("slug mismatch: %q != %q", decoded.Slug, "resolved")
	}
	if len(decoded.Dependencies) != 2 {
		t.Errorf("expected 2 dependencies, got %d", len(decoded.Dependencies))
	}
}

func TestSkillVersionJSON(t *testing.T) {
	now := time.Now()
	version := SkillVersion{
		Version:     "1.0.0",
		SHA256:      "abc",
		ReleaseNote: "Initial release",
		CreatedAt:   now,
		Size:        1024,
	}

	data, err := json.Marshal(version)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded SkillVersion
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Size != 1024 {
		t.Errorf("size mismatch: %d != %d", decoded.Size, 1024)
	}
}

func TestAPIErrorMessage(t *testing.T) {
	err := &APIError{StatusCode: 429, Detail: "rate limited"}

	if err.Error() != "ClawHub API error 429: rate limited" {
		t.Errorf("unexpected error string: %q", err.Error())
	}
}
