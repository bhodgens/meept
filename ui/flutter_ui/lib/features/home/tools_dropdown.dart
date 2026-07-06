import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';

/// Hamburger menu button for the top-left toolbar.
/// Shows known tools only (no skills section at bottom).
/// Uses custom menu overlay for instant animation (no fade/slide).
class HamburgerMenu extends StatefulWidget {
  final ValueChanged<String>? onToolSelected;

  const HamburgerMenu({super.key, this.onToolSelected});

  @override
  State<HamburgerMenu> createState() => _HamburgerMenuState();
}

class _HamburgerMenuState extends State<HamburgerMenu> {
  OverlayEntry? _overlayEntry;
  bool _isOpen = false;

  // Hardcoded tool panels that have implementations
  static const _knownTools = {
    'memory': Icons.memory,
    'files': Icons.folder,
    'terminal': Icons.terminal,
    'calendar': Icons.calendar_today,
    'metrics': Icons.insights,
    'prompts': Icons.edit_note,
    'settings': Icons.settings,
  };

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: _toggleMenu,
      child: Container(
        padding: const EdgeInsets.all(8),
        decoration: BoxDecoration(
          color: CyberpunkColors.blackTransparent(0.3),
          border: Border.all(color: CyberpunkColors.midGray, width: 1),
          borderRadius: BorderRadius.circular(4),
        ),
        child: const Icon(
          Icons.menu,
          size: 20,
          color: CyberpunkColors.orangePrimary,
        ),
      ),
    );
  }

  @override
  void dispose() {
    _overlayEntry?.remove();
    super.dispose();
  }

  void _toggleMenu() {
    if (_isOpen) {
      _closeMenu();
    } else {
      _openMenu();
    }
  }

  void _openMenu() {
    setState(() => _isOpen = true);
    final overlay = Overlay.of(context);
    final renderBox = context.findRenderObject() as RenderBox;
    final size = renderBox.size;
    final offset = renderBox.localToGlobal(Offset.zero);

    // Build menu items
    final entries = <Widget>[
      const Padding(
        padding: EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        child: Text(
          'tools',
          style: TextStyle(
            color: CyberpunkColors.orangePrimary,
            fontWeight: FontWeight.bold,
            fontSize: 12,
          ),
        ),
      ),
      const Divider(height: 1, color: CyberpunkColors.midGray),
    ];

    for (final entry in _knownTools.entries) {
      entries.add(
        InkWell(
          onTap: () {
            _closeMenu();
            widget.onToolSelected?.call(entry.key);
          },
          child: Container(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
            child: Row(
              children: [
                Icon(entry.value, size: 16, color: CyberpunkColors.orangeBright),
                const SizedBox(width: 8),
                Text(
                  entry.key,
                  style: CyberpunkTypography.bodySmall,
                ),
              ],
            ),
          ),
        ),
      );
    }

    // Calculate menu position (below the button, aligned left)
    final menuTop = offset.dy + size.height;
    final menuLeft = offset.dx;
    const menuWidth = 180.0;

    _overlayEntry = OverlayEntry(
      builder: (context) => Stack(
        children: [
          // Transparent overlay to close menu on tap outside
          Positioned.fill(
            child: GestureDetector(
              behavior: HitTestBehavior.translucent,
              onTap: _closeMenu,
              child: Container(color: Colors.transparent),
            ),
          ),
          // Menu
          Positioned(
            top: menuTop,
            left: menuLeft,
            width: menuWidth,
            child: Material(
              elevation: 8,
              color: CyberpunkColors.darkGray,
              borderRadius: BorderRadius.circular(4),
              child: ClipRRect(
                borderRadius: BorderRadius.circular(4),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  mainAxisSize: MainAxisSize.min,
                  children: entries,
                ),
              ),
            ),
          ),
        ],
      ),
    );

    overlay.insert(_overlayEntry!);
  }

  void _closeMenu() {
    setState(() => _isOpen = false);
    _overlayEntry?.remove();
    _overlayEntry = null;
  }
}
