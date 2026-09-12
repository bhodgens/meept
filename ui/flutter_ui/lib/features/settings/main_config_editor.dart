import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../providers/providers.dart';
import '../../services/sdk_client.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';

// Widget keys, exposed so tests can target each control without leaning on
// button-order or color. The settings-panel tests import these.
const Key mainConfigPathKey = ValueKey('main-config-path');
const Key mainConfigTextKey = ValueKey('main-config-text');
const Key mainConfigSaveKey = ValueKey('main-config-save');
const Key mainConfigRevertKey = ValueKey('main-config-revert');
const Key mainConfigReloadKey = ValueKey('main-config-reload');
const Key mainConfigDirtyKey = ValueKey('main-config-dirty');
const Key mainConfigErrorKey = ValueKey('main-config-error');
const Key mainConfigNoticeKey = ValueKey('main-config-notice');
const Key mainConfigReadOnlyNoteKey = ValueKey('main-config-readonly-note');

/// Raw JSON5 editor for the daemon's main config file (meept.json5).
///
/// Loads via GET /api/v1/config/main and writes the whole file back with
/// POST /api/v1/config/main. The daemon validates the text as JSON5 and
/// accepts writes from loopback clients only; both failure modes are shown
/// in place, and a failed save NEVER discards the user's text.
///
/// The file is the daemon's own config, not the client's: a saved change
/// takes effect only after the daemon restarts, and the success notice says
/// so.
class MainConfigEditor extends ConsumerStatefulWidget {
  const MainConfigEditor({super.key});

  @override
  ConsumerState<MainConfigEditor> createState() => _MainConfigEditorState();
}

class _MainConfigEditorState extends ConsumerState<MainConfigEditor> {
  late final SdkApiClient _client;
  final TextEditingController _controller = TextEditingController();

  MainConfigFile? _file;

  /// The content last fetched from (or successfully saved to) the daemon.
  /// The dirty flag compares the editor text against this.
  String _loadedContent = '';

  bool _isLoading = true;
  bool _isSaving = false;
  String? _error;
  String? _notice;

  bool get _isDirty =>
      !_isLoading && _controller.text != _loadedContent;

  @override
  void initState() {
    super.initState();
    _client = ref.read(sdkClientProvider);
    _load();
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() {
      _isLoading = true;
      _error = null;
      _notice = null;
    });
    try {
      final file = await _client.getMainConfig();
      if (!mounted) return;
      setState(() {
        _file = file;
        _loadedContent = file.content;
        _controller.text = file.content;
        _isLoading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e.toString();
        _isLoading = false;
      });
    }
  }

  Future<void> _save() async {
    final text = _controller.text;
    setState(() {
      _isSaving = true;
      _error = null;
      _notice = null;
    });
    try {
      await _client.saveMainConfig(text);
      if (!mounted) return;
      setState(() {
        _isSaving = false;
        _loadedContent = text;
        _notice =
            'saved to ${_file?.path ?? 'the main config file'}. '
            'restart the daemon to load the change.';
      });
    } on SdkApiException catch (e) {
      // Keep the editor text untouched on every failure path: the user's
      // edits are the only copy of their work.
      if (!mounted) return;
      setState(() {
        _isSaving = false;
        if (e.statusCode == 400) {
          // The daemon's JSON5 parser message, verbatim, so the user can
          // see the exact line/column it rejected.
          _error = 'the daemon rejected the json5: ${e.message}';
        } else if (e.statusCode == 403) {
          _error =
              'the daemon accepts config writes only from the same host '
              '(loopback), so meept.json5 cannot be saved from this gui. '
              'your edits are still here.';
        } else {
          _error = 'save failed: ${e.message}';
        }
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _isSaving = false;
        _error = 'save failed: $e';
      });
    }
  }

  void _revert() {
    setState(() {
      _controller.text = _loadedContent;
      _error = null;
      _notice = null;
    });
  }

  Future<void> _reload() async {
    if (_isDirty) {
      final proceed = await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          backgroundColor: CyberpunkColors.darkGray,
          title: const Text(
            'discard unsaved edits?',
            style: CyberpunkTypography.bodyMedium,
          ),
          content: const Text(
            'reloading fetches meept.json5 from the daemon again and '
            'discards the edits you have not saved.',
            style: CyberpunkTypography.bodySmall,
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('cancel'),
            ),
            TextButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('discard and reload'),
            ),
          ],
        ),
      );
      if (proceed != true) return;
    }
    await _load();
  }

  InputDecoration get _decoration => InputDecoration(
    hintText: '// json5 configuration...',
    hintStyle: TextStyle(
      color: CyberpunkColors.midGray,
      fontFamily: 'SourceCodePro',
    ),
    border: InputBorder.none,
    contentPadding: const EdgeInsets.all(12),
  );

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        border: Border(bottom: BorderSide(color: CyberpunkColors.midGray)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _buildHeader(),
          const SizedBox(height: 6),
          _buildPathLine(),
          const SizedBox(height: 8),
          if (_file?.writable == false) _buildReadOnlyNote(),
          if (_error != null) _buildError(),
          if (_notice != null) _buildNotice(),
          const SizedBox(height: 8),
          _buildEditor(),
        ],
      ),
    );
  }

  Widget _buildHeader() {
    final writable = _file?.writable ?? false;
    return Row(
      children: [
        Icon(
          Icons.description_outlined,
          color: CyberpunkColors.orangePrimary,
          size: 16,
        ),
        const SizedBox(width: 8),
        Text(
          'main daemon config',
          style: CyberpunkTypography.label.copyWith(
            color: CyberpunkColors.orangePrimary,
          ),
        ),
        if (_isDirty)
          Padding(
            key: mainConfigDirtyKey,
            padding: const EdgeInsets.only(left: 8),
            child: Text(
              'unsaved changes',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.orangeBright,
                fontSize: 10,
              ),
            ),
          ),
        const Spacer(),
        if (_isSaving)
          Padding(
            padding: const EdgeInsets.only(right: 8),
            child: SizedBox(
              width: 14,
              height: 14,
              child: CircularProgressIndicator(
                strokeWidth: 2,
                valueColor: AlwaysStoppedAnimation<Color>(
                  CyberpunkColors.orangePrimary,
                ),
              ),
            ),
          ),
        TextButton.icon(
          key: mainConfigReloadKey,
          onPressed: _isLoading || _isSaving ? null : _reload,
          icon: const Icon(Icons.refresh, size: 14),
          label: const Text('reload'),
          style: TextButton.styleFrom(
            foregroundColor: CyberpunkColors.lightGray,
            padding: const EdgeInsets.symmetric(horizontal: 8),
          ),
        ),
        TextButton.icon(
          key: mainConfigRevertKey,
          onPressed: _isDirty && !_isSaving ? _revert : null,
          icon: const Icon(Icons.undo, size: 14),
          label: const Text('revert'),
          style: TextButton.styleFrom(
            foregroundColor: CyberpunkColors.lightGray,
            padding: const EdgeInsets.symmetric(horizontal: 8),
          ),
        ),
        const SizedBox(width: 4),
        ElevatedButton.icon(
          key: mainConfigSaveKey,
          onPressed: (_isDirty && writable && !_isSaving) ? _save : null,
          icon: const Icon(Icons.save, size: 14),
          label: const Text('save'),
          style: ElevatedButton.styleFrom(
            backgroundColor: CyberpunkColors.greenSuccess,
            foregroundColor: CyberpunkColors.black,
            disabledBackgroundColor: CyberpunkColors.midGray.withValues(
              alpha: 0.3,
            ),
            disabledForegroundColor: CyberpunkColors.midGray,
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
          ),
        ),
      ],
    );
  }

  Widget _buildPathLine() {
    return Row(
      children: [
        Icon(Icons.folder_open, size: 12, color: CyberpunkColors.midGray),
        const SizedBox(width: 6),
        Expanded(
          child: Text(
            _file?.path ?? '',
            key: mainConfigPathKey,
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.lightGray,
              fontFamily: 'SourceCodePro',
              fontSize: 10,
            ),
            overflow: TextOverflow.ellipsis,
          ),
        ),
      ],
    );
  }

  Widget _buildReadOnlyNote() {
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Text(
        'this file is not writable by the daemon user, so it can only be '
        'viewed here.',
        key: mainConfigReadOnlyNoteKey,
        style: CyberpunkTypography.bodySmall.copyWith(
          color: CyberpunkColors.orangeBright,
          fontSize: 10,
        ),
      ),
    );
  }

  Widget _buildError() {
    return Container(
      key: mainConfigErrorKey,
      width: double.infinity,
      margin: const EdgeInsets.only(bottom: 8),
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(
        color: CyberpunkColors.redAlert.withValues(alpha: 0.12),
        border: Border.all(
          color: CyberpunkColors.redAlert.withValues(alpha: 0.5),
        ),
        borderRadius: BorderRadius.circular(6),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(
            Icons.error_outline,
            size: 14,
            color: CyberpunkColors.redAlert,
          ),
          const SizedBox(width: 6),
          Expanded(
            child: Text(
              _error!,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.redAlert,
                fontSize: 10,
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildNotice() {
    return Container(
      key: mainConfigNoticeKey,
      width: double.infinity,
      margin: const EdgeInsets.only(bottom: 8),
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(
        color: CyberpunkColors.greenSuccess.withValues(alpha: 0.12),
        border: Border.all(
          color: CyberpunkColors.greenSuccess.withValues(alpha: 0.5),
        ),
        borderRadius: BorderRadius.circular(6),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(
            Icons.check_circle_outline,
            size: 14,
            color: CyberpunkColors.greenSuccess,
          ),
          const SizedBox(width: 6),
          Expanded(
            child: Text(
              _notice!,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.greenSuccess,
                fontSize: 10,
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildEditor() {
    if (_isLoading) {
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
    return SizedBox(
      height: 300,
      child: Container(
        decoration: BoxDecoration(
          color: CyberpunkColors.black,
          borderRadius: BorderRadius.circular(8),
          border: Border.all(color: CyberpunkColors.midGray),
        ),
        child: TextField(
          key: mainConfigTextKey,
          controller: _controller,
          readOnly: _file?.writable == false,
          maxLines: null,
          expands: true,
          textAlignVertical: TextAlignVertical.top,
          style: CyberpunkTypography.bodySmall.copyWith(
            fontFamily: 'SourceCodePro',
            height: 1.4,
          ),
          onChanged: (_) {
            // Rebuild so the dirty indicator and save-enabled state track
            // the text. Clear a stale save notice on the next edit.
            setState(() => _notice = null);
          },
          decoration: _decoration,
        ),
      ),
    );
  }
}
