import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:meept_ui/features/search/search_panel.dart';
import 'package:meept_ui/features/sessions/sessions_list.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/session_notifier.dart';
import '../../mocks/mock_websocket_service.dart';

/// Minimal client: the search button test only needs the sessions list to
/// load (empty is fine); SearchPanel itself never searches during the test.
class _EmptySdkClient extends SdkApiClient {
  _EmptySdkClient() : super(host: 'localhost', port: 65439);

  @override
  Future<List<Map<String, dynamic>>> listSessions({int? limit}) async => [];
}

void main() {
  testWidgets(
    'sessions header has search button that opens SearchPanel scoped to sessions',
    (tester) async {
      final router = GoRouter(
        initialLocation: '/sessions',
        routes: [
          GoRoute(
            path: '/sessions',
            builder: (_, __) => const Scaffold(
              body: SizedBox(height: 400, child: SessionsList()),
            ),
          ),
          GoRoute(
            path: '/tools/search',
            builder: (context, state) {
              final scope = state.extra;
              return Scaffold(
                body: SearchPanel(
                  initialScope: scope is SearchScope ? scope : SearchScope.all,
                ),
              );
            },
          ),
        ],
      );

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sessionProvider.overrideWith(
              (ref) => SessionNotifier(
                sdkClient: _EmptySdkClient(),
                websocket: MockWebSocketService(),
              ),
            ),
          ],
          child: MaterialApp.router(routerConfig: router),
        ),
      );
      await tester.pumpAndSettle();

      // Given the sessions tree header, a search button is present.
      final searchButton = find.byTooltip('search sessions');
      expect(searchButton, findsOneWidget);

      // When the search button is tapped.
      await tester.tap(searchButton);
      await tester.pumpAndSettle();

      // Then the existing SearchPanel opens pre-filtered to sessions scope.
      expect(find.byType(SearchPanel), findsOneWidget);
      final panel = tester.widget<SearchPanel>(find.byType(SearchPanel));
      expect(panel.initialScope, SearchScope.sessions);
    },
  );
}
