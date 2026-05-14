package templates

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTemplateText_Valid(t *testing.T) {
	text := `---
name: summarize
description: "summarize text concisely"
scope: turn
---

Summarize the following text in 2-3 sentences.
Focus on the key points and actionable takeaways.

$@
`

	tmpl, err := ParseTemplateText(text)
	if err != nil {
		t.Fatalf("ParseTemplateText failed: %v", err)
	}

	if tmpl.Name != "summarize" {
		t.Errorf("Name = %q, want summarize", tmpl.Name)
	}

	if tmpl.Description != "summarize text concisely" {
		t.Errorf("Description = %q, want 'summarize text concisely'", tmpl.Description)
	}

	if tmpl.Scope != ScopeTurn {
		t.Errorf("Scope = %q, want turn", tmpl.Scope)
	}

	if tmpl.Body == "" {
		t.Error("Body should not be empty")
	}

	wantBodyContains := "Summarize the following text"
	if !contains(tmpl.Body, wantBodyContains) {
		t.Errorf("Body = %q, want to contain %q", tmpl.Body, wantBodyContains)
	}
}

func TestParseTemplateText_DefaultScopeIsTurn(t *testing.T) {
	text := `---
name: explain
description: "explain code step by step"
---

Explain this code step by step.
`

	tmpl, err := ParseTemplateText(text)
	if err != nil {
		t.Fatalf("ParseTemplateText failed: %v", err)
	}

	if tmpl.Scope != ScopeTurn {
		t.Errorf("Scope = %q, want turn (default)", tmpl.Scope)
	}
}

func TestParseTemplateText_SessionScope(t *testing.T) {
	text := `---
name: role-senior-dev
description: "adopt a senior developer persona"
scope: session
---

You are a senior developer.
`

	tmpl, err := ParseTemplateText(text)
	if err != nil {
		t.Fatalf("ParseTemplateText failed: %v", err)
	}

	if tmpl.Scope != ScopeSession {
		t.Errorf("Scope = %q, want session", tmpl.Scope)
	}
}

func TestParseTemplateText_NoFrontmatter(t *testing.T) {
	text := `# Just a regular markdown file

No frontmatter here.
`

	_, err := ParseTemplateText(text)
	if !errors.Is(err, ErrNoFrontmatter) {
		t.Errorf("Expected ErrNoFrontmatter, got %v", err)
	}
}

func TestParseTemplateText_InvalidYAML(t *testing.T) {
	text := `---
name: [invalid yaml structure
description: broken
---

Body.
`

	_, err := ParseTemplateText(text)
	if !errors.Is(err, ErrInvalidYAML) {
		t.Errorf("Expected ErrInvalidYAML, got %v", err)
	}
}

func TestParseTemplateText_NoName(t *testing.T) {
	text := `---
description: Template without a name
---

Body here.
`

	_, err := ParseTemplateText(text)
	if !errors.Is(err, ErrNoName) {
		t.Errorf("Expected ErrNoName, got %v", err)
	}
}

func TestParseTemplateText_EmptyBody(t *testing.T) {
	text := `---
name: minimal-template
description: A minimal template
---
`

	tmpl, err := ParseTemplateText(text)
	if err != nil {
		t.Fatalf("ParseTemplateText failed: %v", err)
	}

	if tmpl.Name != "minimal-template" {
		t.Errorf("Name = %q, want minimal-template", tmpl.Name)
	}

	// Empty body is valid.
	if tmpl.Body != "" {
		t.Errorf("Body = %q, want empty", tmpl.Body)
	}
}

func TestParseTemplateText_LeadingWhitespace(t *testing.T) {
	text := `

   ---
name: whitespace-template
description: Template with leading whitespace
---

Body.
`

	tmpl, err := ParseTemplateText(text)
	if err != nil {
		t.Fatalf("ParseTemplateText failed: %v", err)
	}

	if tmpl.Name != "whitespace-template" {
		t.Errorf("Name = %q, want whitespace-template", tmpl.Name)
	}
}

func TestParseTemplateText_WithPositionalArgs(t *testing.T) {
	text := `---
name: translate
description: "translate text to a specified language"
---

Translate the following text to $1.
Preserve the original formatting and tone.

$2
`

	tmpl, err := ParseTemplateText(text)
	if err != nil {
		t.Fatalf("ParseTemplateText failed: %v", err)
	}

	if tmpl.Name != "translate" {
		t.Errorf("Name = %q, want translate", tmpl.Name)
	}

	// Verify the body contains the substitution markers.
	if !contains(tmpl.Body, "$1") {
		t.Error("Body should contain $1")
	}
	if !contains(tmpl.Body, "$2") {
		t.Error("Body should contain $2")
	}
}

func TestParseTemplateFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "templates-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	content := `---
name: file-template
description: Template loaded from file
scope: turn
---

This template was loaded from a file.
`
	templatePath := filepath.Join(tmpDir, "test-template.md")
	//nolint:gosec // test directory/file
	if err := os.WriteFile(templatePath, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to write template file: %v", err)
	}

	tmpl, err := ParseTemplateFile(templatePath)
	if err != nil {
		t.Fatalf("ParseTemplateFile failed: %v", err)
	}

	if tmpl.Name != "file-template" {
		t.Errorf("Name = %q, want file-template", tmpl.Name)
	}

	if tmpl.Path != templatePath {
		t.Errorf("Path = %q, want %q", tmpl.Path, templatePath)
	}
}

func TestParseTemplateFile_NotFound(t *testing.T) {
	_, err := ParseTemplateFile("/nonexistent/path/template.md")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}

	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		t.Errorf("Expected ParseError, got %T", err)
	}
}

func TestParseTemplateMetadataOnly(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "templates-meta-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	content := `---
name: meta-only
description: Metadata only template
scope: session
---

This body should not be parsed.
`
	templatePath := filepath.Join(tmpDir, "meta-template.md")
	//nolint:gosec // test directory/file
	if err := os.WriteFile(templatePath, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to write template file: %v", err)
	}

	tmpl, err := ParseTemplateMetadataOnly(templatePath)
	if err != nil {
		t.Fatalf("ParseTemplateMetadataOnly failed: %v", err)
	}

	if tmpl.Name != "meta-only" {
		t.Errorf("Name = %q, want meta-only", tmpl.Name)
	}

	if tmpl.Scope != ScopeSession {
		t.Errorf("Scope = %q, want session", tmpl.Scope)
	}

	// Body should be empty for metadata-only parse.
	if tmpl.Body != "" {
		t.Errorf("Body = %q, want empty (metadata only)", tmpl.Body)
	}
}

func TestSplitFrontmatter_Various(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantFront string
		wantBody  string
		wantErr   error
	}{
		{
			name: "standard",
			input: `---
key: value
---

body content`,
			wantFront: "key: value",
			wantBody:  "body content",
		},
		{
			name: "no closing marker",
			input: `---
key: value

no closing`,
			wantErr: ErrNoFrontmatter,
		},
		{
			name: "no opening marker",
			input: `key: value
---

body`,
			wantErr: ErrNoFrontmatter,
		},
		{
			name: "empty frontmatter",
			input: `---
---

body`,
			wantFront: "",
			wantBody:  "body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			front, body, err := splitFrontmatter(tt.input)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Expected error %v, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			frontTrimmed := strings.TrimSpace(front)
			wantFrontTrimmed := strings.TrimSpace(tt.wantFront)
			if frontTrimmed != wantFrontTrimmed {
				t.Errorf("frontmatter = %q, want %q", frontTrimmed, wantFrontTrimmed)
			}

			bodyTrimmed := strings.TrimSpace(body)
			wantBodyTrimmed := strings.TrimSpace(tt.wantBody)
			if bodyTrimmed != wantBodyTrimmed {
				t.Errorf("body = %q, want %q", bodyTrimmed, wantBodyTrimmed)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || s != "" && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
