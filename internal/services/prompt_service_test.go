package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPromptService_List(t *testing.T) {
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	bundledDir := filepath.Join(tmp, "bundled")
	// seed files
	mustWriteFile(t, filepath.Join(bundledDir, "planner", "decompose.md"), "---\nname: x\n---\nHELLO {{.Input}}")
	mustWriteFile(t, filepath.Join(bundledDir, "planner", "interview.md"), "INTERVIEW")
	mustWriteFile(t, filepath.Join(projectDir, "planner", "decompose.md"), "PROJECT OVERRIDE")

	svc := NewPromptService(projectDir, filepath.Join(tmp, "user"), filepath.Join(tmp, "system"), bundledDir)
	entries, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// decompose should resolve to project tier
	var foundDecompose, foundInterview bool
	for _, e := range entries {
		if e.Name == "planner/decompose.md" {
			foundDecompose = true
			if e.Tier != TierProject {
				t.Errorf("decompose tier = %s, want project", e.Tier)
			}
		}
		if e.Name == "planner/interview.md" {
			foundInterview = true
			if e.Tier != TierBundled {
				t.Errorf("interview tier = %s, want bundled", e.Tier)
			}
		}
	}
	if !foundDecompose || !foundInterview {
		t.Errorf("missing entries: decompose=%v interview=%v", foundDecompose, foundInterview)
	}
}

func TestPromptService_Get(t *testing.T) {
	tmp := t.TempDir()
	bundledDir := filepath.Join(tmp, "bundled")
	mustWriteFile(t, filepath.Join(bundledDir, "planner", "interview.md"), "---\nname: x\n---\nBODY {{.Input}}")

	svc := NewPromptService(filepath.Join(tmp, "p"), filepath.Join(tmp, "u"), filepath.Join(tmp, "s"), bundledDir)
	detail, err := svc.Get("planner/interview.md")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if detail.Tier != TierBundled {
		t.Errorf("tier = %s, want bundled", detail.Tier)
	}
	if detail.Content == "" {
		t.Error("content is empty")
	}
}

func TestPromptService_Get_Shorthand(t *testing.T) {
	tmp := t.TempDir()
	bundledDir := filepath.Join(tmp, "bundled")
	mustWriteFile(t, filepath.Join(bundledDir, "planner", "decompose.md"), "BODY")

	svc := NewPromptService(filepath.Join(tmp, "p"), filepath.Join(tmp, "u"), filepath.Join(tmp, "s"), bundledDir)
	// "decompose" shorthand should resolve to "planner/decompose.md"
	detail, err := svc.Get("decompose")
	if err != nil {
		t.Fatalf("Get shorthand: %v", err)
	}
	if detail.Name != "planner/decompose.md" {
		t.Errorf("name = %s, want planner/decompose.md", detail.Name)
	}
}

func TestPromptService_Get_NotFound(t *testing.T) {
	svc := NewPromptService(t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir())
	_, err := svc.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing template")
	}
}

func TestPromptService_Put(t *testing.T) {
	tmp := t.TempDir()
	userDir := filepath.Join(tmp, "user")
	bundledDir := filepath.Join(tmp, "bundled")
	mustWriteFile(t, filepath.Join(bundledDir, "planner", "decompose.md"), "ORIGINAL {{.Input}}")

	svc := NewPromptService(filepath.Join(tmp, "project"), userDir, filepath.Join(tmp, "system"), bundledDir)
	content := "---\nname: override\n---\nOVERRIDE {{.Input}}"
	if err := svc.Put("planner/decompose.md", content); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Verify override was written to user tier
	overridePath := filepath.Join(userDir, "planner", "decompose.md")
	body, err := os.ReadFile(overridePath)
	if err != nil {
		t.Fatalf("read override: %v", err)
	}
	if string(body) != content {
		t.Errorf("override content mismatch")
	}
	// Verify Get now returns the user tier
	detail, _ := svc.Get("planner/decompose.md")
	if detail.Tier != TierUser {
		t.Errorf("tier after put = %s, want user", detail.Tier)
	}
}

func TestPromptService_Put_ValidationFails(t *testing.T) {
	tmp := t.TempDir()
	svc := NewPromptService(filepath.Join(tmp, "p"), filepath.Join(tmp, "u"), filepath.Join(tmp, "s"), filepath.Join(tmp, "b"))
	// malformed template
	err := svc.Put("planner/broken.md", "{{ .Broken")
	if err == nil {
		t.Fatal("expected validation error for malformed template")
	}
}

func TestPromptService_Put_EmptyContent(t *testing.T) {
	svc := NewPromptService(t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir())
	err := svc.Put("planner/empty.md", "")
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestPromptService_Delete(t *testing.T) {
	tmp := t.TempDir()
	userDir := filepath.Join(tmp, "user")
	bundledDir := filepath.Join(tmp, "bundled")
	mustWriteFile(t, filepath.Join(bundledDir, "planner", "decompose.md"), "BUNDLED")
	svc := NewPromptService(filepath.Join(tmp, "p"), userDir, filepath.Join(tmp, "s"), bundledDir)

	// Put an override
	if err := svc.Put("planner/decompose.md", "USER OVERRIDE {{.X}}"); err != nil {
		t.Fatal(err)
	}
	// Delete it
	if err := svc.Delete("planner/decompose.md"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Verify it falls back to bundled
	detail, _ := svc.Get("planner/decompose.md")
	if detail.Tier != TierBundled {
		t.Errorf("tier after delete = %s, want bundled", detail.Tier)
	}
}

func TestPromptService_Delete_NoOverride(t *testing.T) {
	svc := NewPromptService(t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir())
	err := svc.Delete("planner/none.md")
	if err == nil {
		t.Fatal("expected error when no override exists")
	}
}

func TestPromptService_ValidateAll(t *testing.T) {
	tmp := t.TempDir()
	bundledDir := filepath.Join(tmp, "bundled")
	mustWriteFile(t, filepath.Join(bundledDir, "planner", "good.md"), "HELLO {{.X}}")
	mustWriteFile(t, filepath.Join(bundledDir, "planner", "bad.md"), "{{ .Broken")

	svc := NewPromptService(filepath.Join(tmp, "p"), filepath.Join(tmp, "u"), filepath.Join(tmp, "s"), bundledDir)
	errs := svc.ValidateAll()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if errs[0].Name != "planner/bad.md" {
		t.Errorf("error name = %s, want planner/bad.md", errs[0].Name)
	}
}

func TestPromptService_ValidateOne(t *testing.T) {
	tmp := t.TempDir()
	bundledDir := filepath.Join(tmp, "bundled")
	mustWriteFile(t, filepath.Join(bundledDir, "planner", "good.md"), "HELLO {{.X}}")

	svc := NewPromptService(filepath.Join(tmp, "p"), filepath.Join(tmp, "u"), filepath.Join(tmp, "s"), bundledDir)
	if err := svc.ValidateOne("planner/good.md"); err != nil {
		t.Errorf("ValidateOne good: %v", err)
	}
	if err := svc.ValidateOne("planner/missing.md"); err == nil {
		t.Error("expected error for missing template")
	}
}

func TestValidateTemplate(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{"valid", "HELLO {{.X}}", false},
		{"valid_with_frontmatter", "---\nname: test\n---\nBODY {{.X}}", false},
		{"empty", "", true},
		{"whitespace_only", "   \n  ", true},
		{"malformed", "{{ .Broken", true},
		{"plain_text", "just text, no placeholders", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTemplate(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTemplate(%s) err = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"interview", "planner/interview.md"},
		{"planner/interview.md", "planner/interview.md"},
		{"orchestrator/split.md", "orchestrator/split.md"},
		{"/planner/foo.md", "planner/foo.md"},
		{"reflection/turn", "reflection/turn.md"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeName(tt.input)
			if got != tt.want {
				t.Errorf("normalizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// mustWriteFile creates directories and writes a file, failing the test on error.
func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
