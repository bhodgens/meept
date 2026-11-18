/// Shared rename-session dialog.
///
/// Returns the trimmed new name, or null when the user cancels or enters an
/// empty name. Used by both the sessions list and the sidebar session row, so
/// the two rename surfaces stay identical.
library;

import 'package:flutter/material.dart';
import '../theme/colors.dart';
import '../theme/typography.dart';

/// Show the rename dialog for a session.
///
/// [currentTitle] pre-fills the text field. The returned future resolves to
/// the trimmed name, or null on cancel / empty input.
Future<String?> showRenameSessionDialog(
  BuildContext context, {
  required String currentTitle,
}) {
  return showDialog<String>(
    context: context,
    builder: (_) => RenameSessionDialog(currentTitle: currentTitle),
  );
}

/// Rename dialog: owns its TextEditingController so the controller's
/// lifetime matches the dialog widget's (a caller-owned controller is
/// disposed before the dialog's exit animation finishes rebuilding the
/// TextField, tripping use-after-dispose assertions).
class RenameSessionDialog extends StatefulWidget {
  final String currentTitle;

  const RenameSessionDialog({super.key, required this.currentTitle});

  @override
  State<RenameSessionDialog> createState() => _RenameSessionDialogState();
}

class _RenameSessionDialogState extends State<RenameSessionDialog> {
  late final TextEditingController _controller;

  @override
  void initState() {
    super.initState();
    _controller = TextEditingController(text: widget.currentTitle);
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  String? _submit() {
    final value = _controller.text.trim();
    if (value.isEmpty) return null;
    return value;
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      backgroundColor: CyberpunkColors.darkGray,
      title: Text(
        'rename session',
        style: CyberpunkTypography.bodyMedium.copyWith(
          color: CyberpunkColors.orangePrimary,
        ),
      ),
      content: TextField(
        controller: _controller,
        autofocus: true,
        style: CyberpunkTypography.bodyMedium,
        decoration: const InputDecoration(
          isDense: true,
          border: OutlineInputBorder(),
        ),
        onSubmitted: (_) => Navigator.pop(context, _submit()),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: Text(
            'cancel',
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.midGray,
            ),
          ),
        ),
        FilledButton(
          onPressed: () => Navigator.pop(context, _submit()),
          child: const Text('rename', style: CyberpunkTypography.bodyMedium),
        ),
      ],
    );
  }
}
