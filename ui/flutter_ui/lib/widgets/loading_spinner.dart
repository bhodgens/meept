import 'package:flutter/material.dart';
import '../../theme/colors.dart';

/// Loading spinner widget - cyberpunk themed loading indicator
class LoadingSpinner extends StatelessWidget {
  final double size;
  final double strokeWidth;
  final String? message;

  const LoadingSpinner({
    super.key,
    this.size = 32.0,
    this.strokeWidth = 2.0,
    this.message,
  });

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          SizedBox(
            width: size,
            height: size,
            child: CircularProgressIndicator(
              strokeWidth: strokeWidth,
              valueColor: const AlwaysStoppedAnimation<Color>(
                CyberpunkColors.orangePrimary,
              ),
            ),
          ),
          if (message != null) ...[
            const SizedBox(height: 12),
            Text(
              message!,
              style: const TextStyle(
                fontFamily: 'SourceCodePro',
                color: CyberpunkColors.lightGray,
                fontSize: 12,
              ),
            ),
          ],
        ],
      ),
    );
  }
}

/// Mini loading spinner for inline use (e.g., buttons)
class MiniLoadingSpinner extends StatelessWidget {
  final double size;

  const MiniLoadingSpinner({
    super.key,
    this.size = 16.0,
  });

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: size,
      height: size,
      child: const CircularProgressIndicator(
        strokeWidth: 2,
        valueColor: AlwaysStoppedAnimation(CyberpunkColors.orangePrimary),
      ),
    );
  }
}
