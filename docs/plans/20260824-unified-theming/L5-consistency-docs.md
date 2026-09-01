---
name: L5-consistency-docs.md
description: Cross-surface consistency test + configuration docs + AGENTS.md
version: 1.0.0
---

# L5 — Consistency tests and documentation

## Files owned

- `theme/consistency_check.go` or `theme/consistency_test.go` (Go side)
- `ui/flutter_ui/test/theme/tokens_data_parity_test.dart` (new)
- `docs/configuration/theming.md` (new)
- `AGENTS.md` (one paragraph under Configuration)

## Tasks

1. Go test `theme/tokens_test.go` extension: assert all three variants define
   exactly the frozen 18 roles; hexes match `^#[0-9A-Fa-f]{6}$`.
2. Dart parity test: read `theme/tokens.json5` from repo root via File IO in a
   setUpAll (test-only dart:io use is allowed — tests don't ship), strip
   comments, parse leniently, compare role→hex map against tokens_data.dart's
   exported map. Fail with a diff on mismatch. (This guards the single allowed
   duplication from W0.)
3. docs/configuration/theming.md: both config keys (`rendering.ui_theme`
   shared semantics; GUI local fallback key; TUI restart-applied note),
   variant list, role list, how to add a variant (edit tokens.json5 +
   regenerate tokens_data.dart copy + run parity tests).
4. AGENTS.md Configuration section: add 2-line pointer to theming doc.

## Tests

go test ./theme/... ; cd ui/flutter_ui && flutter test test/theme/

## Verify

make graphs-check not needed; go build ./... && flutter analyze.

## Constraints

Docs only + tests; no runtime code changes.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All scope items implemented at the exact owned paths
- [ ] Frozen interface contracts (master) satisfied
- [ ] Tests written and passing (see Tests/Verify above)
- [ ] No scope creep beyond this leaf's ownership
- [ ] No debug artifacts; no line-number corruption

**DO NOT COMMIT.** The orchestrator handles all git operations after
review.

## Review Checklist (For Review Agent)

- [ ] Every scope item implemented; tests present and passing
- [ ] Contracts match the master's frozen list exactly
- [ ] File ownership respected (no overlap with sibling leaves)
- [ ] Repo conventions honored (lowercase UI text, error handling, no new deps)

Output: APPROVED or list of specific gaps with file + line references.
