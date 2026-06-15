// Verifies the S7-H-Mem fix: the "search or browse memories" placeholder
// is reachable (not dead UX). When the panel loads with no memories, the
// placeholder renders instead of being masked by an unreachable branch.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept_ui/features/memory/memory_panel.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/api_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

class _EmptyMemoriesApiClient extends ApiClient {
  _EmptyMemoriesApiClient() : super(host: 'localhost', port: 8081);

  @override
  Future<List<Map<String, dynamic>>> getRecentMemories({int limit = 10}) async {
    return [];
  }

  @override
  Future<List<Map<String, dynamic>>> queryMemory({
    required String query,
    int limit = 10,
    String? category,
  }) async {
    return [];
  }

  @override
  Future<T> get<T>(
    String path, {
    Map<String, dynamic>? queryParameters,
  }) async =>
      <String, dynamic>{} as T;

  @override
  Future<T> post<T>(
    String path, {
    dynamic data,
    Map<String, dynamic>? queryParameters,
  }) async =>
      <String, dynamic>{} as T;

  @override
  Future<T> put<T>(
    String path, {
    dynamic data,
    Map<String, dynamic>? queryParameters,
  }) async =>
      <String, dynamic>{} as T;

  @override
  Future<T> delete<T>(String path) async => <String, dynamic>{} as T;
}

class _StubWebSocket extends WebSocketService {
  _StubWebSocket() : super(host: 'localhost', port: 8081);

  @override
  Future<void> connect({String? path}) async {}
  @override
  void disconnect() {}
  @override
  void send(Map<String, dynamic> message) {}
}

Widget _buildTestApp() {
  return ProviderScope(
    overrides: [
      apiClientProvider.overrideWith((_) => _EmptyMemoriesApiClient()),
      websocketProvider.overrideWith((_) => _StubWebSocket()),
    ],
    child: const MaterialApp(
      home: Scaffold(body: MemoryPanel()),
    ),
  );
}

void main() {
  testWidgets('renders placeholder when no memories and not searched',
      (tester) async {
    await tester.pumpWidget(_buildTestApp());
    // Allow initState + getRecentMemories to complete.
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    // The placeholder should be visible because no memories exist and
    // _hasSearched remains false (loading recent != searching).
    expect(find.text('search or browse memories'), findsOneWidget);
  });
}
