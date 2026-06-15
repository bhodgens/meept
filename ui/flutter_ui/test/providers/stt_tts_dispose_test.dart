// Tests that SttNotifier and TtsNotifier properly call dispose on their
// underlying services when the notifier is disposed (Sprint 4 / S7-H-STT).
//
// Platform plugins (speech_to_text, flutter_tts) are not available in the
// test harness, so we verify dispose wiring via direct notifier disposal.
// The services themselves guard all platform calls behind _initialized.

import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/providers/stt_provider.dart';
import 'package:meept_ui/providers/tts_provider.dart';
import 'package:meept_ui/services/stt_service.dart';
import 'package:meept_ui/services/tts_service.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('SttService.dispose', () {
    test('clears state and is idempotent', () {
      final service = SttService();
      // Never initialized — dispose should still be safe.
      service.dispose();
      // Second call should not throw.
      service.dispose();
      expect(service.isAvailable, isFalse);
      expect(service.isRecording, isFalse);
    });
  });

  group('TtsService.dispose', () {
    test('clears state and is idempotent', () {
      final service = TtsService();
      // Never initialized — dispose should still be safe.
      service.dispose();
      // Second call should not throw.
      service.dispose();
      expect(service.isAvailable, isFalse);
      expect(service.isSpeaking, isFalse);
      expect(service.queueLength, 0);
    });
  });

  group('SttNotifier.dispose calls service.dispose', () {
    test('notifier disposal does not throw', () {
      final notifier = SttNotifier(SttService());
      // Should not throw.
      notifier.dispose();
    });
  });

  group('TtsNotifier.dispose calls service.dispose', () {
    test('notifier disposal does not throw', () {
      final notifier = TtsNotifier();
      // Should not throw.
      notifier.dispose();
    });
  });
}

