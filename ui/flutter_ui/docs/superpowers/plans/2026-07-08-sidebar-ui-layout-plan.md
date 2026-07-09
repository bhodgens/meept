# Sidebar UI Layout Implementation Plan

**Created**: 2026-07-08
**Trigger**: User request for sidebar-based alternative GUI layout
**Reference**: `ui/flutter_ui/lib/layouts/layout_1_classic_sidebar.dart` (Layout 1 mockup)

## Overview

Add a sidebar-based UI layout as an alternative to the existing top-tab navigation. The sidebar layout provides hierarchical session exploration with expandable tree views while maintaining all existing functionality.

## Configuration

Add to `~/.meept/meept.json5`:

```json5
{
  "client": {
    "layout": "sidebar"  // Options: "toptabs" (default) | "sidebar"
  }
}
```

## Architecture Decision: Config vs Runtime Toggle

**Decision**: JSON config option (not runtime UI toggle)

**Rationale**:
1. Layout choice affects routing, widget tree structure, and state management
2. Cleaner code separation - two distinct home screen implementations
3. Matches existing Meept config patterns
4. Avoids complexity of runtime widget tree reconstruction
5. Can add runtime toggle later if user demand warrants it

---

## Phases

### Phase 1: Configuration & Router Setup

**Goal**: Add config option and route to appropriate layout

**Steps**:
1. Add `layout` field to client config schema (`internal/config/`)
2. Expose config via HTTP API (`GET /api/v1/config/client`)
3. Update dart-dio SDK types for new config field
4. Add layout preference to Flutter config provider
5. Create router logic to select home screen variant

**Files**:
- `internal/config/schema.go`
- `internal/config/client.go`
- `sdk/dart/lib/src/api/v1_api.dart` (regenerate)
- `ui/flutter_ui/lib/providers/preferences_provider.dart`
- `ui/flutter_ui/lib/core/router.dart`

**Verification**:
- [ ] Config schema accepts `layout` field
- [ ] API returns layout value
- [ ] Flutter reads layout from config

---

### Phase 2: Sidebar Home Screen Component

**Goal**: Create sidebar-based home screen with navigation

**Structure**:
```
+--------------+------------------------------------------+
|  LOGO        |  [Header: Session Title]                 |
|              +------------------------------------------+
|  [+ Sessions]|                                          |
|    ├─ sess1  |           Message List                   |
|    ├─ sess2  |                                          |
|    └─ sess3  |                                          |
|              +------------------------------------------+
|              |  [Chat Input Area]                       |
+--------------+------------------------------------------+
|  [Status Bar]                                          |
+---------------------------------------------------------+
```

**Sidebar Features**:
- TreeView of sessions (expandable/collapsible)
- Each session shows: `[+] icon` (expand), `session name` (click to open), `[i] icon` (info)
- Auto-scroll vertically when sessions exceed height
- Green tint on background image, orange font

**Files to create**:
- `ui/flutter_ui/lib/features/home/sidebar_home_screen.dart`
- `ui/flutter_ui/lib/features/home/sidebar_session_tree.dart`
- `ui/flutter_ui/lib/features/home/session_info_overlay.dart`

**Files to modify**:
- `ui/flutter_ui/lib/theme/colors.dart` - add sidebar-specific tint colors
- `ui/flutter_ui/lib/features/home/home_screen.dart` - factor out shared logic

**Verification**:
- [ ] Sidebar renders with session tree
- [ ] Sessions expand/collapse with `[+]` click
- [ ] Session name click loads session in chat view
- [ ] Info icon clickable

---

### Phase 3: Session Info Overlay

**Goal**: Tabbed overlay showing session-scoped plans, tasks, agents

**Behavior**:
- Clicking `[i]` icon opens overlay
- Tabs: `plans` | `tasks` | `agents`
- Lists scoped to selected session
- `[X]` in upper-right closes overlay, returns to chat

**Files to create**:
- `ui/flutter_ui/lib/features/sessions/session_overlay_panel.dart`
- `ui/flutter_ui/lib/features/sessions/session_scoped_plans.dart`
- `ui/flutter_ui/lib/features/sessions/session_scoped_tasks.dart`
- `ui/flutter_ui/lib/features/sessions/session_scoped_agents.dart`

**Verification**:
- [ ] Overlay opens on info icon click
- [ ] Three tabs render correctly
- [ ] Lists show session-scoped data
- [ ] X button closes overlay

---

### Phase 4: Background Image & Styling

**Goal**: Apply semi-transparent background image with color tints

**Sidebar**:
- Background image at 10-20% opacity
- Green tint overlay
- Orange text for labels

**Chat Area**:
- Background image identical to current implementation

**Files to modify**:
- `ui/flutter_ui/lib/widgets/background_image.dart` - add tint support
- `ui/flutter_ui/lib/layouts/layout_1_classic_sidebar.dart` - use as reference

**Verification**:
- [ ] Background visible on sidebar with green tint
- [ ] Chat area background matches current UI
- [ ] Text readable over background

---

### Phase 5: Data Integration

**Goal**: Wire sidebar to real session/task/plan data

**Steps**:
1. Connect session tree to `activeSessionProvider`
2. Fetch session hierarchy (sessions → tasks → plans)
3. Handle real-time updates via WebSocket
4. Sync expand/collapse state with preferences

**Files to modify**:
- `ui/flutter_ui/lib/providers/session_provider.dart` - add tree structure
- `ui/flutter_ui/lib/providers/session_notifier.dart` - hierarchy logic
- `ui/flutter_ui/lib/services/websocket_service.dart` - listen for updates

**Verification**:
- [ ] Session tree reflects actual sessions
- [ ] New sessions appear in tree
- [ ] Task/plan hierarchy accurate
- [ ] WebSocket updates propagate

---

### Phase 6: TUI/Flutter Parity Check

**Goal**: Ensure Flutter sidebar matches any TUI sidebar (if exists)

Per CLAUDE.md UI parity requirement:
- If TUI has sidebar, Flutter must match
- Keyboard shortcuts should align where possible
- Document any surface-specific deviations

**Files to check**:
- `internal/tui/` - verify if sidebar exists or planned

**Verification**:
- [ ] Parity check documented
- [ ] Shortcuts aligned where applicable
- [ ] Deviations justified in comments

---

### Phase 7: Testing & Documentation

**Goal**: Write tests and update docs

**Tests**:
- Sidebar renders with empty session list
- Sidebar expands/collapses sessions
- Info overlay opens and closes
- Config option correctly switches layouts

**Documentation**:
- Add to `docs/workflows/flutter-gui.md`
- Add to `docs/configuration/` - document layout option
- Update `ui/flutter_ui/README.md`

**Verification**:
- [ ] Widget tests pass
- [ ] Integration tests pass
- [ ] Docs updated

---

## Implementation Order

```
Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5 → Phase 6 → Phase 7
   │         │         │         │         │         │         │
   └─────────┴─────────┴─────────┴─────────┴─────────┴─────────┘
                          │
                    Parallel verification possible for Phases 5-7
```

---

## Open Questions

1. **Session hierarchy depth**: How many levels (session → tasks → plans → subtasks)?
   - Answer: Check backend API structure, mirror platform semantics

2. **Info overlay behavior**: Should it be modal or modeless?
   - Recommendation: Modeless overlay (can chat while viewing)

3. **Persisted state**: What to persist (expanded sessions, selected session, overlay state)?
   - Recommendation: Persist expanded sessions per local storage patterns

4. **Default layout value**: `"toptabs"` or `"sidebar"`?
   - Recommendation: `"toptabs"` to maintain backward compatibility

---

## Estimated Effort

| Phase | Complexity | Estimated Time |
|-------|------------|----------------|
| 1. Config & Router | Low | 2-4 hours |
| 2. Sidebar Component | Medium | 6-8 hours |
| 3. Info Overlay | Medium | 4-6 hours |
| 4. Background/Styling | Low | 2-3 hours |
| 5. Data Integration | High | 8-12 hours |
| 6. TUI Parity | Variable | 2-4 hours |
| 7. Testing/Docs | Medium | 4-6 hours |
| **Total** | | **28-43 hours** |

---

## Success Criteria

1. User can set `"layout": "sidebar"` in config
2. Flutter app renders sidebar layout
3. Sessions display as expandable tree
4. Clicking session loads chat
5. Info icon opens session-scoped overlay
6. Background image visible with proper tints
7. All existing functionality preserved
