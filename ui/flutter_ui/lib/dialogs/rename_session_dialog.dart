/// Shared rename-session dialog.
///
/// Returns the trimmed new name, or null when the user cancels. An empty
/// submit keeps the dialog open with an inline error (M12 — it used to
/// close silently, indistinguishable from cancel). Used by both the
/// sessions list and the sidebar session row, so the two rename surfaces
/// stay identical.
library;

import 'package:flutter/material.dart';
import '../theme/colors.dart';
import '../theme/typography.dart';

/// Show the rename dialog for a session.
///
/// [currentTitle] pre-fills the text field. The returned future resolves to
/// the trimmed name, or null on cancel. Empty input never resolves the
/// dialog — it shows an inline error and stays open (M12, TUI parity).
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

  /// Inline error shown when the user submits an empty name (M12).
  String? _errorText;

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

  /// Submit the trimmed name. An empty value keeps the dialog open and
  /// surfaces the inline error instead of closing silently (M12 — parity
  /// with the TUI rename modal's empty-input refusal).
  void _submit() {
    final value = _controller.text.trim();
    if (value.isEmpty) {
      setState(() => _errorText = 'name cannot be empty');
      return;
    }
    Navigator.pop(context, value);
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
        decoration: InputDecoration(
          isDense: true,
          border: const OutlineInputBorder(),
          errorText: _errorText,
          errorStyle: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.redAlert,
          ),
        ),
        onChanged: (_) {
          // Clear the error as soon as the user types again.
          if (_errorText != null) setState(() => _errorText = null);
        },
        onSubmitted: (_) => _submit(),
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
          onPressed: _submit,
          child: const Text('rename', style: CyberpunkTypography.bodyMedium),
        ),
      ],
    );
  }
}
