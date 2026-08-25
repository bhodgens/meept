import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/settings/settings_panel.dart';
import 'package:meept_ui/services/storage_service.dart';
import 'package:meept_ui/theme/app_palette.dart';
import 'package:meept_ui/theme/colors.dart';
import 'package:meept_ui/theme/palette_provider.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  // StorageService reads through SharedPreferences; the panel needs an
  // initialized instance or every field defaults to null (and daemonPort's
  // int cast throws).
  SharedPreferences.setMockInitialValues(<String, Object>{
    'api_port': 8081,
    'api_host': 'localhost',
  });
  setUpAll(() async {
    await StorageService.instance.init();
  });
  // Reset the static forwarding layer between tests so palette swaps in one
  // test never leak into another.
  setUp(() {
    CyberpunkColors.setActive(AppPalette.forName('cyberpunk'));
  });

  Future<void> pumpPanel(WidgetTester tester) async {
    // The settings panel has unbounded-width InputDecorators inside Rows;
    // give the surface a fixed test viewport width like the real window.
    tester.view.physicalSize = const Size(1200, 1600);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      const ProviderScope(
        child: MaterialApp(
          home: Scaffold(
            body: SizedBox(width: 1100, child: SettingsPanel()),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('theme dropdown lists all three shipped variants', (
    WidgetTester tester,
  ) async {
    await pumpPanel(tester);

    // The connection section's theme dropdown is identified by its current
    // value label.
    expect(find.text('cyberpunk'), findsOneWidget);

    await tester.tap(find.text('cyberpunk'));
    await tester.pumpAndSettle();

    for (final variant in ['cyberpunk', 'midnight', 'solarized']) {
      expect(
        find.descendant(
          of: find.byType(DropdownMenuItem<String>),
          matching: find.text(variant),
        ),
        findsWidgets,
        reason: 'variant $variant should be offered',
      );
    }
  });

  testWidgets('selecting midnight applies the palette immediately', (
    WidgetTester tester,
  ) async {
    late ProviderContainer container;
    tester.view.physicalSize = const Size(1200, 1600);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      ProviderScope(
        child: Builder(
          builder: (context) {
            container = ProviderScope.containerOf(context);
            return MaterialApp(
              home: Scaffold(
                body: SizedBox(width: 1100, child: SettingsPanel()),
              ),
            );
          },
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(container.read(themeNameProvider), 'cyberpunk');
    final before = CyberpunkColors.orangePrimary;

    await tester.tap(find.text('cyberpunk'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('midnight').last);
    await tester.pumpAndSettle();

    expect(container.read(themeNameProvider), 'midnight');
    expect(CyberpunkColors.orangePrimary, isNot(before));
    expect(
      CyberpunkColors.orangePrimary,
      AppPalette.forName('midnight').primary,
    );
  });
}
