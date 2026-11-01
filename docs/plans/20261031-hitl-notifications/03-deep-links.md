# Session Deep Links (meept:// + Menubar Tap-Through + Web Fallback) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> This leaf has TWO self-contained halves (Dart/Flutter and Swift/menubar)
> sharing ONE pinned grammar (contract C6). Implement BOTH halves; the
> shared schema is pinned in C6 below so the halves meet without
> coordination. Implement ALL tasks using TDD. Do NOT commit - the
> orchestrator handles all git operations after review. Do NOT use
> read_file on existing source files - explore with search_files or
> terminal cat. Write once and stop; report files touched and deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** `meept://session/<id>` deep links: Dart parsing + navigation
  (with feature gate + web fallback), Swift menubar tap-through that opens
  the Flutter app with the link.
- **Dependencies:** none hard; the TARGET surface is the in-flight
  activity-views tree's `/activity` route (feature-gated - see C6)
- **Estimated Context:** 90K
- **Concurrency Group:** B
- **Audit references:** contract C6 in ../master.md

## Goal

Menubar notifications arrive, the user clicks, and nothing opens
(NotificationManager.swift:288-310 only marks read). This leaf makes the
tap (and any dock/launcher URL) open the right session: a `meept://session/
<id>` URL is parsed by one pinned grammar in both clients; the Flutter app
navigates to the activity view when that route exists and otherwise falls
back to the web session URL served by the daemon's existing web substrate
(internal/comm/web/server.go).

## Context

Meept ships a Flutter GUI (ui/flutter_ui, web + macOS desktop) and a
Swift menubar app (menubar/MeeptMenuBar). The pieces you build on:

Dart half:
- ui/flutter_ui/lib/services/sdk_client.dart:374-395 - the ACP permission
  surfaces (parity reference for how the GUI consumes daemon events).
- ui/flutter_ui/lib/core/router.dart - go_router routes; the `/activity`
  route comes from the in-flight activity-views tree and MAY NOT EXIST.
  Your feature gate checks route presence at runtime, never an import.
- internal/comm/web/server.go - serves the GUI web payload; the fallback
  URL is `<web base>/#/activity?session=<id>` per C6.
- Flutter multi-platform rules (root AGENTS.md): no top-level dart:io in
  shared code; use the PlatformService pattern
  (ui/flutter_ui/lib/core/platform/platform_service.dart) and kIsWeb
  guards. Deep-link registration on macOS desktop uses app schema
  handling; on web the module only ever produces fallback navigation.

Swift half:
- menubar/MeeptMenuBar/Services/NotificationManager.swift:288-310 - the tap
  handler; userInfo carries eventID/taskID/agentID (sessionID added per
  C6 - check the payload keys actually sent by the daemon's push path and
  pin the exact key name in your report).
- The menubar app already knows how to launch/focus the GUI (grep
  NSWorkspace / open( in menubar/MeeptMenuBar for the existing launch
  path); reuse it - do not invent a second launcher.

## Verified Anchors

Anchors grep-confirmed 2026-10-31. Re-grep before editing - siblings move lines.

- menubar/MeeptMenuBar/Services/NotificationManager.swift:288-310 -
  didReceive handler; marks read + posts .notificationTapped (:315);
  comment "could open relevant view" - the gap this leaf closes.
- menubar/MeeptMenuBar/Services/NotificationManager.swift:14 -
  UNUserNotificationCenter; SessionBadgeManager 5 s / EvalBadgeManager 30 s
  polls feed the notifications (context only).
- ui/flutter_ui/lib/core/router.dart:17-29 - go_router config (route list
  you must feature-gate against).
- ui/flutter_ui/lib/core/platform/platform_service.dart - platform
  abstraction singleton (kIsWeb-safe returns).
- internal/comm/web/server.go - web payload server (fallback target).
- ui/flutter_ui/lib/services/sdk_client.dart:374-395 - permission event
  consumption precedent.

## Interface Contracts (From Parent)

### What This Leaf Exposes

- C6 grammar: URL `meept://session/<session-id>`, scheme literal "meept",
  host literal "session", id regex ^[A-Za-z0-9-]+$. Dart module
  ui/flutter_ui/lib/core/deep_link.dart exposing
  `resolveDeepLink(Uri) -> DeepLinkTarget{sessionID, useWebFallback}`;
  when the `/activity` go_router route is absent, useWebFallback is true
  and the fallback URL is `<web base>/#/activity?session=<id>`.
  Swift: the tap handler builds the same URL from userInfo["sessionID"]
  and opens the app with it.

### What This Leaf Consumes

- [/activity route presence] - runtime route check in the Flutter app
  (go_router configuration object); no compile-time dependency on the
  activity-views tree.
- [web base URL] - the daemon web base the GUI already knows (grep the
  sdk_client/config for the base URL resolution; pin your source).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Dart deep-link module: parse per C6, feature-gate on /activity route presence, build fallback URL | Create: ui/flutter_ui/lib/core/deep_link.dart; Test: ui/flutter_ui/test/core/deep_link_test.dart | `cd ui/flutter_ui && flutter test test/core/deep_link_test.dart` | module absent | valid link + route present -> useWebFallback false; route absent -> true with correct fallback URL; bad host/scheme/id -> null (or error target per your API - pin it); ASCII id regex enforced | Given meept://session/abc-123 with no /activity route; When resolveDeepLink; Then DeepLinkTarget{sessionID:"abc-123", useWebFallback:true} and fallback URL ends #/activity?session=abc-123 |
| T2 | Dart navigation wiring: deep link -> go_router /activity?session=<id> OR url_launcher web fallback; call sites handle platform differences via PlatformService (kIsWeb guards) | Modify: ui/flutter_ui/lib/core/router.dart (listener), ui/flutter_ui/lib/core/platform/platform_service.dart (openExternal if absent); Test: ui/flutter_ui/test/core/deep_link_nav_test.dart | `cd ui/flutter_ui && flutter test test/core/` | no navigation on link | route-present app navigates to /activity with session param; route-absent app opens external URL; no dart:io outside kIsWeb guards | Given a DeepLinkTarget; When handled; Then the router captured navigation to /activity?session=abc-123 (mock route) and no exception on web target |
| T3 | Swift tap-through: notification tap builds meept://session/<id> from userInfo and opens the Flutter app | Modify: menubar/MeeptMenuBar/Services/NotificationManager.swift (tap handler :288-310); Modify: the menubar launch path file (grep first); Test: logic mirrored in dart tests + compile check `xcodebuild -project menubar/MeeptMenuBar/<proj> -scheme <scheme> build` OR `swiftc` syntax check of the touched file per repo precedent - pin the exact command that works on this host and report it | tap handler builds + logs the URL and invokes the existing launch path with it; missing/empty sessionID keeps today's mark-read-only behavior | no open on tap | handler builds the URL per C6 and calls launch; no URL when sessionID absent | Given userInfo{sessionID:"abc-123"}; When tap handled; Then the launch path received meept://session/abc-123 |
| T4 | Flutter desktop app handles the meept:// scheme on macOS (register scheme + route into the Dart listener) | Modify: macos/ runner files under ui/flutter_ui (Info.plist CFBundleURLTypes + AppDelegate channel into the Dart listener); Test: ui/flutter_ui/test/core/deep_link_nav_test.dart extended (channel -> resolveDeepLink -> navigation) | `cd ui/flutter_ui && flutter test test/core/ && flutter build macos --debug` (or `flutter analyze` if build is too slow - report which) | scheme unregistered | cold-start URL lands in the Dart listener and navigates; invalid URL is ignored with a lowercase log line | Given the app launched via open meept://session/abc-123; When the platform channel fires; Then resolveDeepLink runs and the router navigates |

Worked example (canonical row expansion, T1):

1. Write failing test:

```dart
test('falls back to web when activity route absent', () {
  final t = resolveDeepLink(Uri.parse('meept://session/abc-123'),
      hasActivityRoute: false, webBase: 'http://localhost:8080');
  expect(t, isNotNull);
  expect(t!.useWebFallback, isTrue);
  expect(t.fallbackUrl.toString(),
      'http://localhost:8080/#/activity?session=abc-123');
});
```

2. Verify failure: `cd ui/flutter_ui && flutter test test/core/deep_link_test.dart`
   Expected: FAIL - deep_link.dart not found.
3. Minimal implementation: DeepLinkTarget + resolveDeepLink per C6.
4. Verify pass: same command, PASS.

## Self-Verification Checklist

- [ ] Both halves complete: Dart module + navigation; Swift tap-through
- [ ] C6 grammar byte-identical in BOTH halves (same regex, same keys)
- [ ] All tasks implemented and tests passing; `dart format` run on every
      touched .dart file
- [ ] Feature gate: app builds and runs green with AND without an
      /activity route (test covers both)
- [ ] No top-level dart:io in shared code; kIsWeb guards present
- [ ] All UI text lowercase; no user-visible strings added in Swift beyond
      logs
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output;
   include the pinned Swift compile-check command you actually ran]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] C6 exact: grammar, feature-gate semantics, fallback URL shape
- [ ] Swift handler reuses the existing launch path (no second launcher)
- [ ] Missing sessionID degrades to today's behavior (no crash, no open)
- [ ] Web compatibility preserved (kIsWeb guards; no dart:io at top level)
- [ ] Conventions followed; no bugs, no security issues
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- Pin the EXACT userInfo key for the session id after grepping the daemon
  push payload (NotificationManager comments mention eventID/taskID/
  agentID; the daemon side is internal/services/push_* + the menubar WS
  consumer). If no session id exists in the payload today, add it at the
  daemon push path in this leaf and say so in the report (one-line change,
  same contract).
- No Swift test harness exists in-repo: keep the Swift side thin (build
  URL + call launcher), mirror ALL grammar logic in the Dart tests, and
  record this limitation in the Test Gate matrix (F6 documents it).
- url_launcher (or the repo's existing external-open helper - grep before
  adding a dependency) handles the web fallback; prefer the existing one.
