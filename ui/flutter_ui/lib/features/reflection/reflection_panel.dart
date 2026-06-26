import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:go_router/go_router.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import 'reflection_models.dart';

/// Reflection panel — displays self-reflection proposals from the daemon
/// and lets the user apply, skip, or handle propose-only targets manually.
///
/// Mirrors the SkillPanel aesthetic: cyberpunk colors, lowercase text,
/// loading/error/empty states with retry.
class ReflectionPanel extends ConsumerStatefulWidget {
  const ReflectionPanel({super.key});

  @override
  ConsumerState<ReflectionPanel> createState() => _ReflectionPanelState();
}

class _ReflectionPanelState extends ConsumerState<ReflectionPanel> {
  List<ReflectionProposal> _proposals = [];
  bool _isLoading = true;
  String? _error;
  late final FocusNode _keyboardFocusNode;

  @override
  void initState() {
    super.initState();
    _keyboardFocusNode = FocusNode();
    _loadProposals();
  }

  @override
  void dispose() {
    _keyboardFocusNode.dispose();
    super.dispose();
  }

  Future<void> _loadProposals() async {
    setState(() {
      _isLoading = true;
      _error = null;
    });
    try {
      final client = ref.read(sdkClientProvider);
      final rawList = await client.getReflectionProposalsRaw();
      final proposals =
          rawList.map((m) => ReflectionProposal.fromJson(m)).toList();
      if (mounted) {
        setState(() {
          _proposals = proposals;
          _isLoading = false;
          _error = null;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = e.toString();
          _isLoading = false;
        });
      }
    }
  }

  void _closePanel() {
    context.go('/');
  }

  IconData _typeIcon(String type) {
    switch (type) {
      case 'skill_create':
        return Icons.tune;
      case 'project_instruction':
        return Icons.description;
      case 'agent_prompt':
      case 'prompt_component':
        return Icons.edit_note;
      default:
        return Icons.lightbulb_outline;
    }
  }

  Future<void> _applyProposal(ReflectionProposal proposal) async {
    try {
      final client = ref.read(sdkClientProvider);
      await client.applyReflectionProposal(proposal.id);

      if (!mounted) return;

      if (proposal.isProposeOnly) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              'propose-only: write manually — ${proposal.target}',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.orangeBright,
                fontFamily: 'SourceCodePro',
              ),
            ),
            backgroundColor: CyberpunkColors.darkGray,
            duration: const Duration(seconds: 4),
          ),
        );
      } else {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              'applied: ${proposal.target}',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.greenSuccess,
                fontFamily: 'SourceCodePro',
              ),
            ),
            backgroundColor: CyberpunkColors.darkGray,
            duration: const Duration(seconds: 2),
          ),
        );
      }

      await _loadProposals();
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            'failed to apply: $e',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.redAlert,
              fontFamily: 'SourceCodePro',
            ),
          ),
          backgroundColor: CyberpunkColors.darkGray,
          duration: const Duration(seconds: 3),
        ),
      );
    }
  }

  Future<void> _skipProposal(ReflectionProposal proposal) async {
    try {
      final client = ref.read(sdkClientProvider);
      await client.skipReflectionProposal(proposal.id);
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            'skipped: ${proposal.target}',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.midGray,
              fontFamily: 'SourceCodePro',
            ),
          ),
          backgroundColor: CyberpunkColors.darkGray,
          duration: const Duration(seconds: 2),
        ),
      );
      await _loadProposals();
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            'failed to skip: $e',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.redAlert,
              fontFamily: 'SourceCodePro',
            ),
          ),
          backgroundColor: CyberpunkColors.darkGray,
          duration: const Duration(seconds: 3),
        ),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    return Focus(
      focusNode: _keyboardFocusNode,
      onKeyEvent: (FocusNode node, KeyEvent event) {
        if (event.logicalKey == LogicalKeyboardKey.escape) {
          _closePanel();
        }
        return KeyEventResult.ignored;
      },
      child: Container(
        decoration: BoxDecoration(
          color: CyberpunkColors.darkGray.withValues(alpha: 0.5),
          border: Border(
            top: BorderSide(
              color:
                  CyberpunkColors.orangePrimary.withValues(alpha: 0.3),
              width: 1,
            ),
          ),
        ),
        child: Column(
          children: [
            _buildHeader(),
            Expanded(
              child: _isLoading
                  ? const Center(
                      child: SizedBox(
                        width: 20,
                        height: 20,
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          valueColor: AlwaysStoppedAnimation<Color>(
                            CyberpunkColors.orangePrimary,
                          ),
                        ),
                      ),
                    )
                  : _error != null
                      ? _buildErrorState()
                      : _proposals.isEmpty
                          ? _buildEmptyState()
                          : _buildProposalList(),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildHeader() {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: const BoxDecoration(
        border: Border(
          bottom: BorderSide(color: CyberpunkColors.midGray, width: 1),
        ),
      ),
      child: Row(
        children: [
          const Icon(
            Icons.psychology,
            color: CyberpunkColors.orangePrimary,
            size: 18,
          ),
          const SizedBox(width: 8),
          Text(
            'reflection proposals',
            style: CyberpunkTypography.label.copyWith(
              color: CyberpunkColors.orangePrimary,
            ),
          ),
          const Spacer(),
          IconButton(
            icon: const Icon(Icons.close, size: 18),
            onPressed: _closePanel,
            padding: EdgeInsets.zero,
            constraints: const BoxConstraints(),
            tooltip: 'close',
          ),
          GestureDetector(
            onTap: _loadProposals,
            child: const Icon(
              Icons.refresh,
              color: CyberpunkColors.orangePrimary,
              size: 16,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildErrorState() {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(
            Icons.error_outline,
            color: CyberpunkColors.redAlert,
            size: 48,
          ),
          const SizedBox(height: 16),
          Text(
            'failed to load proposals',
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.redAlert,
            ),
          ),
          const SizedBox(height: 8),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 32),
            child: Text(
              _error ?? '',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.orangeDark,
              ),
              textAlign: TextAlign.center,
              maxLines: 3,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          const SizedBox(height: 16),
          TextButton(
            onPressed: _loadProposals,
            child: Text(
              'retry',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildEmptyState() {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(
            Icons.psychology_outlined,
            size: 48,
            color: CyberpunkColors.orangeDark,
          ),
          const SizedBox(height: 16),
          Text(
            'no pending proposals',
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.orangeDark,
            ),
          ),
          const SizedBox(height: 8),
          Text(
            'reflection lessons will appear here',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.midGray,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildProposalList() {
    return ListView.separated(
      padding: const EdgeInsets.all(8),
      itemCount: _proposals.length,
      separatorBuilder: (_, __) => const SizedBox(height: 4),
      itemBuilder: (context, index) {
        return _buildProposalCard(_proposals[index]);
      },
    );
  }

  Widget _buildProposalCard(ReflectionProposal proposal) {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: CyberpunkColors.black.withValues(alpha: 0.3),
        border: Border.all(
          color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
          width: 1,
        ),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(
                _typeIcon(proposal.type),
                size: 16,
                color: CyberpunkColors.orangeBright,
              ),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  proposal.target.toLowerCase(),
                  style: CyberpunkTypography.bodyMedium.copyWith(
                    color: CyberpunkColors.orangePrimary,
                    fontFamily: 'SourceCodePro',
                  ),
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            ],
          ),
          if (proposal.justification.isNotEmpty) ...[
            const SizedBox(height: 6),
            Text(
              proposal.justification,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.lightGray,
              ),
              maxLines: 3,
              overflow: TextOverflow.ellipsis,
            ),
          ],
          const SizedBox(height: 8),
          Wrap(
            spacing: 6,
            runSpacing: 4,
            children: [
              _buildChip(
                '${proposal.confidence.toStringAsFixed(2)} confidence',
              ),
              if (proposal.source.isNotEmpty)
                _buildChip(proposal.source),
              if (proposal.status != 'pending')
                _buildChip(proposal.status),
            ],
          ),
          const SizedBox(height: 8),
          Row(
            children: [
              _buildActionButton(
                label: 'apply',
                icon: Icons.check,
                color: CyberpunkColors.greenSuccess,
                onTap: () => _applyProposal(proposal),
              ),
              const SizedBox(width: 8),
              _buildActionButton(
                label: 'skip',
                icon: Icons.close,
                color: CyberpunkColors.redAlert,
                onTap: () => _skipProposal(proposal),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _buildChip(String label) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: BoxDecoration(
        color: CyberpunkColors.midGray.withValues(alpha: 0.2),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Text(
        label.toLowerCase(),
        style: CyberpunkTypography.bodySmall.copyWith(
          fontSize: 9,
          color: CyberpunkColors.lightGray,
        ),
      ),
    );
  }

  Widget _buildActionButton({
    required String label,
    required IconData icon,
    required Color color,
    required VoidCallback onTap,
  }) {
    return GestureDetector(
      onTap: onTap,
      child: Container(
        padding:
            const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
        decoration: BoxDecoration(
          color: CyberpunkColors.midGray.withValues(alpha: 0.3),
          border: Border.all(
            color: color.withValues(alpha: 0.4),
            width: 1,
          ),
          borderRadius: BorderRadius.circular(2),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 12, color: color),
            const SizedBox(width: 4),
            Text(
              label,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: color,
                fontWeight: FontWeight.w600,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
