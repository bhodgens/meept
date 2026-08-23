import 'package:flutter/material.dart';

/// Dialog shown when a session loads without a project bound.
class ProjectPromptDialog extends StatefulWidget {
  final String sessionID;
  final String defaultPath;

  const ProjectPromptDialog({
    super.key,
    required this.sessionID,
    required this.defaultPath,
  });

  static Future<ProjectPromptResult?> show(
    BuildContext context, {
    required String sessionID,
    required String defaultPath,
  }) {
    return showDialog<ProjectPromptResult>(
      context: context,
      barrierDismissible: false,
      builder: (context) =>
          ProjectPromptDialog(sessionID: sessionID, defaultPath: defaultPath),
    );
  }

  @override
  State<ProjectPromptDialog> createState() => _ProjectPromptDialogState();
}

class _ProjectPromptDialogState extends State<ProjectPromptDialog> {
  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('No project bound'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text('This session has no project bound.'),
          const SizedBox(height: 16),
          const Text('Use current directory for agent execution?'),
          const SizedBox(height: 8),
          Text(
            widget.defaultPath,
            style: Theme.of(
              context,
            ).textTheme.bodySmall?.copyWith(fontFamily: 'monospace'),
            overflow: TextOverflow.ellipsis,
            maxLines: 2,
          ),
        ],
      ),
      actions: [
        TextButton(
          onPressed: () =>
              Navigator.of(context).pop(ProjectPromptResult.decline),
          child: const Text('No'),
        ),
        TextButton(
          onPressed: () => Navigator.of(context).pop(ProjectPromptResult.pick),
          child: const Text('Pick Project'),
        ),
        ElevatedButton(
          onPressed: () =>
              Navigator.of(context).pop(ProjectPromptResult.accept),
          child: const Text('Yes'),
        ),
      ],
    );
  }
}

enum ProjectPromptResult { accept, decline, pick }
