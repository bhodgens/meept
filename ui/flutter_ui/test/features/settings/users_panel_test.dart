import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/settings/users_panel.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';

/// Stub client for the users panel: answers the single main-config endpoint
/// it is allowed to read. [getMainConfig] is the ONLY config source the
/// panel uses, so a [mainConfigCalls] count proves the panel no longer
/// reaches for a second route.
class _StubUsersClient extends SdkApiClient {
  _StubUsersClient(this.content) : super(host: 'localhost', port: 8081);

  final String content;
  int mainConfigCalls = 0;
  bool fail = false;

  @override
  Future<MainConfigFile> getMainConfig() async {
    mainConfigCalls++;
    if (fail) {
      throw SdkApiException(message: 'boom', statusCode: 500);
    }
    return MainConfigFile(
      path: '/Users/u/.meept/meept.json5',
      content: content,
      writable: true,
    );
  }
}

Future<void> _pump(WidgetTester tester, _StubUsersClient client) async {
  tester.view.physicalSize = const Size(1000, 800);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [sdkClientProvider.overrideWithValue(client)],
      child: const MaterialApp(
        home: Scaffold(body: SizedBox(width: 900, child: UsersPanel())),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

/// A JSON5-ish main config: comments and a trailing comma are present on
/// purpose, so the panel's decode is exercised the same way it was when the
/// text arrived from the retired endpoint.
String _mainConfig({required bool enabled}) =>
    '''
{
  // daemon main config
  "log_level": "info",
  "multiuser": {
    "enabled": $enabled,
  },
}
''';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('reads multiuser.enabled from the main config endpoint', (
    tester,
  ) async {
    final client = _StubUsersClient(_mainConfig(enabled: true));
    await _pump(tester, client);

    expect(client.mainConfigCalls, 1);
    expect(find.text('multi-user: on'), findsOneWidget);
  });

  testWidgets('reports multi-user off when enabled is false', (tester) async {
    final client = _StubUsersClient(_mainConfig(enabled: false));
    await _pump(tester, client);

    expect(find.text('multi-user: off'), findsOneWidget);
    expect(find.textContaining('multi-user is disabled'), findsOneWidget);
  });

  testWidgets('reports unknown when the config fetch fails', (tester) async {
    final client = _StubUsersClient(_mainConfig(enabled: true))..fail = true;
    await _pump(tester, client);

    expect(find.text('multi-user: unknown'), findsOneWidget);
    expect(find.textContaining('could not load daemon config'), findsOneWidget);
  });
}
