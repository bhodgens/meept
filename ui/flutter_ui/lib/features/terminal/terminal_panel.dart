import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../models/api_models.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import '../../widgets/error_banner.dart';

/// Terminal panel - view and execute shell commands
///
/// Note: Requires backend HTTP API endpoints for terminal functionality.
/// Current implementation provides read-only command history view.
class TerminalPanel extends ConsumerStatefulWidget {
  const TerminalPanel({super.key});

  @override
  ConsumerState<TerminalPanel> createState() => _TerminalPanelState();
}

class _TerminalPanelState extends ConsumerState<TerminalPanel> {
  List<CommandEntry> _history = [];
  bool _isLoading = false;
  String? _error;
  final _commandController = TextEditingController();
  bool _isExecuting = false;

  @override
  void initState() {
    super.initState();
    _loadHistory();
  }

  Future<void> _loadHistory() async {
    setState(() => _isLoading = true);
    try {
      final client = ref.read(apiClientProvider);
      final data = await client.getTerminalHistory();
      final historyData = data['history'] as List? ?? [];
      if (mounted) {
        setState(() {
          _history = historyData
              .map((e) => CommandEntry.fromJson(e as Map<String, dynamic>))
              .toList();
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

  Future<void> _executeCommand() async {
    final cmd = _commandController.text.trim();
    if (cmd.isEmpty) return;

    setState(() => _isExecuting = true);
    try {
      final client = ref.read(apiClientProvider);
      final result = await client.executeCommand(cmd);

      if (mounted) {
        _commandController.clear();
        _loadHistory();

        final success = result['success'] as bool? ?? false;
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(success ? 'command executed' : 'command failed'),
            backgroundColor: success
                ? CyberpunkColors.greenSuccess
                : CyberpunkColors.redAlert,
            duration: const Duration(seconds: 2),
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('execution failed: $e'),
            backgroundColor: CyberpunkColors.redAlert,
          ),
        );
      }
    } finally {
      if (mounted) {
        setState(() => _isExecuting = false);
      }
    }
  }

  Future<void> _clearHistory() async {
    try {
      final client = ref.read(apiClientProvider);
      await client.clearTerminalHistory();
      if (mounted) {
        _loadHistory();
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('history cleared'),
            backgroundColor: CyberpunkColors.greenSuccess,
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('failed to clear: $e'),
            backgroundColor: CyberpunkColors.redAlert,
          ),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.darkGray,
      child: Column(
        children: [
          _buildHeader(),
          if (_error != null)
            ErrorBanner(message: _error!, onDismiss: _loadHistory),
          _buildCommandInput(),
          Expanded(child: _buildHistoryList()),
        ],
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
            Icons.terminal,
            color: CyberpunkColors.orangePrimary,
            size: 18,
          ),
          const SizedBox(width: 8),
          Text(
            'terminal',
            style: CyberpunkTypography.label.copyWith(
              color: CyberpunkColors.orangePrimary,
            ),
          ),
          const Spacer(),
          IconButton(
            icon: const Icon(Icons.delete_sweep, size: 16),
            onPressed: _clearHistory,
            tooltip: 'clear history',
          ),
          IconButton(
            icon: const Icon(Icons.refresh, size: 16),
            onPressed: _loadHistory,
            tooltip: 'refresh',
          ),
        ],
      ),
    );
  }

  Widget _buildCommandInput() {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: const BoxDecoration(
        border: Border(
          bottom: BorderSide(color: CyberpunkColors.midGray, width: 1),
        ),
      ),
      child: Row(
        children: [
          const Icon(Icons.arrow_right, color: CyberpunkColors.orangeBright, size: 18),
          const SizedBox(width: 8),
          Expanded(
            child: TextField(
              controller: _commandController,
              style: CyberpunkTypography.bodySmall.copyWith(
                fontFamily: 'SourceCodePro',
                fontSize: 11,
              ),
              decoration: const InputDecoration(
                hintText: 'enter command...',
                border: InputBorder.none,
                contentPadding: EdgeInsets.zero,
              ),
              onSubmitted: (_) => _executeCommand(),
              enabled: !_isExecuting,
            ),
          ),
          if (_isExecuting)
            const SizedBox(
              width: 16,
              height: 16,
              child: CircularProgressIndicator(
                strokeWidth: 2,
                valueColor: AlwaysStoppedAnimation<Color>(CyberpunkColors.orangePrimary),
              ),
            )
          else
            IconButton(
              icon: const Icon(Icons.send, size: 16),
              onPressed: _executeCommand,
              tooltip: 'execute',
            ),
        ],
      ),
    );
  }

  Widget _buildHistoryList() {
    if (_isLoading) {
      return const Center(
        child: SizedBox(
          width: 20,
          height: 20,
          child: CircularProgressIndicator(
            strokeWidth: 2,
            valueColor: AlwaysStoppedAnimation<Color>(CyberpunkColors.orangePrimary),
          ),
        ),
      );
    }

    if (_history.isEmpty) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.terminal, color: CyberpunkColors.midGray, size: 48),
            const SizedBox(height: 8),
            Text(
              'no command history',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
              ),
            ),
            const SizedBox(height: 4),
            Text(
              'executed commands will appear here',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
                fontSize: 10,
              ),
            ),
          ],
        ),
      );
    }

    return ListView.builder(
      itemCount: _history.length,
      itemBuilder: (context, index) => _buildHistoryItem(_history[index]),
    );
  }

  Widget _buildHistoryItem(CommandEntry entry) {
    return Container(
      margin: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: CyberpunkColors.black.withValues(alpha: 0.3),
        border: Border.all(
          color: entry.success
              ? CyberpunkColors.greenSuccess.withValues(alpha: 0.3)
              : CyberpunkColors.redAlert.withValues(alpha: 0.3),
          width: 1,
        ),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(
                entry.success ? Icons.check_circle : Icons.error,
                size: 14,
                color: entry.success
                    ? CyberpunkColors.greenSuccess
                    : CyberpunkColors.redAlert,
              ),
              const SizedBox(width: 6),
              Expanded(
                child: Text(
                  entry.command,
                  style: CyberpunkTypography.bodySmall.copyWith(
                    fontFamily: 'SourceCodePro',
                    fontSize: 11,
                    color: CyberpunkColors.lightGray,
                  ),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              const SizedBox(width: 8),
              Text(
                'exit: ${entry.exitCode}',
                style: CyberpunkTypography.bodySmall.copyWith(
                  fontFamily: 'SourceCodePro',
                  fontSize: 9,
                  color: entry.success
                      ? CyberpunkColors.greenSuccess
                      : CyberpunkColors.redAlert,
                ),
              ),
            ],
          ),
          if (entry.output.isNotEmpty) ...[
            const SizedBox(height: 6),
            Container(
              padding: const EdgeInsets.all(8),
              decoration: BoxDecoration(
                color: CyberpunkColors.midGray.withValues(alpha: 0.1),
                borderRadius: BorderRadius.circular(4),
              ),
              child: Text(
                entry.output,
                style: CyberpunkTypography.bodySmall.copyWith(
                  fontFamily: 'SourceCodePro',
                  fontSize: 9,
                  color: CyberpunkColors.lightGray,
                ),
                maxLines: 3,
                overflow: TextOverflow.ellipsis,
              ),
            ),
          ],
          const SizedBox(height: 4),
          Row(
            children: [
              Icon(Icons.access_time, size: 10, color: CyberpunkColors.midGray),
              const SizedBox(width: 4),
              Text(
                _formatTime(entry.timestamp),
                style: CyberpunkTypography.bodySmall.copyWith(
                  fontFamily: 'SourceCodePro',
                  fontSize: 9,
                  color: CyberpunkColors.midGray,
                ),
              ),
              if (entry.workingDir.isNotEmpty) ...[
                const SizedBox(width: 8),
                Icon(Icons.folder, size: 10, color: CyberpunkColors.midGray),
                const SizedBox(width: 4),
                Expanded(
                  child: Text(
                    entry.workingDir,
                    style: CyberpunkTypography.bodySmall.copyWith(
                      fontFamily: 'SourceCodePro',
                      fontSize: 9,
                      color: CyberpunkColors.midGray,
                    ),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ],
            ],
          ),
        ],
      ),
    );
  }

  String _formatTime(DateTime dt) {
    final hour = dt.hour.toString().padLeft(2, '0');
    final minute = dt.minute.toString().padLeft(2, '0');
    final second = dt.second.toString().padLeft(2, '0');
    return '$hour:$minute:$second';
  }

  @override
  void dispose() {
    _commandController.dispose();
    super.dispose();
  }
}
