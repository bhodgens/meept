---
name: L3-flutter-settings.md
description: GUI settings dropdown variants + persistence + live-swap widget test
version: 1.0.0
---

# L3 — Flutter settings surface

## Files owned

- `ui/flutter_ui/lib/features/settings/settings_panel.dart` (dropdown section
  ~lines 729–775 only)
- `ui/flutter_ui/lib/services/storage_service.dart` (only if L1 didn't add
  getUiTheme/setUiTheme — coordinate: L1 owns the service; L3 only consumes)
- `ui/flutter_ui/test/features/settings/theme_dropdown_test.dart` (new)

## Tasks

1. Replace single-entry dropdown items with all three shipped variants:
   cyberpunk, midnight, solarized. Delete the stale comment block explaining
   why variants were removed.
2. On save (existing `_saveSettings` flow at ~line 171): persist via
   StorageService.setUiTheme(value) AND best-effort PATCH
   /api/v1/config/client {"rendering":{"ui_theme":value}} through the existing
   client-prefs PATCH path if one is wired in this panel; if no PATCH helper
   exists in scope, local persistence alone is acceptable — note deviation.
3. Apply immediately on selection change (not only on save): set
   themeNameProvider + CyberpunkColors.setActive so the swap is live.

## Tests

theme_dropdown_test.dart:
- Pump MaterialApp with ProviderScope overrides; open settings panel;
  assert three dropdown entries exist.
- Select 'midnight'; assert themeNameProvider state == 'midnight' and
  MaterialApp.theme.primaryColor changed (widget test asserting palette swap
  changes rendered colors — the issue's Phase 1 gate).
- Persistence: mock StorageService, assert setUiTheme called with value.

## Verify

cd ui/flutter_ui && flutter analyze && flutter test test/features/settings/

## Constraints

Do not restructure the settings panel. Lowercase UI text.

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
