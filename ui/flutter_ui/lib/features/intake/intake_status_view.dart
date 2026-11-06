import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../providers/providers.dart';
import '../../services/sdk_client.dart';
import 'intake_models.dart';

/// Provider for the intake status snapshot (issue #109 GUI parity for the
/// TUI's /intake view). Fetches through [sdkClientProvider] — the one
/// client that carries the persisted host/port/API key; feature code must
/// never construct SdkApiClient directly.
final intakeStatusProvider =
    StateNotifierProvider<IntakeStatusNotifier, IntakeStatusState>(
      (ref) => IntakeStatusNotifier(ref.read(sdkClientProvider)),
    );

class IntakeStatusState {
  final IntakeStatusModel? status;
  final bool isLoading;
  final String? error;

  const IntakeStatusState({this.status, this.isLoading = false, this.error});

  IntakeStatusState copyWith({
    IntakeStatusModel? status,
    bool? isLoading,
    String? error,
  }) {
    return IntakeStatusState(
      status: status ?? this.status,
      isLoading: isLoading ?? this.isLoading,
      error: error,
    );
  }
}

class IntakeStatusNotifier extends StateNotifier<IntakeStatusState> {
  final SdkApiClient _client;

  IntakeStatusNotifier(this._client) : super(const IntakeStatusState());

  Future<void> load() async {
    state = state.copyWith(isLoading: true);
    try {
      final raw = await _client.getIntakeStatus();
      state = state.copyWith(
        status: IntakeStatusModel.fromJson(raw),
        isLoading: false,
      );
    } catch (e) {
      state = state.copyWith(isLoading: false, error: e.toString());
    }
  }
}

/// Intake status view - the GUI twin of the TUI's /intake report
/// (internal/tui/intake_view.go renderIntakeStatus). Read-only; rows and
/// defaults mirror the TUI's rendering so the two surfaces agree. All UI
/// text lowercase per the repo UI convention.
class IntakeStatusView extends ConsumerStatefulWidget {
  const IntakeStatusView({super.key});

  @override
  ConsumerState<IntakeStatusView> createState() => _IntakeStatusViewState();
}

class _IntakeStatusViewState extends ConsumerState<IntakeStatusView> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(intakeStatusProvider.notifier).load();
    });
  }

  @override
  Widget build(BuildContext context) {
    final st = ref.watch(intakeStatusProvider);

    return SingleChildScrollView(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Text(
                'intake status',
                style: TextStyle(
                  color: Colors.orange,
                  fontSize: 16,
                  fontWeight: FontWeight.bold,
                ),
              ),
              const Spacer(),
              TextButton(
                onPressed: st.isLoading
                    ? null
                    : () => ref.read(intakeStatusProvider.notifier).load(),
                child: const Text(
                  'refresh',
                  style: TextStyle(color: Colors.white70, fontSize: 12),
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          if (st.isLoading && st.status == null)
            const Center(
              child: Padding(
                padding: EdgeInsets.all(24),
                child: CircularProgressIndicator(),
              ),
            )
          else if (st.error != null && st.status == null)
            Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'intake error: ${st.error}',
                  style: const TextStyle(color: Colors.red, fontSize: 12),
                ),
                const SizedBox(height: 8),
                FilledButton.tonal(
                  onPressed: () =>
                      ref.read(intakeStatusProvider.notifier).load(),
                  child: const Text('retry', style: TextStyle(fontSize: 12)),
                ),
              ],
            )
          else if (st.status != null)
            _StatusReport(status: st.status!),
        ],
      ),
    );
  }
}

/// Row-by-row report mirroring renderIntakeStatus in
/// internal/tui/intake_view.go: the disabled state collapses to the note,
/// the enabled state lists every snapshot field with the TUI's defaults
/// for empty values.
class _StatusReport extends StatelessWidget {
  final IntakeStatusModel status;

  const _StatusReport({required this.status});

  @override
  Widget build(BuildContext context) {
    if (!status.enabled) {
      final note = status.note.isEmpty ? 'intake disabled' : status.note;
      return _Row(label: 'status', value: note.toLowerCase());
    }

    final rows = <Widget>[
      _Row(label: 'enabled', value: '${status.enabled}'),
      _Row(label: 'watch_repo', value: _dash(status.watchRepo)),
      _Row(label: 'interval', value: _dash(status.pollInterval)),
      _Row(
        label: 'labels',
        value: status.requiredLabels.isEmpty
            ? 'factory (default)'
            : status.requiredLabels.join(', '),
      ),
      _Row(label: 'done_label', value: _dash(status.doneLabel)),
      _Row(
        label: 'employee',
        value: status.defaultEmployee.trim().isEmpty
            ? 'missing'
            : status.defaultEmployee,
      ),
      _Row(
        label: 'gate',
        value: status.gateCommand.isEmpty ? 'none' : status.gateCommand,
      ),
      _Row(
        label: 'project',
        value: status.projectNumber == 0 ? 'none' : '#${status.projectNumber}',
      ),
      _Row(
        label: 'poll job',
        value: status.pollerScheduled
            ? 'scheduled (intake_poller)'
            : 'not scheduled',
      ),
      if (status.note.isNotEmpty)
        _Row(label: 'note', value: status.note.toLowerCase()),
    ];

    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: rows);
  }

  static String _dash(String v) => v.isEmpty ? '-' : v;
}

class _Row extends StatelessWidget {
  final String label;
  final String value;

  const _Row({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 3),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 110,
            child: Text(
              label,
              style: const TextStyle(
                color: Colors.grey,
                fontSize: 12,
                fontFamily: 'SourceCodePro',
              ),
            ),
          ),
          Expanded(
            child: Text(
              value,
              style: const TextStyle(
                color: Colors.white70,
                fontSize: 12,
                fontFamily: 'SourceCodePro',
              ),
            ),
          ),
        ],
      ),
    );
  }
}
