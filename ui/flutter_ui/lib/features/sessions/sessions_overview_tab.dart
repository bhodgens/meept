import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/session.dart';
import 'sessions_list.dart';
import 'sessions_detail.dart';

/// Sessions overview tab - master-detail view with list and detail panes
class SessionsOverviewTab extends StatefulWidget {
  const SessionsOverviewTab({super.key});

  @override
  State<SessionsOverviewTab> createState() => _SessionsOverviewTabState();
}

class _SessionsOverviewTabState extends State<SessionsOverviewTab> {
  Session? _selectedSession;

  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.black,
      child: Row(
        children: [
          SessionsList(
            sessions: _getSessions(),
            selectedSessionId: _selectedSession?.id,
            onSessionSelected: (sessionId) {
              setState(() {
                _selectedSession = _getSessionById(sessionId);
              });
            },
          ),
          if (_selectedSession != null)
            SessionsDetailPane(session: _selectedSession!),
        ],
      ),
    );
  }

  List<Session> _getSessions() {
    // TODO: Replace with Riverpod provider
    return [
      Session(
        id: 'session-001',
        title: 'API Integration Task',
        createdAt: DateTime.now().subtract(const Duration(hours: 2)),
        lastActivityAt: DateTime.now(),
        duration: const Duration(hours: 1, minutes: 45, seconds: 30),
        tokenCount: 12500,
        taskIds: ['task-001', 'task-002'],
        status: 'active',
      ),
      Session(
        id: 'session-002',
        title: 'debugging flutter ui',
        createdAt: DateTime.now().subtract(const Duration(days: 1)),
        lastActivityAt: DateTime.now().subtract(const Duration(hours: 5)),
        duration: const Duration(minutes: 45),
        tokenCount: 5200,
        taskIds: ['task-003'],
        status: 'completed',
      ),
    ];
  }

  Session? _getSessionById(String id) {
    final sessions = _getSessions();
    try {
      return sessions.firstWhere((s) => s.id == id);
    } catch (e) {
      return null;
    }
  }
}
