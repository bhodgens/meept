import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/providers/rendering_prefs_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';

class _StubConfigClient extends SdkApiClient {
  _StubConfigClient(this.configText) : super(host: 'localhost', port: 8081);
  final String configText;

  @override
  Future<String> getClientConfig() async => configText;
}

void main() {
  group('RenderingPrefsNotifier.load', () {
    test('parses rendering + session blocks from daemon config', () async {
      final notifier = RenderingPrefsNotifier();
      await notifier.load(
        _StubConfigClient('''
{
  "rendering": { "markdown": false, "word_wrap": false },
  "session": { "auto_resume": false }
}
'''),
      );
      expect(notifier.state.markdown, isFalse);
      expect(notifier.state.wordWrap, isFalse);
      expect(notifier.state.autoResume, isFalse);
    });

    test('defaults hold for missing keys', () async {
      final notifier = RenderingPrefsNotifier();
      await notifier.load(_StubConfigClient('{"gui": {"layout": "sidebar"}}'));
      expect(notifier.state.markdown, isTrue);
      expect(notifier.state.wordWrap, isTrue);
      expect(notifier.state.autoResume, isTrue);
    });

    test('unreachable daemon keeps defaults (no throw)', () async {
      final notifier = RenderingPrefsNotifier();
      // Client whose getClientConfig always throws.
      final failing = _FailingClient();
      await notifier.load(failing);
      expect(notifier.state.autoResume, isTrue);
    });
  });

  test('setters update individual fields', () {
    final notifier = RenderingPrefsNotifier();
    notifier.setMarkdown(false);
    expect(notifier.state.markdown, isFalse);
    expect(notifier.state.wordWrap, isTrue);
    notifier.setWordWrap(false);
    expect(notifier.state.wordWrap, isFalse);
    expect(notifier.state.markdown, isFalse); // unchanged
  });
}

class _FailingClient implements SdkApiClient {
  @override
  dynamic noSuchMethod(Invocation invocation) => throw Exception('offline');
}
