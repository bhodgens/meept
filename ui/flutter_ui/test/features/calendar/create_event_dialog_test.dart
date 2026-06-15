import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept_ui/features/calendar/calendar_panel.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/api_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

class _StubApiClient extends ApiClient {
  _StubApiClient() : super(host: 'localhost', port: 8081);

  @override
  Future<Map<String, dynamic>> getCalendarToday() async => {'events': []};

  @override
  Future<void> createCalendarEvent({
    required String summary,
    required DateTime start,
    required DateTime end,
    String? description,
  }) async {}

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
      apiClientProvider.overrideWith((_) => _StubApiClient()),
      websocketProvider.overrideWith((_) => _StubWebSocket()),
    ],
    child: const MaterialApp(
      home: Scaffold(body: CalendarPanel()),
    ),
  );
}

void main() {
  testWidgets('create event dialog exposes start and end pickers',
      (tester) async {
    await tester.pumpWidget(_buildTestApp());
    await tester.pump(); // allow CalendarPanel initState + getCalendarToday
    await tester.pump(const Duration(milliseconds: 100));

    // Open the create-event dialog via the add button.
    await tester.tap(find.byTooltip('create event'));
    await tester.pumpAndSettle();

    // Dialog should be visible.
    expect(find.text('create event'), findsOneWidget);

    // Both 'start' and 'end' section labels should be present.
    expect(find.text('start'), findsOneWidget);
    expect(find.text('end'), findsOneWidget);

    // There should be 4 ElevatedButton.icon buttons (start date, start time,
    // end date, end time) plus the create ElevatedButton.
    expect(find.byType(ElevatedButton), findsNWidgets(5));
  });
}
