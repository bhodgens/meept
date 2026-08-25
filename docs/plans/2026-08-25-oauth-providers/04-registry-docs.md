# OAuth Provider Docs + End-to-End Verification - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md (root leaf)
- **Scope:** Documentation for the three new OAuth providers + full-tree verification (build, tests, analyzers, graphs).
- **Dependencies:** all six implementation leaves REVIEWED
- **Estimated Context:** 35K
- **Concurrency Group:** WAVE 4
- **Audit references:** none

## Goal

Every new provider is documented in the required locations (per AGENTS.md Feature Documentation Requirements), AGENTS.md is reviewed for staleness, and the whole tree passes build + tests + lint + analyzers + graphs fresh.

## Context

AGENTS.md maps docs to code: `internal/<pkg>/` → `docs/workflows/<pkg>.md`. OAuth currently has no dedicated workflow doc (grep `docs/workflows/` for oauth — the CLI table in `docs/reference/cli.md` mentions `config oauth`). The LLM provider surface is documented in `docs/workflows/llm-management.md` if present — verify with `ls docs/workflows/ | grep llm`.

Key files:
- `docs/reference/cli.md` — `config oauth` command table (~line 439)
- `docs/workflows/` — feature docs directory
- `AGENTS.md` — package table + invariants review

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
docs/workflows/oauth-providers.md — new workflow doc covering:
  - the three flows (RFC 8628 + form encoding, Codex non-standard, PKCE paste)
  - provider table: xai-oauth, openai-codex, anthropic-sub (+ existing three)
  - CLI usage examples for connect/status/disconnect
  - refresh behavior + token storage location
Updated docs/reference/cli.md oauth section (provider list)
AGENTS.md reviewed; updated ONLY if something is stale
```

### What This Leaf Consumes

All implementation leaves' final state (file paths, provider IDs, flow kinds).

## Tasks

### Task 1: Workflow doc

**Objective:** `docs/workflows/oauth-providers.md` exists and is accurate.

**Files:**
- Create: `docs/workflows/oauth-providers.md`

Read the implemented `internal/auth/providers.go` registry + flow files (via terminal cat), then write the doc: overview, provider table (ID, flow kind, billing source, endpoints summary — no client IDs needed in docs), CLI examples, token storage/refresh notes, troubleshooting (429 rate limits on OpenAI/Anthropic login, placeholder client IDs for github/google). Add it to `docs/workflows/index.md` if that file lists entries (check).

### Task 2: Reference docs

**Objective:** `docs/reference/cli.md` oauth section lists all six providers.

**Files:**
- Modify: `docs/reference/cli.md`

Find the `config oauth` rows; extend with the provider list or point to the workflow doc. Keep style consistent with neighboring rows.

### Task 3: AGENTS.md review

**Objective:** AGENTS.md reflects any changes.

**Files:**
- Modify: `AGENTS.md` (only if stale)

Check: package table (no new packages — `internal/auth` and `internal/llm` already listed), build commands (none new), invariants (no new bus topics/session semantics — expected NO changes), analyzers (none new). If nothing is stale, do not edit; report "reviewed, no changes needed."

### Task 4: Full verification

**Objective:** Tree-wide green.

Run, in order, and capture results:

1. `go build ./...`
2. `go test ./internal/auth/ ./internal/llm/ ./cmd/... -count=1`
3. `go vet ./internal/auth/ ./internal/llm/ ./cmd/meept/`
4. `gofmt -l internal/auth internal/llm cmd/meept` (expect empty)
5. `go run ./tools/analyzers/predid/... ./internal/auth/ ./internal/llm/ 2>&1 | tail -5` (expect no findings)
6. `make graphs && git status --short docs/generated/` (regenerate; changed files are expected commit content)
7. `grep -rcE '^\s+[0-9]+\|' --include='*.go' internal/auth internal/llm cmd/meept` (expect zero)

Report each command's pass/fail. Any failure → fix if trivial (typo/format), else report as a gap for re-dispatch to the owning leaf.

## Self-Verification Checklist

- [ ] Workflow doc created, accurate against final code, cross-references resolve (cited paths exist)
- [ ] cli.md updated consistently
- [ ] AGENTS.md reviewed (changed or explicitly no-change)
- [ ] All 7 verification commands pass (or gaps reported)
- [ ] No line-number corruption in any edited file

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Doc claims match code (spot-check endpoints/provider IDs against providers.go)
- [ ] Cross-references resolve
- [ ] Verification outputs real (not asserted from memory)
- [ ] UI text in doc examples lowercase

Output: APPROVED or list of specific gaps.

## Notes

- This is a docs + verification leaf; no code changes expected beyond fixes surfaced by Task 4.
- Manual live-login smoke (needs real ChatGPT/Claude/Grok accounts) is OUT of scope for this leaf; the workflow doc's troubleshooting section documents what a user does.
