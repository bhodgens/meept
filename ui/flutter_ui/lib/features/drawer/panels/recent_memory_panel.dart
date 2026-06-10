import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../providers/providers.dart';
import '../../../theme/colors.dart';
import '../../../theme/typography.dart';

/// Recent Memory panel — shows up to 5 memory items with type badge and preview.
class RecentMemoryPanel extends ConsumerStatefulWidget {
  const RecentMemoryPanel({super.key});

  @override
  ConsumerState<RecentMemoryPanel> createState() => _RecentMemoryPanelState();
}

class _RecentMemoryPanelState extends ConsumerState<RecentMemoryPanel> {
  List<Map<String, dynamic>> _memories = [];
  bool _isLoading = true;
  String? _error;
  Timer? _refreshTimer;

  @override
  void initState() {
    super.initState();
    _load();
    _refreshTimer = Timer.periodic(const Duration(seconds: 5), (_) {
      if (mounted) _load();
    });
  }

  @override
  void dispose() {
    _refreshTimer?.cancel();
    super.dispose();
  }

  Future<void> _load() async {
    final client = ref.read(apiClientProvider);
    try {
      final memories = await client.getRecentMemories(limit: 5);
      if (mounted) {
        setState(() {
          _memories = memories;
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

    if (_memories.isEmpty) {
      return Center(
        child: Text(
          'no recent memory',
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.midGray,
          ),
        ),
      );
    }

    return ListView.builder(
      padding: const EdgeInsets.all(12),
      itemCount: _memories.length,
      itemBuilder: (context, index) {
        final mem = _memories[index];
        final category = mem['category'] as String? ?? 'unknown';
        final content = mem['content'] as String? ?? '';
        final preview = content.length > 120
            ? '${content.substring(0, 120)}...'
            : content;

        return Container(
          margin: const EdgeInsets.only(bottom: 8),
          padding: const EdgeInsets.all(10),
          decoration: BoxDecoration(
            color: CyberpunkColors.darkGray,
            borderRadius: BorderRadius.circular(4),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 6,
                      vertical: 2,
                    ),
                    decoration: BoxDecoration(
                      color: CyberpunkColors.orangePrimary.withValues(alpha: 0.15),
                      borderRadius: BorderRadius.circular(2),
                    ),
                    child: Text(
                      category.toLowerCase(),
                      style: CyberpunkTypography.bodySmall.copyWith(
                        fontSize: 9,
                        color: CyberpunkColors.orangePrimary,
                        fontFamily: 'SourceCodePro',
                      ),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 6),
              Text(
                preview.toLowerCase(),
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.lightGray,
                  fontSize: 11,
                ),
                maxLines: 3,
                overflow: TextOverflow.ellipsis,
              ),
            ],
          ),
        );
      },
    );
  }
}
