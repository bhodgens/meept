// Deep-link listener host (hitl-notifications leaf 03, T2).
//
// Mounted once above the navigator (beside AcpPermissionDialogHost in
// CyberpunkApp's builder). Registers the macOS runner channel on desktop
// (cold-start + warm meept://session/<id> links) and renders nothing.
//
// The web fallback URL base is the daemon endpoint the app already dials
// (StorageService host/port over https — the same resolution PairingService
// and SdkApiClient use; the web payload server internal/comm/web listens on
// the daemon's transport).
import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart'
    show ConsumerStatefulWidget, ConsumerState;

import '../services/storage_service.dart';
import 'deep_link.dart';
import 'router.dart' show router;

/// Builds the web fallback base from the stored daemon endpoint.
/// Source pinned: StorageService.getApiHost/getApiPort over https (same
/// precedence as SdkApiClient.storage / PairingService.exchange); falls
/// back to AppConstants defaults via those getters' own defaults.
String resolveWebBase(StorageService storage) {
  final String host = storage.getApiHost() ?? 'localhost';
  final int port = storage.getApiPort() ?? 8081;
  return 'https://$host:$port';
}

/// Stateless-ish host that starts deep-link handling when mounted.
class DeepLinkListenerHost extends ConsumerStatefulWidget {
  final Widget child;

  const DeepLinkListenerHost({super.key, required this.child});

  @override
  ConsumerState<DeepLinkListenerHost> createState() =>
      _DeepLinkListenerHostState();
}

class _DeepLinkListenerHostState extends ConsumerState<DeepLinkListenerHost> {
  @override
  void initState() {
    super.initState();
    // Web: the module only ever produces fallback navigation, and there is
    // no OS to deliver meept:// links from — nothing registers. Desktop:
    // the runner channel routes cold-start and warm links here.
    if (!kIsWeb) {
      initDesktopDeepLinkChannel(
        (uri) => handleSessionDeepLink(
          uri,
          router: router,
          webBase: resolveWebBase(StorageService.instance),
        ),
      );
    }
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
