import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/theme/app_palette.dart';
import 'package:meept_ui/theme/colors.dart';

void main() {
  group('AppPalette.forName', () {
    test('midnight returns midnight hexes', () {
      final p = AppPalette.forName('midnight');
      expect(p.name, 'midnight');
      expect(p.primary, const Color(0xFF7DC4FF));
      expect(p.primaryBright, const Color(0xFFA5D8FF));
      expect(p.primaryDark, const Color(0xFF4B79A8));
      expect(p.primaryGlow, const Color(0xFF9ECE6A));
      expect(p.accent, const Color(0xFFC0CAF5));
      expect(p.secondary, const Color(0xFF9ECE6A));
      expect(p.success, const Color(0xFF9ECE6A));
      expect(p.warning, const Color(0xFFE0AF68));
      expect(p.error, const Color(0xFFF7768E));
      expect(p.info, const Color(0xFF7AA2F7));
      expect(p.background, const Color(0xFF1A1B26));
      expect(p.surface, const Color(0xFF24283B));
      expect(p.surfaceAlt, const Color(0xFF2F334D));
      expect(p.border, const Color(0xFF414868));
      expect(p.textPrimary, const Color(0xFFC0CAF5));
      expect(p.textMuted, const Color(0xFF565F89));
      expect(p.terminalGreen, const Color(0xFF73DACA));
      expect(p.terminalAmber, const Color(0xFFE0AF68));
    });

    test('unknown name falls back to cyberpunk', () {
      final fallback = AppPalette.forName('no-such-variant');
      final cyber = AppPalette.forName('cyberpunk');
      expect(fallback.name, 'cyberpunk');
      expect(fallback.primary, cyber.primary);
      expect(fallback.background, cyber.background);
      // Spot-check a frozen hex.
      expect(fallback.primary, const Color(0xFFFF6600));
    });

    test('palettes covers every variant in tokens data', () {
      expect(AppPalette.palettes.keys.toSet(),
          {'cyberpunk', 'midnight', 'solarized'});
    });
  });

  group('CyberpunkColors.setActive', () {
    test('setActive swaps orangePrimary value', () {
      CyberpunkColors.setActive(AppPalette.forName('cyberpunk'));
      expect(CyberpunkColors.orangePrimary, const Color(0xFFFF6600));

      CyberpunkColors.setActive(AppPalette.forName('midnight'));
      expect(CyberpunkColors.orangePrimary, const Color(0xFF7DC4FF));

      // Restore the default so other tests are unaffected.
      CyberpunkColors.setActive(AppPalette.forName('cyberpunk'));
      expect(CyberpunkColors.orangePrimary, const Color(0xFFFF6600));
    });

    test('legacy members forward to mapped roles after swap', () {
      final midnight = AppPalette.forName('midnight');
      CyberpunkColors.setActive(midnight);
      expect(CyberpunkColors.black, midnight.background);
      expect(CyberpunkColors.darkGray, midnight.surface);
      expect(CyberpunkColors.midGray, midnight.surfaceAlt);
      expect(CyberpunkColors.lightGray, midnight.border);
      expect(CyberpunkColors.blueInfo, midnight.info);

      CyberpunkColors.setActive(AppPalette.forName('cyberpunk'));
    });
  });
}
