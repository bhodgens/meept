/// Layout Preview Entry Point
///
/// Run this to preview all 5 alternative layouts.
///
/// Usage:
///   flutter run -d macos --target=lib/main_layout_preview.dart
///   flutter run -d linux --target=lib/main_layout_preview.dart
///   flutter run -d chrome --target=lib/main_layout_preview.dart
///
/// Navigate between layouts using the dropdown in the top-right.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'theme/cyberpunk_theme.dart';
import 'theme/colors.dart';
import 'layouts/layout_1_classic_sidebar.dart';
import 'layouts/layout_2_bottom_nav.dart';
import 'layouts/layout_3_radial_hub.dart';
import 'layouts/layout_4_top_tabs_panels.dart';
import 'layouts/layout_5_grid_dashboard.dart';

void main() {
  runApp(
    const ProviderScope(
      child: LayoutPreviewApp(),
    ),
  );
}

class LayoutPreviewApp extends StatefulWidget {
  const LayoutPreviewApp({super.key});

  @override
  State<LayoutPreviewApp> createState() => _LayoutPreviewAppState();
}

class _LayoutPreviewAppState extends State<LayoutPreviewApp> {
  int _selectedLayout = 0;

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Meept Layout Preview',
      debugShowCheckedModeBanner: false,
      theme: CyberpunkTheme.darkTheme,
      home: Container(
        color: CyberpunkColors.black,
        child: Stack(
          children: [
            // Layout content
            Positioned.fill(
              top: 80, // Leave space for the overlay header
              child: _buildLayout(_selectedLayout),
            ),
            // Overlay header
            Positioned(
              top: 0,
              left: 0,
              right: 0,
              height: 80,
              child: Material(
                elevation: 4,
                color: CyberpunkColors.darkGray,
                child: SafeArea(
                  child: Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 16),
                    child: Row(
                      children: [
                        const Text(
                          'layout preview',
                          style: TextStyle(
                            color: Colors.white,
                            fontSize: 14,
                            fontFamily: 'SourceCodePro',
                          ),
                        ),
                        const Spacer(),
                        SizedBox(
                          width: 220,
                          child: DropdownButton<int>(
                            value: _selectedLayout,
                            dropdownColor: CyberpunkColors.darkGray,
                            underline: const SizedBox(),
                            style: const TextStyle(
                              color: Colors.white,
                              fontSize: 14,
                              fontFamily: 'SourceCodePro',
                            ),
                            items: const [
                              DropdownMenuItem(value: 0, child: Text('1. sidebar')),
                              DropdownMenuItem(value: 1, child: Text('2. bottom nav')),
                              DropdownMenuItem(value: 2, child: Text('3. radial hub')),
                              DropdownMenuItem(value: 3, child: Text('4. top tabs + panels')),
                              DropdownMenuItem(value: 4, child: Text('5. grid dashboard')),
                            ],
                            onChanged: (value) {
                              if (value != null) {
                                setState(() => _selectedLayout = value);
                              }
                            },
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildLayout(int index) {
    switch (index) {
      case 0: return const Layout1ClassicSidebar();
      case 1: return const Layout2BottomNav();
      case 2: return const Layout3RadialHub();
      case 3: return Layout4TopTabsPanels();
      case 4: return const Layout5GridDashboard();
      default: return const Layout1ClassicSidebar();
    }
  }
}
