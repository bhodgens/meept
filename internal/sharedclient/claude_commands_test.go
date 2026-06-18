// Package sharedclient provides shared client utilities for meept.
package sharedclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverClaudeCommands(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()
	claudeCommands := filepath.Join(tmpDir, ".claude", "commands")
	if err := os.MkdirAll(claudeCommands, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a test command file
	cmdFile := filepath.Join(claudeCommands, "test-cmd.md")
	content := `---
name: test
description: A test command
---
This is a test command with $ARGUMENTS
`
	if err := os.WriteFile(cmdFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Discover commands
	cmds := discoverClaudeCommands(claudeCommands)

	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}

	cmd, ok := cmds["test"]
	if !ok {
		t.Fatal("expected 'test' command to be discovered")
	}

	if cmd.Description != "A test command" {
		t.Errorf("expected description 'A test command', got %q", cmd.Description)
	}

	if cmd.Template != "This is a test command with $ARGUMENTS" {
		t.Errorf("expected template 'This is a test command with $ARGUMENTS', got %q", cmd.Template)
	}
}

func TestDiscoverClaudeCommands_MissingDirectory(t *testing.T) {
	// Should return empty map when directory doesn't exist
	cmds := discoverClaudeCommands("/nonexistent/path/.claude/commands")
	if len(cmds) != 0 {
		t.Errorf("expected empty map for missing directory, got %d commands", len(cmds))
	}
}

func TestDiscoverClaudeCommands_MultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()
	claudeCommands := filepath.Join(tmpDir, ".claude", "commands")
	if err := os.MkdirAll(claudeCommands, 0755); err != nil {
		t.Fatal(err)
	}

	// Create multiple command files
	files := map[string]string{
		"cmd1.md": `---
name: first
description: First command
---
Body 1
`,
		"cmd2.md": `---
name: second
description: Second command
---
Body 2
`,
		"readme.md": `---
name: readme
---
Should be skipped
`,
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(claudeCommands, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	cmds := discoverClaudeCommands(claudeCommands)

	// Should have all 3 files (readme.md is a valid .md file)
	if len(cmds) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(cmds))
	}

	if _, ok := cmds["first"]; !ok {
		t.Error("expected 'first' command")
	}
	if _, ok := cmds["second"]; !ok {
		t.Error("expected 'second' command")
	}
}

func TestClaudeCommandsPath(t *testing.T) {
	path := claudeCommandsPath()
	if path == "" {
		t.Error("expected non-empty path")
	}
	if !filepath.IsAbs(path) {
		t.Error("expected absolute path")
	}
	if !strings.HasSuffix(path, filepath.Join(".claude", "commands")) {
		t.Errorf("expected path to end with .claude/commands, got %q", path)
	}
}
