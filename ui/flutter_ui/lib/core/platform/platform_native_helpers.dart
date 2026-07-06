// Native-only platform helpers.
//
// This file is ONLY imported by platform_service.dart and only
// executed on native platforms (not web). It contains dart:io
// imports that would crash on web if imported directly.
//
// The web build compiler should exclude this file entirely since it
// checks kIsWeb before calling these functions.
//
// IMPORTANT: If you see Platform.* errors on web, it means this file
// is being imported despite the kIsWeb guard. The fix is to use
// conditional imports (export_web.dart / export_native.dart).
library platform_native_helpers;

import 'dart:io' show Platform, File, FileSystemException;
import 'package:flutter/foundation.dart' show debugPrint;

/// Detect if running on macOS.
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
Future<String?> getHomeDirectoryNative() async {
  try {
    return Platform.environment['HOME'] ??
           Platform.environment['USERPROFILE'];
  } catch (_) {
    return null;
  }
}

/// Read a file's contents on native platforms.
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
