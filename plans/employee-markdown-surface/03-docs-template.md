# 03 — Docs + Sample EMPLOYEE.md Template

## Dispatch Instruction

Do NOT commit. Do NOT run git add. Write the documentation and an example `EMPLOYEE.md` template file. Run whatever static checks apply. The orchestrator reviews and commits.

## Objective

Document the markdown authoring surface for employees and ship one reference `EMPLOYEE.md` template so operators can hand-author employee config like they hand-author `AGENT.md` agents.

## Files

- Modify: `docs/workflows/employees.md` (add a "Markdown authoring" section)
- Create: `config/employees/example-employee.md` (reference template; disabled `enabled:false` like the json5 templates)

## Content required

### docs/workflows/employees.md — new section (place near the top-level "Configuration" / CLI create docs, ~line 169 and 348 in the file)

Add a subsection: **"Authoring employees as markdown"**. It must state clearly:

1. Employees have two surfaces: a human-authored **configuration** in markdown (this document describes the format) and **runtime state** in SQLite (constitution, goals, audit findings, messages — unchanged, auto-managed by the daemon).
2. The markdown file is an *input* to `meept agents create <file>` / `update` / `validate`; it is not discovered automatically and the daemon does not watch it.
3. Format: YAML frontmatter (same style as `AGENT.md` agent definitions) + a markdown body that becomes the employee's `prompt`.
4. Frontmatter keys table (copy the exact table from master.md's "Interface Contract", including the aliases and `constraints` sub-keys).
5. Note that the constitution block is passed through and validated by `agents create` / `agents validate` exactly as the JSON5 path does — markdown does not bypass validation.
6. One worked mini-example (can mirror `config/employees/example-employee.md`), with the `meept agents validate` and `meept agents create` commands.
7. A one-line note that `.json5` definitions remain fully supported.

### config/employees/example-employee.md

A commented reference template modeled on `config/employees/secretary.json5` (read it first) but in the markdown format. It MUST:
- Ship with `enabled: false` and comment explaining hiring is an operator opt-in via `meept agents create config/employees/example-employee.md`.
- Include every frontmatter key the parser supports, with inline `#` comments where YAML comments are legal (top-level only; YAML `#` after a key is fine).
- Include a `constitution` block with purpose, role, autonomy_tier, escalates_to, constraints (risk_ceiling, max_tokens_per_turn), amendment_policy.
- Match the frozen contract exactly (key names, `timeout_seconds`, nested `constraints`, `memory_scope` vocabulary).
- Be valid: `meept agents validate config/employees/example-employee.md` must pass (run this to confirm).

## Self-Verification

- [] `meept agents validate config/employees/example-employee.md` exits 0 (build the CLI first: `go build -o /tmp/meept .` from cmd/meept or `make build`; needs no daemon).
- [] docs/workflows/employees.md has no broken internal anchor (grep the links you add).
- [] The template is consistent with the master.md contract table.

## Review Checklist

- Docs are accurate to the implemented parser; no invented fields.
- Template passes validation (the authoritative check).
- `.json5` path is documented as still supported.
- Repository copy conventions: hyphens, not em-dashes; lowercase UI text; plain-language definitions of terms.
