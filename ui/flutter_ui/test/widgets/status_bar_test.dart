import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept_ui/providers/verbosity_provider.dart';
import 'package:meept_ui/providers/status_message_provider.dart';
import 'package:meept_ui/providers/project_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/widgets/status_bar.dart';

/// A [CurrentProjectNotifier] that skips the SDK call entirely and starts
/// from [CurrentProject.empty]. Tests don't need to call refresh(), so the
/// SDK client is never invoked. We pass a real [SdkApiClient] to satisfy
/// the constructor signature (it's only stored, never used in tests).
class _FakeProjectNotifier extends CurrentProjectNotifier {
  _FakeProjectNotifier() : super(SdkApiClient(host: 'localhost'));
}

Widget _wrap({required int tab}) {
  return ProviderScope(
    overrides: [
      // VerbosityNotifier() defaults to VerbosityLevel.normal (1).
      verbosityProvider.overrideWith((ref) => VerbosityNotifier()),
      currentProjectProvider.overrideWith((ref) => _FakeProjectNotifier()),
    ],
    child: MaterialApp(
      home: Scaffold(body: StatusBar(selectedTabIndex: tab)),
    ),
  );
}

void main() {
  testWidgets('renders verbosity + connection', (tester) async {
    await tester.pumpWidget(_wrap(tab: 0));
    await tester.pump();
    expect(find.textContaining('verbosity'), findsOneWidget);
  });

  testWidgets('renders transient status message when set, hides other parts',
      (tester) async {
    final container = ProviderContainer(
      overrides: [
        verbosityProvider.overrideWith((ref) => VerbosityNotifier()),
        currentProjectProvider.overrideWith((ref) => _FakeProjectNotifier()),
      ],
    );
    addTearDown(container.dispose);
    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(
          home: Scaffold(body: StatusBar(selectedTabIndex: 0)),
        ),
      ),
    );
    await tester.pump();
    container.read(statusMessageProvider.notifier).state = 'session archived';
    await tester.pump();
    expect(find.text('session archived'), findsOneWidget);
    expect(find.textContaining('verbosity'), findsNothing);
  });

  testWidgets('keybind hint shows sessions-specific text on sessions tab',
      (tester) async {
    await tester.pumpWidget(_wrap(tab: 1));
    await tester.pump();
    // Two assertions guard against a session title like "archive-test"
    // leaking into the session-part and false-matching the keybind hint.
    expect(find.textContaining('dbl-click'), findsOneWidget);
    expect(find.textContaining('archive'), findsOneWidget);
  });

  testWidgets('keybind hint shows chat-specific text on chat tab',
      (tester) async {
    await tester.pumpWidget(_wrap(tab: 0));
    await tester.pump();
    expect(find.textContaining('focus'), findsOneWidget);
    expect(find.textContaining('verbosity'), findsOneWidget);
  });
}
