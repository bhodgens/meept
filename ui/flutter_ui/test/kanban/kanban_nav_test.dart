// Route/nav registration assertions for the kanban board view (leaf
// 05/05), mirroring voice_nav_test.dart: the REAL shipped router must
// expose /kanban, and the command palette must carry the lowercase
// "kanban board" entry with the TUI ViewKanban parity key 'b'.

import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';

import 'package:meept_ui/core/router.dart';
import 'package:meept_ui/widgets/command_palette.dart';

void main() {
  group('kanban route/nav registration (leaf 05/05)', () {
    test('the shipped router exposes /kanban', () {
      final route = router.configuration.routes
          .whereType<GoRoute>()
          .where((r) => r.path == '/kanban')
          .toList();
      expect(route, hasLength(1));
      expect(route.single.name, 'kanban');
    });

    test('the command palette carries the kanban entry', () {
      final items = CommandPalette.defaultItems;
      final kanban = items.where((i) => i.label == 'kanban board').toList();
      expect(kanban, hasLength(1));
      expect(kanban.single.route, '/kanban');
      // TUI ViewKanban default key 'b' (internal/tui/config.go) — parity.
      expect(kanban.single.keybinding, 'b');
      expect(kanban.single.label, kanban.single.label.toLowerCase());
    });
  });
}
