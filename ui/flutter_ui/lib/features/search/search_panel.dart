import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/api_models.dart';
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
  final _debouncer = Debouncer(delay: const Duration(milliseconds: 300));

  late final ApiClient _apiClient;
  late final VoidCallback _searchListener;

  List<SearchResult> _results = [];
  bool _isSearching = false;
  String _lastQuery = '';
  SearchScope _scope = SearchScope.all;
  String? _error;
  bool _showClear = false;

  @override
  void initState() {
    super.initState();
    _apiClient = ref.read(apiClientProvider);
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
      final scopeName = _scope == SearchScope.all ? '' : _scope.name;
      final data = await _apiClient.post<Map<String, dynamic>>(
        '/search',
        data: {
          'query': query,
          if (scopeName.isNotEmpty) 'scope': scopeName,
        },
      );

      if (!mounted) return;

      final rawResults = data['results'] as List?;
      final List<SearchResult> parsed = [];
      if (rawResults != null) {
        for (final r in rawResults) {
          final map = r as Map<String, dynamic>;
          parsed.add(SearchResult(
            type: _parseResultType(map['type'] as String?),
            id: map['id'] as String? ?? '',
            title: map['title'] as String? ?? '',
            snippet: map['snippet'] as String? ?? '',
          ));
        }
      }

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

  SearchResultType _parseResultType(String? type) {
    switch (type) {
      case 'session':
        return SearchResultType.session;
      case 'task':
        return SearchResultType.task;
      case 'memory':
        return SearchResultType.memory;
      case 'plan':
        return SearchResultType.plan;
      default:
        return SearchResultType.session;
    }
  }

  @override
  Widget build(BuildContext context) {
    return Container(
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
                          side: BorderSide(
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
    );
  }

  Widget _buildEmptyState() {
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(
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
          Icon(
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
            textAlign: TextAlign.center,
          ),
        ],
      ),
    );
  }

  Widget _buildResults() {
    // Group results by type
    final grouped = <SearchResultType, List<SearchResult>>{};
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

  Widget _buildResultTile(SearchResult result) {
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

enum SearchScope {
  all,
  sessions,
  tasks,
  memories,
  plans;

  String get displayName {
    switch (this) {
      case SearchScope.all:
        return 'all';
      case SearchScope.sessions:
        return 'sessions';
      case SearchScope.tasks:
        return 'tasks';
      case SearchScope.memories:
        return 'memories';
      case SearchScope.plans:
        return 'plans';
    }
  }
}

enum SearchResultType {
  session,
  task,
  memory,
  plan;

  String get displayName {
    switch (this) {
      case SearchResultType.session:
        return 'sessions';
      case SearchResultType.task:
        return 'tasks';
      case SearchResultType.memory:
        return 'memories';
      case SearchResultType.plan:
        return 'plans';
    }
  }
}

class SearchResult {
  final SearchResultType type;
  final String id;
  final String title;
  final String snippet;

  SearchResult({
    required this.type,
    required this.id,
    required this.title,
    this.snippet = '',
  });
}

class Debouncer {
  final Duration delay;
  Timer? _timer;

  Debouncer({required this.delay});

  void run(VoidCallback action) {
    _timer?.cancel();
    _timer = Timer(delay, action);
  }

  void dispose() {
    _timer?.cancel();
    _timer = null;
  }
}
