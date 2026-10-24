// ACP human-permission relay provider (leaf 05, Contract E): pending queue
// keyed by id, WS requested/resolved frame handling, initial + reconnect
// HTTP catch-up (GET /api/v1/acp/permissions), and allow/deny POST to
// /api/v1/acp/permissions/resolve.
//
// Stub idiom mirrors test/features/calendar/create_event_dialog_test.dart
// (subclass SdkApiClient, override the endpoint methods) and
// test/providers/job_provider_test.dart (injectable WS stream).

import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/providers/acp_permission_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

class _StubSdkClient extends SdkApiClient {
  _StubSdkClient() : super(host: 'localhost', port: 8081);

  /// Snapshot returned by getAcpPermissions (frozen envelope shape).
  Map<String, dynamic> snapshot = {'enabled': false, 'pending': <Object>[]};

  /// When set, getAcpPermissions blocks until this completes — lets a test
  /// interleave a WS frame mid-fetch to pin the race-merge behaviour.
  Completer<void>? getGate;

  /// When set, resolveAcpPermission throws this instead of succeeding.
  Object? resolveError;

  final resolves = <List<String>>[];

  @override
  Future<Map<String, dynamic>> getAcpPermissions() async {
    final gate = getGate;
    if (gate != null) await gate.future;
    return snapshot;
  }

  @override
  Future<Map<String, dynamic>> resolveAcpPermission({
    required String id,
    required String decision,
  }) async {
    final err = resolveError;
    if (err != null) throw err;
    resolves.add([id, decision]);
    return {'decision': decision, 'reason': 'answered'};
  }
}

class _InjectableWebSocket extends WebSocketService {
  _InjectableWebSocket()
    : _messages = StreamController<Map<String, dynamic>>.broadcast(sync: true),
      _connection = StreamController<bool>.broadcast(sync: true),
      super(host: 'localhost', port: 8081);

  final StreamController<Map<String, dynamic>> _messages;
  final StreamController<bool> _connection;

  @override
  Future<void> connect({String? path}) async {}

  @override
  void disconnect() {}

  @override
  bool get isConnected => false;

  @override
  Stream<bool> get connectionStream => _connection.stream;

  void pushConnection(bool connected) => _connection.add(connected);

  /// Pushes a raw frame through the SAME filter the production
  /// subscribeToAcpPermissions applies.
  void pushFrame(Map<String, dynamic> frame) {
    final type = frame['type'];
    final topic = frame['source_topic'];
    if (type != 'agent_progress') return;
    if (topic != 'acp.permission.requested' &&
        topic != 'acp.permission.resolved') {
      return;
    }
    _messages.add(frame);
  }

  @override
  Stream<Map<String, dynamic>> subscribeToAcpPermissions() => _messages.stream;
}

Map<String, dynamic> _requested({
  String id = 'perm-aaaa1111',
  String agentId = 'claude',
  String sessionId = 'sess-1',
  String summary = 'edit file outside workspace',
  String createdAt = '2026-10-19T12:00:00Z',
  List<Map<String, dynamic>>? options,
}) => {
  'type': 'agent_progress',
  'source_topic': 'acp.permission.requested',
  'id': id,
  'agent_id': agentId,
  'session_id': sessionId,
  'summary': summary,
  'created_at': createdAt,
  if (options != null) 'options': options,
};

Map<String, dynamic> _resolved({
  String id = 'perm-aaaa1111',
  String decision = 'allow',
  String reason = 'answered',
  String? approver,
}) => {
  'type': 'agent_progress',
  'source_topic': 'acp.permission.resolved',
  'id': id,
  'agent_id': 'claude',
  'session_id': 'sess-1',
  'summary': 'edit file outside workspace',
  'created_at': '2026-10-19T12:00:00Z',
  'decision': decision,
  'reason': reason,
  if (approver != null) 'approver': approver,
};

void main() {
  late _StubSdkClient client;
  late _InjectableWebSocket ws;
  late AcpPermissionNotifier notifier;

  setUp(() {
    client = _StubSdkClient();
    ws = _InjectableWebSocket();
    notifier = AcpPermissionNotifier(sdkClient: client, websocket: ws);
  });

  tearDown(() {
    notifier.dispose();
    ws.dispose();
  });

  group('requested/resolved frames', () {
    test('requested frame queues an entry keyed by id; head is first', () {
      ws.pushFrame(_requested());
      expect(notifier.state.count, 1);
      expect(notifier.state.head!.id, 'perm-aaaa1111');
      expect(notifier.state.head!.agentId, 'claude');
      expect(notifier.state.enabled, isTrue);
    });

    test('duplicate requested id keeps its FIFO slot', () {
      ws.pushFrame(_requested(id: 'a'));
      ws.pushFrame(_requested(id: 'b'));
      ws.pushFrame(_requested(id: 'a', summary: 're-sent'));
      expect(notifier.state.queue.map((p) => p.id), ['a', 'b']);
      expect(notifier.state.head!.summary, 're-sent');
    });

    test('resolved frame clears the entry by id (external answer/timeout)', () {
      ws.pushFrame(_requested(id: 'a'));
      ws.pushFrame(_requested(id: 'b'));
      ws.pushFrame(_resolved(id: 'a', decision: 'deny', reason: 'timeout'));
      expect(notifier.state.count, 1);
      expect(notifier.state.head!.id, 'b');
    });

    test('resolved for an unknown id is an idempotent no-op', () {
      ws.pushFrame(_requested(id: 'a'));
      ws.pushFrame(_resolved(id: 'zzz'));
      expect(notifier.state.count, 1);
      expect(notifier.state.notice, isNull);
    });

    group('answered-elsewhere notice (TUI parity, audit M5 attribution)', () {
      test('resolved frame for a still-pending entry sets the notice with '
          'decision, reason, and approver', () {
        ws.pushFrame(_requested(id: 'a'));
        ws.pushFrame(
          _resolved(
            id: 'a',
            decision: 'allow',
            reason: 'answered',
            approver: 'peercred:pid=900,uid=501',
          ),
        );
        expect(notifier.state.count, 0);
        final notice = notifier.state.notice!;
        expect(notice, contains('allow'));
        expect(notice, contains('(answered)'));
        expect(notice, contains('by peercred:pid=900,uid=501'));
      });

      test('timeout resolution notices without an approver', () {
        ws.pushFrame(_requested(id: 'a'));
        ws.pushFrame(_resolved(id: 'a', decision: 'deny', reason: 'timeout'));
        final notice = notifier.state.notice!;
        expect(notice, contains('deny (timeout)'));
        expect(notice, isNot(contains('by ')));
      });

      test(
        'a locally-resolved entry does NOT get a notice (echo no-ops)',
        () async {
          ws.pushFrame(_requested(id: 'a'));
          await notifier.resolve('a', 'allow');
          expect(notifier.state.notice, isNull);
          ws.pushFrame(_resolved(id: 'a')); // daemon echo
          expect(notifier.state.notice, isNull);
        },
      );

      test('clearNotice drops the one-shot', () {
        ws.pushFrame(_requested(id: 'a'));
        ws.pushFrame(_resolved(id: 'a'));
        expect(notifier.state.notice, isNotNull);
        notifier.clearNotice();
        expect(notifier.state.notice, isNull);
      });
    });

    test('frame with an empty id never enters the queue', () {
      // Passes the production source_topic filter but the notifier guards
      // on id presence (a resolved frame keyed by "" must never clear or
      // open anything).
      ws.pushFrame(_requested(id: ''));
      expect(notifier.state.count, 0);
    });
  });

  group('HTTP catch-up', () {
    test('connection arms initial GET snapshot fetch', () async {
      client.snapshot = {
        'enabled': true,
        'pending': [
          {
            'id': 'snap-1',
            'agent_id': 'codex',
            'session_id': 's9',
            'summary': 'run tests',
            'created_at': '2026-10-19T11:00:00Z',
          },
        ],
      };
      ws.pushConnection(true);
      await Future<void>.delayed(Duration.zero);
      expect(notifier.state.count, 1);
      expect(notifier.state.head!.id, 'snap-1');
      expect(notifier.state.enabled, isTrue);
    });

    test(
      'reconnect refetch drops entries whose resolved frame was missed',
      () async {
        ws.pushFrame(_requested(id: 'stale'));
        await Future<void>.delayed(const Duration(milliseconds: 2));
        client.snapshot = {'enabled': true, 'pending': <Object>[]};
        ws.pushConnection(true);
        await Future<void>.delayed(Duration.zero);
        // 'stale' predates the fetch and is absent from the authoritative
        // snapshot (resolved frame missed while disconnected) -> dropped.
        expect(notifier.state.count, 0);
      },
    );

    test(
      'frame that races an in-flight GET survives the snapshot merge',
      () async {
        client.getGate = Completer<void>();
        ws.pushConnection(true);
        await Future<void>.delayed(const Duration(milliseconds: 2));
        ws.pushFrame(_requested(id: 'racer'));
        client.snapshot = {'enabled': true, 'pending': <Object>[]};
        client.getGate!.complete();
        await Future<void>.delayed(Duration.zero);
        expect(notifier.state.count, 1);
        expect(notifier.state.head!.id, 'racer');
      },
    );

    test('disabled envelope does not wipe live WS-fed entries', () async {
      ws.pushFrame(_requested(id: 'live'));
      client.snapshot = {'enabled': false, 'pending': <Object>[]};
      ws.pushConnection(true);
      await Future<void>.delayed(Duration.zero);
      expect(notifier.state.count, 1);
      expect(notifier.state.head!.id, 'live');
    });
  });

  group('resolve', () {
    test('allow POSTs the resolve route and clears the entry', () async {
      ws.pushFrame(_requested(id: 'a'));
      await notifier.resolve('a', 'allow');
      expect(client.resolves, [
        ['a', 'allow'],
      ]);
      expect(notifier.state.count, 0);
      expect(notifier.state.error, isNull);
    });

    test('deny POSTs decision deny', () async {
      ws.pushFrame(_requested(id: 'a'));
      await notifier.resolve('a', 'deny');
      expect(client.resolves.single, ['a', 'deny']);
      expect(notifier.state.count, 0);
    });

    test('always POSTs decision always (provider passes it through)', () async {
      // acp-always-allow leaf 02: the daemon accepts the third verb; the
      // provider never validates the string locally.
      ws.pushFrame(_requested(id: 'a'));
      await notifier.resolve('a', 'always');
      expect(client.resolves.single, ['a', 'always']);
      expect(notifier.state.count, 0);
    });

    test('requested frame parses the additive options key', () {
      ws.pushFrame(
        _requested(
          options: const [
            {
              'optionId': 'allow-once-1',
              'name': 'Allow once',
              'kind': 'allow_once',
            },
            {
              'optionId': 'always-1',
              'name': 'Always allow in session',
              'kind': 'allow_always',
            },
          ],
        ),
      );
      final head = notifier.state.head!;
      expect(head.options.length, 2);
      expect(head.options.first.optionId, 'allow-once-1');
      expect(head.hasAlwaysOption, isTrue);
    });

    test('frame without options (or only once-kind) never has always', () {
      ws.pushFrame(_requested(id: 'a'));
      expect(notifier.state.head!.options, isEmpty);
      expect(notifier.state.head!.hasAlwaysOption, isFalse);
      ws.pushFrame(
        _requested(
          id: 'b',
          options: const [
            {
              'optionId': 'allow-once-1',
              'name': 'Allow once',
              'kind': 'allow_once',
            },
          ],
        ),
      );
      expect(notifier.state.pending['b']!.hasAlwaysOption, isFalse);
    });

    test('failed resolve keeps the entry and records the error', () async {
      ws.pushFrame(_requested(id: 'a'));
      client.resolveError = StateError('daemon unreachable');
      await notifier.resolve('a', 'allow');
      expect(notifier.state.count, 1);
      expect(notifier.state.error, contains('daemon unreachable'));
      notifier.clearError();
      expect(notifier.state.error, isNull);
    });

    test('echo resolved frame after a local resolve is idempotent', () async {
      ws.pushFrame(_requested(id: 'a'));
      await notifier.resolve('a', 'allow');
      ws.pushFrame(_resolved(id: 'a'));
      expect(notifier.state.count, 0);
    });
  });
}
