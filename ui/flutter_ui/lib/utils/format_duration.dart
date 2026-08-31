/// Duration formatting utilities mirroring the TUI's FormatDuration parity.
///
/// All functions are kIsWeb-safe (no dart:io).
library;

/// Format a [Duration] using the same parity contract as the TUI
/// (internal/tui/format_duration.go, which mirrors internal/llm
/// errors_quota.go): values are truncated to the minute (never rounded up),
/// so a 90s wait shows as "1m" and a 30s wait as "0m". There is no seconds
/// tier.
///
///  - >= 1h with minutes -> "3h 12m"
///  - >= 1h, minutes == 0 -> "2h"
///  - < 1h               -> "45m"
///
/// Byte-matched to the Go formatter — do not change the strings without
/// updating the TUI side.
///
/// Note: Dart's [Duration] class normalizes all values to be non-negative
/// internally, so to represent a negative (past-due) duration you must pass
/// a [Duration] whose [inMilliseconds] is negative (i.e. constructed via
/// [Duration.microseconds] with a negative value or by arithmetic). Past-due
/// handling lives in the badge layer ("resets soon"), not here.
String formatDuration(Duration d) {
  // Past-due (negative): the badge layer renders "resets soon" for live
  // countdowns, but the formatter preserves the historical sentinel for
  // callers that pass a negative duration directly.
  if (d.inMilliseconds < 0) return 'resuming…';
  // Total seconds, flooring toward zero for negative (past-due) inputs so
  // truncation semantics match Go's time.Duration.Truncate(time.Minute).
  final totalSeconds = d.inSeconds;
  final totalMinutes = totalSeconds ~/ 60;
  if (totalMinutes >= 60) {
    final hours = totalMinutes ~/ 60;
    final minutes = totalMinutes % 60;
    if (minutes == 0) {
      return '${hours}h';
    }
    return '${hours}h ${minutes}m';
  }
  return '${totalMinutes}m';
}
