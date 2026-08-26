# Leaf 05 — TUI + Flutter GUI multi-user config parity

DISPATCH INSTRUCTION: Implement this leaf end-to-end. Do NOT commit.
Parent: master.md. Depends on: 01-auth-store (REVIEWED). Est. context ~70K.

## Goal

Configuration tooling parity (AGENTS.md rule): both the TUI and the Flutter
GUI expose multi-user settings and key management views.

## Scope

1. **TUI** (`internal/tui/`): a "users" section in the existing
   settings/config surface listing users and keys (id, label, expiry),
   with add/revoke actions shelling through to the same store access as the
   CLI (inspect how tui invokes daemon operations today; follow it). All
   text lowercase; bubblezone-clickable rows.
2. **Flutter GUI** (`ui/flutter_ui/`): a Users page in Settings: list users/
   keys, add user, add key (label + optional expiry date picker), revoke.
   Uses the HTTP API if leaf 02 exposed endpoints — otherwise read-only
   display of current identity + link to CLI instructions. Web-compatible:
   no dart:io top-level imports; use PlatformService patterns.
3. Both surfaces show a clear "multi-user is disabled" empty state pointing
   at `multiuser.enabled`.

## Contract

- Identical capability set across TUI and GUI (parity invariant); any
  deviation must be documented in your report with justification.
- Never display raw keys after creation moment (GUI shows once via dialog;
  TUI prints once).
- No new bus topics required for v1.

## Tasks

1. Survey existing TUI config screens + GUI settings pages; match their
   navigation patterns.
2. Implement TUI view + actions; test with `go test ./internal/tui/...`
   where the pattern supports it.
3. Implement Flutter page; run `flutter analyze` inside ui/flutter_ui;
   audit-dart-enum-name-shadow script must stay clean
   (`python3 scripts/audit-dart-enum-name-shadow.py ui/flutter_ui/lib`).
4. Empty-state handling when disabled.

## Self-Verification Checklist

- [ ] flutter analyze clean; enum-shadow audit clean
- [ ] go build ./... ; tui tests green
- [ ] kIsWeb guards present around any file/platform code
- [ ] Lowercase UI text throughout both surfaces
- [ ] Raw keys never rendered post-creation

## Review Checklist (orchestrator)

- [ ] Parity checklist: every TUI action has GUI equivalent or documented
      deviation
- [ ] No dart:io at import top-level of shared files
