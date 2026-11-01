// Deep-link navigation tests (hitl-notifications leaf 03, T2+T4).
//
// Covers the REAL app router surface: routerHasActivityRoute against an
// actual GoRouter configuration, and the desktop channel
// (getInitialLink / onDeepLink) landing a meept:// URL in the listener.
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:go_router/go_router.dart';

import 'package:meept_ui/core/deep_link.dart';
import 'package:meept_ui/core/router.dart' show router;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('routerHasActivityRoute (real router config)', () {
    test('the shipped router currently exposes /activity', () {
      // Feature-gate reality check: the activity-views tree's /activity
      // route IS present in the shipped router today. If this flips, the
      // feature gate must resolve to web fallback (covered below).
      expect(routerHasActivityRoute(router), isTrue);
    });

    test('a router without /activity reports absent', () {
      final bare = GoRouter(
        routes: [GoRoute(path: '/', builder: (_, __) => const SizedBox())],
      );
      expect(routerHasActivityRoute(bare), isFalse);
    });

    test(
      'a router with /activity nested under a shell route reports present',
      () {
        final shelled = GoRouter(
          routes: [
            ShellRoute(
              builder: (_, __, child) => child,
              routes: [
                GoRoute(
                  path: '/activity',
                  builder: (_, __) => const SizedBox(),
                ),
              ],
            ),
          ],
        );
        expect(routerHasActivityRoute(shelled), isTrue);
      },
    );
  });

  group('desktop deep-link channel (T4)', () {
    test('onDeepLink fires the listener with a valid meept:// URL', () async {
      Uri? received;
      await initDesktopDeepLinkChannel((uri) async => received = uri);

      final binding = TestDefaultBinaryMessengerBinding.instance;
      await binding.defaultBinaryMessenger.handlePlatformMessage(
        'com.caimlas.meept/deep_link',
        const StandardMethodCodec().encodeMethodCall(
          const MethodCall('onDeepLink', 'meept://session/abc-123'),
        ),
        (_) {},
      );
      expect(received, isNotNull);
      expect(received.toString(), 'meept://session/abc-123');
    });

    test(
      'onDeepLink with an empty payload does not fire the listener',
      () async {
        Uri? received;
        await initDesktopDeepLinkChannel((uri) async => received = uri);

        final binding = TestDefaultBinaryMessengerBinding.instance;
        await binding.defaultBinaryMessenger.handlePlatformMessage(
          'com.caimlas.meept/deep_link',
          const StandardMethodCodec().encodeMethodCall(
            const MethodCall('onDeepLink', ''),
          ),
          (_) {},
        );
        expect(received, isNull);
      },
    );

    test(
      'channel handler composes into navigation: valid link navigates',
      () async {
        final navigated = <String>[];
        await initDesktopDeepLinkChannel(
          (uri) => handleSessionDeepLink(
            uri,
            router: _RecordingRouter(navigated),
            webBase: 'http://localhost:8080',
          ),
        );

        final binding = TestDefaultBinaryMessengerBinding.instance;
        await binding.defaultBinaryMessenger.handlePlatformMessage(
          'com.caimlas.meept/deep_link',
          const StandardMethodCodec().encodeMethodCall(
            const MethodCall('onDeepLink', 'meept://session/abc-123'),
          ),
          (_) {},
        );
        expect(navigated, ['/activity?session=abc-123']);
      },
    );

    test(
      'invalid URL is ignored with a lowercase log, no navigation',
      () async {
        final navigated = <String>[];
        await initDesktopDeepLinkChannel(
          (uri) => handleSessionDeepLink(
            uri,
            router: _RecordingRouter(navigated),
            webBase: 'http://localhost:8080',
          ),
        );

        final binding = TestDefaultBinaryMessengerBinding.instance;
        await binding.defaultBinaryMessenger.handlePlatformMessage(
          'com.caimlas.meept/deep_link',
          const StandardMethodCodec().encodeMethodCall(
            const MethodCall('onDeepLink', 'meept://bogus/x'),
          ),
          (_) {},
        );
        expect(navigated, isEmpty);
      },
    );
  });
}

/// GoRouter stand-in recording go() calls; route table says /activity is
/// present (the shipped-router shape). The channel tests assert dispatch,
/// not real navigation.
class _RecordingRouter implements GoRouter {
  final List<String> navigated;

  _RecordingRouter(this.navigated);

  @override
  late final RouteConfiguration configuration = _RecordingConfig();

  @override
  void go(String location, {Object? extra}) => navigated.add(location);

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class _RecordingConfig implements RouteConfiguration {
  @override
  List<RouteBase> get routes => <RouteBase>[
    GoRoute(path: '/activity', builder: (_, __) => const SizedBox()),
  ];

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}
