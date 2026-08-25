# User-Facing Change Surfaces - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Do NOT commit.
> Do NOT use read_file on existing source — search_files/terminal cat only.

## Meta

- **Parent:** ../master.md
- **Scope:** HTTP/RPC endpoints + TUI modal + Flutter panel for pending changes and journal (list/revert), so humans review diffs without asking the agent.
- **Dependencies:** 05-write-staging.md, 06-journal.md
- **Estimated Context:** 90K
- **Concurrency Group:** D
- **Audit references:** parity-audit gap #4b (no user-facing diff surface)

## Goal

PendingChangesRegistry is currently agent-resolved only (resolve tool); no comm/rpc/tui surface exists (verified by grep during audit). This leaf exposes: GET list + POST accept/reject endpoints over the existing HTTP server; a Bubbletea modal in the TUI (list, view diff, accept/reject keys); and a Flutter panel mirroring TUI per the parity mandate. Journal read/revert exposed via same surfaces + existing CLI.

## Context

HTTP server: internal/comm/http/server.go registers routes (search for existing route registration + JSON response envelope conventions). WS events exist for chat; add change lifecycle WS events reusing transformBusEventToWS pattern ONLY if trivial — else poll from clients (simpler, acceptable). TUI: internal/tui with modals/ package; follow task-detail modal pattern. Flutter: ui/flutter_ui/lib — follow existing panels structure and meept-design skill notes (SDK gaps doc) — keep scope to a functional list+diff view+buttons.

Key files:
- internal/comm/http/server.go - route patterns/envelope
- internal/tui/modals/ - modal pattern
- internal/tui/models/ - keybinding registration
- ui/flutter_ui/lib/ - panel/service structure (PlatformService singleton note)

## Interface Contracts (From Parent)

### Exposes

```
HTTP (auth via existing middleware):
GET  /api/sessions/{sid}/pending-changes     -> [{id,file_path,diff,created_at,expires_at}]
POST /api/pending-changes/{id}/accept        -> {status:"applied"} | 409 drift message
POST /api/pending-changes/{id}/reject        -> {status:"rejected"}
GET  /api/changes/journal?session=&limit=    -> entries (no pre_image bytes)
POST /api/changes/journal/{id}/revert        -> {restored_path} | 409 drift | 400 size-capped

WS event (if bus->ws mapping extended): topic pending_change.created -> type "pending_change"
(only if mapping table edit is small; otherwise omit and rely on client refresh)

TUI:
Ctrl+D opens Pending Changes modal: j/k navigate, v view full diff pager,
a accept, r reject, esc close. Status-bar indicator when count>0.
All strings lowercase. bubblezone-clickable rows.

Flutter:
ChangesPanel under sessions area: list cards w/ syntax-plain diff body,
accept/reject buttons calling same endpoints via existing API service layer.
```

Registry/Journal consumed via their public methods only (leaves 05/06). Daemon passes registry+journal into http server constructor (extend options pattern used there).

### Consumes

Contracts 5+6 from orchestrator; existing auth middleware; existing modal/router patterns.

## Tasks

### Task 1: HTTP endpoints

**Files:** Modify internal/comm/http/server.go (+ handlers file if split convention); Test internal/comm/http/server_test.go extension using httptest pattern already present.
Failing tests per endpoint incl. auth-required case, 409 drift path (stage -> externally mutate -> accept), reject removes from registry. Standard cycle.

### Task 2: TUI modal + status indicator

**Files:** Create internal/tui/modals/pending_changes.go; wire keybinding Ctrl+D in models/ keymap + status bar count.
Failing tests: modal renders N items from fake registry; accept key calls registry method via injected interface (define small interface PendingChangeAPI in tui to avoid concrete dep); lowercase assertions on visible strings where testable.
Standard cycle. Keep modal code within existing modal size norms.

### Task 3: Flutter panel

**Files:** Create panel widget + API service methods under ui/flutter_ui/lib/features/<existing-features-dir>/ matching current architecture (locate via search_files "sessions" feature dir); register route/tab near sessions/tasks panels.
Verification: flutter analyze clean; widget tests for render + button->service-call wiring using existing mock-server fixtures if present (check test/ dir conventions); manual smoke deferred to integration phase.
Docs: extend docs/workflows page touched by leaves 05/06 OR create docs/workflows/change-review.md covering all three surfaces (preferred single page).

## Self-Verification Checklist

- [ ] Endpoints tested incl. failure paths; envelope matches sibling routes
- [ ] TUI parity: same actions as resolve tool; strings lowercase; Ctrl+D documented in keybinding docs page (find it)
- [ ] Flutter analyze green; kIsWeb-safe (no dart:io top-level)
- [ ] No direct DB access from UI layers — APIs only

**DO NOT COMMIT.**
**Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Auth middleware applied to new routes
- [ ] Diff bodies NOT included in journal list endpoint (bytes stay server-side)
- [ ] TUI↔GUI parity mandate satisfied (same capabilities both)
- [ ] AGENTS.md component table updated if new files added under internal/

Output: APPROVED or gaps.

## Notes

- Largest leaf in tree; if context pressure hits, split Flutter into its own follow-up leaf and report deviation — HTTP+TUI first.
- Do not redesign envelopes; match neighbors exactly.
