import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import 'queue_tab.dart';

/// Queue job detail pane — payload / error / retries for the selected job,
/// mirroring the TUI's job detail panel (internal/tui/models/queue.go
/// renderJobDetail).
class QueueDetail extends ConsumerWidget {
  const QueueDetail({super.key});

  static final _selectJobStyle = TextStyle(
    color: CyberpunkColors.orangePrimary,
  );

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final job = ref.watch(activeQueueJobProvider);
    if (job == null) {
      return Center(child: Text('select a job', style: _selectJobStyle));
    }

    final stateColor = _getStateColor(job.state);

    return Container(
      padding: const EdgeInsets.all(24),
      child: SingleChildScrollView(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Container(
                  width: 10,
                  height: 10,
                  decoration: BoxDecoration(
                    color: stateColor,
                    shape: BoxShape.circle,
                  ),
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    job.id,
                    style: CyberpunkTypography.headlineLarge.copyWith(
                      color: CyberpunkColors.orangePrimary,
                      fontFamily: 'SourceCodePro',
                    ),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 16),
            _row('state', job.state),
            _row('type', job.type),
            _row('priority', job.priorityName),
            _row('task', job.taskId.isEmpty ? '-' : job.taskId),
            _row('claimed by', job.claimedBy.isEmpty ? '-' : job.claimedBy),
            _row('retries', '${job.retryCount} / max ${job.maxRetries}'),
            if (job.createdAt != null)
              _row('created', job.createdAt!.toIso8601String()),
            const SizedBox(height: 24),
            Text(
              'error',
              style: CyberpunkTypography.label.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
            const SizedBox(height: 8),
            SelectableText(
              job.error.isEmpty ? '-' : job.error,
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: job.error.isEmpty
                    ? CyberpunkColors.midGray
                    : CyberpunkColors.redAlert,
              ),
            ),
            const SizedBox(height: 24),
            Row(
              children: [
                Text(
                  'payload',
                  style: CyberpunkTypography.label.copyWith(
                    color: CyberpunkColors.orangePrimary,
                  ),
                ),
                const Spacer(),
                if (job.payload != null)
                  IconButton(
                    tooltip: 'copy payload',
                    icon: const Icon(Icons.copy, size: 16),
                    color: CyberpunkColors.midGray,
                    onPressed: () => Clipboard.setData(
                      ClipboardData(text: formatQueuePayload(job.payload!)),
                    ),
                  ),
              ],
            ),
            const SizedBox(height: 8),
            SelectableText(
              job.payload == null || job.payload!.isEmpty
                  ? '-'
                  : formatQueuePayload(job.payload!),
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
                fontFamily: 'SourceCodePro',
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _row(String label, String value) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 110,
            child: Text(
              label,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
              ),
            ),
          ),
          Expanded(
            child: SelectableText(
              value,
              style: CyberpunkTypography.bodyMedium.copyWith(
                fontFamily: 'SourceCodePro',
              ),
            ),
          ),
        ],
      ),
    );
  }

  Color _getStateColor(String state) {
    switch (state) {
      case 'pending':
        return CyberpunkColors.yellowWarning;
      case 'claimed':
      case 'processing':
        return CyberpunkColors.blueInfo;
      case 'completed':
        return CyberpunkColors.greenSuccess;
      case 'failed':
      case 'dead':
        return CyberpunkColors.redAlert;
      default:
        return CyberpunkColors.midGray;
    }
  }
}
