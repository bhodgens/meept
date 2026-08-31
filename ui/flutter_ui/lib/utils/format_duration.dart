/// Duration formatting utilities mirroring the TUI's FormatDuration parity.
///
/// All functions are kIsWeb-safe (no dart:io).
library;

/// Format a [Duration] using the same parity contract as the TUI:
///  - negative (past-due) -> "resuming…"
///  - >= 1h -> "Nh Mm" (omit minutes when 0)
///  - >= 1m -> "Mm"
///  - < 1m -> "Ss"
///
/// Byte-matched to internal/tui/format_duration.go:FormatDuration.
///
/// Note: Dart's [Duration] class normalizes all values to be non-negative
/// internally, so to represent a negative (past-due) duration you must pass
/// a [Duration] whose [inMilliseconds] is negative (i.e. constructed via
/// [Duration.microseconds] with a negative value or by arithmetic).
String formatDuration(Duration d) {
  if (d.inMilliseconds < 0) return 'resuming…';
  final totalSeconds = d.inSeconds.abs();
  if (totalSeconds < 60) {
    return '$totalSeconds s';
  }
  final totalMinutes = d.inMinutes.abs();
  if (totalMinutes < 60) {
    return '$totalMinutes m';
  }
  final hours = d.inHours.abs();
  final minutes = totalMinutes % 60;
  if (minutes == 0) {
    return '$hours h';
  }
  return '$hours h $minutes m';
}
