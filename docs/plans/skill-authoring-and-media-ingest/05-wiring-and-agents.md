# Wiring, Agent Grants, Docs, and the learn-from-video Skill - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on
> existing source files — explore with search_files or terminal cat.
> After writing a file, do NOT read it back to verify — write once and
> stop. After completing, report what you built, what files you touched,
> and any deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Register the three new tools in the daemon, add security
  risk rules, add config for transcript_fetch, grant the tools to the
  agents that should use them, update feature docs, and ship the
  `learn-from-video` composition skill.
- **Dependencies:** 01, 02, 03, 04 COMMITTED (this leaf wires their
  exported symbols; it verifies against HEAD, not WIP)
- **Estimated Context:** 55K
- **Concurrency Group:** B (alone)

## Goal

Unwired features are incomplete (AGENTS.md wiring requirement). This
leaf makes the three tools real: construction + registration in the
daemon with the correct ordering, security classification
(transcript_fetch LOW; skills_create/skills_patch HIGH so the standard
confirmation flow gates self-modification), config plumbing for the
transcript subprocess, tool grants on researcher/analyst/general
agents, docs (docs/workflows/skills.md + media ingest), and the
shipped skill that composes transcript_fetch -> model synthesis ->
skills_create into the user-visible "paste a URL, get a skill" flow.

## Context

Daemon construction: `internal/daemon/components.go` runs
`initializeSkills` at line ~856 (registry/executor/writer: writer is
`lifecycle.NewWriter` with SetVersioner + SetRegistry wired; see the
initializeSkills body) and initializes TOOLS earlier in the same init
path — the exact ordering is the first thing this leaf verifies (grep
`initializeSkills` and the tools init function; read both). Three
tools from leaves 01-03 with these constructors (exact signatures in
master.md Interface Contracts):
- `builtin.NewTranscriptFetchTool(builtin.TranscriptConfig{...}, logger)`
- `builtin.NewSkillCreateTool(writer, logger)` (+ SetRegistry)
- `builtin.NewSkillPatchTool(registry, writer, logger)`

Security rules: the engine classifies tool calls by a rules table
(internal/security/engine.go; the cua-driver precedent shows prefix
rules with a documented mapping — computer-use-security.md). Config
schema lives in internal/config/schema.go (search for the Tools/
browser config pattern at ~3134 for the AutoSkillUnder precedent).

Key files:
- internal/daemon/components.go — tool registration + initializeSkills
- internal/config/schema.go — Tools config struct
- internal/security/engine.go — risk rules table
- config/skills/ — shipped skill directories (pick a neighbor as the
  layout exemplar, e.g. config/skills/computer-use/)
- docs/workflows/skills.md — feature doc to extend

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/config/schema.go
// (within the existing Tools config struct — find the browser tools'
// config as the template)
type TranscriptToolConfig struct {
    Enabled        bool   `json:"enabled"`          // default false
    PythonPath     string `json:"python_path"`      // default "python3"
    ModuleName     string `json:"module_name"`      // default youtube-transcript-api
    TimeoutSeconds int    `json:"timeout_seconds"`  // default 60
}
// Field tag style MUST match the surrounding struct (read it first;
// some sections use json5 tags with omitempty conventions).

// config/skills/learn-from-video/SKILL.md — ships in the system tier;
// body follows master.md Contract: workflow = transcript_fetch ->
// chunk if large -> extract generalizable procedure -> draft skill
// content -> skills_create (or skills_patch to extend an existing
// skill) -> report created path. See Task 5 for required sections.
```

### What This Leaf Consumes

```
// From leaf 01 (committed): builtin.NewTranscriptFetchTool,
// builtin.TranscriptConfig, tool name "transcript_fetch".
// From leaf 02 (committed): builtin.NewSkillCreateTool,
// SetRegistry, tool name "skills_create".
// From leaf 03 (committed): builtin.NewSkillPatchTool,
// tool name "skills_patch".
// From leaf 04 (committed): skills.Skill.Dir/LinkedAssets (docs
// reference them; skills.list output includes them).
// Existing: Components.SkillRegistry, the lifecycle writer built in
// initializeSkills, the security engine rules table, config schema.
```

## Tasks

### Task 1: Config struct + defaults

**Objective:** Add TranscriptToolConfig to the tools config section
with defaults applied like sibling sections do.

**Files:**
- Modify: `internal/config/schema.go`
- Test: `internal/config/schema_test.go` (extend the defaults test
  pattern used for other tool sections)

**Step 1: Write failing test**

```go
func TestTranscriptToolConfigDefaults(t *testing.T) {
    // Default config: Enabled==false, PythonPath=="python3",
    // ModuleName=="youtube-transcript-api", TimeoutSeconds==60.
}
```

**Step 2: Run test to verify failure**
Run: `go test ./internal/config/ -run TestTranscriptToolConfig -count=1`
Expected: FAIL

**Step 3: Write minimal implementation**

Follow the file's existing default-application convention (some
sections default in Load/UnmarshalJSON, some via a normalize func —
read how the browser or media section does it and mirror).

**Step 4: Run test to verify pass**
Expected: PASS

### Task 2: Tool registration with correct ordering

**Objective:** Construct + register all three tools in the daemon;
skills tools must hold non-nil writer/registry.

**Files:**
- Modify: `internal/daemon/components.go` (initializeTools block near
  the webSearchTool registration; plus initializeSkills ONLY if
  ordering requires deferring skills-tool registration to after the
  writer exists — prefer the minimal change)
- Test: `internal/daemon/components_test.go` or a new
  `internal/daemon/skill_tools_wiring_test.go`

**Step 1: Write failing test**

```go
func TestSkillAndTranscriptToolsRegistered(t *testing.T) {
    // Construct Components the way existing components tests do (read
    // components_test.go for the harness — if full Components init is
    // too heavy, extract nothing and instead test the registration
    // helper this task introduces: see Step 3).
    // Assert registry.Resolve (or the registry's accessor) returns
    // non-nil for "transcript_fetch" (with config Enabled=true),
    // "skills_create", "skills_patch"; and that the skills tools were
    // built with non-nil writer/registry handles (expose via getter or
    // registration-order evidence the test can check).
}
```

**Step 2: Run test to verify failure**
Expected: FAIL

**Step 3: Write minimal implementation**

- First VERIFY the ordering (grep the init path). If tools initialize
  before initializeSkills (expected), register the skills tools at the
  END of initializeSkills (where the registry/writer already exist) —
  a `registerSkillAuthoringTools(registry, writer, logger)` helper
  keeps the diff surgical. transcript_fetch registers in the
  initializeTools web block, gated `if cfg.Tools.Transcript.Enabled`
  (mirror the [browser] gate style) and built from the config struct.
- skills_create: `SetRegistry(c.SkillRegistry)` after construction.
- Wire TranscriptConfig fields through (python path, module, timeout).

**Step 4: Run test to verify pass**
Expected: PASS

### Task 3: Security risk rules

**Objective:** transcript_fetch LOW; skills_create + skills_patch HIGH
(fail-closed default classification must not accidentally mis-rate
them — explicit rules, cua-driver precedent).

**Files:**
- Modify: `internal/security/engine.go` rules table (the section with
  the ComputerUseRule prefix mapping, or the base rules list — follow
  where tool-specific base rules live)
- Test: `internal/security/engine_test.go` (extend the rule-lookup
  test pattern)

**Step 1: Write failing test**

```go
func TestSkillToolRiskClassification(t *testing.T) {
    // Check("transcript_fetch", ...) -> LOW, no confirmation required.
    // Check("skills_create", ...) -> HIGH; confirmation required when
    // require_confirmation_high=true.
    // Check("skills_patch", ...) -> HIGH, same.
}
```

**Step 2: Run test to verify failure**
Expected: FAIL

**Step 3: Write minimal implementation**

Add the three base rules. transcript_fetch observation-only LOW.
skills_create/skills_patch HIGH (self-modification: the agent edits
its own future instructions). Do NOT add tool_rules config entries —
base rules only; operators can override in DB later.

**Step 4: Run test to verify pass**
Expected: PASS

### Task 4: Agent tool grants

**Objective:** The agents that learn things can use the new tools.

**Files:**
- Modify: `config/agents/analyst/AGENT.md`,
  `config/agents/researcher/AGENT.md` (whichever of these exist —
  search_files "AGENT.md" under config/agents/ and grant to the
  general research/analysis agents; the coder agent does NOT get
  skills_create/patch by default)
- Test: none (config files; verified by Task 6 grep)

**Step 1:** Identify the agent spec format (read two neighbor
AGENT.md files; tool grants are a YAML list per the roster gate
doc's frontmatter pattern).
**Step 2:** Add `transcript_fetch` to researcher + analyst; add
`skills_create`, `skills_patch`, and `transcript_fetch` to the
general-purpose/default agent if one exists in config/agents/ (the
chat agent spec). One line each, no other changes.

### Task 5: The learn-from-video skill

**Objective:** Ship the composition skill that makes the capability
user-visible.

**Files:**
- Create: `config/skills/learn-from-video/SKILL.md`

**Step 1:** Read one shipped skill (config/skills/computer-use/
SKILL.md) for frontmatter + section conventions (meept's parser rules:
name, description, optional tags/requires).
**Step 2:** Write the skill. Required content:

```markdown
---
name: learn-from-video
description: "Turn a YouTube video (lecture, tutorial, demo) into a
  reusable skill: fetch the transcript, extract the generalizable
  procedure, and persist it as a new or updated skill."
tags: [media, learning, skills]
---

# learn from video

## when to use
User pastes a YouTube URL (or names a video) and wants the knowledge
captured as a durable skill — not just summarized.

## workflow
1. Call transcript_fetch with the URL. If it errors, surface the
   install guidance verbatim; do not improvise a fallback without
   asking.
2. If the transcript exceeds ~50k characters, summarize it in
   overlapping ~40k chunks (2k overlap) before synthesis.
3. Extract what generalizes: steps, decision rules, failure modes,
   tool/API names, and the WHY behind choices. Discard one-off
   specifics (names, prices, dates) unless the user asked for them.
4. Draft the skill: frontmatter (kebab-case name derived from the
   topic; one-line description; tags), body with: when to use,
   prerequisites, numbered procedure, decision rules, verification
   steps, pitfalls. Keep it under ~200 lines.
5. SHOW THE DRAFT to the user and ask to confirm before writing.
6. On confirm: skills_create for a new skill; skills_patch (replace
   mode) to extend an existing skill. Report the created/patched path.

## decision rules
- The video teaches a PROCEDURE -> skill. It only reports NEWS ->
  offer a summary instead.
- Existing skill covers the topic -> propose skills_patch with the
  exact old_string; never blind-rewrite.
- Ambiguous or conflicting steps in the transcript -> ask the user;
  never guess silently.
- Secrets, tokens, or credentials appearing in the transcript -> never
  copy them into the skill.

## verification
- After writing, run the skills list/get path (skills.get) to confirm
  the skill is discoverable, and report the name the user can invoke.
```

**Step 3:** Verify it parses: run the discovery fixture approach from
discovery_test.go against a temp copy, or the parser directly — the
orchestrator will re-verify at integration.

### Task 6: Docs

**Objective:** Feature documentation per AGENTS.md (internal/<pkg> ->
docs/workflows/<pkg>.md).

**Files:**
- Modify: `docs/workflows/skills.md` — new "Agent-facing skill
  authoring" section: the two tools, HIGH-risk confirmation behavior,
  tier targeting (new -> user tier; system tier read-only), versioning
  via lifecycle.Writer, and `dir`/`linked_assets` fields (leaf 04)
  with the SKILL_DIR executor convention.
- Modify: `docs/workflows/external-integrations.md` — short
  "transcript_fetch" subsection next to the media tooling: config
  keys, dependency install, YouTube URL forms, error behaviors.
- Modify: `AGENTS.md` — NO changes required (no new packages; tools
  join existing internal/tools/builtin). Verify that claim and state
  it in your report (AGENTS.md maintenance rule satisfied by
  verification).

**Step 1-2:** Write the doc sections (lowercase UI strings; code
identifiers verbatim). Cross-link the two docs.

### Task 7: gofmt + affected packages green

gofmt; `go build ./... && go vet ./...`; `go test ./internal/config/
./internal/security/ ./internal/daemon/ -run '<your new tests>'
-count=1` (scoped -run only; full daemon suite is orchestrator's
integration job). All pass.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep - only what the tasks specify
- [ ] gofmt clean; no ignored errors; config defaults correct

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match exactly (config field names/tags;
      skill frontmatter; tool names)
- [ ] Ordering: skills tools demonstrably hold non-nil writer/registry
      (test evidence, not comments)
- [ ] transcript_fetch registered ONLY when config enabled (default
      off preserved — verify daemon starts without the dependency)
- [ ] No bugs, no security issues (HIGH classification present for
      both authoring tools; no grant of authoring tools to coder)
- [ ] No scope creep beyond specified tasks (NO RPC handlers; NO
      evolver changes; NO new agents)

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The skills tools registering at the end of initializeSkills is the
  expected resolution of the master's ordering hazard. If you find the
  writer is actually available earlier, prefer registering in the
  main tools block for cohesion — but ONLY with test evidence that
  the handles are non-nil at registration time.
- The learn-from-video skill intentionally requires user confirmation
  before writing (step 5) — this matches the HIGH risk class and the
  user's review preferences. Do not soften it.
- AGENTS.md: this tree adds no new packages and no new build targets;
  the maintenance rule is satisfied by the no-change verification.
  State that explicitly in your report.
