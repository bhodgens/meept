// Text-to-speech service wrapping the flutter_tts Flutter plugin.
// Uses platform-native TTS synthesis (macOS AVSpeechSynthesizer,
// Windows SAPI, Android TextToSpeech) via the flutter_tts plugin.
//
// All synthesis happens client-side; the daemon is not involved.

import 'package:flutter_tts/flutter_tts.dart';

/// TTS configuration for queue/interrupt behavior.
class TtsConfig {
  bool enabled;
  String voice;
  double volume;
  double rate;
  bool interruptOnNewMsg;
  bool queueMessages;
  int maxQueueSize;

  TtsConfig({
    this.enabled = false,
    this.voice = 'en-US',
    this.volume = 1.0,
    this.rate = 0.5,
    this.interruptOnNewMsg = true,
    this.queueMessages = false,
    this.maxQueueSize = 5,
  });
}

/// Client-side text-to-speech service.
///
/// Wraps the platform `flutter_tts` plugin to provide speech synthesis
/// with interrupt/queue behavior and configuration support.
/// All synthesis happens client-side; the daemon is not involved.
class TtsService {
  final FlutterTts _flutterTts = FlutterTts();
  TtsConfig _config = TtsConfig();

  bool _initialized = false;
  bool _isSpeaking = false;
  final List<String> _queue = [];
  bool _processingQueue = false;

  Function(String)? _onError;
  Function()? _onStart;
  Function()? _onComplete;

  /// Whether the underlying TTS engine has been successfully initialized.
  bool get isAvailable => _initialized;

  /// Whether the service is actively speaking.
  bool get isSpeaking => _isSpeaking;

  /// Current queue length.
  int get queueLength => _queue.length;

  /// Apply new configuration.
  void applyConfig(TtsConfig config) {
    _config = config;
    if (_initialized) {
      _flutterTts.setVolume(config.volume);
      _flutterTts.setSpeechRate(config.rate);
    }
  }

  /// Initialize the TTS engine.
  ///
  /// Returns `true` if the engine is available on the current platform.
  /// Safe to call multiple times -- subsequent calls are no-ops.
  Future<bool> initialize() async {
    if (_initialized) return true;

    try {
      // Set default configuration from current config
      await _flutterTts.setLanguage(_config.voice.split('_').first ?? "en-US");
      await _flutterTts.setSpeechRate(_config.rate);
      await _flutterTts.setVolume(_config.volume);
      await _flutterTts.setPitch(1.0);

      // Set up completion handlers
      _flutterTts.setStartHandler(() {
        _isSpeaking = true;
        _onStart?.call();
      });

      _flutterTts.setCompletionHandler(() {
        _isSpeaking = false;
        _onComplete?.call();
        _processQueue();
      });

      _flutterTts.setErrorHandler((error) {
        _isSpeaking = false;
        _queue.clear();
        _onError?.call(error);
      });

      _initialized = true;
      return true;
    } catch (e) {
      return false;
    }
  }

  void _handleError(String error) {
    _isSpeaking = false;
    _onError?.call(error);
  }

  /// Start speaking the given text, respecting interrupt/queue behavior.
  ///
  /// If [interruptOnNewMsg] is true and already speaking, stops current
  /// speech and starts new text immediately.
  ///
  /// If [queueMessages] is true, adds text to queue for later playback.
  Future<void> speak(String text) async {
    if (!_initialized) {
      final initialized = await initialize();
      if (!initialized) return;
    }

    if (text.isEmpty) return;

    if (_isSpeaking) {
      if (_config.interruptOnNewMsg) {
        await stop();
        await _doSpeak(text);
      } else if (_config.queueMessages) {
        _addToQueue(text);
      }
      return;
    }

    await _doSpeak(text);
  }

  Future<void> _doSpeak(String text) async {
    try {
      await _flutterTts.speak(text);
    } catch (e) {
      _handleError(e.toString());
    }
  }

  void _addToQueue(String text) {
    if (_queue.length >= _config.maxQueueSize) {
      _queue.removeAt(0); // Drop oldest
    }
    _queue.add(text);
    if (!_processingQueue) {
      _processQueue();
    }
  }

  Future<void> _processQueue() async {
    if (_queue.isEmpty || _processingQueue || _isSpeaking) return;

    _processingQueue = true;
    while (_queue.isNotEmpty && !_isSpeaking) {
      final next = _queue.removeAt(0);
      await _doSpeak(next);
    }
    _processingQueue = false;
  }

  /// Stop any ongoing speech and clear queue.
  ///
  /// Returns immediately if not speaking.
  Future<void> stop() async {
    if (_isSpeaking || _queue.isNotEmpty) {
      await _flutterTts.stop();
      _isSpeaking = false;
      _queue.clear();
      _processingQueue = false;
    }
  }

  /// Get list of available voices.
  ///
  /// Returns a list of voice maps with 'name', 'locale', and 'quality' keys.
  Future<List<Map<String, dynamic>>> getVoices() async {
    if (!_initialized) {
      final initialized = await initialize();
      if (!initialized) return [];
    }

    try {
      final voices = await _flutterTts.getVoices;
      return voices.cast<Map<String, dynamic>>();
    } catch (e) {
      return [];
    }
  }

  /// Set the voice to use for synthesis.
  Future<void> setVoice(String voiceName) async {
    _config.voice = voiceName;
    if (!_initialized) {
      await initialize();
    }
    await _flutterTts.setVoice(voiceName);
  }

  /// Set the speech rate (0.0 to 1.0).
  Future<void> setSpeed(double speed) async {
    _config.rate = speed;
    await _flutterTts.setSpeechRate(speed);
  }

  /// Set the pitch (0.5 to 2.0).
  Future<void> setPitch(double pitch) async {
    await _flutterTts.setPitch(pitch);
  }

  /// Set the volume (0.0 to 1.0).
  Future<void> setVolume(double volume) async {
    _config.volume = volume;
    await _flutterTts.setVolume(volume);
  }
}
