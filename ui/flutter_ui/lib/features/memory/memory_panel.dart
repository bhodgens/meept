import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../models/api_models.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import '../../widgets/error_banner.dart';

/// Memory panel - search and browse episodic and task memories
class MemoryPanel extends ConsumerStatefulWidget {
  const MemoryPanel({super.key});

  @override
  ConsumerState<MemoryPanel> createState() => _MemoryPanelState();
}

class _MemoryPanelState extends ConsumerState<MemoryPanel> {
  final _queryController = TextEditingController();
  List<MemoryResultModel> _memories = [];
  bool _isLoading = false;
  bool _hasSearched = false;
  String? _error;
  Timer? _debounceTimer;

  @override
  void initState() {
    super.initState();
    // Load recent memories on startup
    _loadRecentMemories();
  }

  @override
  void dispose() {
    _queryController.dispose();
    _debounceTimer?.cancel();
    super.dispose();
  }

  Future<void> _loadRecentMemories() async {
    setState(() {
      _isLoading = true;
      _hasSearched = true;
    });
    try {
      final client = ref.read(apiClientProvider);
      final data = await client.getRecentMemories(limit: 20);
      if (mounted) {
        setState(() {
          _memories = data
              .map((m) => MemoryResultModel.fromJson(m))
              .toList();
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

  Future<void> _searchMemories() async {
    final query = _queryController.text.trim();
    if (query.isEmpty) {
      _loadRecentMemories();
      return;
    }

    setState(() {
      _isLoading = true;
      _hasSearched = true;
    });
    try {
      final client = ref.read(apiClientProvider);
      final data = await client.queryMemory(query: query, limit: 20);
      if (mounted) {
        setState(() {
          _memories = data
              .map((m) => MemoryResultModel.fromJson(m))
              .toList();
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

  void _onQueryChanged(String query) {
    _debounceTimer?.cancel();
    _debounceTimer = Timer(const Duration(milliseconds: 500), () {
      if (query.trim().isNotEmpty) {
        _searchMemories();
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray.withValues(alpha: 0.5),
        border: Border(
          top: BorderSide(
            color: CyberpunkColors.orangePrimary.withValues(alpha: 0.3),
            width: 1,
          ),
        ),
      ),
      child: Column(
        children: [
          _buildHeader(),
          if (_error != null)
            ErrorBanner(message: _error!, onDismiss: _loadRecentMemories),
          Expanded(
            child: _isLoading && _memories.isEmpty
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
                : !_hasSearched
                    ? const Center(
                        child: Text(
                          'search or browse memories',
                          style: CyberpunkTypography.bodySmall,
                        ),
                      )
                    : _memories.isEmpty
                        ? Center(
                            child: Column(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                Icon(
                                  Icons.search_off,
                                  color: CyberpunkColors.midGray,
                                  size: 48,
                                ),
                                const SizedBox(height: 8),
                                Text(
                                  'no memories found',
                                  style: CyberpunkTypography.bodySmall.copyWith(
                                    color: CyberpunkColors.midGray,
                                  ),
                                ),
                              ],
                            ),
                          )
                        : ListView.builder(
                            itemCount: _memories.length,
                            itemBuilder: (context, index) {
                              return _buildMemoryItem(_memories[index]);
                            },
                          ),
          ),
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
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Icon(
                Icons.memory,
                color: CyberpunkColors.orangePrimary,
                size: 18,
              ),
              const SizedBox(width: 8),
              Text(
                'memory',
                style: CyberpunkTypography.label.copyWith(
                  color: CyberpunkColors.orangePrimary,
                ),
              ),
              const Spacer(),
              IconButton(
                icon: const Icon(Icons.refresh, size: 16),
                onPressed: _loadRecentMemories,
                padding: EdgeInsets.zero,
                constraints: const BoxConstraints(),
                tooltip: 'refresh',
              ),
            ],
          ),
          const SizedBox(height: 8),
          TextField(
            controller: _queryController,
            onChanged: _onQueryChanged,
            style: CyberpunkTypography.bodySmall.copyWith(
              fontFamily: 'SourceCodePro',
            ),
            decoration: InputDecoration(
              hintText: 'search memories...',
              hintStyle: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
              ),
              prefixIcon: const Icon(Icons.search, size: 18),
              border: OutlineInputBorder(
                borderRadius: BorderRadius.circular(8),
                borderSide: const BorderSide(color: CyberpunkColors.midGray),
              ),
              contentPadding: const EdgeInsets.symmetric(
                horizontal: 12,
                vertical: 8,
              ),
              filled: true,
              fillColor: CyberpunkColors.black,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildMemoryItem(MemoryResultModel memory) {
    return Container(
      margin: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: CyberpunkColors.black.withValues(alpha: 0.3),
        border: Border.all(
          color: _getRelevanceColor(memory.relevanceScore).withValues(alpha: 0.3),
          width: 1,
        ),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              _buildTypeIcon(memory.type),
              const SizedBox(width: 6),
              Expanded(
                child: Text(
                  memory.source.toLowerCase(),
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.lightGray,
                    fontSize: 10,
                  ),
                ),
              ),
              Text(
                '${(memory.relevanceScore * 100).toInt()}%',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: _getRelevanceColor(memory.relevanceScore),
                  fontWeight: FontWeight.bold,
                  fontSize: 10,
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Text(
            memory.content,
            style: CyberpunkTypography.bodySmall.copyWith(
              fontFamily: 'SourceCodePro',
              height: 1.4,
            ),
            maxLines: 4,
            overflow: TextOverflow.ellipsis,
          ),
          if (memory.category.isNotEmpty || memory.sessionId != null) ...[
            const SizedBox(height: 6),
            Wrap(
              spacing: 4,
              runSpacing: 4,
              children: [
                if (memory.category.isNotEmpty)
                  _buildChip(memory.category),
                if (memory.sessionId != null)
                  _buildChip('session'),
                if (memory.taskId != null)
                  _buildChip('task'),
              ],
            ),
          ],
        ],
      ),
    );
  }

  Widget _buildTypeIcon(String type) {
    IconData icon;
    Color color;
    switch (type.toLowerCase()) {
      case 'episodic':
        icon = Icons.chat_bubble_outline;
        color = CyberpunkColors.blueInfo;
        break;
      case 'task':
        icon = Icons.assignment_outlined;
        color = CyberpunkColors.orangePrimary;
        break;
      case 'personality':
        icon = Icons.person_outline;
        color = Colors.purple;
        break;
      default:
        icon = Icons.storage_outlined;
        color = CyberpunkColors.lightGray;
    }
    return Icon(icon, color: color, size: 14);
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

  Color _getRelevanceColor(double score) {
    if (score >= 0.8) return CyberpunkColors.greenSuccess;
    if (score >= 0.6) return CyberpunkColors.orangePrimary;
    if (score >= 0.4) return Colors.amber;
    return CyberpunkColors.redAlert;
  }
}
