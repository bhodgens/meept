import '../../providers/agent_provider.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../utils/format_duration.dart';
import 'package:flutter/material.dart';

/// Quota status badge widgets shared by the agents tab (leaf 09,
/// quota-reset-resilience). Countdown text is byte-matched to the TUI's
/// FormatQuotaCountdown (internal/tui/quota_status.go) — do not change the
/// strings without updating the TUI side.
///
/// M9 timezone convention: the wire RFC3339 carries the DAEMON's offset.
/// Surfaces render DAEMON-LOCAL wall-clock by default — never UTC, never
/// the device zone. `useDeviceTimeForQuota` (settings toggle, default
/// off) switches to client-local, mirroring the TUI's
/// client.json5 `rendering.time_display: "daemon"|"local"`.
///
/// I-M8: the park-event `reason` distinguishes give-up from wait —
/// `reason == 'throttle_give_up'` renders the give-up badge instead of a
/// wait label (byte-matched to the TUI's QuotaWaitLabel).

/// Default for the client-local time toggle: OFF — surfaces render the
/// daemon's wall-clock by default (M9 user decision). The live value comes
/// from renderingPrefs (client.json5 `rendering.time_display`); this
/// constant is the fallback before prefs load and in tests.
const bool useDeviceTimeForQuota = false;

/// Daemon zone label for the detail line, from the daemon offset minutes
/// embedded in the RFC3339 value ("+02" / "-05:30" / "UTC" for Z).
/// Mirrors the zone abbreviation the TUI detail line renders.
String _daemonZoneLabel(int? offsetMinutes) {
  if (offsetMinutes == null) return '';
  if (offsetMinutes == 0) return 'UTC';
  final sign = offsetMinutes < 0 ? '-' : '+';
  final abs = offsetMinutes.abs();
  final h = (abs ~/ 60).toString().padLeft(2, '0');
  final m = (abs % 60).toString().padLeft(2, '0');
  return m == '00' ? '$sign$h' : '$sign$h:$m';
}

/// True when the client runs in the daemon's zone (or an unknown offset
/// forces the device interpretation): with the local toggle ON there is
/// nothing to convert, and the daemon/device rendering coincide.
bool _isClientZoneOffset(int? offsetMinutes) {
  if (offsetMinutes == null) return true;
  return offsetMinutes == DateTime.now().timeZoneOffset.inMinutes;
}

/// Renders the HH:MM wall-clock for a wait instant: daemon-local (the
/// offset embedded in the wire RFC3339, captured at parse time as
/// [AgentQuotaState.quotaWaitUntilOffsetMinutes]) by default; client-local
/// when [useDeviceTime] is on. Never UTC.
String _formatQuotaHHmm(int epochMs, int? offsetMinutes, bool useDeviceTime) {
  if (useDeviceTime || _isClientZoneOffset(offsetMinutes)) {
    final t = DateTime.fromMillisecondsSinceEpoch(epochMs);
    return '${t.hour.toString().padLeft(2, '0')}:'
        '${t.minute.toString().padLeft(2, '0')}';
  }
  // Daemon-local: epoch shifted by (daemon offset − device offset),
  // rendered with the device getters.
  final delta = offsetMinutes! - DateTime.now().timeZoneOffset.inMinutes;
  final t = DateTime.fromMillisecondsSinceEpoch(epochMs + delta * 60000);
  return '${t.hour.toString().padLeft(2, '0')}:'
      '${t.minute.toString().padLeft(2, '0')}';
}

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

/// Absolute HH:MM of the wait instant per the M9 convention: the
/// daemon-local wall-clock (offset captured from the wire RFC3339 at parse
/// time) by default; client-local when [useDeviceTime] is on. The TUI
/// renders the same instant from the parsed time.Time the same way
/// (QuotaWaitLabel via renderQuotaHHmm). Null when there is no wait time.
String? _quotaWaitHHmm(AgentQuotaState? state, bool useDeviceTime) {
  if (state == null || state.quotaWaitUntilEpoch == null) return null;
  return _formatQuotaHHmm(
    state.quotaWaitUntilEpoch!,
    state.quotaWaitUntilOffsetMinutes,
    useDeviceTime,
  );
}

/// The leaf 04 wait label (tree 03 leaf 04 Task 4 + I-M8, byte-matched to
/// the TUI's QuotaWaitLabel in internal/tui/quota_status.go — change both
/// together):
///
///   quota class (or absent): "quota_wait · reset HH:MM"
///   throttle class:          "quota_wait · throttle retry HH:MM"
///   throttle_give_up reason: "throttle gave up · action required"
///
/// Null when there is no wait time (agents without a parked turn never
/// build the badge) and the reason is not a give-up. Lowercase per
/// AGENTS.md UI rule.
String? quotaWaitLabel(AgentQuotaState? state,
    {bool useDeviceTime = useDeviceTimeForQuota}) {
  if (state == null || state.quotaBlocked) return null;
  // I-M8: a throttle give-up is a failure surface, not a wait — the badge
  // must not imply the agent is still parked.
  if (state.reason == 'throttle_give_up') {
    return 'throttle gave up · action required';
  }
  final hhmm = _quotaWaitHHmm(state, useDeviceTime);
  if (hhmm == null) return null;
  if (state.waitClass == 'throttle') {
    return 'quota_wait · throttle retry $hhmm';
  }
  return 'quota_wait · reset $hhmm';
}

/// A small badge showing quota wait, give-up, or blocked status under an
/// agent tile. Lowercase text per AGENTS.md; amber (warning) tone for
/// quota wait, red (error) tone for blocked and give-up. Agents without
/// quota episodes never build this widget, so their tiles render exactly
/// as before.
class QuotaStatusBadge extends StatelessWidget {
  final AgentQuotaState quotaState;
  final bool useDeviceTime;

  const QuotaStatusBadge({
    super.key,
    required this.quotaState,
    this.useDeviceTime = useDeviceTimeForQuota,
  });

  @override
  Widget build(BuildContext context) {
    if (quotaState.quotaBlocked) {
      return const _BlockedBadge();
    }
    // Leaf 04 label + I-M8 give-up override: "quota_wait · reset HH:MM" /
    // "quota_wait · throttle retry HH:MM" / "throttle gave up · action
    // required" — byte-matched to the TUI's QuotaWaitLabel. Null (no wait
    // time and not a give-up) renders nothing, so agents without a parked
    // turn are unchanged.
    final text = quotaWaitLabel(quotaState, useDeviceTime: useDeviceTime);
    if (text == null) return const SizedBox.shrink();
    final isGiveUp = quotaState.reason == 'throttle_give_up';
    final tone = isGiveUp ? CyberpunkColors.redAlert : CyberpunkColors.yellowWarning;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 2),
      decoration: BoxDecoration(
        color: tone.withValues(alpha: 0.2),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Text(
        text,
        style: CyberpunkTypography.bodySmall.copyWith(
          color: tone,
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
/// renders byte-identically for agents never quota-hit. <time> follows the
/// M9 convention: the DAEMON-local wall-clock by default — never UTC as
/// the pre-M9 implementation rendered — or client-local when
/// [useDeviceTime] is on. A zone hint ("+02") is appended when rendering
/// the daemon clock from a different client zone, mirroring the TUI
/// detail line's "15:04 MST".
List<String> quotaDetailLines(String? primaryModel, String? fallbackModel,
    int? waitUntilEpoch,
    {int? waitUntilOffsetMinutes,
    bool useDeviceTime = useDeviceTimeForQuota}) {
  if (fallbackModel == null || fallbackModel.isEmpty) return const [];
  var until = 'unknown';
  if (waitUntilEpoch != null) {
    until = _formatQuotaHHmm(waitUntilEpoch, waitUntilOffsetMinutes,
        useDeviceTime || _isClientZoneOffset(waitUntilOffsetMinutes));
    if (!useDeviceTime && !_isClientZoneOffset(waitUntilOffsetMinutes)) {
      until += ' ${_daemonZoneLabel(waitUntilOffsetMinutes)}';
    }
  }
  final primary = (primaryModel == null || primaryModel.isEmpty)
      ? 'unknown'
      : primaryModel;
  return [
    'primary: $primary (blocked until $until)',
    'active: $fallbackModel',
  ];
}
