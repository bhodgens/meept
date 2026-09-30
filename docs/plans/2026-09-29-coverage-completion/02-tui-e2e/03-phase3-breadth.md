# Leaf 03 — TUI Phase 3: breadth

**Objective:** Extend goldens and flows to the remaining surfaces.

**Files:**
- Modify: `internal/tui/golden_test.go` — goldens for queue view,
  memory view, plans view, search view.
- Modify: `internal/tui/*_test.go` (modal key-flow tests) — confirmation
  modal, rename modal, pending-changes modal, project prompt modal:
  open → interact → confirm/cancel, assert state.
- Modify: `internal/tui/config_test.go` — keybinding overrides via
  client.json5 fixtures (internal/tui/config.go): an override remaps a
  default key and the model responds to the new binding.
- Goldens only (no behavioral flow): sidebar, vim mode, viz, prompts.

**Invariants:** same as Phase 1 (fixed WindowSizeMsg, no wall-clock,
lowercase goldens). Modal focus-order assertions matter most — that is
the failure mode unit tests cannot see (tui-e2e-plan.md section 1.2).

**Verify:**
```
go test ./internal/tui/...      # full package green
make e2e-fast                   # full tier green (no cross-suite breakage)
```

**Commit:** `test(tui): breadth coverage — remaining views, modals, keybinding overrides`

**Self-check:** `grep -rn "t.Skip" internal/tui/ e2e/suites/tui-flows/`
returns zero behavioral skips (env-conditional skips allowed).
