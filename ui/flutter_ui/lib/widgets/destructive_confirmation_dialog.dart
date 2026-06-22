import 'package:flutter/material.dart';

/// DestructiveConfirmationDialog renders a confirmation prompt for
/// destructive tool actions (mark_superseded, reject_claim, etc.).
class DestructiveConfirmationDialog extends StatefulWidget {
  final Map<String, dynamic> response;

  const DestructiveConfirmationDialog({
    super.key,
    required this.response,
  });

  @override
  State<DestructiveConfirmationDialog> createState() =>
      _DestructiveConfirmationDialogState();
}

class _DestructiveConfirmationDialogState
    extends State<DestructiveConfirmationDialog> {
  bool _showDetails = false;

  @override
  Widget build(BuildContext context) {
    final action = widget.response['action'] as String? ?? 'confirm';
    final summary = widget.response['summary'] as String? ?? '';
    final reversible = widget.response['reversible'] as bool? ?? false;
    final details = widget.response['details'] as Map<String, dynamic>?;

    return AlertDialog(
      title: Text('$action — confirm action'),
      content: SizedBox(
        width: 480,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(summary),
            const SizedBox(height: 16),
            if (details != null) ...[
              if (details['old_preview'] != null)
                _PreviewBlock(label: 'OLD', text: details['old_preview'].toString()),
              if (details['new_preview'] != null)
                _PreviewBlock(label: 'NEW', text: details['new_preview'].toString()),
              if (details['affected_edges'] != null)
                Padding(
                  padding: const EdgeInsets.only(top: 8),
                  child: Text('${details['affected_edges']} edges will be redirected.'),
                ),
            ],
            const SizedBox(height: 8),
            Text('reversible: ${reversible ? 'yes' : 'no'}'),
            if (_showDetails) ...[
              const SizedBox(height: 16),
              const Divider(),
              const Text('full details', style: TextStyle(fontWeight: FontWeight.bold)),
              ...widget.response.entries
                  .where((e) => e.key != 'details' && e.key != 'requires_confirmation')
                  .map((e) => Text('  ${e.key}: ${e.value}')),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text('cancel'),
        ),
        TextButton(
          onPressed: () => setState(() => _showDetails = !_showDetails),
          child: Text(_showDetails ? 'hide details' : 'view full details'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(true),
          child: const Text('confirm'),
        ),
      ],
    );
  }
}

class _PreviewBlock extends StatelessWidget {
  final String label;
  final String text;

  const _PreviewBlock({required this.label, required this.text});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(top: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('$label:', style: const TextStyle(fontWeight: FontWeight.bold)),
          Container(
            padding: const EdgeInsets.all(8),
            decoration: BoxDecoration(
              color: Theme.of(context).colorScheme.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(4),
            ),
            child: Text(text),
          ),
        ],
      ),
    );
  }
}
