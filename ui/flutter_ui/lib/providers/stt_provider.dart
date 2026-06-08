// Riverpod provider for STT recording state management.
// Manages recording lifecycle and exposes state to the UI.

import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../services/stt_service.dart';

/// Recording lifecycle states exposed to the UI.
enum SttState {
  /// Not recording; ready to accept a new recording request.
  idle,

  /// Actively capturing and transcribing speech.
  recording,

  /// Recording has stopped; waiting for the final transcription result.
  transcribing,
}

/// StateNotifier that manages the speech-to-text recording lifecycle.
///
/// Wraps [SttService] and exposes a declarative [SttState] for the UI
/// to react to (send button icon changes, recording overlays, etc.).
class SttNotifier extends StateNotifier<SttState> {
  final SttService _service;

  SttNotifier(this._service) : super(SttState.idle);

  /// Whether the underlying speech recognizer is available on this device.
  bool get isAvailable => _service.isAvailable;

  /// The current recording state.
  SttState get currentState => state;

  /// Start recording speech input.
  ///
  /// Lazily initializes the speech recognizer on first use.
  /// [onResult] is called with streaming transcription results.
  /// [onError] is called when recognition errors occur.
  Future<void> startRecording({
    String language = 'en_US',
    required Function(String) onResult,
    Function(String)? onError,
  }) async {
    if (!_service.isAvailable) {
      await _service.initialize();
    }
    if (!_service.isAvailable) return;

    state = SttState.recording;
    _service.startRecording(
      language: language,
      onResult: onResult,
      onError: onError,
    );
  }

  /// Stop recording and return the final transcription.
  ///
  /// Transitions through [SttState.transcribing] while waiting for the
  /// result, then back to [SttState.idle].
  Future<String> stopRecording() async {
    state = SttState.transcribing;
    final text = await _service.stopRecording();
    state = SttState.idle;
    return text;
  }

  /// Cancel recording and discard captured audio.
  ///
  /// Immediately returns to [SttState.idle] without waiting for a
  /// transcription result.
  void cancelRecording() {
    _service.cancelRecording();
    state = SttState.idle;
  }
}

/// Global Riverpod provider for STT recording state.
///
/// Creates a fresh [SttService] and [SttNotifier] on first access.
final sttProvider = StateNotifierProvider<SttNotifier, SttState>((ref) {
  return SttNotifier(SttService());
});
