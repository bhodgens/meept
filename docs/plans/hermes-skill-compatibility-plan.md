# Hermes Skill Compatibility Plan

**Created**: 2026-06-12
**Status**: Pending (execute after skill evolution system complete)
**Inspired by**: Hermes-Agent (Nous Research) - https://github.com/nousresearch/hermes-agent

---

## Executive Summary

This plan adds compatibility for **Hermes-Agent skills** to Meept, allowing users to:
1. Discover and load skills from `~/.hermes/skills/` automatically
2. Parse Hermes SKILL.md format (agentskills.io standard)
3. Execute Hermes skills that use compatible tools
4. Translate Hermes-specific tool calls to Meept equivalents

**Scope**: Read-only compatibility initially (load and execute Hermes skills). Write compatibility (creating Hermes-format skills) is out of scope.

---

## Background: Hermes Skill Format

Hermes uses the **agentskills.io** open standard with this structure:

```
~/.hermes/skills/
├── skill-name/
│   ├── SKILL.md           # Main instructions (YAML frontmatter + markdown)
│   ├── references/        # Supporting documentation
│   ├── templates/         # Output templates
│   ├── scripts/           # Executable scripts
│   └── assets/            # Supplementary files
```

**SKILL.md frontmatter format**:
```yaml
---
name: skill-name
description: Brief description
version: 1.0.0
license: MIT
platforms: [macos, linux]     # Optional OS restrictions
prerequisites:                # Optional runtime requirements
  env_vars: [API_KEY]
  commands: [node, npm]
metadata:
  hermes:
    config:                   # Config.yaml integration
      - key: wiki.path
        description: Path to wiki
        default: "~/wiki"
---

# Skill instructions

[Full procedural knowledge here]
```

---

## Implementation Phases

### Phase 1: Parser Compatibility

**Goal**: Parse Hermes SKILL.md format without modification.

**Hermes-specific frontmatter fields** to support:
```yaml
version: string          # Meept doesn't track skill versions
license: string          # Metadata only
platforms: [string]      # Similar to Meept, but different values
prerequisites:           # NEW: runtime requirements
  env_vars: [string]
  commands: [string]
  python_packages: [string]
metadata:
  hermes:
    config: [...]        # Config integration (similar to Meept)
    triggers: [string]   # Trigger keywords (Meept uses tags)
    toolsets: [string]   # Tool groupings (Meept doesn't have)
```

**Implementation**:
```go
// internal/skills/parser.go

type HermesSkillMetadata struct {
    // Embedded base Meept metadata
    SkillMetadata

    // Hermes-specific fields
    Version       string                  `yaml:"version"`
    License       string                  `yaml:"license"`
    Platforms     []string                `yaml:"platforms"`
    Prerequisites HermesPrerequisites     `yaml:"prerequisites"`
    Metadata      *HermesMetadataExtended `yaml:"metadata"`
}

type HermesPrerequisites struct {
    EnvVars       []string `yaml:"env_vars"`
    Commands      []string `yaml:"commands"`
    PythonPackages []string `yaml:"python_packages"`
}

type HermesMetadataExtended struct {
    Hermes *HermesExtended `yaml:"hermes"`
}

type HermesExtended struct {
    Config    []ConfigVar `yaml:"config"`
    Triggers  []string    `yaml:"triggers"`
    Toolsets  []string    `yaml:"toolsets"`
}

// ParseSkillText already handles YAML frontmatter generically.
// Add alternative field mapping for Hermes-specific names.
```

**Changes to `ParseSkillText`**:
- Map `platforms` → `tags` (for discovery filtering)
- Map `metadata.hermes.triggers` → `tags`
- Store `prerequisites` in metadata for validation at execution time

---

### Phase 2: Discovery Integration

**Goal**: Automatically discover Hermes skills from `~/.hermes/skills/`.

**Implementation**:

**Option A: Add to external_dirs config** (simplest)

User adds to `~/.meept/meept.json5`:
```json5
{
  skills: {
    external_dirs: [
      "~/.hermes/skills",
    ],
  },
}
```

**Pros**: No code changes needed
**Cons**: Manual setup, doesn't auto-detect Hermes install

**Option B: Auto-discovery in DefaultTiers** (recommended)

```go
// internal/skills/discovery.go

func DefaultTiers() []DiscoveryTier {
    homeDir, _ := os.UserHomeDir()

    tiers := []DiscoveryTier{
        {Path: ".meept/skills", Priority: PriorityProject},
        {Path: filepath.Join(homeDir, ".meept", "skills"), Priority: PriorityUser},
        {Path: filepath.Join(homeDir, ".config", "meept", "skills"), Priority: PrioritySystem},
    }

    // Auto-add Hermes skills if directory exists
    hermesSkills := filepath.Join(homeDir, ".hermes", "skills")
    if _, err := os.Stat(hermesSkills); err == nil {
        tiers = append(tiers, DiscoveryTier{
            Path:     hermesSkills,
            Priority: PriorityHermes, // Lower priority than Meept-native
        })
    }

    return tiers
}
```

**Changes to `Priority` constants**:
```go
const (
    PriorityProject = 0
    PriorityUser    = 1
    PriorityClaude  = 2
    PriorityHermes  = 3  // NEW: After Claude skills
    PrioritySystem  = 4
)
```

---

### Phase 3: Prerequisites Validation

**Goal**: Check Hermes skill prerequisites before execution.

**Implementation**:

```go
// internal/skills/executor.go

type PrerequisiteChecker interface {
    CheckEnvVars(vars []string) error
    CheckCommands(cmds []string) error
    CheckPythonPackages(pkgs []string) error
}

type DefaultPrerequisiteChecker struct{}

func (c *DefaultPrerequisiteChecker) CheckEnvVars(vars []string) error {
    for _, v := range vars {
        if os.Getenv(v) == "" {
            return fmt.Errorf("missing required env var: %s", v)
        }
    }
    return nil
}

func (c *DefaultPrerequisiteChecker) CheckCommands(cmds []string) error {
    for _, cmd := range cmds {
        if _, err := exec.LookPath(cmd); err != nil {
            return fmt.Errorf("missing required command: %s", cmd)
        }
    }
    return nil
}

// Add to Skill struct:
type Skill struct {
    // ... existing fields
    Prerequisites *HermesPrerequisites `json:"prerequisites,omitempty"`
}

// In Execute():
if skill.Prerequisites != nil {
    if err := CheckPrerequisites(skill.Prerequisites); err != nil {
        return nil, &ExecutorError{
            SkillName: skill.Name,
            Message:   "prerequisites not met",
            Cause:     err,
        }
    }
}
```

---

### Phase 4: Tool Mapping

**Goal**: Map Hermes tool references to Meept equivalents.

**Hermes tools** (from tools/ directory analysis):
| Hermes Tool | Meept Equivalent | Notes |
|-------------|------------------|-------|
| `shell` | `shell` | Direct match |
| `file_read` | `file_read` | Direct match |
| `file_write` | `file_write` | Direct match |
| `file_edit` | `file_edit` | Direct match |
| `web_search` | `web_search` | Direct match |
| `git` | `git` | Direct match |
| `schedule` | `schedule_create` | Name differs |
| `memory_store` | `memory_store` | Direct match |
| `memory_search` | `memory_search` | Direct match |
| `skill_view` | `skills.get` | Name differs |
| `skills_list` | `skills.list` | Name differs |
| `task_create` | `task_create` | Direct match |
| `team_create` | N/A | Meept may not have teams |
| `image_gen` | N/A | Meept may not have image gen |

**Implementation**:

```go
// internal/skills/executor.go

// HermesToolMapper translates Hermes tool names to Meept equivalents
type HermesToolMapper struct {
    mapping map[string]string
}

func NewHermesToolMapper() *HermesToolMapper {
    return &HermesToolMapper{
        mapping: map[string]string{
            // Hermes → Meept
            "schedule":      "schedule_create",
            "skill_view":    "skills.get",
            "skills_list":   "skills.list",
            // Add more as needed
        },
    }
}

func (m *HermesToolMapper) Translate(toolName string) string {
    if mep, ok := m.mapping[toolName]; ok {
        return mep
    }
    return toolName // Identity if no mapping
}

// In skill execution context, intercept tool calls:
func (e *Executor) ExecuteWithMapping(ctx, skill, input, mappedTools) {
    // Replace tool references in skill.Body
    translatedBody := e.mapper.TranslateToolReferences(skill.Body)
}
```

---

### Phase 5: Config Integration

**Goal**: Support Hermes-style config.yaml integration.

Hermes skills declare config vars in frontmatter:
```yaml
metadata:
  hermes:
    config:
      - key: wiki.path
        description: Path to wiki
        default: "~/wiki"
        prompt: Wiki directory path
```

**Implementation** (already partially done in Meept):

Meept already has similar functionality in `skill_utils.go`:
- `extract_skill_config_vars()`
- `resolve_skill_config_values()`
- `inject_skill_config()`

**Changes needed**:
1. Ensure Meept parser extracts `metadata.hermes.config`
2. Verify config resolution works for dotted keys (`wiki.path`)
3. Store resolved config in skill execution context

---

### Phase 6: Testing

**Test cases**:

1. **Discovery test**: Skills in `~/.hermes/skills/` are discovered
2. **Parser test**: Hermes frontmatter fields are parsed
3. **Prerequisites test**: Missing env vars/commands fail gracefully
4. **Tool mapping test**: Hermes tool names are translated
5. **Config test**: `metadata.hermes.config` values are resolved

**Test data**:
```bash
# Clone some Hermes skills for testing
git clone https://github.com/nousresearch/hermes-agent /tmp/hermes-test
cp -r /tmp/hermes-test/skills ~/.hermes/skills/
```

---

## Configuration Schema Additions

Add to `internal/config/schema.go`:

```go
type SkillsConfig struct {
    ExternalDirs        []string `json:"external_dirs,omitempty"`
    AutoDiscoverHermes  bool     `json:"auto_discover_hermes"`  // Default: true
    HermesSkillsDir     string   `json:"hermes_skills_dir"`     // Default: ~/.hermes/skills
    ValidatePrerequisites bool   `json:"validate_prerequisites"`  // Default: true
    toolMapping         map[string]string // Internal
}
```

---

## File Changes Summary

| File | Change Type | Description |
|------|-------------|-------------|
| `internal/skills/models.go` | Extend | Add `Prerequisites` field to `Skill` |
| `internal/skills/parser.go` | Extend | Parse Hermes-specific frontmatter |
| `internal/skills/discovery.go` | Extend | Auto-discover `~/.hermes/skills/` |
| `internal/skills/executor.go` | Extend | Add prerequisite checker, tool mapper |
| `internal/config/schema.go` | Extend | Add Hermes compatibility config |
| `internal/skills/hermes_compat.go` | NEW | New file for Hermes compatibility layer |
| `tests/integration/hermes_skills_test.go` | NEW | Integration tests |

---

## Dependency Analysis

### Hermes Skills That Will Work Out-of-Box

Skills that only use:
- ✅ Shell commands
- ✅ File operations (read, write, edit)
- ✅ Web search
- ✅ Git operations
- ✅ Memory operations

### Hermes Skills That Need Mapping

Skills that use:
- ⚠️ `schedule` → map to `schedule_create`
- ⚠️ `skill_view` → map to `skills.get`
- ⚠️ `team_*` tools → may not have equivalents

### Hermes Skills That Won't Work

Skills that require:
- ❌ MCP servers (if not configured)
- ❌ Custom Hermes-only tools
- ❌ Platform-specific commands (e.g., `brew` on Linux)

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Frontmatter parsing conflicts | Medium | Test with real Hermes skills first |
| Tool name mismatches cause failures | Medium | Log warnings, fall back to identity |
| Prerequisites check too strict | Low | Make validation optional via config |
| Shadowing Meept-native skills | Low | Priority tiers (Meept > Hermes) |

---

## Success Criteria

1. User with existing Hermes skills can run Meept and immediately use them
2. No code changes needed to existing Hermes SKILL.md files
3. Prerequisites are validated before execution (optional)
4. Tool calls are translated transparently
5. Config integration works identically to native Meept skills

---

## Timeline Estimate

| Phase | Effort | Dependencies |
|-------|--------|--------------|
| Phase 1: Parser | 2-4 hours | None |
| Phase 2: Discovery | 1-2 hours | Phase 1 |
| Phase 3: Prerequisites | 2-3 hours | Phase 1 |
| Phase 4: Tool Mapping | 4-6 hours | Phase 1 |
| Phase 5: Config | 1-2 hours | Phase 1 |
| Phase 6: Testing | 2-4 hours | All phases |
| **Total** | **12-21 hours** | ~2-3 days |

---

## Appendix: Sample Hermes Skill

```markdown
---
name: research-Deep-dive
description: Perform deep research on a topic
version: 1.0.0
license: MIT
platforms: [macos, linux]
prerequisites:
  env_vars: [BRAVE_API_KEY]
  commands: [curl, jq]
metadata:
  hermes:
    config:
      - key: research.notes_dir
        description: Directory for research notes
        default: "~/research"
    triggers: [research, investigate, analyze]
    toolsets: [research_tools]
---

# Research Deep Dive Skill

## Overview
This skill performs comprehensive research on a given topic.

## Prerequisites
- BRAVE_API_KEY environment variable
- curl and jq installed

## Usage
Invoke with a research question or topic.

## Process
1. Search using Brave Search API
2. Fetch and summarize relevant pages
3. Store findings in notes_dir
4. Generate synthesis report
```

---

## Notes

**Decision**: Read-only compatibility first. Creating new Hermes-format skills is out of scope.

**Key insight**: Hermes and Meept skill formats are 80% compatible already. The main differences are:
1. Additional Hermes frontmatter fields (version, license, prerequisites)
2. Different tool naming conventions
3. Config integration structure

The implementation should focus on **parsing** Hermes fields without breaking Meept-native skills.
