# Plan: Integrate CLAUDE.md and .claude Artifacts into Meept

## Overview

Update Meept's agent system to be aware of and utilize CLAUDE.md and .claude artifacts including:
- **CLAUDE.md**: Project documentation, architecture, conventions
- **.claude/skills/**: 10+ skill directories with detailed documentation
- **.claude/mind.mv2**: Claude's persistent memory (52MB binary format)

## Phase 1: Artifact Analysis & Indexing

### 1.1 Document CLAUDE.md Structure
- [ ] Parse CLAUDE.md sections into structured knowledge
- [ ] Extract: Build commands, architecture, agents, config, conventions
- [ ] Create summary for agent context injection

### 1.2 Catalog .claude/skills/
- [ ] List all available skills:
  - agent-development
  - agent-memory-systems
  - agents-langchain
  - docx
  - frontend-design
  - mermaid-diagrams
  - playwright-skill
  - react-best-practices
  - senior-architect
  - senior-devops
  - senior-frontend
  - senior-ml-engineer
- [ ] Extract SKILL.md frontmatter from each
- [ ] Build skill registry (name, description, version)

### 1.3 Assess mind.mv2 Accessibility
- [ ] Determine mind.mv2 format (binary memvid format)
- [ ] Evaluate if Meept can read/query it directly
- [ ] Alternative: Export or index relevant portions
- [ ] Note: Currently 52MB, likely too large for direct context injection

## Phase 2: Capability Enhancement for Meept

### 2.1 Add Claude Artifacts to Agent Context
- [ ] Modify `internal/agent/prompt/builder.go` to inject CLAUDE.md context
- [ ] Add `.claude/` directory to context search paths
- [ ] Create summary of CLAUDE.md for baseline agent prompts

### 2.2 Skill Discovery Integration
- [ ] Extend `internal/skills/discovery.go` to include `.claude/skills/`
- [ ] Parse SKILL.md frontmatter from Claude skills
- [ ] Add Claude skills to skill registry alongside project skills
- [ ] Priority order: `.meept/skills/` → `~/.meept/skills/` → `~/.config/meept/skills/` → `.claude/skills/` → `~/.meept/clawskills/`

### 2.3 Memory Integration Options
- [ ] Option A: Add `.claude/mind.mv2` as memory source in `internal/memory/manager.go`
- [ ] Option B: Create memory import utility for key learnings
- [ ] Option C: Reference skills from .claude/skills/ as task memory
- [ ] Evaluate and choose best approach

### 2.4 Documentation Sync Check
- [ ] Implement validation: CLAUDE.md vs diagram.md vs implementation
- [ ] Add check in `make test` or CI
- [ ] Warn on mismatches between docs and code

## Phase 3: Integration Implementation

### 3.1 Modify Agent Prompt Builder
**File**: `internal/agent/prompt/builder.go`

```go
// Add CLAUDE.md context injection
func (b *PromptBuilder) AddClaudeContext() error {
    claudeMD, err := os.ReadFile("CLAUDE.md")
    if err != nil {
        return err
    }
    b.sections = append(b.sections, PromptSection{
        Name:    "project-context",
        Content: summarizeClaudeMD(claudeMD),
    })
    return nil
}
```

### 3.2 Extend Skills Discovery
**File**: `internal/skills/discovery.go`

```go
// Add .claude/skills/ to search paths
var SkillSearchPaths = []string{
    ".meept/skills/",
    "~/.meept/skills/",
    "~/.config/meept/skills/",
    ".claude/skills/",  // NEW
    "~/.meept/clawskills/",
}

// Parse SKILL.md frontmatter
func parseSkillSkillMD(path string) (*Skill, error) {
    // Extract YAML frontmatter from Claude skills
}
```

### 3.3 Add Tool for Claude Artifacts
**File**: `internal/tools/builtin/claude_artifacts.go`

```go
// New tool: claude_artifacts_search
// Search CLAUDE.md and .claude/skills/ for relevant information
```

### 3.4 Update CLAUDE.md
Add section about Claude artifacts integration:
- Where artifacts are located
- How Meept uses them
- Skills discovery priority

## Phase 4: Testing & Validation

### 4.1 Unit Tests
- [ ] Test CLAUDE.md parsing and summarization
- [ ] Test .claude/skills/ frontmatter parsing
- [ ] Test skill registry with Claude skills included
- [ ] Test agent context injection with Claude artifacts

### 4.2 Integration Tests
- [ ] Verify agents can reference CLAUDE.md content
- [ ] Verify skills from .claude/skills/ are discoverable
- [ ] Verify memory system can access Claude knowledge
- [ ] Test documentation sync checks

### 4.3 Manual Verification
- [ ] Start daemon and run: `./bin/meept clawskills list`
- [ ] Confirm Claude skills appear in list
- [ ] Ask agent about project conventions (from CLAUDE.md)
- [ ] Verify agent can reference agent-development skill

## Phase 5: Documentation

### 5.1 Update CLAUDE.md
- [ ] Add section: "Claude Artifacts Integration"
- [ ] Document how artifacts enhance agent capabilities
- [ ] Explain skills discovery priority order

### 5.2 Update diagram.md
- [ ] Add Claude artifacts to architecture diagram
- [ ] Show .claude/ directory relationship to agent system

### 5.3 Create README in .claude/
- [ ] Explain artifact structure
- [ ] Document memvid format if applicable
- [ ] Provide guide for contributing skills

## Success Criteria

1. ✅ Meept agents automatically receive CLAUDE.md context
2. ✅ `.claude/skills/` skills are discoverable via `clawskills list`
3. ✅ Agent can reference agent-development skill when creating agents
4. ✅ Documentation sync check detects mismatches
5. ✅ Tests pass with new integration

## Estimated Effort

| Phase | Tasks | Est. Time |
|-------|-------|-----------|
| Phase 1 | Analysis & Indexing | 2-3 hours |
| Phase 2 | Capability Design | 3-4 hours |
| Phase 3 | Implementation | 6-8 hours |
| Phase 4 | Testing | 3-4 hours |
| Phase 5 | Documentation | 1-2 hours |
| **Total** | | **15-21 hours** |

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| mind.mv2 is binary/unreadable | Medium | Skip direct reading, use .claude/skills/ instead |
| Claude skills incompatible format | Low | Create adapter for frontmatter parsing |
| Context overflow from too much info | Medium | Summarize CLAUDE.md, don't inject full content |
| Documentation sync too strict | Low | Make warnings, not blocking errors |
