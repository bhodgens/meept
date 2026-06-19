import 'dart:async';
import 'dart:io';
import 'dart:ui' show Rect, Size;

import 'package:flutter/foundation.dart' show debugPrint;
import 'package:window_manager/window_manager.dart';

import '../core/constants.dart';
import 'storage_service.dart';

/// Persists and restores desktop window geometry (position + size) across
/// app restarts. Uses [WindowManager] for native window control and
/// [StorageService] (SharedPreferences) for persistence.
///
/// On web/mobile this is a no-op.
class WindowGeometryService with WindowListener {
  WindowGeometryService._();
  static final WindowGeometryService instance = WindowGeometryService._();

  static const _minWidth = 600.0;
  static const _minHeight = 400.0;

  Timer? _debounce;

  /// Ensures [windowManager] is initialized and restores saved geometry.
  /// Call once at startup, before [runApp].
  static Future<void> initialize() async {
    if (!Platform.isWindows && !Platform.isMacOS && !Platform.isLinux) {
      return;
    }

    try {
      await windowManager.ensureInitialized();

      // Hide the window until geometry is restored to prevent a flash of
      // the default size/position on launch.
      await windowManager.waitUntilReadyToShow(const WindowOptions(), () async {
        await _restoreGeometry();
        await windowManager.show();
      });

      // Listen for live resize/move events to persist geometry as the user
      // drags, so we don't lose state on force-quit.
      windowManager.addListener(instance);
    } catch (e) {
      debugPrint('[window_geometry] init failed: $e');
    }
  }

  /// Saves the current window position, size, and maximized state.
  /// Call on app close or detach window events.
  static Future<void> save() async {
    if (!Platform.isWindows && !Platform.isMacOS && !Platform.isLinux) {
      return;
    }

    instance._debounce?.cancel();
    instance._debounce = null;

    try {
      final prefs = StorageService.instance;
      if (!prefs.isInitialized) return;

      final isMaximized = await windowManager.isMaximized();
      await prefs.setBool(AppConstants.windowMaximizedPref, isMaximized);

      if (isMaximized) {
        // Don't overwrite position/size when maximized — keep last normal
        // geometry so un-maximize restores the right window.
        return;
      }

      final size = await windowManager.getSize();
      final position = await windowManager.getPosition();

      await prefs.setDouble(AppConstants.windowWidthPref, size.width);
      await prefs.setDouble(AppConstants.windowHeightPref, size.height);
      await prefs.setDouble(AppConstants.windowXPref, position.dx);
      await prefs.setDouble(AppConstants.windowYPref, position.dy);
    } catch (e) {
      debugPrint('[window_geometry] save failed: $e');
    }
  }

  // --- WindowListener: debounced save on resize/move ---

  @override
  void onWindowResized() => _scheduleSave();

  @override
  void onWindowMoved() => _scheduleSave();

  @override
  void onWindowMaximize() {
    StorageService.instance
        .setBool(AppConstants.windowMaximizedPref, true)
        .catchError((_) {});
  }

  @override
  void onWindowUnmaximize() {
    StorageService.instance
        .setBool(AppConstants.windowMaximizedPref, false)
        .catchError((_) {});
  }

  void _scheduleSave() {
    _debounce?.cancel();
    _debounce = Timer(const Duration(seconds: 1), save);
  }

  static Future<void> _restoreGeometry() async {
    final prefs = StorageService.instance;
    if (!prefs.isInitialized) return;

    final wasMaximized =
        prefs.getBool(AppConstants.windowMaximizedPref) ?? false;

    final width = prefs.getDouble(AppConstants.windowWidthPref);
    final height = prefs.getDouble(AppConstants.windowHeightPref);
    final x = prefs.getDouble(AppConstants.windowXPref);
    final y = prefs.getDouble(AppConstants.windowYPref);

    if (wasMaximized) {
      await windowManager.maximize();
      return;
    }

    if (width != null && height != null) {
      final clampedWidth = width < _minWidth ? _minWidth : width;
      final clampedHeight = height < _minHeight ? _minHeight : height;

      if (x != null && y != null) {
        await windowManager.setBounds(
          Rect.fromLTWH(x, y, clampedWidth, clampedHeight),
        );
      } else {
        await windowManager.setSize(Size(clampedWidth, clampedHeight));
      }
    }
  }
}
