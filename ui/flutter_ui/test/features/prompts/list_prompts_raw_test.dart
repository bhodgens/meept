// Exercises the REAL SdkApiClient.listPromptsRaw() through a mock Dio
// adapter. The panel widget test stubs listPromptsRaw() out entirely, so this
// is the only place the real request/parse boundary is covered.
import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/prompts/prompt_models.dart';
import 'package:meept_ui/services/sdk_client.dart';

/// Minimal Dio adapter that always returns a canned response, so the real
/// client's request/parse path runs without any network or TLS.
class _FakeAdapter implements HttpClientAdapter {
  _FakeAdapter(this.statusCode, this.body, {this.contentType = 'application/json'});

  final int statusCode;
  final String body;
  final String contentType;

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    return ResponseBody.fromString(
      body,
      statusCode,
      headers: {
        Headers.contentTypeHeader: [contentType],
      },
    );
  }

  @override
  void close({bool force = false}) {}
}

SdkApiClient _clientWith(
  int status,
  String body, {
  String contentType = 'application/json',
}) {
  final client = SdkApiClient(host: 'localhost', port: 8081);
  client.dio.httpClientAdapter =
      _FakeAdapter(status, body, contentType: contentType);
  return client;
}

const _onePromptEnvelope = '{"prompts":[{"name":"planner/decompose.md",'
    '"tier":"bundled","source_path":"config/prompts/planner/decompose.md",'
    '"modified":"2026-08-30T15:32:01-06:00"}]}';

void main() {
  test('parses the daemon {"prompts": [...]} envelope', () async {
    final client = _clientWith(200, _onePromptEnvelope);

    final raw = await client.listPromptsRaw();
    expect(raw, hasLength(1));
    final summary = PromptSummary.fromJson(raw.first);
    expect(summary.name, 'planner/decompose.md');
    expect(summary.tier, 'bundled');
    expect(summary.sourcePath, 'config/prompts/planner/decompose.md');
    expect(summary.modified, isNotNull);
    client.dispose();
  });

  test('accepts a bare top-level array defensively', () async {
    final client = _clientWith(
      200,
      '[{"name":"a.md","tier":"user","source_path":"x/a.md"}]',
    );

    final raw = await client.listPromptsRaw();
    expect(raw, hasLength(1));
    expect(PromptSummary.fromJson(raw.first).name, 'a.md');
    client.dispose();
  });

  test('empty prompts array is a valid empty result (not an error)', () async {
    final client = _clientWith(200, '{"prompts":[]}');
    expect(await client.listPromptsRaw(), isEmpty);
    client.dispose();
  });

  test('missing "prompts" key raises instead of returning a silent []',
      () async {
    final client = _clientWith(200, '{}');
    await expectLater(
      client.listPromptsRaw(),
      throwsA(
        isA<SdkApiException>()
            .having((e) => e.message, 'message', contains('missing')),
      ),
    );
    client.dispose();
  });

  test('wrong-typed "prompts" value raises instead of being dropped',
      () async {
    final client = _clientWith(200, '{"prompts":{"a":1}}');
    await expectLater(
      client.listPromptsRaw(),
      throwsA(isA<SdkApiException>()),
    );
    client.dispose();
  });

  test('503 surfaces as SdkApiException carrying the daemon text', () async {
    final client = _clientWith(
      503,
      jsonEncode({'error': 'prompt service not available'}),
    );

    await expectLater(
      client.listPromptsRaw(),
      throwsA(
        isA<SdkApiException>()
            .having((e) => e.statusCode, 'statusCode', 503)
            .having((e) => e.message, 'message',
                contains('prompt service not available')),
      ),
    );
    client.dispose();
  });

  test('404 with a non-json body still surfaces as SdkApiException', () async {
    final client = _clientWith(404, '404 page not found',
        contentType: 'text/plain');

    await expectLater(
      client.listPromptsRaw(),
      throwsA(
        isA<SdkApiException>().having((e) => e.statusCode, 'statusCode', 404),
      ),
    );
    client.dispose();
  });
}
