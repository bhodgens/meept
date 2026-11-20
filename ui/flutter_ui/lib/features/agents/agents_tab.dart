import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'dart:async';
import '../../core/constants.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../widgets/background_image.dart';
import '../../providers/providers.dart';
import '../../providers/tab_activation_provider.dart'
    show keyboardFocusProvider;
import '../../models/api_models.dart';
// agent_provider symbols come via providers.dart (exports agent_provider).
import 'agent_activity_widgets.dart';
import 'eval_runs_panel.dart';
import 'execution_runs_panel.dart';
import 'facts_panel.dart';
import 'quota_status.dart';

/// Agents tab - displays all available agents.
///
/// This is the Flutter full-window agents view. The menubar app
/// (menubar/MeeptMenuBar/Views/AgentsView.swift) is a separate native
/// Swift implementation for the macOS menu bar. Both surfaces query
/// the same REST API (/api/v1/agents/*) but serve different UX roles:
/// - Swift menubar tab: quick status, approve plans
/// - Flutter full window: detailed constitution editing, audit review,
///   plus pause/resume and status chips (GUI parity with the TUI agents
///   panel — previously deferred to the menubar app, now wired here over
///   the same POST /api/v1/agents/{id}/pause|resume endpoints).
///
/// The widget shows the employee grid plus a goals pane
/// (health, gate command, approve/reject) when an agent is selected.
class AgentsTab extends ConsumerStatefulWidget {
  const AgentsTab({super.key});

  @override
  ConsumerState<AgentsTab> createState() => _AgentsTabState();
}

class _AgentsTabState extends ConsumerState<AgentsTab> {
  /// Keyboard cursor into the agents grid. Selection itself lives in
  /// activeAgentProvider (single source of truth); this index is kept in
  /// sync with it so the first tile never stays highlighted after another
  /// tile is tapped (issue #160 follow-up selection bug). -1 = no
  /// keyboard cursor until the user arrows or taps.
  int _selectedIndex = -1;
  final _gridFocusNode = FocusNode();
  List<EmployeeGoal> _goals = const [];
  bool _goalsLoading = false;
  String? _goalsError;
  String? _goalsForId;
  Timer? _quotaTimer;

  /// Agent ids paused (or resumed) from THIS client this session. The
  /// agents.list payload carries no status field (it returns employee
  /// definitions without runtime state), so a pause/resume is recorded
  /// here until the daemon surfaces status in the list payload — the same
  /// reason the TUI seeds its status from events.
  final Set<String> _pausedLocally = {};

  // Worst goal health across the loaded goals (worst-health-wins):
  // broken > at_risk > healthy > unknown. Drives the aggregate health
  // badge on the goals pane header.
  String get _worstGoalHealth {
    var worst = 'unknown';
    var worstRank = 0;
    for (final g in _goals) {
      final rank = switch (g.health) {
        'broken' => 3,
        'at_risk' => 2,
        'healthy' => 1,
        _ => 0,
      };
      if (rank > worstRank) {
        worstRank = rank;
        worst = g.health;
      }
    }
    return worst;
  }

  Color get _goalsHealthColor => switch (_worstGoalHealth) {
    'healthy' => CyberpunkColors.greenSuccess,
    'at_risk' => CyberpunkColors.orangePrimary,
    'broken' => CyberpunkColors.redAlert,
    _ => CyberpunkColors.midGray,
  };

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      ref.read(agentProvider.notifier).loadAgents();
      _initQuotaListener();
      // M3: execution-run polling is visibility-gated. Activate on mount;
      // didChangeDependencies re-checks Visibility.of for hosts that keep
      // this tab alive while hidden (SessionInfoOverlay's IndexedStack).
      _syncExecutionPolling();
    });
    _startQuotaTimer();
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _syncExecutionPolling();
  }

  /// Last synced runs notifier. dispose() must NOT touch `ref`:
  /// flutter_riverpod 2.x asserts not-disposed inside every ref member
  /// (consumer.dart _assertNotDisposed), and the element is already
  /// inactive when State.dispose runs — ref.exists() in dispose threw
  /// StateError and aborted teardown (pending timers). Holding the
  /// instance instead also means teardown never resurrects the provider.
  ExecutionRunsNotifier? _runsNotifier;

  /// Drives the execution-runs poll cadence from this tab's real visibility
  /// (M3, plan contract: poll while the agents tab is visible). The main
  /// window unmounts inactive tabs (TabContent switch); the session info
  /// overlay keeps all four alive in an IndexedStack — IndexedStack wraps
  /// every child in a Visibility(maintainState: true), where Visibility.of
  /// reports the hidden ones. With NO Visibility ancestor (plain widget
  /// tests pump the tab directly) Visibility.of returns true by contract,
  /// so a bare-mounted tab polls like before.
  void _syncExecutionPolling() {
    if (!mounted) return;
    // read(notifier) creates the provider when it is not yet alive — the
    // ensure-then-use the exists() guard was reaching for, minus the
    // dispose-time ref.
    final notifier = ref.read(executionRunsProvider.notifier);
    _runsNotifier = notifier;
    notifier.setPollingActive(Visibility.of(context));
  }

  @override
  void dispose() {
    // M3: leaving the tab stops the runs/roster/histogram poll cadence.
    // The notifier itself may outlive this widget (other hosts), so pause
    // rather than dispose; the next visible mount re-activates + refreshes.
    _runsNotifier?.setPollingActive(false);
    _runsNotifier = null;
    _gridFocusNode.dispose();
    _quotaTimer?.cancel();
    _quotaTimer = null;
    super.dispose();
  }

  void _startQuotaTimer() {
    _quotaTimer?.cancel();
    _quotaTimer = Timer.periodic(const Duration(seconds: 1), (_) {
      // Only rebuild if there are active quota episodes.
      final episodes = ref.read(agentProvider).quotaEpisodes;
      if (episodes.isNotEmpty && mounted) {
        setState(() {});
      }
    });
  }

  void _initQuotaListener() {
    final ws = ref.read(websocketProvider);
    ws.messageStream
        .where((m) {
          final type = m['type'] as String?;
          return type == 'agent_progress' && m['to'] != null;
        })
        .listen((msg) {
          // AgentProgress.fromJson is null-safe by design; a malformed payload
          // must never crash the listener. Guard with try/catch anyway.
          try {
            final progress = AgentProgress.fromJson(msg);
            final quota = progress.quota;
            if (quota == null) return;
            ref
                .read(agentProvider.notifier)
                .handleQuotaEvent(
                  agentId: quota.agentId,
                  to: quota.to,
                  unblockAt: quota.unblockAt,
                  fallbackModel: quota.fallbackModel,
                  escalation: quota.escalation,
                  // Parked-turn class (leaf 04); null-safe for legacy events.
                  waitClass: quota.waitClass,
                  // Park lifecycle reason (I-M8: throttle_give_up etc.); null-safe
                  // for legacy events.
                  reason: quota.reason,
                );
          } catch (_) {
            // malformed quota payload — ignore without crash
          }
        });
  }

  /// Handle keyboard navigation for the agents grid.
  KeyEventResult _handleKey(FocusNode node, KeyEvent event) {
    if (event is KeyDownEvent && node.hasFocus) {
      final agentState = ref.read(agentProvider);
      final count = agentState.agents.length;
      if (count == 0) return KeyEventResult.ignored;

      // Seed the cursor from the current selection so arrow keys move
      // FROM the tapped tile, not from a stale index.
      if (_selectedIndex < 0 || _selectedIndex >= count) {
        final selected = ref.read(activeAgentProvider);
        final idx = selected == null
            ? -1
            : agentState.agents.indexWhere((a) => a.id == selected.id);
        _selectedIndex = idx >= 0 ? idx : 0;
      }

      // Calculate columns based on grid delegate (maxCrossAxisExtent: 290)
      final screenWidth = MediaQuery.of(node.context!).size.width;
      final cols = (screenWidth / 290).floor().clamp(1, 4);

      int? next;
      if (event.logicalKey == LogicalKeyboardKey.arrowDown) {
        next = (_selectedIndex + cols).clamp(0, count - 1);
      } else if (event.logicalKey == LogicalKeyboardKey.arrowUp) {
        next = (_selectedIndex - cols).clamp(0, count - 1);
      } else if (event.logicalKey == LogicalKeyboardKey.arrowRight) {
        next = (_selectedIndex + 1).clamp(0, count - 1);
      } else if (event.logicalKey == LogicalKeyboardKey.arrowLeft) {
        next = (_selectedIndex - 1).clamp(0, count - 1);
      }
      if (next != null) {
        setState(() {
          _selectedIndex = next!;
        });
        // Keyboard selection drives the same provider as taps: one
        // highlight source, goals pane follows the cursor.
        final agent = agentState.agents[_selectedIndex];
        ref.read(activeAgentProvider.notifier).state = agent;
        _loadGoals(agent.id);
        return KeyEventResult.handled;
      }
    }
    return KeyEventResult.ignored;
  }

  Future<void> _loadGoals(String employeeId) async {
    setState(() {
      _goalsLoading = true;
      _goalsError = null;
      _goalsForId = employeeId;
    });
    try {
      final raw = await ref
          .read(sdkClientProvider)
          .listEmployeeGoals(employeeId);
      final goals = raw.map(EmployeeGoal.fromJson).toList(growable: false);
      if (!mounted || _goalsForId != employeeId) return;
      setState(() {
        _goals = goals;
        _goalsLoading = false;
      });
    } catch (e) {
      if (!mounted || _goalsForId != employeeId) return;
      setState(() {
        _goals = const [];
        _goalsLoading = false;
        _goalsError = e.toString();
      });
    }
  }

  /// Pause or resume [agent] via POST /api/v1/agents/{id}/pause|resume
  /// (internal/employee/api_handlers.go:68-69), then refresh the agent
  /// list so the status chip reflects the new state.
  Future<void> _togglePause(Agent agent, {required bool pause}) async {
    final client = ref.read(sdkClientProvider);
    try {
      if (pause) {
        await client.pauseAgent(agent.id);
        _pausedLocally.add(agent.id);
      } else {
        await client.resumeAgent(agent.id);
        _pausedLocally.remove(agent.id);
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(pause ? 'pause failed: $e' : 'resume failed: $e'),
          ),
        );
      }
      return;
    }
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          pause
              ? 'paused: ${agent.name.toLowerCase()}'
              : 'resumed: ${agent.name.toLowerCase()}',
        ),
        duration: const Duration(milliseconds: 2500),
      ),
    );
    await ref.read(agentProvider.notifier).loadAgents();
  }

  @override
  Widget build(BuildContext context) {
    final agentState = ref.watch(agentProvider);
    final activeAgent = ref.watch(activeAgentProvider);

    return BackgroundImage(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Expanded(child: _buildAgentColumn(agentState)),
            if (activeAgent != null) ...[
              const SizedBox(width: 12),
              SizedBox(
                width: 300,
                // The WHOLE sidebar scrolls as one column (issue #160
                // follow-up): goals, eval runs, executions and memory facts
                // are all reachable at any viewport height. This replaces
                // the nested Expanded/SingleChildScrollView split that
                // overflowed by 13px at 900x600. Panels render their lists
                // with shrinkWrap inside the scroll view; the goals list
                // keeps a bounded height so one long goal list cannot
                // push the other panels off-screen.
                child: SingleChildScrollView(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      _buildGoalsPane(activeAgent),
                      const SizedBox(height: 12),
                      const EvalRunsPanel(),
                      const SizedBox(height: 12),
                      const ExecutionRunsPanel(),
                      const SizedBox(height: 12),
                      const FactsPanel(),
                      const SizedBox(height: 12),
                    ],
                  ),
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _buildAgentColumn(AgentState agentState) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Text(
              'agents',
              style: CyberpunkTypography.headlineMedium.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
            const Spacer(),
            IconButton(
              tooltip: 'refresh employees',
              icon: const Icon(Icons.refresh, size: 18),
              color: CyberpunkColors.orangePrimary,
              onPressed: () {
                ref.read(agentProvider.notifier).loadAgents();
              },
            ),
          ],
        ),
        const SizedBox(height: 16),
        if (agentState.isLoading)
          const Expanded(child: Center(child: CircularProgressIndicator()))
        else if (agentState.error != null)
          Expanded(
            child: Center(
              child: Column(
                children: [
                  SizedBox(
                    width: 280,
                    child: _AgentErrorBanner(message: agentState.error!),
                  ),
                  const SizedBox(height: 12),
                  FilledButton.tonal(
                    onPressed: () =>
                        ref.read(agentProvider.notifier).loadAgents(),
                    child: const Text(
                      'retry',
                      style: CyberpunkTypography.bodySmall,
                    ),
                  ),
                ],
              ),
            ),
          )
        else if (agentState.agents.isEmpty)
          const Expanded(child: Center(child: Text('no agents available')))
        else
          Expanded(
            child: Focus(
              onFocusChange: (hasFocus) {
                if (hasFocus) {
                  ref.read(keyboardFocusProvider.notifier).setFocusedPane(0);
                }
              },
              onKeyEvent: _handleKey,
              child: GridView.builder(
                // Issue #160 follow-up: ~30% wider tiles (225 -> 290) at
                // roughly half the old height (aspect 0.87 -> 3.6). The
                // compact card carries a 2x icon, the 8-bit token
                // histogram and the concurrent-run counter.
                gridDelegate: const SliverGridDelegateWithMaxCrossAxisExtent(
                  maxCrossAxisExtent: 290,
                  crossAxisSpacing: 8,
                  mainAxisSpacing: 8,
                  childAspectRatio: 3.6,
                ),
                itemCount: agentState.agents.length,
                itemBuilder: (context, index) {
                  final agent = agentState.agents[index];
                  final selected = ref.watch(activeAgentProvider);
                  final isSelected = selected?.id == agent.id;
                  final quotaState = agentState.quotaEpisodes[agent.id];
                  return _buildAgentCard(agent, index, isSelected, quotaState);
                },
              ),
            ),
          ),
      ],
    );
  }

  Widget _buildAgentCard(
    Agent agent,
    int index,
    bool isSelected,
    AgentQuotaState? quotaState,
  ) {
    // acp executor reference straight off the roster row's M11
    // executor_agent key (Agent.executorAgent). The provider's legacy
    // 404-fallback enrichment copies the employee-list reference onto
    // the same field, so one source covers both daemon generations.
    final executorAgent = agent.executorAgent.trim();
    // Status chip: paused / quota_wait are real states; a live roster
    // status (M17: running/paused/error/stopped/quota_wait from the C1
    // row) wins over the legacy presence default 'online' — none of these
    // agents are necessarily doing work (issue #160 follow-up wording).
    // 'running' is reserved for the active pulse below, driven by live
    // run counts.
    final paused = _pausedLocally.contains(agent.id);
    final rosterStatus = agent.status.toLowerCase();
    final status = paused
        ? 'paused'
        : (quotaState != null
              ? 'quota_wait'
              : (rosterStatus.isNotEmpty ? rosterStatus : 'online'));

    final execState = ref.watch(executionRunsProvider);
    final histogram = execState.histograms[agent.id] ?? const <int>[];
    // Active = doing work right now: roster active_count > 0, or a quota
    // episode in flight. Drives the pulsing orange border.
    final ownActive = execState.activeCounts[agent.id] ?? agent.activeCount;
    final isActive = !paused && (ownActive > 0 || quotaState != null);
    // Per-card counter: OTHER concurrent active runs org-wide (this
    // agent's own runs excluded), from the polled roster.
    final otherActive = execState.otherActiveCount(agent.id);

    final borderRadius = BorderRadius.circular(8);
    // Card content height grows with the C1 roster line / M11 stats line /
    // quota badge so the fixed-height Column never overflows (the
    // issue-#160 redesign's 52px box only fits name + histogram). FittedBox
    // still scales the whole card down when the grid cell is smaller.
    final hasRosterLine =
        agent.type.isNotEmpty ||
        agent.activeCount > 0 ||
        agent.dailyCostCents > 0;
    // M11 stats line rides roster rows only (type non-empty): legacy
    // config rows carry no drift/findings/last-run data and render nothing.
    final hasStatsLine = agent.type.isNotEmpty;
    final cardHeight =
        52.0 +
        (hasRosterLine ? 14.0 : 0.0) +
        (hasStatsLine ? 14.0 : 0.0) +
        (quotaState != null ? 20.0 : 0.0);
    return InkWell(
      key: ValueKey('agent-tile-${agent.id}'),
      onTap: () {
        // Tap and keyboard share one selection source; sync the cursor so
        // the previously selected tile loses its highlight immediately.
        setState(() {
          _selectedIndex = index;
        });
        ref.read(activeAgentProvider.notifier).state = agent;
        _loadGoals(agent.id);
      },
      child: PulsingBorder(
        active: isActive,
        borderRadius: borderRadius,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
          decoration: BoxDecoration(
            color: isSelected
                ? CyberpunkColors.orangePrimary.withValues(alpha: 0.1)
                : CyberpunkColors.black,
            border: Border.all(
              color: isSelected
                  ? CyberpunkColors.orangePrimary
                  : CyberpunkColors.midGray,
              width: isSelected ? 2 : 1,
            ),
            borderRadius: borderRadius,
          ),
          // FittedBox scales the whole card content down when the tile is
          // narrower/shorter than the natural layout (small viewports),
          // so the compact card NEVER overflows its grid cell.
          child: FittedBox(
            fit: BoxFit.scaleDown,
            alignment: Alignment.centerLeft,
            child: SizedBox(
              width: 260,
              height: cardHeight,
              child: Row(
                children: [
                  // 2x icon (20 -> 40) per the redesign spec.
                  Icon(
                    getAgentIcon(agent.id),
                    color: isSelected
                        ? CyberpunkColors.orangePrimary
                        : CyberpunkColors.greenSuccess,
                    size: 40,
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      mainAxisAlignment: MainAxisAlignment.center,
                      children: [
                        Row(
                          children: [
                            Flexible(
                              child: Text(
                                agent.name.toLowerCase(),
                                style: CyberpunkTypography.bodySmall.copyWith(
                                  color: isSelected
                                      ? CyberpunkColors.orangePrimary
                                      : CyberpunkColors.greenSuccess,
                                  fontFamily: 'SourceCodePro',
                                ),
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                              ),
                            ),
                            if (executorAgent.isNotEmpty)
                              const _AcpExecutorBadge(),
                            _AgentStatusChip(status: status),
                          ],
                        ),
                        const SizedBox(height: 3),
                        // 8-bit token-activity histogram, last hour.
                        TokenHistogram(cells: histogram, height: 18),
                        // C1 roster line (GUI parity with the TUI agents
                        // panel, leaf 03/02): tier, active count and daily
                        // spend from the unified roster (M17). Legacy
                        // config rows carry no C1 fields and render nothing.
                        if (agent.type.isNotEmpty ||
                            agent.activeCount > 0 ||
                            agent.dailyCostCents > 0)
                          Text(
                            'type: ${agent.type.isEmpty ? 'n/a' : agent.type.toLowerCase()}'
                            ' · active: ${agent.activeCount}'
                            ' · cost: \$${(agent.dailyCostCents / 100).toStringAsFixed(2)}',
                            key: ValueKey('agent-roster-${agent.id}'),
                            style: CyberpunkTypography.bodySmall.copyWith(
                              color: CyberpunkColors.midGray,
                              fontSize: 9,
                              fontFamily: 'SourceCodePro',
                            ),
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                          ),
                        // M11 roster stats line (GUI parity with the TUI
                        // agents panel's drift / findings / last run
                        // columns, agents_panel.go updateAgentsTable):
                        // drift formatted %.2f, findings count, last run
                        // HH:MM — 'never' when last_invocation is absent
                        // (never-run rows omit the key daemon-side). Rides
                        // roster rows only; legacy config rows carry no
                        // M11 data and render nothing.
                        if (hasStatsLine)
                          Text(
                            'drift: ${agent.driftScore.toStringAsFixed(2)}'
                            ' · findings: ${agent.findingsCount}'
                            ' · last run: ${_formatLastRun(agent.lastInvocation)}',
                            key: ValueKey('agent-stats-${agent.id}'),
                            style: CyberpunkTypography.bodySmall.copyWith(
                              color: CyberpunkColors.midGray,
                              fontSize: 9,
                              fontFamily: 'SourceCodePro',
                            ),
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                          ),
                        // Quota episode badge (leaf 04 wait label / I-M8
                        // give-up): restored after the issue-#160 redesign
                        // dropped it — the quota_detail_wiring pins require
                        // the tile label alongside the goals-pane lines.
                        if (quotaState != null)
                          QuotaStatusBadge(
                            quotaState: quotaState,
                            useDeviceTime: ref
                                .watch(renderingPrefsProvider)
                                .useDeviceTimeForQuota,
                          ),
                      ],
                    ),
                  ),
                  const SizedBox(width: 6),
                  // Right rail: pause/resume + the concurrent-run counter
                  // at the bottom-right of the card. Compact 20px buttons
                  // (no 48px Material touch target inside a 52px card).
                  Column(
                    mainAxisAlignment: MainAxisAlignment.spaceBetween,
                    crossAxisAlignment: CrossAxisAlignment.end,
                    children: [
                      if (paused)
                        _CompactIconButton(
                          tooltip: 'resume agent',
                          icon: Icons.play_arrow,
                          color: CyberpunkColors.greenSuccess,
                          onPressed: () => _togglePause(agent, pause: false),
                        )
                      else
                        _CompactIconButton(
                          tooltip: 'pause agent',
                          icon: Icons.pause,
                          color: CyberpunkColors.orangeDark,
                          onPressed: () => _togglePause(agent, pause: true),
                        ),
                      Tooltip(
                        message: 'other concurrent active runs (org-wide)',
                        child: Text(
                          'runs: $otherActive',
                          key: ValueKey('agent-runs-${agent.id}'),
                          style: CyberpunkTypography.bodySmall.copyWith(
                            color: otherActive > 0
                                ? CyberpunkColors.orangePrimary
                                : CyberpunkColors.midGray,
                            fontSize: 10,
                            fontFamily: 'SourceCodePro',
                          ),
                        ),
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  /// acp executor reference for [id], read from the agent model's M11
  /// executor_agent field (empty when absent). The roster decodes it
  /// directly; the legacy 404-fallback enrichment stamps the employee
  /// list's reference onto the same field. Read during build(); the pane
  /// rebuilds whenever agentState changes.
  String _executorFor(String id) {
    final agents = ref.read(agentProvider).agents;
    for (final a in agents) {
      if (a.id == id) return a.executorAgent.trim();
    }
    return '';
  }

  /// Last-run cell for the M11 stats line: 'never' when the roster row
  /// carried no last_invocation (never run), else device-local HH:MM.
  /// The TUI's formatTimeAgoTime renders HH:MM ('n/a' for the zero time);
  /// the GUI uses 'never' for the absent-key shape and device-local time
  /// (the execution-runs panel precedent) — documented surface deviation.
  static String _formatLastRun(DateTime? t) {
    if (t == null) return 'never';
    final local = t.toLocal();
    String pad(int n) => n.toString().padLeft(2, '0');
    return '${pad(local.hour)}:${pad(local.minute)}';
  }

  Widget _buildGoalsPane(Agent agent) {
    // Quota episode for the selected agent (parity with the TUI detail
    // view): when a fallback model is carrying work, show the
    // primary/active model lines under the agent name. The event's
    // model_id (primary model) is not stored in AgentQuotaState yet, so
    // the primary line degrades to "unknown" via quotaDetailLines.
    // M9: quotaDetailLines renders the daemon-local HH:MM by default (the
    // offset captured from the wire RFC3339), device-local when the
    // settings toggle is on.
    final useDeviceTime = ref
        .watch(renderingPrefsProvider)
        .useDeviceTimeForQuota;
    final quotaState = ref.watch(agentProvider).quotaEpisodes[agent.id];
    final quotaBlocked = quotaState?.quotaBlocked ?? false;
    final quotaLines = quotaDetailLines(
      null,
      quotaState?.fallbackModel,
      quotaState?.quotaWaitUntilEpoch,
      waitUntilOffsetMinutes: quotaState?.quotaWaitUntilOffsetMinutes,
      useDeviceTime: useDeviceTime,
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          'goals',
          style: CyberpunkTypography.headlineMedium.copyWith(
            color: CyberpunkColors.orangePrimary,
            fontSize: 16,
          ),
        ),
        Text(
          agent.name.toLowerCase(),
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.midGray,
          ),
        ),
        // acp executor detail line (phase 3 display parity with the
        // TUI detail view's "executor: <id> (acp)" line). Only rendered
        // when the employee roster carries a reference; lowercase.
        if (_executorFor(agent.id) case final String exec
            when exec.isNotEmpty) ...[
          const SizedBox(height: 2),
          Text(
            'executor: ${exec.toLowerCase()} (acp)',
            key: ValueKey('agent-executor-${agent.id}'),
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.midGray,
              fontFamily: 'SourceCodePro',
            ),
          ),
        ],
        // Aggregate health badge (GUI parity with the TUI goals view):
        // worst-health-wins across the loaded goals for this agent.
        if (_goals.isNotEmpty) ...[
          const SizedBox(height: 4),
          Row(
            children: [
              Icon(Icons.circle, size: 8, color: _goalsHealthColor),
              const SizedBox(width: 6),
              Text(
                'health: ${_worstGoalHealth.toLowerCase()}',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: _goalsHealthColor,
                  fontSize: 10,
                ),
              ),
            ],
          ),
        ],
        if (quotaLines.isNotEmpty) ...[
          const SizedBox(height: 8),
          for (final line in quotaLines)
            Text(
              line,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: quotaBlocked
                    ? CyberpunkColors.redAlert
                    : CyberpunkColors.yellowWarning,
                fontFamily: 'SourceCodePro',
              ),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
        ],
        const SizedBox(height: 12),
        // Bounded goals region inside the scrolling sidebar: the list
        // shrink-wraps up to 260px, then scrolls internally, so a long
        // goal list never pushes the panels below it off-screen.
        if (_goalsLoading)
          const SizedBox(
            height: 80,
            child: Center(child: CircularProgressIndicator()),
          )
        else if (_goalsError != null)
          Text(
            _goalsError!,
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.redAlert,
            ),
          )
        else if (_goals.isEmpty)
          const SizedBox(height: 60, child: Center(child: Text('no goals')))
        else
          ConstrainedBox(
            constraints: const BoxConstraints(maxHeight: 260),
            child: ListView.separated(
              shrinkWrap: true,
              itemCount: _goals.length,
              separatorBuilder: (_, __) => const SizedBox(height: 8),
              itemBuilder: (context, i) {
                final g = _goals[i];
                return _GoalCard(
                  goal: g,
                  onApprove: g.activePlanId.isEmpty
                      ? null
                      : () async {
                          await ref
                              .read(sdkClientProvider)
                              .approvePlan(g.activePlanId);
                          await _loadGoals(agent.id);
                        },
                  onReject: g.activePlanId.isEmpty
                      ? null
                      : () async {
                          await ref
                              .read(sdkClientProvider)
                              .rejectPlan(
                                g.activePlanId,
                                reason: 'rejected via gui',
                              );
                          await _loadGoals(agent.id);
                        },
                );
              },
            ),
          ),
      ],
    );
  }
}

/// Compact 20px icon button for the agent card's right rail. The standard
/// IconButton enforces a 48px Material touch target, which overflows the
/// ~52px-tall redesigned card; this keeps the tap affordance without the
/// minimum-size constraint.
class _CompactIconButton extends StatelessWidget {
  const _CompactIconButton({
    required this.tooltip,
    required this.icon,
    required this.color,
    required this.onPressed,
  });

  final String tooltip;
  final IconData icon;
  final Color color;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    return Tooltip(
      message: tooltip,
      child: InkWell(
        onTap: onPressed,
        borderRadius: BorderRadius.circular(4),
        child: Padding(
          padding: const EdgeInsets.all(2),
          child: Icon(icon, size: 16, color: color),
        ),
      ),
    );
  }
}

/// Small 'acp' badge on an agent tile whose employee entry carries an
/// `executor_agent` reference (acp-employees phase 3 display parity with
/// the TUI list 'exec' column marker). Lowercase per the repo UI rule.
class _AcpExecutorBadge extends StatelessWidget {
  const _AcpExecutorBadge();

  @override
  Widget build(BuildContext context) {
    final color = CyberpunkColors.cyanAccent;
    return Padding(
      padding: const EdgeInsets.only(right: 4),
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
        decoration: BoxDecoration(
          color: color.withValues(alpha: 0.15),
          borderRadius: BorderRadius.circular(4),
        ),
        child: Text(
          'acp',
          style: CyberpunkTypography.bodySmall.copyWith(
            color: color,
            fontSize: 9,
            fontFamily: 'SourceCodePro',
          ),
        ),
      ),
    );
  }
}

/// Lowercase status chip mirroring TUI agents_panel.go statusBadge color
/// semantics: running → success, paused/quota_wait → warning, error →
/// error, unknown states → muted text.
class _AgentStatusChip extends StatelessWidget {
  final String status;

  const _AgentStatusChip({required this.status});

  Color get _color => switch (status) {
    'running' || 'online' => CyberpunkColors.greenSuccess,
    'paused' || 'quota_wait' => CyberpunkColors.yellowWarning,
    'error' => CyberpunkColors.redAlert,
    _ => CyberpunkColors.midGray,
  };

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: BoxDecoration(
        color: _color.withValues(alpha: 0.15),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Text(
        status.toLowerCase(),
        style: CyberpunkTypography.bodySmall.copyWith(
          color: _color,
          fontSize: 9,
          fontFamily: 'SourceCodePro',
        ),
      ),
    );
  }
}

/// Inline error banner for agent list errors
class _AgentErrorBanner extends StatelessWidget {
  final String message;

  const _AgentErrorBanner({required this.message});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(12),
      color: CyberpunkColors.redAlert.withValues(alpha: 0.2),
      child: Row(
        children: [
          Icon(Icons.error_outline, color: CyberpunkColors.redAlert, size: 20),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              message,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.redAlert,
              ),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
          ),
        ],
      ),
    );
  }
}

class EmployeeGoal {
  final String id;
  final String title;
  final String health;
  final String activePlanId;
  final String gateCommand;

  const EmployeeGoal({
    required this.id,
    required this.title,
    required this.health,
    required this.activePlanId,
    required this.gateCommand,
  });

  factory EmployeeGoal.fromJson(Map<String, dynamic> json) {
    final gate = json['gate'];
    var gateCmd = '';
    if (gate is Map) {
      gateCmd = '${gate['command'] ?? ''}';
    }
    return EmployeeGoal(
      id: '${json['id'] ?? ''}',
      title: '${json['title'] ?? json['id'] ?? ''}',
      health: _healthLabel(json['health']),
      activePlanId: '${json['active_plan_id'] ?? ''}',
      gateCommand: gateCmd,
    );
  }

  static String _healthLabel(dynamic raw) {
    if (raw is String && raw.isNotEmpty) return raw;
    if (raw is num) {
      switch (raw.toInt()) {
        case 0:
          return 'healthy';
        case 1:
          return 'at_risk';
        case 2:
          return 'broken';
        default:
          return 'unknown';
      }
    }
    return 'unknown';
  }
}

class _GoalCard extends StatelessWidget {
  final EmployeeGoal goal;
  final Future<void> Function()? onApprove;
  final Future<void> Function()? onReject;

  const _GoalCard({required this.goal, this.onApprove, this.onReject});

  Color get _healthColor {
    switch (goal.health) {
      case 'healthy':
        return CyberpunkColors.greenSuccess;
      case 'at_risk':
        return CyberpunkColors.orangePrimary;
      case 'broken':
        return CyberpunkColors.redAlert;
      default:
        return CyberpunkColors.midGray;
    }
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      key: ValueKey('goal-card-${goal.id}'),
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: CyberpunkColors.black,
        border: Border.all(color: CyberpunkColors.midGray),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.circle, size: 8, color: _healthColor),
              const SizedBox(width: 6),
              Expanded(
                child: Text(
                  goal.title.toLowerCase(),
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.greenSuccess,
                  ),
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            ],
          ),
          const SizedBox(height: 4),
          Text(
            goal.health,
            style: CyberpunkTypography.bodySmall.copyWith(
              color: _healthColor,
              fontSize: 10,
            ),
          ),
          if (goal.gateCommand.isNotEmpty)
            Text(
              'gate: ${goal.gateCommand}',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
                fontSize: 10,
              ),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
          if (onApprove != null || onReject != null)
            Row(
              children: [
                if (onApprove != null)
                  TextButton(
                    onPressed: onApprove,
                    child: const Text('approve'),
                  ),
                if (onReject != null)
                  TextButton(onPressed: onReject, child: const Text('reject')),
              ],
            ),
        ],
      ),
    );
  }
}
