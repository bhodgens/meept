# Cluster H: Skill System Evolution

## Goal
Remove clawskills, add OMO-style MCP-embedded skill support, and enable native use of Claude skills.

## Background
Meept has two disconnected skill systems:
1. `internal/skills/` — Local SKILL.md files with capability-based model binding
2. `internal/clawskills/` — Third-party registry client (not wired to skills discovery)

User wants:
- Remove `clawskills`
- Support OMO-style skills that embed MCP servers
- Support native Claude skills (AGENTS.md, .claude/skills/)
- Archive unused/old skill packages

## Feature Checklist

### 1. Remove ClawSkills
- Delete `internal/clawskills/` package
- Remove `meept clawskills` CLI commands
- Remove references from docs and help text
- Move any valuable logic to `internal/skills/` if applicable

### 2. OMO-Style MCP-Embedded Skills
- OMO skills carry their own MCP servers (spin up on demand, scoped, go away when done)
- Meept skills are just system prompts
- Design:
  - Add `mcp_servers` field to SKILL.md frontmatter:
    ```yaml
    mcp_servers:
      - name: web-search
        command: npx
        args: ["mcp-server-exa"]
    ```
  - When skill is activated, spin up MCP servers
  - Add server tools to agent's tool surface for this task only
  - Shutdown servers when skill execution completes

### 3. Support for Claude Skills
- Claude Code skills live in `~/.claude/skills/`
- Format: `SKILL.md` with YAML frontmatter (same as ours!)
- Add `~/.claude/skills/` as an additional discovery tier
- Parse Claude skill format (nearly identical)
- Support Claude's `allowedTools` field name variant

### 4. Unified Skill Loader
- Single discovery mechanism that handles:
  - Meept native skills
  - Claude skills (with format adaptation)
  - Future skill formats (extensible parser)
- Priority shadowing across all sources

## Implementation Plan

### Phase 1: Archive ClawSkills
1. Delete `internal/clawskills/` directory
2. Remove `clawskills` commands from CLI
3. Update docs (remove clawskills references)
4. Move registry search/install concepts to future roadmap if desired

### Phase 2: MCP-Embedded Skills
1. Add `MCPServer` config to `SkillMetadata` schema
2. Create `internal/skills/mcp_runtime.go`:
   - Spawn MCP server processes on skill activation
   - Collect tools from server via `tools/list`
   - Add to agent's available tools
   - Terminate on skill completion
3. Wire into skill executor: before Execute -> spin up MCPs; after Execute -> shutdown

### Phase 3: Claude Skills Discovery
1. Add `~/.claude/skills/` as 4th discovery tier in `DefaultTiers()`
2. Expand parser to handle Claude field names:
   - `allowedTools` -> `allowed-tools`
   - `temperature` already matches
3. Add format adaptation layer: `ClaudeSkillAdapter`
4. Test loading a real Claude skill

### Phase 4: Unified Loader
1. Refactor `Discovery` to use pluggable `SkillSource` interface
2. Sources: `MeeptFileSource`, `ClaudeFileSource`, `MCPEmbedSource`
3. Single index across all sources with priority shadowing
4. API: `registry.Get("name")` returns unified `Skill` regardless of source

## Files to Modify / Create
- **DELETE** `internal/clawskills/` entire package
- `cmd/meept/clawskills.go` — DELETE
- `internal/skills/models.go` — Add `MCPServers` field
- `internal/skills/mcp_runtime.go` (new) — MCP lifecycle for skills
- `internal/skills/discovery.go` — Add Claude tier, unified loader
- `internal/skills/adapter.go` (new) — Format adaptation layer

## Success Criteria
- [x] `clawskills` package fully removed; build succeeds
- [x] Skill with embedded MCP server spins up/down correctly
- [x] Claude skill from disk loads and executes in Meept
- [x] Unified skill index shadows correctly across all sources
