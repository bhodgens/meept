import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'activity_list.dart';
import 'activity_recent_panel.dart';
import 'activity_findings_panel.dart';
import 'activity_drift_panel.dart';

/// Activity tab - matches TUI parity: sortable list + live recent panel +
/// findings + drift sections.
class ActivityTab extends ConsumerWidget {
  const ActivityTab({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Row(
      children: [
        const SizedBox(width: 300, child: ActivityList()),
        const VerticalDivider(width: 1),
        Expanded(
          child: const SingleChildScrollView(
            child: Column(
              children: [
                ActivityRecentPanel(),
                ActivityFindingsPanel(),
                ActivityDriftPanel(),
              ],
            ),
          ),
        ),
      ],
    );
  }
}
