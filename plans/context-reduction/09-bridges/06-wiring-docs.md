# B06 — Bridges Wiring: CLI/TUI Visibility + e2e + Docs (P10)

**Proposal:** P10 (branch 09). **Depends on:** B04 (B05 in progress is fine).
**Files (3+):**
- Modify: `cmd/meept/` — bridges listing in the memory command surface (locate
  the existing `meept memory`/memory-listing command; add bridge visibility:
  count + sample listing, lowercase UI text per repo convention)
- Modify: `e2e/manifest.json` + a new suite under `e2e/suites/` (path→suite
  mapping for internal/memory bridge code — MANDATORY per repo e2e policy when
  adding files under a new package area)
- Modify: `docs/workflows/memory.md` (bridge generation, k_b, invalidation, config keys)

**Problem:** Repo wiring rule: implementations without user-facing interfaces
are incomplete. Bridges need CLI/TUI visibility, an e2e manifest entry, and
workflow docs — in the same change series, not deferred.

**Contracts (root #7):** UI text lowercase; TUI/GUI parity note: TUI gets the
listing; GUI parity follows later (document the deviation in docs/workflows/memory.md
per repo rule). Config keys documented match schema.go exactly.

### Tasks

**Task 1 — CLI:** extend the memory listing command with bridge rows (marked
`[bridge]`, showing verified flag + evidence count) and a `--bridges` filter.
Match existing command output patterns (search_files cmd/meept for the memory
command). Keep output table-safe if the TUI table helper is shared.

**Task 2 — e2e:** add suite `e2e/suites/bridges/` covering: enable → generate
against a seeded memory store → recall includes ≤ k_b bridges → invalidate a
source memory → bridge gone. Register in `e2e/manifest.json` with the correct
path_map for the new code. Verify with `make e2e-fast-area AREA=bridges`.

**Task 3 — docs:** docs/workflows/memory.md section: what bridges are (2
sentences, plain language), config keys + defaults table, invalidation
guarantees, how to read the B05 eval, explicit note that TUI-only visibility is
a documented deviation pending GUI parity.

**Task 4 — verify:** `make e2e-fast-area AREA=bridges` green; `go build ./...`;
docs cross-references resolve (no dead relative links).

**Task 5 — report.** Do NOT commit.

## Self-Verification Checklist
- [ ] manifest path_map entry present (pre-commit e2e gate would block otherwise)
- [ ] Docs config table matches schema.go defaults exactly
- [ ] Lowercase UI text; no em-dash in user-facing copy

## Review Checklist (for orchestrator)
- [ ] GUI parity deviation documented, not silent
- [ ] e2e suite runs hermetic (no live model dependency — seed the store; mock the gen)
