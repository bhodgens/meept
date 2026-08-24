import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../providers/providers.dart';
import '../../theme/colors.dart';
import '../../widgets/background_image.dart';
import 'sessions_list.dart';
import 'sessions_detail.dart';

/// Sessions overview tab - master-detail view with list and detail panes
class SessionsOverviewTab extends StatelessWidget {
  const SessionsOverviewTab({super.key});

  @override
  Widget build(BuildContext context) {
    return Consumer(
      builder: (context, ref, _) {
        final activeSession = ref.watch(activeSessionProvider);
        return Row(
          children: [
            const SizedBox(width: 280, child: SessionsList()),
            const VerticalDivider(width: 1),
            Expanded(
              child: BackgroundImage(
                child: activeSession != null
                    ? SessionsDetailPane(session: activeSession)
                    : Center(
                        child: Text(
                          'select a session',
                          style: TextStyle(
                            color: CyberpunkColors.orangePrimary,
                          ),
                        ),
                      ),
              ),
            ),
          ],
        );
      },
    );
  }
}
