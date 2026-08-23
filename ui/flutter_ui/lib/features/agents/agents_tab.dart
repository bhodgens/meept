import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../core/constants.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../widgets/background_image.dart';
import '../../providers/providers.dart';
import '../../providers/tab_activation_provider.dart'
    show keyboardFocusProvider;
import '../../models/api_models.dart';

/// Agents tab - displays all available agents.
///
/// S3: This is the Flutter full-window agents view. The menubar app
/// (menubar/MeeptMenuBar/Views/AgentsView.swift) is a separate native
/// Swift implementation for the macOS menu bar. Both surfaces query
/// the same REST API (/api/v1/agents/*) but serve different UX roles:
/// - Swift menubar tab: quick status, pause/resume, approve plans
/// - Flutter full window: detailed constitution editing, audit review
///
/// Currently this widget shows the basic agent grid. Full employee detail
/// (constitution, goals, audit findings) is handled via the agents API and
/// can be extended with additional views.
class AgentsTab extends ConsumerStatefulWidget {
  const AgentsTab({super.key});

  @override
  ConsumerState<AgentsTab> createState() => _AgentsTabState();
}

class _AgentsTabState extends ConsumerState<AgentsTab> {
  int _selectedIndex = 0;
  final _gridFocusNode = FocusNode();

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(agentProvider.notifier).loadAgents();
    });
  }

  @override
  void dispose() {
    _gridFocusNode.dispose();
    super.dispose();
  }

  /// Handle keyboard navigation for the agents grid.
  KeyEventResult _handleKey(FocusNode node, KeyEvent event) {
    if (event is KeyDownEvent && node.hasFocus) {
      final agentState = ref.read(agentProvider);
      final count = agentState.agents.length;
      if (count == 0) return KeyEventResult.ignored;

      // Calculate columns based on grid delegate (maxCrossAxisExtent: 225)
      final screenWidth = MediaQuery.of(node.context!).size.width;
      final cols = (screenWidth / 225).floor().clamp(1, 4);

      if (event.logicalKey == LogicalKeyboardKey.arrowDown) {
        setState(() {
          _selectedIndex = (_selectedIndex + cols).clamp(0, count - 1);
        });
        return KeyEventResult.handled;
      }
      if (event.logicalKey == LogicalKeyboardKey.arrowUp) {
        setState(() {
          _selectedIndex = (_selectedIndex - cols).clamp(0, count - 1);
        });
        return KeyEventResult.handled;
      }
      if (event.logicalKey == LogicalKeyboardKey.arrowRight) {
        setState(() {
          _selectedIndex = (_selectedIndex + 1).clamp(0, count - 1);
        });
        return KeyEventResult.handled;
      }
      if (event.logicalKey == LogicalKeyboardKey.arrowLeft) {
        setState(() {
          _selectedIndex = (_selectedIndex - 1).clamp(0, count - 1);
        });
        return KeyEventResult.handled;
      }
    }
    return KeyEventResult.ignored;
  }

  @override
  Widget build(BuildContext context) {
    final agentState = ref.watch(agentProvider);
    final activeAgent = ref.watch(activeAgentProvider);

    return BackgroundImage(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
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
                      ref
                          .read(keyboardFocusProvider.notifier)
                          .setFocusedPane(0);
                    }
                  },
                  onKeyEvent: _handleKey,
                  child: GridView.builder(
                    gridDelegate:
                        const SliverGridDelegateWithMaxCrossAxisExtent(
                          maxCrossAxisExtent: 225,
                          crossAxisSpacing: 8,
                          mainAxisSpacing: 8,
                          childAspectRatio: 0.87,
                        ),
                    itemCount: agentState.agents.length,
                    itemBuilder: (context, index) {
                      final agent = agentState.agents[index];
                      final isSelected = activeAgent?.id == agent.id;
                      final isKeyboardSelected = _selectedIndex == index;
                      return _buildAgentCard(
                        agent,
                        isSelected,
                        isKeyboardSelected,
                      );
                    },
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }

  Widget _buildAgentCard(
    Agent agent,
    bool isSelected,
    bool isKeyboardSelected,
  ) {
    return InkWell(
      key: ValueKey('agent-tile-${agent.id}'),
      onTap: () {
        ref.read(activeAgentProvider.notifier).state = agent;
      },
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
        decoration: BoxDecoration(
          color: isKeyboardSelected
              ? CyberpunkColors.orangeDark.withValues(alpha: 0.3)
              : (isSelected
                    ? CyberpunkColors.orangePrimary.withValues(alpha: 0.1)
                    : CyberpunkColors.black),
          border: Border.all(
            color: isKeyboardSelected
                ? CyberpunkColors.orangeDark
                : (isSelected
                      ? CyberpunkColors.orangePrimary
                      : CyberpunkColors.midGray),
            width: isKeyboardSelected ? 3 : 1,
          ),
          borderRadius: BorderRadius.circular(8),
        ),
        child: Row(
          children: [
            Icon(
              getAgentIcon(agent.id),
              color: isSelected
                  ? CyberpunkColors.orangePrimary
                  : CyberpunkColors.greenSuccess,
              size: 20,
            ),
            const SizedBox(width: 8),
            Expanded(
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
          ],
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
          const Icon(
            Icons.error_outline,
            color: CyberpunkColors.redAlert,
            size: 20,
          ),
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
