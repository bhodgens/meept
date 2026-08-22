// Layout smoke tests: pump each of the 5 alternative layouts and assert
// they render their primary regions without exceptions. These catch the
// class of bugs found in the visual pass (missing Material ancestor,
// unbounded-flex crashes) that unit tests on individual widgets miss.
//
// Note: main_layout_preview.dart is an entry point (has its own main()),
// so we rebuild the same Material wrapper it uses here.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/layouts/layout_1_classic_sidebar.dart';
import 'package:meept_ui/layouts/layout_2_bottom_nav.dart';
import 'package:meept_ui/layouts/layout_3_radial_hub.dart';
import 'package:meept_ui/layouts/layout_4_top_tabs_panels.dart';
import 'package:meept_ui/layouts/layout_5_grid_dashboard.dart';
import 'package:meept_ui/theme/colors.dart';

Widget _wrap(Widget child) => MaterialApp(
      theme: ThemeData.dark(),
      home: Material(
        color: CyberpunkColors.black,
        child: child,
      ),
    );

void main() {
  testWidgets('layout 1 (classic sidebar) renders without exceptions',
      (tester) async {
    await tester.pumpWidget(_wrap(const Layout1ClassicSidebar()));
    await tester.pump();
    // Sidebar nav items render.
    expect(find.text('meept'), findsOneWidget);
    expect(find.text('chat'), findsOneWidget);
    // Chat input placeholder renders (requires Material ancestor).
    expect(find.text('type a message...'), findsOneWidget);
  });

  testWidgets('layout 2 (bottom nav) renders without exceptions',
      (tester) async {
    await tester.pumpWidget(_wrap(const Layout2BottomNav()));
    await tester.pump();
  });

  testWidgets('layout 3 (radial hub) renders without exceptions',
      (tester) async {
    await tester.pumpWidget(_wrap(const Layout3RadialHub()));
    await tester.pump();
  });

  testWidgets('layout 4 (top tabs + panels) renders without exceptions',
      (tester) async {
    await tester.pumpWidget(_wrap(const Layout4TopTabsPanels()));
    await tester.pump();
    // Tab labels render.
    expect(find.text('chat'), findsWidgets);
  });

  testWidgets('layout 5 (grid dashboard) renders without exceptions',
      (tester) async {
    await tester.pumpWidget(_wrap(const Layout5GridDashboard()));
    await tester.pump();
  });
}
