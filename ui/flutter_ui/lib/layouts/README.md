# Meept Flutter UI - Alternative Layout Concepts

This directory contains 5 static mockup layouts for the Meept Flutter UI. Each layout maintains:

- **Same cyberpunk theme**: Orange glow, dark backgrounds, SourceCodePro typography
- **Same core concepts**: chat, sessions, plans, tasks, agents navigation
- **Same effects**: Background image overlay, glow shadows, angular accents
- **Same status bar**: Connection status, session info, keyboard shortcuts

## Layout Options

### Layout 1: Classic Sidebar (`layout_1_classic_sidebar.dart`)

Traditional left sidebar navigation with vertical menu items.

**Structure:**
- Left: Fixed-width sidebar with navigation items
- Right: Header bar + message list + chat input + status bar

**Best for:** Users who prefer persistent navigation visibility and maximum content width.

```
+----------+--------------------------------+
|  LOGO    |  [Header: Session Title]       |
|          +--------------------------------+
|  chat    |                                |
|  sessions|     Main Content Area          |
|  plans   |                                |
|  tasks   |                                |
|  agents  +--------------------------------+
|          |  [Chat Input]                  |
+----------+--------------------------------+
|  [Status Bar]                            |
+------------------------------------------+
```

---

### Layout 2: Bottom Navigation (`layout_2_bottom_nav.dart`)

Horizontal navigation bar at the bottom, tabs-style.

**Structure:**
- Top: Header with project info and actions
- Middle: Full-width content area
- Bottom: Icon + label navigation bar

**Best for:** Tablet/touch interfaces, users who prefer thumb-accessible navigation.

```
+------------------------------------------+
|  [Header: Logo + Project + Actions]      |
+------------------------------------------+
|                                          |
|            Main Content Area             |
|                                          |
+------------------------------------------+
|  chat  |sessions|  plans  |  tasks  |agents
+------------------------------------------+
|  [Status Bar]                            |
+------------------------------------------+
```

---

### Layout 3: Radial Hub (`layout_3_radial_hub.dart`)

Futuristic radial navigation with central hub and orbiting buttons.

**Structure:**
- Top: Angular header bar with connection pill
- Center: Hub with radially positioned nav buttons
- Status bar at bottom

**Best for:** Cyberpunk aesthetic enthusiasts, keyboard-first users who want visual flair.

```
+------------------------------------------+
|  [Header: Logo + Status + Actions]       |
+------------------------------------------+
|                                          |
|         [nav]       [nav]                |
|                                          |
|              (Hub)                       |
|                                          |
|    [nav]             [nav]               |
|                                          |
|              [nav]                       |
+------------------------------------------+
|  [Status Bar]                            |
+------------------------------------------+
```

---

### Layout 4: Top Tabs with Side Panels (`layout_4_top_tabs_panels.dart`)

Traditional top tabs with collapsible tool panels on both sides.

**Structure:**
- Top: Logo + horizontal tab bar
- Left: Collapsible tools panel (search, memory, prompts)
- Right: Collapsible project panel (branches, files, terminal)
- Center: Main content area

**Best for:** Power users who want quick access to tools without navigation.

```
+------------------------------------------+
|  [Logo]   chat | sessions | plans | ...  |
+------------------------------------------+
| [Tools]   |                      | [Proj]|
|  search   |                      |branch |
|  memory   |   Main Content       | files |
|  prompts  |                      |terminal|
|           |                      |       |
+-----------+----------------------+-------+
|  [Status Bar]                          |
+-----------------------------------------+
```

---

### Layout 5: Grid Dashboard (`layout_5_grid_dashboard.dart`)

Card-based dashboard showing all sections at a glance.

**Structure:**
- Top: Logo header with status pill
- Center: 3x2 grid of feature cards
- Floating chat input bar
- FAB for quick actions

**Best for:** Overview-focused workflows, users who want quick context switching.

```
+------------------------------------------+
|  [Logo] meept            [Status Pill]   |
+------------------------------------------+
|                                          |
|  +--------+  +--------+  +--------+      |
|  | chat   |  |sessions|  | plans  |      |
|  | [icon] |  | [icon] |  | [icon] |      |
|  +--------+  +--------+  +--------+      |
|                                          |
|  +--------+  +--------+                  |
|  | tasks  |  | agents |                  |
|  | [icon] |  | [icon] |                  |
|  +--------+  +--------+                  |
+------------------------------------------+
|  [Floating Chat Bar]                     |
+------------------------------------------+
|  [Status Bar]                            |
+------------------------------------------+
```

---

## Theme Elements (All Layouts)

### Colors (from `theme/colors.dart`)
- Primary: `#FF6600` (Orange)
- Background: `#000000` (Black), `#1A1A1A` (Dark Gray)
- Accents: Cyan, Green, Red, Yellow, Blue
- Text: `#E0E0E0` (Very Light Gray)

### Typography (from `theme/typography.dart`)
- Font: SourceCodePro (monospace)
- All lowercase text
- Letter spacing: 1-3px for cyberpunk feel

### Effects (from `theme/effects.dart`)
- Orange glow shadows
- Angular gradient overlays
- Scanline effects
- Cut-corner decorations

### Background
- `assets/images/gui-bg.png` with opacity overlay
- Semi-transparent containers for content areas

## Next Steps

To prototype any of these layouts:

1. Review the static mockups
2. Select a layout to implement
3. Create implementation plan in `docs/superpowers/plans/`
4. Wire up to existing providers and services

## Comparison Table

| Layout | Nav Position | Panel Style | Best For |
|--------|--------------|-------------|----------|
| 1 - Sidebar | Left vertical | None | Traditional desktop |
| 2 - Bottom | Bottom horizontal | None | Touch/tablet |
| 3 - Radial | Center radial | None | Visual flair |
| 4 - Panels | Top tabs | Collapsible sides | Power users |
| 5 - Grid | N/A (dashboard) | Cards | Overview focus |
