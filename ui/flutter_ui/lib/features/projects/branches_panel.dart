import 'package:flutter/material.dart';
import '../../styles/cyberpunk_theme.dart';

/// BranchesPanel displays git branches for the current project.
///
/// Allows switching branches and viewing branch status.
class BranchesPanel extends StatefulWidget {
  const BranchesPanel({super.key});

  @override
  State<BranchesPanel> createState() => _BranchesPanelState();
}

class _BranchesPanelState extends State<BranchesPanel> {
  List<BranchInfo> _branches = [];
  String? _currentBranch;
  bool _isLoading = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _loadBranches();
  }

  Future<void> _loadBranches() async {
    setState(() {
      _isLoading = true;
      _error = null;
    });

    // TODO: Wire to API - GET /api/v1/projects/{id}/branches
    // For now, show placeholder
    await Future.delayed(const Duration(milliseconds: 500));

    setState(() {
      _isLoading = false;
      _branches = [
        // Sample data - replace with API call
        BranchInfo(name: 'main', isCurrent: true, isHead: false),
        BranchInfo(name: 'develop', isCurrent: false, isHead: false),
      ];
      _currentBranch = 'main';
    });
  }

  Future<void> _checkoutBranch(String branchName) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => _buildCheckoutDialog(branchName),
    );

    if (confirmed == true) {
      // TODO: Wire to API - POST /api/v1/projects/{id}/checkout
      setState(() {
        _currentBranch = branchName;
      });
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('switched to branch $branchName'),
            backgroundColor: CyberpunkColors.orangePrimary,
          ),
        );
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
}
