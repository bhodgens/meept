/// Native-only platform helpers.
///
/// This file is ONLY imported by [platform_service.dart] and only
/// executed on native platforms (not web). It contains dart:io
/// imports that would crash on web if imported directly.
///
/// The web build compiler excludes this file entirely since it
/// checks `kIsWeb` before calling these functions.
library platform_native_helpers;

import 'dart:io' show Platform, File, FileSystemException;
import 'package:flutter/foundation.dart' show debugPrint;

/// Detect if running on macOS.
///
/// Safe to call - returns false on any error.
bool nativeIsMacOS() {
  try {
    return Platform.isMacOS;
  } catch (_) {
    return false;
  }
}

/// Detect if running on Linux.
bool nativeIsLinux() {
  try {
    return Platform.isLinux;
  } catch (_) {
    return false;
  }
}

/// Detect if running on Windows.
bool nativeIsWindows() {
  try {
    return Platform.isWindows;
  } catch (_) {
    return false;
  }
}

/// Get the home directory path on native platforms.
///
/// Returns null on web or if HOME environment variable is not set.
Future<String?> getHomeDirectoryNative() async {
  try {
    return Platform.environment['HOME'] ??
           Platform.environment['USERPROFILE'];
  } catch (_) {
    return null;
  }
}

/// Read a file's contents on native platforms.
///
/// Returns null if the file doesn't exist, isn't readable, or on web.
Future<String?> readFileNative(String path) async {
  try {
    final file = File(path);
    if (await file.exists()) {
      return await file.readAsString();
    }
    return null;
  } on FileSystemException catch (e) {
    debugPrint('[PlatformService] FileSystemException reading $path: $e');
    return null;
  } catch (e) {
    debugPrint('[PlatformService] readFileNative failed for $path: $e');
    return null;
  }
}
