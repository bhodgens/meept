# Flutter GUI Gap Fixes — Design Spec

**Date:** 2026-06-27
**Status:** Draft (pending user review)
**Scope:** `ui/flutter_ui/`, `internal/session/`, `internal/comm/http/`, `internal/tui/`, `pkg/models/` (read-only reference)

## Context

Seven gaps reported in the Flutter GUI, with one of them (archive semantics) requiring cross-surface parity with the TUI. This spec covers all seven as a single implementation plan since several are tightly coupled (the new status bar depends on the new verbosity wiring; the archive feature requires model + storage + API + UI changes across both surfaces).

Findings from exploration (file:line evidence):

- **No bottom status bar exists in Flutter today.** Only a top-right `_ConnectionDot` (`ui/flutter_ui/lib/features/home/home_screen.dart:94-182`).
- **TUI status bar** renders a single line at `internal/tui/app.go:2236-2289`: connection dot, branch indicator, context-sensitive keybinding hints, project indicator, cwd path, verbosity, transient status message.
- **TUI Ctrl+X** opens a 12-item command palette modal (`internal/tui/modal.go:188`). **Flutter Cmd/Ctrl+X** is wired as a two-keystroke "leader key" (`ui/flutter_ui/lib/core/shortcuts.dart:184-209`) with follow-ups `s/p/b/c/?` — completely different behavior. This is the root of "Ctrl-X doesn't work in GUI."
- **Flutter agents tab** uses `SliverGridDelegateWithFixedCrossAxisCount(crossAxisCount: 2, childAspectRatio: 1.5)` (`ui/flutter_ui/lib/features/agents/agents_tab.dart:98-104`). Fixed two columns regardless of window width.
- **Double-click session handler** exists at `ui/flutter_ui/lib/features/sessions/sessions_list.dart:196-199` but only calls `context.go('/')`. The `_HomeScreenState._selectedTab` field is what `TabContent` switches on (`tab_content.dart:24-37`), so the chat tab never becomes visually active. Root cause identified.
- **Trash icon = hard delete** via `DELETE /api/v1/sessions/$id` (`ui/flutter_ui/lib/services/session_notifier.dart:80-92`, `sdk_client.dart:466-468`). No archive concept anywhere.
- **No `archived` column** in `sessions` SQLite table (`internal/session/store_sqlite.go:84-92`, migration list at lines 114-141). No archive field on Go `Session` (`internal/session/session.go:58-82`), TUI `Session` (`internal/tui/types/types.go:169-186`), or Flutter `Session` (`api_models.dart:448-486`).
- **Verbosity is TUI-only today.** Daemon-side `progress_synthesizer.go` assigns a `Tier` (0/1/2) to every synthesized agent event before sending; TUI filters client-side (`app.go:1347`: `if tier <= a.verbosity`). Flutter receives the `tier` field on each event (`api_models.dart:133`) but never reads or filters on it.
- **TUI sessions view claims `d` deletes** (`app.go:717` status message) but the key is dead — `deleteSession` (`app.go:1926-1940`) is never called from the sessions key handler.

## Non-Goals

- New tab types, new HTTP endpoints unrelated to archive, refactoring `ChatNotifier` internals beyond the minimum needed for Gap 6, redesigning the cyberpunk theme, mobile/responsive layout work.
- Adding verbosity to the daemon config — verbosity stays a client-side filter (mirrors TUI behavior exactly).
- Building a sidebar concept in Flutter just to mirror the TUI command palette's "toggle sidebar" item.

## Design

### Gap 1 — Bottom Status Bar (Flutter) + Gap 1b — Verbosity Wiring

**Goal:** parity with the TUI bottom line, including a working verbosity cycle.

#### Verbosity wiring (Gap 1b — do first, the status bar depends on it)

New file `ui/flutter_ui/lib/providers/verbosity_provider.dart`:

- `verbosityProvider = StateProvider<int>((ref) => defaultFromClientConfig())` — 0=quiet, 1=normal, 2=verbose. Default loaded from client config (`~/.meept/client.json5` `chat.verbosity`, default `normal`).
- On change, persist to client config via `SdkClient` (new method `setClientConfig(path, value)` calling `PUT /api/v1/config/client` — see "Config endpoint" below if it doesn't exist).
- Hotkey **Ctrl+V on all platforms** cycles 0→1→2→0, matching TUI Ctrl+V (`app.go:725-728`). Per the project parity rule, TUI and GUI features stay in sync — using Ctrl+V on mac (instead of Cmd+V) keeps the bindings identical across surfaces and avoids conflict with system paste. Register in `core/shortcuts.dart` alongside the existing Cmd+K/Cmd+F handlers.

Filter agent events by tier. Today `ChatNotifier` receives events and forwards all to the UI. Insert a filter at the point where synthesized agent events arrive: `if (event.tier <= ref.read(verbosityProvider)) emit(event)`. This mirrors `app.go:1347` exactly. The filter belongs in a new `AgentEventFilter` helper rather than inline in `ChatNotifier` so it can be unit-tested.

**Config persistence:** verify whether `PUT /api/v1/config/client` already exists during implementation. **Preferred path:** if it exists or is trivial to add (small handler in `internal/comm/http/server.go` + RPC method), use it. **Fallback path:** local-only persistence — write `~/.meept/client.json5` directly from Flutter via `path_provider` + `dart:io`. The fallback is acceptable because the TUI also stores verbosity in client config that it reads on startup; the daemon doesn't read it. Pick one path during implementation phase 1 based on what the existing codebase offers; don't build both.

#### Status bar widget

New file `ui/flutter_ui/lib/widgets/status_bar.dart` — a `ConsumerWidget` rendering a single-line `Container` pinned at the bottom of `HomeScreen`'s `Column` (insert after `Expanded(child: _buildTabContent())` at `home_screen.dart:436-438`).

Left→right, separated by ` · ` (each part conditionally rendered):

1. **Connection dot + status text.** Reuses existing `connectionStateProvider`, `connectionStatusProvider`, `connectionColorProvider`. Renders `● connected` / `● disconnected` / `● connecting…`. Source: same providers as `_ConnectionDot`.
2. **Active session name.** From `activeSessionProvider`. Suppress when name is null, empty, or `'default'`. Prefix `session: `. Mirrors TUI branch indicator (`app.go:2261-2263`).
3. **Context-sensitive keybinding hints.** Static string per `_selectedTab`, mirrored after TUI's `getQuickActions()` (`app.go:2292-2389`). Concise form:
   - chat: `⌘k focus · / cmd · ⌘f find · ^v verbosity`
   - sessions: `dbl-click open · ⌫ archive`
   - plans / tasks / agents: `j/k navigate · enter select`
4. **Project indicator.** From a new `currentProjectProvider` reading project sync state. Renders `[projectname branch*]` (git, where `*` means dirty) or `[local:/path]` (local) or omits when no project. Mirrors TUI `renderProjectIndicator()` (`app.go:2395-2422`) field-for-field.

   **Implementation:** the backend plumbing already exists — Flutter's `SdkClient.listProjects()` (`sdk_client.dart:916`) calls `GET /api/v1/projects` (handler at `internal/comm/http/api_handlers.go:2399`). The TUI uses `rpc.ProjectStatus(id)` to get dirty+branch for git projects; an HTTP equivalent (`GET /api/v1/projects/{id}/status`) needs verification during implementation. If it doesn't exist, fall back to the fields already on the project list response (mode, name, status) without dirty/branch detail — dirty/branch then becomes a follow-up.
   - New `currentProjectProvider` (StateNotifier) on app connect: fetch `listProjects()`, find the entry with `status == 'active'`, expose `{name, mode, branch, dirty}`. Re-fetch on project switch (route change to `/tools/branches`).
   - The status bar watches `currentProjectProvider` and renders the indicator string using the same logic as TUI `renderProjectIndicator()`: name (truncated to 16 chars), mode-aware formatting, dirty `*` for git.
5. **Verbosity.** From `verbosityProvider`. Renders `verbosity: normal` (or `quiet`/`verbose`). Always shown. Mirrors TUI (`app.go:2279`).

**Transient status messages:** new `statusMessageProvider` (a `StateProvider<String?>` with auto-clear via a 2.5s timer). When non-null, the status bar renders only that message. Wire key user actions to set it (e.g., "session archived", "verbosity: verbose", "session created"). This is additive — not every snackbar needs to migrate, just the high-signal ones.

**Styling:** height 22px, background `CyberpunkColors.blackTransparent(0.7)` (matches the existing toolbar at `home_screen.dart:411`), single-line `Text` with `SourceCodePro`, color `CyberpunkColors.midGray` (matches TUI `StatusBar` style at `internal/tui/styles.go:165-175`). Border-top `1px CyberpunkColors.midGray` for separation.

### Gap 2 — Cmd/Ctrl+X Command Palette

**Goal:** Cmd+X (mac) / Ctrl+X (other) opens a command palette modal matching the TUI's 12 items, replacing the current leader-key behavior.

#### Leader key removal

In `ui/flutter_ui/lib/core/shortcuts.dart`:
- Remove the leader-mode state machine from `LeaderKeyController` (`_waiting`, `_enterLeaderMode`, `_exitLeaderMode`, `handleLeaderSequence`, the 500ms timer, the `isWaiting` indicator in `home_screen.dart:442-460`).
- Keep `LeaderKeyController` as a thin dispatcher: it still owns the `onTabSelected`/`onFocusInput`/`onFind`/`onInSessionFind`/`onGlobalSearch`/`onBranches`/`onShowHelp`/`onNavigate` callbacks (used by direct shortcuts like Cmd+K, Cmd+F). Just drop the two-keystroke chord.
- Remove the `isWaiting`-positioned banner in `home_screen.dart:442-460`.

#### Command palette modal

New file `ui/flutter_ui/lib/widgets/command_palette.dart` — a `Dialog` opened via `showDialog`. Triggered from `AppShortcuts._handleKeyEvent` when the leader trigger (`_isLeaderTrigger` at `shortcuts.dart:244-251`) fires: instead of entering leader mode, call `onShowCommandPalette?.call()` which `HomeScreen` wires to `showDialog`.

Items (matching TUI `modal.go:192-205`, adapted to Flutter surfaces):

| Label | Description | Flutter action |
|---|---|---|
| chat | switch to chat view | `_selectedTab = HomeTab.chat` + `context.go('/')` |
| sessions | switch to sessions view | `_selectedTab = HomeTab.sessions` + `context.go('/sessions')` |
| plans | switch to plans view | `_selectedTab = HomeTab.plans` + `context.go('/plans')` |
| tasks | switch to tasks view | `_selectedTab = HomeTab.tasks` + `context.go('/tasks')` |
| agents | switch to employees view | `_selectedTab = HomeTab.agents` + `context.go('/agents')` |
| find… | search sessions and tasks | `context.go('/tools/search')` |
| new session | create a new session | trigger `SessionNotifier.createSession` flow (reuse `_showCreateSessionDialog` from `sessions_list.dart:27-77`) |
| edit description | edit session description | open rename dialog for `activeSessionProvider` |
| projects | manage projects | `context.go('/tools/branches')` (closest existing Flutter surface) |

**Omitted TUI items** (no Flutter equivalent or no demand stated):
- "queue view", "memory view" — no Flutter route exists today. Omit; can be added when those views land.
- "toggle sidebar" — Flutter has no sidebar. Omit.

**Behavior:** arrow keys move selection, enter activates, escape closes, click activates. Filter-by-typing is **not** in scope (TUI doesn't have it either). Each row shows keybinding hint (left, muted) + label (mid, primary) + description (right, lightGray), mirroring TUI palette layout.

### Gap 3 — Agent Tile Sizing

**Goal:** tiles half as tall, fixed ~150px wide, more per row as the window grows.

Change `ui/flutter_ui/lib/features/agents/agents_tab.dart:98-104`:
- Replace `SliverGridDelegateWithFixedCrossAxisCount(crossAxisCount: 2, …)` with `SliverGridDelegateWithMaxCrossAxisExtent(maxCrossAxisExtent: 150, crossAxisSpacing: 8, mainAxisSpacing: 8, childAspectRatio: 2.6)`.
  - `childAspectRatio: 2.6` ≈ 150 wide / ~58 tall.
- Inner card padding `EdgeInsets.all(16)` → `EdgeInsets.symmetric(horizontal: 10, vertical: 8)`.
- Drop the separate `agent.id` line below the name (`agents_tab.dart:165-172`) — it's redundant with the name row. Keep icon (24→20px) + name only, on a single line.

Result: ~58px tall tiles (down from ~96px at default width), ~150px wide, count-per-row scales with window width. Two columns at narrow widths, more at wider widths.

### Gap 4 — Double-Click Activates Chat Tab

**Goal:** double-clicking a session in the sessions tab loads it in chat AND switches to the chat tab.

**Root cause:** `sessions_list.dart:196-199` calls `context.go('/')` but never updates `_HomeScreenState._selectedTab`. `TabContent` switches on `_selectedTab`, so the chat tab never becomes visually active even though the URL changes.

**Fix:**
1. Add a callback parameter to `SessionsList` (or use a new `tabActivationProvider`).
2. `HomeScreen` wires the callback to `setState(() => _selectedTab = HomeTab.chat)` followed by `context.go('/')`.
3. The double-tap handler invokes the callback after setting `activeSessionProvider`.

Prefer the `tabActivationProvider` approach (`StateProvider<HomeTab?>`) over a callback because it composes cleanly with other tabs that may need to trigger tab switches (e.g., clicking a task's "open session" affordance). `HomeScreen` watches it and updates `_selectedTab` + clears it back to null.

### Gap 5 — Click Latency (All Tabs)

**Goal:** optimistic state + async load + cached detail, applied uniformly to sessions, agents, plans, tasks.

**Pattern:** shared `CachedDetailProvider<T>` family.

New file `ui/flutter_ui/lib/providers/cached_detail.dart`:

```dart
// Per-tab family provider keyed by id. State = (detail | loading | error).
// detailCacheProvider<T>(id) returns AsyncValue<T?>.
// First read triggers fetch; subsequent reads return cache.
// prefetchProvider<T>(id) warms the cache without blocking on result.
```

Concrete instantiations (one per tab):
- `sessionDetailProvider(id)` — fetches via `SdkClient.getSession(id)` (add method if missing).
- `agentDetailProvider(id)` — fetches via `SdkClient.getAgent(id)` (existing `agents show` API).
- `planDetailProvider(id)`, `taskDetailProvider(id)` — similar.

**Behavior on click:**
1. **Immediate:** set the tab's active-id provider (`activeSessionProvider` / `activeAgentProvider` / etc.). UI state changes synchronously — the detail pane / chat view swaps to the new id.
2. **Placeholder:** the detail widget watches `<tab>DetailProvider(id)`. While `isLoading`, renders a `loading…` placeholder (skeleton or just the text "loading…"). No network-wait visible to the user.
3. **Async fetch:** the provider's create function fetches and caches. On completion, the detail widget rebuilds with real data.
4. **Cache hit:** subsequent clicks on the same id return synchronously from cache.

**Prefetch (decision (b) — lazy):** no proactive prefetch on app connect. Prefetch happens lazily on first tab visit: when the user navigates to a tab for the first time in a session, that tab's list-level notifier prefetches detail for the first item in the list (the most-recently-modified one, since lists are sorted by activity). Subsequent clicks within the tab hit cache for that item.

**List-level prefetch stays as-is:** the list of sessions/agents/plans/tasks is already loaded on connect (`home_screen.dart:267-274`) and on first tab visit (`sessions_list.dart:20-25`, `agents_tab.dart:30-35`). Only detail fetches are at risk of latency.

**Edge case — active session on app start (exception to "lazy"):** `activeSessionProvider` defaults to null which resolves to `'default'` in `tab_content.dart:27`. The chat tab is the default landing tab, so the `'default'` session's detail is needed immediately on app open. Warm `sessionDetailProvider('default')` on connect. This is the **only** non-lazy prefetch; it's justified because the chat tab is always the first tab the user sees.

### Gap 6 — Grey Transcript on Session Recall

**Goal:** fix the partial grey rendering that occurs when recalling an existing session.

**Reported symptom:** "only when recalling existent sessions, not while chatting in an existing session. Partial — I can see what I sent."

**Hypothesis:** stream-subscription handoff race in `ChatNotifier`. When `activeSessionProvider` changes:
- Local user messages render immediately (they're in the notifier's local action history).
- Assistant responses need either (a) a fresh fetch of session history, or (b) a stream-subscription swap that hasn't emitted yet — so the list briefly renders user-only with assistant bubbles empty/grey.

**Plan: investigate root cause first, then fix at source.** Not a styling patch.

Investigation steps (to be performed during implementation, not as part of this spec):
1. Trace `activeSessionProvider.state = session` → `ChatNotifier` reaction → subscription swap. Find where the new session's prior messages are loaded (or whether they're loaded at all on swap).
2. Reproduce: open app, click an existing session with prior assistant messages, observe initial render. Capture the exact frame sequence.
3. Identify whether the bug is: (a) prior assistant messages not fetched on swap, (b) fetch fires but completes after the first paint, (c) text color/state on assistant bubbles defaults to grey until a stream event "paints" them, (d) something else.
4. Fix at the source. Likely candidates:
   - **(a)** Add a synchronous history fetch in `ChatNotifier`'s session-change handler before notifying listeners, with the loading placeholder shown for the brief window.
   - **(b)** Reserve the `ChatMessageList` scroll position + render skeleton bubbles for known-but-unloaded assistant turns until their content arrives.
   - **(c)** Ensure assistant bubble text color is the normal sent-message color by default, regardless of stream state.

The implementation plan will include a dedicated investigation task (with reproduction) before the fix task. Marking this as **root-cause-first** in the plan.

### Gap 7 — Archive (Model + Storage + API + Flutter + TUI)

**Goal:** trash icon archives (not deletes); archived sessions grey out and sort to bottom; permanent delete remains accessible. Both Flutter and TUI get the same semantics.

#### Data model

`internal/session/session.go:58-82` — add field:
```go
Archived bool `json:"archived,omitempty"`
```

#### SQLite migration

`internal/session/store_sqlite.go` — append to the migration list (lines 114-141):
```sql
ALTER TABLE sessions ADD COLUMN archived BOOLEAN DEFAULT 0;
```
Also add `archived` to the `List()` query (line 531-554): `SELECT …, archived FROM sessions …` and change `ORDER BY last_activity DESC` to `ORDER BY archived ASC, last_activity DESC` so archived sessions sort to the bottom of each group.

Add `Archive(id string, archived bool) error` method to the store interface and SQLite impl: `UPDATE sessions SET archived = ? WHERE id = ?`.

Wire `Manager.ArchiveSession` in `internal/session/manager.go` (or wherever `Manager.DeleteSession` lives — mirror its locking pattern).

#### HTTP API

New endpoint in `internal/comm/http/server.go`:

```
PATCH /api/v1/sessions/{id}
Body: {"archived": true}   (or false)
Response: 204 No Content
```

- Validate body; reject unknown fields with 400.
- Returns 404 if session doesn't exist.
- Existing `DELETE /api/v1/sessions/{id}` remains unchanged — permanent delete.

Add a corresponding RPC method `sessions.archive` in `internal/rpc/` for TUI use (mirrors how `delete` is wired), so the TUI doesn't need to speak HTTP.

#### Flutter

- `ui/flutter_ui/lib/models/api_models.dart` — add `@Default(false) bool archived` to the `Session` freezed class.
- `ui/flutter_ui/lib/services/sdk_client.dart` — add `archiveSession(id, archived)` doing `PATCH /api/v1/sessions/$id` with the JSON body.
- `ui/flutter_ui/lib/services/session_notifier.dart` — add `archiveSession(id)` and `unarchiveSession(id)` methods; on success, mutate the local list to flip the flag and re-sort.
- `ui/flutter_ui/lib/features/sessions/sessions_list.dart`:
  - Change trash icon → `Icons.archive_outlined` (`sessions_list.dart:237-242`).
  - Confirmation dialog text: "archive session?" → on confirm, call `notifier.archiveSession(id)`.
  - Render archived sessions with reduced opacity (e.g., `Opacity(opacity: 0.5)`) and greyed text color.
  - Long-press the tile → opens a context menu with "delete permanently" → opens the permanent-delete confirmation. Delete stays accessible but is not the default. (Long-press chosen over a secondary icon to keep tile layout clean at 150px-wide constraint from Gap 3's neighbor tabs; the sessions list is unaffected by Gap 3 but consistency is nice.)
- Sort order comes from the API (`archived ASC, last_activity DESC`), so archived sessions automatically appear at the bottom of the list.

#### TUI

- `internal/tui/types/types.go:169-186` — add `Archived bool` to the TUI `Session` type.
- `internal/tui/models/sessions.go` — render archived sessions with grey/dim styling (existing styles in `internal/tui/styles.go`).
- Wire the dead `d` key (`app.go:717` claims it but it's a no-op) to **archive** (not delete): on press, call `sessions.archive` RPC, show status message "archived: <name>".
- Wire `D` (shift+d) to permanent delete via the existing `deleteSession` path.
- Update the status message at `app.go:717` from `"sessions tab (create: n, delete: d)"` to `"sessions tab (create: n, archive: d, delete: shift+d)"`.

## Architecture / Cross-Cutting Concerns

### Why a single plan, not seven

Gaps 1 and 1b are coupled (status bar depends on verbosity provider). Gap 7 spans five layers and benefits from a single coherent commit boundary. Gaps 5 and 6 both touch `ChatNotifier` and the session detail flow. A single plan with clearly phased tasks keeps the cross-layer changes coherent.

### Testing

Per-gap test requirements:

| Gap | Tests |
|---|---|
| 1 (status bar) | Widget test: all parts render given providers; parts suppress when values are null; project indicator renders both `git` and `local` modes correctly; dirty `*` appears only when `dirty == true`. |
| 1b (verbosity) | Unit test: filter drops events with tier > level. Widget test: cycling Cmd+V updates provider + persists. |
| 2 (palette) | Widget test: palette opens on Cmd+X, arrow keys move selection, enter activates correct handler. |
| 3 (agents) | Widget test: tiles render at expected size; window-width variation produces expected column counts (use `tester.binding.window.physicalSizeTestValue`). |
| 4 (double-click) | Widget test: double-tap fires `tabActivationProvider` → HomeScreen updates `_selectedTab`. |
| 5 (cache) | Unit test: `CachedDetailProvider` returns cached value on second read; prefetch warms cache. Widget test: detail widget shows placeholder while `isLoading`. |
| 6 (grey) | **Repro test first.** Once root cause is identified, add a regression test that fails before the fix and passes after. |
| 7 (archive) | Go: `store_sqlite_test.go` covers `Archive`, sort order, migration. HTTP: server test for `PATCH /api/v1/sessions/{id}`. Flutter: widget test for archive icon + greyed rendering. TUI: test that `d` archives and `D` deletes. |

### Backwards / forwards compatibility

- The `archived` column defaults to 0 — all existing sessions are non-archived, behavior unchanged for existing users.
- The `archived` JSON field is `omitempty` — old clients that don't know about it just ignore it.
- The PATCH endpoint is additive — old clients continue using DELETE.
- The TUI `d` key was previously a no-op (status message lied) — wiring it to archive is a strict improvement, not a regression for any working flow.
- Removing the leader-key chord is a behavior change for anyone who relied on Cmd+X+s/p/b/c. Mitigation: the palette covers every action the leader key did, with discoverable labels. Document in the plan's commit message.

### Performance

- Status bar adds one `Container` + `Consumer` row. Negligible.
- Verbosity filter *reduces* event traffic to the UI (drops high-tier events at low verbosity). Net win.
- Cached detail providers eliminate repeat fetches. Net win.
- Archive migration is a single `ALTER TABLE` — fast on any reasonable session count.

### Security

No new attack surface. PATCH endpoint validates body and rejects unknown fields. Archive/unarchive do not bypass any existing auth. No new secrets, no new outbound calls.

## Open Questions for Implementation

None blocking — all decisions locked during brainstorming:
- (A) Verbosity is in-scope as first-class (Gap 1b), cycled via **Ctrl+V on all platforms**.
- (B1) Replace leader key with command palette.
- (C) Both archive and delete accessible; archive is default.
- (b) Lazy prefetch on first tab visit; warm `'default'` session detail on app start.
- (D) Project indicator is in-scope (Gap 1, item 4). Backend plumbing for `listProjects` exists; `GET /api/v1/projects/{id}/status` (for dirty/branch detail) needs verification — fallback to list-response fields if missing.

## Implementation Plan Reference

The implementation plan (produced by `writing-plans`) will phase the work roughly:

1. **Foundation (Gap 1b + Gap 7 model/storage/API)** — verbosity provider, archive schema + RPC + HTTP. No UI yet; everything testable in isolation.
2. **Flutter status bar + palette (Gap 1 + Gap 2)** — depends on verbosity provider from phase 1.
3. **Flutter tile/list fixes (Gap 3 + Gap 4)** — independent of phases 1-2.
4. **Cached detail (Gap 5)** — independent of phases 1-3.
5. **Grey transcript root-cause + fix (Gap 6)** — independent; investigation may begin in parallel with phase 1.
6. **TUI parity for archive (Gap 7 TUI portion)** — depends on phase 1 RPC.
7. **Tests + docs** — interleaved per phase, with a final consolidation pass.

Phase ordering may shift based on what `writing-plans` produces.
