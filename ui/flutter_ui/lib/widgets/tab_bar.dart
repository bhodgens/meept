import 'package:flutter/material.dart';
import '../theme/colors.dart';
import '../theme/typography.dart';

/// ORANGE VOID custom top tab bar - all lowercase labels with orange accent
class OrangeVoidTabBar extends StatelessWidget {
  final List<String> tabs;
  final int selectedIndex;
  final ValueChanged<int> onTabSelected;

  const OrangeVoidTabBar({
    super.key,
    required this.tabs,
    required this.selectedIndex,
    required this.onTabSelected,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: const BoxDecoration(
        color: CyberpunkColors.black,
        border: Border(
          bottom: BorderSide(
            color: CyberpunkColors.orangeDark,
            width: 2,
          ),
        ),
      ),
      child: Row(
        children: List.generate(tabs.length, (index) {
          final isSelected = selectedIndex == index;
          return Expanded(
            child: InkWell(
              onTap: () => onTabSelected(index),
              child: Container(
                padding: const EdgeInsets.symmetric(vertical: 16),
                decoration: BoxDecoration(
                  border: Border(
                    bottom: BorderSide(
                      color: isSelected
                          ? CyberpunkColors.orangePrimary
                          : Colors.transparent,
                      width: 3,
                    ),
                  ),
                ),
                child: Center(
                  child: Text(
                    tabs[index].toLowerCase(),
                    style: CyberpunkTypography.label.copyWith(
                      color: isSelected
                          ? CyberpunkColors.orangePrimary
                          : Colors.grey,
                      letterSpacing: 2,
                    ),
                  ),
                ),
              ),
            ),
          );
        }),
      ),
    );
  }
}
