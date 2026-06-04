import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../services/api_client.dart';
import '../../../providers/providers.dart';
import '../../../theme/colors.dart';
import '../../../theme/typography.dart';

/// Status panel — shows daemon connection state, uptime, active agents, pending tasks.
class StatusPanel extends ConsumerStatefulWidget {
  const StatusPanel({super.key});

  @override
  ConsumerState<StatusPanel> createState() => _StatusPanelState();
}

class _StatusPanelState extends ConsumerState<StatusPanel> {
  Map<String, dynamic>? _status;
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
      final status = await client.getDaemonStatus();
      if (mounted) {
        setState(() {
          _status = status;
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

    final status = _status ?? {};
    final connected = status['running'] == true;
    final uptime = status['uptime'] as String? ?? 'unknown';
    final pid = status['pid']?.toString() ?? 'unknown';
    final version = status['version']?.toString() ?? 'unknown';

    return SingleChildScrollView(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _buildRow('status', connected ? 'running' : 'stopped',
              valueColor: connected
                  ? CyberpunkColors.greenSuccess
                  : CyberpunkColors.redAlert),
          _buildRow('uptime', uptime),
          _buildRow('pid', pid),
          _buildRow('version', version),
          if (status['active_agents'] != null)
            _buildRow('active agents', status['active_agents'].toString()),
          if (status['pending_tasks'] != null)
            _buildRow('pending tasks', status['pending_tasks'].toString()),
        ],
      ),
    );
  }

  Widget _buildRow(String label, String value, {Color? valueColor}) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
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
                color: valueColor ?? CyberpunkColors.greenSuccess,
                fontFamily: 'SourceCodePro',
              ),
            ),
          ),
        ],
      ),
    );
  }
}
