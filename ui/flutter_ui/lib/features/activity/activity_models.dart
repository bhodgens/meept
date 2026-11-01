import 'package:flutter/foundation.dart';

/// ActivityRecord matches the C1 contract from the master plan.
/// JSON field names are identical to the RPC/HTTP surface.
class ActivityRecord {
  final String
  kind; // "task_step" | "session_message" | "audit_record" | "bus_event"
  final DateTime at;
  final String? agentId;
  final String? sessionId;
  final String? taskId;
  final String? stepId;
  final String? planId;
  final String? state;
  final String? severity; // info | warning | critical
  final String summary;
  final Map<String, String>? detail;

  const ActivityRecord({
    required this.kind,
    required this.at,
    this.agentId,
    this.sessionId,
    this.taskId,
    this.stepId,
    this.planId,
    this.state,
    this.severity,
    required this.summary,
    this.detail,
  });

  factory ActivityRecord.fromJson(Map<String, dynamic> json) {
    return ActivityRecord(
      kind: json['kind'] as String,
      at: DateTime.parse(json['at'] as String),
      agentId: json['agent_id'] as String?,
      sessionId: json['session_id'] as String?,
      taskId: json['task_id'] as String?,
      stepId: json['step_id'] as String?,
      planId: json['plan_id'] as String?,
      state: json['state'] as String?,
      severity: json['severity'] as String?,
      summary: json['summary'] as String,
      detail: json['detail'] as Map<String, String>?,
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'kind': kind,
      'at': at.toIso8601String(),
      if (agentId != null) 'agent_id': agentId,
      if (sessionId != null) 'session_id': sessionId,
      if (taskId != null) 'task_id': taskId,
      if (stepId != null) 'step_id': stepId,
      if (planId != null) 'plan_id': planId,
      if (state != null) 'state': state,
      if (severity != null) 'severity': severity,
      'summary': summary,
      if (detail != null) 'detail': detail,
    };
  }
}

/// ActivityQueryResult matches C2: {"records":[ActivityRecord],"total":int}
class ActivityQueryResult {
  final List<ActivityRecord> records;
  final int total;

  const ActivityQueryResult({required this.records, required this.total});

  factory ActivityQueryResult.fromJson(Map<String, dynamic> json) {
    final recordsRaw = json['records'] as List? ?? [];
    final records = recordsRaw
        .whereType<Map<String, dynamic>>()
        .map((r) => ActivityRecord.fromJson(r))
        .toList();
    return ActivityQueryResult(
      records: records,
      total: (json['total'] as num?)?.toInt() ?? records.length,
    );
  }
}

/// ActivityRecentResult matches C2: {"records":[...],"dropped":int,"total":int}
class ActivityRecentResult {
  final List<ActivityRecord> records;
  final int dropped;
  final int total;

  const ActivityRecentResult({
    required this.records,
    required this.dropped,
    required this.total,
  });

  factory ActivityRecentResult.fromJson(Map<String, dynamic> json) {
    final recordsRaw = json['records'] as List? ?? [];
    final records = recordsRaw
        .whereType<Map<String, dynamic>>()
        .map((r) => ActivityRecord.fromJson(r))
        .toList();
    return ActivityRecentResult(
      records: records,
      dropped: (json['dropped'] as num?)?.toInt() ?? 0,
      total: (json['total'] as num?)?.toInt() ?? records.length,
    );
  }
}

/// ActivityOverviewResult matches C2: {"by_agent":[...],"generated_at"}
class ActivityOverviewResult {
  final List<AgentOverview> byAgent;
  final DateTime generatedAt;

  const ActivityOverviewResult({
    required this.byAgent,
    required this.generatedAt,
  });

  factory ActivityOverviewResult.fromJson(Map<String, dynamic> json) {
    final byAgentRaw = json['by_agent'] as List? ?? [];
    final byAgent = byAgentRaw
        .whereType<Map<String, dynamic>>()
        .map((a) => AgentOverview.fromJson(a))
        .toList();
    return ActivityOverviewResult(
      byAgent: byAgent,
      generatedAt: DateTime.parse(json['generated_at'] as String),
    );
  }
}

class AgentOverview {
  final String agentId;
  final int activeTasks;
  final DateTime? lastActivity;
  final double? driftScore;
  final int openFindings;

  const AgentOverview({
    required this.agentId,
    required this.activeTasks,
    this.lastActivity,
    this.driftScore,
    required this.openFindings,
  });

  factory AgentOverview.fromJson(Map<String, dynamic> json) {
    return AgentOverview(
      agentId: json['agent_id'] as String,
      activeTasks: (json['active_tasks'] as num?)?.toInt() ?? 0,
      lastActivity: json['last_activity'] != null
          ? DateTime.parse(json['last_activity'] as String)
          : null,
      driftScore: json['drift_score'] as double?,
      openFindings: (json['open_findings'] as num?)?.toInt() ?? 0,
    );
  }
}

/// ActivityFindingsResult matches C2: {"findings":[...],"total":int}
class ActivityFindingsResult {
  final List<AuditFinding> findings;
  final int total;

  const ActivityFindingsResult({required this.findings, required this.total});

  factory ActivityFindingsResult.fromJson(Map<String, dynamic> json) {
    final findingsRaw = json['findings'] as List? ?? [];
    final findings = findingsRaw
        .whereType<Map<String, dynamic>>()
        .map((f) => AuditFinding.fromJson(f))
        .toList();
    return ActivityFindingsResult(
      findings: findings,
      total: (json['total'] as num?)?.toInt() ?? findings.length,
    );
  }
}

class AuditFinding {
  final String id;
  final String? employeeId;
  final String severity; // info | warning | critical
  final String summary;
  final DateTime? createdAt;
  final bool resolved;
  final String? planId;

  const AuditFinding({
    required this.id,
    this.employeeId,
    required this.severity,
    required this.summary,
    this.createdAt,
    required this.resolved,
    this.planId,
  });

  factory AuditFinding.fromJson(Map<String, dynamic> json) {
    return AuditFinding(
      id: json['id'] as String,
      employeeId: json['employee_id'] as String?,
      severity: json['severity'] as String,
      summary: json['summary'] as String,
      createdAt: json['created_at'] != null
          ? DateTime.parse(json['created_at'] as String)
          : null,
      resolved: json['resolved'] as bool,
      planId: json['plan_id'] as String?,
    );
  }
}

/// AgentReviewResult matches C4: {"drift_score":float,"recent_findings":[...]}
class AgentReviewResult {
  final double driftScore;
  final List<AuditFinding> recentFindings;

  const AgentReviewResult({
    required this.driftScore,
    required this.recentFindings,
  });

  factory AgentReviewResult.fromJson(Map<String, dynamic> json) {
    final recentFindingsRaw = json['recent_findings'] as List? ?? [];
    final recentFindings = recentFindingsRaw
        .whereType<Map<String, dynamic>>()
        .map((f) => AuditFinding.fromJson(f))
        .toList();
    return AgentReviewResult(
      driftScore: (json['drift_score'] as num?)?.toDouble() ?? 0.0,
      recentFindings: recentFindings,
    );
  }
}
