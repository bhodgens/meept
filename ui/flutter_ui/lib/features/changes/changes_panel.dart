import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../providers/providers.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../widgets/error_banner.dart';
import 'changes_models.dart';

/// Changes panel — human review surface for staged file diffs.
///
/// Two tabs mirror the daemon's pending-changes registry (leaf 05) and
/// the changes journal (leaf 06):
///
///  * `pending` — cards with the file path + plain diff body, with
///    accept/reject buttons (`POST /pending-changes/{id}/accept|reject`).
///  * `journal` — applied-change log with per-entry revert buttons
///    (`POST /changes/journal/{id}/revert`).
///
/// Drift (409) and size-cap (400) failures surface the daemon's error
/// message in a snackbar.  All strings lowercase (TUI parity mandate).
///
/// [sessionId] scopes the pending list; when null the panel falls back to
/// [activeSessionProvider] (same pattern as the chat tab).
class ChangesPanel extends ConsumerStatefulWidget {
  final String? sessionId;

  const ChangesPanel({super.key, this.sessionId});

  @override
  ConsumerState<ChangesPanel> createState() => _ChangesPanelState();
}

class _ChangesPanelState extends ConsumerState<ChangesPanel> {
  List<PendingChange> _pending = [];
  List<ChangeJournalEntry> _journal = [];
  bool _loadingPending = true;
  bool _loadingJournal = true;
  String? _pendingError;
  String? _journalError;
  late final FocusNode _keyboardFocusNode;

  @override
  void initState() {
    super.initState();
    _keyboardFocusNode = FocusNode();
    _loadPending();
    _loadJournal();
  }

  @override
  void dispose() {
    _keyboardFocusNode.dispose();
    super.dispose();
  }

  /// Session scope for pending changes; null when nothing is selected.
  String? _effectiveSessionId() {
    final explicit = widget.sessionId;
    if (explicit != null && explicit.isNotEmpty) return explicit;
    return ref.read(activeSessionProvider)?.id;
  }

  Future<void> _loadPending() async {
    setState(() {
      _loadingPending = true;
      _pendingError = null;
    });
    final sid = _effectiveSessionId();
    if (sid == null) {
      if (mounted) {
        setState(() {
          _pending = [];
          _loadingPending = false;
        });
      }
      return;
    }
    try {
      final client = ref.read(sdkClientProvider);
      final raw = await client.listPendingChanges(sid);
      if (!mounted) return;
      setState(() {
        _pending = raw.map((m) => PendingChange.fromJson(m)).toList();
        _loadingPending = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _pendingError = _friendlyError(e);
        _loadingPending = false;
      });
    }
  }

  Future<void> _loadJournal() async {
    setState(() {
      _loadingJournal = true;
      _journalError = null;
    });
    try {
      final client = ref.read(sdkClientProvider);
      // Scope to the active session when one is selected; otherwise show
      // the global journal (most recent first).
      final sid = _effectiveSessionId();
      final raw = await client.listChangesJournal(sessionId: sid, limit: 50);
      if (!mounted) return;
      setState(() {
        _journal = raw.map((m) => ChangeJournalEntry.fromJson(m)).toList();
        _loadingJournal = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _journalError = _friendlyError(e);
        _loadingJournal = false;
      });
    }
  }

  Future<void> _accept(PendingChange change) async {
    try {
      final client = ref.read(sdkClientProvider);
      await client.acceptPendingChange(change.id);
      if (!mounted) return;
      _snackbar('applied: ${change.filePath}', CyberpunkColors.greenSuccess);
      await _loadPending();
    } catch (e) {
      if (!mounted) return;
      _snackbar(_friendlyError(e), CyberpunkColors.redAlert);
    }
  }

  Future<void> _reject(PendingChange change) async {
    try {
      final client = ref.read(sdkClientProvider);
      await client.rejectPendingChange(change.id);
      if (!mounted) return;
      _snackbar('rejected: ${change.filePath}', CyberpunkColors.yellowWarning);
      await _loadPending();
    } catch (e) {
      if (!mounted) return;
      _snackbar(_friendlyError(e), CyberpunkColors.redAlert);
    }
  }

  Future<void> _revert(ChangeJournalEntry entry) async {
    try {
      final client = ref.read(sdkClientProvider);
      final res = await client.revertJournalEntry(entry.id);
      if (!mounted) return;
      final restored = res['restored_path'] as String? ?? entry.filePath;
      _snackbar('reverted: $restored', CyberpunkColors.greenSuccess);
      await _loadJournal();
    } catch (e) {
      if (!mounted) return;
      _snackbar(_friendlyError(e), CyberpunkColors.redAlert);
    }
  }

  /// SdkApiException already carries the daemon's error message for
  /// 409/400 responses; everything else falls back to toString().
  String _friendlyError(Object e) {
    final text = e.toString();
    return text.replaceFirst('SdkApiException: ', '');
  }

  void _snackbar(String message, Color color) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          message.toLowerCase(),
          style: CyberpunkTypography.bodySmall.copyWith(
            color: color,
            fontFamily: 'SourceCodePro',
          ),
        ),
        backgroundColor: CyberpunkColors.darkGray,
        duration: const Duration(seconds: 3),
      ),
    );
  }

  void _closePanel() {
    context.go('/');
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
      child: DefaultTabController(
        length: 2,
        child: Container(
          color: CyberpunkColors.black,
          child: Column(
            children: [
              _buildHeader(),
              Expanded(
                child: TabBarView(
                  children: [
                    _buildPendingTab(),
                    _buildJournalTab(),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildHeader() {
    return Container(
      padding: const EdgeInsets.fromLTRB(12, 12, 12, 0),
      decoration: BoxDecoration(
        border: Border(
          bottom: BorderSide(color: CyberpunkColors.midGray, width: 1),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              GestureDetector(
                onTap: _closePanel,
                child: Icon(
                  Icons.arrow_back,
                  color: CyberpunkColors.orangePrimary,
                  size: 18,
                ),
              ),
              const SizedBox(width: 8),
              Icon(
                Icons.compare_arrows,
                color: CyberpunkColors.orangePrimary,
                size: 18,
              ),
              const SizedBox(width: 8),
              Text(
                'changes',
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
            ],
          ),
          TabBar(
            labelColor: CyberpunkColors.orangePrimary,
            unselectedLabelColor: CyberpunkColors.midGray,
            indicatorColor: CyberpunkColors.orangePrimary,
            labelStyle: CyberpunkTypography.label,
            tabs: const [
              Tab(text: 'pending'),
              Tab(text: 'journal'),
            ],
          ),
        ],
      ),
    );
  }

  // ------------------------------------------------------------------
  // Pending tab
  // ------------------------------------------------------------------

  Widget _buildPendingTab() {
    if (_pendingError != null) {
      return Column(
        children: [
          ErrorBanner(message: _pendingError!, onRetry: _loadPending),
          const Expanded(child: SizedBox.shrink()),
        ],
      );
    }
    if (_loadingPending) return _spinner();
    if (_effectiveSessionId() == null) {
      return _emptyState(
        icon: Icons.touch_app,
        message: 'select a session to review pending changes',
      );
    }
    if (_pending.isEmpty) {
      return _emptyState(
        icon: Icons.check_circle_outline,
        message: 'no pending changes',
        hint: 'staged diffs will appear here for review',
      );
    }
    return ListView.builder(
      padding: const EdgeInsets.all(8),
      itemCount: _pending.length,
      itemBuilder: (context, index) => _buildPendingCard(_pending[index]),
    );
  }

  Widget _buildPendingCard(PendingChange change) {
    return Container(
      margin: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray.withValues(alpha: 0.5),
        border: Border.all(
          color: CyberpunkColors.orangePrimary.withValues(alpha: 0.3),
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
                Icons.description_outlined,
                color: CyberpunkColors.orangePrimary,
                size: 14,
              ),
              const SizedBox(width: 6),
              Expanded(
                child: Text(
                  change.filePath.toLowerCase(),
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.lightGray,
                    fontFamily: 'SourceCodePro',
                    fontWeight: FontWeight.bold,
                  ),
                ),
              ),
              if (change.expiresAt != null)
                Text(
                  'expires ${_formatTime(change.expiresAt!)}',
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.midGray,
                    fontSize: 9,
                  ),
                ),
            ],
          ),
          const SizedBox(height: 8),
          // Plain mono diff body — no syntax highlighting by design
          // (TUI parity: plain diff pager).
          Container(
            width: double.infinity,
            padding: const EdgeInsets.all(8),
            decoration: BoxDecoration(
              color: CyberpunkColors.black.withValues(alpha: 0.6),
              borderRadius: BorderRadius.circular(4),
            ),
            child: SelectableText(
              change.diff.isEmpty ? '(empty diff)' : change.diff,
              style: CyberpunkTypography.code.copyWith(
                fontSize: 11,
                height: 1.4,
                color: CyberpunkColors.terminalGreen,
              ),
            ),
          ),
          const SizedBox(height: 8),
          Row(
            mainAxisAlignment: MainAxisAlignment.end,
            children: [
              TextButton(
                onPressed: () => _reject(change),
                style: TextButton.styleFrom(
                  foregroundColor: CyberpunkColors.redAlert,
                  padding: const EdgeInsets.symmetric(
                    horizontal: 12,
                    vertical: 4,
                  ),
                ),
                child: const Text('reject'),
              ),
              const SizedBox(width: 8),
              TextButton(
                onPressed: () => _accept(change),
                style: TextButton.styleFrom(
                  foregroundColor: CyberpunkColors.greenSuccess,
                  padding: const EdgeInsets.symmetric(
                    horizontal: 12,
                    vertical: 4,
                  ),
                ),
                child: const Text('accept'),
              ),
            ],
          ),
        ],
      ),
    );
  }

  // ------------------------------------------------------------------
  // Journal tab
  // ------------------------------------------------------------------

  Widget _buildJournalTab() {
    if (_journalError != null) {
      return Column(
        children: [
          ErrorBanner(message: _journalError!, onRetry: _loadJournal),
          const Expanded(child: SizedBox.shrink()),
        ],
      );
    }
    if (_loadingJournal) return _spinner();
    if (_journal.isEmpty) {
      return _emptyState(
        icon: Icons.history,
        message: 'no applied changes',
        hint: 'accepted diffs are journaled here and can be reverted',
      );
    }
    return ListView.builder(
      padding: const EdgeInsets.all(8),
      itemCount: _journal.length,
      itemBuilder: (context, index) => _buildJournalRow(_journal[index]),
    );
  }

  Widget _buildJournalRow(ChangeJournalEntry entry) {
    return Container(
      margin: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: CyberpunkColors.black.withValues(alpha: 0.3),
        border: Border.all(
          color: CyberpunkColors.midGray.withValues(alpha: 0.3),
          width: 1,
        ),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Row(
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  entry.filePath.toLowerCase(),
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.lightGray,
                    fontFamily: 'SourceCodePro',
                    fontWeight: FontWeight.bold,
                  ),
                ),
                const SizedBox(height: 4),
                Text(
                  _journalMeta(entry),
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.midGray,
                    fontSize: 9,
                  ),
                ),
              ],
            ),
          ),
          TextButton(
            onPressed: () => _revert(entry),
            style: TextButton.styleFrom(
              foregroundColor: CyberpunkColors.yellowWarning,
              padding: const EdgeInsets.symmetric(
                horizontal: 12,
                vertical: 4,
              ),
            ),
            child: const Text('revert'),
          ),
        ],
      ),
    );
  }

  String _journalMeta(ChangeJournalEntry entry) {
    final parts = <String>[
      if (entry.appliedAt != null) 'applied ${_formatTime(entry.appliedAt!)}',
      if (entry.postSha.isNotEmpty) 'sha ${_shortSha(entry.postSha)}',
      '${_formatBytes(entry.preImageSize)} pre-image',
      '${entry.changeIds.length} change(s)',
    ];
    return parts.join(' · ');
  }

  // ------------------------------------------------------------------
  // Shared bits
  // ------------------------------------------------------------------

  Widget _spinner() {
    return Center(
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
    );
  }

  Widget _emptyState({
    required IconData icon,
    required String message,
    String? hint,
  }) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, color: CyberpunkColors.midGray, size: 48),
          const SizedBox(height: 8),
          Text(
            message,
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.midGray,
            ),
          ),
          if (hint != null) ...[
            const SizedBox(height: 4),
            Text(
              hint,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
                fontSize: 10,
              ),
            ),
          ],
        ],
      ),
    );
  }

  static String _formatTime(DateTime t) {
    final local = t.toLocal();
    final hh = local.hour.toString().padLeft(2, '0');
    final mm = local.minute.toString().padLeft(2, '0');
    final mo = local.month.toString().padLeft(2, '0');
    final dd = local.day.toString().padLeft(2, '0');
    return '${local.year}-$mo-$dd $hh:$mm';
  }

  static String _shortSha(String sha) =>
      sha.length > 8 ? sha.substring(0, 8) : sha;

  static String _formatBytes(int bytes) {
    if (bytes < 1024) return '${bytes}b';
    if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)}kb';
    return '${(bytes / (1024 * 1024)).toStringAsFixed(1)}mb';
  }
}
