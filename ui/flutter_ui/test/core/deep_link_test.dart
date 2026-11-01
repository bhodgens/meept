// Deep-link grammar + feature-gate tests (hitl-notifications leaf 03, C6).
//
// These tests pin the C6 grammar for BOTH clients: the Swift menubar
// mirrors only the literals (scheme "meept", host "session", id regex
// ^[A-Za-z0-9-]+$) and delegates all resolution logic to this module, so
// this file is the single grammar authority (documented Swift limitation:
// no in-repo Swift test harness).
import 'package:flutter/material.dart' show SizedBox;
import 'package:flutter_test/flutter_test.dart';

import 'package:go_router/go_router.dart';

import 'package:meept_ui/core/deep_link.dart';

void main() {
  group('resolveDeepLink (C6 grammar)', () {
    test('valid link with activity route present navigates in-app', () {
      final target = resolveDeepLink(
        Uri.parse('meept://session/abc-123'),
        hasActivityRoute: true,
      );
      expect(target, isNotNull);
      expect(target!.sessionID, 'abc-123');
      expect(target.useWebFallback, isFalse);
      expect(target.fallbackUrl, '/activity?session=abc-123');
    });

    test('falls back to web when activity route absent', () {
      final target = resolveDeepLink(
        Uri.parse('meept://session/abc-123'),
        hasActivityRoute: false,
        webBase: 'http://localhost:8080',
      );
      expect(target, isNotNull);
      expect(target!.useWebFallback, isTrue);
      expect(
        target.fallbackUrl,
        'http://localhost:8080/#/activity?session=abc-123',
      );
    });

    test('web fallback tolerates trailing slash on webBase', () {
      final target = resolveDeepLink(
        Uri.parse('meept://session/abc-123'),
        hasActivityRoute: false,
        webBase: 'http://localhost:8080/',
      );
      expect(
        target!.fallbackUrl,
        'http://localhost:8080/#/activity?session=abc-123',
      );
    });

    test('bad scheme is rejected', () {
      expect(
        resolveDeepLink(
          Uri.parse('https://session/abc-123'),
          hasActivityRoute: true,
        ),
        isNull,
      );
    });

    test('bad host is rejected', () {
      expect(
        resolveDeepLink(
          Uri.parse('meept://tasks/abc-123'),
          hasActivityRoute: true,
        ),
        isNull,
      );
    });

    test('missing id is rejected', () {
      expect(
        resolveDeepLink(Uri.parse('meept://session/'), hasActivityRoute: true),
        isNull,
      );
    });

    test('empty id is rejected', () {
      expect(
        resolveDeepLink(Uri.parse('meept://session'), hasActivityRoute: true),
        isNull,
      );
    });

    test('id with invalid characters is rejected (ASCII regex)', () {
      // underscore, dot, slash-walk, unicode are all outside ^[A-Za-z0-9-]+$
      expect(
        resolveDeepLink(
          Uri.parse('meept://session/abc_123'),
          hasActivityRoute: true,
        ),
        isNull,
      );
      expect(
        resolveDeepLink(
          Uri.parse('meept://session/abc.123'),
          hasActivityRoute: true,
        ),
        isNull,
      );
      expect(
        resolveDeepLink(
          Uri.parse('meept://session/se%20ssion'),
          hasActivityRoute: true,
        ),
        isNull,
      );
    });

    test('uppercase and mixed alphanumeric ids are accepted', () {
      final target = resolveDeepLink(
        Uri.parse('meept://session/SessiON-9X'),
        hasActivityRoute: false,
        webBase: 'http://localhost:8080',
      );
      expect(target!.sessionID, 'SessiON-9X');
      expect(
        target.fallbackUrl,
        'http://localhost:8080/#/activity?session=SessiON-9X',
      );
    });

    test('extra path segments beyond the id do not crash resolution', () {
      // The grammar pins only the first segment; the id itself is valid so
      // the link resolves (trailing junk is ignored, not user-controlled
      // in the menubar path).
      final target = resolveDeepLink(
        Uri.parse('meept://session/abc-123/extra'),
        hasActivityRoute: true,
      );
      expect(target!.sessionID, 'abc-123');
    });
  });

  group('handleSessionDeepLink (navigation + fallback dispatch)', () {
    test('route-present target navigates the router to /activity', () async {
      final navigated = <String>[];
      await handleSessionDeepLink(
        Uri.parse('meept://session/abc-123'),
        router: _FakeRouter(hasActivityRoute: true, navigated: navigated),
        webBase: 'http://localhost:8080',
        openExternal: (url) => navigated.add('EXTERNAL:$url'),
      );
      expect(navigated, ['/activity?session=abc-123']);
    });

    test('route-absent target opens the external web fallback', () async {
      final actions = <String>[];
      await handleSessionDeepLink(
        Uri.parse('meept://session/abc-123'),
        router: _FakeRouter(hasActivityRoute: false, navigated: actions),
        webBase: 'http://localhost:8080',
        openExternal: (url) => actions.add('EXTERNAL:$url'),
      );
      expect(actions, [
        'EXTERNAL:http://localhost:8080/#/activity?session=abc-123',
      ]);
    });

    test('malformed link is ignored without navigation or throw', () async {
      final actions = <String>[];
      await handleSessionDeepLink(
        Uri.parse('meept://other/abc'),
        router: _FakeRouter(hasActivityRoute: true, navigated: actions),
        webBase: 'http://localhost:8080',
        openExternal: (url) => actions.add('EXTERNAL:$url'),
      );
      expect(actions, isEmpty);
    });
  });
}

class _FakeRouter implements GoRouter {
  final bool hasActivityRoute;
  final List<String> navigated;

  _FakeRouter({required this.hasActivityRoute, required this.navigated});

  @override
  void go(String location, {Object? extra}) {
    navigated.add(location);
  }

  @override
  late final RouteConfiguration configuration = _FakeConfiguration(this);

  // The deep-link path only touches route presence + go(); everything
  // else from the interface is dead weight, never called.
  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

/// Stand-in RouteConfiguration exposing just the routes list
/// routerHasActivityRoute walks.
class _FakeConfiguration implements RouteConfiguration {
  final _FakeRouter router;

  _FakeConfiguration(this.router);

  @override
  List<RouteBase> get routes => <RouteBase>[
    if (router.hasActivityRoute)
      GoRoute(path: '/activity', builder: (_, __) => const SizedBox()),
  ];

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}
