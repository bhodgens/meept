// Speech-to-text service wrapping the speech_to_text Flutter plugin.
// Uses platform-native speech recognition (macOS SFSpeechRecognizer,
// Windows SpeechRecognition) via the speech_to_text plugin.

import 'dart:async';
import 'package:flutter/foundation.dart';
import 'package:speech_to_text/speech_to_text.dart'
    as stt show SpeechToText, ListenMode, SpeechListenOptions;
import 'package:speech_to_text/speech_recognition_error.dart';

/// Client-side speech-to-text service.
///
/// Wraps the platform `speech_to_text` plugin to provide recording,
/// streaming recognition, and graceful cancellation.  All transcription
/// happens on-device; the daemon is not involved.
class SttService {
  final stt.SpeechToText _speech = stt.SpeechToText();
  bool _initialized = false;
  bool _isRecording = false;
  String _lastResult = '';
  Function(String)? _onError;

  /// Whether the underlying speech recognizer has been successfully
  /// initialized and is ready to accept recording requests.
  bool get isAvailable => _initialized;

  /// Whether the service is actively listening for speech input.
  bool get isRecording => _isRecording;

  /// Initialize the speech recognizer.
  ///
  /// Requests microphone/permission from the OS.  Returns `true` if the
  /// recognizer is available on the current platform.  Safe to call
  /// multiple times -- subsequent calls are no-ops.
  Future<bool> initialize() async {
    if (_initialized) return true;
    _initialized = await _speech.initialize(
      onError: _handleError,
      onStatus: _handleStatus,
    );
    return _initialized;
  }

  void _handleError(SpeechRecognitionError error) {
    _isRecording = false;
    _onError?.call(error.errorMsg);
  }

  void _handleStatus(String status) {
    if (status == 'notListening' || status == 'done') {
      _isRecording = false;
    }
  }

  /// Start recording speech input.
  ///
  /// [onResult] is called with the current best transcription text
  /// whenever the recognizer produces a new result.  [onError] is
  /// called when a recognition error occurs.
  void startRecording({
    String language = 'en_US',
    required Function(String) onResult,
    Function(String)? onError,
  }) {
    if (!_initialized || _isRecording) return;
    _onError = onError;
    _lastResult = '';
    _isRecording = true;
    _speech.listen(
      onResult: (result) {
        _lastResult = result.recognizedWords;
        onResult(_lastResult);
      },
      listenOptions: stt.SpeechListenOptions(
        localeId: language,
        listenMode: stt.ListenMode.dictation,
      ),
    );
  }

  /// Stop recording and return the final transcription text.
  ///
  /// Returns an empty string if no speech was recognized.
  Future<String> stopRecording() async {
    await _speech.stop();
    _isRecording = false;
    return _lastResult;
  }

  /// Cancel recording and discard any captured audio.
  ///
  /// Unlike [stopRecording], this does not wait for a final result
  /// and clears the transcription buffer.
  void cancelRecording() {
    _speech.cancel();
    _isRecording = false;
    _lastResult = '';
  }
}
