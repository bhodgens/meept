# Integration notes — meept_lints custom_lint plugin

This directory contains a project-local `custom_lint` plugin that
flags the Dart `Enum.name` extension-shadowing bug class that bit us
in Round 6 of the codebase review.

## Status

- Rule implemented: `enum_name_shadowing` (see `enum_name_shadowing.dart`).
- **NOT WIRED** into the app's dependency graph.
- `analysis_options.yaml` already references `custom_lint` under
  `analyzer.plugins`, but the `custom_lint` package is missing from
  `pubspec.yaml`. That plugin entry is currently dangling and is a
  no-op until the package is added.

## Why the plugin is not wired yet

`flutter pub add --dev custom_lint --dry-run` reports that adding the
package would change **17 dependencies** including:

| Package          | Current  | After add |
|------------------|----------|-----------|
| analyzer         | 6.4.1    | 7.6.0     |
| analyzer_plugin  | (absent) | 0.12.0    |
| build_runner     | 2.4.13   | 2.5.4     |
| source_gen       | 1.5.0    | 2.0.0     |
| freezed          | 2.5.2    | 2.5.8     |

`analyzer` 6 -> 7 and `source_gen` 1 -> 2 are major version bumps
that can break consumers (freezed, json_serializable, build_runner
codegen). Wiring the plugin must happen in a dedicated commit so the
fallout can be triaged separately from feature work.

## Wiring steps (when ready)

1. Pick a clean commit point (no in-flight feature branches).
2. From `ui/flutter_ui/`:
   ```sh
   flutter pub add --dev custom_lint
   flutter pub add --dev meept_lints --path tools/lints
   flutter pub get
   ```
3. Add the rule name to `analysis_options.yaml`:
   ```yaml
   analyzer:
     plugins:
       - custom_lint
   custom_lint:
     rules:
       - enum_name_shadowing
   ```
4. Run `dart run custom_lint` and fix any new findings.
5. Run `flutter pub run build_runner build --delete-conflicting-outputs`
   to confirm codegen still works under analyzer 7.x.
6. Verify the full test suite passes (`flutter test`).

## Why not just use the Python audit script?

`scripts/audit-dart-enum-name-shadow.py` is the **primary** line of
defense and runs out-of-band (CI, pre-commit). It does not require
any Dart toolchain. Use it.

This `custom_lint` rule is a **complementary** in-editor warning that
fires at the moment a developer types the shadowing getter — earlier
feedback than a CI script. It is the proper long-term fix; the script
is a stopgap that works today.
