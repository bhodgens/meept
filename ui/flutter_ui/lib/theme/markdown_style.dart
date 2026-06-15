import 'package:flutter/material.dart';
import 'package:flutter_markdown/flutter_markdown.dart';
import 'colors.dart';
import 'typography.dart';

/// Custom MarkdownStyleSheet matching the TUI's glamour cyberpunk theme.
MarkdownStyleSheet buildCyberpunkMarkdownStyle(BuildContext context) {
  return MarkdownStyleSheet(
    h1: CyberpunkTypography.headlineLarge.copyWith(
      color: const Color(0xFFF97316),
      fontSize: 22,
    ),
    h2: CyberpunkTypography.headlineMedium.copyWith(
      color: const Color(0xFFF59E0B),
      fontSize: 18,
    ),
    h3: CyberpunkTypography.headlineSmall.copyWith(
      color: const Color(0xFF10B981),
      fontSize: 16,
    ),
    h4: CyberpunkTypography.bodyLarge.copyWith(
      color: const Color(0xFF3B82F6),
      fontSize: 14,
      fontWeight: FontWeight.bold,
    ),
    h5: CyberpunkTypography.bodyMedium.copyWith(
      color: const Color(0xFF8B5CF6),
      fontSize: 13,
      fontWeight: FontWeight.bold,
    ),
    h6: CyberpunkTypography.bodyMedium.copyWith(
      color: const Color(0xFFEC4899),
      fontSize: 12,
      fontWeight: FontWeight.bold,
    ),
    p: CyberpunkTypography.bodyMedium.copyWith(
      color: CyberpunkColors.orangeGlow,
      fontSize: 14,
      height: 1.5,
    ),
    pPadding: const EdgeInsets.only(bottom: 8),
    strong: const TextStyle(
      color: Color(0xFFFFFFFF),
      fontWeight: FontWeight.bold,
    ),
    em: const TextStyle(
      color: Color(0xFFE5E7EB),
      fontStyle: FontStyle.italic,
    ),
    a: const TextStyle(
      color: Color(0xFF3B82F6),
      decoration: TextDecoration.underline,
    ),
    code: CyberpunkTypography.bodyMedium.copyWith(
      color: const Color(0xFF10B981),
      backgroundColor: const Color(0xFF1F2937),
      fontFamily: 'SourceCodePro',
      fontSize: 12,
      height: 1.4,
    ),
    codeblockDecoration: BoxDecoration(
      color: const Color(0xFF111827),
      borderRadius: const BorderRadius.all(Radius.circular(4)),
      border: Border.all(
        color: CyberpunkColors.midGray,
        width: 1,
      ),
    ),
    codeblockPadding: const EdgeInsets.all(12),
    blockquote: CyberpunkTypography.bodyMedium.copyWith(
      color: CyberpunkColors.orangeGlow,
    ),
    blockquoteDecoration: const BoxDecoration(
      border: Border(
        left: BorderSide(
          color: Color(0xFF6B7280),
          width: 3,
        ),
      ),
    ),
    blockquotePadding: const EdgeInsets.only(left: 12),
    listBullet: CyberpunkTypography.bodyMedium.copyWith(
      color: CyberpunkColors.orangeGlow,
    ),
    tableHead: CyberpunkTypography.bodyMedium.copyWith(
      color: const Color(0xFFFFFFFF),
      fontWeight: FontWeight.bold,
    ),
    tableBody: CyberpunkTypography.bodyMedium.copyWith(
      color: CyberpunkColors.orangeGlow,
    ),
    tableBorder: TableBorder.all(
      color: CyberpunkColors.midGray,
      width: 1,
    ),
    tableCellsDecoration: const BoxDecoration(
      color: Color(0xFF1A1A1A),
    ),
    tableColumnWidth: const FlexColumnWidth(),
    horizontalRuleDecoration: const BoxDecoration(
      border: Border(
        top: BorderSide(
          color: CyberpunkColors.midGray,
          width: 1,
        ),
      ),
    ),
  );
}
