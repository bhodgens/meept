import 'package:flutter/material.dart';
import '../../styles/cyberpunk_theme.dart';

/// SkillPanel displays available skills and allows skill execution.
///
/// When a skill has a `ui_type` of 'panel', it renders as a full panel.
/// For skills without ui_type, shows description + execute button.
class SkillPanel extends StatefulWidget {
  const SkillPanel({super.key});

  @override
  State<SkillPanel> createState() => _SkillPanelState();
}

class _SkillPanelState extends State<SkillPanel> {
  String? _selectedSkill;
  bool _isLoading = false;

  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.darkGray,
      child: Column(
        children: [
          // Header
          Container(
            padding: const EdgeInsets.all(16),
            decoration: BoxDecoration(
              border: Border(
                bottom: BorderSide(
                  color: CyberpunkColors.orangePrimary.withOpacity(0.3),
                  width: 1,
                ),
              ),
            ),
            child: Row(
              children: [
                const Icon(
                  Icons.auto_awesome,
                  color: CyberpunkColors.orangeBright,
                  size: 24,
                ),
                const SizedBox(width: 12),
                Text(
                  'skills',
                  style: CyberpunkTypography.headingSmall.copyWith(
                    color: CyberpunkColors.orangePrimary,
                  ),
                ),
              ],
            ),
          ),

          // Content
          Expanded(
            child: _isLoading
                ? const Center(
                    child: CircularProgressIndicator(
                      color: CyberpunkColors.orangePrimary,
                    ),
                  )
                : _buildSkillList(),
          ),
        ],
      ),
    );
  }

  Widget _buildSkillList() {
    // TODO: Wire to API to fetch skills
    // For now, show a placeholder
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(
            Icons.auto_awesome_outlined,
            size: 64,
            color: CyberpunkColors.orangeDark,
          ),
          const SizedBox(height: 16),
          Text(
            'skill panel',
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.orangePrimary,
            ),
          ),
          const SizedBox(height: 8),
          Text(
            'select a skill to execute',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.orangeDark,
            ),
          ),
          const SizedBox(height: 24),
          ElevatedButton(
            onPressed: () {
              // TODO: Show skill execution dialog
            },
            style: ElevatedButton.styleFrom(
              backgroundColor: CyberpunkColors.orangePrimary,
              foregroundColor: CyberpunkColors.darkGray,
            ),
            child: const Text('browse skills'),
          ),
        ],
      ),
    );
  }
}
