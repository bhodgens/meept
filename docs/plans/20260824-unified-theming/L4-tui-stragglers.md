---
name: L4-tui-stragglers.md
description: Migrate raw lipgloss literals + string-consts across 17 TUI files to palette
version: 1.0.0
---

# L4 — TUI straggler migration

## Files owned (exact census 2026-08-24, 378 literal refs)

internal/tui/selfimprove.go, sidebar.go, slash_autocomplete.go, modal.go,
agents_panel.go, components/notification.go, components/sparkline.go,
prompts/prompts_view.go, viz/robot.go,
models/{chat,memory,tasks,sessions,queue,search,plans,status}.go

## Tasks

1. Replace `lipgloss.Color("#HEX")` and `lipgloss.Color(ColorAmber|Green|Red|
   Gray)` with palette lookups. Mapping (cyberpunk values):
   #F97316/#FF6600→primary; #10B981/#9ece6a→secondary/success;
   #F59E0B→warning; #EF4444/#DC2626/#f7768e→error; #06B6D4/#3B82F6/
   #7dc4ff→info; #6B7280/#565f89/#9CA3AF/#4B5563→textMuted;
   #374151→border; #E5E7EB/#c0caf5→textPrimary; #1F2937→surfaceAlt
   (background role is #000000 in cyberpunk — for the dark-gray panel fills
   use surfaceAlt); #FFFFFF→textPrimaryBright? NO such role — keep #FFFFFF as
   literal where it means "max contrast white" or map to textPrimary if
   visually equivalent — implementer judgment, note choices; #8B5CF6/#EC4899/
   #e0af68/#9A3412 have no role → leave as-is with a `// palette-exempt`
   comment (they're one-off viz accents).
2. Unfreeze init-time styles: selfimprove.go si* vars and models/sessions.go
   archivedRowStyle become funcs or are rebuilt lazily on first use reading
   Current() (simplest: convert var → func returning style, update ~10 call
   sites in same file).
3. Delete duplicated ColorAmber/Green/Red/Gray consts from BOTH constants.go
   and models/constants.go once no references remain (grep first; models
   package may still need them if referenced from tests — migrate tests too).
4. chat.go findStyles (~365): route through palette.

## Tests

- Existing suites must pass unchanged: go test ./internal/tui/...
- Add table test asserting every file in internal/tui has zero remaining
  unexempt `lipgloss.Color("` hex/string literals outside palette.go/styles.go
  (regex scan in a Go test — this is the lint-rule substitute; issue Phase 4).

## Verify

go build ./... && go test ./internal/tui/... && gofmt -l internal/tui

## Constraints

Purely mechanical color swaps — do not restyle borders/padding.
Do not touch styles.go/config.go/app.go/viz/colors.go (L2 owns).
