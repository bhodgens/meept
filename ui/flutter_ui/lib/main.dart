import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'services/storage_service.dart';
import 'services/websocket_service.dart';
import 'theme/cyberpunk_theme.dart';
import 'features/home/home_screen.dart';
import 'providers/providers.dart';

void main() async {
  WidgetsFlutterBinding.ensureInitialized();

  // Initialize persistent storage before any provider or service reads
  await StorageService.instance.init();

  runZonedGuarded(() {
    runApp(
      const ProviderScope(
        child: CyberpunkApp(),
      ),
    );
  }, (error, stackTrace) {
    // Log unhandled errors — in production, wire this to a crash reporting service
    debugPrint('Unhandled error: $error');
    debugPrint('Stack trace: $stackTrace');
  });
}

class CyberpunkApp extends StatelessWidget {
  const CyberpunkApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Meept Cyberpunk UI',
      debugShowCheckedModeBanner: false,
      theme: CyberpunkTheme.darkTheme,
      home: const _AppLifecycleWrapper(child: HomeScreen()),
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

  const _AppLifecycleWrapper({required this.child, super.key});

  @override
  ConsumerState<_AppLifecycleWrapper> createState() =>
      _AppLifecycleWrapperState();
}

class _AppLifecycleWrapperState
    extends ConsumerState<_AppLifecycleWrapper>
    with WidgetsBindingObserver {
  late final WebSocketService _websocket;
  Timer? _reconnectDelay;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _websocket = ref.read(websocketProvider);
  }

  @override
  void dispose() {
    _reconnectDelay?.cancel();
    _reconnectDelay = null;
    WidgetsBinding.instance.removeObserver(this);
    _websocket.disconnect();
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
