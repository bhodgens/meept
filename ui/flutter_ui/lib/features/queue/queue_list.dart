import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:timeago/timeago.dart' as timeago;
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import 'queue_tab.dart';

/// Queue jobs list — id / state / age / retries per row, tapping selects a
/// job for the detail pane. Columns mirror the TUI queue table
/// (internal/tui/models/queue.go updateTable).
class QueueList extends ConsumerStatefulWidget {
  const QueueList({super.key});

  @override
  ConsumerState<QueueList> createState() => _QueueListState();
}

class _QueueListState extends ConsumerState<QueueList> {
  @override
  Widget build(BuildContext context) {
    final view = ref.watch(queueViewProvider);
    final filter = ref.watch(queueFilterProvider);
    final jobs = _applyFilter(view.jobs, filter);

    return Container(
      decoration: BoxDecoration(
        border: Border(
          right: BorderSide(
            color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
            width: 1,
          ),
        ),
      ),
      child: Column(
        children: [
          Padding(
            padding: const EdgeInsets.all(16),
            child: Row(
              children: [
                Text(
                  'jobs',
                  style: CyberpunkTypography.headlineMedium.copyWith(
                    color: CyberpunkColors.orangePrimary,
                  ),
                ),
                const Spacer(),
                IconButton(
                  tooltip: 'refresh',
                  icon: const Icon(Icons.refresh, size: 18),
                  color: CyberpunkColors.orangePrimary,
                  onPressed: view.isLoading
                      ? null
                      : () => ref.read(queueViewProvider.notifier).refresh(),
                ),
              ],
            ),
          ),
          // Filter chips (TUI p/f/c/a keys).
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Wrap(
              spacing: 6,
              children: QueueFilter.values
                  .map(
                    (f) => ChoiceChip(
                      label: Text(
                        f.name,
                        style: CyberpunkTypography.bodySmall.copyWith(
                          fontFamily: 'SourceCodePro',
                        ),
                      ),
                      selected: filter == f,
                      selectedColor: CyberpunkColors.orangePrimary.withValues(
                        alpha: 0.3,
                      ),
                      onSelected: (_) =>
                          ref.read(queueFilterProvider.notifier).state = f,
                    ),
                  )
                  .toList(),
            ),
          ),
          const SizedBox(height: 8),
          if (view.isLoading && view.jobs.isEmpty)
            const Expanded(child: Center(child: CircularProgressIndicator()))
          else if (view.error != null && view.jobs.isEmpty)
            Expanded(
              child: Center(
                child: Text(
                  'error: ${view.error}',
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.redAlert,
                  ),
                  textAlign: TextAlign.center,
                ),
              ),
            )
          else if (jobs.isEmpty)
            const Expanded(child: Center(child: Text('no jobs')))
          else
            Expanded(
              child: ListView.builder(
                itemCount: jobs.length,
                itemBuilder: (context, index) =>
                    _buildJobTile(context, jobs[index]),
              ),
            ),
        ],
      ),
    );
  }

  List<QueueJobView> _applyFilter(List<QueueJobView> jobs, QueueFilter filter) {
    if (filter == QueueFilter.all) return jobs;
    return jobs.where((j) => j.state == filter.name).toList();
  }

  Widget _buildJobTile(BuildContext context, QueueJobView job) {
    final active = ref.watch(activeQueueJobProvider);
    final isSelected = active?.id == job.id;
    final stateColor = _getStateColor(job.state);

    return InkWell(
      onTap: () => ref.read(activeQueueJobProvider.notifier).state = job,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
        decoration: BoxDecoration(
          color: isSelected
              ? CyberpunkColors.orangeDark.withValues(alpha: 0.3)
              : null,
          border: Border(
            left: BorderSide(
              color: isSelected ? CyberpunkColors.orangeDark : stateColor,
              width: isSelected ? 3 : 2,
            ),
          ),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              job.shortId,
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.greenSuccess,
                fontFamily: 'SourceCodePro',
              ),
            ),
            const SizedBox(height: 4),
            Row(
              children: [
                Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 6,
                    vertical: 2,
                  ),
                  decoration: BoxDecoration(
                    color: stateColor.withValues(alpha: 0.2),
                    borderRadius: BorderRadius.circular(3),
                  ),
                  child: Text(
                    job.state,
                    style: CyberpunkTypography.bodySmall.copyWith(
                      color: stateColor,
                      fontFamily: 'SourceCodePro',
                    ),
                  ),
                ),
                const Spacer(),
                Text(
                  'retries: ${job.retryCount}/${job.maxRetries}',
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.midGray,
                    fontFamily: 'SourceCodePro',
                  ),
                ),
              ],
            ),
            const SizedBox(height: 4),
            Row(
              children: [
                Text(
                  '${job.type} · ${job.priorityName}',
                  style: CyberpunkTypography.bodySmall,
                ),
                const Spacer(),
                if (job.createdAt != null)
                  Text(
                    timeago.format(job.createdAt!),
                    style: CyberpunkTypography.bodySmall,
                  ),
              ],
            ),
          ],
        ),
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
