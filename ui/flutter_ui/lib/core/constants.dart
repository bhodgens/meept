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
  // NOTE: use_tls was removed — HTTPS is mandatory and not configurable

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
