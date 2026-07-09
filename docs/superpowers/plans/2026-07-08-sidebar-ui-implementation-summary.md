# Sidebar UI Implementation Summary (2026-07-08)

## What Was Implemented

A sidebar-based navigation layout for the Meept Flutter GUI, providing an alternative to the traditional top-tabs layout. This gives users who prefer a persistent session browser alongside their chat a different organizational model where sessions, tasks, and plans live in a left sidebar rather than being accessed through top-level tabs.

## Files Created/Modified

### New files (created in prior phases)
- `ui/flutter_ui/lib/features/home/sidebar_home_screen.dart` -- Sidebar home screen with session tree, header bar, and chat area
- `ui/flutter_ui/lib/features/home/session_info_overlay.dart` -- Tabbed dialog showing session-scoped plans, tasks, and agents

### Modified files
- `ui/flutter_ui/lib/core/router.dart` -- Added `_LayoutShell` widget that reads `guiLayoutProvider` and renders either `SidebarHomeScreen` (for `"sidebar"`) or `_HomeShell` (for `"toptabs"`). All root routes (`/`, `/sessions`, `/tasks`, `/plans`, `/agents`) now go through `_LayoutShell`. Added live reactivity to layout changes via `ref.listen`.
- `ui/flutter_ui/lib/main.dart` -- Added `guiLayoutProvider.notifier.load()` call in `_ModifierKeyInitializerState.initState()` so the persisted layout preference is read from storage on app startup.

### Documentation
- `docs/workflows/flutter_gui.md` -- Added "Layout modes" section with visual diagram, component locations, and configuration instructions
- `docs/configuration/index.md` -- Added "Client Configuration" section documenting `gui.layout` and other client.json5 options
- `config/client.json5` -- Already contained `gui.layout` config option (from prior phase)

## Configuration

Set in `~/.meept/client.json5`:

```json5
{
  "gui": {
    "layout": "sidebar"  // or "toptabs" for the default
  }
}
```

Values:
- `"toptabs"` (default): Traditional horizontal tab bar with chat/sessions/plans/tasks/agents tabs
- `"sidebar"`: Vertical left sidebar with session tree, chat always visible in main area

The layout switch takes effect immediately without restarting the app when changed. The preference persists across restarts via storage.

## Data Flow

```
client.json5 ("gui.layout")
  -> StorageService.getGuiLayout()
    -> GuiLayoutNotifier.load() -> guiLayoutProvider state
      -> Router _LayoutShell reads provider
        -> If "sidebar": SidebarHomeScreen
        -> If "toptabs": _HomeShell (original layout)
```

## Known Limitations and Future Work

1. **Session tree child nodes are currently stubbed.** The sidebar shows sessions in the tree, but tasks and plans displayed as child nodes are rendered as placeholder "stub" items (not fully wired to real data). Future work should connect the tree rendering to actual task/plan data from `taskProvider` and `planProvider`.

2. **Sessions tab is not separately accessible in sidebar mode.** The sidebar layout only exposes the chat view at the top level; other tabs (sessions, plans, tasks, agents) are not directly reachable via route navigation. In sidebar mode, the top-level routes still exist in the router but the sidebar shell (`SidebarHomeScreen`) only supports the chat tab. Tool menus are accessible via the hamburger menu (search, branches).

3. **No session tree context menu.** Long-press or right-click context actions (archive, delete, rename) on session tree items are not yet implemented.

4. **No responsive sizing.** The sidebar width is fixed at 220px. A resizeable divider would improve adaptability for different screen sizes.

5. **No keyboard navigation in the session tree.** The TUI has rich keyboard navigation for the session panel (arrow keys, enter to select, etc.). Sidebar tree navigation currently requires mouse interaction.

6. **No drag-and-drop reorder** of sessions in the sidebar tree.

## Configuration Fix (2026-07-09)

**Problem:** Setting `"gui.layout": "sidebar"` in `~/.meept/client.json5` was not respected by the Flutter app. The app read from Flutter's SharedPreferences only, ignoring the file-based config.

**Fix:** Modified `StorageService` to:
1. Read `~/.meept/client.json5` at startup using `platformService.readFile()`
2. Parse the `gui.layout` value via regex
3. Cache the file-based value and use it as a fallback when SharedPreferences has no value

**Files modified:**
- `ui/flutter_ui/lib/services/storage_service.dart` - Added `loadGuiLayoutFromFile()` and `_cachedClientGuiLayout`

**Priority:** SharedPreferences (set via UI) > client.json5 file > default ("toptabs")

## How to Use

1. Open the Meept Flutter GUI
2. Edit `~/.meept/client.json5` and set `"gui": {"layout": "sidebar"}`
3. Restart the app (or switch back to `"toptabs"` and back to `"sidebar"` to see the live switch)
4. In sidebar mode:
   - The left panel shows all sessions in an expandable tree
   - Click a session to select it and view its chat in the main area
   - Click the expand/collapse arrows to show tasks and plans under a session
   - Click the [i] icon next to a session to open the session info overlay (plans, tasks, agents tabs)
   - The header bar shows the current session title, connection status, and hamburger menu for tools
