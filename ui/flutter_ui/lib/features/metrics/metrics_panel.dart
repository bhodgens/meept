import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/metrics_provider.dart';
import '../../models/api_models.dart';
import '../../widgets/tool_panel_shell.dart';

/// Metrics panel - live queue depth, active agents, job counts, and
/// (new) per-model / per-agent usage from GET /api/v1/metrics/live.
///
/// The panel body is wrapped in the shared [ToolPanelShell], which supplies
/// the back control and the ESC handler, and exposes a manual refresh
/// action. All optional usage sections (`models`, `agents`, `totals`)
/// degrade to a lowercase note when the daemon does not report them, so the
/// existing tiles always render.
class MetricsPanel extends ConsumerWidget {
  final bool compact;

  const MetricsPanel({super.key, this.compact = false});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(metricsProvider);

    // The compact row variant is an inline summary (no header chrome).
    if (compact) {
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
        child: _buildContent(state, compact: true),
      );
    }

    return ToolPanelShell(
      title: 'metrics',
      icon: Icons.query_stats,
      actions: [
        IconButton(
          icon: const Icon(Icons.refresh, size: 18),
          color: CyberpunkColors.orangePrimary,
          tooltip: 'refresh metrics',
          onPressed: () => ref.read(metricsProvider.notifier).refresh(),
        ),
      ],
      child: _buildContent(state, compact: false),
    );
  }

  Widget _buildContent(MetricsState state, {required bool compact}) {
    // Loading only takes over the panel before the first payload arrives.
    if (state.current == null) {
      if (state.isLoading) return const _MetricsLoading();
      if (state.error != null) return _MetricsError(message: state.error!);
      return const _MetricsEmpty(message: 'no metrics reported yet');
    }

    if (compact) {
      return _MetricsContent(state: state, compact: true);
    }

    // With data present, an error is a soft inline note - never a
    // whole-panel red error that hides the live tiles.
    return _MetricsContent(state: state, compact: false);
  }
}

class _MetricsLoading extends StatelessWidget {
  const _MetricsLoading();

  @override
  Widget build(BuildContext context) {
    return Center(
      child: SizedBox(
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
            const SizedBox(width: 8),
            const Text(
              'loading metrics...',
              style: CyberpunkTypography.bodySmall,
            ),
          ],
        ),
      ),
    );
  }
}

class _MetricsError extends StatelessWidget {
  final String message;

  const _MetricsError({required this.message});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Text(
        message.toLowerCase(),
        textAlign: TextAlign.center,
        style: CyberpunkTypography.bodySmall.copyWith(
          color: CyberpunkColors.redAlert,
        ),
        maxLines: 3,
        overflow: TextOverflow.ellipsis,
      ),
    );
  }
}

class _MetricsEmpty extends StatelessWidget {
  final String message;

  const _MetricsEmpty({required this.message});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Text(
        message,
        style: CyberpunkTypography.bodySmall.copyWith(
          color: CyberpunkColors.midGray,
        ),
      ),
    );
  }
}

/// A lowercase note used when an optional usage section is absent or empty.
class _SectionNote extends StatelessWidget {
  final String message;

  const _SectionNote(this.message);

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Text(
        message,
        style: CyberpunkTypography.bodySmall.copyWith(
          color: CyberpunkColors.midGray,
          fontStyle: FontStyle.italic,
        ),
      ),
    );
  }
}

class _MetricsContent extends StatelessWidget {
  final MetricsState state;
  final bool compact;

  const _MetricsContent({required this.state, required this.compact});

  @override
  Widget build(BuildContext context) {
    if (compact) return _buildCompactLayout();

    final snapshot = state.current!;
    final updatedAt = state.receivedAt ?? snapshot.timestamp;

    return SingleChildScrollView(
      padding: const EdgeInsets.all(12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _buildStatusRow(updatedAt),
          if (state.error != null) ...[
            const SizedBox(height: 6),
            _SectionNote('last refresh failed: ${state.error!.toLowerCase()}'),
          ],
          const SizedBox(height: 10),
          _buildTiles(snapshot),
          const SizedBox(height: 16),
          _buildTotalsSection(),
          const SizedBox(height: 16),
          _buildModelsSection(),
          const SizedBox(height: 16),
          _buildAgentsSection(),
        ],
      ),
    );
  }

  Widget _buildStatusRow(DateTime updatedAt) {
    return Row(
      children: [
        Container(
          width: 6,
          height: 6,
          decoration: BoxDecoration(
            color: CyberpunkColors.greenSuccess,
            shape: BoxShape.circle,
          ),
        ),
        const SizedBox(width: 6),
        Text(
          'live',
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.greenSuccess,
          ),
        ),
        const Spacer(),
        Text(
          'updated ${_formatClock(updatedAt)}',
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.midGray,
          ),
        ),
      ],
    );
  }

  Widget _buildTiles(MetricsSnapshot snapshot) {
    final queueDepth = snapshot.queueDepth;
    final activeAgents = snapshot.activeAgents;

    final tiles = <Widget>[
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
        value: '${snapshot.runningJobs}',
        color: CyberpunkColors.blueInfo,
        icon: Icons.play_circle_outline,
      ),
      _MetricTile(
        label: 'pending',
        value: '${snapshot.pendingJobs}',
        color: CyberpunkColors.orangePrimary,
        icon: Icons.hourglass_empty,
      ),
      _MetricTile(
        label: 'total jobs',
        value: '${snapshot.totalJobs}',
        color: CyberpunkColors.lightGray,
        icon: Icons.list_alt,
      ),
      _MetricTile(
        label: 'req/sec',
        value: snapshot.requestsPerSec.toStringAsFixed(1),
        color: CyberpunkColors.cyanAccent,
        icon: Icons.speed,
      ),
    ];

    // Wrap (not GridView aspect-ratio boxes) so narrow viewports reflow to
    // a single column without a RenderFlex overflow.
    return LayoutBuilder(
      builder: (context, constraints) {
        final columns = constraints.maxWidth < 360 ? 1 : 2;
        const spacing = 8.0;
        final tileWidth =
            (constraints.maxWidth - spacing * (columns - 1)) / columns;
        return Wrap(
          spacing: spacing,
          runSpacing: spacing,
          children: [
            for (final tile in tiles) SizedBox(width: tileWidth, child: tile),
          ],
        );
      },
    );
  }

  Widget _buildTotalsSection() {
    final totals = state.totals;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const _SectionLabel('totals'),
        if (totals == null)
          const _SectionNote('usage totals not reported by this daemon')
        else
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              _TotalTile(
                label: 'total calls',
                value: _formatCount(totals.calls),
              ),
              _TotalTile(
                label: 'tokens in',
                value: _formatCount(totals.tokensIn),
              ),
              _TotalTile(
                label: 'tokens out',
                value: _formatCount(totals.tokensOut),
              ),
            ],
          ),
      ],
    );
  }

  Widget _buildModelsSection() {
    final models = state.modelUsage;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const _SectionLabel('model usage'),
        if (models == null)
          const _SectionNote('model usage not reported by this daemon')
        else if (models.isEmpty)
          const _SectionNote('no model usage in the last 24h')
        else
          _UsageTable(
            headers: const [
              'model',
              'calls',
              'tokens in',
              'tokens out',
              'avg latency',
            ],
            rows: [
              // Preserve daemon order - do not re-sort.
              for (final m in models)
                [
                  m.id,
                  _formatCount(m.calls),
                  _formatCount(m.tokensIn),
                  _formatCount(m.tokensOut),
                  _formatLatency(m.avgLatencyMs),
                ],
            ],
          ),
      ],
    );
  }

  Widget _buildAgentsSection() {
    final agents = state.agentUsage;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const _SectionLabel('agent usage'),
        if (agents == null)
          const _SectionNote('agent usage not reported by this daemon')
        else if (agents.isEmpty)
          const _SectionNote('no agents reported')
        else
          _UsageTable(
            headers: const ['agent', 'state', 'completed', 'failed'],
            rows: [
              for (final a in agents)
                [
                  a.id,
                  a.state.isEmpty ? '-' : a.state,
                  _formatCount(a.tasksCompleted),
                  _formatCount(a.tasksFailed),
                ],
            ],
          ),
      ],
    );
  }

  Widget _buildCompactLayout() {
    final snapshot = state.current!;
    return Row(
      children: [
        _CompactMetricTile(label: 'agents', value: '${snapshot.activeAgents}'),
        const SizedBox(width: 12),
        _CompactMetricTile(label: 'queue', value: '${snapshot.queueDepth}'),
        const SizedBox(width: 12),
        _CompactMetricTile(label: 'running', value: '${snapshot.runningJobs}'),
        const SizedBox(width: 12),
        _CompactMetricTile(label: 'pending', value: '${snapshot.pendingJobs}'),
      ],
    );
  }
}

class _SectionLabel extends StatelessWidget {
  final String text;

  const _SectionLabel(this.text);

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: Text(
        text.toLowerCase(),
        style: CyberpunkTypography.label.copyWith(
          color: CyberpunkColors.orangePrimary,
        ),
      ),
    );
  }
}

/// Fixed-width table of usage rows. The first column takes more space so a
/// `provider/model` id still reads; numeric columns share the remainder.
class _UsageTable extends StatelessWidget {
  final List<String> headers;
  final List<List<String>> rows;

  const _UsageTable({required this.headers, required this.rows});

  @override
  Widget build(BuildContext context) {
    return Table(
      columnWidths: {
        0: const FlexColumnWidth(2.4),
        for (var i = 1; i < headers.length; i++) i: const FlexColumnWidth(1),
      },
      border: TableBorder.all(
        color: CyberpunkColors.midGray.withValues(alpha: 0.4),
        width: 1,
      ),
      children: [
        TableRow(
          decoration: BoxDecoration(
            color: CyberpunkColors.midGray.withValues(alpha: 0.25),
          ),
          children: [for (final h in headers) _cell(h, header: true)],
        ),
        for (final row in rows)
          TableRow(
            children: [
              for (var i = 0; i < headers.length; i++)
                _cell(i < row.length ? row[i] : ''),
            ],
          ),
      ],
    );
  }

  Widget _cell(String text, {bool header = false}) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 5),
      child: Text(
        text,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: CyberpunkTypography.bodySmall.copyWith(
          fontFamily: 'SourceCodePro',
          fontSize: 11,
          color: header
              ? CyberpunkColors.orangePrimary
              : CyberpunkColors.lightGray,
          fontWeight: header ? FontWeight.w600 : FontWeight.normal,
        ),
      ),
    );
  }
}

class _TotalTile extends StatelessWidget {
  final String label;
  final String value;

  const _TotalTile({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(
        color: CyberpunkColors.midGray.withValues(alpha: 0.2),
        border: Border.all(
          color: CyberpunkColors.cyanAccent.withValues(alpha: 0.3),
          width: 1,
        ),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(
            label,
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.cyanAccent.withValues(alpha: 0.8),
            ),
          ),
          Text(
            value,
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.lightGray,
              fontSize: 15,
              fontWeight: FontWeight.w600,
            ),
          ),
        ],
      ),
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
        border: Border.all(color: color.withValues(alpha: 0.3), width: 1),
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
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
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

/// `HH:MM:SS` in local time for the 'updated <time>' stamp.
String _formatClock(DateTime time) {
  final local = time.toLocal();
  String pad(int n) => n.toString().padLeft(2, '0');
  return '${pad(local.hour)}:${pad(local.minute)}:${pad(local.second)}';
}

/// Group thousands so token counts stay readable: 3400 -> '3,400'.
String _formatCount(int value) {
  final negative = value < 0;
  final digits = value.abs().toString();
  final buffer = StringBuffer();
  if (negative) buffer.write('-');
  for (var i = 0; i < digits.length; i++) {
    if (i > 0 && (digits.length - i) % 3 == 0) buffer.write(',');
    buffer.write(digits[i]);
  }
  return buffer.toString();
}

/// Milliseconds with one decimal; an unknown/zero latency shows '-'.
String _formatLatency(double ms) {
  if (ms <= 0) return '-';
  return '${ms.toStringAsFixed(1)}ms';
}
