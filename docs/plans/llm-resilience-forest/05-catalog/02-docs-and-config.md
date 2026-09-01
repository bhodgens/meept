# Catalog Config + Docs Consolidation - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Configuration reference + workflow docs for context
  discovery and LM Studio; precedence documentation; AGENTS.md review.
- **Dependencies:** 01-context-discovery.md, 03-lmstudio-provider.md (both merged)
- **Estimated Context:** 40K
- **Concurrency Group:** C
- **Decision references:** D12, D13

## Goal

Everything a user needs to USE the two features, in the docs:

1. **Context discovery** — config knobs (enabled/interval/override),
   the three-line precedence rule (master Contract 3), which providers
   expose context length and which never will (OpenAI, Anthropic —
   D13), and how to verify it worked (log lines to look for).
2. **LM Studio** — setup walkthrough (serve → point meept at the base
   URL → models appear), models.json5 examples, context-length note.
3. **AGENTS.md** — same-commit review: providers/context discovery
   touch no package table entries, but the Configuration section's
   models/llm line may need the discovery mention; add it if so.

This leaf is docs + config-defaults verification. It writes NO runtime
code. Where a doc claim is checkable (config key names, defaults), the
leaf VERIFIES each against schema.go before writing it.

## Context

Key files (verify, do not trust leaves 01/03's plans blindly — the
SHIPPED state is authoritative):
- `internal/config/schema.go` — the llm block's ContextDiscoveryConfig
  as leaf 01 actually landed it (names, tags, defaults).
- `internal/llm/provider_registry.go` — lmstudio entry as leaf 03
  landed it (BaseURL, supports list).
- `internal/llm/context_discovery.go` — fetcher provider list (which
  providers really have fetchers).
- `docs/configuration/llm.md` — the providers/models reference (the
  "### Ollama (Local)" section at llm.md:280 is the section-shape
  model; there is no separate models.md/providers.md) — this leaf
  extends it; `docs/workflows/llm-management.md` (the LLM workflow page —
  there is no llm.md) for the discovery workflow note.

## Interface Contracts (From Parent)

### What This Leaf Exposes

Exactly master Contract 3 (documentation of the precedence rule) plus
the docs pages. Contract:

```
Precedence (verbatim in docs):
1. models.json5 explicit context_limit (authoritative when set)
2. discovered value (when allow_context_override OR catalog value is 0)
3. catalog default
```

### What This Leaf Consumes

The shipped code from leaves 01 and 03 (read it; correct the docs to
match, never the reverse — if docs reveal a code bug, STOP and report
it in Deviations instead of documenting around it).

## Tasks

### Task 1: Configuration reference

**Objective:** Every knob documented with verified names/defaults.

**Files:**
- Modify: `docs/configuration/` — the llm/models config page:
  `context_discovery` block (verify each key against schema.go:
  enabled, interval, allow_context_override — use the ACTUAL json5
  names), defaults, an example snippet, the precedence rule verbatim,
  the provider exposure table (ollama/openrouter/llama.cpp yes;
  openai/anthropic never), log-line verification hint.
- Verify: every key name in the docs matches schema.go EXACTLY
  (grep each one; a mismatch = fix the doc, note nothing).

**Step 1:** Read the landed schema. **Step 2:** Write the section.
**Step 3:** Cross-check each documented key with a grep; list the
greps in the leaf report.

### Task 2: LM Studio provider docs

**Objective:** Setup walkthrough.

**Files:**
- Modify: `docs/configuration/llm.md` — LM Studio section in the
  providers reference (no separate providers page exists),
  mirroring Ollama's (leaf 03 may have started it; extend/verify, do
  not duplicate): base URL + override, discovery behavior, models.json5
  example, CapTools nuance (endpoint advertises; model must support),
  context-length note.

**Verify:** the example parses as valid JSON5 (spot-check the syntax
by eye against existing examples on the page).

### Task 3: Workflow + AGENTS.md

**Files:**
- Modify: `docs/workflows/llm-management.md` — one short section: what discovery
  runs, when (interval), what it changes, what it never changes
  (explicit config), where LM Studio models come from.
- Modify: `AGENTS.md` — review per the repo rule: Configuration
  section (add discovery mention if models.json5 docs change),
  Critical Invariants (none expected — discovery is inert by
  default; state the check).

**Verify:** AGENTS.md diff is either empty or minimal + justified.

## Self-Verification Checklist

- [ ] Every documented config key grep-verified against schema.go
- [ ] Precedence rule verbatim (Contract 3)
- [ ] No docs/code contradictions (any found → Deviations + stop)
- [ ] LM Studio section mirrors Ollama's structure
- [ ] AGENTS.md reviewed; diff minimal or empty

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented
- [ ] Key names verified (the report lists the greps)
- [ ] No runtime code changes in this leaf's diff
- [ ] Docs internally consistent (no orphan references to removed keys)

Output: APPROVED or specific gaps with file + line references.

## Notes

- Users on remote LM Studio hosts (non-localhost) need the base-URL
  override called out explicitly — the default binds localhost only.
- If leaf 01 landed an `lmstudio` fetcher registration BEFORE leaf 03's
  provider existed (ordering), verify the registration is live in the
  shipped wiring and note which leaf owns it — one owner, no
  duplicates.
