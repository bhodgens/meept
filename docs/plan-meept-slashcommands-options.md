# Plan: Meept Slash Command Autocomplete Options

**Status:** Research Complete
**Date:** 2026-04-22
**Problem:** Terminal UI limitations prevent true overlay-style popup menus

---

## Context

The Meept TUI (built on Bubble Tea v2) needs slash command autocomplete functionality. The desired behavior is a popup menu that:
- Appears when typing `/` at the start of input
- Floats above the viewport content (overlay/Z-index layer)
- Aligns with the cursor position
- Shows filtered command suggestions
- Can be navigated with arrow keys

**Core Problem:** Bubble Tea uses a sequential rendering model (top-to-bottom string building) with no support for layered/overlay rendering or absolute positioning.

---

## Option Analysis

### Option 1: Squeeze-Based Layout (Bubble Tea Native)

The popup exists in the layout flow; other elements shrink to make room.

**Implementation:**
- Calculate popup height dynamically when visible
- Reduce viewport height by popup height
- Render popup between viewport and input

**Pros:**
- Works within Bubble Tea's model
- No external dependencies
- Reliable rendering

**Cons:**
- Layout shifts when popup appears (viewport shrinks)
- Popup cannot visually overlap content
- Feels less "native" than GUI-style dropdowns

**Status:** Partially implemented; viewport height calculation needs fixing.

---

### Option 2: Full-Screen Grid Rebuild (Bubble Tea Hack)

Build the screen as a 2D grid of characters, insert popup at calculated position, overwrite underlying content.

**Implementation:**
- Render viewport, input, statusbar to separate buffers
- Combine into 2D character grid
- Draw popup lines at calculated Y position, overwriting viewport lines
- Convert grid back to string for Bubble Tea

**Pros:**
- Achieves true overlay effect
- Stays within Bubble Tea ecosystem

**Cons:**
- Complex implementation
- Fragile (breaks with scrolling, resizing)
- ANSI code handling is tricky
- Fighting against framework model

**Status:** Not implemented; high complexity, uncertain reliability.

---

### Option 3: tview Migration

Migrate from Bubble Tea to [tview](https://github.com/rivo/tview), a widget-based TUI library built on tcell.

**Implementation:**
- Rewrite TUI layer using tview primitives
- Use `SetDrawFunc()` for custom overlay rendering
- Implement autocomplete as custom primitive or draw function

**Example:**
```go
app.SetDrawFunc(func(screen tcell.Screen) tcell.Screen {
    // Draw popup at absolute coordinates
    for y := popupTop; y < popupBottom; y++ {
        for x := popupLeft; x < popupRight; x++ {
            screen.SetContent(x, y, ch, style)
        }
    }
    return screen
})
```

**Pros:**
- True overlay/absolute positioning support
- Mature library with good documentation
- Widget-based paradigm familiar to GUI developers

**Cons:**
- Significant rewrite of existing TUI code
- Different architecture (widgets vs. functional update model)
- Loss of Bubble Tea features (vim mode integration, existing components)

**Estimated Effort:** 40-80 hours for full migration

---

### Option 4: Hybrid Bubble Tea + tcell

Use Bubble Tea for main UI, drop to tcell for popup rendering.

**Implementation:**
- Capture tcell screen reference before Bubble Tea render
- After Bubble Tea renders, draw popup directly to screen
- Force screen refresh

**Pros:**
- Keep Bubble Tea for most logic
- Get low-level overlay capability

**Cons:**
- Complex integration
- Race conditions with Bubble Tea's render cycle
- May cause flicker
- Fighting the framework

**Status:** Not recommended; integration complexity outweighs benefits.

---

### Option 5: Terminal Scroll Workaround

When popup appears, scroll viewport content up to make room, creating illusion of overlay.

**Implementation:**
- On popup show: scroll viewport up by popup height
- Render popup in freed space
- On popup hide: scroll viewport back down

**Pros:**
- No layout recalculation
- Popup appears in consistent screen position

**Cons:**
- Complex scroll state management
- May lose user's scroll position
- Jarring visual effect

**Status:** Not implemented; UX concerns.

---

### Option 6: External Popup Process

Render popup as separate terminal output using ANSI escapes, outside Bubble Tea's control.

**Implementation:**
- Calculate popup position
- Output ANSI cursor positioning + popup text directly to stdout
- Hope Bubble Tea doesn't overwrite it

**Cons:**
- Fundamental conflict with Bubble Tea's full-screen redraw model
- Will flicker or be overwritten
- **Not viable**

---

## Recommendation

**Short-term:** Fix Option 1 (squeeze-based layout). Accept the terminal-native paradigm where overlays don't exist.

**Medium-term:** If overlay is truly required, migrate to tview (Option 3).

**Long-term:** Watch Bubble Tea v2 for layer support; consider contributing to the project.

---

## Non-Popup Alternatives

See separate analysis in `docs/plan-slashcommand-alternatives.md` for:
- Inline completion (fish-shell style)
- Status bar completions
- Expandable input hints
- Tab-cycling completion

---

## Key Insight

Terminal UIs are **document-based**, not **layer-based**. The mental model shift is:

> Instead of "popup floating over content," think "document that temporarily includes a popup section."

This is why Vim, tmux, and other terminal tools use squeeze-based menus rather than overlays. It's not a limitation—it's the terminal-native pattern.

---

## Decision Criteria

| Criteria | Weight | Option 1 | Option 2 | Option 3 | Option 5 |
|----------|--------|----------|----------|----------|----------|
| Implementation effort | High | ✅ | ❌ | ❌ | ⚠️ |
| Visual quality | Medium | ⚠️ | ✅ | ✅ | ⚠️ |
| Maintainability | High | ✅ | ❌ | ✅ | ❌ |
| Framework alignment | High | ✅ | ❌ | N/A | ❌ |
| User experience | High | ⚠️ | ✅ | ✅ | ❌ |

**Legend:** ✅ = Good, ⚠️ = Acceptable, ❌ = Poor, N/A = Different framework

---

## Next Steps

1. **If overlay is required:** Plan tview migration
2. **If squeeze is acceptable:** Fix current implementation
3. **If exploring alternatives:** Document non-popup patterns
