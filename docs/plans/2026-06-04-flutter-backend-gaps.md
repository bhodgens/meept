# Flutter UI Backend-Dependent Gaps

**Date:** 2026-06-04
**Branch:** combined-ui-parity-design
**Status:** Planned — requires backend endpoint work

---

## Gap 1: Search / Find Endpoint

**Current state:** Leader key `leader p` shows a snackbar "search: not yet implemented".
**Location:** `ui/flutter_ui/lib/features/home/home_screen.dart:91`

### What's needed

1. **Backend:** Add search endpoint to the daemon:
   - `POST /api/v1/search` — full-text search across sessions, tasks, memories, and plans
   - Accept `query` string, optional `scope` filter (`sessions|tasks|memories|plans|all`)
   - Return ranked results with type, id, title, and snippet

2. **Flutter:** Create search panel:
   - `ui/flutter_ui/lib/features/search/search_panel.dart` — full-screen search overlay
   - Debounced input field (300ms)
   - Result groups by type with navigation on tap
   - Wire to `onFind` callback in `LeaderKeyController`

---

## Gap 2: Branches / Projects Context

**Current state:** Leader key `leader b` shows a snackbar "branches: not yet implemented".
**Location:** `ui/flutter_ui/lib/features/home/home_screen.dart:94`

### What's needed

1. **Backend:** Extend project context system:
   - `GET /api/v1/projects/{name}/branches` — list git branches for a registered project
   - `POST /api/v1/projects/{name}/checkout` — switch branch
   - `GET /api/v1/projects/{name}/status` — git status summary

2. **Flutter:** Create branches panel:
   - `ui/flutter_ui/lib/features/projects/branches_panel.dart`
   - List branches with current indicator
   - Tap to checkout (with confirmation)
   - Show dirty working tree status
   - Wire to `onBranches` callback in `LeaderKeyController`

---

## Gap 3: Dynamic Skill Rendering

**Current state:** Skills fetched from daemon API via `ToolsDropdown` show "coming soon" when selected.
**Location:** `ui/flutter_ui/lib/features/chat/chat_tab.dart:52-81`

### What's needed

1. **Backend:** Add skill UI descriptor to skill metadata:
   - Extend `GET /api/v1/skills` response with optional `ui_type` field:
     - `"panel"` — renders as a panel with content from the skill
     - `"dialog"` — opens a dialog for configuration
     - `"external"` — opens URL in browser
   - Add `GET /api/v1/skills/{slug}/execute` for interactive skill execution

2. **Flutter:** Add a default skill panel handler:
   - `ui/flutter_ui/lib/features/skills/skill_panel.dart` — generic skill rendering
   - Falls back to a description + execute button for skills without `ui_type`
   - Add a `default` case in `ChatTab._buildToolView` that routes to `SkillPanel`
