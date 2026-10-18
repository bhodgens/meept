# Leaf 03 — docs + matrix cell

## Depends on

Leaf 02 landed (final verb list). Read the landed tool + client code —
document what shipped, not what was planned.

## Tasks

1. `docs/workflows/hermes-interop.md`: problem statement, Hermes-side
   setup (API_SERVER_ENABLED/API_SERVER_KEY env + gateway run), meept-side
   config ([hermes] section, env key), verb reference table, error
   semantics (429/409/401), security note, non-goals. Link in mkdocs.yml
   Workflows nav.
2. One-line cell update in docs/feature-comparison-matrix.md Hermes
   interop context (only if a natural row exists — do not restructure the
   table).
3. `go build` both binaries (docs-only otherwise).

## Constraints

- Explicit paths; commit 'docs: hermes interop workflow (hermes-interop leaf 03)'.

## Report back

Doc path, mkdocs nav line, files.
