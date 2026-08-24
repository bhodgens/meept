import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/providers.dart';
import '../theme/colors.dart';
import '../theme/typography.dart';

/// A daemon-side directory browser dialog.
///
/// Calls `GET /api/v1/filesystem/browse` to list subdirectories on the
/// daemon's filesystem. This works for both localhost (same machine) and
/// remote daemon connections — the listing is always from the daemon's
/// perspective.
///
/// The user navigates into directories by tapping them, goes up via the
/// parent button, and confirms the selection with "select here".
/// Returns the selected absolute path, or null if cancelled.
class DirectoryBrowserDialog extends ConsumerStatefulWidget {
  /// Optional starting path. Defaults to the daemon's home directory.
  final String? initialPath;

  const DirectoryBrowserDialog({super.key, this.initialPath});

  /// Show the dialog and return the selected path.
  static Future<String?> show(BuildContext context, {String? initialPath}) {
    return showDialog<String>(
      context: context,
      builder: (_) => DirectoryBrowserDialog(initialPath: initialPath),
    );
  }

  @override
  ConsumerState<DirectoryBrowserDialog> createState() =>
      _DirectoryBrowserDialogState();
}

class _DirectoryBrowserDialogState
    extends ConsumerState<DirectoryBrowserDialog> {
  String? _currentPath;
  List<Map<String, dynamic>> _entries = [];
  String? _parentPath;
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load(widget.initialPath);
  }

  Future<void> _load(String? path) async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final client = ref.read(sdkClientProvider);
      final result = await client.browseDirectories(path: path);
      if (!mounted) return;
      setState(() {
        _currentPath = result['path'] as String?;
        _parentPath = result['parent'] as String?;
        final rawEntries = result['entries'] as List? ?? [];
        _entries = rawEntries.map((e) => e as Map<String, dynamic>).toList();
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e.toString();
        _loading = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      backgroundColor: CyberpunkColors.darkGray,
      title: Text(
        'select project directory',
        style: CyberpunkTypography.bodyMedium.copyWith(
          fontFamily: 'SourceCodePro',
          fontSize: 14,
        ),
      ),
      content: SizedBox(
        width: double.maxFinite,
        height: 400,
        child: Column(
          children: [
            // Current path + navigation
            _buildPathBar(),
            const SizedBox(height: 8),
            // Directory listing
            Expanded(child: _buildListing()),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(null),
          child: const Text('cancel'),
        ),
        ElevatedButton(
          onPressed: _currentPath != null && !_loading
              ? () => Navigator.of(context).pop(_currentPath)
              : null,
          child: const Text('select here'),
        ),
      ],
    );
  }

  Widget _buildPathBar() {
    return Row(
      children: [
        // Up button
        IconButton(
          icon: const Icon(Icons.arrow_upward, size: 16),
          onPressed: _parentPath != null && !_loading
              ? () => _load(_parentPath)
              : null,
          tooltip: 'parent directory',
          iconSize: 16,
          padding: EdgeInsets.zero,
          constraints: const BoxConstraints(minWidth: 32, minHeight: 32),
        ),
        const SizedBox(width: 8),
        // Current path
        Expanded(
          child: Container(
            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
            decoration: BoxDecoration(
              color: CyberpunkColors.black,
              border: Border.all(color: CyberpunkColors.midGray),
              borderRadius: BorderRadius.circular(2),
            ),
            child: Text(
              _currentPath ?? '/',
              style: CyberpunkTypography.bodySmall.copyWith(
                fontFamily: 'SourceCodePro',
                fontSize: 11,
                color: CyberpunkColors.greenSuccess,
              ),
              overflow: TextOverflow.ellipsis,
            ),
          ),
        ),
        const SizedBox(width: 8),
        // Refresh button
        IconButton(
          icon: const Icon(Icons.refresh, size: 16),
          onPressed: _loading ? null : () => _load(_currentPath),
          tooltip: 'refresh',
          iconSize: 16,
          padding: EdgeInsets.zero,
          constraints: const BoxConstraints(minWidth: 32, minHeight: 32),
        ),
      ],
    );
  }

  Widget _buildListing() {
    if (_loading) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_error != null) {
      return Center(
        child: Text(
          _error!,
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.redAlert,
          ),
          textAlign: TextAlign.center,
        ),
      );
    }
    if (_entries.isEmpty) {
      return Center(
        child: Text(
          'no subdirectories',
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.midGray,
          ),
        ),
      );
    }
    return ListView.builder(
      itemCount: _entries.length,
      itemBuilder: (context, index) {
        final entry = _entries[index];
        final name = entry['name'] as String? ?? '';
        final path = entry['path'] as String? ?? '';
        return ListTile(
          dense: true,
          leading: Icon(
            Icons.folder,
            size: 16,
            color: CyberpunkColors.orangePrimary,
          ),
          title: Text(
            name,
            style: CyberpunkTypography.bodySmall.copyWith(
              fontFamily: 'SourceCodePro',
              fontSize: 12,
            ),
          ),
          onTap: () => _load(path),
          trailing: Text(
            'open',
            style: CyberpunkTypography.bodySmall.copyWith(
              fontSize: 10,
              color: CyberpunkColors.midGray,
            ),
          ),
        );
      },
    );
  }
}
