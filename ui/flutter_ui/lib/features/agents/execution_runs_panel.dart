import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../models/ws_events.dart';
import '../../providers/execution_runs_provider.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';

/// Executions panel (issue #160 follow-up).
///
/// A list of recent bot/employee execution runs, newest first, each row a
/// colored status dot plus `session:<worktype>` text. Fully server-backed:
/// the provider polls GET /api/v1/executions (durable store), so the list
/// survives disconnect/reconnect and daemon restart. Live WS frames merge
/// between polls. Worktype comes from trigger_ref (e.g. cron:..., intake:...,
/// http, manual); unknown when absent. Visible text caps at ~24 chars with
/// ellipsis; the full value shows in the tooltip.
class ExecutionRunsPanel extends ConsumerWidget {
  const ExecutionRunsPanel({super.key});

  /// Derive the worktype label from trigger_ref. Prefix before ':' wins
  /// (cron:ci -> cron, intake:repo#12 -> intake); a bare ref is used as-is;
  /// empty -> unknown.
  static String worktypeOf(ExecutionResultEvent run) {
    final ref = run.triggerRef.trim();
    if (ref.isEmpty) return 'unknown';
    final colon = ref.indexOf(':');
    final head = colon > 0 ? ref.substring(0, colon) : ref;
    return head.toLowerCase();
  }

  static String sessionLabel(ExecutionResultEvent run) {
    return 'session:${worktypeOf(run)}';
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(executionRunsProvider);
    final runs = state.runs;

    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: CyberpunkColors.black.withValues(alpha: 0.5),
        border: Border.all(color: CyberpunkColors.midGray),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.bolt, size: 16, color: CyberpunkColors.orangePrimary),
              const SizedBox(width: 6),
              Text(
                'executions',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.orangePrimary,
                ),
              ),
              const Spacer(),
              Text(
                '${state.total}',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.midGray,
                ),
              ),
              IconButton(
                icon: const Icon(Icons.refresh, size: 16),
                onPressed: () =>
                    ref.read(executionRunsProvider.notifier).refresh(),
                tooltip: 'refresh',
                padding: EdgeInsets.zero,
                constraints: const BoxConstraints(),
              ),
            ],
          ),
          const SizedBox(height: 4),
          Text(
            'server history - survives reconnect',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.midGray,
              fontSize: 10,
            ),
          ),
          if (state.error != null)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Text(
                state.error!,
                style: const TextStyle(color: Colors.red, fontSize: 10),
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
              ),
            ),
          const SizedBox(height: 8),
          if (!state.loaded)
            const Center(
              child: Padding(
                padding: EdgeInsets.symmetric(vertical: 12),
                child: SizedBox(
                  width: 16,
                  height: 16,
                  child: CircularProgressIndicator(strokeWidth: 2),
                ),
              ),
            )
          else if (runs.isEmpty)
            const Center(
              child: Padding(
                padding: EdgeInsets.symmetric(vertical: 12),
                child: Text(
                  'no executions yet',
                  style: TextStyle(color: Colors.grey, fontSize: 11),
                ),
              ),
            )
          else
            // Bounded, internally-scrolling list inside the sidebar's
            // scroll view: newest 100 runs reachable without pushing the
            // facts panel off-screen.
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 240),
              child: ListView.builder(
                shrinkWrap: true,
                itemCount: runs.length,
                itemBuilder: (context, i) => _RunRow(run: runs[i]),
              ),
            ),
        ],
      ),
    );
  }
}

class _RunRow extends StatelessWidget {
  const _RunRow({required this.run});

  final ExecutionResultEvent run;

  Color get _dotColor => switch (run.status) {
    ExecutionResultEvent.statusCompleted => CyberpunkColors.greenSuccess,
    ExecutionResultEvent.statusFailed => CyberpunkColors.redAlert,
    ExecutionResultEvent.statusSkipped => CyberpunkColors.midGray,
    _ => CyberpunkColors.lightGray,
  };

  @override
  Widget build(BuildContext context) {
    final label = ExecutionRunsPanel.sessionLabel(run);
    final when = run.effectiveTime;
    final timeLabel = when == null
        ? ''
        : '${when.hour.toString().padLeft(2, '0')}:${when.minute.toString().padLeft(2, '0')}';
    return Tooltip(
      // Full detail on hover/long-press: agent, tier, full trigger ref,
      // status, tokens, duration, error.
      message:
          '${run.tier.isEmpty ? 'run' : run.tier}: ${run.agentId}\n'
          '$label\n'
          'trigger: ${run.triggerRef.isEmpty ? 'unknown' : run.triggerRef}\n'
          '${run.status} - ${run.tokensUsed} tokens - ${run.durationMs} ms'
          '${run.error.isEmpty ? '' : '\nerror: ${run.error}'}',
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 3),
        child: Row(
          children: [
            Container(
              width: 8,
              height: 8,
              decoration: BoxDecoration(
                color: _dotColor,
                shape: BoxShape.circle,
              ),
            ),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                label,
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.lightGray,
                  fontFamily: 'SourceCodePro',
                  fontSize: 11,
                ),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
              ),
            ),
            const SizedBox(width: 6),
            Text(
              run.agentId,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
                fontSize: 10,
              ),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
            const SizedBox(width: 6),
            Text(
              timeLabel,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
                fontSize: 10,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
