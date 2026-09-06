# Plan dialect v1 — normative specification

Status: normative for plan-dialect v1. The compiler (`plan.CompileSealed`,
leaf 02) implements this document verbatim. The planner agent's draft
template (`config/prompts/planner/plan_draft.md`) mirrors it.

## 1. Overview

The plan dialect is a markdown-only, human-editable brainstorm format for
meept plans. The planner agent writes it, the user edits it over one or
more brainstorm turns, and — after the user seals it — a deterministic Go
compiler reads it into `[]plan.PlanPhaseSpec`. No LLM is involved in
compilation.

Two hard rules define the dialect. Both exist to eliminate the LLM-JSON
failure class observed in live runs (2026-09-06): unmarshal errors on
`depends_on` arrays and "no JSON found in phase planner output".

1. **No JSON anywhere.** The document is markdown; no section, field, or
   annotation is JSON or JSON-embedded.
2. **No numeric cross-phase indices.** Work is referenced by *names*:
   artifacts by kebab-case name, steps as `PhaseN.S<step#>` refs. A bare
   integer is never a reference to another phase's work.

Phases themselves carry visible ordinal numbers in their headings
(`### Phase 1: …`) and the optional `**Depends on:** Phases N, M` line may
cite those numbers — that is a visible label on the section being
referenced, not an opaque index. Step and artifact references never use
bare numbers.

Waves (parallel phases) are *implicit*: two phases that consume only
artifacts from the same earlier foundation can run in parallel. The
orchestrator's existing frontier computes this from the produced/consumed
artifact graph. The dialect has no `wave:` fields.

Audiences: the planner agent (writes/updates the draft each turn), the
user (edits, then seals), and the compiler (parses the sealed draft). The
dialect supersedes `config/prompts/planner/interview.md` when
`plans.plan_compiler_enabled` is true.

## 2. Document shape and grammar

The complete v1 shape and rules, verbatim from master.md Contract A:

```
Document shape (all sections required unless marked optional):
  # Plan: <title>
  ## Meta            — YAML block: task_id, version, status
                       (draft|sealed), updated
  ## Goal            — 1-3 sentences, user-visible outcome
  ## Decisions       — bullet list: "Decision: X — Rationale: Y"
  ## Open Questions  — bullet list; MUST be empty to seal
  ## Phases          — one "### Phase N: <name>" per phase; each has:
    - prose intent paragraph (1-3 sentences)
    - "**Produces:**" bullet list of named artifacts
      `- <name> (<kind>) — <one-line description>` where kind ∈
      {file, interface, schema, decision, test_suite}
    - "**Consumes:**" same shape (may be "none")
    - optional "**Depends on:** Phases N, M" (omit when derivable
      from consumes; compiler fills it)
    - "**Steps:**" numbered list `1. <description> [tool_hint]
      (needs: <artifact-or-step-ref>)`; needs references artifacts
      BY NAME or prior steps as "PhaseN.S<step#>"
  ## Notes (optional)
Rules:
  - Artifact names are kebab-case, unique across the whole plan;
    consume names MUST match a produce name of an earlier phase.
  - Open Questions non-empty ⇒ seal refuses (exit code 2).
  - tool_hint ∈ existing hints {code, refactor, debug, fix, analyze,
    research, git, plan, chat, bash}.
  - No JSON anywhere. No numeric cross-phase indices.
```

The sections appear in that order; every required section appears exactly
once (`## Notes` is optional). Section order is recommended but not
compiler-enforced; duplicates are compile errors.

Line-precision grammar (regex-level). Blank lines are insignificant
everywhere except where noted; prose sections accumulate non-blank lines.

| Element | Line grammar |
|---|---|
| Title | `^# Plan: (.+)$` — exactly one, first heading, non-empty |
| Meta heading | `^## Meta$` |
| Meta entry | `^- ([a-z_]+): (.+)$` |
| Goal heading | `^## Goal$`; body is 1-3 sentences of plain prose |
| Decisions heading | `^## Decisions$`; body bullets `^- Decision: (.+) — Rationale: (.+)$` |
| Open Questions heading | `^## Open Questions$`; body is a `- ` bullet list, or nothing |
| Phases heading | `^## Phases$` |
| Phase heading | `^### Phase ([1-9][0-9]*): (.+)$` |
| Produces label | `^\*\*Produces:\*\*$` |
| Consumes label | `^\*\*Consumes:\*\*$` or `^\*\*Consumes:\*\* none$` |
| Artifact bullet | `` ^- `([a-z0-9]+(-[a-z0-9]+)*)` \((file\|interface\|schema\|decision\|test_suite)\) — (.+)$ `` |
| Depends on line | `^\*\*Depends on:\*\* Phases ([0-9]+(, [0-9]+)*)$` |
| Steps label | `^\*\*Steps:\*\*$` |
| Step line | `^([1-9][0-9]*)\. (.+?)( \[([a-z]+)\])?( \(needs: (.+)\))?$` |
| needs ref | `[a-z0-9]+(-[a-z0-9]+)*` (artifact name) or `Phase[1-9][0-9]*\.S[1-9][0-9]*` (step ref); refs are comma-separated inside one `(...)`: `(needs: auth-schema, Phase1.S2)` |
| Notes heading | `^## Notes$`; body is free `- ` bullets, compiler-ignored |

Notes on the grammar:

- The em dash `—` (U+2014) is significant in artifact bullets and
  Decision bullets; the plain hyphen `-` is not a substitute.
- Artifact names are written in backticks in the draft, matching how
  `writer.go` renders them.
- The intent paragraph is the non-label prose between the phase heading
  and the first `**…:**` label. It is free prose; no grammar applies.
- A phase heading MUST NOT carry a bracketed state suffix. `[state]`
  belongs to the persisted format only; a `### Phase 1: foo [pending]`
  line fails the phase-heading grammar.
- The word `none` after the Consumes label is the only inline value any
  label line accepts. Produces has no `none` form — every phase produces
  at least one artifact.
- The `## Decisions` and `## Notes` bodies are not validated by the
  compiler (they are for humans and the planner agent); the
  `Decision: X — Rationale: Y` bullet form is a template requirement.

## 3. Artifact rules

- **Names** are kebab-case: lowercase letters and digits joined by single
  hyphens (`^[a-z0-9]+(-[a-z0-9]+)*$`). No uppercase, no underscores, no
  leading/trailing/double hyphens.
- **Uniqueness:** a produced artifact name is unique across the whole
  plan. Two phases may not produce the same name.
- **Kinds** are exactly: `file`, `interface`, `schema`, `decision`,
  `test_suite` — the same enum as `plan.Artifact.IsValidKind`. No other
  kind compiles.
- **Consume-before-produce:** a name in a phase's Consumes block must be
  produced by an *earlier* phase. Same-phase and later-phase produces do
  not satisfy the rule. This mirrors Contract A verbatim: "consume names
  MUST match a produce name of an earlier phase."
- **Required is derived, never written.** The draft grammar has no
  `required` marker. The compiler sets `Artifact.Required = true` exactly
  when a later phase consumes the artifact, and leaves it false
  otherwise. A hand-written `(file, required)` parenthetical fails kind
  validation.
- **Descriptions** are one line, non-empty, free text after the em dash.
- Needs references to artifacts (see section 4) obey the same
  earlier-phase rule as Consumes.

## 4. Steps and needs references

Steps are a numbered list per phase. Numbers start at 1 and increase by
1 — they are labels used by `PhaseN.S<step#>` refs, so gaps and repeats
are compile errors.

`tool_hint` is optional. When present it is a lowercase word in square
brackets drawn from the existing hints, verbatim:
`{code, refactor, debug, fix, analyze, research, git, plan, chat, bash}`.
An unknown hint is a compile error; a missing hint compiles as-is (the
orchestrator applies its default routing).

`(needs: …)` is optional. When present it lists one or more
comma-separated references:

- **Artifact reference** — a kebab-case name that some *earlier* phase
  produces. Same-phase or later-phase artifacts may not be referenced.
  The compiler does not require the artifact to also appear in the
  phase's Consumes block, but emits a warning when it does not (see
  section 7).
- **Step reference** — `PhaseN.S<step#>`, where `N` is the target
  phase's ordinal from its heading and `<step#>` is the target step's
  displayed number. Allowed targets:
  - any step of an **earlier** phase;
  - a **prior** step of the **same** phase (`Phase2.S1` written inside
    phase 2). Later same-phase steps may not be referenced.

The dialect has no intra-phase "depends" syntax other than needs refs,
and no way to reference a later phase's step. When a step has no needs
clause, it depends on nothing beyond its phase's position.

## 5. Seal gate

A draft seals only when its `## Open Questions` section is empty —
defined as: zero non-blank lines under the heading. Resolved questions
are *deleted*, never struck through; a struck-through line is still
non-blank and still blocks sealing.

The seal flow (leaf 04 wiring):

1. `meept plan seal <task-id>` checks Open Questions first. Non-empty ⇒
   seal refuses with exit code 2 and the draft stays a draft.
2. The sealed document becomes immutable input. Its exact markdown bytes
   are hashed (sha256) and the hash is recorded on the task metadata
   (sealing is a state change, not an edit).
3. The compiler runs on the sealed bytes. Compile problems (section 7)
   are returned to the caller as readable plan problems; the draft stays
   a draft and feeds the next brainstorm round. There is no LLM retry.

The compiler itself also enforces the empty-Open-Questions rule (defense
in depth for direct library use), so the same problem can surface from
either layer.

## 6. Mapping to the persisted format

The sealed draft is a *superset* of meept's persisted plan markdown
(`internal/plan/parser.go`, `internal/plan/writer.go`). Leaf 04 persists
phases through the existing `PersistPlan` path; this table is the
normative translation.

| Draft element | Persisted plan.md (parser.go / writer.go) | CompiledPlan / PlanPhaseSpec |
|---|---|---|
| `# Plan: <title>` | `# Plan: <title>` (identical) | — |
| `- task_id: X` Meta entry | kept verbatim as an extra Meta key (parser.go `ExtraMeta` preserves unknown keys) | — (also recorded on task metadata) |
| `- version: 1` | kept verbatim as an extra Meta key | validated, not copied |
| `- status: draft` / `sealed` | **replaced** by the plan state (writer.go prints `plan.State`; initial value `pending`) | seal status + hash recorded on task metadata, not in the plan file |
| `- updated: YYYY-MM-DD` | kept verbatim as an extra Meta key | — |
| — (persistence adds) | `- plan_id:` and `- created:` Meta entries | plan ID minted at persistence |
| `## Goal` body | becomes the `## Summary` section body | — |
| `## Decisions` bullets | carried into `## Notes` as bullets (before existing Notes bullets) | — |
| `## Open Questions` | dropped (must be empty to seal) | — |
| `## Phases` container heading | dropped — persisted phases are top-level `## Phase N:` sections | — |
| `### Phase N: <name>` | `## Phase N: <name> [pending]` (state written by writer.go) | `Phases[N-1].Name` |
| phase intent prose | **not representable** in today's `ParsedPhase` markdown round-trip | `Phases[N-1].Description` |
| Produces bullets | `**Produces:**` block, one `` - `name` (kind) — desc `` line per artifact; consumed-by-later artifacts render as `(kind, required)` (writer.go `writeArtifactsBlock`) | `Produces []Artifact`; `Required` derived per section 3 |
| Consumes bullets | `**Consumes:**` block, same shape | `Consumes []Artifact` |
| `**Consumes:** none` | no Consumes block emitted (empty list) | empty `Consumes` |
| `**Depends on:** Phases N, M` | **not representable** in the markdown round-trip | `DependsOn []int` (phase sequence numbers); filled from consumes when the line is omitted |
| step `N. desc [hint] (needs: …)` | `N. desc [pending]`, plus ` (depends: a, b)` only for same-phase step refs | `Steps[i]{Description, ToolHint, DependsOn}` |
| `[hint]` bracket on a step | dropped from the line — in the persisted format the bracket is the step **status**, not a hint | `ToolHint` verbatim |
| needs artifact ref (earlier phase) | no per-step `depends` entry | contributes to the phase's `DependsOn` |
| needs `PhaseN.S#` (own phase, prior step) | ` (depends: <step#>)` | step-level `DependsOn` |
| needs `PhaseM.S#` (M < N) | no per-step `depends` entry — the persisted format's `depends` is intra-phase only | phase `DependsOn` gains M; the step-level edge is retained in `CompiledPlan` for tree emission (leaf 03) |
| `## Notes` bullets | `## Notes` bullets | — |

On persistence every phase is `[pending]` and every step `[pending]`;
progress states are written later by `UpdatePlanStatus`.

**Bracket semantics differ between the two formats.** In a draft, a step's
`[word]` is a tool hint; in a persisted plan, `[word]` is a step status.
A persisted plan file is therefore **not** a valid draft: feeding one back
through the compiler fails with unknown-tool-hint problems (`pending` is
not a hint). Do not round-trip persisted output through the compiler.

## 7. Compile error classes

`CompileSealed` returns **all** problems in one pass (never just the
first), each as `CompileProblem{Line, Message}`, so one brainstorm round
can fix everything. Callers render `plan compile failed: N problems`
followed by the list; fatal problems aggregate into
`*CompileError{Problems []CompileProblem}`. Non-fatal notes go to
`CompiledPlan.Warnings` as plain strings and never block sealing.

Line conventions for `CompileProblem.Line`: content errors anchor to the
offending line; phase-level errors anchor to the phase heading line;
`## Open Questions` anchors to its heading line; document-level problems
use line `0`.

### Hard errors

| # | Class | Example input (offending line) | Exact Message |
|---|---|---|---|
| 1 | missing required section | document without `## Goal` | `missing required section: %s` (arg: the heading text, e.g. `## Goal`) |
| 2 | duplicate section | a second `## Meta` | `duplicate section: %s` |
| 3 | no phases | `## Phases` with no `### Phase` under it | `plan declares no phases` |
| 4 | phase count over max | 13 phases, maxPhases 12 | `plan declares %d phases; maximum is %d` |
| 5 | dependency cycle | (unreachable in v1 — see below) | `dependency cycle among phases: %s` (arg: phase names joined by ` → `) |
| 6 | meta key missing | `## Meta` without `- task_id:` | `Meta is missing required key: %s` |
| 7 | bad version | `- version: 2` | `unsupported dialect version: got %q; this compiler implements version 1` |
| 8 | bad status | `- status: wip` | `Meta status must be "draft" or "sealed" (got %q)` |
| 9 | malformed Meta line | `task_id: t-1` (no leading `- `) | `expected "- <key>: <value>", got %q` |
| 10 | Open Questions non-empty | `- Do we cache?` under the heading | `Open Questions must be empty to seal (%d unresolved)` (count: non-blank lines in the section) |
| 11 | malformed phase heading | `### Phase one: setup` | `expected phase heading "### Phase N: <name>", got %q` (also fires for a `[state]` suffix) |
| 12 | phase numbering | second phase headed `### Phase 3:` | `phase numbering must be consecutive from 1 (expected Phase %d, got Phase %d)` |
| 13 | missing Produces | phase with no `**Produces:**` label | `phase %q declares no Produces block` |
| 14 | empty phase | phase with `**Steps:**` label but no steps, or no steps at all | `phase %q declares no steps` |
| 15 | malformed artifact bullet | `- user schema (file) — desc` | `` expected "- `<name>` (<kind>) — <description>", got %q `` |
| 16 | bad artifact name | `- User_Schema (file) — desc` | `artifact name %q is not kebab-case (lowercase letters and digits joined by single hyphens)` |
| 17 | unknown kind | `- user-schema (table) — desc` | `artifact %q has unknown kind %q (must be one of file, interface, schema, decision, test_suite)` |
| 18 | duplicate artifact | same name produced in two phases | `duplicate artifact name %q (already produced by phase %q)` |
| 19 | unknown consume | `- payment-gateway (interface) — …`, nothing produces it | `unknown artifact %q consumed by phase %q: no phase in this plan produces it` |
| 20 | consume before produce | phase 1 consumes a phase 2 produce | `phase %q consumes %q, which is produced by a later phase (%q): consumes must reference artifacts from earlier phases` |
| 21 | bad Depends on ref | `**Depends on:** Phases 4` (only 3 phases) | `phase %q cites nonexistent phase %d in "Depends on"` |
| 22 | forward Depends on | phase 1: `**Depends on:** Phases 2` | `phase %q cites phase %d in "Depends on", but only earlier phases may be cited` |
| 23 | malformed Depends on line | `**Depends on:** phase two` | `expected "**Depends on:** Phases <n>[, <n>...]", got %q` |
| 24 | malformed step line | `1 do the thing [code]` | `expected "<n>. <description> [tool_hint] (needs: <refs>)", got %q` |
| 25 | step numbering | steps numbered `1.` then `3.` | `step numbering in phase %q must start at 1 and increase by 1 (expected step %d, got step %d)` |
| 26 | unknown tool hint | `2. Run checks [test]` | `step %d of phase %q has unknown tool_hint %q (must be one of code, refactor, debug, fix, analyze, research, git, plan, chat, bash)` |
| 27 | unknown needs ref | `(needs: billing-invoice)` with no such artifact or step | `step %d of phase %q has a needs reference %q that matches no artifact name or step` |
| 28 | step ref to missing step | `(needs: Phase1.S9)`, phase 1 has 2 steps | `step %d of phase %q references %q, but phase %d has no step %d` |
| 29 | step ref to later phase | phase 1 step refs `Phase2.S1` | `step %d of phase %q references %q from phase %d, a later phase: step references may only target earlier phases` |
| 30 | step ref to later same-phase step | phase 2 step 1 refs `Phase2.S3` | `step %d of phase %q references %q, a later step in the same phase: steps may only reference prior steps` |
| 31 | needs artifact from same phase | step of phase 1 refs `Phase1`'s own produce | `step %d of phase %q references artifact %q, which is produced by the same phase: artifact references must target earlier phases` |

Class 5 (cycle) is unreachable while the earlier-phase rules (consumes,
Depends on, needs) are enforced — every edge points backward, so the
consume graph is a DAG by construction. Implement the check anyway
(Contract B requires it): it is the primary guard if ordering rules are
ever relaxed, and it costs one graph walk over the final edge set.

### Warnings

| # | Trigger | Exact Warnings entry |
|---|---|---|
| W1 | a phase omits `**Depends on:**` and has consumes | `depends_on inferred for phase %q from consumed artifacts` |
| W2 | a phase's explicit `**Depends on:**` is fully implied by its consumes | `phase %q's "Depends on" duplicates dependencies already implied by consumed artifacts` |
| W3 | a needs ref names an earlier-phase artifact absent from the phase's Consumes block | `step %d of phase %q references artifact %q in needs, but it does not appear in the phase's Consumes block` |

Meta `status` is descriptive: the compiler does not gate on it. The CLI
seal flow is what transitions a draft to sealed and records the hash.

## 8. Examples

### 8.1 Minimal — two phases, three steps

```markdown
# Plan: Add user avatar upload

## Meta

- task_id: t-20260906-avatar
- version: 1
- status: draft
- updated: 2026-09-06

## Goal

Users can upload a profile avatar that persists across sessions.

## Decisions

- Decision: Store avatars on the local filesystem — Rationale: single-node deployment, no object store available.

## Open Questions

## Phases

### Phase 1: Avatar storage

Give uploads a durable home and validation rules.

**Produces:**

- `avatar-store` (file) — on-disk avatar directory plus write and validate helpers

**Consumes:** none

**Steps:**

1. Create the avatar directory layout and permissions [code]
2. Implement size and type validation helpers [code] (needs: Phase1.S1)

### Phase 2: Upload endpoint

Wire the HTTP upload path to the store.

**Produces:**

- `avatar-upload-endpoint` (interface) — POST /users/me/avatar handler

**Consumes:**

- `avatar-store` (file) — validated storage for uploaded images

**Steps:**

1. Add the upload handler backed by avatar-store [code] (needs: avatar-store)

## Notes

Max upload size is 2 MiB; JPEG and PNG only.
```

Walk-through: phase numbering 1,2 consecutive; step numbering 1,2 and 1
consecutive; `Phase1.S1` is a prior same-phase step; `avatar-store` is
produced by phase 1 and consumed by later phase 2 (so the compiler sets
`avatar-store.Required = true`); phase 2's `DependsOn` is inferred from
the consume (warning W1); Open Questions is empty, so the draft is
sealable once the user confirms.

### 8.2 Parallel — shared foundation, implicit waves

```markdown
# Plan: Split monolith config loading

## Meta

- task_id: t-20260906-config-split
- version: 1
- status: draft
- updated: 2026-09-06

## Goal

The CLI and the daemon each load only the config sections they need instead of the whole file.

## Decisions

- Decision: One shared schema package, two thin loaders — Rationale: validation lives in one place while loaders stay small.

## Open Questions

## Phases

### Phase 1: Config schema package

Extract schema definitions and validation into a shared internal package.

**Produces:**

- `config-schema` (schema) — validated struct definitions for all config sections
- `config-schema-tests` (test_suite) — table tests covering schema validation

**Consumes:** none

**Steps:**

1. Move struct definitions into internal/configschema [code]
2. Add validation table tests [code] (needs: Phase1.S1)
3. Run the suite and fix fallout [debug] (needs: Phase1.S2)

### Phase 2: CLI loader

Load only CLI-relevant sections in cmd/meept.

**Produces:**

- `cli-config-loader` (interface) — LoadCLIConfig entry point

**Consumes:**

- `config-schema` (schema) — shared definitions for the CLI section

**Steps:**

1. Implement LoadCLIConfig on top of config-schema [code] (needs: config-schema)
2. Wire cmd/meept flags to the loaded config [code] (needs: Phase2.S1)
3. Verify the CLI loads a sample config [bash] (needs: Phase2.S2, Phase1.S3)

### Phase 3: Daemon loader

Load only daemon-relevant sections in internal/daemon.

**Produces:**

- `daemon-config-loader` (interface) — LoadDaemonConfig entry point

**Consumes:**

- `config-schema` (schema) — shared definitions for the daemon section

**Steps:**

1. Implement LoadDaemonConfig on top of config-schema [code] (needs: config-schema)
2. Wire daemon startup to the loaded config [code] (needs: Phase3.S1)
3. Run both loader suites green [bash] (needs: Phase3.S2)

## Notes

Keep the legacy whole-file loader until both callers are migrated.
```

Walk-through: phases 2 and 3 each consume only `config-schema` from phase
1 and reference nothing from each other, so their compiled `DependsOn`
sets are both `{1}` — the frontier runs them in parallel (wave 2) with no
explicit wave syntax. `Phase2.S3` demonstrates an earlier-phase step ref;
it maps to a phase-level dependency on phase 1 at persistence (section 6)
and never couples phases 2 and 3.

### 8.3 Compile error — unknown consume plus non-empty Open Questions

The same draft as the two problems below. Line numbers are exact.

```markdown
# Plan: Billing CSV export

## Meta

- task_id: t-20260906-billing
- version: 1
- status: draft
- updated: 2026-09-06

## Goal

Export monthly billing summaries as CSV for finance review.

## Decisions

- Decision: CSV over the wire, not XLSX — Rationale: finance tooling ingests CSV directly.

## Open Questions

- Do we include credits as negative line items?

## Phases

### Phase 1: Extract billing rows

Pull the billing-period rows from the ledger store.

**Produces:**

- `billing-rows` (schema) — normalized ledger rows for one billing period

**Consumes:** none

**Steps:**

1. Query the ledger store for the billing period [code]
2. Normalize currency and tax fields [code] (needs: Phase1.S1)

### Phase 2: Render CSV

Turn normalized rows into the finance CSV.

**Produces:**

- `billing-csv` (file) — the monthly CSV export

**Consumes:**

- `payment-gateway` (interface) — charge records folded into line items

**Steps:**

1. Fold payment records into the normalized rows [code] (needs: billing-rows)
2. Write the CSV with finance header conventions [code] (needs: Phase2.S1)

## Notes

CSV encoding is UTF-8; currency is USD cents.
```

Compile output (all problems in one pass):

```text
plan compile failed: 2 problems
  line 18: Open Questions must be empty to seal (1 unresolved)
  line 49: unknown artifact "payment-gateway" consumed by phase "Render CSV": no phase in this plan produces it
```

Both problems come back together — the user answers the question, drops
or adds a producer for `payment-gateway`, and the next seal passes. Note
the CLI seal flow (section 5) would refuse at step 1 with exit code 2 on
the Open Questions check before compiling; the listing above is what
`CompileSealed` returns when called directly (or after the gate passes).

## 9. Versioning

The dialect version lives in Meta as `version: 1` — a plain integer, and
v1 is the only value this compiler accepts.

- Missing `version` key ⇒ hard error (class 6).
- Any value other than `1` ⇒ hard error (class 7): unknown major
  versions refuse to compile rather than being best-effort parsed.
- v1 has no minor or patch component. A future v2 defines its own shape
  and its own compiler acceptance; v1 documents never upgrade silently.
