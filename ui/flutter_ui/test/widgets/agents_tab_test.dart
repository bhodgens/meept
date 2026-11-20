import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept_ui/features/agents/agents_tab.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/models/ws_events.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';

import '../mocks/mock_websocket_service.dart';

void main() {
  group('AgentsTab tile sizing', () {
    testWidgets('agent tiles are ~250 wide and ~70 tall (redesign)', (
      tester,
    ) async {
      tester.view.devicePixelRatio = 1.0;
      tester.view.physicalSize = const Size(800, 600);
      addTearDown(tester.view.resetPhysicalSize);

      final agents = List.generate(
        3,
        (i) => Agent(id: 'agent-$i', name: 'Agent $i', description: 'd'),
      );

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            agentProvider.overrideWith(
              (ref) => AgentNotifier(sdkClient: _FakeSdkClient(agents: agents)),
            ),
          ],
          child: const MaterialApp(home: Scaffold(body: AgentsTab())),
        ),
      );
      // Allow the post-frame loadAgents() callback to run and settle.
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 100));
      await tester.pump(const Duration(milliseconds: 500));

      final tiles = find.byKey(const ValueKey('agent-tile-agent-0'));
      expect(tiles, findsOneWidget);

      final firstTile = tester.getRect(tiles);
      // Issue #160 follow-up redesign: ~30% wider tiles (maxCrossAxisExtent
      // 290) at roughly half the old height (childAspectRatio 3.6). At the
      // 800px test surface the grid yields ~250x70 tiles.
      expect(firstTile.width, closeTo(250, 60));
      expect(firstTile.height, closeTo(70, 30));
    });

    testWidgets('widening window shows more tiles per row', (tester) async {
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.resetPhysicalSize);

      final agents = List.generate(
        10,
        (i) => Agent(id: 'agent-$i', name: 'Agent $i', description: 'd'),
      );

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            agentProvider.overrideWith(
              (ref) => AgentNotifier(sdkClient: _FakeSdkClient(agents: agents)),
            ),
          ],
          child: const MaterialApp(home: Scaffold(body: AgentsTab())),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 100));
      await tester.pump(const Duration(milliseconds: 500));

      // Keys match the pattern agent-tile-<agent.id> where id is "agent-N".
      final tileKeys = [
        for (var i = 0; i < 10; i++) ValueKey('agent-tile-agent-$i'),
      ];
      List<Rect> rectsForKey(ValueKey key) {
        final f = find.byKey(key);
        return f.evaluate().isNotEmpty ? [tester.getRect(f)] : const [];
      }

      tester.view.physicalSize = const Size(400, 800);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));

      final narrowRects = [for (final k in tileKeys) ...rectsForKey(k)];
      expect(
        narrowRects,
        isNotEmpty,
        reason: 'should find at least one narrow tile',
      );
      final narrowFirstRowY = narrowRects.first.top;
      final narrowCols = narrowRects
          .where((r) => (r.top - narrowFirstRowY).abs() < 1)
          .length;

      tester.view.physicalSize = const Size(1200, 800);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));

      final wideRects = [for (final k in tileKeys) ...rectsForKey(k)];
      expect(
        wideRects,
        isNotEmpty,
        reason: 'should find at least one wide tile',
      );
      final wideFirstRowY = wideRects.first.top;
      final wideCols = wideRects
          .where((r) => (r.top - wideFirstRowY).abs() < 1)
          .length;

      expect(
        wideCols,
        greaterThan(narrowCols),
        reason: 'wider window should show more tiles per row',
      );
    });

    testWidgets('selecting an agent shows its goals pane', (tester) async {
      tester.view.devicePixelRatio = 1.0;
      tester.view.physicalSize = const Size(900, 600);
      addTearDown(tester.view.resetPhysicalSize);

      final agents = [
        const Agent(id: 'agent-0', name: 'coder', description: 'd'),
      ];
      final fake = _FakeSdkClient(agents: agents);

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sdkClientProvider.overrideWith((ref) => fake),
            agentProvider.overrideWith((ref) => AgentNotifier(sdkClient: fake)),
          ],
          child: const MaterialApp(home: Scaffold(body: AgentsTab())),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));

      await tester.tap(find.byKey(const ValueKey('agent-tile-agent-0')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));

      expect(find.text('goals'), findsOneWidget);
      expect(find.text('keep ci green'), findsOneWidget);
      expect(find.textContaining('gate:'), findsOneWidget);
    });
  });

  group('agents tab acp executor display (phase 3 parity)', () {
    Future<void> pumpWithRoster(
      WidgetTester tester,
      List<Map<String, dynamic>> employees,
    ) async {
      tester.view.devicePixelRatio = 1.0;
      tester.view.physicalSize = const Size(900, 600);
      addTearDown(tester.view.resetPhysicalSize);

      final agents = [
        const Agent(id: 'agent-0', name: 'coder', description: 'd'),
      ];
      final fake = _FakeSdkClient(agents: agents, employees: employees);

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sdkClientProvider.overrideWith((ref) => fake),
            agentProvider.overrideWith((ref) => AgentNotifier(sdkClient: fake)),
          ],
          child: const MaterialApp(home: Scaffold(body: AgentsTab())),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));

      await tester.tap(find.byKey(const ValueKey('agent-tile-agent-0')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));
    }

    testWidgets('acp-backed employee tile shows the acp badge and the '
        'goals pane shows the executor detail line', (tester) async {
      await pumpWithRoster(tester, [
        {'id': 'agent-0', 'name': 'coder', 'executor_agent': 'Hermes'},
      ]);

      // Tile badge (parity with the TUI list 'exec' column marker).
      expect(find.text('acp'), findsOneWidget);
      // Detail line, lowercase, (acp) suffix — parity with the TUI
      // detail view and the CLI `agents show` executor line.
      expect(find.text('executor: hermes (acp)'), findsOneWidget);
    });

    testWidgets('employee without executor_agent renders nothing acp', (
      tester,
    ) async {
      await pumpWithRoster(tester, [
        {'id': 'agent-0', 'name': 'coder'},
      ]);

      expect(find.text('acp'), findsNothing);
      expect(
        find.byKey(const ValueKey('agent-executor-agent-0')),
        findsNothing,
      );
    });
  });

  // M17: the unified roster's C1 fields render on the tiles — tier chip,
  // live status chip and daily cost.
  group('agents tab unified roster display (M17)', () {
    testWidgets('roster rows show tier, live status and daily cost', (
      tester,
    ) async {
      tester.view.devicePixelRatio = 1.0;
      tester.view.physicalSize = const Size(1200, 800);
      addTearDown(tester.view.reset);

      final fake = _RosterDisplaySdkClient();
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sdkClientProvider.overrideWith((ref) => fake),
            agentProvider.overrideWith((ref) => AgentNotifier(sdkClient: fake)),
          ],
          child: const MaterialApp(home: Scaffold(body: AgentsTab())),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));
      await tester.pump(const Duration(milliseconds: 500));

      // All three tiers render as tiles.
      expect(
        find.byKey(const ValueKey('agent-tile-roster-agent')),
        findsOneWidget,
      );
      expect(
        find.byKey(const ValueKey('agent-tile-roster-bot')),
        findsOneWidget,
      );
      expect(
        find.byKey(const ValueKey('agent-tile-roster-emp')),
        findsOneWidget,
      );

      // C1 roster line per tile: type / active / cost (leaf 03/02 shape).
      expect(
        find.text('type: agent · active: 0 · cost: \$0.00'),
        findsOneWidget,
      );
      expect(find.text('type: bot · active: 0 · cost: \$0.00'), findsOneWidget);
      expect(
        find.text('type: employee · active: 1 · cost: \$12.34'),
        findsOneWidget,
      );
      expect(
        find.byKey(const ValueKey('agent-roster-roster-emp')),
        findsOneWidget,
      );

      // Live roster status replaces the legacy 'online' presence label.
      expect(find.text('stopped'), findsNWidgets(2));
      expect(find.text('online'), findsNothing);

      // M11: the acp badge rides every row carrying executor_agent —
      // the bot AND the employee (bots carry the key too, roster.go).
      expect(find.text('acp'), findsNWidgets(2));
    });
  });

  // M11: drift / findings / last-run display parity with the TUI agents
  // panel's drift/findings/last run columns (agents_panel.go
  // updateAgentsTable): drift %.2f, findings count, last run HH:MM with
  // 'never' for the absent-key (never-run) shape.
  group('agents tab M11 roster stats display', () {
    Future<void> pumpRoster(WidgetTester tester) async {
      tester.view.devicePixelRatio = 1.0;
      tester.view.physicalSize = const Size(1200, 800);
      addTearDown(tester.view.reset);

      final fake = _RosterDisplaySdkClient();
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sdkClientProvider.overrideWith((ref) => fake),
            agentProvider.overrideWith((ref) => AgentNotifier(sdkClient: fake)),
          ],
          child: const MaterialApp(home: Scaffold(body: AgentsTab())),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));
      await tester.pump(const Duration(milliseconds: 500));
    }

    String hhmm(DateTime utc) {
      final local = utc.toLocal();
      String pad(int n) => n.toString().padLeft(2, '0');
      return '${pad(local.hour)}:${pad(local.minute)}';
    }

    testWidgets('employee tile shows drift/findings/last-run stats line', (
      tester,
    ) async {
      await pumpRoster(tester);

      final lastRun = hhmm(DateTime.parse('2026-11-19T12:00:00Z'));
      expect(
        find.text('drift: 0.25 · findings: 3 · last run: $lastRun'),
        findsOneWidget,
      );
      expect(
        find.byKey(const ValueKey('agent-stats-roster-emp')),
        findsOneWidget,
      );
      // The C1 roster line stays intact alongside the new stats line.
      expect(
        find.text('type: employee · active: 1 · cost: \$12.34'),
        findsOneWidget,
      );
    });

    testWidgets('never-run + zero-drift shape renders never / 0.00 / 0', (
      tester,
    ) async {
      await pumpRoster(tester);

      // All M11 keys zero-omitted on the wire: drift displays an honest
      // 0.00 (no production writer yet), findings 0, last run 'never'.
      // (The registry-agent tile renders the same default shape, so
      // scope the assertion to the idle employee's keyed Text.)
      expect(
        find.byWidgetPredicate(
          (w) =>
              w is Text &&
              w.key == const ValueKey('agent-stats-roster-idle') &&
              w.data == 'drift: 0.00 · findings: 0 · last run: never',
        ),
        findsOneWidget,
      );
      expect(
        find.byKey(const ValueKey('agent-stats-roster-idle')),
        findsOneWidget,
      );
    });

    testWidgets('bot tile shows executor badge and zero-drift stats line', (
      tester,
    ) async {
      await pumpRoster(tester);

      // Bots carry executor_agent + last_invocation only: drift/findings
      // default to 0 and render honestly.
      final lastRun = hhmm(DateTime.parse('2026-11-19T08:30:00Z'));
      expect(
        find.text('drift: 0.00 · findings: 0 · last run: $lastRun'),
        findsOneWidget,
      );
      expect(
        find.byKey(const ValueKey('agent-stats-roster-bot')),
        findsOneWidget,
      );
    });

    testWidgets('registry agent tile shows default stats, no acp badge', (
      tester,
    ) async {
      await pumpRoster(tester);

      // Registry agents carry no M11 keys: defaults render (the TUI shows
      // the same zero columns for them), and no executor badge.
      expect(
        find.text('drift: 0.00 · findings: 0 · last run: never'),
        findsNWidgets(2), // roster-agent + roster-idle
      );
      expect(
        find.byKey(const ValueKey('agent-stats-roster-agent')),
        findsOneWidget,
      );
    });

    testWidgets('goals pane executor line reads the roster model field', (
      tester,
    ) async {
      await pumpRoster(tester);

      // Select the acp-backed employee: the detail line lowercases the
      // id ('Hermes' on the wire) exactly like the TUI executorDetailLine.
      await tester.tap(find.byKey(const ValueKey('agent-tile-roster-emp')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));
      expect(find.text('executor: hermes (acp)'), findsOneWidget);
      expect(
        find.byKey(const ValueKey('agent-executor-roster-emp')),
        findsOneWidget,
      );
    });

    testWidgets('legacy config rows render no M11 stats line', (tester) async {
      tester.view.devicePixelRatio = 1.0;
      tester.view.physicalSize = const Size(1200, 800);
      addTearDown(tester.view.reset);

      // Old-daemon shape: roster 404 -> legacy listAgents rows carry no
      // type/M11 keys; the stats line must not render for them.
      final fake = _FakeSdkClient(
        agents: [const Agent(id: 'agent-0', name: 'coder', description: 'd')],
      );
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            sdkClientProvider.overrideWith((ref) => fake),
            agentProvider.overrideWith((ref) => AgentNotifier(sdkClient: fake)),
          ],
          child: const MaterialApp(home: Scaffold(body: AgentsTab())),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));
      await tester.pump(const Duration(milliseconds: 500));

      expect(find.byKey(const ValueKey('agent-stats-agent-0')), findsNothing);
      expect(find.textContaining('drift:'), findsNothing);
    });
  });

  // M3 (wave 5, plan master.md "GUI polls ... while the agents tab is
  // visible"): the constructor arms no timer; the tab's visibility drives
  // setPollingActive. Pins: no polling until active, activation refreshes
  // immediately + arms the 5s cadence, deactivation kills the timer (the
  // WS subscription and cached state survive so re-entry is instant).
  group('execution runs polling visibility gate (M3)', () {
    testWidgets('polling does not start until active', (tester) async {
      final sdk = _CountingRunsSdkClient();
      final notifier = ExecutionRunsNotifier(
        sdkClient: sdk,
        websocket: MockWebSocketService(),
      );
      addTearDown(notifier.dispose);

      await tester.pumpWidget(const SizedBox());
      expect(notifier.isPollingActive, isFalse);
      // 30s of clock with no activation = zero runs fetches: the hidden/
      // never-opened tab never pays the runs + roster + histogram cadence.
      await tester.pump(const Duration(seconds: 30));
      expect(sdk.runsFetches, 0);
    });

    testWidgets(
      'activation refreshes immediately, deactivation stops the timer',
      (tester) async {
        final sdk = _CountingRunsSdkClient();
        final notifier = ExecutionRunsNotifier(
          sdkClient: sdk,
          websocket: MockWebSocketService(),
        );
        addTearDown(notifier.dispose);
        await tester.pumpWidget(const SizedBox());

        notifier.setPollingActive(true);
        await tester.pump();
        expect(notifier.isPollingActive, isTrue);
        // Activation fetches now (no wait for the first tick), then ticks
        // every 5s.
        expect(sdk.runsFetches, 1);
        await tester.pump(const Duration(seconds: 5));
        expect(sdk.runsFetches, 2);
        await tester.pump(const Duration(seconds: 5));
        expect(sdk.runsFetches, 3);

        // Deactivation: idempotent no-ops aside, the timer is cancelled and
        // the clock no longer produces fetches.
        notifier.setPollingActive(true);
        notifier.setPollingActive(false);
        expect(notifier.isPollingActive, isFalse);
        final frozen = sdk.runsFetches;
        await tester.pump(const Duration(seconds: 30));
        expect(sdk.runsFetches, frozen);

        // Re-entry refreshes immediately again (cached-state instant feel).
        notifier.setPollingActive(true);
        await tester.pump();
        expect(sdk.runsFetches, frozen + 1);

        // Leave the gate open for teardown (dispose also cancels, but be
        // explicit: no pending timer when the widget tree goes away).
        notifier.setPollingActive(false);
      },
    );

    // Widget-level gate: with no Visibility ancestor Visibility.of returns
    // true (bare pump = visible, the pre-M3 behavior these tests rely on);
    // inside an IndexedStack host the hidden copy never polls, showing it
    // activates, hiding it again stops the cadence, and unmounting while
    // visible pauses the notifier that outlives the widget (the
    // SessionInfoOverlay close path).
    testWidgets('tab visibility drives activation, hiding, and unmount pause', (
      tester,
    ) async {
      final sdk = _CountingRunsSdkClient();
      final container = ProviderContainer(
        overrides: [
          sdkClientProvider.overrideWith((ref) => sdk),
          websocketProvider.overrideWith((ref) => MockWebSocketService()),
          agentProvider.overrideWith((ref) => AgentNotifier(sdkClient: sdk)),
        ],
      );
      addTearDown(container.dispose);
      // Hold the provider past the tab's unmount so the dispose-time pause
      // stays observable (a widget-only ProviderScope would auto-dispose).
      final keepAlive = container.listen(executionRunsProvider, (_, __) {});
      addTearDown(keepAlive.close);
      bool pollingActive() =>
          container.read(executionRunsProvider.notifier).isPollingActive;

      Future<void> host({required int index, required bool includeTab}) async {
        await tester.pumpWidget(
          UncontrolledProviderScope(
            container: container,
            child: MaterialApp(
              home: Scaffold(
                body: IndexedStack(
                  index: index,
                  children: [
                    const SizedBox(key: Key('host-first')),
                    if (includeTab) const AgentsTab() else const SizedBox(),
                  ],
                ),
              ),
            ),
          ),
        );
        // Flush the initState post-frame sync.
        await tester.pump();
      }

      // 1. Mounted but hidden (IndexedStack keeps it alive): Visibility.of
      // is false, so the gate stays shut — zero fetches.
      await host(index: 0, includeTab: true);
      expect(pollingActive(), isFalse);
      expect(sdk.runsFetches, 0);
      await tester.pump(const Duration(seconds: 30));
      expect(sdk.runsFetches, 0);

      // 2. Switched to the agents tab: activation refreshes immediately
      // and the 5s cadence arms.
      await host(index: 1, includeTab: true);
      expect(pollingActive(), isTrue);
      final afterShow = sdk.runsFetches;
      expect(afterShow, greaterThanOrEqualTo(1));
      await tester.pump(const Duration(seconds: 5));
      expect(sdk.runsFetches, greaterThan(afterShow));

      // 3. Hidden again: the timer stops, the fetch count freezes.
      await host(index: 0, includeTab: true);
      expect(pollingActive(), isFalse);
      final frozen = sdk.runsFetches;
      await tester.pump(const Duration(seconds: 30));
      expect(sdk.runsFetches, frozen);

      // 4. Visible, then unmounted while active (overlay close): dispose
      // pauses the surviving notifier so no orphan cadence keeps polling.
      await host(index: 1, includeTab: true);
      expect(pollingActive(), isTrue);
      await tester.pump();
      await host(index: 0, includeTab: false);
      expect(pollingActive(), isFalse);
      final afterUnmount = sdk.runsFetches;
      await tester.pump(const Duration(seconds: 30));
      expect(sdk.runsFetches, afterUnmount);
    });
  });

  // L3 (TUI parity, commit e9fe1926 H1): mergeLiveRuns identity is the
  // store id when both sides carry one, else the composite key with the
  // live timestamp truncated to the store's second precision. A live row
  // strictly newer than the server page survives the poll replace; a live
  // row the server now carries is dropped exactly once.
  group('execution runs live merge (L3)', () {
    ExecutionResultEvent serverRow({
      required int id,
      required String agent,
      required DateTime createdAt,
      String status = 'completed',
      String tier = 'employee',
    }) {
      return ExecutionResultEvent(
        agentId: agent,
        status: status,
        id: id,
        storedTier: tier,
        createdAt: createdAt,
      );
    }

    ExecutionResultEvent liveRow({
      required String agent,
      required DateTime timestamp,
      String status = 'completed',
    }) {
      return ExecutionResultEvent(
        agentId: agent,
        status: status,
        sourceTopic: ExecutionResultEvent.sourceTopicEmployee,
        timestamp: timestamp,
      );
    }

    test('keeps a live row strictly newer than the server page', () {
      final t1 = DateTime.utc(2026, 11, 19, 12, 0, 0);
      final server = [serverRow(id: 5, agent: 'a', createdAt: t1)];
      // Different composite (other agent) + strictly newer: projector-
      // lagged live row, must NOT be dropped by the poll replace.
      final live = [
        liveRow(
          agent: 'b',
          timestamp: t1.add(const Duration(milliseconds: 500)),
        ),
      ];
      final merged = mergeLiveRuns(server, live);
      expect(merged.length, 2);
      expect(merged.first.agentId, 'b');
      expect(merged.last.id, 5);
    });

    test('drops a live row the server page now carries (ns vs sec + id)', () {
      // e9fe1926 H1 shape: live frame carries nanosecond publish time, the
      // persisted row the truncated second + its store id. One execution
      // must render exactly ONE row.
      final live = [
        liveRow(
          agent: 'coder',
          timestamp: DateTime.utc(2026, 11, 19, 12, 0, 0, 0, 500),
        ),
      ];
      final server = [
        serverRow(
          id: 7,
          agent: 'coder',
          createdAt: DateTime.utc(2026, 11, 19, 12),
        ),
      ];
      final merged = mergeLiveRuns(server, live);
      expect(merged.length, 1);
      expect(merged.single.id, 7);
    });

    test(
      'distinct ids survive; same-composite drop, fresh-key equal-second keep',
      () {
        final base = DateTime.utc(2026, 11, 19, 12);
        final server = [
          serverRow(id: 2, agent: 'a', createdAt: base),
          serverRow(id: 1, agent: 'b', createdAt: base),
        ];
        final live = [
          // Same second + same composite as server id 2 -> dropped once the
          // server carries it (the e9fe1926 H1 window).
          liveRow(
            agent: 'a',
            timestamp: base.add(const Duration(milliseconds: 250)),
          ),
          // Same second, DIFFERENT composite (agent c, failed): the time gate
          // is strict After(maxServer), so it passes and no identity match
          // drops it -> preserved (exact TUI mergeLiveRuns behavior).
          liveRow(
            agent: 'c',
            timestamp: base.add(const Duration(milliseconds: 900)),
            status: 'failed',
          ),
          // Strictly newer second, fresh composite -> preserved.
          liveRow(agent: 'd', timestamp: base.add(const Duration(seconds: 1))),
        ];
        final merged = mergeLiveRuns(server, live);
        expect(merged.map((r) => r.agentId).toList(), ['c', 'd', 'a', 'b']);
        expect(merged[2].id, 2);
        expect(merged[3].id, 1);
      },
    );

    test('empty server page preserves every timestamped live row', () {
      final live = [
        liveRow(agent: 'a', timestamp: DateTime.utc(2026, 11, 19, 12, 0, 2)),
        liveRow(agent: 'b', timestamp: DateTime.utc(2026, 11, 19, 12, 0, 1)),
        // Untimestamped live row: no identity clock, dropped (TUI parity:
        // zero Timestamp skips).
        liveRow(agent: 'c', timestamp: DateTime.utc(1970)),
      ];
      // effectiveTime null only when BOTH createdAt and timestamp are
      // absent; the epoch row above still has a time, so add a real null.
      const noTime = ExecutionResultEvent(agentId: 'e', status: 'completed');
      final merged = mergeLiveRuns(const [], [...live, noTime]);
      expect(merged.length, 3);
      expect(merged.map((r) => r.agentId), ['a', 'b', 'c']);
    });
  });
}

/// Roster-first client for the M17 display pins: agents.roster answers
/// one row per tier with live status and cost, plus the additive M11 keys
/// on the employee row (executor/drift/findings/last-run); the legacy list
/// must not be consulted.
class _RosterDisplaySdkClient extends _FakeSdkClient {
  _RosterDisplaySdkClient() : super(agents: const []);

  @override
  Future<Map<String, dynamic>> getAgentsRoster() async => {
    'agents': [
      {
        'id': 'roster-agent',
        'name': 'dispatcher',
        'type': 'agent',
        'status': 'running',
        'active_count': 0,
        'daily_cost_cents': 0,
        'role': 'dispatcher',
        'description': 'routes tasks',
        'enabled': true,
        // Registry agents carry no M11 keys.
      },
      {
        'id': 'roster-bot',
        'name': 'news bot',
        'type': 'bot',
        'status': 'stopped',
        'active_count': 0,
        'daily_cost_cents': 0,
        'role': '',
        'description': 'posts headlines',
        'enabled': true,
        // Bots carry executor_agent + last_invocation only.
        'executor_agent': 'hermes',
        'last_invocation': '2026-11-19T08:30:00Z',
      },
      {
        'id': 'roster-emp',
        'name': 'coder',
        'type': 'employee',
        'status': 'running',
        'active_count': 1,
        'daily_cost_cents': 1234,
        'role': 'engineer',
        'description': 'writes code',
        'enabled': true,
        // Employees carry all four M11 keys.
        'executor_agent': 'Hermes',
        'drift_score': 0.25,
        'findings_count': 3,
        'last_invocation': '2026-11-19T12:00:00Z',
      },
      {
        'id': 'roster-idle',
        'name': 'idle emp',
        'type': 'employee',
        'status': 'stopped',
        'active_count': 0,
        'daily_cost_cents': 0,
        'role': '',
        'description': 'never ran',
        'enabled': true,
        // Never-run / zero-drift shape: all M11 keys zero-omitted.
      },
    ],
  };

  @override
  Future<List<Map<String, dynamic>>> listEmployees() async => const [];
}

/// Counting runs-poll client (M3 pins): records every getExecutionRuns
/// call so the tests can assert the visibility gate actually gates the
/// cadence. Roster 404s (no histogram fan-out), execution fetches succeed.
class _CountingRunsSdkClient extends _FakeSdkClient {
  _CountingRunsSdkClient() : super(agents: const []);

  int runsFetches = 0;

  @override
  Future<Map<String, dynamic>> getExecutionRuns({
    String? agentId,
    int? limit,
    String? since,
  }) async {
    runsFetches++;
    return {'runs': <Map<String, dynamic>>[], 'total': 0};
  }

  @override
  Future<List<Map<String, dynamic>>> listAgents() async => const [];

  @override
  Future<List<Map<String, dynamic>>> listEmployees() async => const [];
}

/// Fake [SdkApiClient] that returns pre-built agent JSON for [listAgents].
///
/// Implements the full [SdkApiClient] surface via [noSuchMethod] so the
/// test only needs to override the agent-listing path.
class _FakeSdkClient implements SdkApiClient {
  final List<Agent> agents;

  /// Employee-roster rows returned by listEmployees (acp-employees phase
  /// 3 display parity): entries carrying executor_agent mark their id as
  /// acp-backed in the agents tab.
  final List<Map<String, dynamic>> employees;

  _FakeSdkClient({required this.agents, this.employees = const []});

  @override
  Future<List<Map<String, dynamic>>> listEmployees() async => employees;

  @override
  Future<List<Map<String, dynamic>>> listAgents() async {
    return agents
        .map(
          (a) => <String, dynamic>{
            'id': a.id,
            'name': a.name,
            'description': a.description,
          },
        )
        .toList();
  }

  @override
  Future<List<Map<String, dynamic>>> listEmployeeGoals(
    String employeeId,
  ) async {
    if (employeeId == 'agent-0') {
      return [
        {
          'id': 'goal_keep_ci',
          'title': 'keep ci green',
          'health': 'healthy',
          'active_plan_id': '',
          'gate': {'command': 'go test ./...'},
        },
      ];
    }
    return [];
  }

  @override
  Future<Map<String, dynamic>> getExecutionRuns({
    String? agentId,
    int? limit,
    String? since,
  }) async => {'runs': <Map<String, dynamic>>[], 'total': 0};

  @override
  Future<Map<String, dynamic>> getExecutionHistogram(String agentId) async => {
    'agent_id': agentId,
    'cells': List<int>.filled(8, 0),
  };

  @override
  Future<Map<String, dynamic>> getAgentsRoster() async =>
      // Old-daemon shape: the roster route 404s, so loadAgents falls back
      // to the legacy listAgents payload these tests seed.
      throw SdkApiException(message: 'not found', statusCode: 404);

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}
