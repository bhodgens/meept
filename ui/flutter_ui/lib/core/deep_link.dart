// Session deep links (hitl-notifications leaf 03, contract C6).
//
// Grammar (C6, byte-identical in the Swift menubar half):
//
//   meept://session/<session-id>
//     - scheme literal "meept"
//     - host literal "session"
//     - <session-id> matches ^[A-Za-z0-9-]+$
//
// The /activity go_router route comes from the in-flight activity-views
// tree and MAY NOT EXIST; the feature gate checks route presence at
// RUNTIME against the router's live configuration (never an import).
// When the route is absent the link resolves to the daemon's web session
// URL <web base>/#/activity?session=<id>, served by internal/comm/web.
//
// The Swift menubar mirrors only the grammar literals (scheme, host, id
// regex); all navigation and fallback logic lives here.
library;

import 'package:flutter/foundation.dart' show debugPrint, kIsWeb;

import 'package:flutter/services.dart' show MethodChannel;

import 'package:go_router/go_router.dart';

import 'platform/platform_service.dart';

/// Scheme literal of the meept:// deep link grammar (C6).
const String deepLinkScheme = 'meept';

/// Host literal of the meept:// deep link grammar (C6).
const String deepLinkSessionHost = 'session';

/// The pinned C6 session-id grammar: ^[A-Za-z0-9-]+$.
final RegExp _sessionIdRe = RegExp(r'^[A-Za-z0-9-]+$');

/// macOS runner channel that delivers meept:// links into the Dart
/// listener (registered by MainFlutterWindow; see initDesktopDeepLinkChannel).
const MethodChannel _deepLinkChannel = MethodChannel(
  'com.caimlas.meept/deep_link',
);

/// Result of resolving a meept://session/<id> deep link (C6).
class DeepLinkTarget {
  /// The session id carried by the link (C6-validated).
  final String sessionID;

  /// True when the /activity route is absent and the link must open the
  /// daemon's web session URL instead of navigating in-app.
  final bool useWebFallback;

  /// Route-present: the in-app go_router location
  /// (`/activity?session=<id>`). Route-absent: the full web fallback URL
  /// (`<web base>/#/activity?session=<id>`).
  final String fallbackUrl;

  const DeepLinkTarget({
    required this.sessionID,
    required this.useWebFallback,
    required this.fallbackUrl,
  });

  @override
  String toString() =>
      'DeepLinkTarget(sessionID: $sessionID, useWebFallback: $useWebFallback, '
      'fallbackUrl: $fallbackUrl)';
}

/// True when the router's route table contains a GoRoute with path
/// "/activity" (top-level or nested). This is the C6 feature gate: a
/// runtime check on the configuration object, never an import of the
/// activity-views tree.
bool routerHasActivityRoute(GoRouter router) {
  bool walk(List<RouteBase> routes) {
    for (final RouteBase route in routes) {
      if (route is GoRoute && route.path == '/activity') {
        return true;
      }
      if (walk(route.routes)) {
        return true;
      }
    }
    return false;
  }

  return walk(router.configuration.routes);
}

/// Resolve [uri] per the pinned C6 grammar.
///
/// - Returns null for any link that is not `meept://session/<id>` with an
///   id matching ^[A-Za-z0-9-]+$ (bad scheme, bad host, bad/missing id).
/// - When [hasActivityRoute] is true the target navigates in-app.
/// - When false, [useWebFallback] is set and [fallbackUrl] is
///   `<webBase>/#/activity?session=<id>` (trailing slash on [webBase] is
///   tolerated).
DeepLinkTarget? resolveDeepLink(
  Uri uri, {
  required bool hasActivityRoute,
  String webBase = '',
}) {
  if (uri.scheme.toLowerCase() != deepLinkScheme) {
    return null;
  }
  if (uri.host.toLowerCase() != deepLinkSessionHost) {
    return null;
  }
  final String id = uri.pathSegments.isNotEmpty ? uri.pathSegments.first : '';
  if (!_sessionIdRe.hasMatch(id)) {
    return null;
  }
  if (hasActivityRoute) {
    return DeepLinkTarget(
      sessionID: id,
      useWebFallback: false,
      fallbackUrl: '/activity?session=$id',
    );
  }
  final String base = webBase.endsWith('/')
      ? webBase.substring(0, webBase.length - 1)
      : webBase;
  return DeepLinkTarget(
    sessionID: id,
    useWebFallback: true,
    fallbackUrl: '$base/#/activity?session=$id',
  );
}

/// Convenience resolver: feature-gates against [router]'s live route table.
DeepLinkTarget? resolveSessionDeepLink(
  Uri uri,
  GoRouter router, {
  String webBase = '',
}) {
  return resolveDeepLink(
    uri,
    hasActivityRoute: routerHasActivityRoute(router),
    webBase: webBase,
  );
}

/// Handle a deep link end-to-end: navigate [router] to
/// /activity?session=<id> when the route exists, otherwise hand the web
/// fallback URL to [openExternal] (default: platformService.openExternal).
///
/// Malformed links are ignored with a lowercase log line (no navigation,
/// no exception).
Future<void> handleSessionDeepLink(
  Uri uri, {
  required GoRouter router,
  required String webBase,
  void Function(String url)? openExternal,
}) async {
  final DeepLinkTarget? target = resolveSessionDeepLink(
    uri,
    router,
    webBase: webBase,
  );
  if (target == null) {
    debugPrint('[deep-link] ignored malformed link: $uri');
    return;
  }
  if (target.useWebFallback) {
    if (openExternal != null) {
      openExternal(target.fallbackUrl);
    } else {
      await platformService?.openExternal(target.fallbackUrl);
    }
    return;
  }
  router.go(target.fallbackUrl);
}

/// Desktop-only: register the macOS runner's deep-link channel and route
/// both the cold-start link (getInitialLink pull) and warm links
/// (onDeepLink pushes) through [onLink]. No-op on web.
Future<void> initDesktopDeepLinkChannel(
  Future<void> Function(Uri uri) onLink,
) async {
  if (kIsWeb) {
    return;
  }
  _deepLinkChannel.setMethodCallHandler((call) async {
    if (call.method == 'onDeepLink' && call.arguments is String) {
      await _handleChannelUrl(call.arguments as String, onLink);
    }
    return null;
  });
  try {
    final String? initial = await _deepLinkChannel.invokeMethod<String>(
      'getInitialLink',
    );
    await _handleChannelUrl(initial, onLink);
  } catch (e) {
    debugPrint('[deep-link] initial link fetch failed: $e');
  }
}

Future<void> _handleChannelUrl(
  String? url,
  Future<void> Function(Uri uri) onLink,
) async {
  if (url == null || url.isEmpty) {
    return;
  }
  final Uri uri = Uri.parse(url);
  await onLink(uri);
}
