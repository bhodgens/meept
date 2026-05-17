import 'package:flutter/material.dart';
import '../../theme/colors.dart';

class CyberpunkLoader extends StatelessWidget {
  final String? message;

  const CyberpunkLoader({super.key, this.message});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          SizedBox(
            width: 48,
            height: 48,
            child: CircularProgressIndicator(
              valueColor: AlwaysStoppedAnimation<Color>(
                CyberpunkColors.orangePrimary,
              ),
              strokeWidth: 3,
            ),
          ),
          if (message != null) ...[
            const SizedBox(height: 16),
            Text(
              message!,
              style: TextStyle(
                fontFamily: 'JetBrainsMono',
                color: CyberpunkColors.orangeGlow,
                fontSize: 12,
              ),
            ),
          ],
        ],
      ),
    );
  }
}
