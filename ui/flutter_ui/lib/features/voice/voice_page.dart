// voice_page.dart - the voice feature page (voice-local-leg leaf 13b).
//
// GUI half of push-to-talk parity: drives the SAME voice.local.* RPC
// control plane as the TUI (leaf 13a) and the CLI, through the daemon's
// HTTP bus/call bridge. Employee picker + start button (voice.local.open),
// a 2s live status poll (voice.local.status, both legs), and an end
// button per live call (voice.local.end).
//
// Error envelopes surface VERBATIM: every [VoiceServiceException]
// message is the daemon's own string (e.g. the disabled-leg refusal
// "voice: local leg is disabled" when voice.local.enabled=false) and is
// rendered as-is, never re-mapped.
import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../providers/providers.dart';
import '../../services/voice_audio.dart';
import '../../services/voice_service.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../widgets/tool_panel_shell.dart';

/// Poll cadence for the live status list (leaf 13b T2: 2s).
const Duration voiceStatusPollInterval = Duration(seconds: 2);

/// Voice page state held by [VoicePageNotifier].
class VoicePageState {
  /// Employees offered by the picker (employee roster ids/names).
  final List<Map<String, String>> employees;

  /// Live calls from the latest voice.local.status snapshot (both legs).
  final List<VoiceSessionRow> sessions;

  final bool isLoading;
  final bool opening;

  /// The daemon's verbatim error envelope message, or null. Rendered
  /// as-is; the GUI never re-maps it.
  final String? error;

  /// Last one-line status note (start/end outcomes).
  final String? note;

  const VoicePageState({
    this.employees = const [],
    this.sessions = const [],
    this.isLoading = false,
    this.opening = false,
    this.error,
    this.note,
  });

  VoicePageState copyWith({
    List<Map<String, String>>? employees,
    List<VoiceSessionRow>? sessions,
    bool? isLoading,
    bool? opening,
    Object? error = _unset,
    Object? note = _unset,
  }) {
    return VoicePageState(
      employees: employees ?? this.employees,
      sessions: sessions ?? this.sessions,
      isLoading: isLoading ?? this.isLoading,
      opening: opening ?? this.opening,
      error: identical(error, _unset) ? this.error : (error as String?),
      note: identical(note, _unset) ? this.note : (note as String?),
    );
  }
}

const _unset = Object();

/// StateNotifier driving the voice page: employee list, start, 2s status
/// poll, per-session end.
class VoicePageNotifier extends StateNotifier<VoicePageState> {
  VoicePageNotifier({required this.voiceService, this.employeeLister})
    : super(const VoicePageState(isLoading: true)) {
    _init();
  }

  final VoiceServiceContract voiceService;

  /// Employee-list seam. Defaults to the employee roster
  /// (SdkApiClient.listEmployees) via [rosterEmployeeLister]; tests
  /// inject a fake.
  final Future<List<Map<String, dynamic>>> Function()? employeeLister;

  Timer? _pollTimer;
  bool _disposed = false;

  void _checkMounted() {
    assert(() {
      if (_disposed) {
        throw StateError('VoicePageNotifier was used after dispose');
      }
      return true;
    }());
  }

  void _init() {
    unawaited(_loadEmployees());
    unawaited(_refreshStatus());
    _pollTimer = Timer.periodic(voiceStatusPollInterval, (_) {
      unawaited(_refreshStatus());
    });
  }

  @override
  void dispose() {
    _disposed = true;
    _pollTimer?.cancel();
    super.dispose();
  }

  /// Employees for the picker, from the employee roster (the same list
  /// `meept voice local --list` shows). Roster failure degrades to an
  /// empty picker with a note - the voice RPC plane itself stays usable
  /// (open still accepts any employee id the daemon knows).
  Future<void> _loadEmployees() async {
    _checkMounted();
    final lister = employeeLister;
    if (lister == null) {
      state = state.copyWith(isLoading: false);
      return;
    }
    try {
      final raw = await lister();
      final employees = raw
          .map(
            (e) => {
              'id': (e['id'] as String?) ?? '',
              'name': (e['name'] as String?) ?? (e['id'] as String?) ?? '',
            }.cast<String, String>(),
          )
          .where((e) => e['id']!.isNotEmpty)
          .toList(growable: false);
      _checkMounted();
      state = state.copyWith(employees: employees, isLoading: false);
    } catch (e) {
      _checkMounted();
      // Roster is best-effort only; never block the voice plane on it.
      state = state.copyWith(
        isLoading: false,
        note: 'employee list unavailable - type an employee id below',
      );
    }
  }

  /// One voice.local.status refresh. An error here does NOT kill the
  /// poll: the next tick retries (transient daemon reconnects are
  /// normal), the message just surfaces until the next good snapshot.
  Future<void> _refreshStatus() async {
    _checkMounted();
    try {
      final sessions = await voiceService.status();
      if (_disposed) return;
      state = state.copyWith(sessions: sessions, error: null);
    } on VoiceServiceException catch (e) {
      if (_disposed) return;
      state = state.copyWith(error: e.message);
    } catch (e) {
      if (_disposed) return;
      state = state.copyWith(error: e.toString());
    }
  }

  /// voice.local.open for the picked employee, then an immediate status
  /// refresh so the new call row appears without waiting a tick.
  Future<void> startCall(String employeeId, String label) async {
    _checkMounted();
    if (employeeId.isEmpty) {
      state = state.copyWith(note: 'pick or type an employee id first');
      return;
    }
    state = state.copyWith(opening: true, error: null, note: null);
    try {
      final open = await voiceService.open(employeeId, label);
      if (_disposed) return;
      state = state.copyWith(
        opening: false,
        note: 'call ${open.callId} open (session ${open.sessionId})',
      );
      await _refreshStatus();
    } on VoiceServiceException catch (e) {
      if (_disposed) return;
      // Verbatim pass-through: this is the named refusal surface
      // (disabled leg, unknown employee, busy, duplicate).
      state = state.copyWith(opening: false, error: e.message);
    } catch (e) {
      if (_disposed) return;
      state = state.copyWith(opening: false, error: e.toString());
    }
  }

  /// voice.local.end for one call row, then refresh.
  Future<void> endCall(String callId) async {
    _checkMounted();
    try {
      await voiceService.end(callId);
      if (_disposed) return;
      state = state.copyWith(note: 'call $callId ended');
      await _refreshStatus();
    } on VoiceServiceException catch (e) {
      if (_disposed) return;
      state = state.copyWith(error: e.message);
    } catch (e) {
      if (_disposed) return;
      state = state.copyWith(error: e.toString());
    }
  }

  /// Manual refresh (the shell's refresh action).
  Future<void> refresh() async {
    _checkMounted();
    await _refreshStatus();
  }
}

/// voiceProvider: a [VoiceService]-backed notifier. The service reads the
/// same storage-backed host/port/API key as every other panel (matching
/// SdkApiClient.storage) but talks voice.local.* over the bus/call
/// bridge; the employee picker lists the employee roster.
final voiceProvider = StateNotifierProvider<VoicePageNotifier, VoicePageState>((
  ref,
) {
  final sdk = ref.watch(sdkClientProvider);
  // NOTE: no ref.onDispose(notifier.dispose) here - StateNotifierProvider
  // already disposes the notifier itself; an extra dispose is a
  // double-dispose (state_notifier's mounted assert fires).
  return VoicePageNotifier(
    voiceService: VoiceService.fromDefaults(
      storage: ref.watch(storageProvider),
    ),
    employeeLister: () => sdk.listEmployees(),
  );
});

/// The browser audio capture/playback lifecycle (leaf 13b T4). Web
/// builds get the getUserMedia/MediaRecorder + WebAudio pipeline; native
/// and tests get the unsupported stub through the conditional factory.
/// Overridden in widget tests that exercise capture/playback states.
final voiceAudioProvider = Provider<VoiceAudio>((ref) {
  final audio = createVoiceAudio();
  ref.onDispose(() => unawaited(audio.dispose()));
  return audio;
});

/// The voice feature page (leaf 13b). Reachable at /tools/voice.
class VoicePage extends ConsumerWidget {
  const VoicePage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(voiceProvider);

    return ToolPanelShell(
      title: 'voice',
      icon: Icons.graphic_eq,
      actions: [
        IconButton(
          icon: const Icon(Icons.refresh, size: 18),
          color: CyberpunkColors.orangePrimary,
          tooltip: 'refresh status',
          onPressed: () => ref.read(voiceProvider.notifier).refresh(),
        ),
      ],
      child: _VoiceBody(state: state),
    );
  }
}

class _VoiceBody extends ConsumerWidget {
  final VoicePageState state;

  const _VoiceBody({required this.state});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        // Verbatim daemon error envelope (disabled leg, busy, ...).
        if (state.error != null)
          Container(
            margin: const EdgeInsets.fromLTRB(16, 12, 16, 0),
            padding: const EdgeInsets.all(10),
            decoration: BoxDecoration(
              color: CyberpunkColors.darkGray,
              border: Border.all(
                color: CyberpunkColors.redAlert.withValues(alpha: 0.6),
              ),
              borderRadius: BorderRadius.circular(4),
            ),
            child: Text(
              state.error!,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.redAlert,
              ),
            ),
          ),
        if (state.note != null)
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
            child: Text(
              state.note!,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
              ),
            ),
          ),
        const _VoiceAudioRow(),
        const _VoiceStartRow(),
        const Divider(height: 24),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16),
          child: Text(
            'live calls (both legs, 2s poll)',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.orangePrimary,
            ),
          ),
        ),
        const SizedBox(height: 4),
        Expanded(child: _VoiceSessionList(sessions: state.sessions)),
      ],
    );
  }
}

/// Mic capture row (leaf 13b T4): start/stop browser mic capture behind
/// the [VoiceAudio] lifecycle. On unsupported platforms (native, tests)
/// the row degrades to a lowercase note; capture state is widget-local.
class _VoiceAudioRow extends ConsumerStatefulWidget {
  const _VoiceAudioRow();

  @override
  ConsumerState<_VoiceAudioRow> createState() => _VoiceAudioRowState();
}

class _VoiceAudioRowState extends ConsumerState<_VoiceAudioRow> {
  bool _capturing = false;
  String? _audioNote;

  @override
  Widget build(BuildContext context) {
    final supported = ref.watch(voiceAudioProvider).isSupported;

    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
      child: Row(
        children: [
          Icon(
            Icons.mic,
            size: 16,
            color: _capturing
                ? CyberpunkColors.redAlert
                : CyberpunkColors.midGray,
          ),
          const SizedBox(width: 8),
          Text(
            _capturing ? 'mic capturing...' : 'mic idle',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: _capturing
                  ? CyberpunkColors.redAlert
                  : CyberpunkColors.midGray,
            ),
          ),
          const SizedBox(width: 12),
          if (supported)
            TextButton(
              onPressed: () => unawaited(_toggleCapture()),
              child: Text(
                _capturing ? 'stop capture' : 'start capture',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.orangePrimary,
                ),
              ),
            )
          else
            Text(
              'capture unavailable on this platform',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
                fontSize: 10,
              ),
            ),
          if (_audioNote != null) ...[
            const SizedBox(width: 12),
            Expanded(
              child: Text(
                _audioNote!,
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.redAlert,
                  fontSize: 10,
                ),
                overflow: TextOverflow.ellipsis,
              ),
            ),
          ],
        ],
      ),
    );
  }

  Future<void> _toggleCapture() async {
    final audio = ref.read(voiceAudioProvider);
    try {
      if (_capturing) {
        final result = await audio.stopCapture();
        if (!mounted) return;
        setState(() {
          _capturing = false;
          _audioNote =
              'captured ${result.bytes.length} bytes (${result.mimeType})';
        });
      } else {
        await audio.startCapture();
        if (!mounted) return;
        setState(() {
          _capturing = true;
          _audioNote = null;
        });
      }
    } on VoiceAudioException catch (e) {
      if (!mounted) return;
      setState(() {
        _capturing = false;
        _audioNote = e.message;
      });
    }
  }
}

/// Employee picker + start button (voice.local.open).
class _VoiceStartRow extends ConsumerStatefulWidget {
  const _VoiceStartRow();

  @override
  ConsumerState<_VoiceStartRow> createState() => _VoiceStartRowState();
}

class _VoiceStartRowState extends ConsumerState<_VoiceStartRow> {
  final _labelController = TextEditingController();
  String? _pickedEmployeeId;
  final _manualIdFocus = FocusNode();

  @override
  void dispose() {
    _labelController.dispose();
    _manualIdFocus.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(voiceProvider);
    final notifier = ref.read(voiceProvider.notifier);

    return Padding(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'start a local call',
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.orangePrimary,
            ),
          ),
          const SizedBox(height: 8),
          Row(
            children: [
              // Employee picker (roster-backed when available).
              DropdownButton<String>(
                hint: Text(
                  _pickedEmployeeId ?? 'employee...',
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.lightGray,
                  ),
                ),
                value: _pickedEmployeeId,
                dropdownColor: CyberpunkColors.darkGray,
                items: [
                  for (final e in state.employees)
                    DropdownMenuItem<String>(
                      value: e['id'],
                      child: Text(
                        e['name']! == e['id']!
                            ? e['id']!
                            : '${e['name']} (${e['id']})',
                        style: CyberpunkTypography.bodySmall,
                      ),
                    ),
                ],
                onChanged: (id) => setState(() => _pickedEmployeeId = id),
              ),
              const SizedBox(width: 12),
              // Manual id entry (roster empty / arbitrary target label).
              SizedBox(
                width: 140,
                child: TextField(
                  focusNode: _manualIdFocus,
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.lightGray,
                  ),
                  decoration: InputDecoration(
                    hintText: 'employee id',
                    hintStyle: CyberpunkTypography.bodySmall.copyWith(
                      color: CyberpunkColors.midGray,
                    ),
                    isDense: true,
                  ),
                  onChanged: (v) => setState(() => _pickedEmployeeId = v),
                ),
              ),
              const SizedBox(width: 12),
              SizedBox(
                width: 140,
                child: TextField(
                  controller: _labelController,
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.lightGray,
                  ),
                  decoration: InputDecoration(
                    hintText: 'label (optional)',
                    hintStyle: CyberpunkTypography.bodySmall.copyWith(
                      color: CyberpunkColors.midGray,
                    ),
                    isDense: true,
                  ),
                ),
              ),
              const SizedBox(width: 12),
              state.opening
                  ? const SizedBox(
                      width: 20,
                      height: 20,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : ElevatedButton.icon(
                      icon: const Icon(Icons.call, size: 16),
                      label: const Text(
                        'start',
                        style: CyberpunkTypography.bodySmall,
                      ),
                      style: ElevatedButton.styleFrom(
                        backgroundColor: CyberpunkColors.darkGray,
                        foregroundColor: CyberpunkColors.orangePrimary,
                      ),
                      onPressed: () {
                        final id =
                            _pickedEmployeeId ??
                            state.employees.firstOrNull?['id'] ??
                            '';
                        unawaited(
                          notifier.startCall(id, _labelController.text.trim()),
                        );
                      },
                    ),
            ],
          ),
        ],
      ),
    );
  }
}

/// Live session list with an end button per row.
class _VoiceSessionList extends StatelessWidget {
  final List<VoiceSessionRow> sessions;

  const _VoiceSessionList({required this.sessions});

  @override
  Widget build(BuildContext context) {
    if (sessions.isEmpty) {
      return Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16),
        child: Text(
          'no live calls',
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.midGray,
          ),
        ),
      );
    }
    return ListView.builder(
      padding: const EdgeInsets.symmetric(horizontal: 16),
      itemCount: sessions.length,
      itemBuilder: (context, index) {
        final row = sessions[index];
        return _VoiceSessionRowTile(row: row);
      },
    );
  }
}

class _VoiceSessionRowTile extends ConsumerWidget {
  final VoiceSessionRow row;

  const _VoiceSessionRowTile({required this.row});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Container(
      margin: const EdgeInsets.symmetric(vertical: 4),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray,
        border: Border.all(color: CyberpunkColors.midGray, width: 1),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Row(
        children: [
          Icon(
            row.leg == 'local' ? Icons.graphic_eq : Icons.phone,
            size: 16,
            color: CyberpunkColors.orangePrimary,
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  '${row.employeeId} -> ${row.target}',
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.lightGray,
                  ),
                  overflow: TextOverflow.ellipsis,
                ),
                Text(
                  '${row.leg} - since ${row.sinceRfc3339}',
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.midGray,
                    fontSize: 10,
                  ),
                  overflow: TextOverflow.ellipsis,
                ),
              ],
            ),
          ),
          const SizedBox(width: 8),
          TextButton(
            onPressed: () =>
                unawaited(ref.read(voiceProvider.notifier).endCall(row.callId)),
            child: Text(
              'end',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.redAlert,
              ),
            ),
          ),
        ],
      ),
    );
  }
}
