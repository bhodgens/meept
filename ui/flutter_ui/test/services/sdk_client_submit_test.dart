import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_client/meept_client.dart' as sdk;
import 'package:meept_ui/services/sdk_client.dart';

/// Fake [HttpClientAdapter] that records the request path and replies with
/// a canned JSON body — lets tests assert the exact endpoint URL the
/// migrated chat path hits (the old /api/v1/chat must NOT be called by the
/// submit path) without touching the network.
class _FakeAdapter implements HttpClientAdapter {
  _FakeAdapter(this.responseBody);

  final Map<String, dynamic> responseBody;
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
    return ResponseBody.fromString(
      jsonEncode(responseBody),
      200,
      headers: {Headers.contentTypeHeader: [Headers.jsonContentType]},
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
  group('ChatSubmitAck.fromParse', () {
    test('parses a full accepted ack', () {
      final ack = ChatSubmitAck.fromParse({
        'turn_id': 'turn-123',
        'conversation_id': 'conv-456',
        'session_id': 'session-abc',
        'accepted': true,
        'note': 'accepted; result arrives via turn.terminal',
      });
      expect(ack.turnId, 'turn-123');
      expect(ack.conversationId, 'conv-456');
      expect(ack.sessionId, 'session-abc');
      expect(ack.accepted, isTrue);
      expect(ack.note, contains('turn.terminal'));
    });

    test('parses a rejected ack with empty turn id and note', () {
      final ack = ChatSubmitAck.fromParse({
        'turn_id': '',
        'conversation_id': 'conv-456',
        'session_id': '',
        'accepted': false,
        'note': 'message is required',
      });
      expect(ack.accepted, isFalse);
      expect(ack.turnId, isEmpty);
      expect(ack.note, 'message is required');
    });

    test('defaults missing keys defensively', () {
      final ack = ChatSubmitAck.fromParse({});
      expect(ack.turnId, isEmpty);
      expect(ack.accepted, isFalse);
      expect(ack.note, isEmpty);
    });
  });

  group('submitTurn / submitChat endpoint contract', () {
    test('POSTs to /api/v1/chat/submit — never the legacy sync endpoint',
        () async {
      final adapter = _FakeAdapter({
        'turn_id': 'turn-1',
        'conversation_id': 'conv-1',
        'session_id': 'sess-1',
        'accepted': true,
        'note': 'accepted; result arrives via turn.terminal',
      });
      final client = _clientWith(adapter);

      final ack = await client.submitTurn(
        message: 'hello',
        conversationId: 'sess-1',
      );

      // The migrated path must hit ONLY the submit endpoint. The old
      // /api/v1/chat still exists during migration but must not be called.
      expect(adapter.capturedPaths, ['/api/v1/chat/submit']);
      expect(ack.accepted, isTrue);
      expect(ack.turnId, 'turn-1');
      client.dispose();
    });

    test('request body carries the chat payload keys', () async {
      final adapter = _FakeAdapter({'turn_id': 't', 'accepted': true});
      final client = _clientWith(adapter);

      await client.submitTurn(
        message: 'hello',
        conversationId: 'sess-1',
        agentId: 'coder',
        parts: [
          {
            'type': 'image_url',
            'image_url': {'url': 'data:image/png;base64,x'},
          },
        ],
      );

      final body = adapter.capturedBodies.single;
      expect(body['message'], 'hello');
      expect(body['conversation_id'], 'sess-1');
      expect(body['agent_id'], 'coder');
      expect(body['source_client'], 'flutter_ui');
      expect(body['parts'], hasLength(1));
      client.dispose();
    });

    test('submitChat (typed ChatRequest) posts the same endpoint', () async {
      final adapter = _FakeAdapter({'turn_id': 't', 'accepted': false, 'note': 'nope'});
      final client = _clientWith(adapter);

      final ack = await client.submitChat(
        sdk.ChatRequest(
          (b) => b
            ..message = 'hi'
            ..conversationId = 'sess-1',
        ),
      );

      expect(adapter.capturedPaths, ['/api/v1/chat/submit']);
      expect(ack.accepted, isFalse);
      expect(ack.note, 'nope');
      client.dispose();
    });

    test('connection failures surface as SdkApiException', () async {
      // Port 8099: nothing listens there; the real adapter fails fast and
      // _handleError must map it to SdkApiException like the sync path.
      final client = SdkApiClient(host: 'localhost', port: 8099);
      await expectLater(
        client.submitTurn(message: 'x', conversationId: 'sess-1'),
        throwsA(isA<SdkApiException>()),
      );
      client.dispose();
    });
  });
}
