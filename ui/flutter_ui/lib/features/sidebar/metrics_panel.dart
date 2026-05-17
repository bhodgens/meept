import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';

class MetricsPanel extends StatelessWidget {
  const MetricsPanel({super.key});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray.withOpacity(0.5),
        border: Border(
          top: BorderSide(color: CyberpunkColors.orangePrimary.withOpacity(0.3), width: 1),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'SYSTEM METRICS',
            style: CyberpunkTypography.label,
          ),
          const SizedBox(height: 12),
          _buildMetricRow('ACTIVE AGENTS', '8', CyberpunkColors.blueInfo),
          const SizedBox(height: 8),
          _buildMetricRow('QUEUE DEPTH', '3', CyberpunkColors.orangePrimary),
          const SizedBox(height: 8),
          _buildMetricRow('TOKENS/SEC', '245', CyberpunkColors.greenSuccess),
          const SizedBox(height: 8),
          _buildMetricRow('REQ/MIN', '42', Colors.purple),
        ],
      ),
    );
  }

  Widget _buildMetricRow(String label, String value, Color color) {
    return Row(
      mainAxisAlignment: MainAxisAlignment.spaceBetween,
      children: [
        Text(
          label,
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.lightGray,
            fontSize: 10,
          ),
        ),
        Text(
          value,
          style: CyberpunkTypography.bodyMedium.copyWith(
            color: color,
            fontWeight: FontWeight.bold,
            fontSize: 16,
          ),
        ),
      ],
    );
  }
}
