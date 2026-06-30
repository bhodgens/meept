import 'dart:async';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// Transient status message shown by the StatusBar. Auto-clears after
/// 2.5 seconds. Null = no message (status bar renders normal content).
final statusMessageProvider = StateProvider<String?>((ref) => null);

/// Show a transient message. Auto-clears after 2.5 seconds.
void showStatusMessage(WidgetRef ref, String message) {
  ref.read(statusMessageProvider.notifier).state = message;
  Timer(const Duration(milliseconds: 2500), () {
    // Only clear if the same message is still showing (don't clobber a
    // newer message).
    if (ref.read(statusMessageProvider) == message) {
      ref.read(statusMessageProvider.notifier).state = null;
    }
  });
}
