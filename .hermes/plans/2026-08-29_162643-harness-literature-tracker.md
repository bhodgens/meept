# Harness Literature Tracker Implementation Plan

> **For Hermes:** Use hierarchical-plan-execution on the tree. Do not implement from this pointer file.

**Goal:** Living catalog of harness techniques vs meept, with regressive evidence checks and optional arXiv refresh.

**Tree:** `docs/plans/harness-literature-tracker/`

- `master.md` — orchestrator
- `01-catalog.md` — JSON catalog + evidence audit
- `02-script.md` — `scripts/research-harness-lit.py`
- `03-wiring.md` — Makefile + AGENTS.md

**Already on disk (aborted implement):** `docs/research/harness-techniques.json` — leaf 01 audits it.

**Do not implement until the user says to execute the tree.**
