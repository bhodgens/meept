import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../models/api_models.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';

/// Facts panel - shows active memory facts from the harness-eval leaf 12 store.
/// Read-only. Blank = no facts, not a layout bug.
class FactsPanel extends ConsumerStatefulWidget {
  const FactsPanel({super.key});

  @override
  ConsumerState<FactsPanel> createState() => _FactsPanelState();
}

class _FactsPanelState extends ConsumerState<FactsPanel> {
  List<MemoryFact> _facts = const [];
  bool _loading = false;
  String? _error;
  String? _filterKind;

  @override
  void initState() {
    super.initState();
    _loadFacts();
  }

  Future<void> _loadFacts() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final client = ref.read(sdkClientProvider);
      final raw = await client.listMemoryFacts(kind: _filterKind);
      if (!mounted) return;
      setState(() {
        _facts = raw.map(MemoryFact.fromJson).toList(growable: false);
        _loading = false;
        _error = null;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _facts = const [];
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
              Icon(Icons.manage_accounts, size: 16, color: CyberpunkColors.orangePrimary),
              const SizedBox(width: 6),
              Text(
                'facts',
                style: CyberpunkTypography.label.copyWith(
                  color: CyberpunkColors.orangePrimary,
                ),
              ),
              const Spacer(),
              IconButton(
                icon: const Icon(Icons.refresh, size: 16),
                onPressed: _loadFacts,
                tooltip: 'refresh',
                padding: EdgeInsets.zero,
                constraints: const BoxConstraints(),
              ),
            ],
          ),
          const SizedBox(height: 8),
          if (_error != null)
            _buildErrorBanner()
          else if (_loading && _facts.isEmpty)
            const Center(child: CircularProgressIndicator())
          else if (_facts.isEmpty)
            const Center(
              child: Text(
                'no facts',
                style: TextStyle(color: Colors.grey),
              ),
            )
          else
            Expanded(
              child: ListView.builder(
                shrinkWrap: true,
                physics: const NeverScrollableScrollPhysics(),
                itemCount: _facts.length,
                itemBuilder: (context, index) {
                  return _buildFactTile(_facts[index]);
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
            onPressed: _loadFacts,
            child: const Text('retry', style: TextStyle(fontSize: 10)),
          ),
        ],
      ),
    );
  }

  Widget _buildFactTile(MemoryFact fact) {
    return Container(
      margin: const EdgeInsets.only(bottom: 4),
      padding: const EdgeInsets.all(6),
      decoration: BoxDecoration(
        color: CyberpunkColors.black.withValues(alpha: 0.3),
        border: Border.all(color: CyberpunkColors.midGray.withValues(alpha: 0.5)),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Row(
        children: [
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 2),
            decoration: BoxDecoration(
              color: _kindColor(fact.kind).withValues(alpha: 0.2),
              borderRadius: BorderRadius.circular(2),
            ),
            child: Text(
              fact.kind,
            style: CyberpunkTypography.bodySmall.copyWith(
              color: _kindColor(fact.kind),
            ),
            ),
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              '${fact.key}: ${fact.value}',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.lightGray,
                fontFamily: 'SourceCodePro',
              ),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
          ),
        ],
      ),
    );
  }

  Color _kindColor(String kind) {
    switch (kind.toLowerCase()) {
      case 'preference':
        return CyberpunkColors.blueInfo;
      case 'restriction':
        return CyberpunkColors.orangePrimary;
      case 'account':
        return CyberpunkColors.orangeAccent;
      case 'temporal':
        return CyberpunkColors.greenSuccess;
      default:
        return CyberpunkColors.midGray;
    }
  }
}
