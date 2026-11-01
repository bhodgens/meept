import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../providers/providers.dart';
import 'activity_models.dart';

/// Live recent events panel - polls activity/recent on a timer
class ActivityRecentPanel extends ConsumerStatefulWidget {
  const ActivityRecentPanel({super.key});

  @override
  ConsumerState<ActivityRecentPanel> createState() =>
      _ActivityRecentPanelState();
}

class _ActivityRecentPanelState extends ConsumerState<ActivityRecentPanel> {
  Timer? _timer;
  List<ActivityRecord> _records = [];
  int _dropped = 0;

  @override
  void initState() {
    super.initState();
    _startPolling();
  }

  void _startPolling() {
    _timer?.cancel();
    _timer = Timer.periodic(const Duration(seconds: 2), (timer) {
      _fetchRecent();
    });
  }

  Future<void> _fetchRecent() async {
    try {
      final client = ref.read(sdkClientProvider);
      final raw = await client.getActivityRecent(limit: 20);
      final result = ActivityRecentResult.fromJson(raw);
      setState(() {
        _records = result.records;
        _dropped = result.dropped;
      });
    } catch (e) {
      debugPrint('activity recent poll failed: $e');
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'recent activity',
            style: TextStyle(
              color: Colors.orange,
              fontSize: 14,
              fontWeight: FontWeight.bold,
            ),
          ),
          const SizedBox(height: 8),
          if (_records.isEmpty)
            const Text(
              'no recent events',
              style: TextStyle(color: Colors.grey, fontSize: 12),
            )
          else
            ..._records.map((record) {
              return Text(
                '${record.kind}: ${record.summary.toLowerCase()}',
                style: const TextStyle(color: Colors.white70, fontSize: 12),
              );
            }),
          if (_dropped > 0)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Text(
                'dropped: $_dropped',
                style: const TextStyle(color: Colors.orange, fontSize: 10),
              ),
            ),
        ],
      ),
    );
  }
}
