# Flutter GUI Status Surface — Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD.
> Do NOT commit. Do NOT use read_file on existing source files.

## Meta

- **Parent:** ../master.md
- **Scope:** Flutter GUI parity for the ACP status surface (Dart), consuming leaf 08's HTTP endpoint
- **Dependencies:** 08 (endpoint + payload shape frozen there)
- **Estimated Context:** 40K
- **Concurrency Group:** F

## Goal

Close the TUI/GUI parity invariant for ACP: the Flutter desktop/web client shows the same acp status the TUI does — enabled state and live agent sessions. Source of truth is the endpoint leaf 08 ships: `GET /api/v1/acp/agents` → `{"enabled": bool, "agents": [{"id","enabled","running","state","uptime_s"}]}`. Disabled default renders nothing (no dead UI). This leaf is Dart/Flutter ONLY — no Go changes.

## Context

Key files (survey with search_files; do not read_file existing sources into writes):
- ui/flutter_ui/lib/core/platform/platform_service.dart — the platform abstraction pattern; web/desktop guards (`kIsWeb`) are mandatory
- ui/flutter_ui/ — locate the existing status surface: grep for where MCP server status or daemon status is rendered in the GUI (AGENTS.md parity means MCP likely has a GUI surface already — mirror its data-fetch and rendering pattern exactly)
- The GUI's HTTP client layer — reuse its auth/header mechanism; do not hand-roll a second client

Flutter constraints from AGENTS.md:
- No top-level `dart:io` imports in shared code — `kIsWeb` guards
- Platform detection via `bool.fromEnvironment('dart.library.io')`
- File I/O wrapped in `if (kIsWeb) return;` guards (this leaf should need none)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```dart
// ui/flutter_ui/lib/features/acp/acp_status.dart (path adjusted to match
// the GUI's actual feature/layout convention — mirror where MCP status lives)
class AcpAgentStatus {
  final String id;
  final bool enabled;
  final bool running;
  final String state;   // "ready"|"busy"|"closed"|""
  final int uptimeS;
}

class AcpStatusService {
  // GET /api/v1/acp/agents through the GUI's existing HTTP layer
  Future<AcpStatusView> fetch(); // AcpStatusView{enabled, agents}
  // Web-safe: uses the same dio/http client the GUI already configures (headers included)
}

// Widget: AcpStatusCard (or panel section matching the MCP status pattern)
// Renders: nothing when enabled==false; agent list (id, state, uptime) otherwise.
// All text lowercase (repo UI invariant).
```

### What This Leaf Consumes

From 08 (frozen payload): GET /api/v1/acp/agents envelope. From the GUI codebase: its HTTP client + auth headers, its status-card pattern, its test harness pattern.

## Tasks

### Task 1: Model + service with failing tests

**Files:** Create: ui/flutter_ui/test/acp_status_test.dart (mirror the GUI's existing test file layout); implementation in the lib feature path found in Context

Model parses the frozen payload JSON (null-safe: missing fields → disabled/empty). Service hit test uses the GUI's existing mocked-client pattern (find how sibling status tests fake HTTP; never hit a real daemon in tests).

### Task 2: Widget with failing tests

**Files:** Same feature dir + test

AcpStatusCard: enabled=false → renders SizedBox.shrink() (or the sibling pattern's empty). Enabled with 2 fake agents → id, state, uptime visible; lowercase text asserted. Web parity: widget test runs on the default test platform; no dart:io anywhere in the new code (assert via grep in your report).

### Task 3: Wire into the status surface

**Files:** Modify: the screen/panel where MCP/daemon status renders (found in Context)

Add the card next to the sibling status cards. No new route, no new navigation — follow the surface that already exists.

### Task 4: Analyzer + audit scripts

Run: `flutter analyze` scoped to changed dirs (probe-before-purge rule: fix real findings; a false "undefined name" on a correct symbol gets a throwaway probe file before any cache talk). Run the repo's Dart audit scripts if they cover new files: `python3 scripts/audit-dart-enum-name-shadow.py` scope-check (only if it scans the changed tree).

## Self-Verification Checklist

- [ ] `flutter analyze` clean on changed files (probe before purge on weird findings)
- [ ] Widget + model tests green via the GUI's test runner (`flutter test test/acp_status_test.dart` or suite convention)
- [ ] Zero `dart:io` top-level imports in new code; `kIsWeb` guards where the pattern requires
- [ ] Disabled default renders nothing — tested
- [ ] All text lowercase
- [ ] No TODOs

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Payload contract matches leaf 08's frozen shape exactly (field names, types)
- [ ] Mirrors the GUI's existing status pattern (client, card, test harness)
- [ ] Web-safe per AGENTS.md Flutter rules
- [ ] Dart-only diff — no Go files touched
- [ ] File scope respected; foreign hunks reported not staged

## Notes

- This leaf exists because of DECISION Q3 (user: "fix this so it works with flutter too") — the original tree's deferred-parity deviation is removed; master's Notes no longer records it as deferred.
- If the GUI has NO existing status surface to mirror (survey finds none), STOP and report — the fallback (building a new settings section) is an orchestrator decision, not yours.
- Uptime formatting: seconds int → human string in the widget ("2m 14s"), lowercase, or reuse a sibling formatter if one exists.
