import 'package:flutter/material.dart';

import '../../theme/colors.dart';
import '../../theme/typography.dart';

/// Shared "you have unsaved edits" confirmation for the settings panel.
///
/// Three call-sites drop edited config text if the user proceeds, so all
/// three go through this one dialog - wording and button labels cannot drift
/// apart the way three hand-written copies would:
///   - [MainConfigEditor]'s reload control,
///   - [SettingsPanel]'s config-file chip switch,
///   - [SettingsPanel]'s exit guard, which the shared back control and the
///     esc key consult before leaving the panel.
///
/// Returns `true` only when the user picks the confirm button. Every other
/// dismissal (cancel, barrier tap, esc) resolves `false`, so callers treat
/// "not true" as "keep the edits".
Future<bool> showDiscardEditsDialog(
  BuildContext context, {
  required String message,
  String confirmLabel = 'discard',
}) async {
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      backgroundColor: CyberpunkColors.darkGray,
      title: const Text(
        'discard unsaved edits?',
        style: CyberpunkTypography.bodyMedium,
      ),
      content: Text(message, style: CyberpunkTypography.bodySmall),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context, false),
          child: const Text('cancel'),
        ),
        TextButton(
          onPressed: () => Navigator.pop(context, true),
          child: Text(confirmLabel),
        ),
      ],
    ),
  );
  return confirmed ?? false;
}
