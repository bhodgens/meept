import 'dart:async';
// Platform checks removed for web compatibility
import 'package:flutter/foundation.dart' show kIsWeb;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:sentry_flutter/sentry_flutter.dart';
import 'package:window_manager/window_manager.dart';
import 'services/storage_service.dart';
import 'services/sdk_client.dart';
import 'services/websocket_service.dart';
import 'services/window_geometry_service.dart';
import 'theme/cyberpunk_theme.dart';
import 'core/constants.dart';
import 'core/router.dart';
import 'providers/providers.dart';

void main() async {
  WidgetsFlutterBinding.ensureInitialized();

  // Initialize persistent storage before any provider or service reads
  await StorageService.instance.init();

  // Restore saved window size/position on desktop platforms
  await WindowGeometryService.initialize();
  if (!kIsWeb) {  // Desktop-only: window management
    // Intercept the native close button so we can persist geometry
    await windowManager.setPreventClose(true);
    windowManager.addListener(_WindowCloseHandler());
  }

  // Initialize certificate pinning before any HTTP/WebSocket connections
  await SdkApiClient.initCertPinning();

  // Initialize Sentry for crash reporting (only when a real DSN is configured)
  // Environment variables not available on web
    final sentryDsn = null;  // Platform.environment['SENTRY_DSN'];
  if (sentryDsn != null && sentryDsn.isNotEmpty) {
    await SentryFlutter.init(
      (options) {
        options.dsn = sentryDsn;
        options.tracesSampleRate = 1.0;
      },
      appRunner: () => runApp(
        ProviderScope(
          child: _ModifierKeyInitializer(
            child: const CyberpunkApp(),
          ),
        ),
      ),
    );
  } else {
    runApp(
      ProviderScope(
        child: _ModifierKeyInitializer(
          child: const CyberpunkApp(),
        ),
      ),
    );
  }
}

/// Initializes the modifier key preference at app startup.
/// Must be wrapped in ProviderScope.
class _ModifierKeyInitializer extends ConsumerStatefulWidget {
  final Widget child;
  const _ModifierKeyInitializer({required this.child});

  @override
  ConsumerState<_ModifierKeyInitializer> createState() => _ModifierKeyInitializerState();
}

class _ModifierKeyInitializerState extends ConsumerState<_ModifierKeyInitializer> {
  @override
  void initState() {
    super.initState();
    // Load the modifier key preference from storage
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(modifierKeyProvider.notifier).load();
    });
  }

  @override
  Widget build(BuildContext context) => widget.child;
}

/// Listens for the native close event, saves window geometry, then
/// allows the window to close.
class _WindowCloseHandler extends WindowListener {
  @override
  void onWindowClose() async {
    await WindowGeometryService.save();
    await windowManager.setPreventClose(false);
    await windowManager.destroy();
  }
}

class CyberpunkApp extends StatelessWidget {
  const CyberpunkApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp.router(
      routerConfig: router,
      title: 'meept gui client v${AppConstants.appVersion}',
      debugShowCheckedModeBanner: false,
      theme: CyberpunkTheme.darkTheme,
      builder: (context, child) {
        return _AppLifecycleWrapper(child: child!);
      },
    );
  }
}

/// Wraps the app's home screen to handle app lifecycle events.
///
/// On `paused` (app backgrounded), it disconnects the WebSocket so the OS
/// can cleanly release the network socket. On `resumed` (app foregrounded),
/// it reconnects after a short delay to let the OS network stack settle.
class _AppLifecycleWrapper extends ConsumerStatefulWidget {
  final Widget child;

  const _AppLifecycleWrapper({required this.child});

  @override
  ConsumerState<_AppLifecycleWrapper> createState() =>
      _AppLifecycleWrapperState();
}

class _AppLifecycleWrapperState
    extends ConsumerState<_AppLifecycleWrapper>
    with WidgetsBindingObserver {
  Timer? _reconnectDelay;

  /// Always returns the current WebSocketService instance from the provider.
  WebSocketService get _websocket => ref.read(websocketProvider);

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    // Eagerly start the connection monitor so health checks run.
    ref.read(connectionMonitorProvider);
  }

  @override
  void dispose() {
    _reconnectDelay?.cancel();
    _reconnectDelay = null;
    WidgetsBinding.instance.removeObserver(this);
    _websocket.pause();
    super.dispose();
  }

  void _scheduleReconnect() {
    // Cancel any pending reconnect to avoid duplicates
    _reconnectDelay?.cancel();
    _reconnectDelay = Timer(const Duration(seconds: 1), () {
      if (mounted) {
        _websocket.connect();
      }
    });
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    super.didChangeAppLifecycleState(state);
    switch (state) {
      case AppLifecycleState.paused:
        _websocket.pause();
        break;
      case AppLifecycleState.resumed:
        _scheduleReconnect();
        break;
      default:
        break;
    }
  }

  @override
  Widget build(BuildContext context) {
    return widget.child;
  }
}
