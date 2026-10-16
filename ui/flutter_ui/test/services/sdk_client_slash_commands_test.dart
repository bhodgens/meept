import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/services/sdk_client.dart';

/// Fake [HttpClientAdapter] that replies per-path with canned JSON bodies.
class _FakeAdapter implements HttpClientAdapter {
  _FakeAdapter(this.routes);

  /// path suffix -> response body (Map or List).
  final Map<String, Object?> routes;
  final capturedPaths = <String>[];
  final capturedBodies = <Map<String, dynamic>>[];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    capturedPaths.add(options.uri.path);
    final data = options.data;
    if (data is Map) {
      capturedBodies.add(Map<String, dynamic>.from(data));
    }
    final body = routes[options.uri.path];
    if (body == null) {
      return ResponseBody.fromString(
        jsonEncode({'error': 'not found'}),
        404,
        headers: {
          Headers.contentTypeHeader: [Headers.jsonContentType],
        },
      );
    }
    return ResponseBody.fromString(
      jsonEncode(body),
      200,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );
  }

  @override
  void close({bool force = false}) {}
}

SdkApiClient _clientWith(_FakeAdapter adapter) {
  final client = SdkApiClient(host: 'localhost', port: 8081);
  client.dio.httpClientAdapter = adapter;
  return client;
}

void main() {
  group('SdkApiClient.listCustomCommands', () {
    test('fetches GET /api/v1/templates and returns entries', () async {
      final adapter = _FakeAdapter({
        '/api/v1/templates': [
          {'name': '/migrate', 'description': 'run db migration'},
          {'name': '/review-pr', 'description': 'review a pull request'},
        ],
      });
      final client = _clientWith(adapter);

      final commands = await client.listCustomCommands();

      expect(adapter.capturedPaths, ['/api/v1/templates']);
      expect(commands, hasLength(2));
      expect(commands[0]['name'], '/migrate');
      expect(commands[1]['description'], 'review a pull request');
    });

    test('tolerates a non-array body as an empty list', () async {
      final adapter = _FakeAdapter({
        '/api/v1/templates': {'unexpected': 'shape'},
      });
      final client = _clientWith(adapter);

      final commands = await client.listCustomCommands();

      expect(commands, isEmpty);
    });
  });

  group('SdkApiClient.stopSession', () {
    test('calls session.stop via bus/call with session_id', () async {
      final adapter = _FakeAdapter({
        '/api/v1/bus/call': {
          'result': {
            'status': 'stopped',
            'session_id': 'sess-1',
            'workers_stopped': ['w1', 'w2'],
          },
        },
      });
      final client = _clientWith(adapter);

      final result = await client.stopSession(sessionId: 'sess-1');

      expect(adapter.capturedPaths, ['/api/v1/bus/call']);
      expect(adapter.capturedBodies.single['method'], 'session.stop');
      expect(
        (adapter.capturedBodies.single['params'] as Map)['session_id'],
        'sess-1',
      );
      expect(result['status'], 'stopped');
      expect(result['workers_stopped'], ['w1', 'w2']);
    });
  });

  group('SdkApiClient.getChildTasks', () {
    test('unwraps the result envelope tasks array', () async {
      final adapter = _FakeAdapter({
        '/api/v1/bus/call': {
          'result': {
            'session_id': 'sess-1',
            'tasks': ['worker-a', 'worker-b'],
          },
        },
      });
      final client = _clientWith(adapter);

      final tasks = await client.getChildTasks(sessionId: 'sess-1');

      expect(
        adapter.capturedBodies.single['method'],
        'session.get_child_tasks',
      );
      expect(tasks, ['worker-a', 'worker-b']);
    });

    test('returns empty list when payload has no tasks key', () async {
      final adapter = _FakeAdapter({
        '/api/v1/bus/call': {
          'result': {'session_id': 'sess-1'},
        },
      });
      final client = _clientWith(adapter);

      final tasks = await client.getChildTasks(sessionId: 'sess-1');

      expect(tasks, isEmpty);
    });
  });

  group('SdkApiClient.getDefaultModel', () {
    test('fetches GET /api/v1/models/default', () async {
      final adapter = _FakeAdapter({
        '/api/v1/models/default': {
          'provider': 'zai',
          'model': 'glm-5.3-flash',
          'full_name': 'zai/glm-5.3-flash',
          'context_limit': 128000,
        },
      });
      final client = _clientWith(adapter);

      final model = await client.getDefaultModel();

      expect(adapter.capturedPaths, ['/api/v1/models/default']);
      expect(model['provider'], 'zai');
      expect(model['full_name'], 'zai/glm-5.3-flash');
    });
  });
}
