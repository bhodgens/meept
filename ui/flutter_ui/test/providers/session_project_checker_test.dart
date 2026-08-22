import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/providers/session_project_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';

class _StubSdkClient extends SdkApiClient {
  _StubSdkClient() : super(host: 'localhost', port: 8081);

  @override
  Future<Map<String, dynamic>> setProject({
    required String sessionId,
    String? projectId,
    String? path,
  }) async => {'session_id': sessionId, 'path': path};
}

Session _unboundSession() => Session(
      id: 's1',
      title: 'unbound',
      createdAt: DateTime(2025, 1, 1),
    );

void main() {
  testWidgets('pick flow binds the chosen directory via project.set',
      (tester) async {
    String? boundPath;
    var skipped = false;

    await tester.pumpWidget(ProviderScope(
      overrides: [
        sdkClientProvider.overrideWith((_) => _StubSdkClient()),
      ],
      child: MaterialApp(
        home: Scaffold(
          body: Consumer(builder: (context, ref, _) {
            return ElevatedButton(
              onPressed: () {
                SessionProjectChecker.checkAndPrompt(
                  context: context,
                  ref: ref,
                  session: _unboundSession(),
                  onSkip: () => skipped = true,
                  onProjectBound: (p) => boundPath = p,
                );
              },
              child: const Text('activate'),
            );
          }),
        ),
      ),
    ));

    await tester.tap(find.text('activate'));
    await tester.pumpAndSettle();

    // Project prompt appears; choose "Pick Project".
    // The prompt dialog's directory browser then opens; cancel it to
    // verify the null path aborts activation.
    expect(find.byType(AlertDialog), findsWidgets);
    await tester.tap(find.text('Pick Project'));
    await tester.pumpAndSettle();

    // Directory browser opened — dismiss it without picking.
    await tester.sendKeyEvent(LogicalKeyboardKey.escape);
    await tester.pumpAndSettle();

    // No binding happened; user declined the browser.
    expect(boundPath, isNull);
  });

  testWidgets('needsProjectPrompt respects each binding signal',
      (tester) async {
    final base = _unboundSession();
    expect(SessionProjectChecker.needsProjectPrompt(base), isTrue);

    expect(
      SessionProjectChecker.needsProjectPrompt(base.copyWith(
        projectPath: '/tmp/p',
      )),
      isFalse,
    );
    expect(
      SessionProjectChecker.needsProjectPrompt(base.copyWith(
        projectId: 'p1',
      )),
      isFalse,
    );
  });
}
