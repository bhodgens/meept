import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/settings/client_prefs_editor.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';

class _StubConfigClient extends SdkApiClient {
  String configText = '''
{
  "gui": { "layout": "sidebar" },
  "chat": {
    "auto_copy_on_release": false,
    "scroll_speed": 3,
    "verbosity": "normal"
  },
  "rendering": {
    "markdown": true,
    "sidebar_animation": true,
    "syntax_highlighting": true,
    "theme": "monokai",
    "word_wrap": true
  },
  "session": { "auto_resume": true, "default_name": "default" },
  // json5 comment with trailing comma below
}
''';
  final List<Map<String, dynamic>> patches = [];

  _StubConfigClient() : super(host: 'localhost', port: 8081);

  @override
  Future<String> getClientConfig() async => configText;

  @override
  Future<void> setClientConfig(Map<String, dynamic> patch) async {
    patches.add(patch);
    // Apply the merge-patch to the stored text (JSON round-trip is fine
    // for the stub; the daemon's hujson handles JSON5).
    final cfg = parseClientConfig(configText);
    _deepMerge(cfg, patch);
    configText = const JsonEncoder.withIndent('  ').convert(cfg);
  }

  void _deepMerge(Map<String, dynamic> base, Map<String, dynamic> patch) {
    for (final e in patch.entries) {
      if (e.value is Map<String, dynamic> &&
          base[e.key] is Map<String, dynamic>) {
        _deepMerge(
          base[e.key] as Map<String, dynamic>,
          e.value as Map<String, dynamic>,
        );
      } else {
        base[e.key] = e.value;
      }
    }
  }
}

Future<void> _pump(WidgetTester tester, _StubConfigClient client) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [sdkClientProvider.overrideWithValue(client)],
      child: const MaterialApp(
        home: Scaffold(body: SingleChildScrollView(child: ClientPrefsEditor())),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('renders all preference rows from live config', (tester) async {
    final client = _StubConfigClient();
    await _pump(tester, client);

    expect(find.text('client preferences'), findsOneWidget);
    expect(find.text('chat scroll speed'), findsNWidgets(2)); // label + field
    expect(find.text('auto-copy on release'), findsOneWidget);
    expect(find.text('markdown rendering'), findsOneWidget);
    // Seeded int value lands in its field.
    expect(find.widgetWithText(TextField, '3'), findsOneWidget);
    expect(find.byType(Switch), findsNWidgets(6));
    expect(find.byType(DropdownButton<String>), findsNWidgets(2));
  });

  testWidgets('toggling a switch PATCHes the dotted key', (tester) async {
    final client = _StubConfigClient();
    await _pump(tester, client);

    // Toggle 'auto-copy on release' (first Switch).
    await tester.tap(find.byType(Switch).first);
    await tester.pumpAndSettle();

    expect(client.patches, hasLength(1));
    expect(client.patches.first, {
      'chat': {'auto_copy_on_release': true},
    });
  });

  testWidgets('choosing a dropdown value PATCHes and persists', (tester) async {
    final client = _StubConfigClient();
    final container = ProviderContainer(
      overrides: [sdkClientProvider.overrideWithValue(client)],
    );
    addTearDown(container.dispose);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(
          home: Scaffold(
            body: SingleChildScrollView(child: ClientPrefsEditor()),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byType(DropdownButton<String>).first);
    await tester.pumpAndSettle();
    await tester.tap(find.text('verbose').last);
    await tester.pumpAndSettle();

    expect(client.patches, hasLength(1));
    expect(client.patches.first, {
      'chat': {'verbosity': 'verbose'},
    });
    // Live-applied to the running provider, not just persisted.
    expect(container.read(verbosityProvider), VerbosityLevel.verbose);
    // Re-fetch applied the merge: switch a second toggle and confirm the
    // earlier value survived in the stub text.
    expect(client.configText, contains('"verbosity": "verbose"'));
  });

  testWidgets('filter narrows rows by label and dotted key', (tester) async {
    final client = _StubConfigClient();
    await _pump(tester, client);

    expect(find.text('markdown rendering'), findsOneWidget);

    // Filter by label substring.
    await tester.enterText(
      find.widgetWithText(TextField, ''), // the filter field
      'wrap',
    );
    await tester.pump();

    expect(find.text('word wrap'), findsOneWidget);
    expect(find.text('markdown rendering'), findsNothing);

    // Filter by dotted key.
    await tester.enterText(find.byType(TextField).first, 'chat.scroll');
    await tester.pump();
    expect(find.text('chat scroll speed'), findsNWidgets(2)); // label + hint
    expect(find.text('word wrap'), findsNothing);

    // No matches shows an empty-state message.
    await tester.enterText(find.byType(TextField).first, 'zzzz');
    await tester.pump();
    expect(find.textContaining('no settings match'), findsOneWidget);

    // Clear restores all rows.
    await tester.tap(find.byIcon(Icons.close));
    await tester.pump();
    expect(find.text('markdown rendering'), findsOneWidget);
  });

  testWidgets('parseClientConfig strips comments and trailing commas', (
    tester,
  ) async {
    const raw = '''
// leading comment
{
  "a": 1, // inline
  /* block */
  "b": { "c": true, },
}
''';
    final parsed = parseClientConfig(raw);
    expect(parsed['a'], 1);
    expect((parsed['b'] as Map)['c'], true);
  });
}
