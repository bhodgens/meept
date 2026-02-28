# Plan: CLAUDE.md and .claude Artifact Integration (Generalized)

## Executive Summary

This plan outlines a generalized integration of CLAUDE.md and .claude directory artifacts into Meept's capabilities. When Meept starts in any directory containing these artifacts, it should automatically detect and utilize them in a manner consistent with Claude Code (claude.ai/code), providing project-specific context, skills, and agent configurations that enhance task execution and decision-making.

## Core Philosophy

**Meept should behave consistently with Claude Code's artifact usage patterns:**
- Detect artifacts in the current working directory on startup
- Use CLAUDE.md for project-specific guidance, commands, and architecture
- Load skills from .claude/skills/ as available capabilities
- Respect the artifact structure and conventions established by Claude Code
- Work seamlessly in any repository that uses Claude Code artifacts
- Maintain Claude Code's intended behavior and artifact semantics

## Goals

1. **Universal Detection**: Automatically detect CLAUDE.md and .claude/ artifacts in any working directory
2. **Claude-Consistent Behavior**: Use artifacts in the same manner Claude Code does
3. **Context Awareness**: Inject project-specific context from CLAUDE.md when available
4. **Skill Availability**: Make .claude/skills/ available as tools when present
5. **Zero Configuration**: Work automatically without per-repository setup
6. **Graceful Degradation**: Function normally when artifacts are absent
7. **Artifact Compatibility**: Maintain compatibility with Claude Code's artifact format and usage

## How Claude Code Uses These Artifacts

### CLAUDE.md

Claude Code uses CLAUDE.md to understand:
- **Project Structure**: How code is organized
- **Build Commands**: How to build, test, and run the project
- **Architecture Patterns**: Key components and their relationships
- **Development Workflow**: Preferred ways of working with the codebase
- **Conventions**: Coding standards, naming patterns, UI guidelines
- **Agent Definitions**: Available specialist agents and their purposes
- **Tool Requirements**: Which tools are needed for different tasks

### .claude/ Directory

Claude Code uses the .claude/ directory for:
- **Skills**: Reusable capabilities (.claude/skills/)
- **Brain Memory**: Persistent context (.claude/.mind.mv2)
- **Sessions**: Session tracking (.claude/mind-session-*.json)
- **Agents**: Agent definitions (.claude/agents/ if present)
- **Customizations**: User and project-specific configurations

### Skills (.claude/skills/*)

Claude Code discovers and uses skills by:
- Scanning .claude/skills/ for SKILL.md files
- Parsing YAML frontmatter for metadata
- Loading skill content when triggered by context
- Making skill capabilities available to agents
- Following skill requirements and dependencies

## Implementation Plan

### Phase 1: Universal Artifact Detection (Priority: CRITICAL)

#### 1.1 Startup-Time Artifact Scanner

**Location**: `internal/context/artifact_scanner.go`

**Responsibilities**:
- Scan working directory on daemon startup
- Detect presence of CLAUDE.md
- Detect presence of .claude/ directory
- Scan .claude/skills/ for SKILL.md files
- Scan .claude/agents/ for agent definitions (if present)
- Maintain artifact registry per working directory

**Key Functions**:
```go
type ArtifactScanner struct {
    workingDir  string
    artifacts   *Artifacts
    watchers    []fsnotify.Watcher
}

type Artifacts struct {
    WorkingDir  string
    CLAUDEMD    *CLAUDEDocument
    ClaudeDir   *ClaudeDirectory
    Available   bool
    LastScanned time.Time
}

type ClaudeDirectory struct {
    Path        string
    Skills      []*Skill
    Agents      []*AgentDefinition
    MindFile    string
    SessionFile string
}

// Scan the current working directory for Claude artifacts
func (as *ArtifactScanner) Scan() (*Artifacts, error)

// Watch for changes to artifacts
func (as *ArtifactScanner) Watch() error

// Get artifacts for a specific directory
func GetArtifactsForDirectory(dir string) (*Artifacts, error)
```

**Behavior**:
- Runs automatically on daemon startup
- Can be triggered manually (refresh command)
- Watches for file changes using fsnotify
- Caches results to avoid repeated scanning
- Handles missing artifacts gracefully

#### 1.2 Directory-Aware Artifact Manager

**Location**: `internal/context/artifact_manager.go`

**Responsibilities**:
- Manage artifacts for multiple directories
- Handle workspace switching
- Provide artifact lookups by directory
- Invalidate cache on file changes

**Key Functions**:
```go
type ArtifactManager struct {
    mu         sync.RWMutex
    artifacts  map[string]*Artifacts  // workingDir -> artifacts
    scanners   map[string]*ArtifactScanner
}

func NewArtifactManager() *ArtifactManager
func (am *ArtifactManager) ScanDirectory(dir string) (*Artifacts, error)
func (am *ArtifactManager) GetArtifacts(dir string) (*Artifacts, error)
func (am *ArtifactManager) Invalidate(dir string)
```

**Use Cases**:
- Different CLI sessions in different directories
- Workspace management
- Multi-directory task execution
- File change notifications

### Phase 2: CLAUDE.md Parser (Priority: HIGH)

#### 2.1 Claude-Consistent Parser

**Location**: `internal/context/claude_parser.go`

**Responsibilities**:
- Parse CLAUDE.md following Claude Code's conventions
- Extract all sections that Claude Code uses
- Maintain semantic compatibility
- Handle various CLAUDE.md formats

**Key Functions**:
```go
type CLAUDEDocument struct {
    Path              string
    RawContent        string
    WorkingDir        string

    // Sections following Claude Code conventions
    BuildCommands     []BuildCommand
    Architecture      *ArchitectureSection
    Components        []ComponentMapping
    Agents            []AgentDefinition
    SecurityLayers    []SecurityLayer
    Configuration     []ConfigReference
    SkillsDiscovery   *SkillsDiscoverySection
    MultiAgentSystem  *MultiAgentSection
    Conventions       *CodeConventions
    ProjectStructure  *ProjectTree
    Documentation     *DocumentationMaintenance

    // Metadata
    LastModified      time.Time
}

func ParseCLAUDEMD(path string) (*CLAUDEDocument, error)
func (cd *CLAUDEDocument) GetCommandsForContext(ctx string) []BuildCommand
func (cd *CLAUDEDocument) GetAgentForTask(task string) *AgentDefinition
```

**Parsing Strategy**:
- Recognize standard sections (Build Commands, Architecture Overview, etc.)
- Handle free-form sections gracefully
- Extract code blocks with language hints
- Parse markdown structures (headers, lists, code blocks)
- Preserve original structure for reference

#### 2.2 Command and Action Extraction

**Location**: `internal/context/command_extractor.go`

**Responsibilities**:
- Extract build and development commands from CLAUDE.md
- Categorize commands by purpose
- Match commands to task contexts
- Provide intelligent command suggestions

**Key Functions**:
```go
type BuildCommand struct {
    Description   string
    Command       string
    Category      string  // build, test, run, deploy, etc.
    Context       []string // When this command is relevant
    Requires      []string // Tools or setup needed
}

type CommandExtractor struct {
    document *CLAUDEDocument
}

func (ce *CommandExtractor) ExtractCommands() []BuildCommand
func (ce *CommandExtractor) SuggestCommand(task string) *BuildCommand
func (ce *CommandExtractor) GetCommandsByCategory(category string) []BuildCommand
```

**Claude-Consistent Behavior**:
- Recognize command patterns from CLAUDE.md
- Suggest commands based on user intent (e.g., "build this" → suggest build commands)
- Support command execution with context
- Respect command categories and relationships

#### 2.3 Architecture Context Provider

**Location**: `internal/context/architecture_provider.go`

**Responsibilities**:
- Extract architecture information from CLAUDE.md
- Provide component relationships
- Enable architecture-aware planning
- Support diagram references

**Key Functions**:
```go
type ArchitectureSection struct {
    RequestFlow      []string
    KeyComponents    []ComponentMapping
    SecurityLayers   []SecurityLayer
    DataFlow         []DataFlowStep
}

type ComponentMapping struct {
    Layer      string
    Packages   []string
}

func (ap *ArchitectureProvider) GetComponentForPath(path string) *ComponentMapping
func (ap *ArchitectureProvider) GetRelatedComponents(component string) []string
func (ap *ArchitectureProvider) ValidateChange(affectedPaths []string) []string
```

### Phase 3: Skills Integration (Priority: HIGH)

#### 3.1 Skills Scanner

**Location**: `internal/context/skills_scanner.go`

**Responsibilities**:
- Scan .claude/skills/ directory
- Parse SKILL.md files with YAML frontmatter
- Extract skill metadata and capabilities
- Maintain skill registry

**Key Functions**:
```go
type Skill struct {
    Slug         string
    Path         string
    Name         string
    Description  string
    Version      string
    Requires     []string  // Capabilities required
    Content      string
    Category     string
    Triggers     []string  // When to trigger this skill
}

type SkillsDirectory struct {
    Path      string
    Skills    []*Skill
    ByCategory map[string][]*Skill
    BySlug     map[string]*Skill
}

func ScanSkillsDirectory(dir string) (*SkillsDirectory, error)
func (sd *SkillsDirectory) FindSkillForTask(task string) *Skill
func (sd *SkillsDirectory) GetSkillsByRequirement(requirement string) []*Skill
```

**Claude-Consistent Behavior**:
- Scan recursively through .claude/skills/
- Parse YAML frontmatter exactly as Claude Code does
- Respect skill triggers and requirements
- Load skills on-demand based on context

#### 3.2 Skill Tool Adapter

**Location**: `internal/tools/skill_adapter.go`

**Responsibilities**:
- Adapt Claude skills to Meept's tool interface
- Execute skills with proper context
- Handle skill requirements
- Provide skill output formatting

**Key Functions**:
```go
type SkillTool struct {
    skill    *Skill
    executor SkillExecutor
}

func (st *SkillTool) Name() string
func (st *SkillTool) Description() string
func (st *SkillTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)

type SkillExecutor interface {
    ExecuteSkill(skill *Skill, context map[string]interface{}) (string, error)
}

// Register skills as tools in the tool registry
func RegisterSkillsAsTools(skills []*Skill, registry *ToolRegistry) error
```

**Execution Model**:
- Skills are invoked when their triggers match
- Skill content is used as a prompt template
- Required capabilities are validated before execution
- Output is formatted consistently with tool responses

### Phase 4: Context Injection (Priority: HIGH)

#### 4.1 Claude-Consistent Context Builder

**Location**: `internal/agent/context_builder.go`

**Responsibilities**:
- Build context from artifacts for agent prompts
- Inject relevant sections based on task type
- Maintain Claude Code's context patterns
- Handle context size limits

**Key Functions**:
```go
type ContextBuilder struct {
    artifacts *Artifacts
}

type BuildContext struct {
    TaskType       string
    TaskDescription string
    RelevantSections []string
    Commands       []BuildCommand
    Architecture   *ArchitectureSection
    Skills         []*Skill
    Agents         []AgentDefinition
}

func (cb *ContextBuilder) BuildForTask(task string) *BuildContext
func (cb *ContextBuilder) FormatForPrompt(context *BuildContext) string
func (cb *ContextBuilder) ShouldInjectContext(task string) bool
```

**Context Injection Rules**:
- **Build/Test tasks**: Inject build commands and test commands
- **Code generation**: Inject architecture and components
- **Architecture questions**: Inject architecture section and diagrams
- **Agent selection**: Inject agent definitions and capabilities
- **Skill usage**: Inject relevant skill definitions
- **General tasks**: Inject project overview and conventions

#### 4.2 Agent Prompt Enhancement

**Location**: `internal/agent/prompt_enhancer.go`

**Responsibilities**:
- Enhance agent system prompts with artifact context
- Add CLAUDE.md sections when relevant
- Include available skills in tool descriptions
- Maintain natural language flow

**Key Functions**:
```go
func EnhancePrompt(basePrompt string, context *BuildContext, workingDir string) string

// Example enhancement:
/*
You are working in a project with the following context:

## Project Overview
[From CLAUDE.md - first section]

## Available Build Commands
- go build -o bin/meept ./cmd/meept
- go test ./... -v

## Architecture
[From CLAUDE.md - architecture section]

## Available Skills
- mermaid-diagrams: Generate diagrams
- docx: Manipulate Word documents

[Original system prompt continues...]
*/
```

### Phase 5: Memvid Integration (Priority: MEDIUM - Exploratory)

#### 5.1 Memvid Reader

**Location**: `internal/memory/memvid_reader.go`

**Responsibilities**:
- Read Claude's brain memvid files (.claude/.mind.mv2)
- Extract relevant context from memory
- Query by relevance and recency
- Maintain read-only safety

**Key Functions**:
```go
type MemvidReader struct {
    memvidPath string
    // Internal memvid parsing structures
}

func NewMemvidReader(path string) (*MemvidReader, error)
func (mr *MemvidReader) Query(query string, limit int) ([]MemoryEntry, error)
func (mr *MemvidReader) GetRecent(count int) ([]MemoryEntry, error)
func (mr *MemvidReader) IsAvailable() bool
```

**Research Phase**:
- Investigate memvid file format
- Determine read feasibility
- Assess query capabilities
- Evaluate privacy implications

**Integration Approach**:
- Phase 1: Read-only exploration and format documentation
- Phase 2: Context retrieval for relevant queries
- Phase 3: Optional memory consolidation to Meept's memory

#### 5.2 Session Context Integration

**Location**: `internal/context/session_integration.go`

**Responsibilities**:
- Read Claude session files (.claude/mind-session-*.json)
- Track current session context
- Maintain session continuity

**Key Functions**:
```go
type ClaudeSession struct {
    SessionID  string
    Source     string
    StartTime  int64
}

func ReadSessionFile(path string) (*ClaudeSession, error)
func (si *SessionIntegration) GetCurrentSession() *ClaudeSession
```

### Phase 6: Agent Awareness (Priority: MEDIUM)

#### 6.1 Agent Definition Parser

**Location**: `internal/agent/claude_agent_parser.go`

**Responsibilities**:
- Parse agent definitions from CLAUDE.md
- Parse .claude/agents/*.md if present
- Map to Meept's agent system
- Enable cross-agent delegation

**Key Functions**:
```go
type ClaudeAgentDefinition struct {
    ID           string
    Name         string
    Role         string
    Purpose      string
    Capabilities []string
    Model        string
    Color        string
}

func ParseAgentDefinitions(doc *CLAUDEDocument) []ClaudeAgentDefinition
func ParseAgentsDirectory(dir string) []ClaudeAgentDefinition
func MatchTaskToAgent(task string, agents []ClaudeAgentDefinition) *ClaudeAgentDefinition
```

**Integration with Meept**:
- Map Claude agents to Meept's agent system
- Use agent definitions for task routing
- Support agent collaboration patterns
- Respect agent roles and capabilities

#### 6.2 Coworker Awareness

**Location**: `internal/agent/coworker_awareness.go`

**Responsibilities**:
- Discover available coworkers from CLAUDE.md
- Enable agent delegation
- Support multi-agent workflows

**Key Functions**:
```go
type CoworkerRegistry struct {
    agents []ClaudeAgentDefinition
}

func (cr *CoworkerRegistry) GetAgents() []ClaudeAgentDefinition
func (cr *CoworkerRegistry) FindAgentForTask(task string) *ClaudeAgentDefinition
func (cr *CoworkerRegistry) DelegateTask(agentID string, task string) (string, error)
```

## Behavior Specifications

### Startup Behavior

```
1. Daemon starts
2. Scan working directory for CLAUDE.md
3. Scan for .claude/ directory
4. Parse CLAUDE.md if found
5. Scan .claude/skills/ if found
6. Register skills as tools
7. Load memvid if available
8. Ready to serve requests with enhanced context
```

### Request Processing Behavior

```
User Request
    ↓
Determine working directory
    ↓
Check for artifacts in working directory
    ↓
If artifacts exist:
    - Parse task type
    - Build relevant context from artifacts
    - Inject context into agent prompt
    - Consider skill usage
    - Consider agent delegation
    ↓
Execute task with enhanced understanding
```

### Directory Switching Behavior

```
1. CLI session changes directory
2. Invalidate cached artifacts for old directory
3. Scan new directory for artifacts
4. Update context for new directory
5. Continue operation with new context
```

## Configuration

### Default Behavior

```toml
[claude_artifacts]
# Enable automatic artifact detection and usage
enabled = true

# Scan working directory on startup
auto_scan = true

# Watch for file changes and reload
watch_changes = true

# Inject context from CLAUDE.md into prompts
inject_context = true

# Load skills from .claude/skills/
load_skills = true

# Register skills as available tools
register_skill_tools = true

# Memvid integration (read-only)
memvid_enabled = true
memvid_readonly = true  # Always read-only for safety

# Session tracking
track_sessions = true

# Cache parsed artifacts (performance)
cache_artifacts = true
cache_ttl = 300  # seconds
```

### Per-Directory Override

Users can create `.claude/meept.toml` for directory-specific configuration:

```toml
[claude_artifacts]
# Disable for this directory if needed
enabled = false

# Or customize behavior
inject_context = true
load_skills = ["mermaid-diagrams", "docx"]  # Only specific skills
```

## Error Handling

### Graceful Degradation

```
Scenario 1: CLAUDE.md not found
→ Continue normally without project context
→ Log informational message

Scenario 2: .claude/ not found
→ Continue without skills
→ Log informational message

Scenario 3: Malformed CLAUDE.md
→ Continue with partial parsing
→ Log warning with details
→ Notify user of parsing issues

Scenario 4: Skill execution fails
→ Continue with other tools
→ Log error
→ Provide user feedback

Scenario 5: Memvid read fails
→ Continue without memvid context
→ Log warning
```

### User Feedback

Informative messages when artifacts are loaded:
```
[INFO] Detected CLAUDE.md in /path/to/project
[INFO] Loaded 11 skills from .claude/skills/
[INFO] Injecting project context into agent prompts
```

Warnings when issues occur:
```
[WARN] CLAUDE.md parse error at line 42: Invalid markdown
[WARN] Skill 'broken-skill' has invalid frontmatter, skipping
```

## Testing Strategy

### Unit Tests

```go
// Artifact Scanner
func TestArtifactScanner_ScanDirectory(t *testing.T)
func TestArtifactScanner_DetectCLAUDEMD(t *testing.T)
func TestArtifactScanner_DetectClaudeDirectory(t *testing.T)

// CLAUDE.md Parser
func TestParseCLAUDEMD_ValidDocument(t *testing.T)
func TestParseCLAUDEMD_ExtractCommands(t *testing.T)
func TestParseCLAUDEMD_ExtractArchitecture(t *testing.T)

// Skills Scanner
func TestScanSkillsDirectory_ValidSkills(t *testing.T)
func TestScanSkillsDirectory_ParseFrontmatter(t *testing.T)

// Context Builder
func TestContextBuilder_BuildForTask(t *testing.T)
func TestContextBuilder_FormatForPrompt(t *testing.T)
```

### Integration Tests

```go
// End-to-end artifact detection and usage
func TestArtifactDiscovery_FullWorkflow(t *testing.T)
func TestContextInjection_InAgentPrompt(t *testing.T)
func TestSkillExecution_AsTool(t *testing.T)
func TestMultiDirectory_SwitchingContext(t *testing.T)
```

### Manual Testing Scenarios

**Scenario 1: Fresh Project with CLAUDE.md**
```bash
cd /tmp/test-project
echo "# CLAUDE.md" > CLAUDE.md
# Run Meept
# Verify artifacts detected
# Verify context injected
```

**Scenario 2: Project with Skills**
```bash
cd project-with-skills
mkdir -p .claude/skills/my-skill
echo "---\nname: my-skill\n---\n" > .claude/skills/my-skill/SKILL.md
# Run Meept
# Verify skill available as tool
# Test skill execution
```

**Scenario 3: Project without Artifacts**
```bash
cd plain-project
# Run Meept
# Verify normal operation
# Verify no errors logged
```

**Scenario 4: Directory Switching**
```bash
cd project-with-claude
./bin/meept status  # Should detect artifacts
cd ../plain-project
./bin/meept status  # Should detect no artifacts
```

## Performance Considerations

### Caching Strategy

```go
type ArtifactCache struct {
    entries map[string]*CacheEntry
    ttl     time.Duration
}

type CacheEntry struct {
    artifacts  *Artifacts
    lastAccess time.Time
    size       int64
}
```

- Cache parsed artifacts by directory
- TTL-based invalidation (default 5 minutes)
- Manual invalidation on file changes
- Size limits to prevent memory bloat

### Lazy Loading

- Parse CLAUDE.md only when needed
- Load skill content on first use
- Read memivd entries on-demand
- Defer expensive operations

### Async Scanning

```go
func (as *ArtifactScanner) ScanAsync(dir string, callback func(*Artifacts, error)) {
    go func() {
        artifacts, err := as.Scan(dir)
        callback(artifacts, err)
    }()
}
```

- Non-blocking artifact scanning
- Continue operation while scanning
- Update context when ready

## Security Considerations

### File Access

- Only read artifacts from working directory
- Validate file paths (prevent directory traversal)
- Respect file permissions
- Limit file size (prevent DOS)

### Memvid Privacy

- Read-only access by default
- User opt-in for any write operations
- No automatic memory export
- Clear documentation of what's accessed

### Skill Execution

- Validate skill content before execution
- Sandbox skill execution
- Limit skill permissions
- Audit skill usage

### Context Injection

- Sanitize context before injection
- Prevent prompt injection via artifacts
- Limit context size
- Mask sensitive information

## Documentation

### User Documentation

**Location**: `docs/claude-artifacts.md`

Content:
- How Meept uses CLAUDE.md and .claude/
- What artifacts are detected
- How context is injected
- How to customize behavior
- Troubleshooting common issues

### Developer Documentation

**Location**: `internal/context/README.md`

Content:
- Architecture of artifact system
- How to extend parsers
- How to add new skill types
- Testing guidelines

### CLAUDE.md Template

**Location**: `config/CLAUDE.md.template`

Content:
- Template CLAUDE.md for new projects
- Documentation of recognized sections
- Examples of good CLAUDE.md

## Migration Guide

For projects migrating from Claude Code to Meept:

1. **Existing Artifacts**: Work automatically, no changes needed
2. **Custom Skills**: Ensure SKILL.md has proper frontmatter
3. **Agent Definitions**: Compatible with Meept's agent system
4. **Memvid**: Read-only access by default
5. **Configuration**: Optional .claude/meept.toml for customization

## Success Criteria

### Functional Requirements

✅ Detects CLAUDE.md in any working directory
✅ Detects .claude/ directory in any working directory
✅ Parses CLAUDE.md following Claude Code conventions
✅ Scans .claude/skills/ and registers skills as tools
✅ Injects relevant context into agent prompts
✅ Suggests commands from CLAUDE.md based on context
✅ Handles directory switching correctly
✅ Works with or without artifacts present
✅ Compatible with Claude Code artifact format

### Non-Functional Requirements

✅ Startup time impact < 100ms when artifacts present
✅ Context injection adds < 50ms to prompt generation
✅ Memory overhead < 50MB for cached artifacts
✅ No performance degradation when artifacts absent
✅ Graceful handling of malformed artifacts
✅ Clear user feedback and logging

### Quality Requirements

✅ Unit test coverage > 80% for new code
✅ Integration tests cover all major workflows
✅ Documentation complete and accurate
✅ Error handling robust and user-friendly
✅ Backward compatible with existing behavior

## Implementation Timeline

### Sprint 1 (Week 1-2)
- [ ] Create `internal/context` package structure
- [ ] Implement artifact scanner
- [ ] Implement directory-aware artifact manager
- [ ] Write unit tests for scanning

### Sprint 2 (Week 3-4)
- [ ] Implement CLAUDE.md parser
- [ ] Implement command extractor
- [ ] Implement architecture provider
- [ ] Write unit tests for parsing

### Sprint 3 (Week 5-6)
- [ ] Implement skills scanner
- [ ] Implement skill tool adapter
- [ ] Implement context builder
- [ ] Write unit tests for skills

### Sprint 4 (Week 7-8)
- [ ] Implement prompt enhancer
- [ ] Integrate with existing agent loop
- [ ] Add configuration support
- [ ] Write integration tests

### Sprint 5 (Week 9-10)
- [ ] Implement memvid reader (exploratory)
- [ ] Implement session integration
- [ ] Add caching and performance optimization
- [ ] Write documentation

### Sprint 6 (Week 11-12)
- [ ] Manual testing across projects
- [ ] Performance tuning
- [ ] Bug fixes and polish
- [ ] User guide and examples

## Open Questions

1. **Memvid Format**: What is the exact format of .claude/.mind.mv2 files? Can we read them reliably?

2. **Skill Execution**: Should we execute skills directly or wrap them through Meept's tool system?

3. **Context Priority**: How should we prioritize context from artifacts vs. Meept's internal knowledge?

4. **Artifact Updates**: How quickly should we reload artifacts when they change?

5. **Multi-Directory**: Should we maintain context for multiple directories simultaneously?

6. **Skill Categorization**: Should we use Claude's categories or our own?

7. **Agent Mapping**: How precisely should we map Claude agents to Meept's agent system?

## Dependencies

### External Dependencies
- `github.com/fsnotify/fsnotify` - File watching
- `gopkg.in/yaml.v3` - YAML frontmatter parsing

### Internal Dependencies
- `internal/config` - Configuration loading
- `internal/agent` - Agent system integration
- `internal/tools` - Tool registry
- `internal/memory` - Potential memvid integration

## Future Enhancements

1. **Artifact Indexing**: Build search index for large CLAUDE.md files
2. **Skill Discovery**: Suggest relevant skills based on task
3. **Context Visualization**: UI showing what context is active
4. **Artifact Editor**: Edit CLAUDE.md and skills through Meept
5. **Skill Templates**: Generate skill scaffolds
6. **Cross-Project Sharing**: Share artifacts across related projects
7. **Artifact Versioning**: Track different versions of artifacts
8. **Smart Context**: AI-driven context selection
9. **Artifact Analytics**: Track artifact usage patterns
10. **Community Skills**: Integrate with shared skill repositories

## Conclusion

This generalized plan ensures Meept works consistently with Claude Code's artifact system across any project. By automatically detecting and utilizing CLAUDE.md and .claude/ artifacts in the working directory, Meept gains project-specific context, additional skills, and enhanced decision-making capabilities without requiring per-repository configuration.

The key principles are:

1. **Automatic Detection**: Works in any directory with artifacts
2. **Claude-Consistent**: Uses artifacts the same way Claude Code does
3. **Zero Configuration**: No setup required per project
4. **Graceful Degradation**: Functions normally without artifacts
5. **Universal Compatibility**: Works with any Claude Code artifacts

This approach provides immediate value to users who already have Claude Code artifacts in their projects while maintaining full backward compatibility for users without them.
