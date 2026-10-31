# GUI Activity Page (Flutter) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** A new Flutter GUI activity page: activity list with three sort
  modes, live recent-events panel, findings + drift sections; HTTP client
  methods; route + palette parity entries.
- **Dependencies:** 02-rpc.md (activity.* + agents.review RPC methods)
- **Estimated Context:** 95K
- **Concurrency Group:** C (dispatch after 02 reaches REVIEWED; parallel with 03)

## Goal

The GUI user opens an activity tab and sees the same data as the TUI: a
sortable activity list (chronological / by task / by plan), a live recent
panel polling activity/recent, and sections listing audit findings and
per-employee drift scores. Web + desktop compatible (kIsWeb-safe code only).

## Context

The Flutter app routes top-level tabs through go_router into a HomeScreen
shell; feature pages live in ui/flutter_ui/lib/features/<name>/ (copy the
tasks feature: tab + list + detail files; queue_list.dart shows a
ConsumerStatefulWidget ListView). The HTTP client is SdkApiClient in
services/sdk_client.dart - the GUI talks HTTP, NOT the RPC socket, so this
leaf adds the HTTP handlers for the activity endpoints FIRST (in
internal/comm/http), then the Dart methods, then the page. The palette
parity manifest lives at internal/tui/testdata/gui_inventory.json and the
GUI palette items live in widgets/command_palette.dart - BOTH get the new
entry, and command_palette_test.dart enforces the match.

## Verified Anchors

Anchors grep-confirmed 2026-10-30. Re-grep before editing.

- internal/comm/http/api_handlers.go:685 - GET /api/v1/tasks handler - pattern for new GET handlers.
- internal/comm/http/api_handlers.go:1837 - GET /api/v1/tasks/{id}/steps - sub-resource pattern.
- internal/comm/http/api_handlers.go:3069 - GET /api/v1/plans - list-with-filters pattern.
- internal/employee/api_handlers.go:444-456 - findings query params (?since=&severity=) precedent.
- ui/flutter_ui/lib/core/router.dart:17-29,60+ - go_router routes; /tasks tab pattern - add /activity.
- ui/flutter_ui/lib/features/tasks/ - tasks_tab.dart, tasks_list.dart - the list page to copy.
- ui/flutter_ui/lib/features/queue/queue_list.dart:102 - ConsumerStatefulWidget + ListView.builder pattern.
- ui/flutter_ui/lib/services/sdk_client.dart:38 - SdkApiClient; listTasks :861, getTask :878, listAgents :764 - method patterns to copy.
- ui/flutter_ui/lib/widgets/command_palette.dart:51+ - CommandPalette.defaultItems - add the activity entry.
- ui/flutter_ui/test/widgets/command_palette_test.dart:19,37 - parity assertions against gui_inventory.json.
- internal/tui/testdata/gui_inventory.json - shared parity manifest (leaf 03 adds the TUI entry; this leaf must match it).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/comm/http/api_handlers.go (or a new activity_handlers.go in the
// same package) - HTTP surface, JSON IDENTICAL to C2/C4 RPC results:
// GET /api/v1/activity          ?since&until&sort&task_id&plan_id&agent_id&kinds&limit&offset
//                               -> {"records":[ActivityRecord],"total":N}
// GET /api/v1/activity/recent   ?limit -> {"records":[...],"dropped":N,"total":N}
// GET /api/v1/activity/overview -> {"by_agent":[...],"generated_at"}
// GET /api/v1/activity/findings ?employee_id&since&severity&resolved&limit&offset
//                               -> {"findings":[...],"total":N}
// GET /api/v1/agents/{id}/review -> Review JSON (drift_score, recent findings)
// Handlers call the SAME daemon-side aggregator/ring/manager as leaf 02.
// Dart: SdkApiClient methods listActivity/getActivityRecent/getActivityOverview/
// getActivityFindings/getAgentReview decoding those exact field names.
// Route: /activity tab; palette item "activity view" (lowercase) matching
// the manifest entry leaf 03 added.
```

### What This Leaf Consumes

- C1/C2/C4 result JSON shapes (leaf 02 is the RPC twin; field names identical)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | HTTP handlers for the 5 activity endpoints over shared daemon handles | Modify: internal/comm/http/api_handlers.go (or new file in package); Test: internal/comm/http/activity_handlers_test.go | `go test -p 2 ./internal/comm/http/ -run TestActivity` | routes 404 | each endpoint returns C2/C4-shaped JSON; sort/filter params parse; 400 on bad sort | Given fake aggregator; When GET /api/v1/activity?sort=plan; Then JSON records grouped by plan, total set |
| T2 | Dart client methods + activity page with sort modes | Modify: ui/flutter_ui/lib/services/sdk_client.dart; Create: ui/flutter_ui/lib/features/activity/ (activity_tab.dart, activity_list.dart); Modify: ui/flutter_ui/lib/core/router.dart; Test: ui/flutter_ui/test/features/activity/activity_list_test.dart | `cd ui/flutter_ui && flutter test test/features/activity/` | page/client absent | page lists records; segmented control switches sort mode (chronological/by task/by plan) re-querying with the param; empty state text lowercase | Given mock client with 10 records; When tap "by task"; Then list re-groups and client captured sort=task |
| T3 | Live recent panel + findings/drift sections + palette parity | Modify: features/activity files; Modify: ui/flutter_ui/lib/widgets/command_palette.dart; Test: command_palette_test.dart + activity_list_test.dart | `cd ui/flutter_ui && flutter test test/widgets/command_palette_test.dart test/features/activity/ && go test -p 2 ./internal/tui/ -run TestParity` | sections absent | recent panel polls on a timer with drop counter; findings section lists severity + resolution state; drift section renders per-employee scores; palette item matches manifest (parity tests green BOTH surfaces) | Given mock review drift 0.4; When page loads; Then employee card shows 0.4 |

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly (HTTP JSON == RPC JSON)
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify
- [ ] Web-safe code: no top-level dart:io imports; kIsWeb guards where needed
- [ ] All new UI text lowercase

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] HTTP JSON field names byte-identical to C1/C2/C4
- [ ] Palette entry present in BOTH command_palette.dart and gui_inventory.json;
      both parity tests green
- [ ] dart format run on all touched files
- [ ] Polling timer disposed with the widget (no leak)
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- The HTTP handlers (T1) are Go and land in internal/comm/http - coordinate
  with leaf 02 ONLY through the shared daemon handles; do not call RPC
  methods over the socket from HTTP handlers.
- gui_inventory.json is SHARED with the TUI leaf: leaf 03 adds the entry;
  if 03 has not landed when you reach T3, add the manifest entry yourself
  and note it - the orchestrator deduplicates at integration.
- Polling interval: 2s for recent, matching TUI; stop when route not active.
