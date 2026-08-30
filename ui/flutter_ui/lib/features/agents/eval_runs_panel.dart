import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../models/api_models.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';

/// Eval runs panel - shows the last N eval runs from the harness-eval system.
/// Data pipeline first: blank = no data, not a layout bug.
class EvalRunsPanel extends ConsumerStatefulWidget {
  const EvalRunsPanel({super.key});

  @override
  ConsumerState<EvalRunsPanel> createState() => _EvalRunsPanelState();
}

class _EvalRunsPanelState extends ConsumerState<EvalRunsPanel> {
  List<EvalRun> _runs = const [];
  bool _loading = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _loadRuns();
  }

  Future<void> _loadRuns() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final client = ref.read(sdkClientProvider);
      final raw = await client.listEvalRuns();
      if (!mounted) return;
      setState(() {
        _runs = raw.map(EvalRun.fromJson).toList(growable: false);
        _loading = false;
        _error = null;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _runs = const [];
        _loading = false;
        _error = e.toString();
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.all(12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.analytics, size: 16, color: CyberpunkColors.orangePrimary),
              const SizedBox(width: 6),
              Text(
                'eval runs',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.orangePrimary,
                ),
              ),
              const Spacer(),
              IconButton(
                icon: const Icon(Icons.refresh, size: 16),
                onPressed: _loadRuns,
                tooltip: 'refresh',
                padding: EdgeInsets.zero,
                constraints: const BoxConstraints(),
              ),
            ],
          ),
          const SizedBox(height: 8),
          if (_error != null)
            _buildErrorBanner()
          else if (_loading && _runs.isEmpty)
            const Center(child: CircularProgressIndicator())
          else if (_runs.isEmpty)
            const Center(
              child: Text(
                'no eval runs',
                style: TextStyle(color: Colors.grey),
              ),
            )
          else
            Expanded(
              child: ListView.builder(
                shrinkWrap: true,
                physics: const NeverScrollableScrollPhysics(),
                itemCount: _runs.length,
                itemBuilder: (context, index) {
                  return _buildRunCard(_runs[index]);
                },
              ),
            ),
        ],
      ),
    );
  }

  Widget _buildErrorBanner() {
    return Container(
      padding: const EdgeInsets.all(8),
      color: CyberpunkColors.redAlert.withValues(alpha: 0.2),
      child: Row(
        children: [
          const Icon(Icons.error_outline, size: 16, color: Colors.red),
          const SizedBox(width: 6),
          Expanded(
            child: Text(
              _error!,
              style: const TextStyle(color: Colors.red, fontSize: 10),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          TextButton(
            onPressed: _loadRuns,
            child: const Text('retry', style: TextStyle(fontSize: 10)),
          ),
        ],
      ),
    );
  }

  Widget _buildRunCard(EvalRun run) {
    final statusColor = run.passed
        ? CyberpunkColors.greenSuccess
        : CyberpunkColors.redAlert;
    return Container(
      margin: const EdgeInsets.only(bottom: 6),
      padding: const EdgeInsets.all(8),
      decoration: BoxDecoration(
        color: CyberpunkColors.black.withValues(alpha: 0.5),
        border: Border.all(color: CyberpunkColors.midGray),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Row(
        children: [
          Icon(
            run.passed ? Icons.check_circle : Icons.cancel,
            size: 16,
            color: statusColor,
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  run.taskId.substring(0, run.taskId.length.clamp(0, 12)),
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.lightGray,
                  ),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
                Text(
                  '${run.kind} k=${run.k} model=${run.modelId.substring(0, run.modelId.length.clamp(0, 16))}',
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.midGray,
                  ),
                ),
              ],
            ),
          ),
          Text(
            _formatDate(run.createdAt),
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.midGray,
            ),
          ),
        ],
      ),
    );
  }

  String _formatDate(String raw) {
    if (raw.isEmpty) return '';
    try {
      final dt = DateTime.parse(raw);
      return '${dt.month.toString().padLeft(2, '0')}/${dt.day.toString().padLeft(2, '0')}';
    } catch (_) {
      return raw.substring(0, raw.length.clamp(0, 10));
    }
  }
}
