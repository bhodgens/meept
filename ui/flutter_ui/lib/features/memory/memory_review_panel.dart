import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../providers/providers.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';

/// Human-review panel over the auto-claim review queue.
///
/// GUI parity with the TUI MemoryReviewModal (internal/tui/models/
/// memory_review.go): lists status=auto claims from the SAME
/// memory.reviewQueue RPC surface the TUI and `meept memory review` use,
/// and verdicts go through promote (POST /api/v1/memory/claims/{id}/promote)
/// and reject (POST /api/v1/memory/claims/{id}/reject) — no second write
/// path. After a verdict the current page refreshes from the daemon.
///
/// Pagination mirrors the TUI modal (bughunt M13): fixed page size, offset
/// navigation bounded by the daemon-reported total.
///
/// All UI text is lowercase (repo convention).
class MemoryReviewPanel extends ConsumerStatefulWidget {
  const MemoryReviewPanel({super.key});

  /// Static builder so the command palette can push this panel directly
  /// (no dedicated go_router route yet — see report).
  static WidgetBuilder get builder =>
      (_) => const MemoryReviewPanel();

  @override
  ConsumerState<MemoryReviewPanel> createState() => _MemoryReviewPanelState();
}

class _MemoryReviewPanelState extends ConsumerState<MemoryReviewPanel> {
  /// Page size the review panel requests. Matches the TUI's reviewPageSize:
  /// the RPC accepts up to 200; 50 keeps a page scannable.
  static const int _pageSize = 50;

  List<Map<String, dynamic>> _claims = [];
  int _total = 0;
  int _offset = 0;
  bool _loading = false;
  bool _fetching = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _fetchPage();
    });
  }

  bool get _hasNext => _offset + _claims.length < _total;

  Future<void> _fetchPage() async {
    if (_fetching) return;
    setState(() {
      _fetching = true;
      _loading = _claims.isEmpty;
      _error = null;
    });
    try {
      final result = await ref
          .read(sdkClientProvider)
          .listMemoryReviewQueue(limit: _pageSize, offset: _offset);
      if (!mounted) return;
      final raw = result['auto_claims'] as List?;
      final claims = (raw ?? const [])
          .whereType<Map>()
          .map((c) => Map<String, dynamic>.from(c))
          .toList();
      setState(() {
        _claims = claims;
        _total = (result['total'] as num?)?.toInt() ?? _claims.length;
        _fetching = false;
        _loading = false;
        if (_offset > 0 && _claims.isEmpty && _offset >= _total) {
          // Page walked past the end (claims consumed on earlier pages):
          // snap back to the last non-empty page.
          _offset = (_total - _pageSize) > 0 ? _total - _pageSize : 0;
        }
      });
      // Snap-back needs one more fetch to fill the page.
      if (_offset > 0 && claims.isEmpty) {
        await _fetchPage();
      }
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e.toString();
        _fetching = false;
        _loading = false;
      });
    }
  }

  Future<void> _verdict({
    required String claimId,
    required String content,
    required bool promote,
  }) async {
    final client = ref.read(sdkClientProvider);
    setState(() => _error = null);
    try {
      if (promote) {
        await client.promoteClaim(id: claimId, confirmed: true);
      } else {
        await client.rejectClaim(id: claimId, confirmed: true);
      }
    } catch (e) {
      if (!mounted) return;
      setState(() => _error = e.toString());
      return;
    }
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          promote
              ? 'promoted: ${_preview(content)}'
              : 'rejected: ${_preview(content)}',
        ),
        duration: const Duration(milliseconds: 2500),
      ),
    );
    await _fetchPage();
  }

  static String _preview(String content) {
    final lower = content.toLowerCase();
    return lower.length > 40 ? '${lower.substring(0, 40)}…' : lower;
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: CyberpunkColors.black,
      appBar: AppBar(
        backgroundColor: CyberpunkColors.darkGray,
        title: Text(
          'memory review',
          style: CyberpunkTypography.headlineSmall.copyWith(
            color: CyberpunkColors.orangePrimary,
          ),
        ),
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : _error != null && _claims.isEmpty
          ? Center(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(
                    _error!,
                    style: CyberpunkTypography.bodySmall.copyWith(
                      color: CyberpunkColors.redAlert,
                    ),
                    textAlign: TextAlign.center,
                  ),
                  const SizedBox(height: 12),
                  FilledButton.tonal(
                    onPressed: _fetchPage,
                    child: const Text('retry'),
                  ),
                ],
              ),
            )
          : _claims.isEmpty
          ? const Center(child: Text('no pending claims'))
          : Column(
              children: [
                Expanded(child: _buildClaimList()),
                _buildPaginationBar(),
              ],
            ),
    );
  }

  Widget _buildClaimList() {
    return ListView.separated(
      itemCount: _claims.length,
      separatorBuilder: (_, __) => const SizedBox(height: 8),
      padding: const EdgeInsets.all(12),
      itemBuilder: (context, i) {
        final claim = _claims[i];
        final id = '${claim['id'] ?? ''}';
        final content = '${claim['content'] ?? ''}';
        final metadata =
            (claim['metadata'] as Map?)?.cast<String, dynamic>() ??
            const <String, dynamic>{};
        final category = '${claim['category'] ?? ''}';
        final confidence = (metadata['confidence'] as num?)?.toDouble();
        return Container(
          key: ValueKey('review-claim-$id'),
          padding: const EdgeInsets.all(10),
          decoration: BoxDecoration(
            color: CyberpunkColors.darkGray,
            border: Border.all(
              color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
            ),
            borderRadius: BorderRadius.circular(8),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                content.toLowerCase(),
                style: CyberpunkTypography.bodyMedium.copyWith(
                  color: CyberpunkColors.greenSuccess,
                ),
              ),
              const SizedBox(height: 4),
              Row(
                children: [
                  if (category.isNotEmpty) ...[
                    Text(
                      category.toLowerCase(),
                      style: CyberpunkTypography.bodySmall.copyWith(
                        color: CyberpunkColors.midGray,
                      ),
                    ),
                    const SizedBox(width: 12),
                  ],
                  if (confidence != null)
                    Text(
                      'confidence: ${confidence.toStringAsFixed(2)}',
                      style: CyberpunkTypography.bodySmall.copyWith(
                        color: CyberpunkColors.midGray,
                      ),
                    ),
                ],
              ),
              const SizedBox(height: 8),
              Row(
                mainAxisAlignment: MainAxisAlignment.end,
                children: [
                  TextButton(
                    key: ValueKey('promote-claim-$id'),
                    onPressed: () =>
                        _verdict(claimId: id, content: content, promote: true),
                    child: const Text('promote'),
                  ),
                  const SizedBox(width: 8),
                  TextButton(
                    key: ValueKey('reject-claim-$id'),
                    onPressed: () =>
                        _verdict(claimId: id, content: content, promote: false),
                    child: Text(
                      'reject',
                      style: CyberpunkTypography.bodySmall.copyWith(
                        color: CyberpunkColors.redAlert,
                      ),
                    ),
                  ),
                ],
              ),
            ],
          ),
        );
      },
    );
  }

  Widget _buildPaginationBar() {
    if (_total <= _pageSize && _offset == 0) return const SizedBox.shrink();
    final page = (_offset / _pageSize).floor() + 1;
    final pages = ((_total + _pageSize - 1) / _pageSize).ceil();
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          TextButton(
            onPressed: _offset > 0 && !_fetching
                ? () {
                    setState(
                      () => _offset = (_offset - _pageSize).clamp(0, _total),
                    );
                    _fetchPage();
                  }
                : null,
            child: const Text('prev'),
          ),
          Text(
            'page $page / $pages ($_total claims)',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.midGray,
            ),
          ),
          TextButton(
            onPressed: _hasNext && !_fetching
                ? () {
                    setState(() => _offset += _pageSize);
                    _fetchPage();
                  }
                : null,
            child: const Text('next'),
          ),
        ],
      ),
    );
  }
}
