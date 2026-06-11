// TTS settings section for the settings panel.
// Follows the same pattern as _buildSttSection() in settings_panel.dart.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/tts_provider.dart';
import 'package:cyberpunk_theme/cyberpunk_theme.dart';

/// Provider for TTS behavior settings (interrupt/queue).
final ttsBehaviorProvider = StateProvider.family<bool, String>((ref, key) {
  // Default values
  if (key == 'interrupt') return true;
  if (key == 'queue') return false;
  return false;
});

/// Builds the TTS settings section widget.
Widget buildTtsSection(WidgetRef ref) {
  final ttsNotifier = ref.read(ttsProvider.notifier);
  final isAvailable = ttsNotifier.isAvailable;
  final isEnabled = ttsNotifier.enabled;
  final interruptEnabled = ref.watch(ttsBehaviorProvider('interrupt'));
  final queueEnabled = ref.watch(ttsBehaviorProvider('queue'));

  // Initialize TTS on first build
  WidgetsBinding.instance.addPostFrameCallback((_) {
    ttsNotifier.initialize();
  });

  return Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      // Section header
      Row(
        children: [
          Icon(
            Icons.volume_up,
            color: CyberpunkColors.greenSuccess,
            size: 20,
          ),
          const SizedBox(width: 8),
          const Text(
            'Text-to-Speech',
            style: TextStyle(
              fontFamily: 'ShareTechMono',
              fontSize: 16,
              color: CyberpunkColors.greenSuccess,
            ),
          ),
        ],
      ),
      const SizedBox(height: 16),

      // Enable/disable toggle
      Row(
        children: [
          const Text(
            'Enable TTS',
            style: TextStyle(
              fontFamily: 'ShareTechMono',
              fontSize: 14,
              color: CyberpunkColors.textPrimary,
            ),
          ),
          const Spacer(),
          Switch(
            value: isEnabled,
            onChanged: isAvailable
                ? (value) => ttsNotifier.setEnabled(value)
                : null,
            activeColor: CyberpunkColors.greenSuccess,
          ),
        ],
      ),
      const SizedBox(height: 16),

      // Voice selector
      FutureBuilder<List<Map<String, dynamic>>>(
        future: ttsNotifier.getVoices(),
        builder: (context, snapshot) {
          final voices = snapshot.data ?? [];
          return Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text(
                'Voice',
                style: TextStyle(
                  fontFamily: 'ShareTechMono',
                  fontSize: 12,
                  color: CyberpunkColors.textSecondary,
                ),
              ),
              const SizedBox(height: 8),
              DropdownButtonFormField<String>(
                value: voices.isNotEmpty ? voices.first['name'] as String? : null,
                items: voices.map((voice) {
                  return DropdownMenuItem(
                    value: voice['name'] as String?,
                    child: Text(
                      voice['name'] as String? ?? 'Unknown',
                      style: const TextStyle(
                        fontFamily: 'ShareTechMono',
                        fontSize: 12,
                        color: CyberpunkColors.textPrimary,
                      ),
                    ),
                  );
                }).toList(),
                onChanged: isAvailable && isEnabled
                    ? (value) => value != null && ttsNotifier.setVoice(value)
                    : null,
                decoration: InputDecoration(
                  border: OutlineInputBorder(
                    borderSide: const BorderSide(color: CyberpunkColors.greenSuccess),
                    borderRadius: BorderRadius.circular(4),
                  ),
                  filled: true,
                  fillColor: CyberpunkColors.darkGray,
                ),
              ),
            ],
          );
        },
      ),
      const SizedBox(height: 16),

      // Speed slider
      Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Speech Rate',
            style: TextStyle(
              fontFamily: 'ShareTechMono',
              fontSize: 12,
              color: CyberpunkColors.textSecondary,
            ),
          ),
          const SizedBox(height: 8),
          Slider(
            value: 0.5,
            min: 0.0,
            max: 1.0,
            divisions: 10,
            activeColor: CyberpunkColors.greenSuccess,
            onChanged: isAvailable && isEnabled
                ? (value) => ttsNotifier.setSpeed(value)
                : null,
          ),
        ],
      ),
      const SizedBox(height: 16),

      // Volume slider
      Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Volume',
            style: TextStyle(
              fontFamily: 'ShareTechMono',
              fontSize: 12,
              color: CyberpunkColors.textSecondary,
            ),
          ),
          const SizedBox(height: 8),
          Slider(
            value: 1.0,
            min: 0.0,
            max: 1.0,
            divisions: 10,
            activeColor: CyberpunkColors.greenSuccess,
            onChanged: isAvailable && isEnabled
                ? (value) => ttsNotifier.setVolume(value)
                : null,
          ),
        ],
      ),
      const SizedBox(height: 16),

      // Behavior section header
      const Divider(color: CyberpunkColors.midGray),
      const SizedBox(height: 8),
      const Text(
        'Behavior',
        style: TextStyle(
          fontFamily: 'ShareTechMono',
          fontSize: 14,
          color: CyberpunkColors.textPrimary,
        ),
      ),
      const SizedBox(height: 12),

      // Interrupt on new message toggle
      Row(
        children: [
          const Expanded(
            child: Text(
              'Interrupt on new msg',
              style: TextStyle(
                fontFamily: 'ShareTechMono',
                fontSize: 12,
                color: CyberpunkColors.textSecondary,
              ),
            ),
          ),
          Switch(
            value: interruptEnabled,
            onChanged: isAvailable && isEnabled
                ? (value) {
                    ref.read(ttsBehaviorProvider('interrupt').notifier).state = value;
                    if (value) {
                      ref.read(ttsBehaviorProvider('queue').notifier).state = false;
                    }
                  }
                : null,
            activeColor: CyberpunkColors.greenSuccess,
          ),
        ],
      ),
      const SizedBox(height: 8),

      // Queue messages toggle
      Row(
        children: [
          const Expanded(
            child: Text(
              'Queue messages',
              style: TextStyle(
                fontFamily: 'ShareTechMono',
                fontSize: 12,
                color: CyberpunkColors.textSecondary,
              ),
            ),
          ),
          Switch(
            value: queueEnabled,
            onChanged: isAvailable && isEnabled
                ? (value) {
                    ref.read(ttsBehaviorProvider('queue').notifier).state = value;
                    if (value) {
                      ref.read(ttsBehaviorProvider('interrupt').notifier).state = false;
                    }
                  }
                : null,
            activeColor: CyberpunkColors.greenSuccess,
          ),
        ],
      ),

      // Availability warning
      if (!isAvailable) ...[
        const SizedBox(height: 16),
        Container(
          padding: const EdgeInsets.all(8),
          decoration: BoxDecoration(
            border: Border.all(color: CyberpunkColors.orangeWarning),
            borderRadius: BorderRadius.circular(4),
          ),
          child: const Text(
            'TTS not available on this platform. Platform-native synthesis requires flutter_tts plugin.',
            style: TextStyle(
              fontFamily: 'ShareTechMono',
              fontSize: 11,
              color: CyberpunkColors.orangeWarning,
            ),
          ),
        ),
      ],
    ],
  );
}
