---
name: W0-tokens-and-generator.md
description: Wave 0 — role census, theme/tokens.json5, embedding (in-session, orchestrator-owned)
version: 1.0.0
---

# W0 — Token foundation (orchestrator in-session)

Small, contract-critical, and everything depends on it: do directly, no subagent.

## Tasks

1. **Role census.** Map every distinct color used by both surfaces to a semantic
   role. Known inputs:
   - Flutter members (18): black, darkGray, midGray, lightGray, veryLightGray,
     orangePrimary, orangeBright, orangeDark, orangeGlow, orangeAccent,
     cyanAccent, greenSuccess, redAlert, yellowWarning, blueInfo, terminalGreen,
     terminalAmber + gradients.
   - TUI roles (10): Primary #F97316, Secondary #10B981, Accent #F59E0B,
     Error #EF4444, Warning #F59E0B, Success #10B981, Muted #6B7280,
     Foreground #E5E7EB, Background #1F2937, Border #374151.
   - TUI stragglers: #06B6D4 (cyan/info), #3B82F6 (blue), #8B5CF6/#EC4899
     (purple/pink), #9ece6a/#c0caf5/#e0af68/#f7768e/#565f89/#7dc4ff (tokyonight),
     #FFFFFF, #000000, #9CA3AF, #DC2626, #9A3412, #4B5563.

2. **Write `theme/tokens.json5`** with variants `cyberpunk`, `midnight`,
   `solarized`. cyberpunk values MUST reproduce today's GUI hexes exactly for
   the shared roles (no visual diff): background #000000, surface #1A1A1A,
   surfaceAlt #2A2A2A, border #333333, textPrimary #FF6600-forwarding rules per
   role table below. TUI-only roles get their current TUI hex under cyberpunk
   EXCEPT primary-family roles where the issue demands convergence: use the GUI
   value (#FF6600) as the single source; TUI styles change is accepted visual
   drift documented in L2.
   Frozen role list (both files must define exactly these):
   primary, primaryBright, primaryDark, primaryGlow, accent, secondary,
   success, warning, error, info, background, surface, surfaceAlt, border,
   textPrimary, textMuted, terminalGreen, terminalAmber.
   (cyanAccent→info, blueInfo→info fallback mapping lives in the consumer.)

3. **Embedding**: Go side reads it via `//go:embed ../../theme/tokens.json5`
   is impossible (embed can't cross module root upward) → instead add
   `theme/embed.go` (package theme, `//go:embed tokens.json5`) and have
   internal/tui import `github.com/bhodgens/meept/theme`. Flutter reads it via
   pubspec asset entry `../theme/tokens.json5`? No — assets cannot escape the
   package dir either → **copy step is forbidden** (drift risk). Resolution:
   keep canonical file at repo-root `theme/tokens.json5`; Go embeds via the
   `theme` package; Flutter loads at runtime from
   `Asset('packages/…')` is also unavailable → Flutter generator exception:
   a tiny checked-in Dart file `lib/theme/tokens_data.dart` containing the
   same map as a const literal, PLUS a consistency test that parses
   theme/tokens.json5 and asserts tokens_data.dart matches byte-for-byte on
   role names+hexes (L5 owns that test). This is the single allowed
   duplication, guarded by test.

## Verification

- `go build ./theme/...` passes.
- tokens.json5 parses with hujson (add `theme/tokens_test.go`: parse, assert
  all three variants contain exactly the frozen 18 roles).
- `python3 -c "import json5"`-free check not needed; rely on go test.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All scope items implemented at the exact owned paths
- [ ] Frozen interface contracts (master) satisfied
- [ ] Tests written and passing (see Tests/Verify above)
- [ ] No scope creep beyond this leaf's ownership
- [ ] No debug artifacts; no line-number corruption

**DO NOT COMMIT.** The orchestrator handles all git operations after
review.

## Review Checklist (For Review Agent)

- [ ] Every scope item implemented; tests present and passing
- [ ] Contracts match the master's frozen list exactly
- [ ] File ownership respected (no overlap with sibling leaves)
- [ ] Repo conventions honored (lowercase UI text, error handling, no new deps)

Output: APPROVED or list of specific gaps with file + line references.
