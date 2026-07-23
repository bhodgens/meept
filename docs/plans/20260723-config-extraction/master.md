---
name: master.md
description: Root orchestrator for config extraction plan
version: 1.0.0
author: Hermes Agent
license: MIT
metadata:
  hermes:
    tags: [configuration, hardcoded-values, defaults]
---

# Config Extraction Plan — Root Orchestrator

## Goal

Extract hardcoded configuration values into the config schema to improve deployment flexibility.

**Targets:**
- HTTP base URL: `https://localhost:8081` → configurable
- Ollama endpoint: `http://localhost:11434` → configurable  
- SQLite busy timeout: 5000ms → configurable
- Prompt truncation: 8000 chars → configurable
- TLS minimum version: TLS 1.2 → configurable

**Estimated effort**: ~1-2 hours (single leaf)

## Child Index

| ID | Document | Type | Est. Context | Dependencies | Status |
|----|----------|------|--------------|--------------|--------|
| 01 | 01-extract-hardcoded-configs.md | Leaf | ~60K | None | PENDING |

## Dispatch Protocol

1. **Dispatch implementation agent** via `delegate_task`:
   - Read leaf document
   - Include: "Do NOT commit. Write code, run tests, report results."
   
2. **Review** (main model, in-session):
   - Verify all hardcoded values extracted
   - Check config schema updated
   - Run `go build ./...`

3. **Commit** (after review passes):
   - Stage changed files
   - `git commit -m "feat(config): extract hardcoded operational parameters"`

## Completion Tracking Table

| Leaf | Status | Iter | Completed | % | Notes |
|------|--------|------|-----------|---|-------|
| 01-extract-hardcoded-configs | COMPLETE | 1 | 2026-07-23T17:30 | 100% | 5 parallel subagents + daemon wiring. All tests pass. |

## Integration Review Plan

After completion:
- [ ] `go build ./...` succeeds
- [ ] Config schema includes new fields
- [ ] Defaults match previous hardcoded values
- [ ] Documentation updated if needed

## Coding Conventions

- Add fields to appropriate config struct sections
- Provide sensible defaults matching current hardcoded values
- Use camelCase for JSON field names
- Add comments explaining each field's purpose

## Review Checklist

- [ ] All 5+ hardcoded values identified and extracted
- [ ] Config schema updated with new fields
- [ ] Defaults match previous hardcoded values
- [ ] Code uses config values instead of literals
- [ ] `go build ./...` succeeds
- [ ] No debug artifacts
