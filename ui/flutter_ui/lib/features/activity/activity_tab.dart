import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'activity_list.dart';
import 'activity_recent_panel.dart';
import 'activity_findings_panel.dart';
import 'activity_drift_panel.dart';

/// Activity tab - matches TUI parity: sortable list + live recent panel +
/// findings + drift sections.
///
/// 05-navigation T4: the deep link `meept://session/<id>` lands on
/// `/activity?session=<id>` (deep_link.dart). The route builders never
/// forwarded that param, so it must be CONSUMED here, inside the tab
/// seam (GoRouterState), or it silently drops (gate deep_link_param_test).
class ActivityTab extends ConsumerWidget {
  const ActivityTab({super.key});

  /// The `session` query param when navigated via a deep link; null on a
  /// plain visit (the view stays unfiltered -- gate regression guard).
  static String? sessionFilterFor(BuildContext context) {
    try {
      final state = GoRouterState.of(context);
      final session = state.uri.queryParameters['session'];
      return (session == null || session.isEmpty) ? null : session;
    } catch (_) {
      // Outside a GoRouter context (widget tests, dialogs): no filter.
      return null;
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final sessionFilter = sessionFilterFor(context);
    return Row(
      children: [
        SizedBox(width: 300, child: ActivityList(sessionFilter: sessionFilter)),
        const VerticalDivider(width: 1),
        const Expanded(
          child: SingleChildScrollView(
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
