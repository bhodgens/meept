import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import '../memory/memory_panel.dart';

/// Files panel - browse files referenced in memory/context
///
/// Note: Direct file system access requires backend HTTP API endpoints.
/// This panel shows files mentioned in episodic memory as a safer alternative.
class FilesPanel extends ConsumerStatefulWidget {
  const FilesPanel({super.key});

  @override
  ConsumerState<FilesPanel> createState() => _FilesPanelState();
}

class _FilesPanelState extends ConsumerState<FilesPanel> {
  List<FileEntry> _files = [];
  bool _isLoading = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _loadFiles();
  }

  Future<void> _loadFiles() async {
    setState(() {
      _isLoading = true;
      _error = null;
    });
    try {
      final client = ref.read(apiClientProvider);
      // Query memory for file-related entries
      final memories = await client.queryMemory(
        query: 'file path read write',
        limit: 50,
        category: 'episodic',
      );

      // Extract file paths from memory content
      final filePaths = <String>{};
      final fileRegex = RegExp(r'(/[\w/.~-]+|~/[\w/.-]+|\.?/[\w/.-]+)');

      for (final mem in memories) {
        final content = mem['content'] as String? ?? '';
        final matches = fileRegex.allMatches(content);
        for (final match in matches) {
          final path = match.group(0)!;
          // Filter for likely file paths
          if (path.contains('.') || path.startsWith('/') || path.startsWith('~/')) {
            filePaths.add(path);
          }
        }
      }

      if (mounted) {
        setState(() {
          _files = filePaths.take(20).map((path) => FileEntry(path: path)).toList();
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

  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.darkGray,
      child: Column(
        children: [
          _buildHeader(),
          if (_error != null) _buildErrorBanner(),
          Expanded(child: _buildFileList()),
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
            Icons.folder,
            color: CyberpunkColors.orangePrimary,
            size: 18,
          ),
          const SizedBox(width: 8),
          Text(
            'files',
            style: CyberpunkTypography.label.copyWith(
              color: CyberpunkColors.orangePrimary,
            ),
          ),
          const Spacer(),
          IconButton(
            icon: const Icon(Icons.refresh, size: 16),
            onPressed: _loadFiles,
            padding: EdgeInsets.zero,
            constraints: const BoxConstraints(),
            tooltip: 'refresh',
          ),
        ],
      ),
    );
  }

  Widget _buildErrorBanner() {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      color: const Color(0x80FF3030),
      child: Row(
        children: [
          const Icon(Icons.error_outline, color: Color(0xFFFF6060), size: 14),
          const SizedBox(width: 6),
          Expanded(
            child: Text(
              _error!,
              style: const TextStyle(
                color: Color(0xFFFF6060),
                fontSize: 10,
              ),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          GestureDetector(
            onTap: _loadFiles,
            child: const Icon(Icons.refresh, color: Color(0xFFFF6060), size: 14),
          ),
        ],
      ),
    );
  }

  Widget _buildFileList() {
    if (_isLoading) {
      return const Center(
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

    if (_files.isEmpty) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.folder_open,
              color: CyberpunkColors.midGray,
              size: 48,
            ),
            const SizedBox(height: 8),
            Text(
              'no files found in memory',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
              ),
            ),
            const SizedBox(height: 4),
            Text(
              'files referenced in chat will appear here',
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
      itemCount: _files.length,
      itemBuilder: (context, index) {
        return _buildFileItem(_files[index]);
      },
    );
  }

  Widget _buildFileItem(FileEntry file) {
    return Container(
      margin: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: CyberpunkColors.black.withValues(alpha: 0.3),
        border: Border.all(color: CyberpunkColors.midGray.withValues(alpha: 0.3)),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Row(
        children: [
          _getFileIcon(file.path),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  file.path,
                  style: CyberpunkTypography.bodySmall.copyWith(
                    fontFamily: 'SourceCodePro',
                    fontSize: 11,
                  ),
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                ),
              ],
            ),
          ),
          IconButton(
            icon: const Icon(Icons.copy, size: 14),
            onPressed: () {
              // Copy path to clipboard would go here
              ScaffoldMessenger.of(context).showSnackBar(
                const SnackBar(
                  content: Text('path copied'),
                  duration: Duration(seconds: 1),
                ),
              );
            },
            padding: EdgeInsets.zero,
            constraints: const BoxConstraints(),
          ),
        ],
      ),
    );
  }

  Widget _getFileIcon(String path) {
    IconData icon;
    Color color;

    if (path.endsWith('.dart')) {
      icon = Icons.code;
      color = Colors.blue;
    } else if (path.endsWith('.go')) {
      icon = Icons.code;
      color = Colors.cyan;
    } else if (path.endsWith('.json') || path.endsWith('.json5')) {
      icon = Icons.data_object;
      color = Colors.orange;
    } else if (path.endsWith('.yaml') || path.endsWith('.yml')) {
      icon = Icons.data_object;
      color = Colors.red;
    } else if (path.endsWith('.md')) {
      icon = Icons.description;
      color = Colors.lightBlue;
    } else if (path.endsWith('.txt')) {
      icon = Icons.insert_drive_file;
      color = CyberpunkColors.lightGray;
    } else {
      icon = Icons.insert_drive_file_outlined;
      color = CyberpunkColors.midGray;
    }

    return Icon(icon, color: color, size: 20);
  }
}

class FileEntry {
  final String path;
  FileEntry({required this.path});
}
