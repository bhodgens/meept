import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../services/sdk_client.dart';
import 'activity_models.dart';

/// Provider for SdkApiClient instance
final sdkApiClientProvider = Provider<SdkApiClient>((ref) {
  return SdkApiClient(host: 'localhost', port: 8080);
});

/// Provider for activity data
final activityProvider = StateNotifierProvider<ActivityNotifier, ActivityState>(
  (ref) => ActivityNotifier(ref.read(sdkApiClientProvider)),
);

class ActivityState {
  final List<ActivityRecord> records;
  final String sortMode; // "chrono" | "task" | "plan"
  final bool isLoading;
  final String? error;

  const ActivityState({
    this.records = const [],
    this.sortMode = 'chrono',
    this.isLoading = false,
    this.error,
  });

  ActivityState copyWith({
    List<ActivityRecord>? records,
    String? sortMode,
    bool? isLoading,
    String? error,
  }) {
    return ActivityState(
      records: records ?? this.records,
      sortMode: sortMode ?? this.sortMode,
      isLoading: isLoading ?? this.isLoading,
      error: error ?? this.error,
    );
  }
}

class ActivityNotifier extends StateNotifier<ActivityState> {
  final SdkApiClient _client;

  ActivityNotifier(this._client) : super(const ActivityState());

  Future<void> loadActivity({String? sort}) async {
    state = state.copyWith(isLoading: true, error: null);
    try {
      final result = await _client.getActivity(sort: sort);
      final activityResult = ActivityQueryResult.fromJson(result);
      state = state.copyWith(
        records: activityResult.records,
        sortMode: sort ?? 'chrono',
        isLoading: false,
      );
    } catch (e) {
      state = state.copyWith(isLoading: false, error: e.toString());
    }
  }
}

/// Activity list widget with sort modes
class ActivityList extends ConsumerStatefulWidget {
  const ActivityList({super.key});

  @override
  ConsumerState<ActivityList> createState() => _ActivityListState();
}

class _ActivityListState extends ConsumerState<ActivityList> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(activityProvider.notifier).loadActivity();
    });
  }

  @override
  Widget build(BuildContext context) {
    final activityState = ref.watch(activityProvider);

    return Container(
      constraints: const BoxConstraints(minWidth: 200),
      decoration: BoxDecoration(
        border: Border(
          right: BorderSide(color: Colors.orange.withOpacity(0.3), width: 1),
        ),
      ),
      child: Column(
        children: [
          Padding(
            padding: const EdgeInsets.all(16),
            child: Row(
              children: [
                const Text(
                  'activity',
                  style: TextStyle(
                    color: Colors.orange,
                    fontSize: 16,
                    fontWeight: FontWeight.bold,
                  ),
                ),
                const Spacer(),
                // Sort mode segmented control
                _SortSegmentedControl(
                  currentSort: activityState.sortMode,
                  onSortChanged: (sort) {
                    ref
                        .read(activityProvider.notifier)
                        .loadActivity(sort: sort);
                  },
                ),
              ],
            ),
          ),
          if (activityState.isLoading)
            const Expanded(child: Center(child: CircularProgressIndicator()))
          else if (activityState.error != null)
            Expanded(
              child: Column(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  Text(
                    'error: ${activityState.error}',
                    style: const TextStyle(color: Colors.red),
                  ),
                  const SizedBox(height: 8),
                  FilledButton.tonal(
                    onPressed: () =>
                        ref.read(activityProvider.notifier).loadActivity(),
                    child: const Text('retry', style: TextStyle(fontSize: 12)),
                  ),
                ],
              ),
            )
          else if (activityState.records.isEmpty)
            const Expanded(child: Center(child: Text('no activity records')))
          else
            Expanded(
              child: ListView.builder(
                itemCount: activityState.records.length,
                itemBuilder: (context, index) {
                  final record = activityState.records[index];
                  return _ActivityTile(record: record);
                },
              ),
            ),
        ],
      ),
    );
  }
}

/// Sort mode segmented control
class _SortSegmentedControl extends StatelessWidget {
  final String currentSort;
  final ValueChanged<String> onSortChanged;

  const _SortSegmentedControl({
    required this.currentSort,
    required this.onSortChanged,
  });

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        _SortButton(
          label: 'chrono',
          isActive: currentSort == 'chrono',
          onTap: () => onSortChanged('chrono'),
        ),
        const SizedBox(width: 4),
        _SortButton(
          label: 'by task',
          isActive: currentSort == 'task',
          onTap: () => onSortChanged('task'),
        ),
        const SizedBox(width: 4),
        _SortButton(
          label: 'by plan',
          isActive: currentSort == 'plan',
          onTap: () => onSortChanged('plan'),
        ),
      ],
    );
  }
}

class _SortButton extends StatelessWidget {
  final String label;
  final bool isActive;
  final VoidCallback onTap;

  const _SortButton({
    required this.label,
    required this.isActive,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
        decoration: BoxDecoration(
          color: isActive ? Colors.orange.withOpacity(0.3) : null,
          border: Border(
            left: BorderSide(
              color: isActive ? Colors.orange : Colors.transparent,
              width: 2,
            ),
          ),
        ),
        child: Text(
          label,
          style: const TextStyle(
            color: Colors.white70,
            fontSize: 10,
            fontFamily: 'SourceCodePro',
          ),
        ),
      ),
    );
  }
}

/// Activity tile for individual records
class _ActivityTile extends StatelessWidget {
  final ActivityRecord record;

  const _ActivityTile({required this.record});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            record.summary.toLowerCase(),
            style: const TextStyle(color: Colors.green, fontSize: 14),
          ),
          const SizedBox(height: 4),
          Row(
            children: [
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                decoration: BoxDecoration(
                  color: Colors.orange.withOpacity(0.2),
                  borderRadius: BorderRadius.circular(3),
                ),
                child: Text(
                  record.kind.toLowerCase(),
                  style: const TextStyle(
                    color: Colors.orange,
                    fontSize: 10,
                    fontFamily: 'SourceCodePro',
                  ),
                ),
              ),
              const Spacer(),
              Text(
                record.at.toLocal().toString().substring(11, 16),
                style: const TextStyle(color: Colors.grey, fontSize: 10),
              ),
            ],
          ),
        ],
      ),
    );
  }
}
