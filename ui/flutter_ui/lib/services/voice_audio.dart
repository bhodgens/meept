// voice_audio.dart - browser-side audio capture/playback for the voice
// page (voice-local-leg leaf 13b, T4).
//
// SCOPE (CL8 + leaf 13b): the voice.local.* plane carries CONTROL only -
// no audio over RPC. The daemon's local leg reads mic input from a unix
// socket a local client dials; a BROWSER tab cannot dial unix sockets.
// So the GUI ships the capture/playback LIFECYCLE (getUserMedia ->
// MediaRecorder for mic capture; AudioContext.decodeAudioData ->
// BufferSource for WAV playback) behind the [VoiceAudio] interface, with
// the browser implementation web-conditional and an unsupported stub for
// tests/native builds, so widget tests never touch getUserMedia.
//
// NO new pub dependencies: everything uses package:web (>=1.0.0,
// already in pubspec.yaml; platform_open_web.dart uses it the same way)
// plus dart:js_interop from the SDK.
//
// Conditional-import convention (same shape as platform_open_helpers):
// import THIS file; never the halves directly.
library;

import 'voice_audio_stub.dart'
    if (dart.library.js_interop) 'voice_audio_browser.dart'
    if (dart.library.html) 'voice_audio_browser.dart';

export 'voice_audio_stub.dart'
    if (dart.library.js_interop) 'voice_audio_browser.dart'
    if (dart.library.html) 'voice_audio_browser.dart';

/// A captured mic recording (MediaRecorder's default webm/opus
/// container). [bytes] is the whole recording at stop time.
class VoiceCaptureResult {
  final List<int> bytes;
  final String mimeType;

  const VoiceCaptureResult({required this.bytes, required this.mimeType});
}

/// Errors surfaced by the capture/playback lifecycle. The [message] is
/// lowercase and user-renderable; permission denial and unsupported
/// platforms are the two named cases.
class VoiceAudioException implements Exception {
  final String message;
  const VoiceAudioException(this.message);

  @override
  String toString() => message;
}

/// Capture/playback lifecycle. Implemented by [BrowserVoiceAudio] (web
/// builds); tests and native builds get [UnsupportedVoiceAudio] through
/// the conditional factory below.
abstract class VoiceAudio {
  /// Whether this platform can actually capture/play audio. The stub
  /// reports false; the page uses it to degrade the mic controls.
  bool get isSupported;

  /// Begin mic capture (requests permission via getUserMedia on first
  /// use - the browser shows its standard prompt).
  Future<void> startCapture();

  /// Stop capture and return the recorded bytes.
  Future<VoiceCaptureResult> stopCapture();

  /// Play [wavBytes] (a canonical WAV file) through the speakers.
  Future<void> playWav(List<int> wavBytes);

  /// Drop the mic stream / stop any playback.
  Future<void> dispose();
}

/// The concrete implementation for this build mode (web: the browser
/// pipeline; VM/native/tests: the unsupported stub).
VoiceAudio createVoiceAudio() => createPlatformVoiceAudio();
