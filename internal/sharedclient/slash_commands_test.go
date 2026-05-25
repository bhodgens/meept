package sharedclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenderTemplateArguments(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		args []string
		want string
	}{
		{
			name: "no arguments",
			tmpl: "hello world",
			args: nil,
			want: "hello world",
		},
		{
			name: "$ARGUMENTS substitution",
			tmpl: "deploy to $ARGUMENTS now",
			args: []string{"production", "us-east"},
			want: "deploy to production us-east now",
		},
		{
			name: "$ARGUMENTS with no args",
			tmpl: "deploy to $ARGUMENTS now",
			args: nil,
			want: "deploy to  now",
		},
		{
			name: "positional $1",
			tmpl: "run goose $1",
			args: []string{"up"},
			want: "run goose up",
		},
		{
			name: "positional $1 $2",
			tmpl: "move $1 to $2",
			args: []string{"file.txt", "backup/"},
			want: "move file.txt to backup/",
		},
		{
			name: "positional $1 $2 $3",
			tmpl: "create $1 named $2 in $3",
			args: []string{"resource", "my-thing", "namespace"},
			want: "create resource named my-thing in namespace",
		},
		{
			name: "mixed $ARGUMENTS and positional",
			tmpl: "deploy $1 with args: $ARGUMENTS",
			args: []string{"app", "--force"},
			want: "deploy app with args: app --force",
		},
		{
			name: "extra positional not in template",
			tmpl: "only $1 here",
			args: []string{"used", "unused"},
			want: "only used here",
		},
		{
			name: "template with no placeholders",
			tmpl: "static content",
			args: []string{"ignored"},
			want: "static content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderTemplate(tt.tmpl, tt.args)
			if got != tt.want {
				t.Errorf("RenderTemplate(%q, %v) = %q, want %q", tt.tmpl, tt.args, got, tt.want)
			}
		})
	}
}

func TestDiscoverCustomCommands(t *testing.T) {
	// Create two temp directories: one simulating user-global, one project-local.
	userDir := t.TempDir()
	projectDir := t.TempDir()

	userCmdDir := filepath.Join(userDir, ".meept", "commands")
	projectCmdDir := filepath.Join(projectDir, ".meept", "commands")

	if err := os.MkdirAll(userCmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectCmdDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write user-global command
	userGlobal := `---
name: "global-hello"
description: "global hello command"
arguments: ["name"]
---
Hello $1 from global
`
	if err := os.WriteFile(filepath.Join(userCmdDir, "global-hello.md"), []byte(userGlobal), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write project-local command that overrides the global one
	projectOverride := `---
name: "global-hello"
description: "project-local override"
arguments: ["name"]
---
Hi there $1 from project
`
	if err := os.WriteFile(filepath.Join(projectCmdDir, "global-hello.md"), []byte(projectOverride), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a project-only command
	projectOnly := `---
name: "deploy"
description: "deploy the app"
---
deploying $ARGUMENTS
`
	if err := os.WriteFile(filepath.Join(projectCmdDir, "deploy.md"), []byte(projectOnly), 0o644); err != nil {
		t.Fatal(err)
	}

	// Use loadCommandsFromDir directly for testing (avoids cwd/home deps).
	cmds := make(map[string]CustomCommand)
	loadCommandsFromDir(cmds, userCmdDir)
	loadCommandsFromDir(cmds, projectCmdDir)

	// Verify project-local overrides user-global
	cmd, ok := cmds["global-hello"]
	if !ok {
		t.Fatal("global-hello command not found")
	}
	if cmd.Description != "project-local override" {
		t.Errorf("global-hello description = %q, want %q", cmd.Description, "project-local override")
	}
	if cmd.Template != "Hi there $1 from project" {
		t.Errorf("global-hello template = %q, want %q", cmd.Template, "Hi there $1 from project")
	}

	// Verify project-only command exists
	cmd, ok = cmds["deploy"]
	if !ok {
		t.Fatal("deploy command not found")
	}
	if cmd.Description != "deploy the app" {
		t.Errorf("deploy description = %q, want %q", cmd.Description, "deploy the app")
	}
}

func TestParseCommandFileWithFrontmatter(t *testing.T) {
	dir := t.TempDir()

	content := `---
name: "migrate"
description: "Run database migrations"
arguments: ["direction"]
---
Run the following command: goose $1
`
	path := filepath.Join(dir, "migrate.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd, err := parseCommandFile(path)
	if err != nil {
		t.Fatalf("parseCommandFile() error: %v", err)
	}

	if cmd.Name != "migrate" {
		t.Errorf("Name = %q, want %q", cmd.Name, "migrate")
	}
	if cmd.Description != "Run database migrations" {
		t.Errorf("Description = %q, want %q", cmd.Description, "Run database migrations")
	}
	if len(cmd.Arguments) != 1 || cmd.Arguments[0] != "direction" {
		t.Errorf("Arguments = %v, want [direction]", cmd.Arguments)
	}
	if cmd.Template != "Run the following command: goose $1" {
		t.Errorf("Template = %q, want %q", cmd.Template, "Run the following command: goose $1")
	}
}

func TestParseCommandFileNoFrontmatter(t *testing.T) {
	dir := t.TempDir()

	content := "Just a simple template with no metadata.\n"
	path := filepath.Join(dir, "simple.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd, err := parseCommandFile(path)
	if err != nil {
		t.Fatalf("parseCommandFile() error: %v", err)
	}

	// Name should be derived from filename
	if cmd.Name != "simple" {
		t.Errorf("Name = %q, want %q", cmd.Name, "simple")
	}
	if cmd.Description != "" {
		t.Errorf("Description = %q, want empty", cmd.Description)
	}
	if cmd.Template != "Just a simple template with no metadata." {
		t.Errorf("Template = %q, want %q", cmd.Template, "Just a simple template with no metadata.")
	}
}

func TestParseCommandFileEmptyBody(t *testing.T) {
	dir := t.TempDir()

	content := `---
name: "empty-body"
description: "has no body"
---
`
	path := filepath.Join(dir, "empty-body.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd, err := parseCommandFile(path)
	if err != nil {
		t.Fatalf("parseCommandFile() error: %v", err)
	}
	if cmd.Name != "empty-body" {
		t.Errorf("Name = %q, want %q", cmd.Name, "empty-body")
	}
	// Empty body is acceptable; Template should be empty string
	if cmd.Template != "" {
		t.Errorf("Template = %q, want empty", cmd.Template)
	}
}

func TestParseCommandFileInvalidName(t *testing.T) {
	dir := t.TempDir()

	content := `---
name: "has spaces"
---
body
`
	path := filepath.Join(dir, "bad-name.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := parseCommandFile(path)
	if err == nil {
		t.Error("expected error for invalid command name, got nil")
	}
}

func TestParseCommandFileInvalidFrontmatter(t *testing.T) {
	dir := t.TempDir()

	content := `---
not: valid: yaml: [broken
---
body
`
	path := filepath.Join(dir, "bad-yaml.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := parseCommandFile(path)
	if err == nil {
		t.Error("expected error for invalid YAML frontmatter, got nil")
	}
}

func TestIsCustomCommand(t *testing.T) {
	// Set up custom commands
	SetCustomCommands(map[string]CustomCommand{
		"migrate": {Name: "migrate", Template: "goose $1"},
	})
	defer SetCustomCommands(nil)

	if !IsCustomCommand("migrate") {
		t.Error("IsCustomCommand(migrate) = false, want true")
	}
	if IsCustomCommand("nonexistent") {
		t.Error("IsCustomCommand(nonexistent) = true, want false")
	}
	if IsCustomCommand("help") {
		t.Error("IsCustomCommand(help) = true, want false (it's a builtin)")
	}
}

func TestGetCustomCommand(t *testing.T) {
	cmd := CustomCommand{
		Name:        "migrate",
		Description: "Run database migrations",
		Arguments:   []string{"direction"},
		Template:    "goose $1",
	}
	SetCustomCommands(map[string]CustomCommand{"migrate": cmd})
	defer SetCustomCommands(nil)

	got, ok := GetCustomCommand("migrate")
	if !ok {
		t.Fatal("GetCustomCommand(migrate) returned not ok")
	}
	if got.Name != "migrate" {
		t.Errorf("Name = %q, want %q", got.Name, "migrate")
	}
	if got.Template != "goose $1" {
		t.Errorf("Template = %q, want %q", got.Template, "goose $1")
	}

	_, ok = GetCustomCommand("nonexistent")
	if ok {
		t.Error("GetCustomCommand(nonexistent) returned ok, want false")
	}
}

func TestCustomCommandNames(t *testing.T) {
	SetCustomCommands(map[string]CustomCommand{
		"zebra":   {Name: "zebra"},
		"alpha":   {Name: "alpha"},
		"middle":  {Name: "middle"},
	})
	defer SetCustomCommands(nil)

	names := CustomCommandNames()
	expected := []string{"alpha", "middle", "zebra"}
	if len(names) != len(expected) {
		t.Fatalf("CustomCommandNames() = %v, want %v", names, expected)
	}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("names[%d] = %q, want %q", i, name, expected[i])
		}
	}
}

func TestCustomCommandNamesNil(t *testing.T) {
	SetCustomCommands(nil)
	names := CustomCommandNames()
	if names != nil {
		t.Errorf("CustomCommandNames() = %v, want nil", names)
	}
}

func TestPriorityProjectOverridesUser(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	userCmdDir := filepath.Join(userDir, "commands")
	projectCmdDir := filepath.Join(projectDir, "commands")

	if err := os.MkdirAll(userCmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectCmdDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// User-global version
	if err := os.WriteFile(
		filepath.Join(userCmdDir, "build.md"),
		[]byte("---\nname: build\ndescription: user build\n---\nuser build template"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	// Project-local version (should override)
	if err := os.WriteFile(
		filepath.Join(projectCmdDir, "build.md"),
		[]byte("---\nname: build\ndescription: project build\n---\nproject build template"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cmds := make(map[string]CustomCommand)
	loadCommandsFromDir(cmds, userCmdDir)
	loadCommandsFromDir(cmds, projectCmdDir)

	cmd, ok := cmds["build"]
	if !ok {
		t.Fatal("build command not found")
	}
	if cmd.Description != "project build" {
		t.Errorf("Description = %q, want %q", cmd.Description, "project build")
	}
	if cmd.Template != "project build template" {
		t.Errorf("Template = %q, want %q", cmd.Template, "project build template")
	}
}

func TestNewBuiltinCommands(t *testing.T) {
	// Verify the three new builtins are registered
	for _, name := range []string{"diff", "model", "compact"} {
		if !IsBuiltin(name) {
			t.Errorf("IsBuiltin(%q) = false, want true", name)
		}
	}

	// Verify they appear in BuiltinCommands list
	cmds := BuiltinCommands()
	found := map[string]bool{}
	for _, c := range cmds {
		found[c] = true
	}
	for _, name := range []string{"diff", "model", "compact"} {
		if !found[name] {
			t.Errorf("BuiltinCommands() missing %q", name)
		}
	}
}

func TestDiscoverCustomCommandsNonexistentDir(t *testing.T) {
	// loadCommandsFromDir on a nonexistent directory should silently return
	cmds := make(map[string]CustomCommand)
	loadCommandsFromDir(cmds, "/nonexistent/path/commands")
	if len(cmds) != 0 {
		t.Errorf("expected empty map for nonexistent dir, got %d entries", len(cmds))
	}
}

func TestDiscoverCustomCommandsIgnoresNonMarkdown(t *testing.T) {
	dir := t.TempDir()
	// Create a non-markdown file
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a command"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create a subdirectory
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a valid command
	if err := os.WriteFile(filepath.Join(dir, "valid.md"), []byte("---\nname: valid\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmds := make(map[string]CustomCommand)
	loadCommandsFromDir(cmds, dir)

	if _, ok := cmds["valid"]; !ok {
		t.Error("expected 'valid' command to be loaded")
	}
	if len(cmds) != 1 {
		t.Errorf("expected 1 command, got %d", len(cmds))
	}
}
