// Riverpod provider for TTS service.
// Follows the same pattern as stt_provider.dart.

import 'package:flutter/foundation.dart' show debugPrint;
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../services/tts_service.dart';
import '../services/storage_service.dart';

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
  final StorageService _storage;
  bool _enabled = false;

  TtsNotifier() : _service = TtsService(), _storage = StorageService.instance, super(TtsState.idle) {
    _loadSettings();
  }

  /// Load persisted TTS settings
  void _loadSettings() {
    _enabled = _storage.getTtsEnabled();
    // Apply other settings to service
    _service.applyConfig(TtsConfig(
      enabled: _enabled,
      voice: _storage.getTtsVoice() ?? 'en-US',
      volume: _storage.getTtsVolume(),
      rate: _storage.getTtsRate(),
      interruptOnNewMsg: _storage.getTtsInterrupt(),
      queueMessages: _storage.getTtsQueue(),
      maxQueueSize: _storage.getTtsMaxQueueSize(),
    ));
  }

  /// Persist current settings (called from setters)
  Future<void> _saveSettings({
    double? volume,
    double? rate,
    bool? interrupt,
    bool? queue,
    int? maxQueueSize,
  }) async {
    await _storage.setTtsEnabled(_enabled);
    if (volume != null) await _storage.setTtsVolume(volume);
    if (rate != null) await _storage.setTtsRate(rate);
    if (interrupt != null) await _storage.setTtsInterrupt(interrupt);
    if (queue != null) await _storage.setTtsQueue(queue);
    if (maxQueueSize != null) await _storage.setTtsMaxQueueSize(maxQueueSize);
  }

  /// Whether TTS is enabled.
  bool get enabled => _enabled;

  /// Whether the TTS service is available.
  bool get isAvailable => _service.isAvailable;

  /// Whether currently speaking.
  bool get isSpeaking => _service.isSpeaking;

  /// Initialize the TTS service.
  Future<bool> initialize() async {
    _service.setOnComplete(() {
      if (state == TtsState.speaking) {
        state = TtsState.idle;
      }
    });
    return await _service.initialize();
  }

  /// Toggle TTS on/off.
  Future<void> toggleTts() async {
    _enabled = !_enabled;
    if (!_enabled) {
      await stop();
    }
    await _saveSettings().catchError((e) {
      debugPrint('[tts] Failed to save settings: $e');
    });
  }

  /// Set TTS enabled state.
  Future<void> setEnabled(bool value) async {
    _enabled = value;
    if (!value) {
      await stop();
    }
    await _saveSettings().catchError((e) {
      debugPrint('[tts] Failed to save settings: $e');
    });
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
    await _storage.setTtsVoice(voiceName);
  }

  /// Set behavior settings
  Future<void> setBehaviorSettings({required bool interrupt, required bool queue, int? maxQueueSize}) async {
    _service.applyConfig(TtsConfig(
      enabled: _enabled,
      voice: _storage.getTtsVoice() ?? 'en-US',
      volume: _storage.getTtsVolume(),
      rate: _storage.getTtsRate(),
      interruptOnNewMsg: interrupt,
      queueMessages: queue,
      maxQueueSize: maxQueueSize ?? 5,
    ));
    await _saveSettings(interrupt: interrupt, queue: queue, maxQueueSize: maxQueueSize);
  }

  /// Set speech speed.
  Future<void> setSpeed(double speed) async {
    await _service.setSpeed(speed);
    await _storage.setTtsRate(speed);
  }

  /// Set pitch.
  Future<void> setPitch(double pitch) async {
    await _service.setPitch(pitch);
  }

  /// Set volume.
  Future<void> setVolume(double volume) async {
    await _service.setVolume(volume);
    await _storage.setTtsVolume(volume);
  }

  @override
  void dispose() {
    // Stop any in-flight synthesis, then release platform resources.
    _service.dispose();
    super.dispose();
  }
}

/// Riverpod provider for TTS.
final ttsProvider = StateNotifierProvider<TtsNotifier, TtsState>((ref) {
  return TtsNotifier();
});
