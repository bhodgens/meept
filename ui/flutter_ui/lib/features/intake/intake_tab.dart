import 'package:flutter/material.dart';

import 'intake_status_view.dart';

/// Intake tab - GUI twin of the TUI's /intake report (issue #109 parity
/// for the github-intake [intake] surface; structure mirrors
/// features/activity/activity_tab.dart). Read-only over the
/// GET /api/v1/intake/status bridge.
class IntakeTab extends StatelessWidget {
  const IntakeTab({super.key});

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: const BoxDecoration(
        border: Border(left: BorderSide(color: Colors.black26, width: 1)),
      ),
      child: const IntakeStatusView(),
    );
  }
}
