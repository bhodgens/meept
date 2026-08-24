import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../dialogs/project_prompt_dialog.dart';
import '../dialogs/directory_browser_dialog.dart';
import '../models/api_models.dart';
import 'providers.dart';

/// Helper to check session project binding and show prompt if needed.
class SessionProjectChecker {
  /// Check if session needs project prompt (no project bound).
  static bool needsProjectPrompt(Session session) {
    if (session.projectPath != null && session.projectPath!.isNotEmpty) {
      return false;
    }
    if (session.projectId != null && session.projectId!.isNotEmpty) {
      return false;
    }
    final cwd = session.detectionContext?.cwd;
    if (cwd != null && cwd.isNotEmpty) {
      return false;
    }
    return true;
  }

  /// Show project prompt dialog if needed. Returns true if the caller
  /// should proceed with session activation. [ref] resolves the SDK
  /// client for the "pick project" directory-binding flow.
  static Future<bool> checkAndPrompt({
    required BuildContext context,
    required WidgetRef ref,
    required Session session,
    required VoidCallback onSkip,
    required Future<void> Function(String?) onProjectBound,
  }) async {
    if (!needsProjectPrompt(session)) {
      return true;
    }

    final cwd = session.detectionContext?.cwd ?? '';
    final result = await ProjectPromptDialog.show(
      context,
      sessionID: session.id,
      defaultPath: cwd.isNotEmpty ? cwd : '(no project detected)',
    );

    if (result == null) {
      return false;
    }

    switch (result) {
      case ProjectPromptResult.accept:
        // User accepts using the detected CWD as the project.
        // Signal that the session now has a project bound via CWD.
        if (cwd.isNotEmpty) {
          onProjectBound(cwd);
        }
        return true;
      case ProjectPromptResult.decline:
        // User skips project context but proceeds with the session.
        onSkip();
        return true;
      case ProjectPromptResult.pick:
        // Open the daemon-side browser so the user picks any directory
        // on the daemon host; bind it via project.set (DetectFromPath).
        final client = ref.read(sdkClientProvider);
        // This is a static helper: [context] has no `mounted` of its own,
        // so capture it via the dialog's own result — show() returns null
        // when the route was popped, covering the unmounted case.
        // ignore: use_build_context_synchronously
        final picked = await DirectoryBrowserDialog.show(context);
        if (picked == null) {
          return false;
        }
        try {
          await client.setProject(sessionId: session.id, path: picked);
          onProjectBound(picked);
        } catch (_) {
          // Binding failed — still activate the session unbound rather
          // than stranding the user in the prompt.
          onSkip();
        }
        return true;
    }
  }
}
