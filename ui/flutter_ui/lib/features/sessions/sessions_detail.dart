import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/api_models.dart';
import '../../providers/providers.dart';
import '../../providers/session_detail.dart';

/// Session detail pane - displays in-depth session information with tasks and plans
class SessionsDetailPane extends ConsumerStatefulWidget {
  final Session session;
  /// Optional: when provided, the pane refreshes from [sessionDetailFamily].
  /// Falls back to [session] while loading or on error.
  final String? sessionId;

  const SessionsDetailPane({
    super.key,
    required this.session,
    this.sessionId,
  });

  @override
  ConsumerState<SessionsDetailPane> createState() => _SessionsDetailPaneState();
}

class _SessionsDetailPaneState extends ConsumerState<SessionsDetailPane> {
  bool _loading = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    // Defer to next frame so we don't modify providers during the build
    // phase (initState runs inside the build lifecycle). Without this,
    // tests and strict mode throw "Tried to modify a provider while the
    // widget tree was building."
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _fetchRelatedItems();
    });
  }

  @override
  void didUpdateWidget(covariant SessionsDetailPane oldWidget) {
    super.didUpdateWidget(oldWidget);
    // Re-fetch related items when the session changes so the detail pane
    // doesn't show stale tasks/plans from the previously-selected session.
    // Check both the passed-in session and the sessionId (if any).
    final oldId = oldWidget.sessionId ?? oldWidget.session.id;
    final newId = widget.sessionId ?? widget.session.id;
    if (oldId != newId) {
      _fetchRelatedItems();
    }
  }

  Future<void> _fetchRelatedItems() async {
    setState(() => _loading = true);
    try {
      // Fetch tasks and plans for this session
      await ref.read(taskProvider.notifier).loadTasks();
      await ref.read(planProvider.notifier).loadPlans(
            sessionID: widget.sessionId ?? widget.session.id,
          );
      if (mounted) {
        setState(() {
          _loading = false;
          // Clear stale error on successful retry so the error banner
          // does not persist visually after the underlying issue is fixed.
          _error = null;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _loading = false;
          _error = e.toString();
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    // If a sessionId is provided, prefer the cached detail family's value
    // (falls back to the passed-in session while loading or on error).
    Session session = widget.session;
    if (widget.sessionId != null) {
      final cached = ref.watch(sessionDetailFamily(widget.sessionId!));
      if (cached.hasValue) {
        session = cached.value!;
      }
    }

    final taskState = ref.watch(taskProvider);
    final planState = ref.watch(planProvider);

    // Filter tasks and plans for this session
    final sessionTasks = taskState.tasks
        .where((t) => t.sessionId == session.id)
        .toList();
    final sessionPlans = planState.plans
        .where((p) => p.sourceSession == session.id || p.taskID == session.id)
        .toList();

    return Expanded(
      child: Container(
        padding: const EdgeInsets.all(24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              'session details',
              style: CyberpunkTypography.headlineMedium.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
            const SizedBox(height: 24),
            _buildDetailRow(
              'title',
              session.title.toLowerCase(),
            ),
            _buildDetailRow(
              'created',
              _formatDateTime(session.createdAt),
            ),
            if (session.lastActivity != null)
              _buildDetailRow(
                'last activity',
                _formatDateTime(session.lastActivity!),
              ),
            const SizedBox(height: 24),

            // Tasks section
            Text(
              'associated tasks',
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.orangePrimary,
                fontWeight: FontWeight.bold,
              ),
            ),
            const SizedBox(height: 8),
            if (_loading)
              const CircularProgressIndicator()
            else if (sessionTasks.isEmpty)
              Text(
                'no tasks',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.midGray,
                  fontStyle: FontStyle.italic,
                ),
              )
            else
              ...sessionTasks.map((t) => _buildTaskRow(t)),

            const SizedBox(height: 16),

            // Plans section
            Text(
              'associated plans',
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.orangePrimary,
                fontWeight: FontWeight.bold,
              ),
            ),
            const SizedBox(height: 8),
            if (_loading)
              const CircularProgressIndicator()
            else if (sessionPlans.isEmpty)
              Text(
                'no plans',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.midGray,
                  fontStyle: FontStyle.italic,
                ),
              )
            else
              ...sessionPlans.map((p) => _buildPlanRow(p)),

            if (_error != null) ...[
              const SizedBox(height: 16),
              Text(
                'error: $_error',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.redAlert,
                ),
              ),
            ],

            const Spacer(),
          ],
        ),
      ),
    );
  }

  Widget _buildDetailRow(String label, String value) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 16),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 100,
            child: Text(
              label,
              style: CyberpunkTypography.bodySmall,
            ),
          ),
          Expanded(
            child: Text(
              value,
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.greenSuccess,
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildTaskRow(Task task) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 4),
      child: Row(
        children: [
          _getStateIcon(task.status),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              task.title.toLowerCase(),
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.lightGray,
              ),
              overflow: TextOverflow.ellipsis,
            ),
          ),
          if ((task.completedJobs ?? 0) > 0 || (task.totalJobs ?? 0) > 0)
            Text(
              '${task.completedJobs}/${task.totalJobs}',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
                fontSize: 9,
              ),
            ),
        ],
      ),
    );
  }

  Widget _buildPlanRow(Plan plan) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 4),
      child: Row(
        children: [
          _getPlanStateIcon(plan.state),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              (plan.title.isEmpty ? plan.id : plan.title).toLowerCase(),
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.lightGray,
              ),
              overflow: TextOverflow.ellipsis,
            ),
          )
        ],
      ),
    );
  }

  Widget _getStateIcon(String state) {
    IconData icon;
    Color color;
    switch (state) {
      case 'completed':
        icon = Icons.check_circle;
        color = CyberpunkColors.greenSuccess;
        break;
      case 'failed':
        icon = Icons.error;
        color = CyberpunkColors.redAlert;
        break;
      case 'executing':
      case 'planning':
        icon = Icons.pending;
        color = CyberpunkColors.orangePrimary;
        break;
      default:
        icon = Icons.circle_outlined;
        color = CyberpunkColors.midGray;
    }
    return Icon(icon, size: 14, color: color);
  }

  Widget _getPlanStateIcon(String state) {
    IconData icon;
    Color color;
    switch (state) {
      case 'confirmed':
      case 'completed':
        icon = Icons.check_circle;
        color = CyberpunkColors.greenSuccess;
        break;
      case 'failed':
        icon = Icons.error;
        color = CyberpunkColors.redAlert;
        break;
      case 'executing':
      case 'approved':
        icon = Icons.pending;
        color = CyberpunkColors.orangePrimary;
        break;
      default:
        icon = Icons.circle_outlined;
        color = CyberpunkColors.midGray;
    }
    return Icon(icon, size: 14, color: color);
  }

  String _formatDateTime(DateTime date) {
    return '${date.year}-${date.month.toString().padLeft(2, '0')}-${date.day.toString().padLeft(2, '0')}';
  }
}
