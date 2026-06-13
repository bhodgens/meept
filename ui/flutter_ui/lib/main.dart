import 'dart:async';
import 'dart:io' show Platform;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:sentry_flutter/sentry_flutter.dart';
import 'services/storage_service.dart';
import 'services/api_client.dart';
import 'services/websocket_service.dart';
import 'theme/cyberpunk_theme.dart';
import 'core/constants.dart';
import 'core/router.dart';
import 'providers/providers.dart';

void main() async {
  WidgetsFlutterBinding.ensureInitialized();

  // Initialize persistent storage before any provider or service reads
  await StorageService.instance.init();

  // Initialize certificate pinning before any HTTP/WebSocket connections
  await ApiClient.initCertPinning();

  // Initialize Sentry for crash reporting
  await SentryFlutter.init(
    (options) {
      options.dsn = Platform.environment['SENTRY_DSN'] ??
          'https://placeholder@placeholder.ingest.sentry.io/placeholder';
      options.debug = true;
      options.tracesSampleRate = 1.0;
    },
    appRunner: () {
      runApp(
        const ProviderScope(
          child: CyberpunkApp(),
        ),
      );
    },
  );
}

class CyberpunkApp extends StatelessWidget {
  const CyberpunkApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp.router(
      routerConfig: router,
      title: 'Meept GUI Client v${AppConstants.appVersion}',
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
