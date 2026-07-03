/// Platform abstraction layer for cross-platform Flutter support.
///
/// This library provides [platformService], a singleton that exposes
/// platform-specific capabilities with safe fallbacks for web.
///
/// ## Usage
///
/// Call [initPlatformService] in [main] before using any platform features:
///
/// ```dart
/// void main() async {
///   WidgetsFlutterBinding.ensureInitialized();
///   await initPlatformService();
///   runApp(MyApp());
/// }
/// ```
///
/// Then access platform features via [platformService]:
///
/// ```dart
/// final homeDir = await platformService.getHomeDirectory();
/// final devKey = await platformService.readDevKeyFile(path);
/// final isWeb = platformService.isWeb;
/// final leaderKey = platformService.defaultLeaderKey;
/// ```
///
/// ## Platform Support
///
/// - **Native (macOS, Linux, Windows)**: Full filesystem access via dart:io
/// - **Web**: Filesystem methods return null; platform flags return false
library platform;

import 'dart:async' show Future;
import 'package:flutter/foundation.dart' show kIsWeb;
import 'platform_native_helpers.dart' as native;

/// Global singleton platform service. Initialized by [initPlatformService].
PlatformService? _platformService;

/// Initialize the platform service singleton. Must be called once in [main]
/// before any platform features are used.
Future<PlatformService> initPlatformService() async {
  if (_platformService != null) {
    return _platformService!;
  }

  final service = PlatformService._();
  await service.init();
  _platformService = service;
  return service;
}

/// Get the initialized platform service singleton.
///
/// Returns null if called before [initPlatformService].
PlatformService? get platformService => _platformService;

/// Platform service that provides platform detection and filesystem access
/// with safe web fallbacks.
///
/// All methods are safe to call on web - they return null/defaults
/// instead of throwing when the underlying platform doesn't support
/// the operation.
class PlatformService {
  /// Internal constructor for singleton.
  PlatformService._();

  /// Initialize the service. Called once by [initPlatformService].
  Future<void> init() async {
    // Cache platform detection results
    _cachedIsWeb = kIsWeb;
    _cachedIsMacOS = !kIsWeb && _detectMacOS();
    _cachedIsLinux = !kIsWeb && _detectLinux();
    _cachedIsWindows = !kIsWeb && _detectWindows();
  }

  bool? _cachedIsWeb;
  bool? _cachedIsMacOS;
  bool? _cachedIsLinux;
  bool? _cachedIsWindows;

  // ---------------------------------------------------------------------------
  // Platform Detection
  // ---------------------------------------------------------------------------

  /// Whether running on Flutter Web.
  bool get isWeb => _cachedIsWeb ?? kIsWeb;

  /// Whether running on macOS.
  bool get isMacOS => _cachedIsMacOS ?? false;

  /// Whether running on Linux.
  bool get isLinux => _cachedIsLinux ?? false;

  /// Whether running on Windows.
  bool get isWindows => _cachedIsWindows ?? false;

  /// Detect macOS via dart:io Platform (only works on native).
  static bool _detectMacOS() => native.nativeIsMacOS();
  static bool _detectLinux() => native.nativeIsLinux();
  static bool _detectWindows() => native.nativeIsWindows();

  // ---------------------------------------------------------------------------
  // File Operations
  // ---------------------------------------------------------------------------

  /// Returns the user's home directory path, or null when unavailable.
  ///
  /// On web, always returns null since browsers don't expose filesystem.
  Future<String?> getHomeDirectory() async {
    if (isWeb) {
      return null;
    }
    return native.getHomeDirectoryNative();
  }

  /// Read the contents of a file at [path], or null on error/unavailable.
  ///
  /// On web, always returns null since browsers don't expose filesystem.
  Future<String?> readFile(String path) async {
    if (isWeb) {
      return null;
    }
    return native.readFileNative(path);
  }

  /// Convenience wrapper for reading the daemon's dev key file.
  Future<String?> readDevKeyFile(String path) => readFile(path);

  // ---------------------------------------------------------------------------
  // Defaults
  // ---------------------------------------------------------------------------

  /// Default leader key for keyboard shortcuts.
  ///
  /// macOS: `cmd+x`
  /// Linux/Windows/Web: `ctrl+x`
  String get defaultLeaderKey {
    if (isMacOS) {
      return 'cmd+x';
    }
    return 'ctrl+x';
  }
}
