import 'package:flutter_riverpod/flutter_riverpod.dart';

/// Per-session pending message ID to scroll to.
///
/// Set by callers (e.g. SearchPanel message-result navigation) before
/// navigating to the chat view. ChatMessageList consumes and clears it
/// once messages have loaded and the scroll has been performed.
///
/// Value is the backend message ID as a string (matches `ChatMessage.id`).
/// Empty string means "no pending scroll".
final pendingScrollMessageProvider =
    StateProvider.family<String, String>((ref, sessionId) => '');
