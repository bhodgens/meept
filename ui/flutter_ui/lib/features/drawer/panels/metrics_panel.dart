import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../providers/providers.dart';
import '../../../theme/colors.dart';
import '../../../theme/typography.dart';

/// Metrics panel — shows queue depth, workers busy, agents active.
class MetricsPanel extends ConsumerStatefulWidget {
  const MetricsPanel({super.key});

  @override
  ConsumerState<MetricsPanel> createState() => _MetricsPanelState();
}

class _MetricsPanelState extends ConsumerState<MetricsPanel> {
  Map<String, dynamic>? _metrics;
  bool _isLoading = true;
  String? _error;

  Timer? _refreshTimer;

  @override
  void initState() {
    super.initState();
    _load();
    _refreshTimer = Timer.periodic(const Duration(seconds: 5), (_) => _load());
  }

  @override
  void dispose() {
    _refreshTimer?.cancel();
    super.dispose();
  }

  Future<void> _load() async {
    final client = ref.read(apiClientProvider);
    try {
      final metrics = await client.getLiveMetrics();
      if (mounted) {
        setState(() {
          _metrics = metrics;
          _isLoading = false;
          _error = null;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = e.toString();
          _isLoading = false;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_isLoading) {
      return const Center(
        child: SizedBox(
          width: 24,
          height: 24,
          child: CircularProgressIndicator(strokeWidth: 2),
        ),
      );
    }

    if (_error != null) {
      return Center(
        child: Text(
          'error: $_error',
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.redAlert,
          ),
        ),
      );
    }

    final metrics = _metrics ?? {};

    return SingleChildScrollView(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _buildMetric('queue depth', metrics['queue_depth']?.toString() ?? '0'),
          _buildMetric('workers busy', metrics['workers_busy']?.toString() ?? '0'),
          _buildMetric('agents active', metrics['agents_active']?.toString() ?? '0'),
          _buildMetric('sessions', metrics['sessions']?.toString() ?? '0'),
          _buildMetric('tasks', metrics['tasks']?.toString() ?? '0'),
        ],
      ),
    );
  }

  Widget _buildMetric(String label, String value) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        children: [
          Text(
            '$label: ',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.midGray,
            ),
          ),
          Expanded(
            child: Text(
              value,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.greenSuccess,
                fontFamily: 'SourceCodePro',
                fontWeight: FontWeight.bold,
              ),
            ),
          ),
        ],
      ),
    );
  }
}
