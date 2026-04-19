# Plan: ClawSkills -- Independent ClawHub Skill System

## Summary

Add a fully independent `clawskills` module (`src/meept/clawskills/`) that manages
skills from the ClawHub registry (clawhub.ai). This is completely separate from the
existing local skills system (`src/meept/skills/`). The CLI gains `meept clawskills`
subcommands for search, install, update, list, inspect, info, and remove.

All third-party clawskills are treated as **untrusted** (HIGH risk, STRICT sanitization,
restricted tool access) by default.

---

## New Module: `src/meept/clawskills/`

```
src/meept/clawskills/
  __init__.py          # Module docstring + public re-exports
  models.py            # Data models (OriginMetadata, LockFileEntry, LockFile, etc.)
  client.py            # Async HTTP client for ClawHub API (search, download, etc.)
  installer.py         # Download, validate, extract, lock-file management
  index.py             # Local index of installed clawskills (daemon-side)
  security.py          # Security adapter (STRICT sanitization + tool filtering)
  cli.py               # CLI subcommand registration and handlers
```

---

## Files Modified (existing)

| File | Change |
|------|--------|
| `cli/__main__.py` | Add early `clawskills` subcommand detection before TUI launch |
| `src/meept/models/config_schema.py` | Add `ClawSkillsConfig` class + `clawskills` field on `MeeptSettings` |
| `config/meept.toml` | Add `[clawskills]` section |
| `src/meept/core/daemon.py` | Load `ClawSkillIndex` alongside `SkillIndex` at startup |

No changes to: `skills/`, `WorkerFactory`, `Orchestrator`, `FrontAgent`, `SkillRegistry`.

---

## Security Architecture

| Layer | Mechanism | Location |
|-------|-----------|----------|
| Archive validation | Path traversal, forbidden files, extension whitelist, size caps | `installer.py` |
| Instruction sanitization | `InputSanitizer` at STRICT level | `security.py` |
| Tool access restriction | Blocked tool list + pattern matching | `security.py` |
| Risk level enforcement | Always HIGH for clawskills | `index.py` |
| Namespace isolation | `claw:` prefix prevents shadowing local skills | `index.py` |
| Runtime permission gate | Existing `SecurityEngine` confirmation for HIGH risk | `daemon.py` (existing) |
| Output monitoring | Existing `OutputMonitor` credential redaction | agent pipeline (existing) |
| Integrity tracking | SHA-256 of ZIP stored in `.origin.json` + `.lock.json` | `installer.py` |
| Slug blocklist | `blocked_slugs` in config | `daemon.py` integration |

---

## CLI Commands

| Command | Description |
|---------|-------------|
| `meept clawskills search <query>` | Search ClawHub, display results table |
| `meept clawskills install <slug> [--version V]` | Download + install |
| `meept clawskills update [slug] [--all]` | Update one or all |
| `meept clawskills list` | List installed (from lock file) |
| `meept clawskills inspect <slug>` | View remote skill detail |
| `meept clawskills info <slug>` | View local installed skill detail |
| `meept clawskills remove <slug>` | Remove installed skill |

---

## Test Plan

**Test directory**: `tests/test_clawskills/`

| File | Coverage |
|------|----------|
| `test_models.py` | Model defaults (risk_level=high), serialization round-trips |
| `test_client.py` | API methods with `httpx.MockTransport`, rate limiter, cache, size limits |
| `test_installer.py` | Install flow, archive rejection (path traversal, .env, executables, bad extensions), lock file, remove |
| `test_index.py` | Scan, `claw:` prefix, risk/iteration overrides, corrupt origin handling |
| `test_security.py` | STRICT sanitization, blocked tools, pattern-based tool filtering |
| `test_cli.py` | Subcommand parsing, output formatting |
| `conftest.py` | Shared fixtures (temp dirs, sample ZIPs, mock transport) |

---

## Dependencies

No new dependencies. `httpx>=0.27` and `pyyaml>=6.0` already in `pyproject.toml`.
