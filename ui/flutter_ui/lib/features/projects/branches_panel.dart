import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../styles/cyberpunk_theme.dart';
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

  @override
  void initState() {
    super.initState();
    _apiClient = ref.read(apiClientProvider);
    _loadBranches();
  }

  Future<void> _loadBranches() async {
    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      final data = await _apiClient
          .get<Map<String, dynamic>>('/projects/default/branches');
      final rawBranches = data['branches'] as List?;
      if (rawBranches == null || rawBranches.isEmpty) {
        if (mounted) {
          setState(() {
            _isLoading = false;
            _branches = [];
            _currentBranch = null;
          });
        }
        return;
      }

      final branches = rawBranches
          .map((b) => BranchInfo.fromJson(b as Map<String, dynamic>))
          .toList();
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

  Future<void> _checkoutBranch(String branchName) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => _buildCheckoutDialog(branchName),
    );

    if (confirmed == true && mounted) {
      try {
        final data = await _apiClient
            .post<Map<String, dynamic>>('/projects/default/checkout', data: {
          'branch': branchName,
        });
        final success = data['success'] as bool? ?? false;
        final message = data['message'] as String?;

        if (success && mounted) {
          // Refresh branch list so is_current flags are accurate
          await _loadBranches();
          if (mounted) {
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(
                content: Text(message ?? 'switched to branch $branchName'),
                backgroundColor: CyberpunkColors.orangePrimary,
              ),
            );
          }
        } else if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text(message ?? 'failed to switch branch'),
              backgroundColor: CyberpunkColors.orangeDark,
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
        style: CyberpunkTypography.headingSmall.copyWith(
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
    return Container(
      color: CyberpunkColors.darkGray,
      child: Column(
        children: [
          // Header
          Container(
            padding: const EdgeInsets.all(16),
            decoration: BoxDecoration(
              border: Border(
                bottom: BorderSide(
                  color: CyberpunkColors.orangePrimary.withOpacity(0.3),
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
                  style: CyberpunkTypography.headingSmall.copyWith(
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
    );
  }

  Widget _buildError() {
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(
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
            Icon(
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
                  : CyberpunkColors.textPrimary,
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

class BranchInfo {
  final String name;
  final bool isCurrent;
  final bool isHead;

  BranchInfo({
    required this.name,
    this.isCurrent = false,
    this.isHead = false,
  });

  factory BranchInfo.fromJson(Map<String, dynamic> json) {
    return BranchInfo(
      name: json['name'] as String? ?? '',
      isCurrent: json['is_current'] as bool? ?? false,
      isHead: json['is_head'] as bool? ?? false,
    );
  }
}
