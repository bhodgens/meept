/// Platform abstraction layer for cross-platform Flutter support.
///
/// This library provides [platformService], a singleton that exposes
/// platform-specific capabilities with safe fallbacks for web.
library platform;

import 'dart:async' show Future;
import 'package:flutter/foundation.dart' show kIsWeb;
// Conditional import - web stub returns null/false, native uses dart:io
import 'platform_helpers.dart' as native;

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
PlatformService? get platformService => _platformService;

/// Platform service that provides platform detection and filesystem access
/// with safe web fallbacks.
class PlatformService {
  PlatformService._();

  Future<void> init() async {
    _cachedIsWeb = kIsWeb;
    // On web, skip native detection entirely
    if (!kIsWeb) {
      _cachedIsMacOS = native.nativeIsMacOS();
      _cachedIsLinux = native.nativeIsLinux();
      _cachedIsWindows = native.nativeIsWindows();
    } else {
      _cachedIsMacOS = false;
      _cachedIsLinux = false;
      _cachedIsWindows = false;
    }
  }

  bool? _cachedIsWeb;
  bool? _cachedIsMacOS;
  bool? _cachedIsLinux;
  bool? _cachedIsWindows;

  bool get isWeb => _cachedIsWeb ?? kIsWeb;
  bool get isMacOS => _cachedIsMacOS ?? false;
  bool get isLinux => _cachedIsLinux ?? false;
  bool get isWindows => _cachedIsWindows ?? false;

  Future<String?> getHomeDirectory() async {
    if (isWeb) return null;
    return native.getHomeDirectoryNative();
  }

  Future<String?> readFile(String path) async {
    if (isWeb) return null;
    return native.readFileNative(path);
  }

  Future<String?> readDevKeyFile(String path) => readFile(path);

  String get defaultLeaderKey {
    if (isMacOS) return 'cmd+x';
    return 'ctrl+x';
  }
}
