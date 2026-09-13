import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_form_builder/flutter_form_builder.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:meept_ui/features/settings/main_config_editor.dart';
import 'package:meept_ui/features/settings/orchestrator_config_editor.dart';
import 'package:meept_ui/features/settings/settings_panel.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/providers/tool_exit_guard.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/storage_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Stub client: serves a fixed [MainConfigFile] and records saves. A
/// non-null [saveError] makes saveMainConfig throw it, so the 400/403
/// paths can be exercised without a daemon.
class _StubMainConfigClient extends SdkApiClient {
  _StubMainConfigClient(this.file) : super(host: 'localhost', port: 8081);

  MainConfigFile file;
  String? lastSavedContent;
  SdkApiException? saveError;
  int loadCount = 0;

  @override
  Future<MainConfigFile> getMainConfig() async {
    loadCount++;
    return file;
  }

  @override
  Future<void> saveMainConfig(String content) async {
    if (saveError != null) throw saveError!;
    lastSavedContent = content;
    file = MainConfigFile(
      path: file.path,
      content: content,
      writable: file.writable,
    );
  }

  // The settings panel (and its sibling editors) fetch these while it
  // builds; short-circuit them so the panel-level test never touches the
  // network.
  @override
  Future<String> getClientConfig() async => '{}';

  @override
  Future<Map<String, dynamic>> getOrchestratorConfig() async => {};
}

const String _path = '/Users/u/.meept/meept.json5';
const String _content = '{\n  // main config\n  "log_level": "info",\n}';

MainConfigFile _file({bool writable = true}) =>
    MainConfigFile(path: _path, content: _content, writable: writable);

Future<void> _pump(WidgetTester tester, _StubMainConfigClient client) async {
  // A wide viewport keeps the header row (label + 3 buttons) free of
  // RenderFlex overflow.
  tester.view.physicalSize = const Size(1200, 1000);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [sdkClientProvider.overrideWithValue(client)],
      child: const MaterialApp(
        home: Scaffold(body: SingleChildScrollView(child: MainConfigEditor())),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

ElevatedButton _saveButton(WidgetTester tester) =>
    tester.widget<ElevatedButton>(find.byKey(mainConfigSaveKey));

/// Pumps the whole SettingsPanel, which owns the config-file chip row.
/// The viewport is wide and tall enough to render the editors without a
/// RenderFlex overflow.
Future<void> _pumpPanel(
  WidgetTester tester,
  _StubMainConfigClient client,
) async {
  tester.view.physicalSize = const Size(1200, 1600);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [sdkClientProvider.overrideWithValue(client)],
      child: const MaterialApp(
        home: Scaffold(body: SizedBox(width: 1100, child: SettingsPanel())),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

/// Selects the meept.json5 chip and scrolls the lazy ListView until the
/// embedded editor is built.
Future<void> _openMainEditor(WidgetTester tester) async {
  await tester.tap(find.text('meept.json5'));
  await tester.pumpAndSettle();
  for (var i = 0; i < 8 && !tester.any(find.byType(MainConfigEditor)); i++) {
    await tester.drag(find.byType(ListView), const Offset(0, -400));
    await tester.pumpAndSettle();
  }
}

/// Scrolls the panel's lazy ListView until [finder] is built.
Future<void> _scrollTo(WidgetTester tester, Finder finder) async {
  for (var i = 0; i < 8 && !tester.any(finder); i++) {
    await tester.drag(find.byType(ListView), const Offset(0, -400));
    await tester.pumpAndSettle();
  }
}

/// The settings panel on the real route shape: `/settings` renders the panel
/// and `/` is the chat home, so the shared back control's exit is observable
/// the way a user sees it.
GoRouter _settingsRouter() => GoRouter(
  initialLocation: '/settings',
  routes: [
    GoRoute(
      path: '/',
      builder: (_, __) => const Scaffold(body: Text('chat home marker')),
    ),
    GoRoute(
      path: '/settings',
      builder: (_, __) =>
          const Scaffold(body: SizedBox(width: 1100, child: SettingsPanel())),
    ),
  ],
);

/// Pumps the panel on the router, so the shell's back control / esc have
/// somewhere to exit to.
Future<void> _pumpPanelOnRouter(
  WidgetTester tester,
  _StubMainConfigClient client,
) async {
  tester.view.physicalSize = const Size(1200, 1600);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [sdkClientProvider.overrideWithValue(client)],
      child: MaterialApp.router(routerConfig: _settingsRouter()),
    ),
  );
  await tester.pumpAndSettle();
  expect(find.text('chat home marker'), findsNothing);
}

/// The shared back control from ToolPanelShell.
Finder _backControl() => find.byTooltip('back (esc)');

/// One of the orchestrator block's numeric fields, found by its label so the
/// test does not depend on widget order inside the panel's lazy list.
Finder _orchestratorField(String label) => find.byWidgetPredicate(
  (widget) => widget is TextField && widget.decoration?.labelText == label,
);

/// One of the connection form's persisted fields, found by its form name.
Finder _connectionField(String name) => find.byWidgetPredicate(
  (widget) => widget is FormBuilderTextField && widget.name == name,
);

/// Text currently rendered by a keyed field, read from its controller rather
/// than matched against a possibly-ambiguous finder.
String _textOf(WidgetTester tester, Key key) =>
    tester.widget<TextField>(find.byKey(key)).controller!.text;

/// Scrolls the panel's lazy ListView until the orchestrator editor is built.
Future<void> _openOrchestratorEditor(WidgetTester tester) async {
  await _scrollTo(tester, find.byType(OrchestratorConfigEditor));
  expect(find.byType(OrchestratorConfigEditor), findsOneWidget);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  // The panel-level test builds SettingsPanel, which reads connection
  // fields through StorageService -> SharedPreferences.
  SharedPreferences.setMockInitialValues(<String, Object>{
    'api_port': 8081,
    'api_host': 'localhost',
  });
  setUpAll(() async {
    await StorageService.instance.init();
  });

  testWidgets('loads the file and displays its path and content', (
    tester,
  ) async {
    final client = _StubMainConfigClient(_file());
    await _pump(tester, client);

    expect(find.byKey(mainConfigPathKey), findsOneWidget);
    expect(find.text(_path), findsOneWidget);
    expect(find.widgetWithText(TextField, _content), findsOneWidget);
    // Nothing to save or revert yet.
    expect(_saveButton(tester).onPressed, isNull);
    expect(find.byKey(mainConfigDirtyKey), findsNothing);
  });

  testWidgets('editing sets the dirty indicator and enables save', (
    tester,
  ) async {
    final client = _StubMainConfigClient(_file());
    await _pump(tester, client);

    expect(_saveButton(tester).onPressed, isNull);

    await tester.enterText(find.byKey(mainConfigTextKey), '{"a": 1}');
    await tester.pump();

    expect(find.byKey(mainConfigDirtyKey), findsOneWidget);
    expect(_saveButton(tester).onPressed, isNotNull);

    // Reverting back to the loaded text clears the dirty flag.
    await tester.enterText(find.byKey(mainConfigTextKey), _content);
    await tester.pump();
    expect(find.byKey(mainConfigDirtyKey), findsNothing);
    expect(_saveButton(tester).onPressed, isNull);
  });

  testWidgets('save posts the edited text and reports the saved path', (
    tester,
  ) async {
    final client = _StubMainConfigClient(_file());
    await _pump(tester, client);

    await tester.enterText(
      find.byKey(mainConfigTextKey),
      '{"log_level": "debug"}',
    );
    await tester.pump();
    await tester.tap(find.byKey(mainConfigSaveKey));
    await tester.pumpAndSettle();

    expect(client.lastSavedContent, '{"log_level": "debug"}');
    // The file did not change underneath, so the staleness check does not
    // interrupt an ordinary save.
    expect(find.byType(AlertDialog), findsNothing);
    expect(find.byKey(mainConfigNoticeKey), findsOneWidget);
    expect(find.textContaining('restart the daemon'), findsOneWidget);
    expect(find.textContaining(_path), findsWidgets);
    // A successful save is the new loaded state: nothing dirty.
    expect(find.byKey(mainConfigDirtyKey), findsNothing);
  });

  testWidgets('400 shows the daemon parse error verbatim and keeps the text', (
    tester,
  ) async {
    const parserError =
        "invalid character '}' looking for beginning of object key string";
    final client = _StubMainConfigClient(_file())
      ..saveError = SdkApiException(message: parserError, statusCode: 400);
    await _pump(tester, client);

    await tester.enterText(find.byKey(mainConfigTextKey), '{"a": }');
    await tester.pump();
    await tester.tap(find.byKey(mainConfigSaveKey));
    await tester.pumpAndSettle();

    expect(find.byKey(mainConfigErrorKey), findsOneWidget);
    expect(find.textContaining(parserError), findsOneWidget);
    // The user's text survives the failed save and is still dirty.
    expect(find.widgetWithText(TextField, '{"a": }'), findsOneWidget);
    expect(find.byKey(mainConfigDirtyKey), findsOneWidget);
  });

  testWidgets('403 explains the loopback-only rule and keeps the text', (
    tester,
  ) async {
    final client = _StubMainConfigClient(_file())
      ..saveError = SdkApiException(
        message: 'config write is restricted to loopback clients',
        statusCode: 403,
      );
    await _pump(tester, client);

    await tester.enterText(find.byKey(mainConfigTextKey), '{"a": 2}');
    await tester.pump();
    await tester.tap(find.byKey(mainConfigSaveKey));
    await tester.pumpAndSettle();

    expect(find.byKey(mainConfigErrorKey), findsOneWidget);
    expect(find.textContaining('same host'), findsOneWidget);
    expect(find.textContaining('loopback'), findsOneWidget);
    expect(find.widgetWithText(TextField, '{"a": 2}'), findsOneWidget);
    expect(find.byKey(mainConfigDirtyKey), findsOneWidget);
  });

  testWidgets('writable=false renders read-only with an explanation', (
    tester,
  ) async {
    final client = _StubMainConfigClient(_file(writable: false));
    await _pump(tester, client);

    final field = tester.widget<TextField>(find.byKey(mainConfigTextKey));
    expect(field.readOnly, isTrue);
    expect(find.byKey(mainConfigReadOnlyNoteKey), findsOneWidget);
    expect(find.textContaining('not writable'), findsOneWidget);
    // Even if it were edited, save stays disabled for an unwritable file.
    expect(_saveButton(tester).onPressed, isNull);
  });

  testWidgets('revert restores the loaded content and clears dirty', (
    tester,
  ) async {
    final client = _StubMainConfigClient(_file());
    await _pump(tester, client);

    await tester.enterText(find.byKey(mainConfigTextKey), '{"gone": true}');
    await tester.pump();
    expect(find.byKey(mainConfigDirtyKey), findsOneWidget);

    await tester.tap(find.byKey(mainConfigRevertKey));
    await tester.pumpAndSettle();

    expect(find.widgetWithText(TextField, _content), findsOneWidget);
    expect(find.byKey(mainConfigDirtyKey), findsNothing);
    expect(_saveButton(tester).onPressed, isNull);
  });

  testWidgets('reload warns before discarding unsaved edits', (tester) async {
    final client = _StubMainConfigClient(_file());
    await _pump(tester, client);

    await tester.enterText(find.byKey(mainConfigTextKey), '{"local": true}');
    await tester.pump();

    // The daemon's copy changed underneath us.
    client.file = const MainConfigFile(
      path: _path,
      content: '{"from_daemon": true}',
      writable: true,
    );

    await tester.tap(find.byKey(mainConfigReloadKey));
    await tester.pumpAndSettle();

    // The warning names the loss before anything is fetched.
    expect(find.textContaining('discards the edits'), findsOneWidget);
    expect(client.loadCount, 1);

    await tester.tap(find.text('discard and reload'));
    await tester.pumpAndSettle();

    expect(client.loadCount, 2);
    expect(
      find.widgetWithText(TextField, '{"from_daemon": true}'),
      findsOneWidget,
    );
    expect(find.byKey(mainConfigDirtyKey), findsNothing);
  });

  testWidgets('settings panel renders the editable editor for meept.json5', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1200, 1600);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    final client = _StubMainConfigClient(_file());

    await tester.pumpWidget(
      ProviderScope(
        overrides: [sdkClientProvider.overrideWithValue(client)],
        child: const MaterialApp(
          home: Scaffold(body: SizedBox(width: 1100, child: SettingsPanel())),
        ),
      ),
    );
    await tester.pumpAndSettle();

    // The chip is labelled with the bare file name -- the old
    // 'meept.json5 (read-only)' treatment is gone.
    expect(find.text('meept.json5'), findsOneWidget);

    await tester.tap(find.text('meept.json5'));
    await tester.pumpAndSettle();

    // The editor sits at the bottom of the panel's lazy ListView; scroll
    // until it is built.
    for (var i = 0; i < 8 && !tester.any(find.byType(MainConfigEditor)); i++) {
      await tester.drag(find.byType(ListView), const Offset(0, -400));
      await tester.pumpAndSettle();
    }

    expect(find.byType(MainConfigEditor), findsOneWidget);
    expect(find.text(_path), findsOneWidget);
    expect(find.widgetWithText(TextField, _content), findsOneWidget);
  });

  testWidgets(
    'switching chips with unsaved meept.json5 edits warns, and cancel keeps them',
    (tester) async {
      final client = _StubMainConfigClient(_file());
      await _pumpPanel(tester, client);
      await _openMainEditor(tester);

      // Type into the main config editor. SettingsPanel._hasChanges never
      // sees this -- the editor reports it through onDirtyChanged.
      await tester.enterText(find.byKey(mainConfigTextKey), '{"edited": true}');
      await tester.pump();
      expect(find.byKey(mainConfigDirtyKey), findsOneWidget);

      await tester.tap(find.text('client.json5'));
      await tester.pumpAndSettle();

      // The warning names the file whose edits would be lost.
      expect(find.byType(AlertDialog), findsOneWidget);
      expect(
        find.textContaining('unsaved changes in meept.json5'),
        findsOneWidget,
      );

      await tester.tap(find.text('cancel'));
      await tester.pumpAndSettle();

      // Cancel is a no-op: still on meept.json5, edits intact.
      expect(find.byType(AlertDialog), findsNothing);
      expect(find.byType(MainConfigEditor), findsOneWidget);
      expect(
        find.widgetWithText(TextField, '{"edited": true}'),
        findsOneWidget,
      );
      expect(find.byKey(mainConfigDirtyKey), findsOneWidget);
      expect(find.text('// client.json5'), findsNothing);
    },
  );

  testWidgets(
    'discarding the unsaved meept.json5 edits switches to the chosen chip',
    (tester) async {
      final client = _StubMainConfigClient(_file());
      await _pumpPanel(tester, client);
      await _openMainEditor(tester);

      await tester.enterText(find.byKey(mainConfigTextKey), '{"edited": true}');
      await tester.pump();

      await tester.tap(find.text('client.json5'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('discard'));
      await tester.pumpAndSettle();

      expect(find.byType(MainConfigEditor), findsNothing);
      expect(find.text('// client.json5'), findsOneWidget);
    },
  );

  testWidgets('switching chips with no unsaved edits does not warn', (
    tester,
  ) async {
    final client = _StubMainConfigClient(_file());
    await _pumpPanel(tester, client);
    await _openMainEditor(tester);

    await tester.tap(find.text('client.json5'));
    await tester.pumpAndSettle();

    expect(find.byType(AlertDialog), findsNothing);
    expect(find.text('// client.json5'), findsOneWidget);
  });

  // The panel hands the shell an exit guard, so the shared back control and
  // esc cannot drop unsaved config edits the way they used to (the chip guard
  // only covered chip switches).
  group('exit guard for the shared back control', () {
    testWidgets('back with nothing unsaved exits without a warning', (
      tester,
    ) async {
      final client = _StubMainConfigClient(_file());
      await _pumpPanelOnRouter(tester, client);

      await tester.tap(_backControl());
      await tester.pumpAndSettle();

      expect(find.byType(AlertDialog), findsNothing);
      expect(find.text('chat home marker'), findsOneWidget);
    });

    testWidgets('back with unsaved meept.json5 edits warns, and cancel '
        'keeps the edits and the panel', (tester) async {
      final client = _StubMainConfigClient(_file());
      await _pumpPanelOnRouter(tester, client);
      await _openMainEditor(tester);

      await tester.enterText(find.byKey(mainConfigTextKey), '{"edited": true}');
      await tester.pump();
      expect(find.byKey(mainConfigDirtyKey), findsOneWidget);

      await tester.tap(_backControl());
      await tester.pumpAndSettle();

      expect(find.byType(AlertDialog), findsOneWidget);
      expect(
        find.textContaining('unsaved changes in meept.json5'),
        findsOneWidget,
      );

      await tester.tap(find.text('cancel'));
      await tester.pumpAndSettle();

      // Nothing was torn down: the panel, the editor and the edits are all
      // still there, and we never left the settings route.
      expect(find.byType(AlertDialog), findsNothing);
      expect(find.text('chat home marker'), findsNothing);
      expect(find.byType(MainConfigEditor), findsOneWidget);
      expect(
        find.widgetWithText(TextField, '{"edited": true}'),
        findsOneWidget,
      );
      expect(find.byKey(mainConfigDirtyKey), findsOneWidget);
    });

    testWidgets('esc with unsaved meept.json5 edits warns, and cancel keeps '
        'the panel', (tester) async {
      final client = _StubMainConfigClient(_file());
      await _pumpPanelOnRouter(tester, client);
      await _openMainEditor(tester);

      await tester.enterText(find.byKey(mainConfigTextKey), '{"edited": true}');
      await tester.pump();

      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pumpAndSettle();

      expect(find.byType(AlertDialog), findsOneWidget);
      expect(
        find.textContaining('unsaved changes in meept.json5'),
        findsOneWidget,
      );

      await tester.tap(find.text('cancel'));
      await tester.pumpAndSettle();

      expect(find.text('chat home marker'), findsNothing);
      expect(
        find.widgetWithText(TextField, '{"edited": true}'),
        findsOneWidget,
      );
    });

    testWidgets('choosing to discard on exit leaves the panel', (tester) async {
      final client = _StubMainConfigClient(_file());
      await _pumpPanelOnRouter(tester, client);
      await _openMainEditor(tester);

      await tester.enterText(find.byKey(mainConfigTextKey), '{"edited": true}');
      await tester.pump();

      await tester.tap(_backControl());
      await tester.pumpAndSettle();
      await tester.tap(find.text('discard and exit'));
      await tester.pumpAndSettle();

      expect(find.text('chat home marker'), findsOneWidget);
      expect(find.byType(SettingsPanel), findsNothing);
    });

    testWidgets('unsaved edits in the generic config editor also guard the '
        'exit', (tester) async {
      // The settings panel's _hasChanges tracks client/models/menubar; those
      // edits are as losable on exit as meept.json5 ones, so one predicate
      // guards both.
      final client = _StubMainConfigClient(_file());
      await _pumpPanelOnRouter(tester, client);
      await _scrollTo(tester, find.byKey(settingsConfigTextKey));
      expect(find.byKey(settingsConfigTextKey), findsOneWidget);

      await tester.enterText(
        find.byKey(settingsConfigTextKey),
        '{"client": "edited"}',
      );
      await tester.pump();

      await tester.tap(_backControl());
      await tester.pumpAndSettle();

      expect(
        find.textContaining('unsaved changes in client.json5'),
        findsOneWidget,
      );

      await tester.tap(find.text('cancel'));
      await tester.pumpAndSettle();

      expect(find.text('chat home marker'), findsNothing);
      expect(
        find.widgetWithText(TextField, '{"client": "edited"}'),
        findsOneWidget,
      );
    });

    // The orchestrator block is written to the same meept.json5 this panel
    // guards, so back/esc must ask about it too: before this, only the
    // whole-file editor and the generic editor were covered (F17).
    testWidgets(
      'back with unsaved orchestrator-block edits warns, and cancel keeps them',
      (tester) async {
        final client = _StubMainConfigClient(_file());
        await _pumpPanelOnRouter(tester, client);
        await _openOrchestratorEditor(tester);

        await tester.enterText(_orchestratorField('max plan steps'), '40');
        await tester.pump();

        await tester.tap(_backControl());
        await tester.pumpAndSettle();

        expect(find.byType(AlertDialog), findsOneWidget);
        expect(
          find.textContaining('unsaved changes in the orchestrator block'),
          findsOneWidget,
        );

        await tester.tap(find.text('cancel'));
        await tester.pumpAndSettle();

        // Cancelling keeps the panel, the editor and the typed value.
        expect(find.text('chat home marker'), findsNothing);
        expect(find.byType(OrchestratorConfigEditor), findsOneWidget);
        expect(find.widgetWithText(TextField, '40'), findsOneWidget);
      },
    );

    testWidgets('esc with unsaved orchestrator-block edits warns too', (
      tester,
    ) async {
      final client = _StubMainConfigClient(_file());
      await _pumpPanelOnRouter(tester, client);
      await _openOrchestratorEditor(tester);

      await tester.enterText(_orchestratorField('max plan steps'), '40');
      await tester.pump();

      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pumpAndSettle();

      expect(
        find.textContaining('unsaved changes in the orchestrator block'),
        findsOneWidget,
      );

      await tester.tap(find.text('discard and exit'));
      await tester.pumpAndSettle();

      expect(find.text('chat home marker'), findsOneWidget);
      expect(find.byType(SettingsPanel), findsNothing);
    });

    // The connection form saves separately from the config files, but leaving
    // the panel drops its unsaved values just the same.
    testWidgets(
      'back with unsaved connection-form edits warns, and cancel keeps them',
      (tester) async {
        final client = _StubMainConfigClient(_file());
        await _pumpPanelOnRouter(tester, client);

        await tester.enterText(_connectionField('daemon_host'), 'otherhost');
        await tester.pump();

        await tester.tap(_backControl());
        await tester.pumpAndSettle();

        expect(
          find.textContaining('unsaved changes in the connection form'),
          findsOneWidget,
        );

        await tester.tap(find.text('cancel'));
        await tester.pumpAndSettle();

        expect(find.text('chat home marker'), findsNothing);
        expect(
          tester
              .widget<EditableText>(
                find.descendant(
                  of: _connectionField('daemon_host'),
                  matching: find.byType(EditableText),
                ),
              )
              .controller
              .text,
          'otherhost',
        );
      },
    );

    testWidgets('a saved connection form does not guard the exit', (
      tester,
    ) async {
      final client = _StubMainConfigClient(_file());
      await _pumpPanelOnRouter(tester, client);

      await tester.enterText(_connectionField('daemon_host'), 'otherhost');
      await tester.pump();

      await tester.tap(find.text('save connection'));
      await tester.pumpAndSettle();

      // The form's values are the saved ones now: back exits without a
      // warning.
      await tester.tap(_backControl());
      await tester.pumpAndSettle();

      expect(find.byType(AlertDialog), findsNothing);
      expect(find.text('chat home marker'), findsOneWidget);
    });
  });

  // The panel publishes its guard in the shared registry, so the paths that
  // never touch ToolPanelShell (the hamburger menu's tool switch, a home tab
  // switch) ask the same question the shared back control asks.
  group('shared exit guard registration', () {
    testWidgets(
      'registers while mounted, refuses a switch while dirty, releases on '
      'dispose',
      (tester) async {
        final client = _StubMainConfigClient(_file());
        final container = ProviderContainer(
          overrides: [sdkClientProvider.overrideWithValue(client)],
        );
        tester.view.physicalSize = const Size(1200, 1600);
        tester.view.devicePixelRatio = 1.0;
        addTearDown(tester.view.reset);

        await tester.pumpWidget(
          UncontrolledProviderScope(
            container: container,
            child: const MaterialApp(
              home: Scaffold(
                body: SizedBox(width: 1100, child: SettingsPanel()),
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();

        final registry = container.read(toolExitGuardProvider);
        expect(registry.guard, isNotNull);

        // Nothing unsaved: the registered guard allows a switch straight
        // away, with no dialog. Calling it directly (not requestExit) keeps
        // the registration for the dirty case below.
        expect(await registry.guard!(), isTrue);
        expect(find.byType(AlertDialog), findsNothing);

        // With unsaved edits the same guard warns, and cancelling refuses
        // the switch. A refusal keeps the registration, so the next path to
        // ask still gets an answer.
        await _scrollTo(tester, find.byKey(settingsConfigTextKey));
        await tester.enterText(
          find.byKey(settingsConfigTextKey),
          '{"client": "edited"}',
        );
        await tester.pump();

        final pending = registry.requestExit();
        await tester.pumpAndSettle();
        expect(find.byType(AlertDialog), findsOneWidget);
        expect(
          find.textContaining('unsaved changes in client.json5'),
          findsOneWidget,
        );

        await tester.tap(find.text('cancel'));
        await tester.pumpAndSettle();

        expect(await pending, isFalse);
        expect(registry.guard, isNotNull);

        // Unmounting the panel releases the registration: a gone panel
        // cannot veto a later switch.
        await tester.pumpWidget(const SizedBox());
        expect(registry.guard, isNull);

        container.dispose();
      },
    );
  });

  // POST /api/v1/config/main rewrites the whole file, so the text captured at
  // load time silently reverted a save made since: the sibling orchestrator
  // editor writes the same file, and so does the CLI (F63). Every save now
  // re-reads it and reconciles.
  group('stale whole-file save', () {
    // The daemon's copy of meept.json5 after another writer saved it.
    const changed = MainConfigFile(
      path: _path,
      content: '{\n  "orchestrator": {"max_phases": 4},\n}',
      writable: true,
    );

    Future<void> editThenChangeFileUnder(
      WidgetTester tester,
      _StubMainConfigClient client,
    ) async {
      await _pump(tester, client);
      await tester.enterText(find.byKey(mainConfigTextKey), '{"mine": true}');
      await tester.pump();
      expect(find.byKey(mainConfigDirtyKey), findsOneWidget);

      // Another writer (the orchestrator block, the CLI) saves the same file.
      client.file = changed;
    }

    testWidgets('a save whose file changed underneath asks before writing', (
      tester,
    ) async {
      final client = _StubMainConfigClient(_file());
      await editThenChangeFileUnder(tester, client);

      await tester.tap(find.byKey(mainConfigSaveKey));
      await tester.pumpAndSettle();

      expect(find.byType(AlertDialog), findsOneWidget);
      expect(find.text('config changed on disk'), findsOneWidget);
      // Nothing was written while the choice is open.
      expect(client.lastSavedContent, isNull);
      expect(_textOf(tester, mainConfigTextKey), '{"mine": true}');
    });

    testWidgets('cancel on the conflict keeps the text and writes nothing', (
      tester,
    ) async {
      final client = _StubMainConfigClient(_file());
      await editThenChangeFileUnder(tester, client);

      await tester.tap(find.byKey(mainConfigSaveKey));
      await tester.pumpAndSettle();
      await tester.tap(find.text('cancel'));
      await tester.pumpAndSettle();

      expect(client.lastSavedContent, isNull);
      expect(_textOf(tester, mainConfigTextKey), '{"mine": true}');
      // Still dirty: the edited copy is the only copy of the work.
      expect(find.byKey(mainConfigDirtyKey), findsOneWidget);
    });

    testWidgets('overwrite writes the editor text on an explicit answer', (
      tester,
    ) async {
      final client = _StubMainConfigClient(_file());
      await editThenChangeFileUnder(tester, client);

      await tester.tap(find.byKey(mainConfigSaveKey));
      await tester.pumpAndSettle();
      await tester.tap(find.text('overwrite'));
      await tester.pumpAndSettle();

      expect(client.lastSavedContent, '{"mine": true}');
      expect(find.byKey(mainConfigNoticeKey), findsOneWidget);
      expect(find.byKey(mainConfigDirtyKey), findsNothing);
    });

    testWidgets('reload takes the daemon copy and writes nothing', (
      tester,
    ) async {
      final client = _StubMainConfigClient(_file());
      await editThenChangeFileUnder(tester, client);

      await tester.tap(find.byKey(mainConfigSaveKey));
      await tester.pumpAndSettle();
      // Scoped to the dialog: the editor header also has a 'reload' control.
      await tester.tap(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.text('reload'),
        ),
      );
      await tester.pumpAndSettle();

      expect(client.lastSavedContent, isNull);
      expect(_textOf(tester, mainConfigTextKey), changed.content);
      expect(find.byKey(mainConfigDirtyKey), findsNothing);
    });
  });
}
