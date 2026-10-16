import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/memory/memory_review_panel.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';

void main() {
  testWidgets('renders pending claims from the reviewQueue RPC', (
    tester,
  ) async {
    final client = _ReviewTestClient([
      _claim('c1', 'user prefers go tests first', 0.9),
      _claim('c2', 'deploys happen on fridays', 0.4),
    ]);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [sdkClientProvider.overrideWith((ref) => client)],
        child: const MaterialApp(home: MemoryReviewPanel()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('review-claim-c1')), findsOneWidget);
    expect(find.byKey(const ValueKey('review-claim-c2')), findsOneWidget);
    expect(find.text('user prefers go tests first'), findsOneWidget);
    expect(find.text('confidence: 0.90'), findsOneWidget);
    // Pagination bar hidden while everything fits one page.
    expect(find.text('prev'), findsNothing);
  });

  testWidgets('promote calls the promote endpoint and refreshes', (
    tester,
  ) async {
    final client = _ReviewTestClient([_claim('c1', 'a claim to promote', 0.9)]);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [sdkClientProvider.overrideWith((ref) => client)],
        child: const MaterialApp(home: MemoryReviewPanel()),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const ValueKey('promote-claim-c1')));
    await tester.pumpAndSettle();

    expect(client.promoted, ['c1']);
    expect(find.textContaining('promoted: a claim to promote'), findsOneWidget);
  });

  testWidgets('reject calls the reject endpoint and refreshes', (tester) async {
    final client = _ReviewTestClient([_claim('c2', 'a claim to reject', 0.3)]);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [sdkClientProvider.overrideWith((ref) => client)],
        child: const MaterialApp(home: MemoryReviewPanel()),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const ValueKey('reject-claim-c2')));
    await tester.pumpAndSettle();

    expect(client.rejected, ['c2']);
    expect(find.textContaining('rejected: a claim to reject'), findsOneWidget);
  });

  testWidgets('pagination: next/prev walk pages bounded by total', (
    tester,
  ) async {
    // 55 claims: page 1 fills the panel's 50-claim page, page 2 has 5. The
    // daemon reports the cross-page total, so the pager renders and
    // next/prev walk real offsets — mirroring the TUI modal's bughunt-M13
    // pagination behavior.
    final client = _ReviewTestClient([
      for (var i = 1; i <= 55; i++) _claim('p$i', 'claim number $i', 0.5),
    ]);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [sdkClientProvider.overrideWith((ref) => client)],
        child: const MaterialApp(home: MemoryReviewPanel()),
      ),
    );
    await tester.pumpAndSettle();

    // Viewport height limits rendered rows; the fetcher served 50 per page
    // and the daemon total (55) spans 3 page bars of 50 — assert on the
    // pager text and boundary claims instead of specific row counts.
    expect(find.textContaining('page 1 /'), findsOneWidget);
    expect(find.byKey(const ValueKey('review-claim-p1')), findsOneWidget);
    expect(find.byKey(const ValueKey('review-claim-p51')), findsNothing);

    await tester.tap(find.text('next'));
    await tester.pumpAndSettle();

    expect(find.textContaining('page 2 /'), findsOneWidget);
    expect(find.byKey(const ValueKey('review-claim-p51')), findsOneWidget);
    expect(find.byKey(const ValueKey('review-claim-p1')), findsNothing);

    await tester.tap(find.text('prev'));
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('review-claim-p1')), findsOneWidget);
  });

  testWidgets('error state shows a retry button', (tester) async {
    final client = _ReviewTestClient([])..failWith = 'daemon offline';

    await tester.pumpWidget(
      ProviderScope(
        overrides: [sdkClientProvider.overrideWith((ref) => client)],
        child: const MaterialApp(home: MemoryReviewPanel()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.textContaining('daemon offline'), findsOneWidget);
    expect(find.text('retry'), findsOneWidget);
  });
}

Map<String, dynamic> _claim(String id, String content, double confidence) => {
  'id': id,
  'content': content,
  'type': 'claim',
  'category': 'conversation',
  'metadata': {'status': 'auto', 'confidence': confidence},
  'created_at': DateTime.now().toIso8601String(),
};

/// Fake [SdkApiClient] serving a canned review queue.
class _ReviewTestClient implements SdkApiClient {
  final List<Map<String, dynamic>> claims;
  final List<String> promoted = [];
  final List<String> rejected = [];
  String? failWith;

  _ReviewTestClient(this.claims);
  @override
  Future<Map<String, dynamic>> listMemoryReviewQueue({
    int limit = 50,
    int offset = 0,
  }) async {
    if (failWith != null) throw Exception(failWith);
    final page = claims.skip(offset).take(limit).toList();
    return {'auto_claims': page, 'total': claims.length};
  }

  @override
  Future<Map<String, dynamic>> promoteClaim({
    required String id,
    bool confirmed = false,
    String reason = '',
  }) async {
    promoted.add(id);
    claims.removeWhere((c) => c['id'] == id);
    return {};
  }

  @override
  Future<Map<String, dynamic>> rejectClaim({
    required String id,
    bool confirmed = false,
  }) async {
    rejected.add(id);
    claims.removeWhere((c) => c['id'] == id);
    return {};
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}
