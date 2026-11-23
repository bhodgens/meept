# Menubar Analytics Tab - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks. Work ONLY in
> `/Users/caimlas/git/meept/.hermes/worktrees/model-rating-metrics-plan`.
> Do NOT commit.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Grades tab in the macOS menubar analytics dashboard (Swift)
- **Dependencies:** 01-http-endpoint REVIEWED (frozen response shape)
- **Estimated Context:** 40K
- **Concurrency Group:** D (parallel with 03-flutter-panel; disjoint dirs)

## Goal

The menubar analytics dashboard shows the same grades data as the GUI panel: by
model, by judge.

## Context

The menubar app lives in `menubar/MeeptMenuBar/`; analytics views in
`Views/Analytics/` (`DashboardWindow.swift`, `LiveMetricsView.swift`). Add a
`ReviewGradesView.swift` tab beside LiveMetricsView, decoding the frozen JSON with
the existing Services networking pattern (`MenubarConfigService.swift` shows the
house HTTP style). No code changes outside `menubar/`.

## Verified Anchors

- `menubar/MeeptMenuBar/Views/Analytics/LiveMetricsView.swift` - view + fetch pattern to mirror
- `menubar/MeeptMenuBar/Views/Analytics/DashboardWindow.swift` - tab registration site
- `menubar/MeeptMenuBar/Services/MenubarConfigService.swift` - HTTP/service style

## Interface Contracts (From Parent)

### What This Leaf Exposes

- `ReviewGradesView.swift`: SwiftUI view listing rows (group, defects, pass_rate)
  with model/judge group-by picker; decodes
  `{window, group_by, rows:[{group, verdicts, defects, pass_rate}], workflow_excluded}`
- Tab entry in DashboardWindow beside LiveMetricsView

### What This Leaf Consumes

- C5 frozen HTTP shape; existing menubar HTTP client

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Codable model + view | Create: menubar/MeeptMenuBar/Views/Analytics/ReviewGradesView.swift | `xcodebuild -project menubar/<existing>.xcodeproj build` (grep for the project file; else `swiftc -parse` on the new file) | file absent | parses/builds | Given the JSON shape; When decoded; Then rows render in a table with lowercase labels |
| T2 | Tab registration | Modify: menubar/MeeptMenuBar/Views/Analytics/DashboardWindow.swift | build command above | tab absent | build green; tab visible | Given the dashboard; When opened; Then grades tab sits beside live metrics with the same selection chrome |

Verification gate: successful build of the menubar target + manual visual check by
the orchestrator (screenshot path acceptable); record both in the completion report.

## Self-Verification Checklist

- [ ] Build green; style matches LiveMetricsView (spacing, error/empty states)
- [ ] Labels lowercase; judge naming (never reviewer)
- [ ] No changes outside menubar/

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. build output tail; 3. Deviations.

## Review Checklist (For Review Agent)

- [ ] Frozen keys decoded exactly; degrade-to-empty on fetch failure like LiveMetricsView
- [ ] No reviewer label anywhere; no scope creep

Output: APPROVED or specific gaps.

## Notes

No unit-test target exists for the menubar app - the gate is build + manual check
(pinned in the master Test Gate matrix, F8: unit n/a, e2e n/a/manual).
