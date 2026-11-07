// voice_audio_browser.dart - the concrete browser audio pipeline
// (voice-local-leg leaf 13b, T4).
//
// Web builds resolve here through voice_audio.dart's conditional import.
// Mic capture: getUserMedia -> MediaRecorder (whole-blob model). WAV
// playback: AudioContext.decodeAudioData -> BufferSource. All through
// package:web + dart:js_interop (no new dependencies).
//
// The lifecycle is deliberately start/stop/whole-blob (no streaming):
// the daemon's local leg cannot accept browser audio today (unix-socket
// pair under $MEEPT_HOME/voice/sock, CL8 - no audio over RPC), so this
// is scaffolding behind the [VoiceAudio] interface for a future
// transport leaf.
import 'dart:async';
import 'dart:js_interop';
import 'dart:typed_data';

import 'package:flutter/foundation.dart' show debugPrint;
import 'package:web/web.dart' as web;

import 'voice_audio.dart'
    show VoiceAudio, VoiceAudioException, VoiceCaptureResult;

/// Conditional-import seam: the web build's real factory.
VoiceAudio createPlatformVoiceAudio() => BrowserVoiceAudio();

/// Browser-backed [VoiceAudio].
class BrowserVoiceAudio implements VoiceAudio {
  web.MediaStream? _micStream;
  web.MediaRecorder? _recorder;
  final List<web.Blob> _chunks = <web.Blob>[];
  web.AudioContext? _playbackContext;
  bool _disposed = false;

  @override
  bool get isSupported => true;

  @override
  Future<void> startCapture() async {
    if (_disposed) {
      throw const VoiceAudioException('voice: audio service disposed');
    }
    if (_recorder != null) return; // already capturing
    try {
      _micStream = await web.window.navigator.mediaDevices
          .getUserMedia(web.MediaStreamConstraints(audio: true.toJS))
          .toDart;
    } catch (e) {
      debugPrint('[voice-audio] getUserMedia failed: $e');
      throw const VoiceAudioException(
        'voice: mic permission denied or unavailable',
      );
    }
    _chunks.clear();
    final recorder = web.MediaRecorder(_micStream!);
    _recorder = recorder;
    recorder.ondataavailable = ((web.Event event) {
      final blobEvent = event as web.BlobEvent;
      _chunks.add(blobEvent.data);
    }).toJS;
    recorder.start();
  }

  @override
  Future<VoiceCaptureResult> stopCapture() async {
    final recorder = _recorder;
    if (recorder == null) {
      throw const VoiceAudioException('voice: capture not started');
    }
    // stop() fires a final dataavailable before onstop; one microtask
    // turn lets the handler collect it (whole-blob model).
    recorder.stop();
    await Future<void>.delayed(Duration.zero);
    _recorder = null;
    for (final track in _micStream?.getTracks().toDart ?? const []) {
      track.stop();
    }
    _micStream = null;

    final bytes = <int>[];
    for (final chunk in _chunks) {
      final buffer = await chunk.arrayBuffer().toDart;
      bytes.addAll(Uint8List.view(buffer.toDart));
    }
    _chunks.clear();
    return VoiceCaptureResult(bytes: bytes, mimeType: 'audio/webm');
  }

  @override
  Future<void> playWav(List<int> wavBytes) async {
    if (_disposed) {
      throw const VoiceAudioException('voice: audio service disposed');
    }
    if (wavBytes.isEmpty) return;
    _playbackContext ??= web.AudioContext();
    final ctx = _playbackContext!;
    final JSArrayBuffer buffer = Uint8List.fromList(wavBytes).buffer.toJS;
    final web.AudioBuffer decoded;
    try {
      decoded = await ctx.decodeAudioData(buffer).toDart;
    } catch (e) {
      debugPrint('[voice-audio] decodeAudioData failed: $e');
      throw const VoiceAudioException('voice: could not decode audio');
    }
    final source = ctx.createBufferSource();
    source.buffer = decoded;
    source.connect(ctx.destination);
    source.onended = (() {
      source.disconnect();
    }).toJS;
    source.start();
  }

  @override
  Future<void> dispose() async {
    _disposed = true;
    if (_recorder != null) {
      _recorder!.stop();
      _recorder = null;
    }
    for (final track in _micStream?.getTracks().toDart ?? const []) {
      track.stop();
    }
    _micStream = null;
    _chunks.clear();
    _playbackContext?.close();
    _playbackContext = null;
  }
}
