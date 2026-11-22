import 'package:flutter/material.dart';

import '../../theme/colors.dart';
import '../../theme/typography.dart';

/// Leaf 03 (plan failure visibility): plan notification bubbles rendered
/// inline in the GUI chat stream from `plan_update` WS frames, mirroring
/// the TUI plan cards (internal/tui/models/chat.go renderPlanNotification).
///
/// The daemon's transformBusEventToWS stamps every frame with
/// `source_topic`; plan lifecycle topics are plan.submitting /
/// plan.completed / plan.confirmed / plan.rejected / plan.failed.

/// Parsed plan_update frame for the chat-stream bubble.
class PlanNotificationData {
  PlanNotificationData({
    required this.state,
    this.planId = '',
    this.sessionId = '',
    this.title = '',
    this.phaseCount = 0,
    this.stepCount = 0,
    this.by = '',
    this.reason = '',
    this.hasSessionKey = true,
  });

  /// One of: submitting / completed / confirmed / rejected / failed.
  /// Empty (unknown source_topic) renders the generic shape.
  final String state;
  final String planId;
  final String sessionId;
  final String title;
  final int phaseCount;
  final int stepCount;
  final String by;
  final String reason;

  /// Whether the raw frame carried a session_id key at all (a MISSING
  /// key is a legacy publisher; an EMPTY value is an evolver sink).
  final bool hasSessionKey;

  /// Parse a raw `plan_update` WS frame (map) into bubble data.
  /// Discriminates the state from `source_topic` (the frame's `type` is
  /// always `plan_update` on that channel).
  static PlanNotificationData fromFrame(Map<String, dynamic> frame) {
    String topic = frame['source_topic'] as String? ?? '';
    if (topic.isEmpty) {
      // Older daemons may not stamp source_topic on every frame; the
      // topic is also the last path segment of a full bus topic string.
      final raw = frame['topic'] as String? ?? '';
      topic = raw.split('.').length >= 2 ? raw : '';
    }
    String state = '';
    for (final s in const [
      'submitting',
      'completed',
      'confirmed',
      'rejected',
      'failed',
    ]) {
      if (topic == 'plan.$s') {
        state = s;
        break;
      }
    }
    int intOf(String key) {
      final v = frame[key];
      if (v is int) return v;
      if (v is double) return v.round();
      if (v is String) return int.tryParse(v) ?? 0;
      return 0;
    }

    return PlanNotificationData(
      state: state,
      planId: frame['plan_id'] as String? ?? '',
      sessionId: frame['session_id'] as String? ?? '',
      title: frame['title'] as String? ?? '',
      phaseCount: intOf('phase_count'),
      stepCount: intOf('step_count'),
      by: frame['by'] as String? ?? '',
      reason: frame['reason'] as String? ?? '',
      hasSessionKey: frame.containsKey('session_id'),
    );
  }

  /// Session scoping, mirroring the TUI chat pump rule: an explicitly
  /// EMPTY session_id marks a machine-originated (evolver sink) plan
  /// that belongs to no chat - drop it. A frame whose session matches
  /// the active chat renders; a MISSING key is a legacy publisher and
  /// renders best-effort.
  bool shouldRenderIn(String activeSessionId) {
    if (hasSessionKey) {
      if (sessionId.isEmpty) return false;
      if (sessionId != activeSessionId) return false;
    }
    return true;
  }

  /// Count row, reusing the TUI planCountLine semantics: a plan with no
  /// phases and no steps is a document proposal (evolver skill plans are
  /// markdown files awaiting approval) and must never read "0 phases -
  /// 0 steps".
  String get countLine {
    if (phaseCount == 0 && stepCount == 0) {
      return 'document proposal - no executable steps';
    }
    final phaseStr = phaseCount == 1 ? '1 phase' : '$phaseCount phases';
    final stepStr = stepCount == 1 ? '1 step' : '$stepCount steps';
    return '$phaseStr - $stepStr';
  }

  /// Lowercase header line per state (TUI parity).
  String get headerLine {
    switch (state) {
      case 'submitting':
        return 'plan ready for review';
      case 'completed':
        return 'plan completed';
      case 'confirmed':
        return 'plan confirmed';
      case 'rejected':
        return 'plan rejected';
      case 'failed':
        return 'plan failed';
      default:
        return state.isEmpty ? 'plan update' : 'plan $state';
    }
  }
}

/// Inline chat-stream bubble for a plan lifecycle event.
class PlanNotificationBubble extends StatelessWidget {
  const PlanNotificationBubble({super.key, required this.data});

  final PlanNotificationData data;

  Color get _accent {
    switch (data.state) {
      case 'failed':
      case 'rejected':
        return CyberpunkColors.redAlert;
      case 'completed':
      case 'confirmed':
        return CyberpunkColors.greenSuccess;
      default:
        return CyberpunkColors.orangePrimary;
    }
  }

  @override
  Widget build(BuildContext context) {
    final detailLines = <String>[];
    if (data.state == 'submitting') {
      detailLines.add(data.countLine);
      detailLines.add('plans tab to review');
    } else if (data.state == 'completed') {
      detailLines.add('${data.countLine} - awaiting confirmation');
      detailLines.add('plans tab to confirm');
    } else if (data.state == 'confirmed') {
      detailLines.add('confirmed by ${data.by.isEmpty ? 'user' : data.by}');
    } else if (data.state == 'rejected' || data.state == 'failed') {
      final reason = data.reason.isEmpty ? 'no reason given' : data.reason;
      detailLines.add('reason: $reason');
    }

    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Container(
        width: double.infinity,
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        decoration: BoxDecoration(
          color: CyberpunkColors.darkGray,
          border: Border(left: BorderSide(color: _accent, width: 3)),
          borderRadius: BorderRadius.circular(4),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              data.headerLine,
              style: CyberpunkTypography.code.copyWith(
                color: _accent,
                fontWeight: FontWeight.bold,
              ),
            ),
            if (data.title.isNotEmpty)
              Text('plan: "${data.title}"', style: CyberpunkTypography.code),
            for (final line in detailLines)
              Text(line, style: CyberpunkTypography.code),
          ],
        ),
      ),
    );
  }
}
