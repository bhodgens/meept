import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/metrics_provider.dart';
import '../../models/api_models.dart';

/// Metrics panel - displays live queue depth, active agents, and job counts
/// (Task 19: Live metrics display)
class MetricsPanel extends ConsumerWidget {
  final bool compact;

  const MetricsPanel({super.key, this.compact = false});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(metricsProvider);

    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray,
        border: Border(
          top: BorderSide(
            color: CyberpunkColors.orangePrimary.withValues(alpha: 0.3),
            width: 1,
          ),
        ),
      ),
      child: state.isLoading
          ? const _MetricsLoading()
          : state.error != null
              ? _MetricsError(message: state.error!)
              : _MetricsContent(snapshot: state.current, compact: compact),
    );
  }
}

class _MetricsLoading extends StatelessWidget {
  const _MetricsLoading();

  @override
  Widget build(BuildContext context) {
    return const SizedBox(
      height: 60,
      child: Row(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          SizedBox(
            width: 14,
            height: 14,
            child: CircularProgressIndicator(
              strokeWidth: 2,
              valueColor: AlwaysStoppedAnimation<Color>(
                CyberpunkColors.orangePrimary,
              ),
            ),
          ),
          SizedBox(width: 8),
          Text(
            'loading metrics...',
            style: CyberpunkTypography.bodySmall,
          ),
        ],
      ),
    );
  }
}

class _MetricsError extends StatelessWidget {
  final String message;

  const _MetricsError({required this.message});

  @override
  Widget build(BuildContext context) {
    return Text(
      message,
      style: CyberpunkTypography.bodySmall.copyWith(
        color: CyberpunkColors.redAlert,
      ),
      maxLines: 2,
      overflow: TextOverflow.ellipsis,
    );
  }
}

class _MetricsContent extends StatelessWidget {
  final MetricsSnapshot? snapshot;
  final bool compact;

  const _MetricsContent({this.snapshot, required this.compact});

  @override
  Widget build(BuildContext context) {
    if (snapshot == null) return const SizedBox.shrink();

    if (compact) {
      return _buildCompactLayout();
    } else {
      return _buildFullGridView();
    }
  }

  Widget _buildFullGridView() {
    final queueDepth = snapshot!.queueDepth;
    final activeAgents = snapshot!.activeAgents;
    final runningJobs = snapshot!.runningJobs;
    final pendingJobs = snapshot!.pendingJobs;
    final totalJobs = snapshot!.totalJobs;
    final rps = snapshot!.requestsPerSec;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Text(
              'metrics',
              style: CyberpunkTypography.label.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
            const Spacer(),
            Row(
              children: [
                Container(
                  width: 6,
                  height: 6,
                  decoration: const BoxDecoration(
                    color: CyberpunkColors.greenSuccess,
                    shape: BoxShape.circle,
                  ),
                ),
                const SizedBox(width: 4),
                Text(
                  'live',
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.greenSuccess,
                  ),
                ),
              ],
            ),
          ],
        ),
        const SizedBox(height: 8),
        GridView.count(
          crossAxisCount: 2,
          mainAxisSpacing: 8,
          crossAxisSpacing: 8,
          shrinkWrap: true,
          physics: const NeverScrollableScrollPhysics(),
          childAspectRatio: 2.5,
          children: [
            _MetricTile(
              label: 'active agents',
              value: '$activeAgents',
              color: activeAgents > 0
                  ? CyberpunkColors.greenSuccess
                  : CyberpunkColors.midGray,
              icon: Icons.person_outline,
            ),
            _MetricTile(
              label: 'queue depth',
              value: '$queueDepth',
              color: queueDepth > 5
                  ? CyberpunkColors.redAlert
                  : queueDepth > 0
                      ? CyberpunkColors.yellowWarning
                      : CyberpunkColors.greenSuccess,
              icon: Icons.queue,
            ),
            _MetricTile(
              label: 'running',
              value: '$runningJobs',
              color: CyberpunkColors.blueInfo,
              icon: Icons.play_circle_outline,
            ),
            _MetricTile(
              label: 'pending',
              value: '$pendingJobs',
              color: CyberpunkColors.orangePrimary,
              icon: Icons.hourglass_empty,
            ),
            _MetricTile(
              label: 'total jobs',
              value: '$totalJobs',
              color: CyberpunkColors.lightGray,
              icon: Icons.list_alt,
            ),
            _MetricTile(
              label: 'req/sec',
              value: rps.toStringAsFixed(1),
              color: CyberpunkColors.cyanAccent,
              icon: Icons.speed,
            ),
          ],
        ),
      ],
    );
  }

  Widget _buildCompactLayout() {
    final queueDepth = snapshot!.queueDepth;
    final activeAgents = snapshot!.activeAgents;
    final runningJobs = snapshot!.runningJobs;
    final pendingJobs = snapshot!.pendingJobs;

    return Row(
      children: [
        _CompactMetricTile(label: 'agents', value: '$activeAgents'),
        const SizedBox(width: 12),
        _CompactMetricTile(label: 'queue', value: '$queueDepth'),
        const SizedBox(width: 12),
        _CompactMetricTile(label: 'running', value: '$runningJobs'),
        const SizedBox(width: 12),
        _CompactMetricTile(label: 'pending', value: '$pendingJobs'),
      ],
    );
  }
}

class _MetricTile extends StatelessWidget {
  final String label;
  final String value;
  final Color color;
  final IconData icon;

  const _MetricTile({
    required this.label,
    required this.value,
    required this.color,
    required this.icon,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
      decoration: BoxDecoration(
        color: CyberpunkColors.midGray,
        border: Border.all(
          color: color.withValues(alpha: 0.3),
          width: 1,
        ),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Row(
        children: [
          Icon(icon, size: 14, color: color),
          const SizedBox(width: 6),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(
                  label,
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: color.withValues(alpha: 0.7),
                  ),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
                Text(
                  value,
                  style: CyberpunkTypography.bodyMedium.copyWith(
                    color: color,
                    fontSize: 16,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _CompactMetricTile extends StatelessWidget {
  final String label;
  final String value;

  const _CompactMetricTile({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(
          label,
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.midGray,
          ),
        ),
        Text(
          value,
          style: CyberpunkTypography.bodyMedium.copyWith(
            color: CyberpunkColors.greenSuccess,
            fontSize: 14,
            fontWeight: FontWeight.bold,
          ),
        ),
      ],
    );
  }
}
