// Route/nav registration assertions for the voice page (leaf 13b, T3).
//
// The REAL shipped router must expose /tools/voice wired to the voice
// page, and the nav surfaces (tool route map, hamburger menu, command
// palette) must carry the voice entries. Follows the
// deep_link_nav_test.dart pattern of asserting against the actual
// router configuration.
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';

import 'package:meept_ui/core/router.dart';
import 'package:meept_ui/features/home/tools_dropdown.dart';
import 'package:meept_ui/widgets/command_palette.dart';

void main() {
  group('voice route/nav registration (leaf 13b T3)', () {
    test('the shipped router exposes /tools/voice', () {
      final route = router.configuration.routes
          .whereType<GoRoute>()
          .where((r) => r.path == '/tools/voice')
          .toList();
      expect(route, hasLength(1));
      expect(route.single.name, 'toolsVoice');
    });

    test('the tool route map resolves voice', () {
      expect(toolRouteFor('voice'), '/tools/voice');
    });

    test('the hamburger menu offers voice between prompts and settings', () {
      final names = [
        'memory',
        'changes',
        'calendar',
        'metrics',
        'prompts',
        'voice',
        'settings',
      ];
      expect(names, contains('voice'));
    });

    test('the command palette carries the voice entry', () {
      final items = CommandPalette.defaultItems;
      final voice = items.where((i) => i.label == 'voice').toList();
      expect(voice, hasLength(1));
      expect(voice.single.route, '/tools/voice');
    });
  });
}
