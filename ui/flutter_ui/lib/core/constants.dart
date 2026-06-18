import 'package:flutter/material.dart';

/// Application-wide constants
abstract class AppConstants {
  // Version (keep in sync with pubspec.yaml)
  static const String appVersion = '1.0.0';

  // API Configuration
  static const String defaultApiHost = 'localhost';
  static const int defaultApiPort = 8081;
  static const String apiVersion = 'v1';

  // Connection
  static const Duration connectionTimeout = Duration(seconds: 30);
  static const Duration receiveTimeout = Duration(minutes: 5);
  static const Duration pingInterval = Duration(seconds: 30);
  static const int maxRetries = 3;

  // UI
  static const double defaultFontSize = 14.0;
  static const double sidebarWidth = 280.0;
  static const double metricsPanelHeight = 150.0;
  static const Duration animationDuration = Duration(milliseconds: 300);

  // Storage keys (SharedPreferences)
  static const String apiKeyPref = 'api_key';
  static const String themePref = 'theme';
  // TTS preferences
  static const String ttsEnabledPref = 'tts_enabled';
  static const String ttsVoicePref = 'tts_voice';
  static const String ttsVolumePref = 'tts_volume';
  static const String ttsRatePref = 'tts_rate';
  static const String ttsInterruptPref = 'tts_interrupt';
  static const String ttsQueuePref = 'tts_queue';
  static const String ttsMaxQueueSizePref = 'tts_max_queue_size';
  // NOTE: use_tls was removed — HTTPS is mandatory and not configurable

  // Window geometry (desktop only)
  static const String windowWidthPref = 'window_width';
  static const String windowHeightPref = 'window_height';
  static const String windowXPref = 'window_x';
  static const String windowYPref = 'window_y';
  static const String windowMaximizedPref = 'window_maximized';

  /// Development API key (injected at build time via --dart-define).
  /// In debug builds: pass `--dart-define=MEEPT_DEV_API_KEY=meept_dev_default_key_CHANGE_ME`.
  /// In release builds: empty string — no fallback prevents silent auth with known keys.
  static const String defaultApiKey = String.fromEnvironment(
    'MEEPT_DEV_API_KEY',
    defaultValue: '',
  );

  // Agent IDs (must match backend)
  static const String agentDispatcher = 'dispatcher';
  static const String agentChat = 'chat';
  static const String agentCoder = 'coder';
  static const String agentDebugger = 'debugger';
  static const String agentPlanner = 'planner';
  static const String agentAnalyst = 'analyst';
  static const String agentCommitter = 'committer';
  static const String agentScheduler = 'scheduler';
}

/// Returns the appropriate Material icon for a given agent ID.
IconData getAgentIcon(String agentId) {
  switch (agentId.toLowerCase()) {
    case 'coder':
      return Icons.code;
    case 'debugger':
      return Icons.bug_report;
    case 'planner':
      return Icons.account_tree;
    case 'analyst':
      return Icons.analytics;
    case 'chat':
      return Icons.chat;
    case 'committer':
      return Icons.source;
    case 'scheduler':
      return Icons.schedule;
    case 'dispatcher':
      return Icons.route;
    default:
      return Icons.smart_toy;
  }
}
