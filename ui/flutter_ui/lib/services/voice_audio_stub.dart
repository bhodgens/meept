// voice_audio_stub.dart - the non-web half of the voice audio split
// (voice-local-leg leaf 13b, T4).
//
// Native/test builds resolve here through voice_audio.dart's
// conditional import. Everything is a named refusal: the mic controls
// degrade via [isSupported] == false and never call getUserMedia.
import 'voice_audio.dart'
    show VoiceAudio, VoiceAudioException, VoiceCaptureResult;

/// Unsupported-platform implementation: every operation refuses with the
/// named error. Widget tests and native desktop builds get this one.
class UnsupportedVoiceAudio implements VoiceAudio {
  const UnsupportedVoiceAudio();

  @override
  bool get isSupported => false;

  @override
  Future<void> startCapture() async {
    throw const VoiceAudioException(
      'voice: audio capture requires the web GUI',
    );
  }

  @override
  Future<VoiceCaptureResult> stopCapture() async {
    throw const VoiceAudioException(
      'voice: audio capture requires the web GUI',
    );
  }

  @override
  Future<void> playWav(List<int> wavBytes) async {
    throw const VoiceAudioException(
      'voice: audio playback requires the web GUI',
    );
  }

  @override
  Future<void> dispose() async {}
}

/// Conditional-import seam: the web half (voice_audio_browser.dart)
/// provides the real one.
VoiceAudio createPlatformVoiceAudio() => const UnsupportedVoiceAudio();
