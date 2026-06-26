/// Data model for reflection proposals returned by the daemon's
/// `/api/v1/reflection/proposals` endpoint.
///
/// JSON shape (from `internal/agent/proposal.go`):
/// ```json
/// {
///   "id": "abc123",
///   "type": "skill_create|agent_prompt|project_instruction|prompt_component",
///   "target": ".meept/skills/x/SKILL.md",
///   "change": "<markdown content>",
///   "justification": "why this matters",
///   "confidence": 0.8,
///   "source": "turn:s1|session:s1|manual:/|manual:http",
///   "status": "pending|applied|skipped",
///   "created_at": "2026-06-26T..."
/// }
/// ```
class ReflectionProposal {
  final String id;
  final String type;
  final String target;
  final String change;
  final String justification;
  final double confidence;
  final String source;
  final String status;
  final DateTime? createdAt;

  const ReflectionProposal({
    required this.id,
    required this.type,
    required this.target,
    required this.change,
    required this.justification,
    required this.confidence,
    required this.source,
    required this.status,
    this.createdAt,
  });

  factory ReflectionProposal.fromJson(Map<String, dynamic> json) {
    return ReflectionProposal(
      id: json['id'] as String? ?? '',
      type: json['type'] as String? ?? '',
      target: json['target'] as String? ?? '',
      change: json['change'] as String? ?? '',
      justification: json['justification'] as String? ?? '',
      confidence: (json['confidence'] as num?)?.toDouble() ?? 0.0,
      source: json['source'] as String? ?? '',
      status: json['status'] as String? ?? '',
      createdAt: json['created_at'] != null
          ? DateTime.tryParse(json['created_at'] as String)
          : null,
    );
  }

  /// Whether the target file must be applied manually (propose-only).
  ///
  /// CLAUDE.md, config/agents/*/AGENT.md, and config/prompts/* are
  /// always propose-only — the daemon will not auto-write them.
  bool get isProposeOnly {
    if (target == 'CLAUDE.md') return true;
    if (target.startsWith('config/agents/') && target.endsWith('AGENT.md')) {
      return true;
    }
    if (target.startsWith('config/prompts/')) return true;
    return false;
  }
}
