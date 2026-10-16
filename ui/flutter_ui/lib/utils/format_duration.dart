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
/// Negative (past-due) inputs are NOT special-cased here: they flow through
/// the same truncation path as Go's time.Duration.Truncate, yielding e.g.
/// "-1m". The sentinel layering (byte-match contract, all three sides) is:
///
///  - badge layer (quotaCountdownText here / QuotaCountdownText in
///    internal/tui/quota_status.go / the llm errors_quota.go callers) owns
///    past-due rendering as "resets soon";
///  - the TUI countdown-string layer (FormatQuotaCountdownAt) owns "soon";
///  - this bare formatter has NO sentinel of its own — it truncates, for
///    negative inputs toward zero, exactly like the Go formatters.
///
/// Byte-matched to the Go formatter — do not change the strings without
/// updating the TUI side (internal/tui/format_duration.go) and the llm side
/// (internal/llm/errors_quota.go).
///
/// Note: Dart's [Duration] class normalizes all values to be non-negative
/// internally, so to represent a negative (past-due) duration you must pass
/// a [Duration] whose [inMilliseconds] is negative (i.e. constructed via
/// [Duration.microseconds] with a negative value or by arithmetic).
String formatDuration(Duration d) {
  // Total seconds, flooring toward zero for negative (past-due) inputs so
  // truncation semantics match Go's time.Duration.Truncate(time.Minute).
  // There is deliberately no negative-input sentinel branch: the bare
  // formatter truncates ("−90 minutes" -> "-90m") on every side, and the
  // badge/countdown layers own past-due wording.
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
