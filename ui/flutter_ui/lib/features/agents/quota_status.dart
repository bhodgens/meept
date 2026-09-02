import '../../providers/agent_provider.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../utils/format_duration.dart';
import 'package:flutter/material.dart';

/// Quota status badge widgets shared by the agents tab (leaf 09,
/// quota-reset-resilience). Countdown text is byte-matched to the TUI's
/// FormatQuotaCountdown (internal/tui/quota_status.go) — do not change the
/// strings without updating the TUI side.

/// Returns the countdown hint for an [AgentQuotaState]: "quota resets in
/// 3h 12m" while waiting, "resets soon" when the unblock time has passed.
/// Null (no wait time) with a blocked state yields null — the blocked badge
/// carries the "action required" hint itself. The "quota wait · " state
/// label is prepended by [QuotaStatusBadge] (parity with the TUI badge
/// format in internal/tui/agents_panel.go quotaStatusBadge).
String? quotaCountdownText(AgentQuotaState? state) {
  if (state == null || state.quotaBlocked) return null;
  final waitUntil = state.quotaWaitUntilEpoch;
  if (waitUntil == null) return null;
  final remaining =
      Duration(milliseconds: waitUntil - DateTime.now().millisecondsSinceEpoch);
  if (remaining.inMilliseconds <= 0) {
    return 'resets soon';
  }
  return 'quota resets in ${formatDuration(remaining)}';
}

/// Absolute HH:MM of [waitUntilEpoch] in the LOCAL zone — the TUI formats
/// the daemon-provided resume instant the same way (QuotaWaitLabel renders
/// `unblockAt.Format("15:04")` on the daemon host's local zone). Rendering
/// the absolute time keeps both surfaces off relative countdown math, which
/// cannot be trusted on the web build.
String? _quotaWaitHHmm(AgentQuotaState? state) {
  if (state == null || state.quotaWaitUntilEpoch == null) return null;
  final t = DateTime.fromMillisecondsSinceEpoch(state.quotaWaitUntilEpoch!);
  final hh = t.hour.toString().padLeft(2, '0');
  final mm = t.minute.toString().padLeft(2, '0');
  return '$hh:$mm';
}

/// The leaf 04 wait label (tree 03 leaf 04 Task 4, byte-matched to the TUI's
/// QuotaWaitLabel in internal/tui/quota_status.go — change both together):
///
///   quota class (or absent): "quota_wait · reset HH:MM"
///   throttle class:          "quota_wait · throttle retry HH:MM"
///
/// Null when there is no wait time (agents without a parked turn never
/// build the badge). Lowercase per AGENTS.md UI rule.
String? quotaWaitLabel(AgentQuotaState? state) {
  if (state == null || state.quotaBlocked) return null;
  final hhmm = _quotaWaitHHmm(state);
  if (hhmm == null) return null;
  if (state.waitClass == 'throttle') {
    return 'quota_wait · throttle retry $hhmm';
  }
  return 'quota_wait · reset $hhmm';
}

/// A small badge showing quota wait or blocked status under an agent tile.
/// Lowercase text per AGENTS.md; amber (warning) tone for quota wait, red
/// (error) tone for blocked. Agents without quota episodes never build this
/// widget, so their tiles render exactly as before.
class QuotaStatusBadge extends StatelessWidget {
  final AgentQuotaState quotaState;

  const QuotaStatusBadge({super.key, required this.quotaState});

  @override
  Widget build(BuildContext context) {
    if (quotaState.quotaBlocked) {
      return const _BlockedBadge();
    }
    // Leaf 04 label: "quota_wait · reset HH:MM" / "quota_wait · throttle
    // retry HH:MM" — byte-matched to the TUI's QuotaWaitLabel. Null (no
    // wait time) renders nothing, so agents without a parked turn are
    // unchanged.
    final text = quotaWaitLabel(quotaState);
    if (text == null) return const SizedBox.shrink();
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 2),
      decoration: BoxDecoration(
        color: CyberpunkColors.yellowWarning.withValues(alpha: 0.2),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Text(
        text,
        style: CyberpunkTypography.bodySmall.copyWith(
          color: CyberpunkColors.yellowWarning,
          fontFamily: 'SourceCodePro',
        ),
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
    );
  }
}

/// Badge for agents stuck in blocked state (24h max-wait escalation):
/// red container, "blocked · action required".
class _BlockedBadge extends StatelessWidget {
  const _BlockedBadge();

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 2),
      decoration: BoxDecoration(
        color: CyberpunkColors.redAlert.withValues(alpha: 0.2),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Text(
        'blocked · action required',
        style: CyberpunkTypography.bodySmall.copyWith(
          color: CyberpunkColors.redAlert,
          fontFamily: 'SourceCodePro',
        ),
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
    );
  }
}

/// Primary/active model lines for the agent detail view, shown only when a
/// fallback model is carrying work while the primary provider waits out its
/// quota reset:
///
///     primary: <primary model> (blocked until <time>)
///     active: <fallback model>
///
/// Returns an empty list when [fallbackModel] is absent so the detail view
/// renders byte-identically for agents never quota-hit.
List<String> quotaDetailLines(
    String? primaryModel, String? fallbackModel, int? waitUntilEpoch) {
  if (fallbackModel == null || fallbackModel.isEmpty) return const [];
  var until = 'unknown';
  if (waitUntilEpoch != null) {
    final t =
        DateTime.fromMillisecondsSinceEpoch(waitUntilEpoch).toUtc().toIso8601String();
    until = t.substring(11, 16); // "HH:mm" of the UTC timestamp
  }
  final primary = (primaryModel == null || primaryModel.isEmpty)
      ? 'unknown'
      : primaryModel;
  return [
    'primary: $primary (blocked until $until)',
    'active: $fallbackModel',
  ];
}
