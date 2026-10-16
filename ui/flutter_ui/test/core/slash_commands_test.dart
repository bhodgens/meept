import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/core/slash_commands.dart';

void main() {
  group('SlashCommandRegistry.setCustomCommands', () {
    test('custom commands appear in all and match()', () {
      final registry = SlashCommandRegistry();
      registry.setCustomCommands([
        const SlashCommand(name: '/migrate', description: 'custom'),
        const SlashCommand(name: '/review-pr', description: 'custom'),
      ]);

      expect(registry.get('/migrate'), isNotNull);
      expect(
        registry.match('/revie'),
        anyElement((c) => c.name == '/review-pr'),
      );
    });

    test('replaces the previous custom set on each call', () {
      final registry = SlashCommandRegistry();
      registry.setCustomCommands([
        const SlashCommand(name: '/old', description: 'custom'),
      ]);
      registry.setCustomCommands([
        const SlashCommand(name: '/new-custom', description: 'custom'),
      ]);

      expect(registry.get('/old'), isNull);
      expect(registry.get('/new-custom'), isNotNull);
    });

    test('custom names colliding with built-ins are dropped', () {
      final registry = SlashCommandRegistry();
      registry.setCustomCommands([
        // /status is a built-in with local handling; a daemon template with
        // the same name must not shadow it.
        const SlashCommand(name: '/status', description: 'malicious? no'),
        const SlashCommand(name: '/safe', description: 'fine'),
      ]);

      expect(registry.get('/safe'), isNotNull);
      // The surviving /status is the built-in description.
      expect(registry.get('/status')!.description, 'show daemon status');
    });

    test('custom set does not leak into a fresh registry instance', () {
      // _custom is process-static by design (chat input + autocomplete popup
      // share the daemon-provided set), so a second instance sees it too.
      final a = SlashCommandRegistry();
      a.setCustomCommands([
        const SlashCommand(name: '/shared', description: 'custom'),
      ]);
      final b = SlashCommandRegistry();

      expect(b.get('/shared'), isNotNull);
      expect(b.all.length, greaterThan(a.all.length - 1));
    });
  });

  group('new built-in parity commands', () {
    test('read-only parity commands are registered', () {
      final registry = SlashCommandRegistry();
      for (final name in [
        '/usage',
        '/status',
        '/model',
        '/diff',
        '/eval',
        '/facts',
        '/prompts',
        '/session',
      ]) {
        expect(registry.get(name), isNotNull, reason: '$name missing');
      }
    });
  });
}
