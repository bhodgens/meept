import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept_ui/features/reflection/reflection_panel.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

/// Stub SdkApiClient that overrides the reflection methods.
class _ReflectionStubClient extends SdkApiClient {
  _ReflectionStubClient(this._proposals) : super(host: 'localhost', port: 8081);

  final List<Map<String, dynamic>> _proposals;
  int applyCallCount = 0;
  int skipCallCount = 0;

  @override
  Future<List<Map<String, dynamic>>> getReflectionProposalsRaw() async {
    // Return a fresh copy so mutation between calls is isolated
    return List<Map<String, dynamic>>.from(
      _proposals.map((p) => Map<String, dynamic>.from(p)),
    );
  }

  @override
  Future<Map<String, dynamic>> applyReflectionProposal(String id) async {
    applyCallCount++;
    _proposals.removeWhere((p) => p['id'] == id);
    return {'status': 'applied', 'id': id};
  }

  @override
  Future<Map<String, dynamic>> skipReflectionProposal(String id) async {
    skipCallCount++;
    _proposals.removeWhere((p) => p['id'] == id);
    return {'status': 'skipped', 'id': id};
  }

  @override
  Future<Map<String, dynamic>> rememberReflection({
    required String target,
    required String change,
    required String justification,
  }) async {
    return {'status': 'queued', 'target': target};
  }
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

Map<String, dynamic> _proposal({
  String id = 'p1',
  String type = 'skill_create',
  String target = '.meept/skills/x/SKILL.md',
  String justification = 'improves code quality',
  double confidence = 0.8,
  String source = 'turn:s1',
  String status = 'pending',
}) {
  return {
    'id': id,
    'type': type,
    'target': target,
    'change': '# new skill\nsome content',
    'justification': justification,
    'confidence': confidence,
    'source': source,
    'status': status,
    'created_at': '2026-06-26T10:00:00Z',
  };
}

Widget _buildTestApp(_ReflectionStubClient client) {
  return ProviderScope(
    overrides: [
      sdkClientProvider.overrideWith((_) => client),
      websocketProvider.overrideWith((_) => _StubWebSocket()),
    ],
    child: const MaterialApp(
      home: Scaffold(body: ReflectionPanel()),
    ),
  );
}

void main() {
  testWidgets('renders placeholder when no proposals', (tester) async {
    final client = _ReflectionStubClient([]);
    await tester.pumpWidget(_buildTestApp(client));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    expect(find.text('no pending proposals'), findsOneWidget);
    expect(find.text('reflection lessons will appear here'), findsOneWidget);
  });

  testWidgets('renders proposals when present', (tester) async {
    final client = _ReflectionStubClient([_proposal()]);
    await tester.pumpWidget(_buildTestApp(client));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    expect(find.text('.meept/skills/x/skill.md'), findsOneWidget);
    expect(find.text('improves code quality'), findsOneWidget);
  });

  testWidgets('shows propose-only warning for CLAUDE.md target',
      (tester) async {
    final client = _ReflectionStubClient([
      _proposal(target: 'CLAUDE.md'),
    ]);
    await tester.pumpWidget(_buildTestApp(client));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    // Tap "apply"
    await tester.tap(find.text('apply'));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    // SnackBar should mention propose-only / manually
    expect(
      find.textContaining('propose-only'),
      findsOneWidget,
    );
    expect(
      find.textContaining('manually'),
      findsOneWidget,
    );
  });

  testWidgets('applies proposal and refreshes list', (tester) async {
    final client = _ReflectionStubClient([
      _proposal(target: 'x.md', id: 'p1'),
    ]);
    await tester.pumpWidget(_buildTestApp(client));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    // Verify proposal is visible
    expect(find.text('x.md'), findsOneWidget);

    // Tap "apply"
    await tester.tap(find.text('apply'));
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));

    // applyReflectionProposal was called
    expect(client.applyCallCount, 1);

    // List should refresh to empty
    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump(const Duration(milliseconds: 50));
    expect(find.text('no pending proposals'), findsOneWidget);
  });
}
