import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/providers.dart';
import '../services/thread_service.dart';

/// Provider for the [ThreadService]. Derives the [SdkApiClient] from the
/// app-wide [sdkClientProvider] so no ProviderScope override is needed.
final threadServiceProvider = Provider<ThreadService>((ref) {
  final client = ref.watch(sdkClientProvider);
  return ThreadService(client);
});

/// State for the thread selector widget.
class ThreadSelectorState {
  final List<Thread> threads;
  final String? activeThreadId;
  final bool isLoading;

  const ThreadSelectorState({
    this.threads = const [],
    this.activeThreadId,
    this.isLoading = false,
  });

  ThreadSelectorState copyWith({
    List<Thread>? threads,
    String? activeThreadId,
    bool? isLoading,
  }) {
    return ThreadSelectorState(
      threads: threads ?? this.threads,
      activeThreadId: activeThreadId ?? this.activeThreadId,
      isLoading: isLoading ?? this.isLoading,
    );
  }
}

/// Notifier that loads and manages threads for a session.
class ThreadSelectorNotifier extends StateNotifier<ThreadSelectorState> {
  ThreadSelectorNotifier(this._service, this._sessionId)
      : super(const ThreadSelectorState());

  final ThreadService _service;
  final String _sessionId;

  /// Load threads from the server.
  Future<void> load() async {
    state = state.copyWith(isLoading: true);
    final threads = await _service.listThreads(_sessionId);
    final active = threads.where((t) => t.isActive).firstOrNull;
    state = ThreadSelectorState(
      threads: threads,
      activeThreadId: active?.id,
      isLoading: false,
    );
  }

  /// Switch to a different thread.
  Future<void> switchThread(String threadId) async {
    final result = await _service.setActiveThread(_sessionId, threadId);
    if (result != null) {
      state = state.copyWith(activeThreadId: threadId);
    }
  }

  /// Create a new thread with the given topic label.
  Future<void> createThread(String topicLabel) async {
    final result = await _service.createThread(
      _sessionId,
      topicLabel: topicLabel,
    );
    if (result != null) {
      await load();
    }
  }

  /// Delete a thread by ID.
  Future<void> deleteThread(String threadId) async {
    final success = await _service.deleteThread(_sessionId, threadId);
    if (success) {
      await load();
    }
  }
}

/// Provider family for [ThreadSelectorNotifier], keyed by session ID.
final threadSelectorProvider = StateNotifierProvider.family<
    ThreadSelectorNotifier, ThreadSelectorState, String>(
  (ref, sessionId) {
    final service = ref.watch(threadServiceProvider);
    return ThreadSelectorNotifier(service, sessionId);
  },
);

/// A compact dropdown widget for selecting the active thread.
class ThreadSelector extends ConsumerWidget {
  final String sessionId;

  const ThreadSelector({
    super.key,
    required this.sessionId,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(threadSelectorProvider(sessionId));

    if (state.isLoading) {
      return const SizedBox(
        width: 16,
        height: 16,
        child: CircularProgressIndicator(strokeWidth: 2),
      );
    }

    if (state.threads.isEmpty) {
      return TextButton.icon(
        onPressed: () => _showCreateDialog(context, ref),
        icon: const Icon(Icons.add, size: 16),
        label: const Text('new thread'),
      );
    }

    return PopupMenuButton<String>(
      onSelected: (threadId) {
        ref
            .read(threadSelectorProvider(sessionId).notifier)
            .switchThread(threadId);
      },
      itemBuilder: (context) {
        return [
          ...state.threads.map((t) => PopupMenuItem<String>(
                value: t.id,
                child: Row(
                  children: [
                    if (t.id == state.activeThreadId)
                      const Padding(
                        padding: EdgeInsets.only(right: 8),
                        child: Icon(Icons.check, size: 16),
                      ),
                    Text(t.topicLabel),
                  ],
                ),
              )),
          const PopupMenuDivider(),
          const PopupMenuItem<String>(
            value: '__new__',
            child: Row(
              children: [
                Icon(Icons.add, size: 16),
                SizedBox(width: 8),
                Text('new thread'),
              ],
            ),
          ),
        ];
      },
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
        decoration: BoxDecoration(
          border: Border.all(
            color: Theme.of(context).colorScheme.outlineVariant,
          ),
          borderRadius: BorderRadius.circular(4),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.forum_outlined, size: 14),
            const SizedBox(width: 4),
            Text(
              state.threads
                      .where((t) => t.id == state.activeThreadId)
                      .firstOrNull
                      ?.topicLabel ??
                  'general',
              style: Theme.of(context).textTheme.labelSmall,
            ),
            const Icon(Icons.arrow_drop_down, size: 16),
          ],
        ),
      ),
    );
  }

  void _showCreateDialog(BuildContext context, WidgetRef ref) {
    final controller = TextEditingController();
    showDialog(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('new thread'),
        content: TextField(
          controller: controller,
          decoration: const InputDecoration(
            labelText: 'topic',
            hintText: 'e.g. debugging, research, planning',
          ),
          autofocus: true,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: const Text('cancel'),
          ),
          FilledButton(
            onPressed: () {
              final topic = controller.text.trim();
              ref
                  .read(threadSelectorProvider(sessionId).notifier)
                  .createThread(topic.isEmpty ? 'general' : topic);
              Navigator.of(context).pop();
            },
            child: const Text('create'),
          ),
        ],
      ),
    );
  }
}
