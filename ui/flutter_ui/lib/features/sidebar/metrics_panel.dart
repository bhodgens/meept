import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import 'dart:async';

/// Metrics panel - displays live system metrics from the API
class MetricsPanel extends ConsumerStatefulWidget {
  const MetricsPanel({super.key});

  @override
  ConsumerState<MetricsPanel> createState() => _MetricsPanelState();
}

class _MetricsPanelState extends ConsumerState<MetricsPanel> {
  Map<String, dynamic>? _metrics;
  bool _isLoading = false;
  String? _error;
  Timer? _periodicRefresh;

  @override
  void initState() {
    super.initState();
    _loadMetrics();
    // Refresh every 10 seconds
    _periodicRefresh = Timer.periodic(const Duration(seconds: 10), (_) => _loadMetrics());
  }

  @override
  void dispose() {
    _periodicRefresh?.cancel();
    super.dispose();
  }

  Future<void> _loadMetrics() async {
    if (!mounted) return;
    setState(() {
      _isLoading = true;
    });
    try {
      final data = await ref.read(apiClientProvider).getLiveMetrics();
      if (mounted) {
        setState(() {
          _metrics = data;
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
    if (_error != null) {
      return Container(
        padding: const EdgeInsets.all(16),
        decoration: BoxDecoration(
          color: CyberpunkColors.darkGray.withValues(alpha: 0.5),
          border: Border(
            top: BorderSide(
              color: CyberpunkColors.orangePrimary.withValues(alpha: 0.3),
              width: 1,
            ),
          ),
        ),
        child: Text(
          'error: $_error',
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.redAlert,
          ),
        ),
      );
    }

    final activeAgents = _metrics?['active_agents'] ?? '-';
    final queueDepth = _metrics?['queue_depth'] ?? '-';
    final tokenUsageRate = _metrics?['token_usage_rate'];
    final requestsPerSec = _metrics?['requests_per_sec'] ?? '-';

    return RefreshIndicator(
      onRefresh: _loadMetrics,
      child: Container(
        padding: const EdgeInsets.all(16),
        decoration: BoxDecoration(
          color: CyberpunkColors.darkGray.withValues(alpha: 0.5),
          border: Border(
            top: BorderSide(
              color: CyberpunkColors.orangePrimary.withValues(alpha: 0.3),
              width: 1,
            ),
          ),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              'SYSTEM METRICS',
              style: CyberpunkTypography.label,
            ),
            const SizedBox(height: 12),
            _buildMetricRow(
              'ACTIVE AGENTS',
              _formatValue(activeAgents, intType: true),
              CyberpunkColors.blueInfo,
            ),
            const SizedBox(height: 8),
            _buildMetricRow(
              'QUEUE DEPTH',
              _formatValue(queueDepth, intType: true),
              CyberpunkColors.orangePrimary,
            ),
            const SizedBox(height: 8),
            _buildMetricRow(
              'TOKENS/SEC',
              tokenUsageRate != null
                  ? (tokenUsageRate is double
                      ? tokenUsageRate.toStringAsFixed(1)
                      : tokenUsageRate.toString())
                  : '-',
              CyberpunkColors.greenSuccess,
            ),
            const SizedBox(height: 8),
            _buildMetricRow(
              'REQ/MIN',
              _formatValue(requestsPerSec, intType: true),
              Colors.purple,
            ),
            if (_isLoading) const SizedBox(height: 8),
            if (_isLoading)
              const Center(
                child: SizedBox(
                  width: 12,
                  height: 12,
                  child: CircularProgressIndicator(
                    strokeWidth: 1,
                    valueColor: AlwaysStoppedAnimation<Color>(
                      CyberpunkColors.orangePrimary,
                    ),
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }

  String _formatValue(dynamic value, {bool intType = false}) {
    if (value == '-') return '-';
    if (value == null || value == 0) return '-';
    if (intType) return value.toString();
    return value.toString();
  }

  Widget _buildMetricRow(String label, String value, Color color) {
    return Row(
      mainAxisAlignment: MainAxisAlignment.spaceBetween,
      children: [
        Text(
          label,
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.lightGray,
            fontSize: 10,
          ),
        ),
        Text(
          value,
          style: CyberpunkTypography.bodyMedium.copyWith(
            color: color,
            fontWeight: FontWeight.bold,
            fontSize: 16,
          ),
        ),
      ],
    );
  }
}
