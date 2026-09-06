# Skill Authoring and Media Ingest - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 5 leaf documents under this node
- **Scope:** Close the meept vs Hermes "learn from media / persist learned
  procedures" capability gap: agent-facing skill authoring tools, a
  transcript-ingest tool, linked-asset support in skills, wiring, and the
  learn-from-video composition skill.

## Goal

Hermes can turn a YouTube URL into a durable skill: a media-ingest skill
fetches the transcript, the model extracts the generalizable procedure,
and `skill_manage` writes the SKILL.md into the library. Meept has the
skills runtime (discovery tiers, parser, registry, executor, lifecycle
Writer with versioning + tier resolution) but NO agent-facing write path,
no media/transcript ingest tool, and no linked-asset convention. Today an
agent that learns a procedure has nowhere to put it.

This tree delivers the full chain:

| # | Capability                    | Leaf |
|---|-------------------------------|------|
| 1 | Transcript ingest (URL->text) | 01   |
| 2 | Skill create (agent-facing)   | 02   |
| 3 | Skill patch (agent-facing)    | 03   |
| 4 | Linked assets (scripts/refs)  | 04   |
| 5 | Wiring + agent specs + the learn-from-video skill | 05 |

After leaves 01-04, `web_fetch` (existing) and `transcript_fetch` (new)
plus `skills_create`/`skills_patch` (new) give meept parity with the
Hermes flow, with meept's own governance (risk-classified tools,
versioned writes, audit) where Hermes has none.

## Architecture

Three new builtin tools in `internal/tools/builtin/` follow the
established tool pattern (`WebFetchTool`): struct + `New*Tool`
constructor + optional `Set*` setters with nil guards + `Name()/
Description()/Parameters()/Execute()` implementing `tools.Tool`
(`internal/tools/interface.go:24`). `transcript_fetch` shells out to
Python (`youtube-transcript-api`) as a subprocess — the same pattern as
STT (whisper.cpp subprocess) — and degrades gracefully when the
dependency is missing. `skills_create` and `skills_patch` are thin,
heavily-validated wrappers over the EXISTING
`internal/skills/lifecycle.Writer` (tier resolution, versioner, SHA
dedup already implemented and wired at `internal/daemon/components.go`
`initializeSkills`); they add no new storage path.

Linked assets extend `internal/skills` read-side only: the `Skill`
struct gains `Dir` + `LinkedAssets`, the parser populates them from the
skill directory, and the executor injects the skill dir into the
execution context so agents can run helper scripts through the existing
`shell_execute` risk classification.

Wiring is centralized in leaf 05 (single-writer discipline for
`internal/daemon/components.go` and `internal/config/schema.go`): tool
registration in the web-tools block of `initializeTools`, security risk
rules, agent spec tool grants, docs, and the shipped
`config/skills/learn-from-video/SKILL.md` composition skill.

## Interface Contracts

### Contract 1: TranscriptFetchTool (C1)

```
// File: internal/tools/builtin/transcript_fetch.go
package builtin

// Tool name: "transcript_fetch". Risk class: LOW (observation-only).
type TranscriptConfig struct {
    PythonPath   string // default "python3"
    ModuleName   string // default "youtube-transcript-api"
    TimeoutSeconds int  // default 60
}

func NewTranscriptFetchTool(cfg TranscriptConfig, logger *slog.Logger) *TranscriptFetchTool

// Parameters (JSON Schema, OpenAI function format):
//   url       string, required  — any YouTube URL form (watch, youtu.be,
//                               shorts, embed, live, raw 11-char video ID)
//   timestamps bool, optional    — include [MM:SS] markers (default false)
//   language  string, optional    — BCP-47 preference; falls back to
//                               default transcript when unavailable
// Output: transcript text (truncated to the tool-result cap); errors are
// actionable strings ("transcript disabled", "video unavailable",
// "youtube-transcript-api not installed — run: <python> -m pip install
// youtube-transcript-api").

// Owner: 01-transcript-fetch-tool.md
// Consumers: 05-wiring-and-agents.md (registration + config)
```

### Contract 2: SkillCreateTool (C2)

```
// File: internal/tools/builtin/skill_create_tool.go
package builtin

// Tool name: "skills_create". Risk class: HIGH (agent modifies its own
// future instructions; [security] require_confirmation_high gates it).
func NewSkillCreateTool(writer *lifecycle.Writer, logger *slog.Logger) *SkillCreateTool
func (t *SkillCreateTool) SetRegistry(r *skills.Registry)  // nil-guarded

// Parameters:
//   name        string, required — kebab/lowercase skill name
//   description string, required — one-line description (frontmatter)
//   body        string, required — full markdown body AFTER the frontmatter
//   tags        []string, optional
// Behavior: assembles frontmatter (name, description, tags) + body into
// SKILL.md content, validates via the existing parser rules (name match,
// no duplicate), delegates to lifecycle.Writer.WriteSkill(name, content).
// The writer's tier resolver lands NEW skills in the user tier
// (~/.meept/skills/<name>/SKILL.md); versioner records the write.
// Output: created path + confirmation. Errors: invalid name, duplicate,
// writer failure — all surfaced verbatim, never swallowed.
// Owner: 02-skill-create-tool.md
// Consumers: 05-wiring-and-agents.md
```

### Contract 3: SkillPatchTool (C3)

```
// File: internal/tools/builtin/skill_patch_tool.go
package builtin

// Tool name: "skills_patch". Risk class: HIGH (same rationale as C2).
func NewSkillPatchTool(registry *skills.Registry, writer *lifecycle.Writer, logger *slog.Logger) *SkillPatchTool

// Parameters (either mode, never both):
//   name       string, required           — existing skill name
//   old_string string, required for replace — unique snippet to find
//   new_string string, required           — replacement (empty = delete match)
//   content    string, required for rewrite — FULL new SKILL.md content
// Behavior: replace-mode loads current body from the registry, asserts
// old_string occurs exactly once, swaps, reassembles, writes via
// lifecycle.Writer (versioned). rewrite-mode writes content after
// parser validation. Refuses to touch skills in the system tier
// (write the user-tier copy instead and say so).
// Output: patched path. Errors: skill not found, old_string not found /
// not unique, system-tier refusal.
// Owner: 03-skill-patch-tool.md
// Consumers: 05-wiring-and-agents.md
```

### Contract 4: Skill linked assets (C4)

```
// Files: internal/skills/models.go, parser.go, executor.go
package skills

// New on Skill:
type LinkedAsset struct {
    Name    string // basename, e.g. "fetch_transcript.py"
    RelPath string // "scripts/fetch_transcript.py"
    Kind    string // "script" | "reference" | "template" | "asset"
}
// Skill gains: Dir string `json:"dir,omitempty"`
//              LinkedAssets []LinkedAsset `json:"linked_assets,omitempty"`

// Dir = the directory containing SKILL.md ("" for flat-file skills).
// LinkedAssets = scan of Dir subdirs scripts/, references/, templates/,
// assets/ (other files ignored). Populated by the directory-layout path
// in scanTier/loadSkillFile. Flat-layout skills keep both empty.

// Executor: when skill.Dir != "", the execution context includes a
// "skill_dir: <Dir>" line so the agent can invoke linked scripts via
// shell_execute. Hermes-compatible skill bodies referencing SKILL_DIR
// get SKILL_DIR substituted with the same value.
// Owner: 04-linked-assets.md
// Consumers: 05-wiring-and-agents.md (docs), skills.list/skills.get RPC
// surfaces pick the fields up automatically via struct serialization.
```

### Contract 5: Registration seam (C5)

```
// File: internal/daemon/components.go, func initializeTools
// Anchor: the block beginning
//   webSearchTool := builtin.NewWebSearchTool(15 * time.Second)
// Immediately after the web-search registration, gated on new config:
//   cfg.Tools.Transcript.Enabled (default FALSE — subprocess dependency,
//   opt-in like [browser])
//   skill authoring tools registered unconditionally (writer already
//   exists at this point? NO — initializeTools runs before
//   initializeSkills; see leaf 05 ordering task).

// File: internal/config/schema.go
//   Tools struct gains Transcript TranscriptConfig `json:"transcript"`
//   with json5 defaults per Contract 1.

// Security rules (internal/security engine rules table):
//   transcript_fetch -> LOW (observation)
//   skills_create    -> HIGH (self-modification)
//   skills_patch     -> HIGH (self-modification)

// Owner: 05-wiring-and-agents.md only (single writer for components.go
// and schema.go in this tree).
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-transcript-fetch-tool.md | leaf | none | 45K | A |
| 02 | 02-skill-create-tool.md | leaf | none | 40K | A |
| 03 | 03-skill-patch-tool.md | leaf | 02 (constructor shape, not files) | 40K | A* |
| 04 | 04-linked-assets.md | leaf | none | 45K | A |
| 05 | 05-wiring-and-agents.md | leaf | 01, 02, 03, 04 COMMITTED | 55K | B |

\* 02 and 03 touch disjoint files (skill_create_tool.go vs
skill_patch_tool.go) and may dispatch together; 03 mirrors 02's
constructor conventions exactly as specified in its Context section.
If either is re-dispatched after review, serialize the fix rounds.

**Concurrency groups:** group A = 01, 02, 03, 04 (no shared files).
Group B = 05 alone, dispatched only after A is committed.

## Dispatch Protocol

For each concurrency group, in dependency order:

### Phase 1: Dispatch Concurrency Group A

Dispatch children 01, 02, 03, 04 simultaneously via `delegate_task`:

- Goal: "Implement all tasks from <leaf doc>"
- Context: full leaf document text + the relevant Interface Contract
  from this master + the coding conventions block below + the named
  key files INLINED (each leaf lists them).
- Include: "Do NOT commit. Do NOT run git add. Write code, run tests,
  report results only."
- Include: "Do NOT use read_file on existing source files — explore
  with search_files or terminal cat instead. If you read a file, never
  feed its output into write_file."
- Include: "After writing a file, do NOT read it back to verify. Write
  once and stop."
- Agent follows TDD per the leaf's instructions.
- Meept-specific: "Run gofmt on every file you touch. Run
  `go build ./...` for the packages you touched. Tests:
  `go test ./internal/tools/builtin/ -run <prefix> -count=1` (or the
  leaf's package). Do NOT run full-repo `go test ./...` unbounded —
  this repo exhausts ephemeral ports; if you must, use -p 2."

### Phase 2: Review and Commit Each Child

After each implementation agent returns, the orchestrator reviews
in-session (the main model reviews directly, NOT a delegated subagent):

1. Read the changed files (from the implementer's file list).
2. Check against leaf spec + interface contracts + Review Checklist.
3. Run the leaf's tests and `go build ./...`.
4. On gaps: re-dispatch with specific feedback (max 3 cycles, then
   escalate). On pass: `git add <exact paths> && git commit -m
   "<leaf's commit message>"`, mark REVIEWED.

Commit order: 02 before 03 (03's test file may reference the create
tool's file only via package-level coexistence, not symbols — verify no
cross-import anyway). 01, 02, 04 may commit in any order.

### Phase 3: Integration Review

After ALL children reach REVIEWED:

1. `go build ./...` and `go vet ./...`.
2. `go test ./internal/tools/... ./internal/skills/... -count=1` (with
   -p 2 if running wider).
3. Verify C5: grep components.go for the three tool names — each
   registered exactly once; grep schema.go for the Transcript config.
4. Verify the shipped skill: `config/skills/learn-from-video/SKILL.md`
   parses (meept's parser accepts it — run the parser test pattern or
   the skills discovery smoke).
5. Normalize formatting: gofmt across all changed files.
6. Verify no line-number corruption:
   `grep -rcE '^\s+[0-9]+\|' --include='*.go' internal/tools internal/skills internal/config internal/daemon` returns zero.
7. Commit integration changes if any; mark all children COMPLETE.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts from this orchestrator are satisfied
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD followed)
- [ ] Code follows project conventions (see Coding Conventions below)
- [ ] No scope creep (nothing beyond spec)
- [ ] No obvious bugs or security issues
- [ ] No debug artifacts: no print/stdout debugging, no TODOs, no
      placeholder values, no commented-out code
- [ ] No line-number corruption: no `     N|` prefixes baked into
      source files
- [ ] Every `Set*` method has a nil guard; no ignored `_ =` errors;
      map type assertions use the two-value form

Output: APPROVED or list of specific gaps.

## Coding Conventions

- **Language:** Go (module github.com/caimlas/meept). gofmt clean.
- **Naming:** exported PascalCase, unexported camelCase; tool files
  `<tool_name>_tool.go` matching the builtin directory pattern.
- **Tool interface:** implement `tools.Tool` exactly
  (internal/tools/interface.go:24): Name, Description, Parameters,
  Execute.
- **Error handling:** wrap with `%w` + context; return early; never
  `panic` in library code; never ignore errors with `_ =`.
- **Setters:** every `Set*` method guards nil (verified by
  internal/tools/builtin/setters_test.go patterns).
- **Type assertions** on bus/map payloads: two-value form only.
- **Tests:** table-driven where shape repeats; `*_test.go` alongside;
  `-count=1`; no sleeps/timing luck; fake subprocess via injectable
  command runner where the leaf specifies.
- **No commits by leaves.** Orchestrator commits after review.
- **Docs:** feature changes update docs/workflows/<pkg>.md (leaf 05
  owns all docs in this tree). UI-facing strings lowercase.
- **ID generation:** pkg/id.Generate, never time.Now().UnixNano().
- **Large test sweeps:** -p 2 (ephemeral-port exhaustion hazard).

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-transcript-fetch-tool | PENDING | 0 | |
| 02-skill-create-tool | PENDING | 0 | |
| 03-skill-patch-tool | PENDING | 0 | |
| 04-linked-assets | PENDING | 0 | |
| 05-wiring-and-agents | PENDING | 0 | |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. Build + vet: `go build ./... && go vet ./...` — clean.
2. Package tests: `go test ./internal/tools/builtin/ ./internal/skills/... -count=1` — pass.
3. Wiring grep: exactly one registration per new tool in
   internal/daemon/components.go; Transcript config present in
   internal/config/schema.go with Enabled default false.
4. Parser acceptance: internal/skills tests confirm a skill directory
   containing scripts/ + references/ populates Dir + LinkedAssets.
5. Manual smoke (post-deploy, optional, documents expected behavior):
   agent turn with transcript_fetch enabled returns transcript text for
   a public video; skills_create writes ~/.meept/skills/<name>/SKILL.md,
   versioner logs the write, skills.list shows the skill without daemon
   restart; skills_patch replace-mode rejects a non-unique old_string.
6. Security: with require_confirmation_high=true (default), a
   skills_create call surfaces a confirmation prompt; transcript_fetch
   never does.

## Structural Completeness Check (Before Dispatch)

Required sections verified present in this master: Dispatch Protocol,
Interface Contracts, Child Index, Review Checklist, Coding Conventions,
Completion Tracking Table, Integration Test Plan. Each leaf carries:
DISPATCH header, Parent/scope/dependencies/estimated context, TDD
tasks, interface contract section, Self-Verification Checklist, Review
Checklist, and a "Do NOT commit" instruction.

## Open Questions

- **youtube-transcript-api CLI surface** (leaf 01): the package's
  command-line interface changed across major versions. The leaf
  instructs the implementer to probe `python3 -m
  youtube_transcript_api --help` at implementation time and prefer the
  `python3 -c` JSON-lines approach if the CLI is unstable. Resolved by
  the leaf's fake-runner tests being CLI-shape-agnostic.
- **Daemon init ordering** (leaf 05): tools initialize before
  initializeSkills in the current components.go path. The leaf's
  preferred resolution (register skill-authoring tools at the end of
  initializeSkills) is expected but must be verified with a wiring
  test before commit — not assumed.
- **Agent spec format** (leaf 05 Task 4): tool grants in AGENT.md
  frontmatter follow the roster-gate doc's YAML pattern; the exact
  key name must be read from two neighbor agent specs before editing.

## Notes

- The lifecycle.Writer already implements tier-resolved writes,
  versioning, SHA dedup, and registry hook-up. Leaves 02/03 must NOT
  reimplement any of that — constructor-inject the writer
  (internal/skills/lifecycle.NewWriter, wired at
  internal/daemon/components.go initializeSkills with
  SetVersioner + SetRegistry + tier resolver).
- Ordering hazard (leaf 05): initializeTools runs before
  initializeSkills in daemon component init (components.go:856 calls
  initializeSkills; tools initialize earlier in the same path). Leaf 05
  must resolve tool-vs-writer construction order explicitly — either
  construct the writer before tool registration or defer skill-tool
  registration into/after initializeSkills. Decide in the leaf, keep
  the diff minimal, and add a wiring test asserting the registered
  tools resolve non-nil writer handles.
- Transcript dependency is external (Python + youtube-transcript-api).
  Everything degrades to an actionable error message; no hard daemon
  dependency. Config-gated off by default, matching the [browser]
  precedent.
- Known non-goal: RPC/CLI write surface for skills (skills.create RPC
  handler) — the agent tools are the deliverable; an RPC handler is a
  trivial follow-up once C2/C3 land. Note it in docs as future work,
  do not build it here.
- Scope guard for all leaves: NO changes to the evolver
  (internal/selfimprove, internal/skills/lifecycle evolver_*), NO
  changes to wiki stores, NO agent loop changes.
