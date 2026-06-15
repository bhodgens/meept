import 'package:flutter/material.dart';

import '../../../models/api_models.dart';
import '../../../theme/colors.dart';
import '../../../theme/typography.dart';

class AgentProgressIndicator extends StatelessWidget {
  final AgentProgress progress;

  const AgentProgressIndicator({super.key, required this.progress});

  @override
  Widget build(BuildContext context) {
    Color messageColor;
    FontStyle fontStyle;

    switch (progress.tier) {
      case 0:
        messageColor = CyberpunkColors.lightGray;
        fontStyle = FontStyle.normal;
        break;
      case 1:
        messageColor = CyberpunkColors.midGray;
        fontStyle = FontStyle.normal;
        break;
      case 2:
      default:
        messageColor = CyberpunkColors.lightGray;
        fontStyle = FontStyle.italic;
    }

    final displayMessage =
        progress.message.length > 60
            ? '${progress.message.substring(0, 57)}...'
            : progress.message;

    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4, horizontal: 16),
      child: Row(
        children: [
          const SizedBox(
            width: 12,
            height: 12,
            child: CircularProgressIndicator(
              strokeWidth: 2,
              valueColor: AlwaysStoppedAnimation<Color>(
                CyberpunkColors.orangePrimary,
              ),
            ),
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  progress.agentId.toLowerCase(),
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.orangePrimary,
                    fontWeight: FontWeight.bold,
                    fontSize: 10,
                  ),
                ),
                const SizedBox(height: 2),
                Text(
                  displayMessage,
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: messageColor,
                    fontStyle: fontStyle,
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
