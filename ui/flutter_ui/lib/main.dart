import 'dart:async';
// Platform checks removed for web compatibility
import 'dart:ui' show PlatformDispatcher;

import 'package:flutter/foundation.dart' show kIsWeb, FlutterError, debugPrint;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:sentry_flutter/sentry_flutter.dart';
import 'package:window_manager/window_manager.dart';
import 'services/storage_service.dart';
import 'services/sdk_client.dart';
import 'services/websocket_service.dart';
import 'services/window_geometry_service.dart';
import 'theme/app_palette.dart';
import 'theme/colors.dart';
import 'theme/palette_provider.dart';
import 'core/constants.dart';
import 'core/router.dart';
import 'providers/providers.dart';
import 'providers/tool_exit_guard.dart';

/// The app's Riverpod container, set by [main] before `runApp` and published
/// here so code that runs outside the widget tree can read the same providers
/// the panels write to.
///
/// The desktop window-close listener has no `BuildContext`, and closing the
/// window unmounts the open panel (and any unsaved config edits with it), so
/// it has to reach [toolExitGuardProvider] where [ToolPanelShell] and the home
/// layouts do.
late final ProviderContainer appProviderContainer;

void main() async {
  WidgetsFlutterBinding.ensureInitialized();

  // One container for the whole app. Created here (rather than by
  // ProviderScope) so [appProviderContainer] is available to the window-close
  // listener below, which lives outside the widget tree.
  appProviderContainer = ProviderContainer();

  // Catch unhandled async errors that escape Future chains (fire-and-forget
  // methods, timer callbacks, etc.). Without this, Dart prints the error but
  // the app has no chance to log or report it properly.
  PlatformDispatcher.instance.onError = (error, stack) {
    debugPrint('[error] unhandled: $error');
    return true;
  };

  // Catch framework errors (build, layout, painting) that Flutter would
  // otherwise dump to the console in debug mode and silently swallow in
  // release mode.
  FlutterError.onError = (details) {
    FlutterError.presentError(details);
    debugPrint('[error] framework: ${details.exception}');
  };

  // Initialize persistent storage before any provider or service reads
  await StorageService.instance.init();

  // Resolve the stored ui theme before runApp so the first frame already
  // renders in the saved palette and CyberpunkColors forwards to it.
  initStoredTheme();

  // Restore saved window size/position on desktop platforms
  await WindowGeometryService.initialize();
  if (!kIsWeb) {
    // Desktop-only: window management
    // Intercept the native close button so we can persist geometry
    await windowManager.setPreventClose(true);
    windowManager.addListener(WindowCloseHandler(appProviderContainer));
    // KNOWN GAP (audit F62 regression, group F): the veto below covers the
    // window close button and the red traffic light only. window_manager
    // implements its veto as NSWindowDelegate.windowShouldClose, and
    // `NSApp.terminate` (Cmd+Q, or Quit from the menu bar) never calls it -
    // the plugin implements no applicationShouldTerminate and neither does
    // macos/Runner/AppDelegate.swift, so the app terminates with unsaved
    // edits and no prompt. Closing this needs a native
    // applicationShouldTerminate in AppDelegate that asks this same guard
    // over a channel and answers `.terminateLater`; that is a desktop build
    // change and could not be verified (or built) here, so it is documented
    // instead of half-implemented.
  }

  // Initialize certificate pinning (desktop only - web uses browser TLS)
  if (!kIsWeb) {
    await SdkApiClient.initCertPinning();
  }

  // Initialize Sentry for crash reporting (only when a real DSN is configured)
  // Environment variables not available on web
  const sentryDsn = null; // Platform.environment['SENTRY_DSN'];
  if (sentryDsn != null && sentryDsn.isNotEmpty) {
    await SentryFlutter.init(
      (options) {
        options.dsn = sentryDsn;
        options.tracesSampleRate = 1.0;
      },
      appRunner: () => runApp(
        UncontrolledProviderScope(
          container: appProviderContainer,
          child: const _ModifierKeyInitializer(child: CyberpunkApp()),
        ),
      ),
    );
  } else {
    runApp(
      UncontrolledProviderScope(
        container: appProviderContainer,
        child: const _ModifierKeyInitializer(child: CyberpunkApp()),
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
  ConsumerState<_ModifierKeyInitializer> createState() =>
      _ModifierKeyInitializerState();
}

class _ModifierKeyInitializerState
    extends ConsumerState<_ModifierKeyInitializer> {
  @override
  void initState() {
    super.initState();
    // Load the modifier key preference from storage
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(modifierKeyProvider.notifier).load();
      ref.read(guiLayoutProvider.notifier).load();
      // Load rendering prefs from the daemon config (best-effort; the
      // daemon may not be reachable yet — defaults hold until it is).
      ref
          .read(renderingPrefsProvider.notifier)
          .load(ref.read(sdkClientProvider));
    });
  }

  @override
  Widget build(BuildContext context) => widget.child;
}

/// The window-lifecycle calls the close path makes, behind one seam.
///
/// Injecting them is what lets a widget test drive [WindowCloseHandler]: the
/// window_manager plugin and the geometry persistence both need a real
/// platform window, so a test that exercised the veto would otherwise have to
/// reach the plugin.
class WindowCloseActions {
  const WindowCloseActions({
    required this.saveGeometry,
    required this.keepOpen,
    required this.close,
  });

  /// Persist window geometry before the window goes away.
  final Future<void> Function() saveGeometry;

  /// Keep the window open.
  ///
  /// The plugin's prevent-close latch is sticky: [main] sets it once and only
  /// [close] clears it, so the window stays open because nothing cleared it.
  /// This call repeats the startup setting (a no-op while it is already true)
  /// rather than being the re-arm step the veto depends on.
  final Future<void> Function() keepOpen;

  /// Let the window close and destroy it.
  final Future<void> Function() close;

  /// The real window-manager calls.
  static final WindowCloseActions real = WindowCloseActions(
    saveGeometry: WindowGeometryService.save,
    keepOpen: () => windowManager.setPreventClose(true),
    close: () async {
      await windowManager.setPreventClose(false);
      await windowManager.destroy();
    },
  );
}

/// Asks the open panel's exit guard before the window is allowed to close.
///
/// Closing the window unmounts every panel, so it drops unsaved config edits
/// exactly like the shared back control does. A veto keeps the window - and
/// the panel, and its edits - exactly as they were. With no guard registered
/// (no panel holds unsaved state) the exit goes straight through, so the
/// handler adds nothing to the ordinary close.
///
/// Scope: this covers the window close button and the red traffic light. It
/// does NOT cover Cmd+Q - see the known-gap note in [main].
class WindowCloseHandler extends WindowListener {
  WindowCloseHandler(this.container, {WindowCloseActions? actions})
    : actions = actions ?? WindowCloseActions.real;

  /// The app's container, read for the guard the open panel registered.
  final ProviderContainer container;

  /// The window-manager calls, injectable for tests.
  final WindowCloseActions actions;

  /// The close request currently being decided, if any.
  ///
  /// The guard is asynchronous (it usually opens the discard dialog), so a
  /// second close request arriving before the first answer - a double click
  /// on the close button, a close during the dialog - would ask the guard
  /// again and stack a second dialog. While one request is in flight,
  /// further requests share its future instead of being told the close is
  /// already done: the listener API gives no future back, but
  /// [handleCloseRequest] is awaited by tests and future callers, and a
  /// caller released before the decision was made would be told "done" while
  /// nothing had been decided. The shared registry
  /// (`ToolExitGuardRegistry._inFlight`) shares its answer the same way.
  Future<void>? _closePending;

  @override
  void onWindowClose() {
    // The listener API gives no future back; handleCloseRequest is the
    // awaitable half so a test can drive the same path.
    unawaited(handleCloseRequest());
  }

  /// Ask the exit guard, then persist geometry and close the window.
  ///
  /// Returns when the request has been decided (and, when allowed, when the
  /// window-manager calls are done), so a widget test can await the decision
  /// the listener itself cannot wait for. A request that arrives while one is
  /// undecided returns that request's future, so both callers see the one
  /// decision and neither is released early.
  Future<void> handleCloseRequest() {
    final inFlight = _closePending;
    if (inFlight != null) return inFlight;
    final pending = _decideCloseRequest();
    _closePending = pending;
    return pending;
  }

  /// The decision half of [handleCloseRequest], with the latch cleared on
  /// every outcome so the next request starts fresh.
  Future<void> _decideCloseRequest() async {
    try {
      final allowed = await container.read(toolExitGuardProvider).requestExit();
      if (!allowed) {
        await actions.keepOpen();
        return;
      }
      // Geometry is persisted before the window goes away.
      await actions.saveGeometry();
      await actions.close();
    } finally {
      _closePending = null;
    }
  }
}

/// Resolves the stored ui theme into startup state, before runApp.
///
/// Reads the 'ui_theme' preference first; if unset, falls back to the legacy
/// 'theme' key but only when its value names a known variant. Stores the
/// result in [initialThemeName] (used as themeNameProvider's initial state)
/// and activates the palette on the static CyberpunkColors forwarding layer
/// so all direct color references follow it from frame one.
void initStoredTheme() {
  final storage = StorageService.instance;
  final stored = storage.getUiTheme() ?? _knownLegacyTheme();
  if (stored == null) return;

  final palette = AppPalette.forName(stored);
  initialThemeName = palette.name;
  CyberpunkColors.setActive(palette);
}

/// Legacy 'theme' value, only when it already names a known ui variant.
String? _knownLegacyTheme() {
  final legacy = StorageService.instance.getTheme();
  if (legacy != null && AppPalette.palettes.containsKey(legacy)) {
    return legacy;
  }
  return null;
}

class CyberpunkApp extends ConsumerWidget {
  const CyberpunkApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return MaterialApp.router(
      routerConfig: router,
      title: 'meept gui client v${AppConstants.appVersion}',
      debugShowCheckedModeBanner: false,
      theme: ref.watch(appThemeProvider),
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

class _AppLifecycleWrapperState extends ConsumerState<_AppLifecycleWrapper>
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
