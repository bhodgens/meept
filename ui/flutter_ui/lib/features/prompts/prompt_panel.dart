import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:go_router/go_router.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import 'prompt_models.dart';

/// Prompt-editor panel — lists discoverable prompt templates from the
/// daemon and lets the user inspect content, copy to user-local override,
/// delete an override, or validate one/all templates.
///
/// Mirrors the ReflectionPanel / SkillPanel aesthetic: cyberpunk colours,
/// lowercase text, loading/error/empty states with retry. Content is
/// read-only in v1; editing is via the CLI (`meept prompts edit`).
class PromptPanel extends ConsumerStatefulWidget {
  const PromptPanel({super.key});

  @override
  ConsumerState<PromptPanel> createState() => _PromptPanelState();
}

class _PromptPanelState extends ConsumerState<PromptPanel> {
  List<PromptSummary> _prompts = [];
  bool _isLoading = true;
  String? _error;
  late final FocusNode _keyboardFocusNode;

  @override
  void initState() {
    super.initState();
    _keyboardFocusNode = FocusNode();
    _loadPrompts();
  }

  @override
  void dispose() {
    _keyboardFocusNode.dispose();
    super.dispose();
  }

  Future<void> _loadPrompts() async {
    setState(() {
      _isLoading = true;
      _error = null;
    });
    try {
      final client = ref.read(sdkClientProvider);
      final rawList = await client.listPromptsRaw();
      final prompts =
          rawList.map((m) => PromptSummary.fromJson(m)).toList();
      // Stable ordering: tier priority (user first) then name.
      prompts.sort((a, b) {
        final tierCmp = _tierRank(a.tier).compareTo(_tierRank(b.tier));
        if (tierCmp != 0) return tierCmp;
        return a.name.compareTo(b.name);
      });
      if (mounted) {
        setState(() {
          _prompts = prompts;
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

  Future<void> _openDetail(PromptSummary summary) async {
    final detail = await Navigator.of(context).push<PromptDetail>(
      MaterialPageRoute<PromptDetail>(
        builder: (_) => _PromptDetailRoute(summary: summary),
      ),
    );
    // If the detail screen mutated state (override created/deleted),
    // refresh the list.
    if (detail == null && _PromptDetailRoute.lastMutated) {
      _loadPrompts();
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
                      : _prompts.isEmpty
                          ? _buildEmptyState()
                          : _buildPromptList(),
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
            Icons.edit_note,
            color: CyberpunkColors.orangePrimary,
            size: 18,
          ),
          const SizedBox(width: 8),
          Text(
            'prompt templates',
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
            onTap: _loadPrompts,
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
            'failed to load prompts',
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
            onPressed: _loadPrompts,
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
            Icons.edit_note,
            size: 48,
            color: CyberpunkColors.orangeDark,
          ),
          const SizedBox(height: 16),
          Text(
            'no prompt templates found',
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.orangeDark,
            ),
          ),
          const SizedBox(height: 8),
          Text(
            'bundled and project prompts will appear here',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.midGray,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildPromptList() {
    return ListView.separated(
      padding: const EdgeInsets.all(8),
      itemCount: _prompts.length,
      separatorBuilder: (_, __) => const SizedBox(height: 4),
      itemBuilder: (context, index) {
        return _buildPromptCard(_prompts[index]);
      },
    );
  }

  Widget _buildPromptCard(PromptSummary summary) {
    final tierColor = _tierColor(summary.tier);
    return GestureDetector(
      onTap: () => _openDetail(summary),
      child: Container(
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: CyberpunkColors.black.withValues(alpha: 0.3),
          border: Border.all(
            color: tierColor.withValues(alpha: 0.4),
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
                  Icons.edit_note,
                  size: 16,
                  color: tierColor,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    summary.name.toLowerCase(),
                    style: CyberpunkTypography.bodyMedium.copyWith(
                      color: CyberpunkColors.orangePrimary,
                      fontFamily: 'SourceCodePro',
                    ),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 6),
            Wrap(
              spacing: 6,
              runSpacing: 4,
              children: [
                _buildTierBadge(summary.tier, tierColor),
                if (summary.sourcePath.isNotEmpty)
                  _buildChip(summary.sourcePath.toLowerCase()),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildTierBadge(String tier, Color color) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.2),
        borderRadius: BorderRadius.circular(4),
        border: Border.all(color: color.withValues(alpha: 0.5), width: 1),
      ),
      child: Text(
        tier,
        style: CyberpunkTypography.bodySmall.copyWith(
          fontSize: 9,
          color: color,
          fontWeight: FontWeight.w600,
        ),
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
        label,
        style: CyberpunkTypography.bodySmall.copyWith(
          fontSize: 9,
          color: CyberpunkColors.lightGray,
        ),
      ),
    );
  }
}

/// Detail route for a single template. Shows the full content and
/// provides the validate / copy-to-override / delete-override actions.
///
/// The route uses a static [lastMutated] flag so the list screen can
/// detect whether to refresh after the route is popped. This is a
/// lightweight alternative to returning a result struct through
/// `Navigator.pop` for every code path.
class _PromptDetailRoute extends ConsumerStatefulWidget {
  final PromptSummary summary;

  /// Set true whenever this route mutates daemon-side state
  /// (PUT/DELETE). Reset at construction so each push is independent.
  static bool lastMutated = false;

  const _PromptDetailRoute({required this.summary});

  @override
  ConsumerState<_PromptDetailRoute> createState() =>
      _PromptDetailRouteState();
}

class _PromptDetailRouteState extends ConsumerState<_PromptDetailRoute> {
  PromptDetail? _detail;
  bool _isLoading = true;
  String? _error;
  bool _isBusy = false;

  @override
  void initState() {
    super.initState();
    // Reset mutation flag on each push so a stale `true` doesn't
    // cause an unnecessary refresh of the list.
    _PromptDetailRoute.lastMutated = false;
    _loadDetail();
  }

  Future<void> _loadDetail() async {
    setState(() {
      _isLoading = true;
      _error = null;
    });
    try {
      final client = ref.read(sdkClientProvider);
      final raw = await client.getPromptRaw(widget.summary.name);
      if (mounted) {
        setState(() {
          _detail = PromptDetail.fromJson(raw);
          _isLoading = false;
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

  Future<void> _copyToOverride() async {
    final detail = _detail;
    if (detail == null || _isBusy) return;
    setState(() => _isBusy = true);
    try {
      final client = ref.read(sdkClientProvider);
      await client.putPromptRaw(widget.summary.name, detail.content);
      _PromptDetailRoute.lastMutated = true;
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            'copied to user override: ${widget.summary.name}',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.greenSuccess,
              fontFamily: 'SourceCodePro',
            ),
          ),
          backgroundColor: CyberpunkColors.darkGray,
          duration: const Duration(seconds: 2),
        ),
      );
      await _loadDetail();
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            'failed to copy: $e',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.redAlert,
              fontFamily: 'SourceCodePro',
            ),
          ),
          backgroundColor: CyberpunkColors.darkGray,
          duration: const Duration(seconds: 3),
        ),
      );
    } finally {
      if (mounted) setState(() => _isBusy = false);
    }
  }

  Future<void> _deleteOverride() async {
    if (_isBusy) return;
    setState(() => _isBusy = true);
    try {
      final client = ref.read(sdkClientProvider);
      await client.deletePromptRaw(widget.summary.name);
      _PromptDetailRoute.lastMutated = true;
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            'deleted user override: ${widget.summary.name}',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.greenSuccess,
              fontFamily: 'SourceCodePro',
            ),
          ),
          backgroundColor: CyberpunkColors.darkGray,
          duration: const Duration(seconds: 2),
        ),
      );
      Navigator.of(context).pop();
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            'failed to delete: $e',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.redAlert,
              fontFamily: 'SourceCodePro',
            ),
          ),
          backgroundColor: CyberpunkColors.darkGray,
          duration: const Duration(seconds: 3),
        ),
      );
    } finally {
      if (mounted) setState(() => _isBusy = false);
    }
  }

  Future<void> _validateOne() async {
    if (_isBusy) return;
    setState(() => _isBusy = true);
    try {
      final client = ref.read(sdkClientProvider);
      final raw = await client.validatePromptRaw(widget.summary.name);
      final result = PromptValidateResult.fromJson(raw);
      if (!mounted) return;
      final color = result.valid
          ? CyberpunkColors.greenSuccess
          : CyberpunkColors.redAlert;
      final message = result.valid
          ? 'valid: ${widget.summary.name}'
          : 'invalid: ${result.error.isNotEmpty ? result.error : widget.summary.name}';
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            message,
            style: CyberpunkTypography.bodySmall.copyWith(
              color: color,
              fontFamily: 'SourceCodePro',
            ),
          ),
          backgroundColor: CyberpunkColors.darkGray,
          duration: const Duration(seconds: 3),
        ),
      );
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            'failed to validate: $e',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.redAlert,
              fontFamily: 'SourceCodePro',
            ),
          ),
          backgroundColor: CyberpunkColors.darkGray,
          duration: const Duration(seconds: 3),
        ),
      );
    } finally {
      if (mounted) setState(() => _isBusy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final isUserTier = widget.summary.isUserTier;
    return Scaffold(
      backgroundColor: CyberpunkColors.black,
      appBar: AppBar(
        backgroundColor: CyberpunkColors.darkGray,
        title: Text(
          widget.summary.name.toLowerCase(),
          style: CyberpunkTypography.bodyMedium.copyWith(
            color: CyberpunkColors.orangePrimary,
            fontFamily: 'SourceCodePro',
          ),
        ),
        leading: IconButton(
          icon: const Icon(Icons.arrow_back, size: 18),
          onPressed: () => Navigator.of(context).pop(),
          tooltip: 'back',
        ),
      ),
      body: _isLoading
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
              ? _buildDetailError()
              : _buildDetailBody(isUserTier),
    );
  }

  Widget _buildDetailError() {
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
            'failed to load template',
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
              maxLines: 4,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          const SizedBox(height: 16),
          TextButton(
            onPressed: _loadDetail,
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

  Widget _buildDetailBody(bool isUserTier) {
    final detail = _detail;
    if (detail == null) {
      return const Center(child: Text('no content'));
    }
    return Column(
      children: [
        // Action bar
        Container(
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
          decoration: const BoxDecoration(
            border: Border(
              bottom: BorderSide(color: CyberpunkColors.midGray, width: 1),
            ),
          ),
          child: Wrap(
            spacing: 8,
            runSpacing: 4,
            children: [
              _buildActionButton(
                label: 'validate',
                icon: Icons.check_circle_outline,
                color: CyberpunkColors.greenSuccess,
                onTap: _isBusy ? null : _validateOne,
              ),
              _buildActionButton(
                label: 'copy to override',
                icon: Icons.file_copy,
                color: CyberpunkColors.orangeBright,
                onTap: _isBusy ? null : _copyToOverride,
              ),
              if (isUserTier)
                _buildActionButton(
                  label: 'delete override',
                  icon: Icons.delete_outline,
                  color: CyberpunkColors.redAlert,
                  onTap: _isBusy ? null : _deleteOverride,
                ),
            ],
          ),
        ),
        // Tier + source meta
        Container(
          width: double.infinity,
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
          color: CyberpunkColors.darkGray.withValues(alpha: 0.3),
          child: Wrap(
            spacing: 8,
            runSpacing: 4,
            children: [
              _buildTierBadge(detail.tier, _tierColor(detail.tier)),
              if (detail.sourcePath.isNotEmpty)
                _buildMetaChip(detail.sourcePath.toLowerCase()),
              if (detail.modified != null)
                _buildMetaChip(_formatDate(detail.modified!)),
            ],
          ),
        ),
        // Content (read-only)
        Expanded(
          child: Container(
            margin: const EdgeInsets.all(8),
            padding: const EdgeInsets.all(10),
            decoration: BoxDecoration(
              color: CyberpunkColors.black.withValues(alpha: 0.5),
              border: Border.all(
                color: CyberpunkColors.midGray.withValues(alpha: 0.5),
                width: 1,
              ),
              borderRadius: BorderRadius.circular(4),
            ),
            child: SingleChildScrollView(
              child: SelectableText(
                detail.content,
                style: CyberpunkTypography.code.copyWith(
                  color: CyberpunkColors.terminalGreen,
                  fontSize: 12,
                ),
              ),
            ),
          ),
        ),
      ],
    );
  }

  Widget _buildActionButton({
    required String label,
    required IconData icon,
    required Color color,
    required VoidCallback? onTap,
  }) {
    final disabled = onTap == null;
    final effectiveColor =
        disabled ? color.withValues(alpha: 0.4) : color;
    return GestureDetector(
      onTap: onTap,
      child: Container(
        padding:
            const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
        decoration: BoxDecoration(
          color: CyberpunkColors.midGray.withValues(alpha: 0.3),
          border: Border.all(
            color: effectiveColor.withValues(alpha: 0.4),
            width: 1,
          ),
          borderRadius: BorderRadius.circular(2),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 12, color: effectiveColor),
            const SizedBox(width: 4),
            Text(
              label,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: effectiveColor,
                fontWeight: FontWeight.w600,
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildTierBadge(String tier, Color color) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.2),
        borderRadius: BorderRadius.circular(4),
        border: Border.all(color: color.withValues(alpha: 0.5), width: 1),
      ),
      child: Text(
        tier,
        style: CyberpunkTypography.bodySmall.copyWith(
          fontSize: 9,
          color: color,
          fontWeight: FontWeight.w600,
        ),
      ),
    );
  }

  Widget _buildMetaChip(String label) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: BoxDecoration(
        color: CyberpunkColors.midGray.withValues(alpha: 0.2),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Text(
        label,
        style: CyberpunkTypography.bodySmall.copyWith(
          fontSize: 9,
          color: CyberpunkColors.lightGray,
        ),
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Lower rank = higher priority (user > project > system > bundled).
/// Unknown tiers sort last but above bundled.
int _tierRank(String tier) {
  switch (tier) {
    case 'user':
      return 0;
    case 'project':
      return 1;
    case 'system':
      return 2;
    case 'bundled':
      return 4;
    default:
      return 3;
  }
}

Color _tierColor(String tier) {
  switch (tier) {
    case 'user':
      return CyberpunkColors.greenSuccess;
    case 'project':
      return CyberpunkColors.blueInfo;
    case 'system':
      return CyberpunkColors.cyanAccent;
    case 'bundled':
    default:
      return CyberpunkColors.midGray;
  }
}

String _formatDate(DateTime dt) {
  String two(int v) => v.toString().padLeft(2, '0');
  return '${dt.year}-${two(dt.month)}-${two(dt.day)} '
      '${two(dt.hour)}:${two(dt.minute)}';
}
