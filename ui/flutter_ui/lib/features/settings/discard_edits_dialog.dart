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

/// What the user chose when meept.json5 changed on disk under an editor.
enum StaleConfigChoice {
  /// Write the editor's text, replacing the change made elsewhere.
  overwrite,

  /// Drop the editor's text and take the copy the editor just re-read.
  reload,

  /// Keep the editor's text and write nothing.
  cancel,
}

/// Describe where [current] and [incoming] first differ, for the stale-file
/// prompt.
///
/// "the file changed" alone asks for a blind overwrite; naming the first
/// different line lets the user see what another writer (the orchestrator
/// block in the same panel, the CLI) actually changed. Returns '' when the two
/// texts are equal, which is the case the prompt is never shown for.
String describeFirstDifference(String current, String incoming) {
  final mine = current.split('\n');
  final theirs = incoming.split('\n');
  final lines = mine.length > theirs.length ? mine.length : theirs.length;
  for (var i = 0; i < lines; i++) {
    final left = i < mine.length ? mine[i] : null;
    final right = i < theirs.length ? theirs[i] : null;
    if (left != right) {
      return 'line ${i + 1}: the file has "${_abbreviate(right)}", '
          'your copy has "${_abbreviate(left)}"';
    }
  }
  return '';
}

/// One line, trimmed and elided, for [describeFirstDifference].
String _abbreviate(String? line) {
  if (line == null) return '(nothing: the file has one line fewer)';
  final trimmed = line.trim();
  if (trimmed.length <= 60) return trimmed;
  return '${trimmed.substring(0, 57)}...';
}

/// Ask before a whole-file save replaces a change another writer made.
///
/// `POST /api/v1/config/main` writes the whole document, so text captured at
/// load time silently reverts a save made since (the orchestrator block in the
/// same panel, the CLI, another window). The editor re-reads the file first
/// and asks here when the two disagree; every dismissal resolves to
/// [StaleConfigChoice.cancel], so nothing is written unless the user says so.
///
/// Pass [current] (the editor's loaded text) and [incoming] (the daemon's) to
/// have the prompt name the first differing line instead of asking for a blind
/// overwrite.
Future<StaleConfigChoice> showStaleConfigDialog(
  BuildContext context, {
  required String path,
  String? current,
  String? incoming,
}) async {
  final difference = (current == null || incoming == null)
      ? ''
      : describeFirstDifference(current, incoming);
  final choice = await showDialog<StaleConfigChoice>(
    context: context,
    builder: (context) => AlertDialog(
      backgroundColor: CyberpunkColors.darkGray,
      title: const Text(
        'config changed on disk',
        style: CyberpunkTypography.bodyMedium,
      ),
      content: Text(
        '$path changed after you loaded it (another editor in this panel or '
        'the CLI saved it).'
        '${difference.isEmpty ? '' : ' first difference - $difference.'} '
        'saving now writes the copy you loaded and replaces that change. '
        "reload to take the daemon's copy and discard your edits, or "
        'overwrite to write yours anyway.',
        style: CyberpunkTypography.bodySmall,
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context, StaleConfigChoice.cancel),
          child: const Text('cancel'),
        ),
        TextButton(
          onPressed: () => Navigator.pop(context, StaleConfigChoice.reload),
          child: const Text('reload'),
        ),
        TextButton(
          onPressed: () => Navigator.pop(context, StaleConfigChoice.overwrite),
          child: const Text('overwrite'),
        ),
      ],
    ),
  );
  return choice ?? StaleConfigChoice.cancel;
}
