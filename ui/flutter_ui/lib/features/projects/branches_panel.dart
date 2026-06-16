import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:go_router/go_router.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/api_models.dart';
import '../../services/api_client.dart';
import '../../providers/providers.dart';

/// BranchesPanel displays git branches for the current project.
///
/// Allows switching branches and viewing branch status.
class BranchesPanel extends ConsumerStatefulWidget {
  const BranchesPanel({super.key});

  @override
  ConsumerState<BranchesPanel> createState() => _BranchesPanelState();
}

class _BranchesPanelState extends ConsumerState<BranchesPanel> {
  List<BranchInfo> _branches = [];
  String? _currentBranch;
  bool _isLoading = false;
  String? _error;

  late final ApiClient _apiClient;
  late final FocusNode _keyboardFocusNode;

  @override
  void initState() {
    super.initState();
    _apiClient = ref.read(apiClientProvider);
    _keyboardFocusNode = FocusNode();
    _loadBranches();
  }

  @override
  void dispose() {
    _keyboardFocusNode.dispose();
    super.dispose();
  }

  Future<void> _loadBranches() async {
    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      // Resolve the active project from the provider rather than hardcoding
      // 'default'. The daemon assigns UUIDs to auto-registered projects, so
      // the literal 'default' only works for one manually-registered project.
      final project = ref.read(resolveActiveProjectProvider).valueOrNull;
      if (project == null) {
        if (mounted) {
          setState(() {
            _isLoading = false;
            _error = 'no active project registered. add a project with '
                '`meept projects add <path>` first.';
          });
        }
        return;
      }
      final branches = await _apiClient.listBranches(project.id);
      final current = branches.where((b) => b.isCurrent).firstOrNull;

      if (mounted) {
        setState(() {
          _isLoading = false;
          _branches = branches;
          _currentBranch = current?.name;
        });
      }
    } on ApiClientException catch (e) {
      if (mounted) {
        setState(() {
          _isLoading = false;
          _error = e.message;
        });
      }
    }
  }

  void _closePanel() {
    context.go('/');
  }

  Future<void> _checkoutBranch(String branchName) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => _buildCheckoutDialog(branchName),
    );

    if (confirmed == true && mounted) {
      try {
        final project = ref.read(resolveActiveProjectProvider).valueOrNull;
        if (project == null) {
          if (mounted) {
            ScaffoldMessenger.of(context).showSnackBar(
              const SnackBar(
                content: Text('no active project'),
                backgroundColor: CyberpunkColors.orangeDark,
              ),
            );
          }
          return;
        }
        await _apiClient.checkoutBranch(project.id, branchName);
        // Refresh branch list so is_current flags are accurate
        await _loadBranches();
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text('switched to branch $branchName'),
              backgroundColor: CyberpunkColors.orangePrimary,
            ),
          );
        }
      } on ApiClientException catch (e) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text(e.message),
              backgroundColor: CyberpunkColors.orangeDark,
            ),
          );
        }
      }
    }
  }

  Widget _buildCheckoutDialog(String branchName) {
    return AlertDialog(
      backgroundColor: CyberpunkColors.darkGray,
      title: Text(
        'checkout branch',
        style: CyberpunkTypography.headlineSmall.copyWith(
          color: CyberpunkColors.orangePrimary,
        ),
      ),
      content: Text(
        'switch to branch $branchName?',
        style: CyberpunkTypography.bodyMedium,
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context, false),
          child: Text(
            'cancel',
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.orangeDark,
            ),
          ),
        ),
        ElevatedButton(
          onPressed: () => Navigator.pop(context, true),
          style: ElevatedButton.styleFrom(
            backgroundColor: CyberpunkColors.orangePrimary,
            foregroundColor: CyberpunkColors.darkGray,
          ),
          child: const Text('checkout'),
        ),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    return KeyboardListener(
      focusNode: _keyboardFocusNode,
      onKeyEvent: (KeyEvent event) {
        if (event.logicalKey == LogicalKeyboardKey.escape) {
          _closePanel();
        }
      },
      child: Container(
      color: CyberpunkColors.darkGray,
      child: Column(
        children: [
          // Header
          Container(
            padding: const EdgeInsets.all(16),
            decoration: BoxDecoration(
              border: Border(
                bottom: BorderSide(
                  color: CyberpunkColors.orangePrimary.withValues(alpha: 0.3),
                  width: 1,
                ),
              ),
            ),
            child: Row(
              children: [
                const Icon(
                  Icons.call_split,
                  color: CyberpunkColors.orangeBright,
                  size: 24,
                ),
                const SizedBox(width: 12),
                Text(
                  'branches',
                  style: CyberpunkTypography.headlineSmall.copyWith(
                    color: CyberpunkColors.orangePrimary,
                  ),
                ),
                const Spacer(),
                if (_currentBranch != null)
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 12,
                      vertical: 4,
                    ),
                    decoration: BoxDecoration(
                      border: Border.all(
                        color: CyberpunkColors.orangePrimary,
                        width: 1,
                      ),
                      borderRadius: BorderRadius.circular(4),
                    ),
                    child: Text(
                      _currentBranch!,
                      style: CyberpunkTypography.bodySmall.copyWith(
                        color: CyberpunkColors.orangeBright,
                      ),
                    ),
                  ),
              ],
            ),
          ),

          // Content
          Expanded(
            child: _isLoading
                ? const Center(
                    child: CircularProgressIndicator(
                      color: CyberpunkColors.orangePrimary,
                    ),
                  )
                : _error != null
                    ? _buildError()
                    : _buildBranchList(),
          ),
        ],
      ),
    ),
    );
  }

  Widget _buildError() {
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          const Icon(
            Icons.error_outline,
            size: 48,
            color: CyberpunkColors.orangeDark,
          ),
          const SizedBox(height: 16),
          Text(
            _error!,
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.orangeDark,
            ),
          ),
          const SizedBox(height: 16),
          ElevatedButton.icon(
            onPressed: _loadBranches,
            icon: const Icon(Icons.refresh),
            label: const Text('retry'),
            style: ElevatedButton.styleFrom(
              backgroundColor: CyberpunkColors.orangePrimary,
              foregroundColor: CyberpunkColors.darkGray,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildBranchList() {
    if (_branches.isEmpty) {
      return Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            const Icon(
              Icons.call_split_outlined,
              size: 48,
              color: CyberpunkColors.orangeDark,
            ),
            const SizedBox(height: 16),
            Text(
              'no branches found',
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.orangeDark,
              ),
            ),
          ],
        ),
      );
    }

    return ListView.builder(
      itemCount: _branches.length,
      itemBuilder: (context, index) {
        final branch = _branches[index];
        final isCurrent = branch.isCurrent || branch.isHead;

        return ListTile(
          leading: Icon(
            isCurrent ? Icons.check_circle : Icons.circle_outlined,
            color: isCurrent
                ? CyberpunkColors.orangeBright
                : CyberpunkColors.orangeDark,
            size: 20,
          ),
          title: Text(
            branch.name,
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: isCurrent
                  ? CyberpunkColors.orangeBright
                  : CyberpunkColors.lightGray,
            ),
          ),
          trailing: branch.isHead
              ? Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 8,
                    vertical: 2,
                  ),
                  decoration: BoxDecoration(
                    border: Border.all(
                      color: CyberpunkColors.orangeDark,
                      width: 1,
                    ),
                    borderRadius: BorderRadius.circular(4),
                  ),
                  child: Text(
                    'detached',
                    style: CyberpunkTypography.bodySmall.copyWith(
                      color: CyberpunkColors.orangeDark,
                    ),
                  ),
                )
              : null,
          onTap: isCurrent ? null : () => _checkoutBranch(branch.name),
        );
      },
    );
  }
}
