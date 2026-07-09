import 'package:flutter/material.dart';
import '../dialogs/project_prompt_dialog.dart';
import '../models/api_models.dart';

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
  /// should proceed with session activation.
  static Future<bool> checkAndPrompt({
    required BuildContext context,
    required Session session,
    required VoidCallback onSkip,
    required ValueSetter<String?> onProjectBound,
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
        // User wants to explicitly pick a project.
        // For now, just proceed with session activation (no project bound).
        // TODO: implement project picker navigation.
        onSkip();
        return true;
    }
  }
}
