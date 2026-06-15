import 'package:flutter/material.dart';

import '../../../models/api_models.dart';
import '../../../theme/colors.dart';
import '../../../theme/typography.dart';

class AgentProgressIndicator extends StatelessWidget {
  final AgentProgress progress;

  const AgentProgressIndicator({super.key, required this.progress});

  bool get isError {
    final msg = progress.message.toLowerCase();
    return msg.contains('failed') || msg.contains('error') || msg.contains('abort');
  }

  Color _getColorForTier(int tier, bool isError) {
    if (isError) return Colors.red[300]!;
    switch (tier) {
      case 0:
        return CyberpunkColors.lightGray;
      case 1:
        return CyberpunkColors.midGray;
      case 2:
      default:
        return CyberpunkColors.lightGray;
    }
  }

  FontStyle _getFontStyleForTier(int tier, bool isError) {
    if (isError) return FontStyle.normal;
    switch (tier) {
      case 0:
        return FontStyle.normal;
      case 1:
        return FontStyle.normal;
      case 2:
      default:
        return FontStyle.italic;
    }
  }

  Widget _buildIndicator(bool isError) {
    if (isError) {
      return Icon(
        Icons.error_outline,
        color: Colors.red[300],
        size: 16,
      );
    }
    return SizedBox(
      width: 12,
      height: 12,
      child: CircularProgressIndicator(
        strokeWidth: 2,
        valueColor: AlwaysStoppedAnimation<Color>(
          CyberpunkColors.orangePrimary,
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final error = isError;
    final messageColor = _getColorForTier(progress.tier, error);
    final fontStyle = _getFontStyleForTier(progress.tier, error);
    final agentColor = error ? Colors.red[300]! : CyberpunkColors.orangePrimary;

    final displayMessage =
        progress.message.length > 60
            ? '${progress.message.substring(0, 57)}...'
            : progress.message;

    return AnimatedSwitcher(
      duration: const Duration(milliseconds: 150),
      switchInCurve: Curves.easeIn,
      switchOutCurve: Curves.easeOut,
      child: Padding(
        key: ValueKey('${progress.agentId}-${progress.tier}-$error'),
        padding: const EdgeInsets.symmetric(vertical: 4, horizontal: 16),
        child: Row(
          children: [
            _buildIndicator(error),
            const SizedBox(width: 8),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    progress.agentId.toLowerCase(),
                    style: CyberpunkTypography.bodySmall.copyWith(
                      color: agentColor,
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
      ),
    );
  }
}
