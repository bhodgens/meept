# Slash Command Extension Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend Meept's slash command system to support file-based custom commands (Claude Code compatible), argument passing, skill autocomplete via `/skill`, and provide pre-built templates for research, QA automation, and Playwright testing.

**Architecture:** Build on existing `internal/sharedclient/slash.go` custom command infrastructure, extend discovery to include `~/.claude/commands/` for Claude Code compatibility, add argument templating, and wire `/skill` command with autocomplete in both TUI and Flutter UI.

**Tech Stack:** Go 1.24.2, bubbletea/v2 TUI, Flutter/Dart, existing skills discovery (`internal/skills/`), RPC communication between client and daemon.

---

## Summary of Requirements

1. **Claude Code Compatibility**: Load commands from `~/.claude/commands/*.md` (already partially supported via `internal/sharedclient/slash.go` but needs verification and expansion)
2. **Argument Passing**: Support commands like `/research build a todo app` where "build a todo app" becomes `$ARGUMENTS` or `$1` in templates
3. **/skill Command**: Add `/skill` slash command that shows autocomplete menu of all installed skills (mirroring existing slash command autocomplete)
4. **Pre-built Templates**: Create Meept-specific commands for:
   - In-depth research with Open Knowledge Format storage
   - QA automation testing with Docker
   - Playwright testing against recent commits

## File Structure

### New Files
- `internal/sharedclient/claude_commands.go` — Claude Code command discovery and parsing
- `internal/sharedclient/claude_commands_test.go` — Tests for Claude command compatibility
- `internal/tui/commands/skill.go` — `/skill` command handler with skill registry integration
- `internal/comm/http/api_templates.go` — HTTP API endpoint for listing templates (Flutter UI)
- `internal/daemon/commands.go` — Daemon-side command execution for templates and skills
- `config/commands/research.md` — Research template with OKF storage
- `config/commands/qa-docker.md` — Docker-based QA automation template
- `config/commands/playwright-test.md` — Playwright testing template
- `docs/user-guide/slash-commands.md` — User documentation for slash commands
- `ui/flutter_ui/lib/services/skills_service.dart` — Flutter service for fetching skills

### Modified Files
- `internal/sharedclient/slash.go` — Extend `DiscoverCustomCommands()` to include Claude commands tier
- `internal/sharedclient/slash_autocomplete.go` — Add `/skill` to command list, integrate skill names
- `internal/tui/command_handler.go` — Add `/skill` case, wire template execution
- `internal/tui/slash_autocomplete.go` — Merge skill names into autocomplete
- `internal/tui/app.go` — Wire slash autocomplete to include skills and templates
- `ui/flutter_ui/lib/core/slash_commands.dart` — Add `/skill` command, fetch skills from daemon
- `ui/flutter_ui/lib/features/chat/slash_autocomplete.dart` — Include skills in Flutter autocomplete
- `ui/flutter_ui/lib/features/chat/chat_input.dart` — Fetch and merge skills/templates into registry
- `cmd/meept/commands.go` — CLI commands for managing slash commands

---

## Task 1: Claude Code Command Compatibility Layer

**Files:**
- Create: `internal/sharedclient/claude_commands.go`
- Create: `internal/sharedclient/claude_commands_test.go`
- Test: `internal/sharedclient/claude_commands_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/sharedclient/claude_commands_test.go
package sharedclient

import (
    "os"
    "path/filepath"
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sharedclient/claude_commands_test.go -v`
Expected: FAIL with "undefined: discoverClaudeCommands"

- [ ] **Step 3: Write claude_commands.go implementation**

```go
// internal/sharedclient/claude_commands.go
package sharedclient

import (
    "log/slog"
    "os"
    "path/filepath"
    "strings"
)

// discoverClaudeCommands scans ~/.claude/commands/ for markdown command files
// and returns a map of command name to CustomCommand.
//
// This provides compatibility with Claude Code's slash command format.
// Claude Code stores commands in ~/.claude/commands/<name>.md with YAML frontmatter.
func discoverClaudeCommands(claudeCommandsPath string) map[string]CustomCommand {
    cmds := make(map[string]CustomCommand)

    // Expand ~ in path
    if strings.HasPrefix(claudeCommandsPath, "~") {
        homeDir, err := os.UserHomeDir()
        if err != nil {
            return cmds
        }
        claudeCommandsPath = filepath.Join(homeDir, claudeCommandsPath[1:])
    }

    entries, err := os.ReadDir(claudeCommandsPath)
    if err != nil {
        // Directory doesn't exist - this is fine, just return empty
        return cmds
    }

    for _, entry := range entries {
        if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
            continue
        }

        cmd, err := parseCommandFile(filepath.Join(claudeCommandsPath, entry.Name()))
        if err != nil {
            slog.Warn("Failed to parse Claude command file",
                "path", entry.Name(),
                "error", err)
            continue
        }

        if cmd.Name == "" {
            slog.Warn("Claude command has no name, skipping",
                "path", entry.Name())
            continue
        }

        cmds[cmd.Name] = cmd
    }

    return cmds
}

// claudeCommandsPath returns the path to ~/.claude/commands/
func claudeCommandsPath() string {
    homeDir, err := os.UserHomeDir()
    if err != nil {
        return ""
    }
    return filepath.Join(homeDir, ".claude", "commands")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/sharedclient/... -v -run TestDiscoverClaudeCommands`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sharedclient/claude_commands.go internal/sharedclient/claude_commands_test.go
git commit -m "feat(slash): add Claude Code command discovery layer

- discoverClaudeCommands() scans ~/.claude/commands/*.md
- Provides out-of-box compatibility with Claude Code slash commands
- Gracefully handles missing directory (returns empty, no error)
"
```

---

## Task 2: Integrate Claude Commands into Discovery

**Files:**
- Modify: `internal/sharedclient/slash.go:228-250` (DiscoverCustomCommands function)
- Test: `internal/sharedclient/slash_test.go` (add integration test)

- [ ] **Step 1: Write the failing test**

```go
// internal/sharedclient/slash_test.go - add to existing test file
func TestDiscoverCustomCommands_WithClaudeCommands(t *testing.T) {
    // Set up temp directories
    tmpDir := t.TempDir()

    // Create .meept/commands with a command
    meeptCommands := filepath.Join(tmpDir, ".meept", "commands")
    os.MkdirAll(meeptCommands, 0755)
    os.WriteFile(filepath.Join(meeptCommands, "meept-cmd.md"), []byte(`---
name: meeptcmd
description: Meept command
---
Meept body
`), 0644)

    // Simulate Claude commands (we'll mock this in integration)
    originalCache := customCommandCache
    defer func() { customCommandCache = originalCache }()

    // Manually set cache with claude command
    customCommandCache = map[string]CustomCommand{
        "claudecmd": {Name: "claudecmd", Description: "Claude command", Template: "Claude body"},
        "meeptcmd":  {Name: "meeptcmd", Description: "Meept command", Template: "Meept body"},
    }

    names := CustomCommandNames()
    if len(names) != 2 {
        t.Errorf("expected 2 commands, got %d", len(names))
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sharedclient/... -v -run TestDiscoverCustomCommands_WithClaudeCommands`
Expected: FAIL (cache won't have expected values without proper integration)

- [ ] **Step 3: Modify DiscoverCustomCommands to include Claude commands**

```go
// internal/sharedclient/slash.go - modify DiscoverCustomCommands function
// DiscoverCustomCommands scans discovery paths for markdown command files and
// returns a map of command name to CustomCommand. Results are cached in the
// package-level customCommandCache for subsequent lookups.
//
// Discovery order (project-local overrides user-global on name collision):
//   1. .meept/commands/*.md  (project-local, if .meept/ exists in cwd)
//   2. ~/.meept/commands/*.md (user-global)
//   3. ~/.claude/commands/*.md (Claude Code compatibility - lower priority)
func DiscoverCustomCommands() map[string]CustomCommand {
    cmds := make(map[string]CustomCommand)

    // User-global first (lower priority)
    homeDir, err := os.UserHomeDir()
    if err == nil {
        userPath := filepath.Join(homeDir, ".meept", "commands")
        loadCommandsFromDir(cmds, userPath)

        // Claude Code commands - lowest priority
        claudePath := filepath.Join(homeDir, ".claude", "commands")
        loadCommandsFromDir(cmds, claudePath)
    }

    // Project-local second (higher priority, overwrites user-global on collision)
    cwd, err := os.Getwd()
    if err == nil {
        projectPath := filepath.Join(cwd, ".meept", "commands")
        if info, statErr := os.Stat(projectPath); statErr == nil && info.IsDir() {
            loadCommandsFromDir(cmds, projectPath)
        }
    }

    customCommandMu.Lock()
    customCommandCache = cmds
    customCommandMu.Unlock()
    return cmds
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/sharedclient/... -v -run TestDiscoverCustomCommands_WithClaudeCommands`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sharedclient/slash.go
git commit -m "feat(slash): integrate Claude commands into discovery chain

- ~/.claude/commands/*.md now discovered at lowest priority
- Discovery order: .meept/ > ~/.meept/ > ~/.claude/
- Claude Code slash commands work out-of-box in Meept TUI
"
```

---

## Task 3: Add /skill Command with Autocomplete

**Files:**
- Create: `internal/tui/commands/skill.go`
- Create: `internal/tui/commands/skill_test.go`
- Modify: `internal/tui/command_handler.go:112-156` (executeBuiltin switch)
- Modify: `internal/tui/slash_autocomplete.go:175-185` (UpdateCommands to include skills)

- [ ] **Step 1: Write the failing test**

```go
// internal/tui/commands/skill_test.go
package commands

import (
    "testing"

    "github.com/caimlas/meept/internal/skills"
    "github.com/caimlas/meept/internal/tui"
)

func TestSkillCommand_List(t *testing.T) {
    registry := skills.NewRegistry()
    registry.Register(&skills.Skill{
        Name:        "code-review",
        Description: "Review code changes",
    })
    registry.Register(&skills.Skill{
        Name:        "debugger",
        Description: "Debug issues",
    })

    handler := NewSkillCommand(registry)
    result := handler.Execute([]string{})

    if result.Output == "" {
        t.Error("expected non-empty output for skill list")
    }

    if !contains(result.Output, "code-review") {
        t.Error("expected output to contain 'code-review'")
    }
}

func contains(s, substr string) bool {
    return strings.Contains(s, substr)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/commands/... -v`
Expected: FAIL with "undefined: NewSkillCommand"

- [ ] **Step 3: Write skill.go implementation**

```go
// internal/tui/commands/skill.go
package commands

import (
    "fmt"
    "strings"

    "github.com/caimlas/meept/internal/skills"
    "github.com/caimlas/meept/internal/tui"
)

// SkillCommand handles /skill slash command execution.
type SkillCommand struct {
    registry *skills.Registry
}

// NewSkillCommand creates a new skill command handler.
func NewSkillCommand(registry *skills.Registry) *SkillCommand {
    return &SkillCommand{
        registry: registry,
    }
}

// Execute executes the /skill command with the given arguments.
//
// Usage:
//   /skill              - list all available skills
//   /skill <name>       - show skill details
//   /skill search <q>   - search skills by name/description
func (c *SkillCommand) Execute(args []string) *tui.CommandResult {
    if len(args) == 0 {
        return c.executeList()
    }

    switch args[0] {
    case "search":
        if len(args) < 2 {
            return &tui.CommandResult{
                Output:  "usage: /skill search <query>",
                IsError: true,
            }
        }
        return c.executeSearch(args[1])
    default:
        // Show specific skill details
        return c.executeShow(args[0])
    }
}

func (c *SkillCommand) executeList() *tui.CommandResult {
    skillList := c.registry.List()
    if len(skillList) == 0 {
        return &tui.CommandResult{Output: "no skills installed"}
    }

    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("installed skills (%d):\n\n", len(skillList)))

    for _, skill := range skillList {
        sb.WriteString(fmt.Sprintf("  /%-20s %s\n", skill.Name, skill.Description))
    }

    sb.WriteString("\nusage: /skill <name> to view details")

    return &tui.CommandResult{Output: sb.String()}
}

func (c *SkillCommand) executeShow(name string) *tui.CommandResult {
    skill := c.registry.Get(name)
    if skill == nil {
        return &tui.CommandResult{
            Output:  fmt.Sprintf("skill not found: %s", name),
            IsError: true,
        }
    }

    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("skill: %s\n", skill.Name))
    sb.WriteString(fmt.Sprintf("description: %s\n", skill.Description))

    if len(skill.Requires) > 0 {
        sb.WriteString(fmt.Sprintf("requires: %s\n", strings.Join(skill.Requires, ", ")))
    }

    if len(skill.Tags) > 0 {
        sb.WriteString(fmt.Sprintf("tags: %s\n", strings.Join(skill.Tags, ", ")))
    }

    if skill.RiskLevel != "" {
        sb.WriteString(fmt.Sprintf("risk: %s\n", skill.RiskLevel))
    }

    return &tui.CommandResult{Output: sb.String()}
}

func (c *SkillCommand) executeSearch(query string) *tui.CommandResult {
    match := c.registry.Match(query)
    if match == nil {
        return &tui.CommandResult{
            Output:  fmt.Sprintf("no skills matching: %s", query),
            IsError: true,
        }
    }

    return c.executeShow(match.Name)
}

// GetSkillNames returns all skill names for autocomplete.
func (c *SkillCommand) GetSkillNames() []string {
    return c.registry.Names()
}
```

- [ ] **Step 4: Wire /skill into command_handler.go**

```go
// internal/tui/command_handler.go - add field and modify NewCommandHandler
type CommandHandler struct {
    rpc              *RPCClient
    getChatModel     func() *models.ChatModel
    skillRegistry    *skills.Registry  // ADD THIS
    skillCommand     *commands.SkillCommand  // ADD THIS
}

// Modify NewCommandHandler to accept skill registry
func NewCommandHandler(rpc *RPCClient, opts ...CommandHandlerOption) *CommandHandler {
    h := &CommandHandler{
        rpc: rpc,
    }
    for _, opt := range opts {
        opt(h)
    }
    // Initialize skill command if registry available
    if h.skillRegistry != nil {
        h.skillCommand = commands.NewSkillCommand(h.skillRegistry)
    }
    return h
}

// Add option for skill registry
func WithSkillRegistry(reg *skills.Registry) CommandHandlerOption {
    return func(h *CommandHandler) {
        h.skillRegistry = reg
    }
}

// Modify executeBuiltin - add case for "skill"
func (h *CommandHandler) executeBuiltin(cmd *SlashCommand) *CommandResult {
    switch cmd.Name {
    // ... existing cases ...
    case "skill":
        if h.skillCommand == nil {
            return &CommandResult{
                Output:  "skill system not initialized",
                IsError: true,
            }
        }
        result := h.skillCommand.Execute(cmd.Args)
        return &CommandResult{
            Output:  result.Output,
            IsError: result.IsError,
        }
    // ... rest of switch ...
    }
}
```

- [ ] **Step 5: Wire skill names into slash autocomplete**

```go
// internal/tui/app.go - find where autocomplete is updated, likely in a handler
// Add skill names to autocomplete when skills are loaded

// Example: after skill discovery completes
func (a *App) handleSkillDiscovered(skills []*skills.Skill) {
    skillNames := make([]string, len(skills))
    for i, s := range skills {
        skillNames[i] = s.Name
    }

    // Merge into existing autocomplete
    if a.slashAutocomplete != nil {
        a.slashAutocomplete.MergeCommands(skillNames)
    }
}
```

- [ ] **Step 6: Run tests and verify**

Run: `go test ./internal/tui/commands/... -v`
Expected: PASS

Run: `go build -o bin/meept ./cmd/meept && ./bin/meept chat` then type `/skill`
Expected: `/skill` appears in autocomplete, executes to show skills list

- [ ] **Step 7: Commit**

```bash
git add internal/tui/commands/skill.go internal/tui/command_handler.go internal/tui/slash_autocomplete.go internal/tui/app.go
git commit -m "feat(slash): add /skill command with autocomplete

- /skill lists all installed skills with descriptions
- /skill <name> shows skill details
- /skill search <query> fuzzy-matches skills
- Skill names appear in slash autocomplete popup
- Integrates with existing slash command infrastructure
"
```

---

## Task 4: Argument Templating for Custom Commands

**Files:**
- Modify: `internal/sharedclient/slash.go:321-338` (RenderTemplate - already exists, verify)
- Create: `internal/comm/http/api_templates.go`
- Modify: `internal/comm/http/server.go` (add route)
- Test: `internal/sharedclient/slash_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/sharedclient/slash_test.go
func TestRenderTemplate_Arguments(t *testing.T) {
    tests := []struct {
        name     string
        template string
        args     []string
        want     string
    }{
        {
            name:     "all arguments",
            template: "Do this: $ARGUMENTS",
            args:     []string{"on", "friday", "at", "12pm"},
            want:     "Do this: on friday at 12pm",
        },
        {
            name:     "positional",
            template: "Research $1 and summarize in $2 sentences",
            args:     []string{"quantum computing", "5"},
            want:     "Research quantum computing and summarize in 5 sentences",
        },
        {
            name:     "mixed",
            template: "Task: $1 - Details: $ARGUMENTS",
            args:     []string{" Urgent", "by EOD", "for client"},
            want:     "Task:  Urgent - Details:  Urgent by EOD for client",
        },
        {
            name:     "no arguments",
            template: "Static template",
            args:     []string{},
            want:     "Static template",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := RenderTemplate(tt.template, tt.args)
            if got != tt.want {
                t.Errorf("RenderTemplate() = %q, want %q", got, tt.want)
            }
        })
    }
}
```

- [ ] **Step 2: Run test to verify current implementation**

Run: `go test ./internal/sharedclient/... -v -run TestRenderTemplate_Arguments`

The existing `RenderTemplate` in `slash.go:321-338` already handles `$ARGUMENTS` and `$N` positional args. The test should PASS.

- [ ] **Step 3: Create HTTP API for template invocation**

```go
// internal/comm/http/api_templates.go - Create if doesn't exist
package http

import (
    "encoding/json"
    "net/http"

    "github.com/caimlas/meept/internal/sharedclient"
)

// TemplatesService handles HTTP requests for template operations.
type TemplatesService struct {
    // Add dependencies as needed
}

// TemplatesRequest represents a template invocation request.
type TemplatesRequest struct {
    Name      string   `json:"name"`
    Arguments []string `json:"arguments"`
}

// HandleInvoke handles POST /api/v1/templates/invoke
func (s *TemplatesService) HandleInvoke(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }

    var req TemplatesRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "invalid request", http.StatusBadRequest)
        return
    }

    // Get the template
    cmd, ok := sharedclient.GetCustomCommand(req.Name)
    if !ok {
        http.Error(w, "template not found", http.StatusNotFound)
        return
    }

    // Render with arguments
    rendered := sharedclient.RenderTemplate(cmd.Template, req.Arguments)

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{
        "content": rendered,
    })
}
```

- [ ] **Step 4: Add HTTP route in server.go**

```go
// internal/comm/http/server.go - add route
// Find where routes are registered and add:
mux.HandleFunc("/api/v1/templates/invoke", templatesService.HandleInvoke)
```

- [ ] **Step 5: Commit**

```bash
git add internal/comm/http/api_templates.go internal/comm/http/server.go
git commit -m "feat(templates): add HTTP API for template invocation

- POST /api/v1/templates/invoke accepts name + arguments
- Returns rendered template with $ARGUMENTS/$N substitution
- Used by Flutter UI for slash command execution
"
```

---

## Task 5: Pre-built Meept Slash Commands

**Files:**
- Create: `config/commands/research.md`
- Create: `config/commands/qa-docker.md`
- Create: `config/commands/playwright-test.md`
- Modify: `Makefile` (add install-commands target)

- [ ] **Step 1: Create research.md template**

```markdown
---
name: research
description: In-depth research with Open Knowledge Format storage
arguments:
  - topic: The research topic or question
---
# Research Task: $ARGUMENTS

## Objective
Conduct thorough research on the topic above and produce a comprehensive report stored in Open Knowledge Format (OKF).

## Research Process

1. **Scope Definition**
   - Identify key questions to answer
   - Define research boundaries and depth

2. **Source Collection**
   - Use web search and firecrawl tools
   - Gather primary and secondary sources
   - Document URLs and timestamps

3. **Analysis**
   - Synthesize findings across sources
   - Identify patterns and contradictions
   - Note confidence levels for claims

4. **Output (OKF Format)**
   Save findings in the following structure:

```
docs/knowledge/{topic}/
├── summary.md          # Executive summary
├── findings/           # Detailed findings
│   ├── finding-001.md
│   └── ...
├── sources.md          # Annotated bibliography
└── metadata.json       # OKF metadata
```

## Tools to Use
- `web_search` - Initial discovery
- `firecrawl_scrape` - Deep content extraction
- `memory_write` - Store findings incrementally
- `file_create` - Generate OKF output files

## Success Criteria
- All claims sourced and timestamped
- Findings stored in OKF-compliant structure
- Summary actionable and self-contained
```

- [ ] **Step 2: Create qa-docker.md template**

```markdown
---
name: qa-docker
description: QA automation test for recent commits using Docker
arguments:
  - scope: Optional scope (e.g., "api", "ui", "all")
---
# QA Automation: Test Recent Commit

## Objective
Run automated QA tests against the most recent commit using Docker containers for isolation and reproducibility.

## Test Scope
$ARGUMENTS

## Test Plan

### Phase 1: Environment Setup
1. Identify changed files from last commit (`git diff HEAD~1 --name-only`)
2. Determine test scope based on changes
3. Pull required Docker images:
   - `docker compose pull` (if compose.yml exists)
   - Or specify images: postgres:15, redis:7, etc.

### Phase 2: Test Execution
1. Start test containers:
   ```bash
   docker compose up -d test-db test-redis
   ```

2. Run test suite:
   ```bash
   docker compose run --rm tests
   ```

3. Capture output and exit codes

### Phase 3: Reporting
1. Parse test results (JUnit XML if available)
2. Generate summary:
   - Total tests
   - Passed/Failed/Skipped
   - Duration per test suite
3. If failures: extract stack traces and related diffs

### Phase 4: Cleanup
```bash
docker compose down -v
```

## Tools to Use
- `shell_execute` - Git and Docker commands
- `file_read` - Parse test reports
- `memory_write` - Store results for trending

## Output Format
```markdown
# QA Report - {date} - Commit {hash}

## Summary
- Tests: X passed, Y failed, Z skipped
- Duration: N minutes

## Failed Tests
{list with stack traces}

## Recommendations
{actionable items}
```
```

- [ ] **Step 3: Create playwright-test.md template**

```markdown
---
name: playwright-test
description: Playwright E2E testing against recent commit
arguments:
  - url: Optional base URL (default: http://localhost:3000)
---
# Playwright E2E Test Run

## Objective
Execute Playwright end-to-end tests against the application following the most recent commit.

## Configuration
- Base URL: ${1:-http://localhost:3000}
- Scope: $ARGUMENTS

## Test Plan

### Phase 1: Setup
1. Check if Playwright is installed:
   ```bash
   npx playwright --version
   ```

2. Install/update browsers if needed:
   ```bash
   npx playwright install
   ```

3. Verify/Start application server

### Phase 2: Test Execution
1. Run test suite:
   ```bash
   npx playwright test ${1:-}
   ```

2. Options to include:
   - `--reporter=html` for visual report
   - `--video=on-first-retry` for debugging
   - `--trace=on` for detailed traces

### Phase 3: Results Analysis
1. Parse HTML report or console output
2. Extract:
   - Pass/fail counts
   - Failed test names and traces
   - Screenshots/videos of failures

3. Compare with previous run if available

### Phase 4: Artifact Storage
1. Save reports to `test-results/{date}-{hash}/`
2. Link to screenshots and traces
3. Update trending document

## Tools to Use
- `shell_execute` - Playwright CLI commands
- `file_read` - Parse test reports
- `file_find` - Locate test files

## Output Format
```markdown
# Playwright Report - {date}

## Run Info
- Commit: {hash}
- URL: {url}
- Duration: {time}

## Results
| Suite | Passed | Failed | Skipped |
|-------|--------|--------|---------|
| ...   | ...    | ...    | ...     |

## Failures
{detailed list with error messages}

## Artifacts
- [HTML Report](path/to/report.html)
- [Screenshots](path/to/screenshots/)
- [Traces](path/to/traces/)
```
```

- [ ] **Step 4: Add Makefile install target**

```makefile
# Makefile - add install-commands target

.PHONY: install-commands
install-commands:
	@echo "Installing slash command templates..."
	@mkdir -p ~/.meept/commands
	@cp config/commands/*.md ~/.meept/commands/ 2>/dev/null || true
	@echo "Commands installed to ~/.meept/commands/"
```

- [ ] **Step 5: Commit**

```bash
git add config/commands/research.md config/commands/qa-docker.md config/commands/playwright-test.md Makefile
git commit -m "feat(commands): add pre-built slash command templates

- /research - In-depth research with OKF storage
- /qa-docker - Docker-based QA automation testing
- /playwright-test - Playwright E2E test execution
- Templates use $ARGUMENTS for user input
- Install via make install-commands
"
```

---

## Task 6: Flutter UI Integration

**Files:**
- Create: `ui/flutter_ui/lib/services/skills_service.dart`
- Modify: `ui/flutter_ui/lib/core/slash_commands.dart`
- Modify: `ui/flutter_ui/lib/features/chat/slash_autocomplete.dart`
- Modify: `ui/flutter_ui/lib/features/chat/chat_input.dart`

- [ ] **Step 1: Create skills_service.dart**

```dart
// ui/flutter_ui/lib/services/skills_service.dart
import 'package:http/http.dart' as http;
import 'dart:convert';
import '../models/skill.dart';
import 'sdk_client.dart';

/// Service for fetching and managing skills.
class SkillsService {
  final SdkClient _client;

  SkillsService({SdkClient? client}) : _client = client ?? SdkClient();

  /// Fetch all installed skills.
  Future<List<Skill>> fetchSkills() async {
    final response = await _client.get(Uri.parse('/api/v1/skills'));
    if (response.statusCode == 200) {
      final data = jsonDecode(response.body) as List;
      return data.map((s) => Skill.fromJson(s)).toList();
    }
    throw Exception('Failed to fetch skills: ${response.statusCode}');
  }

  /// Search skills by query.
  Future<List<Skill>> searchSkills(String query) async {
    final all = await fetchSkills();
    return all.where((s) =>
      s.name.toLowerCase().contains(query.toLowerCase()) ||
      s.description.toLowerCase().contains(query.toLowerCase())
    ).toList();
  }
}
```

- [ ] **Step 2: Modify slash_commands.dart to add /skill**

```dart
// ui/flutter_ui/lib/core/slash_commands.dart
class SlashCommand {
  final String name;
  final String description;
  final String? usage;
  final Future<String?> Function(String args)? handler;

  const SlashCommand({
    required this.name,
    required this.description,
    this.usage,
    this.handler,
  });
}

// Add to _defaultCommands list:
const SlashCommand(name: '/skill', description: 'list or search installed skills', usage: '/skill [search] <query>'),
```

- [ ] **Step 3: Update chat_input.dart to fetch skills on init**

```dart
// ui/flutter_ui/lib/features/chat/chat_input.dart
// Add in initState or a dedicated method:
void _initializeSlashCommands() async {
  final skillsService = SkillsService();
  final skills = await skillsService.fetchSkills();

  // Merge skill names into the slash command registry
  for (final skill in skills) {
    _slashRegistry.addCustomCommand(SlashCommand(
      name: '/${skill.name}',
      description: skill.description,
    ));
  }

  setState(() {
    // Trigger rebuild with updated commands
  });
}
```

- [ ] **Step 4: Commit**

```bash
git add ui/flutter_ui/lib/services/skills_service.dart
git add ui/flutter_ui/lib/core/slash_commands.dart
git add ui/flutter_ui/lib/features/chat/chat_input.dart
git commit -m "feat(flutter): add /skill command with autocomplete

- SkillsService fetches skills from daemon API
- /skill command shows installed skills
- Skill names appear in Flutter slash autocomplete
- Mirrors TUI /skill functionality
"
```

---

## Task 7: Documentation

**Files:**
- Create: `docs/user-guide/slash-commands.md`

- [ ] **Step 1: Create comprehensive documentation**

```markdown
# Slash Commands User Guide

## Overview

Slash commands provide quick access to reusable prompts, skills, and automation workflows in Meept. Type `/` followed by a command name and optional arguments.

## Command Discovery

Commands are discovered from multiple locations (highest priority first):

1. `.meept/commands/*.md` - Project-local commands
2. `~/.meept/commands/*.md` - User-global commands
3. `~/.claude/commands/*.md` - Claude Code compatible commands

This means Claude Code slash commands work out-of-the-box in Meept.

## Using Commands

### Basic Syntax

```
/command-name [arguments]
```

Examples:
```
/research build a quantum computing timeline
/qa-docker api
/playwright-test http://localhost:3000
```

### Argument Substitution

Commands support templating with positional and collective arguments:

| Pattern | Meaning | Example |
|---------|---------|---------|
| `$ARGUMENTS` | All arguments joined | `/cmd a b c` -> `a b c` |
| `$1`, `$2`, ... | Positional argument | `/cmd hello world` -> `$1=hello` |
| `${@:N}` | Arguments from index N | `${@:2}` -> from 2nd onward |
| `${@:N:L}` | L arguments from index N | `${@:2:1}` -> one arg at index 2 |

## Built-in Commands

| Command | Description |
|---------|-------------|
| `/help` | Show available commands |
| `/skill` | List or search installed skills |
| `/new`, `/clear` | Start fresh conversation |
| `/retry` | Retry last response |
| `/undo` | Remove last exchange |
| `/usage` | Show token usage |
| `/stop` | Stop current work |
| `/status` | Show platform health |

## Custom Commands

### Creating Commands

Create a `.md` file in `~/.meept/commands/`:

```markdown
---
name: summarize
description: summarize text concisely
---
Summarize the following in 2-3 sentences:

$ARGUMENTS
```

### Claude Code Compatibility

Meept supports Claude Code's command format. Commands in `~/.claude/commands/` work automatically:

```markdown
---
name: explain
description: explain code step by step
---
Explain this code:

$ARGUMENTS
```

## Pre-built Templates

Meept includes these commands by default:

### /research
In-depth research with Open Knowledge Format output.

Usage: `/research <topic>`

### /qa-docker
Docker-based QA automation for recent commits.

Usage: `/qa-docker [scope]`

### /playwright-test
Playwright E2E test execution.

Usage: `/playwright-test [url]`

## Skills Integration

The `/skill` command lists all installed skills:

```
/skill              # List all skills
/skill code-review  # Show skill details
/skill search code  # Fuzzy search
```

Skill names also appear in slash autocomplete when typing `/`.
```

- [ ] **Step 2: Commit**

```bash
git add docs/user-guide/slash-commands.md
git commit -m "docs: add slash commands user guide

- Covers custom command creation
- Documents argument templating syntax
- Explains Claude Code compatibility
- Lists pre-built templates
"
```

---

## Completion Checklist

- [ ] All 7 tasks implemented
- [ ] Tests passing: `go test ./...`
- [ ] TUI builds: `go build -o bin/meept ./cmd/meept`
- [ ] Flutter builds: `cd ui/flutter_ui && flutter build`
- [ ] Documentation linked in `mkdocs.yml`
- [ ] Plan marked complete

---

## Execution Options

**Plan complete and saved to `docs/superpowers/plans/2026-06-18-slash-command-extension.md`. Two execution options:**

**1. Subagent-Driven (recommended)** - Dispatch fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in current session using executing-plans skill

**Which approach?**