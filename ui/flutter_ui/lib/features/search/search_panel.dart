import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:go_router/go_router.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/api_models.dart';
import '../../services/api_client.dart';
import '../../providers/providers.dart';

/// SearchPanel provides full-text search across sessions, tasks, memories, and plans.
///
/// Features debounced input and grouped results by type.
class SearchPanel extends ConsumerStatefulWidget {
  const SearchPanel({super.key});

  @override
  ConsumerState<SearchPanel> createState() => _SearchPanelState();
}

class _SearchPanelState extends ConsumerState<SearchPanel> {
  final _searchController = TextEditingController();
  final _debouncer = _Debouncer(delay: const Duration(milliseconds: 300));

  late final ApiClient _apiClient;
  late final VoidCallback _searchListener;
  late final FocusNode _keyboardFocusNode;

  List<SearchResultItem> _results = [];
  bool _isSearching = false;
  String _lastQuery = '';
  SearchScope _scope = SearchScope.all;
  String? _error;
  bool _showClear = false;

  @override
  void initState() {
    super.initState();
    _apiClient = ref.read(apiClientProvider);
    _keyboardFocusNode = FocusNode();
    _searchListener = () {
      final hasText = _searchController.text.isNotEmpty;
      if (hasText != _showClear) {
        setState(() => _showClear = hasText);
      }
    };
    _searchController.addListener(_searchListener);
  }

  @override
  void dispose() {
    _keyboardFocusNode.dispose();
    _searchController.removeListener(_searchListener);
    _searchController.dispose();
    _debouncer.dispose();
    super.dispose();
  }

  Future<void> _search(String query) async {
    if (query.isEmpty) {
      setState(() {
        _results = [];
        _lastQuery = '';
        _error = null;
      });
      return;
    }

    setState(() {
      _isSearching = true;
      _lastQuery = query;
      _error = null;
    });

    try {
      final searchResults = await _apiClient.search(
        query: query,
        scope: _scope,
      );

      if (!mounted) return;

      final List<SearchResultItem> parsed = searchResults.results;

      setState(() {
        _isSearching = false;
        _results = parsed;
      });
    } on ApiClientException catch (e) {
      if (!mounted) return;
      setState(() {
        _isSearching = false;
        _error = e.message;
        // Keep previous results on network failure
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _isSearching = false;
        _error = 'search failed: $e';
      });
    }
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
      child: Container(
      color: CyberpunkColors.darkGray,
      child: Column(
        children: [
          // Header with search input
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
            child: Column(
              children: [
                Row(
                  children: [
                    GestureDetector(
                      onTap: _closePanel,
                      child: const Icon(
                        Icons.arrow_back,
                        color: CyberpunkColors.orangePrimary,
                        size: 18,
                      ),
                    ),
                    const SizedBox(width: 8),
                    const Icon(
                      Icons.search,
                      color: CyberpunkColors.orangeBright,
                      size: 24,
                    ),
                    const SizedBox(width: 12),
                    Text(
                      'search',
                      style: CyberpunkTypography.headlineSmall.copyWith(
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
                const SizedBox(height: 12),
                // Search input
                Container(
                  decoration: BoxDecoration(
                    border: Border.all(
                      color: CyberpunkColors.orangePrimary.withValues(alpha: 0.3),
                      width: 1,
                    ),
                    borderRadius: BorderRadius.circular(4),
                  ),
                  child: TextField(
                    controller: _searchController,
                    style: CyberpunkTypography.bodyMedium,
                    decoration: InputDecoration(
                      hintText: 'search query...',
                      hintStyle: CyberpunkTypography.bodyMedium.copyWith(
                        color: CyberpunkColors.orangeDark,
                      ),
                      border: InputBorder.none,
                      contentPadding: const EdgeInsets.symmetric(
                        horizontal: 12,
                        vertical: 8,
                      ),
                      suffixIcon: _showClear
                          ? IconButton(
                              icon: const Icon(
                                Icons.clear,
                                size: 18,
                                color: CyberpunkColors.orangeDark,
                              ),
                              onPressed: () {
                                _searchController.clear();
                                _search('');
                              },
                            )
                          : null,
                    ),
                    onChanged: (value) {
                      _debouncer.run(() => _search(value));
                    },
                    onSubmitted: _search,
                  ),
                ),
                const SizedBox(height: 8),
                // Scope selector
                Row(
                  children: [
                    Text(
                      'scope:',
                      style: CyberpunkTypography.bodySmall.copyWith(
                        color: CyberpunkColors.orangeDark,
                      ),
                    ),
                    const SizedBox(width: 8),
                    ...SearchScope.values.map((scope) {
                      final isSelected = _scope == scope;
                      return Padding(
                        padding: const EdgeInsets.only(right: 8),
                        child: FilterChip(
                          label: Text(
                            scope.displayName,
                            style: CyberpunkTypography.bodySmall.copyWith(
                              color: isSelected
                                  ? CyberpunkColors.darkGray
                                  : CyberpunkColors.orangePrimary,
                            ),
                          ),
                          selected: isSelected,
                          selectedColor: CyberpunkColors.orangePrimary,
                          checkmarkColor: CyberpunkColors.darkGray,
                          backgroundColor: CyberpunkColors.darkGray,
                          side: const BorderSide(
                            color: CyberpunkColors.orangePrimary,
                            width: 1,
                          ),
                          onSelected: (selected) {
                            setState(() => _scope = scope);
                            if (_lastQuery.isNotEmpty) {
                              _search(_lastQuery);
                            }
                          },
                        ),
                      );
                    }),
                  ],
                ),
              ],
            ),
          ),

          // Results
          Expanded(
            child: _isSearching
                ? const Center(
                    child: CircularProgressIndicator(
                      color: CyberpunkColors.orangePrimary,
                    ),
                  )
                : _error != null && _results.isEmpty
                    ? _buildErrorState()
                    : _results.isEmpty && _lastQuery.isNotEmpty
                        ? _buildNoResults()
                        : _results.isEmpty
                            ? _buildEmptyState()
                            : _buildResults(),
          ),
        ],
      ),
    ),
    );
  }

  Widget _buildEmptyState() {
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          const Icon(
            Icons.search_outlined,
            size: 64,
            color: CyberpunkColors.orangeDark,
          ),
          const SizedBox(height: 16),
          Text(
            'search across sessions, tasks, memories, and plans',
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.orangeDark,
            ),
            textAlign: TextAlign.center,
          ),
        ],
      ),
    );
  }

  Widget _buildNoResults() {
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          const Icon(
            Icons.search_off,
            size: 48,
            color: CyberpunkColors.orangeDark,
          ),
          const SizedBox(height: 16),
          Text(
            'no results for "$_lastQuery"',
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.orangeDark,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildErrorState() {
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
            textAlign: TextAlign.center,
          ),
        ],
      ),
    );
  }

  Widget _buildResults() {
    // Group results by type
    final grouped = <SearchResultType, List<SearchResultItem>>{};
    for (final result in _results) {
      grouped.putIfAbsent(result.type, () => []).add(result);
    }

    return ListView.builder(
      itemCount: grouped.length,
      itemBuilder: (context, index) {
        final type = grouped.keys.elementAt(index);
        final results = grouped[type]!;

        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // Type header
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
              child: Text(
                type.displayName,
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.orangePrimary,
                  fontWeight: FontWeight.bold,
                ),
              ),
            ),
            // Results of this type
            ...results.map((result) => _buildResultTile(result)),
          ],
        );
      },
    );
  }

  Widget _buildResultTile(SearchResultItem result) {
    return ListTile(
      title: Text(
        result.title,
        style: CyberpunkTypography.bodyMedium.copyWith(
          color: CyberpunkColors.orangeBright,
        ),
      ),
      subtitle: result.snippet.isNotEmpty
          ? Text(
              result.snippet,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
              ),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            )
          : null,
      onTap: () {
        // TODO: Navigate to the result based on type and id
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('navigating to ${result.type.displayName}: ${result.title}'),
            backgroundColor: CyberpunkColors.orangePrimary,
          ),
        );
      },
    );
  }
}

/// Private debounce helper — file-local to avoid polluting the public API.
class _Debouncer {
  final Duration delay;
  Timer? _timer;

  _Debouncer({required this.delay});

  void run(VoidCallback action) {
    _timer?.cancel();
    _timer = Timer(delay, action);
  }

  void dispose() {
    _timer?.cancel();
    _timer = null;
  }
}
