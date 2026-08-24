import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/theme/app_palette.dart';
import 'package:meept_ui/theme/colors.dart';
import 'package:meept_ui/theme/cyberpunk_theme.dart';

void main() {
  test('build(midnight) and build(cyberpunk) primaryColor differ', () {
    final midnight = CyberpunkTheme.build(AppPalette.forName('midnight'));
    final cyber = CyberpunkTheme.build(AppPalette.forName('cyberpunk'));

    expect(midnight.primaryColor, isNot(cyber.primaryColor));
    expect(cyber.primaryColor, const Color(0xFFFF6600));
    expect(midnight.primaryColor, const Color(0xFF7DC4FF));
  });

  test('scaffoldBackgroundColor follows background role', () {
    final midnight =
        CyberpunkTheme.build(AppPalette.forName('midnight')).scaffoldBackgroundColor;
    expect(midnight, const Color(0xFF1A1B26));

    final cyber =
        CyberpunkTheme.build(AppPalette.forName('cyberpunk')).scaffoldBackgroundColor;
    expect(cyber, const Color(0xFF000000));
  });

  test('darkTheme delegates to the active palette', () {
    // Default (cyberpunk) — no explicit setActive needed for a cold start.
    expect(CyberpunkTheme.darkTheme.scaffoldBackgroundColor,
        const Color(0xFF000000));

    // After activating midnight the legacy accessor follows along.
    final saved = AppPalette.forName('cyberpunk');
    CyberpunkColors.setActive(AppPalette.forName('midnight'));
    expect(CyberpunkTheme.darkTheme.scaffoldBackgroundColor,
        const Color(0xFF1A1B26));

    CyberpunkColors.setActive(saved);
    expect(CyberpunkTheme.darkTheme.scaffoldBackgroundColor,
        const Color(0xFF000000));
  });
}
