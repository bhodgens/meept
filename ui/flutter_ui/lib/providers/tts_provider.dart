// Riverpod provider for TTS service.
// Follows the same pattern as stt_provider.dart.

import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../services/tts_service.dart';

/// TTS state enum.
enum TtsState {
  idle,
  speaking,
}

/// TTS notifier for Riverpod state management.
///
/// Wraps TtsService and provides state notification to UI.
class TtsNotifier extends StateNotifier<TtsState> {
  final TtsService _service;
  bool _enabled = false;

  TtsNotifier() : _service = TtsService(), super(TtsState.idle);

  /// Whether TTS is enabled.
  bool get enabled => _enabled;

  /// Whether the TTS service is available.
  bool get isAvailable => _service.isAvailable;

  /// Whether currently speaking.
  bool get isSpeaking => _service.isSpeaking;

  /// Initialize the TTS service.
  Future<bool> initialize() async {
    return await _service.initialize();
  }

  /// Toggle TTS on/off.
  void toggleTts() {
    _enabled = !_enabled;
    if (!_enabled) {
      stop();
    }
  }

  /// Set TTS enabled state.
  void setEnabled(bool value) {
    _enabled = value;
    if (!value) {
      stop();
    }
  }

  /// Speak text if TTS is enabled.
  Future<void> speak(String text) async {
    if (!_enabled) return;
    state = TtsState.speaking;
    await _service.speak(text);
    // State will be updated by completion handler
  }

  /// Stop speaking.
  Future<void> stop() async {
    await _service.stop();
    state = TtsState.idle;
  }

  /// Get available voices.
  Future<List<Map<String, dynamic>>> getVoices() async {
    return await _service.getVoices();
  }

  /// Set voice.
  Future<void> setVoice(String voiceName) async {
    await _service.setVoice(voiceName);
  }

  /// Set speech speed.
  Future<void> setSpeed(double speed) async {
    await _service.setSpeed(speed);
  }

  /// Set pitch.
  Future<void> setPitch(double pitch) async {
    await _service.setPitch(pitch);
  }

  /// Set volume.
  Future<void> setVolume(double volume) async {
    await _service.setVolume(volume);
  }

  @override
  void dispose() {
    _service.stop();
    super.dispose();
  }
}

/// Riverpod provider for TTS.
final ttsProvider = StateNotifierProvider<TtsNotifier, TtsState>((ref) {
  return TtsNotifier();
});
